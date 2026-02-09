package types

import (
	"encoding/json"
	"strings"
)

// FlexibleCommand accepts command as either a JSON string or a JSON array of strings
// (OpenHands may send e.g. ["/usr/local/bin/openhands-agent-server","--port","60000"]).
type FlexibleCommand []string

// UnmarshalJSON implements json.Unmarshaler so command can be a string or []string.
func (c *FlexibleCommand) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		if s == "" {
			*c = nil
		} else {
			*c = FlexibleCommand([]string{s})
		}
		return nil
	}
	var arr []string
	if err := json.Unmarshal(data, &arr); err != nil {
		return err
	}
	*c = FlexibleCommand(arr)
	return nil
}

// String returns a space-joined representation (for backward compatibility).
func (c FlexibleCommand) String() string {
	return strings.Join(c, " ")
}

// StartRequest represents the request to start a new runtime
type StartRequest struct {
	Image          string            `json:"image"`
	Command        FlexibleCommand   `json:"command"`
	WorkingDir     string            `json:"working_dir"`
	Environment    map[string]string `json:"environment"`
	SessionID      string            `json:"session_id"`
	ResourceFactor float64           `json:"resource_factor,omitempty"`
	RuntimeClass   string            `json:"runtime_class,omitempty"`
}

// StopRequest represents the request to stop a runtime
type StopRequest struct {
	RuntimeID string `json:"runtime_id"`
}

// PauseRequest represents the request to pause a runtime
type PauseRequest struct {
	RuntimeID string `json:"runtime_id"`
}

// ResumeRequest represents the request to resume a runtime
type ResumeRequest struct {
	RuntimeID string `json:"runtime_id"`
}

// RuntimeStatus represents the status of a runtime
type RuntimeStatus string

const (
	StatusRunning RuntimeStatus = "running"
	StatusPaused  RuntimeStatus = "paused"
	StatusStopped RuntimeStatus = "stopped"
	StatusPending RuntimeStatus = "pending"
)

// PodStatus represents the Kubernetes pod status
type PodStatus string

const (
	PodStatusPending          PodStatus = "pending"
	PodStatusRunning          PodStatus = "running"
	PodStatusReady            PodStatus = "ready"
	PodStatusFailed           PodStatus = "failed"
	PodStatusCrashLoopBackOff PodStatus = "crashloopbackoff"
	PodStatusNotFound         PodStatus = "not found"
	PodStatusUnknown          PodStatus = "unknown"
)

// RuntimeResponse represents the response from runtime operations
type RuntimeResponse struct {
	RuntimeID      string         `json:"runtime_id"`
	SessionID      string         `json:"session_id"`
	URL            string         `json:"url"`
	VSCodeURL      string         `json:"vscode_url,omitempty"` // optional; when set (e.g. proxy mode), frontend uses this for "Open in VSCode"
	SessionAPIKey  string         `json:"session_api_key,omitempty"`
	Status         RuntimeStatus  `json:"status"`
	PodStatus      PodStatus      `json:"pod_status"`
	WorkHosts      map[string]int `json:"work_hosts,omitempty"`
	RestartCount   int            `json:"restart_count,omitempty"`
	RestartReasons []string       `json:"restart_reasons,omitempty"`
}

// ListResponse represents the response from list operations
type ListResponse struct {
	Runtimes []RuntimeResponse `json:"runtimes"`
}

// BatchSessionsResponse represents the response from batch sessions query
type BatchSessionsResponse struct {
	Sessions []RuntimeResponse `json:"sessions"`
}

// RegistryPrefixResponse represents the response from registry_prefix endpoint
type RegistryPrefixResponse struct {
	RegistryPrefix string `json:"registry_prefix"`
}

// ImageExistsResponse represents the response from image_exists endpoint
type ImageExistsResponse struct {
	Exists bool `json:"exists"`
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}
