package cleanup

import (
	"testing"
	"time"

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
		t.Error("NewService() returned nil")
	}
	if s.config != cfg {
		t.Error("NewService() config not set correctly")
	}
	if s.stopChan == nil {
		t.Error("NewService() stopChan not initialized")
	}
}
