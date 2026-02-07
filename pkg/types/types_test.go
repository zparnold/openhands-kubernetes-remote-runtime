package types

import (
	"encoding/json"
	"testing"
)

func TestRuntimeStatus(t *testing.T) {
	tests := []struct {
		name     string
		status   RuntimeStatus
		expected string
	}{
		{"Running status", StatusRunning, "running"},
		{"Paused status", StatusPaused, "paused"},
		{"Stopped status", StatusStopped, "stopped"},
		{"Pending status", StatusPending, "pending"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.status) != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, string(tt.status))
			}
		})
	}
}

func TestPodStatus(t *testing.T) {
	tests := []struct {
		name     string
		status   PodStatus
		expected string
	}{
		{"Pending status", PodStatusPending, "pending"},
		{"Running status", PodStatusRunning, "running"},
		{"Ready status", PodStatusReady, "ready"},
		{"Failed status", PodStatusFailed, "failed"},
		{"CrashLoopBackOff status", PodStatusCrashLoopBackOff, "crashloopbackoff"},
		{"NotFound status", PodStatusNotFound, "not found"},
		{"Unknown status", PodStatusUnknown, "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.status) != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, string(tt.status))
			}
		})
	}
}

func TestStartRequest(t *testing.T) {
	req := StartRequest{
		Image:          "test-image",
		Command:        FlexibleCommand{"test-command"},
		WorkingDir:     "/test",
		Environment:    map[string]string{"KEY": "VALUE"},
		SessionID:      "test-session",
		ResourceFactor: 1.5,
		RuntimeClass:   "test-class",
	}

	if req.Image != "test-image" {
		t.Errorf("Expected image 'test-image', got '%s'", req.Image)
	}
	if req.SessionID != "test-session" {
		t.Errorf("Expected session ID 'test-session', got '%s'", req.SessionID)
	}
	if req.ResourceFactor != 1.5 {
		t.Errorf("Expected resource factor 1.5, got %f", req.ResourceFactor)
	}
}

func TestFlexibleCommand_UnmarshalJSON(t *testing.T) {
	t.Run("unmarshal from string", func(t *testing.T) {
		var c FlexibleCommand
		err := json.Unmarshal([]byte(`"echo hello"`), &c)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(c) != 1 || c[0] != "echo hello" {
			t.Errorf("expected [\"echo hello\"], got %v", c)
		}
	})
	t.Run("unmarshal from array", func(t *testing.T) {
		var c FlexibleCommand
		err := json.Unmarshal([]byte(`["/usr/local/bin/openhands-agent-server","--port","60000"]`), &c)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(c) != 3 || c[0] != "/usr/local/bin/openhands-agent-server" || c[1] != "--port" || c[2] != "60000" {
			t.Errorf("expected exec form slice, got %v", c)
		}
	})
	t.Run("unmarshal empty string", func(t *testing.T) {
		var c FlexibleCommand
		err := json.Unmarshal([]byte(`""`), &c)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if c != nil {
			t.Errorf("expected nil, got %v", c)
		}
	})
}

func TestRuntimeResponse(t *testing.T) {
	resp := RuntimeResponse{
		RuntimeID:      "runtime-123",
		SessionID:      "session-456",
		URL:            "https://test.example.com",
		SessionAPIKey:  "key-789",
		Status:         StatusRunning,
		PodStatus:      PodStatusReady,
		WorkHosts:      map[string]int{"host1": 8080, "host2": 8081},
		RestartCount:   2,
		RestartReasons: []string{"OOMKilled", "Error"},
	}

	if resp.RuntimeID != "runtime-123" {
		t.Errorf("Expected runtime ID 'runtime-123', got '%s'", resp.RuntimeID)
	}
	if resp.Status != StatusRunning {
		t.Errorf("Expected status 'running', got '%s'", resp.Status)
	}
	if resp.RestartCount != 2 {
		t.Errorf("Expected restart count 2, got %d", resp.RestartCount)
	}
	if len(resp.WorkHosts) != 2 {
		t.Errorf("Expected 2 work hosts, got %d", len(resp.WorkHosts))
	}
}

func TestErrorResponse(t *testing.T) {
	err := ErrorResponse{
		Error:   "test_error",
		Message: "This is a test error message",
	}

	if err.Error != "test_error" {
		t.Errorf("Expected error 'test_error', got '%s'", err.Error)
	}
	if err.Message != "This is a test error message" {
		t.Errorf("Expected message 'This is a test error message', got '%s'", err.Message)
	}
}
