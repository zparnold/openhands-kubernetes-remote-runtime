package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/zparnold/openhands-kubernetes-remote-runtime/pkg/config"
	"github.com/zparnold/openhands-kubernetes-remote-runtime/pkg/k8s"
	"github.com/zparnold/openhands-kubernetes-remote-runtime/pkg/logger"
	"github.com/zparnold/openhands-kubernetes-remote-runtime/pkg/state"
	"github.com/zparnold/openhands-kubernetes-remote-runtime/pkg/types"
)

func setupTestHandler() (*Handler, *state.StateManager) {
	cfg := &config.Config{
		ServerPort:      "8080",
		APIKey:          "test-api-key",
		Namespace:       "test",
		BaseDomain:      "test.example.com",
		Worker1Port:     12000,
		Worker2Port:     12001,
		AgentServerPort: 60000,
		VSCodePort:      60001,
		DefaultImage:    "test-image",
	}
	stateMgr := state.NewStateManager()

	// Create handler without k8s client for tests that don't need it
	handler := &Handler{
		k8sClient: nil,
		stateMgr:  stateMgr,
		config:    cfg,
	}

	return handler, stateMgr
}

func TestAuthMiddleware(t *testing.T) {
	handler, _ := setupTestHandler()

	t.Run("Valid API key", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("X-API-Key", "test-api-key")

		rr := httptest.NewRecorder()

		nextCalled := false
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			nextCalled = true
			w.WriteHeader(http.StatusOK)
		})

		handler.AuthMiddleware(next).ServeHTTP(rr, req)

		if !nextCalled {
			t.Error("Next handler should have been called")
		}
		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}
	})

	t.Run("Missing API key", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		rr := httptest.NewRecorder()

		nextCalled := false
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			nextCalled = true
		})

		handler.AuthMiddleware(next).ServeHTTP(rr, req)

		if nextCalled {
			t.Error("Next handler should not have been called")
		}
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", rr.Code)
		}
	})

	t.Run("Invalid API key", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("X-API-Key", "wrong-key")
		rr := httptest.NewRecorder()

		nextCalled := false
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			nextCalled = true
		})

		handler.AuthMiddleware(next).ServeHTTP(rr, req)

		if nextCalled {
			t.Error("Next handler should not have been called")
		}
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", rr.Code)
		}
	})

	t.Run("GET /sandbox/{id}/alive without API key is allowed (health check)", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/sandbox/26d5a0654b393db4eb271a5fd797d99b/alive", nil)
		rr := httptest.NewRecorder()

		nextCalled := false
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			nextCalled = true
			w.WriteHeader(http.StatusOK)
		})

		handler.AuthMiddleware(next).ServeHTTP(rr, req)

		if !nextCalled {
			t.Error("Next handler should have been called for /sandbox/.../alive")
		}
		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}
	})

	t.Run("POST /sandbox/{id}/api/bash/start_bash_command without X-API-Key is allowed (session key validated by sandbox)", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/sandbox/a00197a38929f1d32942fa6761ed406a/api/bash/start_bash_command", nil)
		req.Header.Set("X-Session-API-Key", "session-key-from-start-response")
		rr := httptest.NewRecorder()

		nextCalled := false
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			nextCalled = true
			w.WriteHeader(http.StatusOK)
		})

		handler.AuthMiddleware(next).ServeHTTP(rr, req)

		if !nextCalled {
			t.Error("Next handler should have been called for /sandbox/.../api/bash/...")
		}
		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}
	})

	t.Run("GET /sandbox/{id}/api/conversations without X-API-Key is allowed", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/sandbox/a00197a38929f1d32942fa6761ed406a/api/conversations", nil)
		rr := httptest.NewRecorder()

		nextCalled := false
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			nextCalled = true
			w.WriteHeader(http.StatusOK)
		})

		handler.AuthMiddleware(next).ServeHTTP(rr, req)

		if !nextCalled {
			t.Error("Next handler should have been called for /sandbox/.../api/conversations")
		}
		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}
	})
}

func TestGetRegistryPrefix(t *testing.T) {
	handler, _ := setupTestHandler()
	handler.config.RegistryPrefix = "test-registry/prefix"

	req := httptest.NewRequest("GET", "/registry_prefix", nil)
	rr := httptest.NewRecorder()

	handler.GetRegistryPrefix(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}

	var resp types.RegistryPrefixResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp.RegistryPrefix != "test-registry/prefix" {
		t.Errorf("Expected 'test-registry/prefix', got '%s'", resp.RegistryPrefix)
	}
}

func TestCheckImageExists(t *testing.T) {
	handler, _ := setupTestHandler()

	t.Run("With image parameter", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/image_exists?image=test-image", nil)
		rr := httptest.NewRecorder()

		handler.CheckImageExists(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}

		var resp types.ImageExistsResponse
		if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if !resp.Exists {
			t.Error("Expected image to exist")
		}
	})

	t.Run("Without image parameter", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/image_exists", nil)
		rr := httptest.NewRecorder()

		handler.CheckImageExists(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rr.Code)
		}
	})
}

func TestListRuntimes(t *testing.T) {
	_, stateMgr := setupTestHandler()

	// Add some test runtimes
	stateMgr.AddRuntime(&state.RuntimeInfo{
		RuntimeID: "runtime-1",
		SessionID: "session-1",
		Status:    types.StatusRunning,
		PodStatus: types.PodStatusReady,
		PodName:   "pod-1",
	})
	stateMgr.AddRuntime(&state.RuntimeInfo{
		RuntimeID: "runtime-2",
		SessionID: "session-2",
		Status:    types.StatusPaused,
		PodStatus: types.PodStatusNotFound,
		PodName:   "pod-2",
	})

	// Note: This test would fail with nil k8s client because ListRuntimes tries to get pod status
	// In a real scenario, we would use a mock k8s client interface
	// For now, we test that we can retrieve the runtimes from state
	runtimes := stateMgr.ListRuntimes()
	if len(runtimes) != 2 {
		t.Errorf("Expected 2 runtimes in state, got %d", len(runtimes))
	}
}

func TestGetRuntime(t *testing.T) {
	handler, stateMgr := setupTestHandler()

	stateMgr.AddRuntime(&state.RuntimeInfo{
		RuntimeID: "runtime-123",
		SessionID: "session-456",
		Status:    types.StatusRunning,
		PodName:   "pod-123",
	})

	t.Run("Get non-existent runtime", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/runtime/non-existent", nil)
		req = mux.SetURLVars(req, map[string]string{"runtime_id": "non-existent"})
		rr := httptest.NewRecorder()

		handler.GetRuntime(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", rr.Code)
		}
	})

	// Note: Testing with existing runtime would require k8s client mock
	// which is skipped for now
}

func TestGetSession(t *testing.T) {
	handler, stateMgr := setupTestHandler()

	stateMgr.AddRuntime(&state.RuntimeInfo{
		RuntimeID: "runtime-123",
		SessionID: "session-456",
		Status:    types.StatusRunning,
		PodName:   "pod-123",
	})

	t.Run("Get non-existent session", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/sessions/non-existent", nil)
		req = mux.SetURLVars(req, map[string]string{"session_id": "non-existent"})
		rr := httptest.NewRecorder()

		handler.GetSession(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", rr.Code)
		}
	})

	// Note: Testing with existing session would require k8s client mock
}

func TestGetSessionsBatch(t *testing.T) {
	handler, stateMgr := setupTestHandler()

	stateMgr.AddRuntime(&state.RuntimeInfo{RuntimeID: "r1", SessionID: "s1", PodName: "p1"})
	stateMgr.AddRuntime(&state.RuntimeInfo{RuntimeID: "r2", SessionID: "s2", PodName: "p2"})
	stateMgr.AddRuntime(&state.RuntimeInfo{RuntimeID: "r3", SessionID: "s3", PodName: "p3"})

	t.Run("Batch query without IDs", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/sessions/batch", nil)
		rr := httptest.NewRecorder()

		handler.GetSessionsBatch(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rr.Code)
		}
	})

	// Note: Testing with valid IDs would require k8s client mock
}

func TestStopRuntime(t *testing.T) {
	handler, stateMgr := setupTestHandler()

	stateMgr.AddRuntime(&state.RuntimeInfo{
		RuntimeID: "runtime-123",
		SessionID: "session-456",
		PodName:   "pod-123",
	})

	t.Run("Stop non-existent runtime", func(t *testing.T) {
		reqBody := types.StopRequest{RuntimeID: "non-existent"}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest("POST", "/stop", bytes.NewReader(body))
		rr := httptest.NewRecorder()

		handler.StopRuntime(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", rr.Code)
		}
	})

	t.Run("Invalid request body", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/stop", bytes.NewReader([]byte("invalid json")))
		rr := httptest.NewRecorder()

		handler.StopRuntime(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rr.Code)
		}
	})
}

func TestGenerateID(t *testing.T) {
	id1 := generateID()
	id2 := generateID()

	if len(id1) != 32 { // 16 bytes hex encoded = 32 chars
		t.Errorf("Expected ID length 32, got %d", len(id1))
	}

	if id1 == id2 {
		t.Error("Generated IDs should be unique")
	}
}

func TestGenerateSessionAPIKey(t *testing.T) {
	key1 := generateSessionAPIKey()
	key2 := generateSessionAPIKey()

	if len(key1) != 64 { // 32 bytes hex encoded = 64 chars
		t.Errorf("Expected key length 64, got %d", len(key1))
	}

	if key1 == key2 {
		t.Error("Generated keys should be unique")
	}
}

func TestBuildRuntimeResponse_WithoutProxy(t *testing.T) {
	handler, stateMgr := setupTestHandler()
	handler.config.ProxyBaseURL = ""

	stateMgr.AddRuntime(&state.RuntimeInfo{
		RuntimeID:     "rt-123",
		SessionID:     "sess-456",
		URL:           "https://sess-456.test.example.com",
		SessionAPIKey: "skey",
		Status:        types.StatusRunning,
		PodStatus:     types.PodStatusReady,
		ServiceName:   "runtime-rt-123",
	})

	info, _ := stateMgr.GetRuntimeByID("rt-123")
	resp := handler.buildRuntimeResponse(info)

	if resp.URL != "https://sess-456.test.example.com" {
		t.Errorf("Expected URL from RuntimeInfo, got %q", resp.URL)
	}
	if resp.VSCodeURL != "" {
		t.Errorf("Expected empty VSCodeURL when not in proxy mode, got %q", resp.VSCodeURL)
	}
}

func TestBuildRuntimeResponse_WithProxy(t *testing.T) {
	handler, stateMgr := setupTestHandler()
	handler.config.ProxyBaseURL = "https://runtime-api.example.com"

	stateMgr.AddRuntime(&state.RuntimeInfo{
		RuntimeID:     "rt-abc",
		SessionID:     "sess-xyz",
		URL:           "https://sess-xyz.test.example.com",
		SessionAPIKey: "skey",
		Status:        types.StatusRunning,
		PodStatus:     types.PodStatusReady,
		ServiceName:   "runtime-rt-abc",
	})

	info, _ := stateMgr.GetRuntimeByID("rt-abc")
	resp := handler.buildRuntimeResponse(info)

	expectedURL := "https://runtime-api.example.com/sandbox/rt-abc"
	if resp.URL != expectedURL {
		t.Errorf("Expected URL %q, got %q", expectedURL, resp.URL)
	}
	expectedVSCode := "https://runtime-api.example.com/sandbox/rt-abc/vscode"
	if resp.VSCodeURL != expectedVSCode {
		t.Errorf("Expected VSCodeURL %q, got %q", expectedVSCode, resp.VSCodeURL)
	}
}

func TestBuildRuntimeResponse_WithProxyBaseURLTrailingSlash(t *testing.T) {
	handler, stateMgr := setupTestHandler()
	handler.config.ProxyBaseURL = "https://runtime-api.example.com/"

	stateMgr.AddRuntime(&state.RuntimeInfo{
		RuntimeID:   "rt-1",
		SessionID:   "s1",
		URL:         "https://s1.test.example.com",
		Status:      types.StatusRunning,
		PodStatus:   types.PodStatusReady,
		ServiceName: "runtime-rt-1",
	})

	info, _ := stateMgr.GetRuntimeByID("rt-1")
	resp := handler.buildRuntimeResponse(info)

	// buildRuntimeResponse uses TrimSuffix on ProxyBaseURL
	if resp.URL != "https://runtime-api.example.com/sandbox/rt-1" {
		t.Errorf("Expected URL without double slash, got %q", resp.URL)
	}
}

func TestProxySandbox_NotFound(t *testing.T) {
	handler, stateMgr := setupTestHandler()
	stateMgr.AddRuntime(&state.RuntimeInfo{
		RuntimeID:   "rt-1",
		SessionID:   "s1",
		ServiceName: "runtime-rt-1",
	})

	t.Run("Path without sandbox prefix", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/other/rt-1/alive", nil)
		req.URL.Path = "/other/rt-1/alive"
		rr := httptest.NewRecorder()
		handler.ProxySandbox(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Errorf("Expected 404, got %d", rr.Code)
		}
	})

	t.Run("Unknown runtime ID", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/sandbox/nonexistent-id/alive", nil)
		req.URL.Path = "/sandbox/nonexistent-id/alive"
		rr := httptest.NewRecorder()
		handler.ProxySandbox(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Errorf("Expected 404 for unknown runtime, got %d", rr.Code)
		}
		var errResp types.ErrorResponse
		_ = json.NewDecoder(rr.Body).Decode(&errResp)
		if errResp.Error != "runtime_not_found" {
			t.Errorf("Expected error runtime_not_found, got %q", errResp.Error)
		}
	})

	t.Run("Empty path after sandbox", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/sandbox/", nil)
		req.URL.Path = "/sandbox/"
		rr := httptest.NewRecorder()
		handler.ProxySandbox(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Errorf("Expected 404 for empty path, got %d", rr.Code)
		}
	})
}

// setupTestHandlerWithK8s creates a handler with a real fake kubernetes client
func setupTestHandlerWithK8s() (*Handler, *state.StateManager, *k8s.Client) {
	cfg := &config.Config{
		ServerPort:          "8080",
		APIKey:              "test-api-key",
		Namespace:           "test-ns",
		BaseDomain:          "test.example.com",
		Worker1Port:         12000,
		Worker2Port:         12001,
		AgentServerPort:     60000,
		VSCodePort:          60001,
		DefaultImage:        "test-image",
		K8sOperationTimeout: 5 * time.Second,
		K8sQueryTimeout:     5 * time.Second,
	}
	stateMgr := state.NewStateManager()
	fakeClientset := fake.NewSimpleClientset()
	k8sClient := k8s.NewClientFromInterface(fakeClientset, cfg)
	handler := &Handler{
		k8sClient: k8sClient,
		stateMgr:  stateMgr,
		config:    cfg,
	}
	return handler, stateMgr, k8sClient
}

func TestStartRuntime_InvalidBody(t *testing.T) {
	handler, _ := setupTestHandler()
	req := httptest.NewRequest("POST", "/start", bytes.NewReader([]byte("invalid json")))
	rr := httptest.NewRecorder()
	handler.StartRuntime(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", rr.Code)
	}
}

func TestStartRuntime_MissingImage(t *testing.T) {
	handler, _ := setupTestHandler()
	reqBody := map[string]string{"session_id": "test-session"}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/start", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	handler.StartRuntime(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", rr.Code)
	}
	var errResp types.ErrorResponse
	_ = json.NewDecoder(rr.Body).Decode(&errResp)
	if errResp.Error != "invalid_request" {
		t.Errorf("Expected error=invalid_request, got %q", errResp.Error)
	}
}

func TestStartRuntime_MissingSessionID(t *testing.T) {
	handler, _ := setupTestHandler()
	reqBody := map[string]string{"image": "test-image"}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/start", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	handler.StartRuntime(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", rr.Code)
	}
}

func TestStartRuntime_ExistingSession(t *testing.T) {
	handler, stateMgr := setupTestHandler()
	// Pre-populate state with an existing runtime
	stateMgr.AddRuntime(&state.RuntimeInfo{
		RuntimeID: "existing-runtime",
		SessionID: "existing-session",
		Status:    types.StatusRunning,
		PodStatus: types.PodStatusReady,
		PodName:   "existing-pod",
		WorkHosts: map[string]int{},
	})

	reqBody := types.StartRequest{
		Image:     "test-image",
		SessionID: "existing-session",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/start", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	handler.StartRuntime(rr, req)
	// Should return 200 with existing runtime without calling k8s
	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}
	var resp types.RuntimeResponse
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if resp.RuntimeID != "existing-runtime" {
		t.Errorf("Expected RuntimeID=existing-runtime, got %q", resp.RuntimeID)
	}
}

func TestPauseRuntime_InvalidBody(t *testing.T) {
	handler, _ := setupTestHandler()
	req := httptest.NewRequest("POST", "/pause", bytes.NewReader([]byte("invalid json")))
	rr := httptest.NewRecorder()
	handler.PauseRuntime(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", rr.Code)
	}
}

func TestPauseRuntime_NotFound(t *testing.T) {
	handler, _ := setupTestHandler()
	reqBody := types.PauseRequest{RuntimeID: "non-existent"}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/pause", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	handler.PauseRuntime(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("Expected 404, got %d", rr.Code)
	}
}

func TestResumeRuntime_InvalidBody(t *testing.T) {
	handler, _ := setupTestHandler()
	req := httptest.NewRequest("POST", "/resume", bytes.NewReader([]byte("invalid json")))
	rr := httptest.NewRecorder()
	handler.ResumeRuntime(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", rr.Code)
	}
}

func TestResumeRuntime_NotFound(t *testing.T) {
	handler, _ := setupTestHandler()
	reqBody := types.ResumeRequest{RuntimeID: "non-existent"}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/resume", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	handler.ResumeRuntime(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("Expected 404, got %d", rr.Code)
	}
}

func TestResumeRuntime_AlreadyRunning(t *testing.T) {
	handler, stateMgr := setupTestHandler()
	stateMgr.AddRuntime(&state.RuntimeInfo{
		RuntimeID: "rt-running",
		SessionID: "s-running",
		Status:    types.StatusRunning,
		PodName:   "pod-running",
		WorkHosts: map[string]int{},
	})

	reqBody := types.ResumeRequest{RuntimeID: "rt-running"}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/resume", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	handler.ResumeRuntime(rr, req)
	// Already running - should return 200 without k8s call
	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200 for already-running runtime, got %d", rr.Code)
	}
}

func TestResumeRuntime_NotPaused(t *testing.T) {
	handler, stateMgr := setupTestHandler()
	stateMgr.AddRuntime(&state.RuntimeInfo{
		RuntimeID: "rt-pending",
		SessionID: "s-pending",
		Status:    types.StatusPending,
		PodName:   "pod-pending",
	})

	reqBody := types.ResumeRequest{RuntimeID: "rt-pending"}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/resume", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	handler.ResumeRuntime(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for non-paused runtime, got %d", rr.Code)
	}
}

func TestListRuntimes_Empty(t *testing.T) {
	handler, _, _ := setupTestHandlerWithK8s()
	req := httptest.NewRequest("GET", "/list", nil)
	rr := httptest.NewRecorder()
	handler.ListRuntimes(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}
	var resp types.ListResponse
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if len(resp.Runtimes) != 0 {
		t.Errorf("Expected 0 runtimes, got %d", len(resp.Runtimes))
	}
}

func TestListRuntimes_WithRuntimes(t *testing.T) {
	handler, stateMgr, _ := setupTestHandlerWithK8s()
	stateMgr.AddRuntime(&state.RuntimeInfo{
		RuntimeID: "rt-list-1",
		SessionID: "s-list-1",
		Status:    types.StatusRunning,
		PodName:   "pod-list-1",
		WorkHosts: map[string]int{},
	})
	req := httptest.NewRequest("GET", "/list", nil)
	rr := httptest.NewRecorder()
	handler.ListRuntimes(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}
	var resp types.ListResponse
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if len(resp.Runtimes) != 1 {
		t.Errorf("Expected 1 runtime, got %d", len(resp.Runtimes))
	}
}

func TestGetRuntime_ExistingRuntime(t *testing.T) {
	handler, stateMgr, _ := setupTestHandlerWithK8s()
	stateMgr.AddRuntime(&state.RuntimeInfo{
		RuntimeID: "rt-get",
		SessionID: "s-get",
		Status:    types.StatusRunning,
		PodName:   "pod-get",
		WorkHosts: map[string]int{},
	})

	req := httptest.NewRequest("GET", "/runtime/rt-get", nil)
	req = mux.SetURLVars(req, map[string]string{"runtime_id": "rt-get"})
	rr := httptest.NewRecorder()
	handler.GetRuntime(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}
	var resp types.RuntimeResponse
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if resp.RuntimeID != "rt-get" {
		t.Errorf("Expected RuntimeID=rt-get, got %q", resp.RuntimeID)
	}
}

func TestGetSession_ExistingSession(t *testing.T) {
	handler, stateMgr, _ := setupTestHandlerWithK8s()
	stateMgr.AddRuntime(&state.RuntimeInfo{
		RuntimeID: "rt-sess",
		SessionID: "s-sess",
		Status:    types.StatusRunning,
		PodName:   "pod-sess",
		WorkHosts: map[string]int{},
	})

	req := httptest.NewRequest("GET", "/sessions/s-sess", nil)
	req = mux.SetURLVars(req, map[string]string{"session_id": "s-sess"})
	rr := httptest.NewRecorder()
	handler.GetSession(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}
	var resp types.RuntimeResponse
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if resp.SessionID != "s-sess" {
		t.Errorf("Expected SessionID=s-sess, got %q", resp.SessionID)
	}
}

func TestGetSessionsBatch_WithIDs(t *testing.T) {
	handler, stateMgr := setupTestHandler()
	stateMgr.AddRuntime(&state.RuntimeInfo{
		RuntimeID: "r-batch-1",
		SessionID: "s-batch-1",
		PodName:   "p-batch-1",
		WorkHosts: map[string]int{},
	})
	stateMgr.AddRuntime(&state.RuntimeInfo{
		RuntimeID: "r-batch-2",
		SessionID: "s-batch-2",
		PodName:   "p-batch-2",
		WorkHosts: map[string]int{},
	})

	req := httptest.NewRequest("GET", "/sessions/batch?ids=s-batch-1,s-batch-2", nil)
	rr := httptest.NewRecorder()
	handler.GetSessionsBatch(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}
	var resp []types.RuntimeResponse
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if len(resp) != 2 {
		t.Errorf("Expected 2 responses, got %d", len(resp))
	}
}

func TestGetSessionsBatch_MultipleIDParams(t *testing.T) {
	handler, stateMgr := setupTestHandler()
	stateMgr.AddRuntime(&state.RuntimeInfo{
		RuntimeID: "r-multi-1",
		SessionID: "s-multi-1",
		PodName:   "p-multi-1",
		WorkHosts: map[string]int{},
	})

	req := httptest.NewRequest("GET", "/sessions/batch?ids=s-multi-1&ids=s-missing", nil)
	rr := httptest.NewRecorder()
	handler.GetSessionsBatch(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}
	var resp []types.RuntimeResponse
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	// Only s-multi-1 is in state; s-missing is not found (nil k8s, no discovery)
	if len(resp) != 1 {
		t.Errorf("Expected 1 response, got %d", len(resp))
	}
}

func TestLoggingMiddleware(t *testing.T) {
	handler, _ := setupTestHandler()

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()
	handler.LoggingMiddleware(next).ServeHTTP(rr, req)

	if !nextCalled {
		t.Error("Next handler should have been called")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}
}

func TestRewriteCookiePath(t *testing.T) {
	tests := []struct {
		name        string
		cookie      string
		proxyPrefix string
		wantContain string
	}{
		{
			name:        "cookie with path=/",
			cookie:      "session=abc123; Path=/; HttpOnly",
			proxyPrefix: "/sandbox/rt-1",
			wantContain: "Path=/sandbox/rt-1",
		},
		{
			name:        "cookie with empty path",
			cookie:      "session=abc123; Path=; HttpOnly",
			proxyPrefix: "/sandbox/rt-1",
			wantContain: "Path=/sandbox/rt-1",
		},
		{
			name:        "cookie without path",
			cookie:      "session=abc123; HttpOnly",
			proxyPrefix: "/sandbox/rt-1",
			wantContain: "Path=/sandbox/rt-1",
		},
		{
			name:        "cookie with non-root path (unchanged)",
			cookie:      "session=abc123; Path=/myapp; HttpOnly",
			proxyPrefix: "/sandbox/rt-1",
			wantContain: "Path=/myapp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := rewriteCookiePath(tt.cookie, tt.proxyPrefix)
			if !strings.Contains(result, tt.wantContain) {
				t.Errorf("rewriteCookiePath(%q, %q) = %q, expected to contain %q",
					tt.cookie, tt.proxyPrefix, result, tt.wantContain)
			}
		})
	}
}

func TestCreateProxyResponseRewriter(t *testing.T) {
	handler, _ := setupTestHandler()
	handler.config.VSCodePort = 60001

	t.Run("rewrite Location header for agent port", func(t *testing.T) {
		rewriter := handler.createProxyResponseRewriter("rt-proxy", handler.config.AgentServerPort)
		resp := &http.Response{
			Header: http.Header{
				"Location": []string{"/some/path"},
			},
		}
		if err := rewriter(resp); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		loc := resp.Header.Get("Location")
		if !strings.Contains(loc, "/sandbox/rt-proxy") {
			t.Errorf("expected Location to contain /sandbox/rt-proxy, got %q", loc)
		}
	})

	t.Run("rewrite Location header for vscode port", func(t *testing.T) {
		rewriter := handler.createProxyResponseRewriter("rt-vscode", handler.config.VSCodePort)
		resp := &http.Response{
			Header: http.Header{
				"Location": []string{"/vscode/path"},
			},
		}
		if err := rewriter(resp); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		loc := resp.Header.Get("Location")
		if !strings.Contains(loc, "/sandbox/rt-vscode/vscode") {
			t.Errorf("expected Location to contain /sandbox/rt-vscode/vscode, got %q", loc)
		}
	})

	t.Run("rewrite Set-Cookie header", func(t *testing.T) {
		rewriter := handler.createProxyResponseRewriter("rt-cookie", handler.config.AgentServerPort)
		resp := &http.Response{
			Header: http.Header{
				"Set-Cookie": []string{"session=abc; Path=/"},
			},
		}
		if err := rewriter(resp); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		cookies := resp.Header.Values("Set-Cookie")
		if len(cookies) == 0 {
			t.Fatal("expected Set-Cookie header to be present")
		}
		if !strings.Contains(cookies[0], "/sandbox/rt-cookie") {
			t.Errorf("expected cookie path to contain /sandbox/rt-cookie, got %q", cookies[0])
		}
	})

	t.Run("absolute Location URL not rewritten", func(t *testing.T) {
		rewriter := handler.createProxyResponseRewriter("rt-abs", handler.config.AgentServerPort)
		resp := &http.Response{
			Header: http.Header{
				"Location": []string{"https://other.example.com/path"},
			},
		}
		if err := rewriter(resp); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		loc := resp.Header.Get("Location")
		// Absolute URL with host should not be prefixed
		if loc != "https://other.example.com/path" {
			t.Errorf("expected absolute URL to be unchanged, got %q", loc)
		}
	})

	t.Run("no Location header", func(t *testing.T) {
		rewriter := handler.createProxyResponseRewriter("rt-noheader", handler.config.AgentServerPort)
		resp := &http.Response{Header: http.Header{}}
		if err := rewriter(resp); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestNewHandler(t *testing.T) {
	cfg := &config.Config{
		ServerPort: "8080",
		APIKey:     "test-key",
	}
	stateMgr := state.NewStateManager()
	handler := NewHandler(nil, stateMgr, cfg)
	if handler == nil {
		t.Fatal("expected non-nil handler")
	}
	if handler.config != cfg {
		t.Error("expected config to be set")
	}
	if handler.stateMgr != stateMgr {
		t.Error("expected stateMgr to be set")
	}
}

func TestLoggingMiddleware_DebugMode(t *testing.T) {
	handler, _ := setupTestHandler()

	// Enable debug mode
	logger.Init("debug")
	defer logger.Init("info")

	body := bytes.NewReader([]byte(`{"key":"value"}`))
	req := httptest.NewRequest("POST", "/test", body)

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	rr := httptest.NewRecorder()
	handler.LoggingMiddleware(next).ServeHTTP(rr, req)

	if !nextCalled {
		t.Error("Next handler should have been called")
	}
}

func TestProxySandbox_VscodeRouting(t *testing.T) {
	handler, stateMgr := setupTestHandler()
	stateMgr.AddRuntime(&state.RuntimeInfo{
		RuntimeID:   "rt-vscode",
		SessionID:   "s-vscode",
		ServiceName: "runtime-rt-vscode",
		WorkHosts:   map[string]int{},
	})

	t.Run("vscode path attempt", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/sandbox/rt-vscode/vscode", nil)
		req.URL.Path = "/sandbox/rt-vscode/vscode"
		rr := httptest.NewRecorder()
		handler.ProxySandbox(rr, req)
		if rr.Code == http.StatusNotFound {
			t.Error("Expected non-404 response (runtime found), but got 404")
		}
	})

	t.Run("vscode subpath attempt", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/sandbox/rt-vscode/vscode/some/path", nil)
		req.URL.Path = "/sandbox/rt-vscode/vscode/some/path"
		rr := httptest.NewRecorder()
		handler.ProxySandbox(rr, req)
		if rr.Code == http.StatusNotFound {
			t.Error("Expected non-404 response, but got 404")
		}
	})
}

func TestStartRuntime_WithK8s(t *testing.T) {
	handler, _, _ := setupTestHandlerWithK8s()

	reqBody := types.StartRequest{
		Image:     "test-image",
		SessionID: "new-session",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/start", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	handler.StartRuntime(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200 with fake k8s client, got %d", rr.Code)
	}
	var resp types.RuntimeResponse
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Status != types.StatusRunning {
		t.Errorf("Expected status running, got %s", resp.Status)
	}
}

func TestStopRuntime_WithK8s(t *testing.T) {
	handler, stateMgr, _ := setupTestHandlerWithK8s()
	stateMgr.AddRuntime(&state.RuntimeInfo{
		RuntimeID:   "rt-stop",
		SessionID:   "s-stop",
		PodName:     "pod-stop",
		ServiceName: "svc-stop",
		IngressName: "ing-stop",
		WorkHosts:   map[string]int{},
	})

	reqBody := types.StopRequest{RuntimeID: "rt-stop"}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/stop", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	handler.StopRuntime(rr, req)
	// DeleteSandbox ignores not-found errors, so this should succeed
	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}
}

func TestPauseRuntime_WithK8s(t *testing.T) {
	cfg := &config.Config{
		ServerPort:          "8080",
		APIKey:              "test-api-key",
		Namespace:           "test-ns",
		BaseDomain:          "test.example.com",
		Worker1Port:         12000,
		Worker2Port:         12001,
		AgentServerPort:     60000,
		VSCodePort:          60001,
		DefaultImage:        "test-image",
		K8sOperationTimeout: 5 * time.Second,
		K8sQueryTimeout:     5 * time.Second,
	}
	stateMgr := state.NewStateManager()
	// Pre-create pod in fake client
	corev1Pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod-pause", Namespace: "test-ns"},
	}
	fakeClientset := fake.NewSimpleClientset(corev1Pod)
	k8sClient := k8s.NewClientFromInterface(fakeClientset, cfg)
	handler := &Handler{
		k8sClient: k8sClient,
		stateMgr:  stateMgr,
		config:    cfg,
	}

	stateMgr.AddRuntime(&state.RuntimeInfo{
		RuntimeID:   "rt-pause",
		SessionID:   "s-pause",
		PodName:     "pod-pause",
		ServiceName: "svc-pause",
		WorkHosts:   map[string]int{},
	})

	reqBody := types.PauseRequest{RuntimeID: "rt-pause"}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/pause", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	handler.PauseRuntime(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}
}

func TestResumeRuntime_WithK8s(t *testing.T) {
	handler, stateMgr, _ := setupTestHandlerWithK8s()
	stateMgr.AddRuntime(&state.RuntimeInfo{
		RuntimeID:   "rt-resume",
		SessionID:   "s-resume",
		Status:      types.StatusPaused,
		PodName:     "pod-resume",
		ServiceName: "svc-resume",
		WorkHosts:   map[string]int{},
	})

	reqBody := types.ResumeRequest{RuntimeID: "rt-resume"}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/resume", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	handler.ResumeRuntime(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}
}
