package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/zparnold/openhands-kubernetes-remote-runtime/pkg/config"
	"github.com/zparnold/openhands-kubernetes-remote-runtime/pkg/k8s"
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

// AuthMiddleware validates API key
func (h *Handler) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiKey := r.Header.Get("X-API-Key")
		if apiKey == "" || apiKey != h.config.APIKey {
			respondError(w, http.StatusUnauthorized, "unauthorized", "Invalid or missing API key")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// LoggingMiddleware logs requests
func (h *Handler) LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		log.Printf("Started %s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
		log.Printf("Completed %s %s in %v", r.Method, r.URL.Path, time.Since(start))
	})
}

// StartRuntime handles POST /start
func (h *Handler) StartRuntime(w http.ResponseWriter, r *http.Request) {
	var req types.StartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	// Validate required fields
	if req.Image == "" {
		respondError(w, http.StatusBadRequest, "invalid_request", "Image is required")
		return
	}
	if req.SessionID == "" {
		respondError(w, http.StatusBadRequest, "invalid_request", "Session ID is required")
		return
	}

	// Check if runtime already exists for this session
	if existingRuntime, err := h.stateMgr.GetRuntimeBySessionID(req.SessionID); err == nil {
		// Runtime exists, return it
		response := h.buildRuntimeResponse(existingRuntime)
		respondJSON(w, http.StatusOK, response)
		return
	}

	// Generate runtime ID and session API key
	runtimeID := generateID()
	sessionAPIKey := generateSessionAPIKey()

	// Build runtime info
	runtimeInfo := &state.RuntimeInfo{
		RuntimeID:     runtimeID,
		SessionID:     req.SessionID,
		URL:           fmt.Sprintf("https://%s.%s", req.SessionID, h.config.BaseDomain),
		SessionAPIKey: sessionAPIKey,
		Status:        types.StatusPending,
		PodStatus:     types.PodStatusPending,
		PodName:       fmt.Sprintf("runtime-%s", runtimeID),
		ServiceName:   fmt.Sprintf("runtime-%s", runtimeID),
		IngressName:   fmt.Sprintf("runtime-%s", runtimeID),
		WorkHosts: map[string]int{
			fmt.Sprintf("https://work-1-%s.%s", req.SessionID, h.config.BaseDomain): h.config.Worker1Port,
			fmt.Sprintf("https://work-2-%s.%s", req.SessionID, h.config.BaseDomain): h.config.Worker2Port,
		},
	}

	// Add to state
	h.stateMgr.AddRuntime(runtimeInfo)

	// Create sandbox in Kubernetes
	ctx := context.Background()
	if err := h.k8sClient.CreateSandbox(ctx, &req, runtimeInfo); err != nil {
		// Remove from state on failure
		_ = h.stateMgr.DeleteRuntime(runtimeID)
		log.Printf("Failed to create sandbox: %v", err)
		respondError(w, http.StatusInternalServerError, "sandbox_creation_failed", fmt.Sprintf("Failed to create sandbox: %v", err))
		return
	}

	// Update status to running
	runtimeInfo.Status = types.StatusRunning
	_ = h.stateMgr.UpdateRuntime(runtimeInfo)

	// Build and return response
	response := h.buildRuntimeResponse(runtimeInfo)
	respondJSON(w, http.StatusOK, response)
}

// StopRuntime handles POST /stop
func (h *Handler) StopRuntime(w http.ResponseWriter, r *http.Request) {
	var req types.StopRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	runtimeInfo, err := h.stateMgr.GetRuntimeByID(req.RuntimeID)
	if err != nil {
		respondError(w, http.StatusNotFound, "runtime_not_found", "Runtime not found")
		return
	}

	ctx := context.Background()
	if err := h.k8sClient.DeleteSandbox(ctx, runtimeInfo); err != nil {
		log.Printf("Failed to delete sandbox: %v", err)
		respondError(w, http.StatusInternalServerError, "sandbox_deletion_failed", fmt.Sprintf("Failed to delete sandbox: %v", err))
		return
	}

	// Update status
	runtimeInfo.Status = types.StatusStopped
	_ = h.stateMgr.UpdateRuntime(runtimeInfo)

	// Remove from state
	_ = h.stateMgr.DeleteRuntime(req.RuntimeID)

	response := h.buildRuntimeResponse(runtimeInfo)
	respondJSON(w, http.StatusOK, response)
}

// PauseRuntime handles POST /pause
func (h *Handler) PauseRuntime(w http.ResponseWriter, r *http.Request) {
	var req types.PauseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	runtimeInfo, err := h.stateMgr.GetRuntimeByID(req.RuntimeID)
	if err != nil {
		respondError(w, http.StatusNotFound, "runtime_not_found", "Runtime not found")
		return
	}

	// For pause, we delete the pod but keep the state
	ctx := context.Background()
	if err := h.k8sClient.ScalePodToZero(ctx, runtimeInfo.PodName); err != nil {
		log.Printf("Failed to pause runtime: %v", err)
		respondError(w, http.StatusInternalServerError, "pause_failed", fmt.Sprintf("Failed to pause runtime: %v", err))
		return
	}

	// Update status
	runtimeInfo.Status = types.StatusPaused
	runtimeInfo.PodStatus = types.PodStatusNotFound
	_ = h.stateMgr.UpdateRuntime(runtimeInfo)

	response := h.buildRuntimeResponse(runtimeInfo)
	respondJSON(w, http.StatusOK, response)
}

// ResumeRuntime handles POST /resume
func (h *Handler) ResumeRuntime(w http.ResponseWriter, r *http.Request) {
	var req types.ResumeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	runtimeInfo, err := h.stateMgr.GetRuntimeByID(req.RuntimeID)
	if err != nil {
		respondError(w, http.StatusNotFound, "runtime_not_found", "Runtime not found")
		return
	}

	if runtimeInfo.Status != types.StatusPaused {
		respondError(w, http.StatusBadRequest, "invalid_state", "Runtime is not paused")
		return
	}

	// Recreate the pod
	// TODO(technical-debt): Store original image, command, and environment in RuntimeInfo
	// so we can recreate the pod exactly as it was. For now, using defaults.
	startReq := &types.StartRequest{
		Image:      h.config.DefaultImage, // This should be stored in RuntimeInfo in production
		Command:    fmt.Sprintf("/usr/local/bin/openhands-agent-server --port %d", h.config.AgentServerPort),
		WorkingDir: "/openhands/code/",
		SessionID:  runtimeInfo.SessionID,
	}

	ctx := context.Background()
	if err := h.k8sClient.RecreatePod(ctx, startReq, runtimeInfo); err != nil {
		log.Printf("Failed to resume runtime: %v", err)
		respondError(w, http.StatusInternalServerError, "resume_failed", fmt.Sprintf("Failed to resume runtime: %v", err))
		return
	}

	// Update status
	runtimeInfo.Status = types.StatusRunning
	runtimeInfo.PodStatus = types.PodStatusPending
	_ = h.stateMgr.UpdateRuntime(runtimeInfo)

	response := h.buildRuntimeResponse(runtimeInfo)
	respondJSON(w, http.StatusOK, response)
}

// ListRuntimes handles GET /list
func (h *Handler) ListRuntimes(w http.ResponseWriter, r *http.Request) {
	runtimes := h.stateMgr.ListRuntimes()

	responses := make([]types.RuntimeResponse, 0, len(runtimes))
	for _, runtime := range runtimes {
		// Update pod status from Kubernetes
		ctx := context.Background()
		if statusInfo, err := h.k8sClient.GetPodStatus(ctx, runtime.PodName); err == nil {
			runtime.PodStatus = statusInfo.Status
			runtime.RestartCount = statusInfo.RestartCount
			runtime.RestartReasons = statusInfo.RestartReasons
			_ = h.stateMgr.UpdateRuntime(runtime)
		}

		responses = append(responses, h.buildRuntimeResponse(runtime))
	}

	respondJSON(w, http.StatusOK, types.ListResponse{Runtimes: responses})
}

// GetRuntime handles GET /runtime/{runtime_id}
func (h *Handler) GetRuntime(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	runtimeID := vars["runtime_id"]

	runtimeInfo, err := h.stateMgr.GetRuntimeByID(runtimeID)
	if err != nil {
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

	runtimeInfo, err := h.stateMgr.GetRuntimeBySessionID(sessionID)
	if err != nil {
		respondError(w, http.StatusNotFound, "session_not_found", "Session not found")
		return
	}

	// Update pod status from Kubernetes
	h.updateRuntimeStatusFromK8s(runtimeInfo)

	response := h.buildRuntimeResponse(runtimeInfo)
	respondJSON(w, http.StatusOK, response)
}

// GetSessionsBatch handles GET /sessions/batch
func (h *Handler) GetSessionsBatch(w http.ResponseWriter, r *http.Request) {
	idsParam := r.URL.Query().Get("ids")
	if idsParam == "" {
		respondError(w, http.StatusBadRequest, "invalid_request", "ids parameter is required")
		return
	}

	sessionIDs := strings.Split(idsParam, ",")
	runtimes := h.stateMgr.GetRuntimesBySessionIDs(sessionIDs)

	responses := make([]types.RuntimeResponse, 0, len(runtimes))
	for _, runtime := range runtimes {
		// Update pod status from Kubernetes
		ctx := context.Background()
		if statusInfo, err := h.k8sClient.GetPodStatus(ctx, runtime.PodName); err == nil {
			runtime.PodStatus = statusInfo.Status
			runtime.RestartCount = statusInfo.RestartCount
			runtime.RestartReasons = statusInfo.RestartReasons
			_ = h.stateMgr.UpdateRuntime(runtime)
		}

		responses = append(responses, h.buildRuntimeResponse(runtime))
	}

	respondJSON(w, http.StatusOK, types.BatchSessionsResponse{Sessions: responses})
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
		respondError(w, http.StatusBadRequest, "invalid_request", "image parameter is required")
		return
	}

	// For MVP, we'll assume all images exist
	// In production, this should actually check the registry
	respondJSON(w, http.StatusOK, types.ImageExistsResponse{
		Exists: true,
	})
}

// buildRuntimeResponse builds a RuntimeResponse from RuntimeInfo
func (h *Handler) buildRuntimeResponse(info *state.RuntimeInfo) types.RuntimeResponse {
	return types.RuntimeResponse{
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
}

// updateRuntimeStatusFromK8s updates runtime info with latest pod status from Kubernetes
func (h *Handler) updateRuntimeStatusFromK8s(runtimeInfo *state.RuntimeInfo) {
	ctx := context.Background()
	if statusInfo, err := h.k8sClient.GetPodStatus(ctx, runtimeInfo.PodName); err == nil {
		runtimeInfo.PodStatus = statusInfo.Status
		runtimeInfo.RestartCount = statusInfo.RestartCount
		runtimeInfo.RestartReasons = statusInfo.RestartReasons
		_ = h.stateMgr.UpdateRuntime(runtimeInfo)
	}
}

// Helper functions
func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("Error encoding JSON response: %v", err)
	}
}

func respondError(w http.ResponseWriter, status int, errorType, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(types.ErrorResponse{
		Error:   errorType,
		Message: message,
	}); err != nil {
		log.Printf("Error encoding error response: %v", err)
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
