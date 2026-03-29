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

	// Batch-fetch all pod statuses in a single K8s API call.
	podNames := make([]string, 0, len(runtimes))
	for _, runtime := range runtimes {
		if runtime.Status != types.StatusStopped {
			podNames = append(podNames, runtime.PodName)
		}
	}
	statuses, statusErr := s.k8sClient.GetPodStatuses(ctx, podNames)
	if statusErr != nil {
		logger.Debug("Cleanup: Failed to batch-fetch pod statuses: %v", statusErr)
		errors = append(errors, fmt.Sprintf("batch pod status fetch failed: %v", statusErr))
	}

	for _, runtime := range runtimes {
		// Skip if runtime is already stopped or being stopped
		if runtime.Status == types.StatusStopped {
			continue
		}

		// Skip if batch fetch failed or pod not found in results
		if statusErr != nil {
			continue
		}
		podStatus, ok := statuses[runtime.PodName]
		if !ok {
			continue
		}

		shouldCleanup, reason := s.shouldCleanupRuntime(runtime, podStatus)
		if shouldCleanup {
			logger.Info("Cleanup: Cleaning up runtime %s (session: %s) - Reason: %s, Restarts: %d, LastTermination: %s (exit %d) %s",
				runtime.RuntimeID, runtime.SessionID, reason,
				podStatus.RestartCount, podStatus.LastTerminationReason,
				podStatus.LastTerminationExitCode, podStatus.LastTerminationMessage)

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
			case "pod_failed", "excessive_restarts", "pod_not_found":
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

	// Grace period: never clean up runtimes that are still pending or were
	// created very recently.  POST /start adds the runtime to state before
	// the pod exists in K8s, so a concurrent cleanup cycle would see
	// "pod_not_found" and delete the pod moments after it is created.
	// A 60-second grace window prevents this race.
	const creationGracePeriod = 60 * time.Second
	if runtime.Status == types.StatusPending || now.Sub(runtime.CreatedAt) < creationGracePeriod {
		return false, ""
	}

	// Pod no longer exists — clean up orphaned services/ingresses immediately.
	if podStatus.Status == types.PodStatusNotFound {
		return true, "pod_not_found"
	}

	// Excessive restarts indicate persistent OOMKills or crash loops even if the
	// pod is technically Ready right now. Clean up to free cluster resources.
	if s.config.CleanupRestartThreshold > 0 && podStatus.RestartCount >= s.config.CleanupRestartThreshold {
		return true, "excessive_restarts"
	}

	// Check if pod is in a failed state for too long
	if podStatus.Status == types.PodStatusFailed || podStatus.Status == types.PodStatusCrashLoopBackOff {
		failedThreshold := time.Duration(s.config.CleanupFailedThresholdMin) * time.Minute
		if now.Sub(runtime.CreatedAt) >= failedThreshold {
			return true, "pod_failed"
		}
	}

	// Check if pod has been idle for too long based on last activity time.
	// LastActivityTime is updated on every proxied request (ProxySandbox handler)
	// and on activity heartbeats from the app-server.
	if podStatus.Status != types.PodStatusFailed && podStatus.Status != types.PodStatusCrashLoopBackOff {
		idleThreshold := time.Duration(s.config.CleanupIdleThresholdMin) * time.Minute
		lastActive := runtime.LastActivityTime
		if lastActive.IsZero() {
			lastActive = runtime.CreatedAt
		}
		if now.Sub(lastActive) >= idleThreshold {
			return true, "pod_idle"
		}
	}

	return false, ""
}
