package cleanup

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/zparnold/openhands-kubernetes-remote-runtime/pkg/config"
	"github.com/zparnold/openhands-kubernetes-remote-runtime/pkg/k8s"
	"github.com/zparnold/openhands-kubernetes-remote-runtime/pkg/logger"
	"github.com/zparnold/openhands-kubernetes-remote-runtime/pkg/state"
	"github.com/zparnold/openhands-kubernetes-remote-runtime/pkg/types"
)

// Service handles cleanup of orphaned resources
type Service struct {
	k8sClient *k8s.Client
	stateMgr  *state.StateManager
	config    *config.Config
	stopChan  chan struct{}
	wg        sync.WaitGroup
	mu        sync.RWMutex
	lastRun   time.Time
	stats     CleanupStats
}

// CleanupStats tracks cleanup metrics
type CleanupStats struct {
	LastRunTime       time.Time
	TotalRunCount     int
	TotalCleaned      int
	FailedCleaned     int
	IdleCleaned       int
	LastCleanupErrors []string
}

// NewService creates a new cleanup service
func NewService(k8sClient *k8s.Client, stateMgr *state.StateManager, cfg *config.Config) *Service {
	return &Service{
		k8sClient: k8sClient,
		stateMgr:  stateMgr,
		config:    cfg,
		stopChan:  make(chan struct{}),
	}
}

// Start begins the cleanup service
func (s *Service) Start(ctx context.Context) {
	if !s.config.CleanupEnabled {
		logger.Info("Cleanup service is disabled")
		return
	}

	logger.Info("Starting cleanup service - Interval: %d minutes, Failed threshold: %d minutes, Idle threshold: %d minutes",
		s.config.CleanupIntervalMinutes, s.config.CleanupFailedThresholdMin, s.config.CleanupIdleThresholdMin)

	s.wg.Add(1)
	go s.run(ctx)
}

// Stop stops the cleanup service
func (s *Service) Stop() {
	if !s.config.CleanupEnabled {
		return
	}

	logger.Info("Stopping cleanup service...")
	close(s.stopChan)
	s.wg.Wait()
	logger.Info("Cleanup service stopped")
}

// GetStats returns current cleanup statistics
func (s *Service) GetStats() CleanupStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stats
}

func (s *Service) run(ctx context.Context) {
	defer s.wg.Done()

	// Run cleanup immediately on start
	s.runCleanup(ctx)

	ticker := time.NewTicker(time.Duration(s.config.CleanupIntervalMinutes) * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info("Cleanup service context cancelled")
			return
		case <-s.stopChan:
			logger.Info("Cleanup service stop signal received")
			return
		case <-ticker.C:
			s.runCleanup(ctx)
		}
	}
}

func (s *Service) runCleanup(ctx context.Context) {
	logger.Debug("Cleanup: Starting cleanup run")
	s.mu.Lock()
	s.lastRun = time.Now()
	s.stats.TotalRunCount++
	s.stats.LastCleanupErrors = []string{}
	s.mu.Unlock()

	runtimes := s.stateMgr.ListRuntimes()
	logger.Debug("Cleanup: Found %d runtimes to check", len(runtimes))

	var cleanedCount, failedCount, idleCount int
	var errors []string

	for _, runtime := range runtimes {
		// Skip if runtime is already stopped or being stopped
		if runtime.Status == types.StatusStopped {
			continue
		}

		// Get current pod status from Kubernetes
		podStatus, err := s.k8sClient.GetPodStatus(ctx, runtime.PodName)
		if err != nil {
			logger.Debug("Cleanup: Error getting pod status for %s: %v", runtime.PodName, err)
			errors = append(errors, fmt.Sprintf("error getting pod status for %s: %v", runtime.PodName, err))
			continue
		}

		shouldCleanup, reason := s.shouldCleanupRuntime(runtime, podStatus)
		if shouldCleanup {
			logger.Info("Cleanup: Cleaning up runtime %s (session: %s) - Reason: %s",
				runtime.RuntimeID, runtime.SessionID, reason)

			if err := s.k8sClient.DeleteSandbox(ctx, runtime); err != nil {
				logger.Info("Cleanup: Error deleting sandbox for runtime %s: %v", runtime.RuntimeID, err)
				errors = append(errors, fmt.Sprintf("error deleting sandbox for %s: %v", runtime.RuntimeID, err))
				continue
			}

			// Remove from state
			if err := s.stateMgr.DeleteRuntime(runtime.RuntimeID); err != nil {
				logger.Debug("Cleanup: Error removing runtime from state %s: %v", runtime.RuntimeID, err)
			}

			cleanedCount++
			switch reason {
			case "pod_failed":
				failedCount++
			case "pod_idle":
				idleCount++
			}

			logger.Debug("Cleanup: Successfully cleaned up runtime %s", runtime.RuntimeID)
		}
	}

	s.mu.Lock()
	s.stats.TotalCleaned += cleanedCount
	s.stats.FailedCleaned += failedCount
	s.stats.IdleCleaned += idleCount
	s.stats.LastCleanupErrors = errors
	s.mu.Unlock()

	if cleanedCount > 0 {
		logger.Info("Cleanup: Completed - Cleaned %d runtimes (%d failed, %d idle)", cleanedCount, failedCount, idleCount)
	} else {
		logger.Debug("Cleanup: Completed - No runtimes cleaned")
	}
}

// shouldCleanupRuntime determines if a runtime should be cleaned up
func (s *Service) shouldCleanupRuntime(runtime *state.RuntimeInfo, podStatus *k8s.PodStatusInfo) (bool, string) {
	now := time.Now()

	// Check if pod is in a failed state for too long
	if podStatus.Status == types.PodStatusFailed || podStatus.Status == types.PodStatusCrashLoopBackOff {
		failedThreshold := time.Duration(s.config.CleanupFailedThresholdMin) * time.Minute
		if now.Sub(runtime.CreatedAt) >= failedThreshold {
			return true, "pod_failed"
		}
	}

	// Check if pod has been idle for too long
	// We consider a pod idle if it's been running (or in any non-failed state) for longer than the idle threshold
	// This is a simple time-based check; a more sophisticated approach would check actual activity:
	//   - Track last API request time per runtime (requires updating RuntimeInfo on each request)
	//   - Monitor container resource usage (CPU/memory)
	//   - Check application-specific metrics (e.g., last command execution time)
	// Current implementation: Simple time-based cleanup that triggers after CLEANUP_IDLE_THRESHOLD_MINUTES
	// regardless of whether the pod is actively being used
	if podStatus.Status != types.PodStatusFailed && podStatus.Status != types.PodStatusCrashLoopBackOff {
		idleThreshold := time.Duration(s.config.CleanupIdleThresholdMin) * time.Minute
		if now.Sub(runtime.CreatedAt) >= idleThreshold {
			return true, "pod_idle"
		}
	}

	return false, ""
}
