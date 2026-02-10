package state

import (
	"fmt"
	"sync"
	"time"

	"github.com/zparnold/openhands-kubernetes-remote-runtime/pkg/types"
)

// RuntimeInfo stores information about a runtime
type RuntimeInfo struct {
	RuntimeID      string
	SessionID      string
	URL            string
	SessionAPIKey  string
	Status         types.RuntimeStatus
	PodStatus      types.PodStatus
	WorkHosts      map[string]int
	PodName        string
	ServiceName    string
	IngressName    string
	RestartCount   int
	RestartReasons []string
	CreatedAt      time.Time // Track when the runtime was created for cleanup purposes
}

// StateManager manages runtime state
type StateManager struct {
	mu               sync.RWMutex
	runtimeByID      map[string]*RuntimeInfo
	runtimeBySession map[string]*RuntimeInfo
}

// NewStateManager creates a new state manager
func NewStateManager() *StateManager {
	return &StateManager{
		runtimeByID:      make(map[string]*RuntimeInfo),
		runtimeBySession: make(map[string]*RuntimeInfo),
	}
}

// AddRuntime adds a new runtime to the state
func (s *StateManager) AddRuntime(info *RuntimeInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.runtimeByID[info.RuntimeID] = info
	s.runtimeBySession[info.SessionID] = info
}

// GetRuntimeByID retrieves a runtime by its ID
func (s *StateManager) GetRuntimeByID(runtimeID string) (*RuntimeInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	info, exists := s.runtimeByID[runtimeID]
	if !exists {
		return nil, fmt.Errorf("runtime not found: %s", runtimeID)
	}
	return info, nil
}

// GetRuntimeBySessionID retrieves a runtime by its session ID
func (s *StateManager) GetRuntimeBySessionID(sessionID string) (*RuntimeInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	info, exists := s.runtimeBySession[sessionID]
	if !exists {
		return nil, fmt.Errorf("runtime not found for session: %s", sessionID)
	}
	return info, nil
}

// UpdateRuntime updates runtime information
func (s *StateManager) UpdateRuntime(info *RuntimeInfo) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.runtimeByID[info.RuntimeID]; !exists {
		return fmt.Errorf("runtime not found: %s", info.RuntimeID)
	}

	s.runtimeByID[info.RuntimeID] = info
	s.runtimeBySession[info.SessionID] = info
	return nil
}

// DeleteRuntime removes a runtime from the state
func (s *StateManager) DeleteRuntime(runtimeID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	info, exists := s.runtimeByID[runtimeID]
	if !exists {
		return fmt.Errorf("runtime not found: %s", runtimeID)
	}

	delete(s.runtimeByID, runtimeID)
	delete(s.runtimeBySession, info.SessionID)
	return nil
}

// ListRuntimes returns all runtimes
func (s *StateManager) ListRuntimes() []*RuntimeInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	runtimes := make([]*RuntimeInfo, 0, len(s.runtimeByID))
	for _, info := range s.runtimeByID {
		runtimes = append(runtimes, info)
	}
	return runtimes
}

// GetRuntimesBySessionIDs retrieves multiple runtimes by session IDs
func (s *StateManager) GetRuntimesBySessionIDs(sessionIDs []string) []*RuntimeInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	runtimes := make([]*RuntimeInfo, 0, len(sessionIDs))
	for _, sessionID := range sessionIDs {
		if info, exists := s.runtimeBySession[sessionID]; exists {
			runtimes = append(runtimes, info)
		}
	}
	return runtimes
}
