package cleanup

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/zparnold/openhands-kubernetes-remote-runtime/pkg/config"
	"github.com/zparnold/openhands-kubernetes-remote-runtime/pkg/k8s"
	"github.com/zparnold/openhands-kubernetes-remote-runtime/pkg/state"
	"github.com/zparnold/openhands-kubernetes-remote-runtime/pkg/types"
)

func TestShouldCleanupRuntime(t *testing.T) {
	cfg := &config.Config{
		CleanupFailedThresholdMin: 60,   // 1 hour
		CleanupIdleThresholdMin:   1440, // 24 hours
	}

	s := &Service{
		config: cfg,
	}

	tests := []struct {
		name            string
		runtime         *state.RuntimeInfo
		podStatus       *k8s.PodStatusInfo
		expectedCleanup bool
		expectedReason  string
	}{
		{
			name: "Failed pod past threshold",
			runtime: &state.RuntimeInfo{
				RuntimeID: "test1",
				CreatedAt: time.Now().Add(-2 * time.Hour), // 2 hours ago
			},
			podStatus: &k8s.PodStatusInfo{
				Status: types.PodStatusFailed,
			},
			expectedCleanup: true,
			expectedReason:  "pod_failed",
		},
		{
			name: "Failed pod not past threshold",
			runtime: &state.RuntimeInfo{
				RuntimeID: "test2",
				CreatedAt: time.Now().Add(-30 * time.Minute), // 30 minutes ago
			},
			podStatus: &k8s.PodStatusInfo{
				Status: types.PodStatusFailed,
			},
			expectedCleanup: false,
			expectedReason:  "",
		},
		{
			name: "CrashLoopBackOff pod past threshold",
			runtime: &state.RuntimeInfo{
				RuntimeID: "test3",
				CreatedAt: time.Now().Add(-2 * time.Hour), // 2 hours ago
			},
			podStatus: &k8s.PodStatusInfo{
				Status: types.PodStatusCrashLoopBackOff,
			},
			expectedCleanup: true,
			expectedReason:  "pod_failed",
		},
		{
			name: "Idle running pod past threshold",
			runtime: &state.RuntimeInfo{
				RuntimeID: "test4",
				CreatedAt: time.Now().Add(-25 * time.Hour), // 25 hours ago
			},
			podStatus: &k8s.PodStatusInfo{
				Status: types.PodStatusReady,
			},
			expectedCleanup: true,
			expectedReason:  "pod_idle",
		},
		{
			name: "Running pod not past idle threshold",
			runtime: &state.RuntimeInfo{
				RuntimeID: "test5",
				CreatedAt: time.Now().Add(-12 * time.Hour), // 12 hours ago
			},
			podStatus: &k8s.PodStatusInfo{
				Status: types.PodStatusReady,
			},
			expectedCleanup: false,
			expectedReason:  "",
		},
		{
			name: "Pending pod not past idle threshold",
			runtime: &state.RuntimeInfo{
				RuntimeID: "test6",
				CreatedAt: time.Now().Add(-30 * time.Minute), // 30 minutes ago
			},
			podStatus: &k8s.PodStatusInfo{
				Status: types.PodStatusPending,
			},
			expectedCleanup: false,
			expectedReason:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shouldCleanup, reason := s.shouldCleanupRuntime(tt.runtime, tt.podStatus)
			if shouldCleanup != tt.expectedCleanup {
				t.Errorf("shouldCleanupRuntime() shouldCleanup = %v, want %v", shouldCleanup, tt.expectedCleanup)
			}
			if reason != tt.expectedReason {
				t.Errorf("shouldCleanupRuntime() reason = %v, want %v", reason, tt.expectedReason)
			}
		})
	}
}

func TestGetStats(t *testing.T) {
	cfg := &config.Config{
		CleanupEnabled: true,
	}

	s := NewService(nil, nil, cfg)

	// Set some stats
	s.mu.Lock()
	s.stats = CleanupStats{
		LastRunTime:   time.Now(),
		TotalRunCount: 5,
		TotalCleaned:  10,
		FailedCleaned: 3,
		IdleCleaned:   7,
	}
	s.mu.Unlock()

	stats := s.GetStats()

	if stats.TotalRunCount != 5 {
		t.Errorf("GetStats() TotalRunCount = %v, want 5", stats.TotalRunCount)
	}
	if stats.TotalCleaned != 10 {
		t.Errorf("GetStats() TotalCleaned = %v, want 10", stats.TotalCleaned)
	}
	if stats.FailedCleaned != 3 {
		t.Errorf("GetStats() FailedCleaned = %v, want 3", stats.FailedCleaned)
	}
	if stats.IdleCleaned != 7 {
		t.Errorf("GetStats() IdleCleaned = %v, want 7", stats.IdleCleaned)
	}
}

func TestNewService(t *testing.T) {
	cfg := &config.Config{
		CleanupEnabled: true,
	}

	s := NewService(nil, nil, cfg)

	if s == nil {
		t.Fatal("NewService() returned nil")
	}
	if s.config != cfg {
		t.Error("NewService() config not set correctly")
	}
	if s.stopChan == nil {
		t.Error("NewService() stopChan not initialized")
	}
}

func TestStart_Disabled(t *testing.T) {
	cfg := &config.Config{
		CleanupEnabled: false,
	}
	s := NewService(nil, nil, cfg)
	// Should not panic or block when disabled
	ctx := context.Background()
	s.Start(ctx)
	// No goroutine started, wg is at zero - Stop should return immediately
	s.Stop()
}

func TestStop_Disabled(t *testing.T) {
	cfg := &config.Config{
		CleanupEnabled: false,
	}
	s := NewService(nil, nil, cfg)
	// Stop on disabled service should not panic
	s.Stop()
}

func TestRunCleanup_EmptyState(t *testing.T) {
	cfg := &config.Config{
		CleanupFailedThresholdMin: 60,
		CleanupIdleThresholdMin:   1440,
	}
	stateMgr := state.NewStateManager()
	fakeClientset := fake.NewSimpleClientset()
	k8sClient := k8s.NewClientFromInterface(fakeClientset, &config.Config{Namespace: "test-ns"})

	s := NewService(k8sClient, stateMgr, cfg)
	ctx := context.Background()
	s.runCleanup(ctx)

	stats := s.GetStats()
	if stats.TotalRunCount != 1 {
		t.Errorf("expected TotalRunCount=1, got %d", stats.TotalRunCount)
	}
	if stats.TotalCleaned != 0 {
		t.Errorf("expected TotalCleaned=0, got %d", stats.TotalCleaned)
	}
}

func TestRunCleanup_SkipsStoppedRuntime(t *testing.T) {
	cfg := &config.Config{
		CleanupFailedThresholdMin: 60,
		CleanupIdleThresholdMin:   1440,
	}
	stateMgr := state.NewStateManager()
	stateMgr.AddRuntime(&state.RuntimeInfo{
		RuntimeID: "stopped-rt",
		SessionID: "stopped-sess",
		Status:    types.StatusStopped,
		PodName:   "stopped-pod",
		CreatedAt: time.Now().Add(-48 * time.Hour),
	})

	fakeClientset := fake.NewSimpleClientset()
	k8sClient := k8s.NewClientFromInterface(fakeClientset, &config.Config{Namespace: "test-ns"})

	s := NewService(k8sClient, stateMgr, cfg)
	ctx := context.Background()
	s.runCleanup(ctx)

	stats := s.GetStats()
	if stats.TotalCleaned != 0 {
		t.Errorf("expected stopped runtime to not be cleaned, got TotalCleaned=%d", stats.TotalCleaned)
	}
}

func TestRunCleanup_CleansFailedRuntime(t *testing.T) {
	cfg := &config.Config{
		CleanupFailedThresholdMin: 60,
		CleanupIdleThresholdMin:   1440,
	}
	stateMgr := state.NewStateManager()
	stateMgr.AddRuntime(&state.RuntimeInfo{
		RuntimeID: "failed-rt",
		SessionID: "failed-sess",
		Status:    types.StatusRunning,
		PodName:   "failed-pod",
		// Created 2 hours ago - past the 60-minute failed threshold
		CreatedAt: time.Now().Add(-2 * time.Hour),
	})

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "failed-pod", Namespace: "test-ns"},
		Status:     corev1.PodStatus{Phase: corev1.PodFailed},
	}
	fakeClientset := fake.NewSimpleClientset(pod)
	k8sClient := k8s.NewClientFromInterface(fakeClientset, &config.Config{Namespace: "test-ns"})

	s := NewService(k8sClient, stateMgr, cfg)
	ctx := context.Background()
	s.runCleanup(ctx)

	stats := s.GetStats()
	if stats.TotalCleaned != 1 {
		t.Errorf("expected TotalCleaned=1, got %d", stats.TotalCleaned)
	}
	if stats.FailedCleaned != 1 {
		t.Errorf("expected FailedCleaned=1, got %d", stats.FailedCleaned)
	}
}

func TestRunCleanup_CleansIdleRuntime(t *testing.T) {
	cfg := &config.Config{
		CleanupFailedThresholdMin: 60,
		CleanupIdleThresholdMin:   1440, // 24 hours
	}
	stateMgr := state.NewStateManager()
	stateMgr.AddRuntime(&state.RuntimeInfo{
		RuntimeID:        "idle-rt",
		SessionID:        "idle-sess",
		Status:           types.StatusRunning,
		PodName:          "idle-pod",
		CreatedAt:        time.Now().Add(-25 * time.Hour),
		LastActivityTime: time.Now().Add(-25 * time.Hour),
	})

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "idle-pod", Namespace: "test-ns"},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning},
	}
	fakeClientset := fake.NewSimpleClientset(pod)
	k8sClient := k8s.NewClientFromInterface(fakeClientset, &config.Config{Namespace: "test-ns"})

	s := NewService(k8sClient, stateMgr, cfg)
	ctx := context.Background()
	s.runCleanup(ctx)

	stats := s.GetStats()
	if stats.TotalCleaned != 1 {
		t.Errorf("expected TotalCleaned=1, got %d", stats.TotalCleaned)
	}
	if stats.IdleCleaned != 1 {
		t.Errorf("expected IdleCleaned=1, got %d", stats.IdleCleaned)
	}
}

func TestShouldCleanupRuntime_WithLastActivityTime(t *testing.T) {
	cfg := &config.Config{
		CleanupFailedThresholdMin: 60,
		CleanupIdleThresholdMin:   1440,
	}
	s := &Service{config: cfg}

	// Runtime with LastActivityTime set (not zero) - should use it instead of CreatedAt
	runtime := &state.RuntimeInfo{
		RuntimeID:        "active-rt",
		CreatedAt:        time.Now().Add(-30 * time.Hour), // Old creation, but...
		LastActivityTime: time.Now().Add(-1 * time.Hour),  // ...recently active
	}
	podStatus := &k8s.PodStatusInfo{Status: types.PodStatusReady}

	shouldCleanup, _ := s.shouldCleanupRuntime(runtime, podStatus)
	if shouldCleanup {
		t.Error("expected recently active runtime to NOT be cleaned up")
	}
}

func TestStartStop_Enabled(t *testing.T) {
	cfg := &config.Config{
		CleanupEnabled:            true,
		CleanupIntervalMinutes:    60, // Long interval so it doesn't fire during test
		CleanupFailedThresholdMin: 60,
		CleanupIdleThresholdMin:   1440,
	}
	stateMgr := state.NewStateManager()
	fakeClientset := fake.NewSimpleClientset()
	k8sClient := k8s.NewClientFromInterface(fakeClientset, &config.Config{Namespace: "test-ns"})

	s := NewService(k8sClient, stateMgr, cfg)
	ctx, cancel := context.WithCancel(context.Background())

	s.Start(ctx)
	// Cancel context to trigger graceful shutdown via ctx.Done()
	cancel()
	// Stop should complete quickly
	s.Stop()
}
