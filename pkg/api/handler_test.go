package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/zparnold/openhands-kubernetes-remote-runtime/pkg/config"
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
		RuntimeID:     "rt-1",
		SessionID:     "s1",
		URL:           "https://s1.test.example.com",
		Status:        types.StatusRunning,
		PodStatus:     types.PodStatusReady,
		ServiceName:   "runtime-rt-1",
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
