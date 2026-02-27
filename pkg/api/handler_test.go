package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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
		k8sClient:    nil,
		stateMgr:     stateMgr,
		config:       cfg,
		tracedClient: http.DefaultClient,
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

func TestBatchGetConversations_InvalidBody(t *testing.T) {
	handler, _ := setupTestHandler()

	req := httptest.NewRequest("POST", "/sessions/batch-conversations", bytes.NewReader([]byte("not json")))
	rr := httptest.NewRecorder()

	handler.BatchGetConversations(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", rr.Code)
	}

	var errResp types.ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&errResp); err != nil {
		t.Fatalf("Failed to decode error response: %v", err)
	}
	if errResp.Error != "invalid_request" {
		t.Errorf("Expected error 'invalid_request', got %q", errResp.Error)
	}
}

func TestBatchGetConversations_EmptySandboxes(t *testing.T) {
	handler, _ := setupTestHandler()

	reqBody := types.BatchConversationsRequest{
		Sandboxes: map[string]types.BatchConversationSandbox{},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/sessions/batch-conversations", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	handler.BatchGetConversations(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}

	var resp map[string]json.RawMessage
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if len(resp) != 0 {
		t.Errorf("Expected empty response, got %d entries", len(resp))
	}
}

func TestBatchGetConversations_RuntimeNotFound(t *testing.T) {
	handler, _ := setupTestHandler()

	reqBody := types.BatchConversationsRequest{
		Sandboxes: map[string]types.BatchConversationSandbox{
			"nonexistent-runtime": {
				SessionID:       "nonexistent-session",
				ConversationIDs: []string{"conv1"},
			},
		},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/sessions/batch-conversations", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	handler.BatchGetConversations(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}

	var resp map[string]json.RawMessage
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Should return empty array for unfound runtime
	data, ok := resp["nonexistent-runtime"]
	if !ok {
		t.Fatal("Expected key 'nonexistent-runtime' in response")
	}
	if string(data) != "[]" {
		t.Errorf("Expected empty array for unfound runtime, got %s", string(data))
	}
}

func TestBatchGetConversations_WithMockAgentServer(t *testing.T) {
	// Start a mock agent-server that returns conversation data
	mockConversations := `[{"id":"conv1","status":"running"},{"id":"conv2","status":"idle"}]`
	var capturedAPIKey string
	var capturedIDs string
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAPIKey = r.Header.Get("X-Session-API-Key")
		capturedIDs = r.URL.Query().Get("ids")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, mockConversations)
	}))
	defer mockServer.Close()

	// In-cluster DNS won't work in tests, so we use a custom HTTP transport that
	// redirects the in-cluster URL to our mock server.
	handler, stateMgr := setupTestHandler()

	originalTransport := http.DefaultTransport
	http.DefaultTransport = &mockTransport{
		mockServerURL: mockServer.URL,
		inner:         originalTransport,
	}
	defer func() { http.DefaultTransport = originalTransport }()

	// Add a runtime with known service name
	stateMgr.AddRuntime(&state.RuntimeInfo{
		RuntimeID:     "rt-100",
		SessionID:     "sess-100",
		ServiceName:   "runtime-rt-100",
		SessionAPIKey: "test-session-key-abc",
		Status:        types.StatusRunning,
		PodStatus:     types.PodStatusReady,
		PodName:       "pod-100",
	})

	reqBody := types.BatchConversationsRequest{
		Sandboxes: map[string]types.BatchConversationSandbox{
			"rt-100": {
				SessionID:       "sess-100",
				ConversationIDs: []string{"conv1", "conv2"},
			},
		},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/sessions/batch-conversations", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	handler.BatchGetConversations(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d; body: %s", rr.Code, rr.Body.String())
	}

	// Verify the session API key was forwarded
	if capturedAPIKey != "test-session-key-abc" {
		t.Errorf("Expected X-Session-API-Key 'test-session-key-abc', got %q", capturedAPIKey)
	}

	// Verify the conversation IDs were passed
	if capturedIDs != "conv1,conv2" {
		t.Errorf("Expected ids query param 'conv1,conv2', got %q", capturedIDs)
	}

	// Verify the response contains the pass-through JSON
	var resp map[string]json.RawMessage
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	data, ok := resp["rt-100"]
	if !ok {
		t.Fatal("Expected key 'rt-100' in response")
	}

	// Verify the raw JSON was passed through
	if string(data) != mockConversations {
		t.Errorf("Expected pass-through JSON %q, got %q", mockConversations, string(data))
	}
}

func TestBatchGetConversations_MultipleSandboxes(t *testing.T) {
	// Create two mock servers to simulate different agent-server pods
	mockServer1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"id":"conv1","status":"running"}]`)
	}))
	defer mockServer1.Close()

	mockServer2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"id":"conv3","status":"idle"}]`)
	}))
	defer mockServer2.Close()

	handler, stateMgr := setupTestHandler()

	// Redirect all in-cluster calls to mockServer1 for simplicity
	// (both runtimes will hit the same mock, but we test concurrency)
	originalTransport := http.DefaultTransport
	http.DefaultTransport = &mockTransport{
		mockServerURL: mockServer1.URL,
		inner:         originalTransport,
	}
	defer func() { http.DefaultTransport = originalTransport }()

	stateMgr.AddRuntime(&state.RuntimeInfo{
		RuntimeID:     "rt-a",
		SessionID:     "sess-a",
		ServiceName:   "runtime-rt-a",
		SessionAPIKey: "key-a",
		Status:        types.StatusRunning,
		PodName:       "pod-a",
	})
	stateMgr.AddRuntime(&state.RuntimeInfo{
		RuntimeID:     "rt-b",
		SessionID:     "sess-b",
		ServiceName:   "runtime-rt-b",
		SessionAPIKey: "key-b",
		Status:        types.StatusRunning,
		PodName:       "pod-b",
	})

	reqBody := types.BatchConversationsRequest{
		Sandboxes: map[string]types.BatchConversationSandbox{
			"rt-a": {
				SessionID:       "sess-a",
				ConversationIDs: []string{"conv1"},
			},
			"rt-b": {
				SessionID:       "sess-b",
				ConversationIDs: []string{"conv3"},
			},
		},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/sessions/batch-conversations", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	handler.BatchGetConversations(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d; body: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]json.RawMessage
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(resp) != 2 {
		t.Errorf("Expected 2 entries, got %d", len(resp))
	}

	if _, ok := resp["rt-a"]; !ok {
		t.Error("Expected key 'rt-a' in response")
	}
	if _, ok := resp["rt-b"]; !ok {
		t.Error("Expected key 'rt-b' in response")
	}
}

func TestBatchGetConversations_LookupBySessionID(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"id":"conv1"}]`)
	}))
	defer mockServer.Close()

	handler, stateMgr := setupTestHandler()

	originalTransport := http.DefaultTransport
	http.DefaultTransport = &mockTransport{
		mockServerURL: mockServer.URL,
		inner:         originalTransport,
	}
	defer func() { http.DefaultTransport = originalTransport }()

	// Runtime with a different runtime ID than what the request uses
	stateMgr.AddRuntime(&state.RuntimeInfo{
		RuntimeID:     "actual-rt-id",
		SessionID:     "sess-xyz",
		ServiceName:   "runtime-actual-rt-id",
		SessionAPIKey: "key-xyz",
		Status:        types.StatusRunning,
		PodName:       "pod-xyz",
	})

	// Request uses a runtime ID that doesn't exist, but provides the correct session ID
	reqBody := types.BatchConversationsRequest{
		Sandboxes: map[string]types.BatchConversationSandbox{
			"unknown-rt-id": {
				SessionID:       "sess-xyz",
				ConversationIDs: []string{"conv1"},
			},
		},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/sessions/batch-conversations", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	handler.BatchGetConversations(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}

	var resp map[string]json.RawMessage
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Should return data under the requested key (unknown-rt-id), not the actual runtime ID
	data, ok := resp["unknown-rt-id"]
	if !ok {
		t.Fatal("Expected key 'unknown-rt-id' in response")
	}
	if string(data) != `[{"id":"conv1"}]` {
		t.Errorf("Expected pass-through JSON, got %s", string(data))
	}
}

func TestBatchGetConversations_AgentServerError(t *testing.T) {
	// Mock server that returns 500
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error":"internal"}`)
	}))
	defer mockServer.Close()

	handler, stateMgr := setupTestHandler()

	originalTransport := http.DefaultTransport
	http.DefaultTransport = &mockTransport{
		mockServerURL: mockServer.URL,
		inner:         originalTransport,
	}
	defer func() { http.DefaultTransport = originalTransport }()

	stateMgr.AddRuntime(&state.RuntimeInfo{
		RuntimeID:     "rt-err",
		SessionID:     "sess-err",
		ServiceName:   "runtime-rt-err",
		SessionAPIKey: "key-err",
		Status:        types.StatusRunning,
		PodName:       "pod-err",
	})

	reqBody := types.BatchConversationsRequest{
		Sandboxes: map[string]types.BatchConversationSandbox{
			"rt-err": {
				SessionID:       "sess-err",
				ConversationIDs: []string{"conv1"},
			},
		},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/sessions/batch-conversations", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	handler.BatchGetConversations(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200 (batch doesn't fail on individual errors), got %d", rr.Code)
	}

	var resp map[string]json.RawMessage
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Should return empty array for the failed sandbox
	data, ok := resp["rt-err"]
	if !ok {
		t.Fatal("Expected key 'rt-err' in response")
	}
	if string(data) != "[]" {
		t.Errorf("Expected empty array for failed sandbox, got %s", string(data))
	}
}

func TestBatchGetConversations_MixedResults(t *testing.T) {
	// One sandbox succeeds, one fails (not found), one has agent-server error
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ids := r.URL.Query().Get("ids")
		if strings.Contains(ids, "conv-fail") {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"id":"conv-ok"}]`)
	}))
	defer mockServer.Close()

	handler, stateMgr := setupTestHandler()

	originalTransport := http.DefaultTransport
	http.DefaultTransport = &mockTransport{
		mockServerURL: mockServer.URL,
		inner:         originalTransport,
	}
	defer func() { http.DefaultTransport = originalTransport }()

	stateMgr.AddRuntime(&state.RuntimeInfo{
		RuntimeID:     "rt-ok",
		SessionID:     "sess-ok",
		ServiceName:   "runtime-rt-ok",
		SessionAPIKey: "key-ok",
		Status:        types.StatusRunning,
		PodName:       "pod-ok",
	})
	stateMgr.AddRuntime(&state.RuntimeInfo{
		RuntimeID:     "rt-fail",
		SessionID:     "sess-fail",
		ServiceName:   "runtime-rt-fail",
		SessionAPIKey: "key-fail",
		Status:        types.StatusRunning,
		PodName:       "pod-fail",
	})

	reqBody := types.BatchConversationsRequest{
		Sandboxes: map[string]types.BatchConversationSandbox{
			"rt-ok": {
				SessionID:       "sess-ok",
				ConversationIDs: []string{"conv-ok"},
			},
			"rt-fail": {
				SessionID:       "sess-fail",
				ConversationIDs: []string{"conv-fail"},
			},
			"rt-notfound": {
				SessionID:       "sess-notfound",
				ConversationIDs: []string{"conv-x"},
			},
		},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/sessions/batch-conversations", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	handler.BatchGetConversations(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}

	var resp map[string]json.RawMessage
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(resp) != 3 {
		t.Errorf("Expected 3 entries, got %d", len(resp))
	}

	// rt-ok should have conversation data
	if string(resp["rt-ok"]) != `[{"id":"conv-ok"}]` {
		t.Errorf("Expected conversation data for rt-ok, got %s", string(resp["rt-ok"]))
	}

	// rt-fail should have empty array (agent-server error)
	if string(resp["rt-fail"]) != "[]" {
		t.Errorf("Expected empty array for rt-fail, got %s", string(resp["rt-fail"]))
	}

	// rt-notfound should have empty array (runtime not found)
	if string(resp["rt-notfound"]) != "[]" {
		t.Errorf("Expected empty array for rt-notfound, got %s", string(resp["rt-notfound"]))
	}
}

// mockTransport intercepts HTTP requests to in-cluster service URLs and redirects them
// to a mock test server. This lets us test the full BatchGetConversations flow without
// requiring actual Kubernetes DNS resolution.
type mockTransport struct {
	mockServerURL string
	inner         http.RoundTripper
}

func (t *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Intercept requests to *.svc.cluster.local and redirect to mock server
	if strings.Contains(req.URL.Host, "svc.cluster.local") {
		// Rewrite the URL to point to our mock server
		mockURL := t.mockServerURL + req.URL.Path + "?" + req.URL.RawQuery
		newReq, err := http.NewRequestWithContext(req.Context(), req.Method, mockURL, req.Body)
		if err != nil {
			return nil, err
		}
		newReq.Header = req.Header
		return t.inner.RoundTrip(newReq)
	}
	return t.inner.RoundTrip(req)
}
