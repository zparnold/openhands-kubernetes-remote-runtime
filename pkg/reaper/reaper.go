package reaper

import (
	"context"
	"fmt"
	"time"

	"github.com/zparnold/openhands-kubernetes-remote-runtime/pkg/config"
	"github.com/zparnold/openhands-kubernetes-remote-runtime/pkg/logger"
	"github.com/zparnold/openhands-kubernetes-remote-runtime/pkg/state"
	"github.com/zparnold/openhands-kubernetes-remote-runtime/pkg/types"
)

// K8sClient defines the interface for Kubernetes operations needed by the reaper
type K8sClient interface {
	DeleteSandbox(ctx context.Context, runtimeInfo *state.RuntimeInfo) error
}

// Reaper handles automatic cleanup of idle sandboxes
type Reaper struct {
	stateMgr      *state.StateManager
	k8sClient     K8sClient
	config        *config.Config
	stopChan      chan struct{}
	idleTimeout   time.Duration
	checkInterval time.Duration
}

// NewReaper creates a new idle sandbox reaper
func NewReaper(stateMgr *state.StateManager, k8sClient K8sClient, cfg *config.Config) *Reaper {
	idleTimeout := time.Duration(cfg.IdleTimeoutHours) * time.Hour
	return &Reaper{
		stateMgr:      stateMgr,
		k8sClient:     k8sClient,
		config:        cfg,
		stopChan:      make(chan struct{}),
		idleTimeout:   idleTimeout,
		checkInterval: cfg.ReaperCheckInterval,
	}
}

// Start begins the reaper background goroutine
func (r *Reaper) Start() {
	logger.Info("Starting idle sandbox reaper (idle timeout: %s, check interval: %s)",
		r.idleTimeout, r.checkInterval)

	go r.run()
}

// Stop gracefully stops the reaper
func (r *Reaper) Stop() {
	logger.Info("Stopping idle sandbox reaper...")
	close(r.stopChan)
}

// run is the main reaper loop
func (r *Reaper) run() {
	ticker := time.NewTicker(r.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			r.checkAndReapIdleSandboxes()
		case <-r.stopChan:
			logger.Info("Idle sandbox reaper stopped")
			return
		}
	}
}

// checkAndReapIdleSandboxes checks all runtimes and reaps idle ones
func (r *Reaper) checkAndReapIdleSandboxes() {
	logger.Debug("Reaper: Checking for idle sandboxes...")

	runtimes := r.stateMgr.ListRuntimes()
	now := time.Now()
	reapedCount := 0

	for _, runtime := range runtimes {
		// Only check running sandboxes
		if runtime.Status != types.StatusRunning {
			continue
		}

		// Check if idle
		idleDuration := now.Sub(runtime.LastActivityTime)
		if idleDuration > r.idleTimeout {
			logger.Info("Reaper: Sandbox %s (session: %s) idle for %s, reaping...",
				runtime.RuntimeID, runtime.SessionID, idleDuration.Round(time.Second))

			if err := r.reapSandbox(runtime); err != nil {
				logger.Info("Reaper: Failed to reap sandbox %s: %v", runtime.RuntimeID, err)
			} else {
				reapedCount++
				logger.Info("Reaper: Successfully reaped idle sandbox %s", runtime.RuntimeID)
			}
		}
	}

	if reapedCount > 0 {
		logger.Info("Reaper: Reaped %d idle sandbox(es)", reapedCount)
	} else {
		logger.Debug("Reaper: No idle sandboxes to reap")
	}
}

// reapSandbox tears down a sandbox (pod, service, ingress)
func (r *Reaper) reapSandbox(runtime *state.RuntimeInfo) error {
	// Create context with timeout for cleanup operations
	ctx, cancel := context.WithTimeout(context.Background(), r.config.K8sOperationTimeout)
	defer cancel()

	// Delete the sandbox resources
	if err := r.k8sClient.DeleteSandbox(ctx, runtime); err != nil {
		return fmt.Errorf("failed to delete sandbox resources: %w", err)
	}

	// Update status and remove from state
	runtime.Status = types.StatusStopped
	if err := r.stateMgr.UpdateRuntime(runtime); err != nil {
		logger.Debug("Reaper: Failed to update runtime status: %v", err)
	}

	if err := r.stateMgr.DeleteRuntime(runtime.RuntimeID); err != nil {
		logger.Debug("Reaper: Failed to delete runtime from state: %v", err)
	}

	return nil
}
