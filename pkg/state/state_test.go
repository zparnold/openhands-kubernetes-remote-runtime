package state

import (
	"testing"

	"github.com/zparnold/openhands-kubernetes-remote-runtime/pkg/types"
)

func TestNewStateManager(t *testing.T) {
	sm := NewStateManager()
	if sm == nil {
		t.Fatal("NewStateManager should return non-nil StateManager")
	}
	if sm.runtimeByID == nil {
		t.Error("runtimeByID map should be initialized")
	}
	if sm.runtimeBySession == nil {
		t.Error("runtimeBySession map should be initialized")
	}
}

func TestAddRuntime(t *testing.T) {
	sm := NewStateManager()
	
	info := &RuntimeInfo{
		RuntimeID: "runtime-123",
		SessionID: "session-456",
		URL:       "https://test.example.com",
		Status:    types.StatusRunning,
		PodStatus: types.PodStatusReady,
	}

	sm.AddRuntime(info)

	// Verify it was added
	retrieved, err := sm.GetRuntimeByID("runtime-123")
	if err != nil {
		t.Errorf("Failed to retrieve added runtime: %v", err)
	}
	if retrieved.RuntimeID != "runtime-123" {
		t.Errorf("Expected runtime ID 'runtime-123', got '%s'", retrieved.RuntimeID)
	}
}

func TestGetRuntimeByID(t *testing.T) {
	sm := NewStateManager()
	
	info := &RuntimeInfo{
		RuntimeID: "runtime-123",
		SessionID: "session-456",
	}
	sm.AddRuntime(info)

	t.Run("Get existing runtime", func(t *testing.T) {
		retrieved, err := sm.GetRuntimeByID("runtime-123")
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if retrieved.RuntimeID != "runtime-123" {
			t.Errorf("Expected runtime ID 'runtime-123', got '%s'", retrieved.RuntimeID)
		}
	})

	t.Run("Get non-existent runtime", func(t *testing.T) {
		_, err := sm.GetRuntimeByID("non-existent")
		if err == nil {
			t.Error("Expected error for non-existent runtime")
		}
	})
}

func TestGetRuntimeBySessionID(t *testing.T) {
	sm := NewStateManager()
	
	info := &RuntimeInfo{
		RuntimeID: "runtime-123",
		SessionID: "session-456",
	}
	sm.AddRuntime(info)

	t.Run("Get existing session", func(t *testing.T) {
		retrieved, err := sm.GetRuntimeBySessionID("session-456")
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if retrieved.SessionID != "session-456" {
			t.Errorf("Expected session ID 'session-456', got '%s'", retrieved.SessionID)
		}
	})

	t.Run("Get non-existent session", func(t *testing.T) {
		_, err := sm.GetRuntimeBySessionID("non-existent")
		if err == nil {
			t.Error("Expected error for non-existent session")
		}
	})
}

func TestUpdateRuntime(t *testing.T) {
	sm := NewStateManager()
	
	info := &RuntimeInfo{
		RuntimeID: "runtime-123",
		SessionID: "session-456",
		Status:    types.StatusRunning,
	}
	sm.AddRuntime(info)

	t.Run("Update existing runtime", func(t *testing.T) {
		info.Status = types.StatusPaused
		err := sm.UpdateRuntime(info)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}

		retrieved, _ := sm.GetRuntimeByID("runtime-123")
		if retrieved.Status != types.StatusPaused {
			t.Errorf("Expected status 'paused', got '%s'", retrieved.Status)
		}
	})

	t.Run("Update non-existent runtime", func(t *testing.T) {
		newInfo := &RuntimeInfo{
			RuntimeID: "non-existent",
			SessionID: "session-999",
		}
		err := sm.UpdateRuntime(newInfo)
		if err == nil {
			t.Error("Expected error for non-existent runtime")
		}
	})
}

func TestDeleteRuntime(t *testing.T) {
	sm := NewStateManager()
	
	info := &RuntimeInfo{
		RuntimeID: "runtime-123",
		SessionID: "session-456",
	}
	sm.AddRuntime(info)

	t.Run("Delete existing runtime", func(t *testing.T) {
		err := sm.DeleteRuntime("runtime-123")
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}

		// Verify it's deleted
		_, err = sm.GetRuntimeByID("runtime-123")
		if err == nil {
			t.Error("Expected error after deletion")
		}

		// Verify session mapping is also deleted
		_, err = sm.GetRuntimeBySessionID("session-456")
		if err == nil {
			t.Error("Expected error for session mapping after deletion")
		}
	})

	t.Run("Delete non-existent runtime", func(t *testing.T) {
		err := sm.DeleteRuntime("non-existent")
		if err == nil {
			t.Error("Expected error for non-existent runtime")
		}
	})
}

func TestListRuntimes(t *testing.T) {
	sm := NewStateManager()

	t.Run("List empty runtimes", func(t *testing.T) {
		runtimes := sm.ListRuntimes()
		if len(runtimes) != 0 {
			t.Errorf("Expected 0 runtimes, got %d", len(runtimes))
		}
	})

	t.Run("List multiple runtimes", func(t *testing.T) {
		sm.AddRuntime(&RuntimeInfo{RuntimeID: "runtime-1", SessionID: "session-1"})
		sm.AddRuntime(&RuntimeInfo{RuntimeID: "runtime-2", SessionID: "session-2"})
		sm.AddRuntime(&RuntimeInfo{RuntimeID: "runtime-3", SessionID: "session-3"})

		runtimes := sm.ListRuntimes()
		if len(runtimes) != 3 {
			t.Errorf("Expected 3 runtimes, got %d", len(runtimes))
		}
	})
}

func TestGetRuntimesBySessionIDs(t *testing.T) {
	sm := NewStateManager()
	
	sm.AddRuntime(&RuntimeInfo{RuntimeID: "runtime-1", SessionID: "session-1"})
	sm.AddRuntime(&RuntimeInfo{RuntimeID: "runtime-2", SessionID: "session-2"})
	sm.AddRuntime(&RuntimeInfo{RuntimeID: "runtime-3", SessionID: "session-3"})

	t.Run("Get subset of sessions", func(t *testing.T) {
		sessionIDs := []string{"session-1", "session-3"}
		runtimes := sm.GetRuntimesBySessionIDs(sessionIDs)
		if len(runtimes) != 2 {
			t.Errorf("Expected 2 runtimes, got %d", len(runtimes))
		}
	})

	t.Run("Get with non-existent sessions", func(t *testing.T) {
		sessionIDs := []string{"session-1", "non-existent"}
		runtimes := sm.GetRuntimesBySessionIDs(sessionIDs)
		if len(runtimes) != 1 {
			t.Errorf("Expected 1 runtime, got %d", len(runtimes))
		}
	})

	t.Run("Get empty list", func(t *testing.T) {
		runtimes := sm.GetRuntimesBySessionIDs([]string{})
		if len(runtimes) != 0 {
			t.Errorf("Expected 0 runtimes, got %d", len(runtimes))
		}
	})
}

func TestConcurrentAccess(t *testing.T) {
	sm := NewStateManager()
	
	// Test concurrent writes
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(id int) {
			info := &RuntimeInfo{
				RuntimeID: string(rune('A' + id)),
				SessionID: string(rune('a' + id)),
			}
			sm.AddRuntime(info)
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify all were added
	runtimes := sm.ListRuntimes()
	if len(runtimes) != 10 {
		t.Errorf("Expected 10 runtimes after concurrent writes, got %d", len(runtimes))
	}
}
