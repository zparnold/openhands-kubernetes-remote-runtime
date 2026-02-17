package reaper

import (
	"context"
	"testing"
	"time"

	"github.com/zparnold/openhands-kubernetes-remote-runtime/pkg/config"
	"github.com/zparnold/openhands-kubernetes-remote-runtime/pkg/state"
	"github.com/zparnold/openhands-kubernetes-remote-runtime/pkg/types"
)

// mockK8sClient implements a mock Kubernetes client for testing
type mockK8sClient struct {
	deletedRuntimes []*state.RuntimeInfo
}

func (m *mockK8sClient) DeleteSandbox(ctx context.Context, runtime *state.RuntimeInfo) error {
	m.deletedRuntimes = append(m.deletedRuntimes, runtime)
	return nil
}

func TestNewReaper(t *testing.T) {
	cfg := &config.Config{
		IdleTimeoutHours:    12,
		ReaperCheckInterval: 15 * time.Minute,
		K8sOperationTimeout: 60 * time.Second,
	}
	stateMgr := state.NewStateManager()

	reaper := NewReaper(stateMgr, nil, cfg)
	if reaper == nil {
		t.Fatal("NewReaper should return non-nil Reaper")
	}
	if reaper.idleTimeout != 12*time.Hour {
		t.Errorf("Expected idle timeout of 12 hours, got %v", reaper.idleTimeout)
	}
	if reaper.checkInterval != 15*time.Minute {
		t.Errorf("Expected check interval of 15 minutes, got %v", reaper.checkInterval)
	}
}

func TestReaper_ReapIdleSandbox(t *testing.T) {
	cfg := &config.Config{
		IdleTimeoutHours:    1, // 1 hour for testing
		ReaperCheckInterval: 1 * time.Minute,
		K8sOperationTimeout: 60 * time.Second,
	}
	stateMgr := state.NewStateManager()
	mockClient := &mockK8sClient{
		deletedRuntimes: make([]*state.RuntimeInfo, 0),
	}

	// Create a reaper instance - we'll manually call methods instead of Start()
	reaper := &Reaper{
		stateMgr:      stateMgr,
		k8sClient:     mockClient,
		config:        cfg,
		stopChan:      make(chan struct{}),
		idleTimeout:   1 * time.Hour,
		checkInterval: 1 * time.Minute,
	}

	// Add an idle runtime (last activity was 2 hours ago)
	idleRuntime := &state.RuntimeInfo{
		RuntimeID:        "runtime-idle-1",
		SessionID:        "session-idle-1",
		Status:           types.StatusRunning,
		PodStatus:        types.PodStatusReady,
		PodName:          "runtime-idle-1",
		ServiceName:      "runtime-idle-1",
		IngressName:      "runtime-idle-1",
		LastActivityTime: time.Now().Add(-2 * time.Hour),
	}
	stateMgr.AddRuntime(idleRuntime)

	// Add an active runtime (last activity was 30 minutes ago)
	activeRuntime := &state.RuntimeInfo{
		RuntimeID:        "runtime-active-1",
		SessionID:        "session-active-1",
		Status:           types.StatusRunning,
		PodStatus:        types.PodStatusReady,
		PodName:          "runtime-active-1",
		ServiceName:      "runtime-active-1",
		IngressName:      "runtime-active-1",
		LastActivityTime: time.Now().Add(-30 * time.Minute),
	}
	stateMgr.AddRuntime(activeRuntime)

	// Add a paused runtime (should be ignored)
	pausedRuntime := &state.RuntimeInfo{
		RuntimeID:        "runtime-paused-1",
		SessionID:        "session-paused-1",
		Status:           types.StatusPaused,
		PodStatus:        types.PodStatusNotFound,
		PodName:          "runtime-paused-1",
		ServiceName:      "runtime-paused-1",
		IngressName:      "runtime-paused-1",
		LastActivityTime: time.Now().Add(-3 * time.Hour),
	}
	stateMgr.AddRuntime(pausedRuntime)

	// Run the reaper check
	reaper.checkAndReapIdleSandboxes()

	// Verify that only the idle runtime was reaped
	if len(mockClient.deletedRuntimes) != 1 {
		t.Fatalf("Expected 1 runtime to be deleted, got %d", len(mockClient.deletedRuntimes))
	}
	if mockClient.deletedRuntimes[0].RuntimeID != "runtime-idle-1" {
		t.Errorf("Expected idle runtime to be deleted, got %s", mockClient.deletedRuntimes[0].RuntimeID)
	}

	// Verify the idle runtime was removed from state
	_, err := stateMgr.GetRuntimeByID("runtime-idle-1")
	if err == nil {
		t.Error("Expected idle runtime to be removed from state")
	}

	// Verify active runtime still exists
	_, err = stateMgr.GetRuntimeByID("runtime-active-1")
	if err != nil {
		t.Error("Active runtime should still exist in state")
	}

	// Verify paused runtime still exists
	_, err = stateMgr.GetRuntimeByID("runtime-paused-1")
	if err != nil {
		t.Error("Paused runtime should still exist in state")
	}
}

func TestReaper_NoIdleSandboxes(t *testing.T) {
	cfg := &config.Config{
		IdleTimeoutHours:    1,
		ReaperCheckInterval: 1 * time.Minute,
		K8sOperationTimeout: 60 * time.Second,
	}
	stateMgr := state.NewStateManager()
	mockClient := &mockK8sClient{
		deletedRuntimes: make([]*state.RuntimeInfo, 0),
	}

	reaper := &Reaper{
		stateMgr:      stateMgr,
		k8sClient:     mockClient,
		config:        cfg,
		stopChan:      make(chan struct{}),
		idleTimeout:   1 * time.Hour,
		checkInterval: 1 * time.Minute,
	}

	// Add only active runtimes
	activeRuntime := &state.RuntimeInfo{
		RuntimeID:        "runtime-active-1",
		SessionID:        "session-active-1",
		Status:           types.StatusRunning,
		PodStatus:        types.PodStatusReady,
		LastActivityTime: time.Now().Add(-30 * time.Minute),
	}
	stateMgr.AddRuntime(activeRuntime)

	// Run the reaper check
	reaper.checkAndReapIdleSandboxes()

	// Verify no runtimes were deleted
	if len(mockClient.deletedRuntimes) != 0 {
		t.Errorf("Expected no runtimes to be deleted, got %d", len(mockClient.deletedRuntimes))
	}

	// Verify runtime still exists
	_, err := stateMgr.GetRuntimeByID("runtime-active-1")
	if err != nil {
		t.Error("Active runtime should still exist in state")
	}
}

func TestReaper_EmptyState(t *testing.T) {
	cfg := &config.Config{
		IdleTimeoutHours:    1,
		ReaperCheckInterval: 1 * time.Minute,
		K8sOperationTimeout: 60 * time.Second,
	}
	stateMgr := state.NewStateManager()
	mockClient := &mockK8sClient{
		deletedRuntimes: make([]*state.RuntimeInfo, 0),
	}

	reaper := &Reaper{
		stateMgr:      stateMgr,
		k8sClient:     mockClient,
		config:        cfg,
		stopChan:      make(chan struct{}),
		idleTimeout:   1 * time.Hour,
		checkInterval: 1 * time.Minute,
	}

	// Run the reaper check on empty state
	reaper.checkAndReapIdleSandboxes()

	// Verify no runtimes were deleted
	if len(mockClient.deletedRuntimes) != 0 {
		t.Errorf("Expected no runtimes to be deleted, got %d", len(mockClient.deletedRuntimes))
	}
}

func TestReaper_StartStop(t *testing.T) {
	cfg := &config.Config{
		IdleTimeoutHours:    1,
		ReaperCheckInterval: 100 * time.Millisecond, // Short interval for testing
		K8sOperationTimeout: 60 * time.Second,
	}
	stateMgr := state.NewStateManager()
	mockClient := &mockK8sClient{}

	reaper := NewReaper(stateMgr, mockClient, cfg)

	// Start the reaper
	reaper.Start()

	// Let it run for a bit
	time.Sleep(250 * time.Millisecond)

	// Stop the reaper
	reaper.Stop()

	// Give it time to stop
	time.Sleep(100 * time.Millisecond)

	// Test passes if no panic occurs
}
