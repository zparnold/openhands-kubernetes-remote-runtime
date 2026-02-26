package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/zparnold/openhands-kubernetes-remote-runtime/pkg/config"
	"github.com/zparnold/openhands-kubernetes-remote-runtime/pkg/k8s"
	"github.com/zparnold/openhands-kubernetes-remote-runtime/pkg/logger"
	"github.com/zparnold/openhands-kubernetes-remote-runtime/pkg/state"
	"github.com/zparnold/openhands-kubernetes-remote-runtime/pkg/types"
)

// Handler handles HTTP requests
type Handler struct {
	k8sClient *k8s.Client
	stateMgr  *state.StateManager
	config    *config.Config
}

// NewHandler creates a new API handler
func NewHandler(k8sClient *k8s.Client, stateMgr *state.StateManager, cfg *config.Config) *Handler {
	return &Handler{
		k8sClient: k8sClient,
		stateMgr:  stateMgr,
		config:    cfg,
	}
}

// pathIsSandboxProxy returns true if the request is for /sandbox/{runtime_id}/...
// These requests are reverse-proxied to the sandbox pod. The sandbox validates
// X-Session-API-Key; the runtime API does not require X-API-Key (management key)
// for these paths. Covers: /alive health checks, /api/bash/*, /api/conversations,
// /vscode/*, and all other agent-server endpoints.
func pathIsSandboxProxy(r *http.Request) bool {
	path := r.URL.Path
	const prefix = "/sandbox/"
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	rest := strings.TrimPrefix(path, prefix)
	// Must have runtime ID: /sandbox/xxx or /sandbox/xxx/...
	return len(rest) > 0
}

// AuthMiddleware validates API key for management endpoints (/start, /stop, /list, etc.).
// Paths under /sandbox/{runtime_id}/... bypass this check; they are proxied to the
// sandbox pod which validates X-Session-API-Key.
func (h *Handler) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if pathIsSandboxProxy(r) {
			logger.Debug("AuthMiddleware: Allowing /sandbox/... (auth by sandbox)")
			next.ServeHTTP(w, r)
			return
		}
		apiKey := r.Header.Get("X-API-Key")
		logger.Debug("AuthMiddleware: Checking API key for %s %s", r.Method, r.URL.Path)
		if apiKey == "" || apiKey != h.config.APIKey {
			logger.Debug("AuthMiddleware: Invalid or missing API key")
			respondError(w, http.StatusUnauthorized, "unauthorized", "Invalid or missing API key")
			return
		}
		logger.Debug("AuthMiddleware: API key validated successfully")
		next.ServeHTTP(w, r)
	})
}

// LoggingMiddleware logs requests
func (h *Handler) LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		logger.Info("Started %s %s", r.Method, r.URL.Path)

		// Log request details in debug mode
		// ⚠️ SECURITY WARNING: Debug mode logs complete request headers and bodies
		// This includes sensitive data: API keys (X-API-Key), authorization tokens,
		// credentials, session tokens, and environment variables.
		// Only enable debug mode in secure, controlled environments.
		if logger.IsDebugEnabled() {
			logger.Debug("Request Headers: %v", r.Header)
			if r.Body != nil {
				// Read body for logging, then restore it
				bodyBytes, err := io.ReadAll(r.Body)
				if err == nil {
					logger.Debug("Request Body: %s", string(bodyBytes))
					// Restore body for handler
					r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
				} else {
					logger.Debug("Request Body: <unable to read: %v>", err)
					// On error, restore an empty body to prevent nil pointer issues
					r.Body = io.NopCloser(bytes.NewReader([]byte{}))
				}
			}
		}

		next.ServeHTTP(w, r)
		logger.Info("Completed %s %s in %v", r.Method, r.URL.Path, time.Since(start))
	})
}

// StartRuntime handles POST /start
func (h *Handler) StartRuntime(w http.ResponseWriter, r *http.Request) {
	var req types.StartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.Debug("StartRuntime: Failed to decode request body: %v", err)
		respondError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	logger.Debug("StartRuntime: Request decoded - SessionID: %s, Image: %s", req.SessionID, req.Image)

	// Validate required fields
	if req.Image == "" {
		logger.Debug("StartRuntime: Missing required field 'image'")
		respondError(w, http.StatusBadRequest, "invalid_request", "Image is required")
		return
	}
	if req.SessionID == "" {
		logger.Debug("StartRuntime: Missing required field 'session_id'")
		respondError(w, http.StatusBadRequest, "invalid_request", "Session ID is required")
		return
	}

	// Check if runtime already exists for this session
	if existingRuntime, err := h.stateMgr.GetRuntimeBySessionID(req.SessionID); err == nil {
		// Runtime exists, return it
		logger.Debug("StartRuntime: Found existing runtime for session %s: %s", req.SessionID, existingRuntime.RuntimeID)
		response := h.buildRuntimeResponse(existingRuntime)
		respondJSON(w, http.StatusOK, response)
		return
	}

	// Generate runtime ID and session API key
	runtimeID := generateID()
	sessionAPIKey := generateSessionAPIKey()
	logger.Debug("StartRuntime: Generated RuntimeID: %s, SessionID: %s", runtimeID, req.SessionID)

	// Session ID for hostnames must be lowercase (RFC 1123 subdomain); keep original for lookups
	sessionIDForHost := strings.ToLower(req.SessionID)
	// Build runtime info
	runtimeInfo := &state.RuntimeInfo{
		RuntimeID:        runtimeID,
		SessionID:        req.SessionID,
		URL:              fmt.Sprintf("https://%s.%s", sessionIDForHost, h.config.BaseDomain),
		SessionAPIKey:    sessionAPIKey,
		Status:           types.StatusPending,
		PodStatus:        types.PodStatusPending,
		PodName:          fmt.Sprintf("runtime-%s", runtimeID),
		ServiceName:      fmt.Sprintf("runtime-%s", runtimeID),
		IngressName:      fmt.Sprintf("runtime-%s", runtimeID),
		CreatedAt:        time.Now(),
		LastActivityTime: time.Now(),
		WorkHosts: map[string]int{
			fmt.Sprintf("https://work-1-%s.%s", sessionIDForHost, h.config.BaseDomain): h.config.Worker1Port,
			fmt.Sprintf("https://work-2-%s.%s", sessionIDForHost, h.config.BaseDomain): h.config.Worker2Port,
		},
	}

	logger.Debug("StartRuntime: Runtime info created - URL: %s, PodName: %s", runtimeInfo.URL, runtimeInfo.PodName)

	// Add to state
	h.stateMgr.AddRuntime(runtimeInfo)
	logger.Debug("StartRuntime: Added runtime to state manager")

	// Create sandbox in Kubernetes with operation timeout
	ctx, cancel := context.WithTimeout(r.Context(), h.config.K8sOperationTimeout)
	defer cancel()
	logger.Debug("StartRuntime: Creating sandbox in Kubernetes...")
	if err := h.k8sClient.CreateSandbox(ctx, &req, runtimeInfo); err != nil {
		// Remove from state on failure
		_ = h.stateMgr.DeleteRuntime(runtimeID)
		logger.Info("Failed to create sandbox: %v", err)
		respondError(w, http.StatusInternalServerError, "sandbox_creation_failed", fmt.Sprintf("Failed to create sandbox: %v", err))
		return
	}

	logger.Debug("StartRuntime: Sandbox created successfully")

	// Update status to running
	runtimeInfo.Status = types.StatusRunning
	_ = h.stateMgr.UpdateRuntime(runtimeInfo)
	logger.Debug("StartRuntime: Updated runtime status to running")

	// Build and return response
	response := h.buildRuntimeResponse(runtimeInfo)
	logger.Debug("StartRuntime: Returning response for runtime %s", runtimeID)
	respondJSON(w, http.StatusOK, response)
}

// StopRuntime handles POST /stop
func (h *Handler) StopRuntime(w http.ResponseWriter, r *http.Request) {
	var req types.StopRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.Debug("StopRuntime: Failed to decode request body: %v", err)
		respondError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	logger.Debug("StopRuntime: Request decoded - RuntimeID: %s", req.RuntimeID)

	runtimeInfo, err := h.stateMgr.GetRuntimeByID(req.RuntimeID)
	if err != nil {
		logger.Debug("StopRuntime: Runtime not found: %s", req.RuntimeID)
		respondError(w, http.StatusNotFound, "runtime_not_found", "Runtime not found")
		return
	}

	logger.Debug("StopRuntime: Deleting sandbox for runtime %s (Pod: %s)", req.RuntimeID, runtimeInfo.PodName)

	ctx, cancel := context.WithTimeout(r.Context(), h.config.K8sOperationTimeout)
	defer cancel()
	if err := h.k8sClient.DeleteSandbox(ctx, runtimeInfo); err != nil {
		logger.Info("Failed to delete sandbox: %v", err)
		respondError(w, http.StatusInternalServerError, "sandbox_deletion_failed", fmt.Sprintf("Failed to delete sandbox: %v", err))
		return
	}

	logger.Debug("StopRuntime: Sandbox deleted successfully")

	// Update status
	runtimeInfo.Status = types.StatusStopped
	_ = h.stateMgr.UpdateRuntime(runtimeInfo)

	// Remove from state
	_ = h.stateMgr.DeleteRuntime(req.RuntimeID)
	logger.Debug("StopRuntime: Removed runtime from state")

	response := h.buildRuntimeResponse(runtimeInfo)
	respondJSON(w, http.StatusOK, response)
}

// PauseRuntime handles POST /pause
func (h *Handler) PauseRuntime(w http.ResponseWriter, r *http.Request) {
	var req types.PauseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.Debug("PauseRuntime: Failed to decode request body: %v", err)
		respondError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	logger.Debug("PauseRuntime: Request decoded - RuntimeID: %s", req.RuntimeID)

	runtimeInfo, err := h.stateMgr.GetRuntimeByID(req.RuntimeID)
	if err != nil {
		logger.Debug("PauseRuntime: Runtime not found: %s", req.RuntimeID)
		respondError(w, http.StatusNotFound, "runtime_not_found", "Runtime not found")
		return
	}

	logger.Debug("PauseRuntime: Scaling pod to zero for runtime %s (Pod: %s)", req.RuntimeID, runtimeInfo.PodName)

	// For pause, we delete the pod but keep the state
	ctx, cancel := context.WithTimeout(r.Context(), h.config.K8sOperationTimeout)
	defer cancel()
	if err := h.k8sClient.ScalePodToZero(ctx, runtimeInfo.PodName); err != nil {
		logger.Info("Failed to pause runtime: %v", err)
		respondError(w, http.StatusInternalServerError, "pause_failed", fmt.Sprintf("Failed to pause runtime: %v", err))
		return
	}

	logger.Debug("PauseRuntime: Pod scaled to zero successfully")

	// Update status
	runtimeInfo.Status = types.StatusPaused
	runtimeInfo.PodStatus = types.PodStatusNotFound
	_ = h.stateMgr.UpdateRuntime(runtimeInfo)
	logger.Debug("PauseRuntime: Updated runtime status to paused")

	response := h.buildRuntimeResponse(runtimeInfo)
	respondJSON(w, http.StatusOK, response)
}

// ResumeRuntime handles POST /resume
func (h *Handler) ResumeRuntime(w http.ResponseWriter, r *http.Request) {
	var req types.ResumeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.Debug("ResumeRuntime: Failed to decode request body: %v", err)
		respondError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	logger.Debug("ResumeRuntime: Request decoded - RuntimeID: %s", req.RuntimeID)

	runtimeInfo, err := h.stateMgr.GetRuntimeByID(req.RuntimeID)
	if err != nil {
		logger.Debug("ResumeRuntime: Runtime not found: %s", req.RuntimeID)
		respondError(w, http.StatusNotFound, "runtime_not_found", "Runtime not found")
		return
	}

	// Already running: no-op (e.g. WebSocket recovery calls resume for running sandboxes)
	if runtimeInfo.Status == types.StatusRunning {
		logger.Debug("ResumeRuntime: Runtime %s already running, no-op", req.RuntimeID)
		response := h.buildRuntimeResponse(runtimeInfo)
		respondJSON(w, http.StatusOK, response)
		return
	}

	if runtimeInfo.Status != types.StatusPaused {
		logger.Debug("ResumeRuntime: Runtime %s is not paused (status: %s)", req.RuntimeID, runtimeInfo.Status)
		respondError(w, http.StatusBadRequest, "invalid_state", "Runtime is not paused")
		return
	}

	logger.Debug("ResumeRuntime: Recreating pod for runtime %s", req.RuntimeID)

	// Recreate the pod
	// TODO(technical-debt): Store original image, command, and environment in RuntimeInfo
	// so we can recreate the pod exactly as it was. For now, using defaults.
	startReq := &types.StartRequest{
		Image:      h.config.DefaultImage, // This should be stored in RuntimeInfo in production
		Command:    types.FlexibleCommand{"/usr/local/bin/openhands-agent-server", "--port", fmt.Sprintf("%d", h.config.AgentServerPort)},
		WorkingDir: "/openhands/code/",
		SessionID:  runtimeInfo.SessionID,
	}

	ctx, cancel := context.WithTimeout(r.Context(), h.config.K8sOperationTimeout)
	defer cancel()
	if err := h.k8sClient.RecreatePod(ctx, startReq, runtimeInfo); err != nil {
		logger.Info("Failed to resume runtime: %v", err)
		respondError(w, http.StatusInternalServerError, "resume_failed", fmt.Sprintf("Failed to resume runtime: %v", err))
		return
	}

	logger.Debug("ResumeRuntime: Pod recreated successfully")

	// Update status
	runtimeInfo.Status = types.StatusRunning
	runtimeInfo.PodStatus = types.PodStatusPending
	_ = h.stateMgr.UpdateRuntime(runtimeInfo)
	logger.Debug("ResumeRuntime: Updated runtime status to running")

	response := h.buildRuntimeResponse(runtimeInfo)
	respondJSON(w, http.StatusOK, response)
}

// ListRuntimes handles GET /list
func (h *Handler) ListRuntimes(w http.ResponseWriter, r *http.Request) {
	logger.Debug("ListRuntimes: Fetching all runtimes")
	runtimes := h.stateMgr.ListRuntimes()
	logger.Debug("ListRuntimes: Found %d runtimes", len(runtimes))

	responses := make([]types.RuntimeResponse, 0, len(runtimes))
	for _, runtime := range runtimes {
		// Update pod status from Kubernetes
		ctx, cancel := context.WithTimeout(r.Context(), h.config.K8sQueryTimeout)
		if statusInfo, err := h.k8sClient.GetPodStatus(ctx, runtime.PodName); err == nil {
			runtime.PodStatus = statusInfo.Status
			runtime.RestartCount = statusInfo.RestartCount
			runtime.RestartReasons = statusInfo.RestartReasons
			_ = h.stateMgr.UpdateRuntime(runtime)
		}
		cancel() // Cancel immediately after use in loop

		responses = append(responses, h.buildRuntimeResponse(runtime))
	}

	logger.Debug("ListRuntimes: Returning %d runtime responses", len(responses))
	respondJSON(w, http.StatusOK, types.ListResponse{Runtimes: responses})
}

// GetRuntime handles GET /runtime/{runtime_id}
func (h *Handler) GetRuntime(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	runtimeID := vars["runtime_id"]
	logger.Debug("GetRuntime: Fetching runtime %s", runtimeID)

	runtimeInfo, err := h.stateMgr.GetRuntimeByID(runtimeID)
	if err != nil {
		logger.Debug("GetRuntime: Runtime not found: %s", runtimeID)
		respondError(w, http.StatusNotFound, "runtime_not_found", "Runtime not found")
		return
	}

	// Update pod status from Kubernetes
	h.updateRuntimeStatusFromK8s(runtimeInfo)

	response := h.buildRuntimeResponse(runtimeInfo)
	respondJSON(w, http.StatusOK, response)
}

// GetSession handles GET /sessions/{session_id}
func (h *Handler) GetSession(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	sessionID := vars["session_id"]
	logger.Debug("GetSession: Fetching session %s", sessionID)

	runtimeInfo, err := h.stateMgr.GetRuntimeBySessionID(sessionID)
	if err != nil {
		// State was lost (e.g. runtime API restart); try to discover from Kubernetes
		if h.k8sClient != nil {
			ctx, cancel := context.WithTimeout(r.Context(), h.config.K8sQueryTimeout)
			defer cancel()
			if discovered, discoverErr := h.k8sClient.DiscoverRuntimeBySessionID(ctx, sessionID); discoverErr == nil && discovered != nil {
				logger.Info("GetSession: Recovered session %s from Kubernetes (state was lost)", sessionID)
				h.stateMgr.AddRuntime(discovered)
				runtimeInfo = discovered
			} else {
				logger.Debug("GetSession: Session not found: %s", sessionID)
				respondError(w, http.StatusNotFound, "session_not_found", "Session not found")
				return
			}
		} else {
			logger.Debug("GetSession: Session not found: %s", sessionID)
			respondError(w, http.StatusNotFound, "session_not_found", "Session not found")
			return
		}
	}

	// Update pod status from Kubernetes
	h.updateRuntimeStatusFromK8s(runtimeInfo)

	response := h.buildRuntimeResponse(runtimeInfo)
	respondJSON(w, http.StatusOK, response)
}

// GetSessionsBatch handles GET /sessions/batch
func (h *Handler) GetSessionsBatch(w http.ResponseWriter, r *http.Request) {
	// Support both ?ids=1,2,3 and ?ids=1&ids=2&ids=3
	var sessionIDs []string
	for _, idStr := range r.URL.Query()["ids"] {
		for _, id := range strings.Split(idStr, ",") {
			if trimmed := strings.TrimSpace(id); trimmed != "" {
				sessionIDs = append(sessionIDs, trimmed)
			}
		}
	}
	if len(sessionIDs) == 0 {
		respondError(w, http.StatusBadRequest, "invalid_request", "ids parameter is required")
		return
	}
	logger.Debug("GetSessionsBatch: Fetching %d sessions", len(sessionIDs))

	// Build runtimes list, discovering from Kubernetes for any not in state
	ctx, cancel := context.WithTimeout(r.Context(), h.config.K8sQueryTimeout)
	defer cancel()
	runtimesBySession := make(map[string]*state.RuntimeInfo)
	for _, sessionID := range sessionIDs {
		if sessionID == "" {
			continue
		}
		if runtime, err := h.stateMgr.GetRuntimeBySessionID(sessionID); err == nil {
			runtimesBySession[sessionID] = runtime
		} else if h.k8sClient != nil {
			if discovered, discoverErr := h.k8sClient.DiscoverRuntimeBySessionID(ctx, sessionID); discoverErr == nil && discovered != nil {
				logger.Info("GetSessionsBatch: Recovered session %s from Kubernetes (state was lost)", sessionID)
				h.stateMgr.AddRuntime(discovered)
				runtimesBySession[sessionID] = discovered
			}
		}
	}

	// Collect all pod names and fetch their statuses in a single K8s API call.
	if h.k8sClient != nil {
		podNames := make([]string, 0, len(runtimesBySession))
		for _, runtime := range runtimesBySession {
			podNames = append(podNames, runtime.PodName)
		}
		if statuses, err := h.k8sClient.GetPodStatuses(ctx, podNames); err == nil {
			for _, runtime := range runtimesBySession {
				if statusInfo, ok := statuses[runtime.PodName]; ok {
					runtime.PodStatus = statusInfo.Status
					runtime.RestartCount = statusInfo.RestartCount
					runtime.RestartReasons = statusInfo.RestartReasons
					_ = h.stateMgr.UpdateRuntime(runtime)
				}
			}
		}
	}

	responses := make([]types.RuntimeResponse, 0, len(runtimesBySession))
	for _, sessionID := range sessionIDs {
		runtime, ok := runtimesBySession[sessionID]
		if !ok {
			continue
		}
		responses = append(responses, h.buildRuntimeResponse(runtime))
	}

	logger.Debug("GetSessionsBatch: Returning %d runtime responses", len(responses))
	// Return a plain JSON array – the OpenHands app server iterates over the
	// response directly as a list of runtime objects.
	respondJSON(w, http.StatusOK, responses)
}

// BatchGetConversations handles POST /sessions/batch-conversations
// It fans out requests to agent-server pods in-cluster to batch-fetch conversation statuses,
// eliminating the need for the caller to make N individual proxy calls.
func (h *Handler) BatchGetConversations(w http.ResponseWriter, r *http.Request) {
	var req types.BatchConversationsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.Debug("BatchGetConversations: Failed to decode request body: %v", err)
		respondError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	if len(req.Sandboxes) == 0 {
		logger.Debug("BatchGetConversations: Empty sandboxes map")
		respondJSON(w, http.StatusOK, map[string]json.RawMessage{})
		return
	}

	logger.Debug("BatchGetConversations: Fetching conversations for %d sandboxes", len(req.Sandboxes))

	// Fan out requests concurrently
	type result struct {
		runtimeID string
		data      json.RawMessage
	}

	resultsCh := make(chan result, len(req.Sandboxes))
	var wg sync.WaitGroup

	for runtimeID, sandbox := range req.Sandboxes {
		wg.Add(1)
		go func(rtID string, sb types.BatchConversationSandbox) {
			defer wg.Done()

			// Look up runtime info by runtime ID first, fall back to session ID
			runtimeInfo, err := h.stateMgr.GetRuntimeByID(rtID)
			if err != nil {
				// Try by session ID
				runtimeInfo, err = h.stateMgr.GetRuntimeBySessionID(sb.SessionID)
				if err != nil {
					logger.Debug("BatchGetConversations: Runtime not found for %s (session %s)", rtID, sb.SessionID)
					resultsCh <- result{runtimeID: rtID, data: json.RawMessage("[]")}
					return
				}
			}

			// Build in-cluster URL
			ids := strings.Join(sb.ConversationIDs, ",")
			inClusterURL := fmt.Sprintf("http://%s.%s.svc.cluster.local:%d/api/conversations?ids=%s",
				runtimeInfo.ServiceName, h.config.Namespace, h.config.AgentServerPort, ids)

			logger.Debug("BatchGetConversations: Fetching %s", inClusterURL)

			// Make HTTP request with timeout
			ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			defer cancel()

			httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, inClusterURL, nil)
			if err != nil {
				logger.Debug("BatchGetConversations: Failed to create request for %s: %v", rtID, err)
				resultsCh <- result{runtimeID: rtID, data: json.RawMessage("[]")}
				return
			}
			httpReq.Header.Set("X-Session-API-Key", runtimeInfo.SessionAPIKey)

			resp, err := http.DefaultClient.Do(httpReq)
			if err != nil {
				logger.Debug("BatchGetConversations: Request failed for %s: %v", rtID, err)
				resultsCh <- result{runtimeID: rtID, data: json.RawMessage("[]")}
				return
			}
			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				logger.Debug("BatchGetConversations: Failed to read response for %s: %v", rtID, err)
				resultsCh <- result{runtimeID: rtID, data: json.RawMessage("[]")}
				return
			}

			if resp.StatusCode != http.StatusOK {
				logger.Debug("BatchGetConversations: Non-200 status for %s: %d", rtID, resp.StatusCode)
				resultsCh <- result{runtimeID: rtID, data: json.RawMessage("[]")}
				return
			}

			// Pass through the raw JSON from the agent-server
			resultsCh <- result{runtimeID: rtID, data: json.RawMessage(body)}
		}(runtimeID, sandbox)
	}

	// Wait for all goroutines to complete, then close channel
	go func() {
		wg.Wait()
		close(resultsCh)
	}()

	// Aggregate results
	response := make(map[string]json.RawMessage, len(req.Sandboxes))
	for res := range resultsCh {
		response[res.runtimeID] = res.data
	}

	logger.Debug("BatchGetConversations: Returning results for %d sandboxes", len(response))
	respondJSON(w, http.StatusOK, response)
}

// GetRegistryPrefix handles GET /registry_prefix
func (h *Handler) GetRegistryPrefix(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, types.RegistryPrefixResponse{
		RegistryPrefix: h.config.RegistryPrefix,
	})
}

// CheckImageExists handles GET /image_exists
func (h *Handler) CheckImageExists(w http.ResponseWriter, r *http.Request) {
	image := r.URL.Query().Get("image")
	if image == "" {
		logger.Debug("CheckImageExists: Missing 'image' parameter")
		respondError(w, http.StatusBadRequest, "invalid_request", "image parameter is required")
		return
	}

	logger.Debug("CheckImageExists: Checking image %s", image)
	// For MVP, we'll assume all images exist
	// In production, this should actually check the registry
	respondJSON(w, http.StatusOK, types.ImageExistsResponse{
		Exists: true,
	})
}

// buildRuntimeResponse builds a RuntimeResponse from RuntimeInfo
func (h *Handler) buildRuntimeResponse(info *state.RuntimeInfo) types.RuntimeResponse {
	resp := types.RuntimeResponse{
		RuntimeID:      info.RuntimeID,
		SessionID:      info.SessionID,
		URL:            info.URL,
		SessionAPIKey:  info.SessionAPIKey,
		Status:         info.Status,
		PodStatus:      info.PodStatus,
		WorkHosts:      info.WorkHosts,
		RestartCount:   info.RestartCount,
		RestartReasons: info.RestartReasons,
	}
	if h.config.ProxyBaseURL != "" {
		base := strings.TrimSuffix(h.config.ProxyBaseURL, "/")
		resp.URL = fmt.Sprintf("%s/sandbox/%s", base, info.RuntimeID)
		resp.VSCodeURL = fmt.Sprintf("%s/sandbox/%s/vscode", base, info.RuntimeID)
	}
	return resp
}

// updateRuntimeStatusFromK8s updates runtime info with latest pod status from Kubernetes
func (h *Handler) updateRuntimeStatusFromK8s(runtimeInfo *state.RuntimeInfo) {
	ctx, cancel := context.WithTimeout(context.Background(), h.config.K8sQueryTimeout)
	defer cancel()
	if statusInfo, err := h.k8sClient.GetPodStatus(ctx, runtimeInfo.PodName); err == nil {
		runtimeInfo.PodStatus = statusInfo.Status
		runtimeInfo.RestartCount = statusInfo.RestartCount
		runtimeInfo.RestartReasons = statusInfo.RestartReasons
		_ = h.stateMgr.UpdateRuntime(runtimeInfo)
	}
}

// ProxySandbox reverse-proxies requests to the sandbox pod (agent or vscode port) via in-cluster service.
// Path format: /sandbox/{runtime_id}/... or /sandbox/{runtime_id}/vscode/...
// Used when PROXY_BASE_URL is set to avoid per-sandbox DNS (single stable DNS for the runtime API).
func (h *Handler) ProxySandbox(w http.ResponseWriter, r *http.Request) {
	// Use EscapedPath to preserve percent-encoding (e.g. %2F in file upload paths).
	// r.URL.Path is decoded so %2F becomes / — we need the raw form for the backend.
	path := r.URL.EscapedPath()
	const prefix = "/sandbox/"
	if !strings.HasPrefix(path, prefix) {
		respondError(w, http.StatusNotFound, "not_found", "Not found")
		return
	}
	rest := strings.TrimPrefix(path, prefix)
	if rest == "" {
		respondError(w, http.StatusNotFound, "not_found", "Not found")
		return
	}
	// Split on first "/" only — runtime ID is never percent-encoded
	parts := strings.SplitN(rest, "/", 2)
	runtimeID := parts[0]
	if runtimeID == "" {
		respondError(w, http.StatusNotFound, "not_found", "Not found")
		return
	}
	// backendRawPath preserves percent-encoding from the original request
	var backendRawPath string
	var backendPort int
	if len(parts) == 2 && (parts[1] == "vscode" || strings.HasPrefix(parts[1], "vscode/")) {
		backendPort = h.config.VSCodePort
		if parts[1] == "vscode" {
			backendRawPath = "/"
		} else {
			// Remove "vscode" prefix and ensure we don't create double slashes
			backendRawPath = strings.TrimPrefix(parts[1], "vscode")
			if !strings.HasPrefix(backendRawPath, "/") {
				backendRawPath = "/" + backendRawPath
			}
		}
	} else {
		backendPort = h.config.AgentServerPort
		if len(parts) == 2 {
			backendRawPath = "/" + parts[1]
		} else {
			backendRawPath = "/"
		}
	}

	runtimeInfo, err := h.stateMgr.GetRuntimeByID(runtimeID)
	if err != nil {
		// State was lost (e.g. runtime API restart); try to discover from Kubernetes
		if h.k8sClient != nil {
			ctx, cancel := context.WithTimeout(r.Context(), h.config.K8sQueryTimeout)
			defer cancel()
			if discovered, discoverErr := h.k8sClient.DiscoverRuntimeByRuntimeID(ctx, runtimeID); discoverErr == nil && discovered != nil {
				logger.Info("ProxySandbox: Recovered runtime %s from Kubernetes (state was lost)", runtimeID)
				h.stateMgr.AddRuntime(discovered)
				runtimeInfo = discovered
			} else {
				logger.Debug("ProxySandbox: Runtime not found: %s", runtimeID)
				respondError(w, http.StatusNotFound, "runtime_not_found", "Runtime not found")
				return
			}
		} else {
			logger.Debug("ProxySandbox: Runtime not found: %s", runtimeID)
			respondError(w, http.StatusNotFound, "runtime_not_found", "Runtime not found")
			return
		}
	}

	// Update last activity time for this sandbox
	_ = h.stateMgr.UpdateLastActivity(runtimeID)

	// Build backend URL with the raw (percent-encoded) path preserved.
	// We construct scheme+host separately and set the path via RawPath so that
	// url.Parse does not decode percent-encoded characters (e.g. %2F → /).
	backendBase := fmt.Sprintf("http://%s.%s.svc.cluster.local:%d",
		runtimeInfo.ServiceName, h.config.Namespace, backendPort)
	target, err := url.Parse(backendBase)
	if err != nil {
		logger.Debug("ProxySandbox: Invalid backend URL: %v", err)
		respondError(w, http.StatusInternalServerError, "proxy_error", "Invalid backend URL")
		return
	}

	proxy := httputil.NewSingleHostReverseProxy(target) //nolint:gosec // G704: target is built from trusted pod IP, not user input
	proxy.Director = func(req *http.Request) {
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		// Set both Path (decoded, for Go internals) and RawPath (encoded, sent on wire)
		decodedPath, _ := url.PathUnescape(backendRawPath)
		req.URL.Path = decodedPath
		req.URL.RawPath = backendRawPath
		req.URL.RawQuery = r.URL.RawQuery
		req.Host = target.Host
		if req.Header == nil {
			req.Header = make(http.Header)
		}
		// Forward session API key so sandbox can validate
		if v := r.Header.Get("X-Session-API-Key"); v != "" {
			req.Header.Set("X-Session-API-Key", v)
		}
	}

	// Rewrite Set-Cookie and Location headers to use the correct path for the proxy
	proxy.ModifyResponse = h.createProxyResponseRewriter(runtimeID, backendPort)

	proxy.ServeHTTP(w, r) //nolint:gosec // G704: proxy target is a trusted internal pod address
}

// createProxyResponseRewriter creates a response modifier that rewrites Set-Cookie and Location headers
// to use the correct proxy path format (/sandbox/{runtime_id}/...).
func (h *Handler) createProxyResponseRewriter(runtimeID string, backendPort int) func(*http.Response) error {
	return func(resp *http.Response) error {
		// Determine the proxy prefix based on backend port
		var proxyPrefix string
		if backendPort == h.config.VSCodePort {
			proxyPrefix = fmt.Sprintf("/sandbox/%s/vscode", runtimeID)
		} else {
			proxyPrefix = fmt.Sprintf("/sandbox/%s", runtimeID)
		}

		// Rewrite Location header for redirects
		if location := resp.Header.Get("Location"); location != "" {
			// Parse the location URL
			locURL, err := url.Parse(location)
			if err == nil && locURL.Host == "" {
				// Relative URL - prepend proxy prefix
				if !strings.HasPrefix(locURL.Path, proxyPrefix) {
					locURL.Path = proxyPrefix + locURL.Path
					resp.Header.Set("Location", locURL.String())
				}
			}
		}

		// Rewrite Set-Cookie Path attribute
		cookies := resp.Header.Values("Set-Cookie")
		if len(cookies) > 0 {
			resp.Header.Del("Set-Cookie")
			for _, cookie := range cookies {
				// Parse the cookie to rewrite the Path attribute
				rewrittenCookie := rewriteCookiePath(cookie, proxyPrefix)
				resp.Header.Add("Set-Cookie", rewrittenCookie)
			}
		}

		return nil
	}
}

// rewriteCookiePath rewrites the Path attribute in a Set-Cookie header value.
// If no Path is present, adds Path=proxyPrefix. If Path=/, changes to Path=proxyPrefix.
func rewriteCookiePath(cookieHeader, proxyPrefix string) string {
	// Split cookie into attributes
	parts := strings.Split(cookieHeader, ";")
	hasPath := false
	for i, part := range parts {
		trimmed := strings.TrimSpace(part)
		if strings.HasPrefix(strings.ToLower(trimmed), "path=") {
			hasPath = true
			// Extract current path value
			pathValue := strings.TrimPrefix(trimmed, "path=")
			pathValue = strings.TrimPrefix(pathValue, "Path=")
			pathValue = strings.TrimPrefix(pathValue, "PATH=")
			pathValue = strings.TrimSpace(pathValue)

			// Rewrite root path to proxy prefix
			if pathValue == "/" || pathValue == "" {
				parts[i] = " Path=" + proxyPrefix
			}
		}
	}

	// If no Path attribute found, add it
	if !hasPath {
		parts = append(parts, " Path="+proxyPrefix)
	}

	return strings.Join(parts, ";")
}

// Helper functions
func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	// Log response in debug mode
	if logger.IsDebugEnabled() {
		if jsonBytes, err := json.Marshal(data); err == nil {
			logger.Debug("Response [%d]: %s", status, string(jsonBytes))
		}
	}

	if err := json.NewEncoder(w).Encode(data); err != nil {
		logger.Info("Error encoding JSON response: %v", err)
	}
}

func respondError(w http.ResponseWriter, status int, errorType, message string) {
	logger.Debug("Error response [%d]: %s - %s", status, errorType, message)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(types.ErrorResponse{
		Error:   errorType,
		Message: message,
	}); err != nil {
		logger.Info("Error encoding error response: %v", err)
	}
}

func generateID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based ID if crypto rand fails
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func generateSessionAPIKey() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based key if crypto rand fails
		return fmt.Sprintf("%d-%d", time.Now().UnixNano(), time.Now().Unix())
	}
	return hex.EncodeToString(b)
}
