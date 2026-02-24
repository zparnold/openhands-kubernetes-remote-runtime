package k8s

import (
	"context"
	"testing"
	"time"

	"github.com/zparnold/openhands-kubernetes-remote-runtime/pkg/config"
	"github.com/zparnold/openhands-kubernetes-remote-runtime/pkg/state"
	"github.com/zparnold/openhands-kubernetes-remote-runtime/pkg/types"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func testConfig() *config.Config {
	return &config.Config{
		Namespace:       "test-ns",
		BaseDomain:      "test.example.com",
		AgentServerPort: 60000,
		VSCodePort:      60001,
		Worker1Port:     12000,
		Worker2Port:     12001,
		IngressClass:    "nginx",
	}
}

func TestPortToInt32(t *testing.T) {
	tests := []struct {
		name     string
		port     int
		expected int32
	}{
		{"valid port", 8080, 8080},
		{"minimum valid port", 1, 1},
		{"maximum valid port", 65535, 65535},
		{"port too low (0)", 0, 1},
		{"port too low (negative)", -1, 1},
		{"port too high", 70000, 65535},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := portToInt32(tt.port)
			if result != tt.expected {
				t.Errorf("portToInt32(%d) = %d, want %d", tt.port, result, tt.expected)
			}
		})
	}
}

func TestGetPodStatus_NotFound(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	cfg := testConfig()
	c := NewClientFromInterface(fakeClient, cfg)

	status, err := c.GetPodStatus(context.Background(), "non-existent-pod")
	if err != nil {
		t.Fatalf("expected no error for missing pod, got: %v", err)
	}
	if status.Status != types.PodStatusNotFound {
		t.Errorf("expected PodStatusNotFound, got %s", status.Status)
	}
}

func TestGetPodStatus_Pending(t *testing.T) {
	fakeClient := fake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod-pending", Namespace: "test-ns"},
		Status:     corev1.PodStatus{Phase: corev1.PodPending},
	})
	c := NewClientFromInterface(fakeClient, testConfig())

	status, err := c.GetPodStatus(context.Background(), "pod-pending")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.Status != types.PodStatusPending {
		t.Errorf("expected PodStatusPending, got %s", status.Status)
	}
}

func TestGetPodStatus_Running(t *testing.T) {
	fakeClient := fake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod-running", Namespace: "test-ns"},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{Ready: false, RestartCount: 0},
			},
		},
	})
	c := NewClientFromInterface(fakeClient, testConfig())

	status, err := c.GetPodStatus(context.Background(), "pod-running")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.Status != types.PodStatusRunning {
		t.Errorf("expected PodStatusRunning, got %s", status.Status)
	}
}

func TestGetPodStatus_Ready(t *testing.T) {
	fakeClient := fake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod-ready", Namespace: "test-ns"},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{Ready: true, RestartCount: 0},
			},
		},
	})
	c := NewClientFromInterface(fakeClient, testConfig())

	status, err := c.GetPodStatus(context.Background(), "pod-ready")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.Status != types.PodStatusReady {
		t.Errorf("expected PodStatusReady, got %s", status.Status)
	}
}

func TestGetPodStatus_Failed(t *testing.T) {
	fakeClient := fake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod-failed", Namespace: "test-ns"},
		Status:     corev1.PodStatus{Phase: corev1.PodFailed},
	})
	c := NewClientFromInterface(fakeClient, testConfig())

	status, err := c.GetPodStatus(context.Background(), "pod-failed")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.Status != types.PodStatusFailed {
		t.Errorf("expected PodStatusFailed, got %s", status.Status)
	}
}

func TestGetPodStatus_Unknown(t *testing.T) {
	fakeClient := fake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod-unknown", Namespace: "test-ns"},
		Status:     corev1.PodStatus{Phase: corev1.PodUnknown},
	})
	c := NewClientFromInterface(fakeClient, testConfig())

	status, err := c.GetPodStatus(context.Background(), "pod-unknown")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.Status != types.PodStatusUnknown {
		t.Errorf("expected PodStatusUnknown, got %s", status.Status)
	}
}

func TestGetPodStatus_CrashLoopBackOff(t *testing.T) {
	// When pod phase is not explicitly set (zero value), the phase switch is a no-op
	// and the CrashLoopBackOff status set from container state is preserved.
	fakeClient := fake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod-crash", Namespace: "test-ns"},
		Status: corev1.PodStatus{
			// Phase is empty (zero value) so the switch doesn't override container status
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Ready:        false,
					RestartCount: 5,
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{
							Reason: "CrashLoopBackOff",
						},
					},
				},
			},
		},
	})
	c := NewClientFromInterface(fakeClient, testConfig())

	status, err := c.GetPodStatus(context.Background(), "pod-crash")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.Status != types.PodStatusCrashLoopBackOff {
		t.Errorf("expected PodStatusCrashLoopBackOff, got %s", status.Status)
	}
	if status.RestartCount != 5 {
		t.Errorf("expected RestartCount=5, got %d", status.RestartCount)
	}
}

func TestGetPodStatus_WithTerminatedContainer(t *testing.T) {
	fakeClient := fake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod-terminated", Namespace: "test-ns"},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Ready:        false,
					RestartCount: 2,
					State: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{
							Reason: "OOMKilled",
						},
					},
				},
			},
		},
	})
	c := NewClientFromInterface(fakeClient, testConfig())

	status, err := c.GetPodStatus(context.Background(), "pod-terminated")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(status.RestartReasons) == 0 {
		t.Error("expected restart reasons to be populated")
	}
}

func TestDeletePod(t *testing.T) {
	fakeClient := fake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod-to-delete", Namespace: "test-ns"},
	})
	c := NewClientFromInterface(fakeClient, testConfig())

	err := c.DeletePod(context.Background(), "pod-to-delete")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteService(t *testing.T) {
	fakeClient := fake.NewSimpleClientset(&corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "svc-to-delete", Namespace: "test-ns"},
	})
	c := NewClientFromInterface(fakeClient, testConfig())

	err := c.DeleteService(context.Background(), "svc-to-delete")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteSandbox(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "runtime-sandbox", Namespace: "test-ns"},
	}
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "runtime-sandbox", Namespace: "test-ns"},
	}
	fakeClient := fake.NewSimpleClientset(pod, svc)
	c := NewClientFromInterface(fakeClient, testConfig())

	runtimeInfo := &state.RuntimeInfo{
		RuntimeID:   "test-runtime",
		PodName:     "runtime-sandbox",
		ServiceName: "runtime-sandbox",
		IngressName: "runtime-sandbox",
	}

	err := c.DeleteSandbox(context.Background(), runtimeInfo)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteSandbox_NotFound(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	c := NewClientFromInterface(fakeClient, testConfig())

	runtimeInfo := &state.RuntimeInfo{
		RuntimeID:   "test-runtime",
		PodName:     "non-existent-pod",
		ServiceName: "non-existent-svc",
		IngressName: "non-existent-ing",
	}

	// Should not return error for not-found resources
	err := c.DeleteSandbox(context.Background(), runtimeInfo)
	if err != nil {
		t.Fatalf("expected no error for not-found resources, got: %v", err)
	}
}

func TestScalePodToZero(t *testing.T) {
	fakeClient := fake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod-scale", Namespace: "test-ns"},
	})
	c := NewClientFromInterface(fakeClient, testConfig())

	err := c.ScalePodToZero(context.Background(), "pod-scale")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDiscoverRuntimeBySessionID_Empty(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	c := NewClientFromInterface(fakeClient, testConfig())

	result, err := c.DiscoverRuntimeBySessionID(context.Background(), "session-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result for non-existent session, got: %+v", result)
	}
}

func TestDiscoverRuntimeBySessionID_Found(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "runtime-abc",
			Namespace: "test-ns",
			Labels: map[string]string{
				"app":        "openhands-runtime",
				"runtime-id": "runtime-abc",
				"session-id": "session-123",
			},
			CreationTimestamp: metav1.Time{Time: time.Now()},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "openhands-agent",
					Image: "test-image",
					Env: []corev1.EnvVar{
						{Name: "OH_SESSION_API_KEYS_0", Value: "test-api-key"},
					},
				},
			},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	fakeClient := fake.NewSimpleClientset(pod)
	c := NewClientFromInterface(fakeClient, testConfig())

	result, err := c.DiscoverRuntimeBySessionID(context.Background(), "session-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected runtime info, got nil")
	}
	if result.RuntimeID != "runtime-abc" {
		t.Errorf("expected RuntimeID=runtime-abc, got %s", result.RuntimeID)
	}
	if result.SessionID != "session-123" {
		t.Errorf("expected SessionID=session-123, got %s", result.SessionID)
	}
	if result.SessionAPIKey != "test-api-key" {
		t.Errorf("expected SessionAPIKey=test-api-key, got %s", result.SessionAPIKey)
	}
}

func TestDiscoverRuntimeByRuntimeID_Empty(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	c := NewClientFromInterface(fakeClient, testConfig())

	result, err := c.DiscoverRuntimeByRuntimeID(context.Background(), "runtime-xyz")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result for non-existent runtime, got: %+v", result)
	}
}

func TestDiscoverRuntimeByRuntimeID_Found(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "runtime-xyz",
			Namespace: "test-ns",
			Labels: map[string]string{
				"app":        "openhands-runtime",
				"runtime-id": "runtime-xyz",
				"session-id": "session-456",
			},
			CreationTimestamp: metav1.Time{Time: time.Now()},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "openhands-agent",
					Image: "test-image",
					Env: []corev1.EnvVar{
						{Name: "OH_SESSION_API_KEYS_0", Value: "api-key-456"},
					},
				},
			},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	fakeClient := fake.NewSimpleClientset(pod)
	c := NewClientFromInterface(fakeClient, testConfig())

	result, err := c.DiscoverRuntimeByRuntimeID(context.Background(), "runtime-xyz")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected runtime info, got nil")
	}
	if result.RuntimeID != "runtime-xyz" {
		t.Errorf("expected RuntimeID=runtime-xyz, got %s", result.RuntimeID)
	}
	if result.SessionID != "session-456" {
		t.Errorf("expected SessionID=session-456, got %s", result.SessionID)
	}
}

func TestNewClientFromInterface(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	cfg := testConfig()
	c := NewClientFromInterface(fakeClient, cfg)

	if c == nil {
		t.Fatal("expected non-nil Client")
	}
	if c.namespace != cfg.Namespace {
		t.Errorf("expected namespace %s, got %s", cfg.Namespace, c.namespace)
	}
	if c.config != cfg {
		t.Error("expected config to be set")
	}
}

func TestCreateSandbox_Basic(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	cfg := testConfig()
	c := NewClientFromInterface(fakeClient, cfg)

	req := &types.StartRequest{
		Image:     "test-image",
		SessionID: "session-create",
		Command:   types.FlexibleCommand{"/usr/local/bin/server", "--port", "60000"},
	}
	runtimeInfo := &state.RuntimeInfo{
		RuntimeID:   "rt-create",
		SessionID:   "session-create",
		PodName:     "runtime-rt-create",
		ServiceName: "runtime-rt-create",
		IngressName: "runtime-rt-create",
	}

	err := c.CreateSandbox(context.Background(), req, runtimeInfo)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateSandbox_WithSingleCommand(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	cfg := testConfig()
	c := NewClientFromInterface(fakeClient, cfg)

	req := &types.StartRequest{
		Image:     "test-image",
		SessionID: "session-single-cmd",
		Command:   types.FlexibleCommand{"single-command"},
	}
	runtimeInfo := &state.RuntimeInfo{
		RuntimeID:   "rt-single-cmd",
		SessionID:   "session-single-cmd",
		PodName:     "runtime-rt-single",
		ServiceName: "runtime-rt-single",
		IngressName: "runtime-rt-single",
	}

	err := c.CreateSandbox(context.Background(), req, runtimeInfo)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateSandbox_WithRuntimeClass(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	cfg := testConfig()
	c := NewClientFromInterface(fakeClient, cfg)

	req := &types.StartRequest{
		Image:        "test-image",
		SessionID:    "session-rc",
		Command:      types.FlexibleCommand{"/usr/local/bin/server"},
		RuntimeClass: "sysbox-runc",
	}
	runtimeInfo := &state.RuntimeInfo{
		RuntimeID:   "rt-rc",
		SessionID:   "session-rc",
		PodName:     "runtime-rt-rc",
		ServiceName: "runtime-rt-rc",
		IngressName: "runtime-rt-rc",
	}

	err := c.CreateSandbox(context.Background(), req, runtimeInfo)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateSandbox_WithImagePullSecrets(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	cfg := testConfig()
	cfg.ImagePullSecrets = []string{"my-registry-secret"}
	c := NewClientFromInterface(fakeClient, cfg)

	req := &types.StartRequest{
		Image:     "private-registry/image:latest",
		SessionID: "session-pull",
	}
	runtimeInfo := &state.RuntimeInfo{
		RuntimeID:   "rt-pull",
		SessionID:   "session-pull",
		PodName:     "runtime-rt-pull",
		ServiceName: "runtime-rt-pull",
		IngressName: "runtime-rt-pull",
	}

	err := c.CreateSandbox(context.Background(), req, runtimeInfo)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateSandbox_WithCACert(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	cfg := testConfig()
	cfg.CACertSecretName = "ca-certificates"
	cfg.CACertSecretKey = "ca-certificates.crt"
	c := NewClientFromInterface(fakeClient, cfg)

	req := &types.StartRequest{
		Image:     "test-image",
		SessionID: "session-cacert",
	}
	runtimeInfo := &state.RuntimeInfo{
		RuntimeID:   "rt-cacert",
		SessionID:   "session-cacert",
		PodName:     "runtime-rt-cacert",
		ServiceName: "runtime-rt-cacert",
		IngressName: "runtime-rt-cacert",
	}

	err := c.CreateSandbox(context.Background(), req, runtimeInfo)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateSandbox_WithEnvironment(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	cfg := testConfig()
	cfg.AppServerURL = "http://app-server.svc.cluster.local"
	cfg.AppServerPublicURL = "https://app.example.com"
	c := NewClientFromInterface(fakeClient, cfg)

	req := &types.StartRequest{
		Image:     "test-image",
		SessionID: "session-env",
		Environment: map[string]string{
			"CUSTOM_VAR": "custom_value",
		},
		ResourceFactor: 2.0,
	}
	runtimeInfo := &state.RuntimeInfo{
		RuntimeID:     "rt-env",
		SessionID:     "session-env",
		SessionAPIKey: "test-api-key",
		PodName:       "runtime-rt-env",
		ServiceName:   "runtime-rt-env",
		IngressName:   "runtime-rt-env",
	}

	err := c.CreateSandbox(context.Background(), req, runtimeInfo)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRecreatePod(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	cfg := testConfig()
	c := NewClientFromInterface(fakeClient, cfg)

	req := &types.StartRequest{
		Image:     "test-image",
		SessionID: "session-recreate",
	}
	runtimeInfo := &state.RuntimeInfo{
		RuntimeID:   "rt-recreate",
		SessionID:   "session-recreate",
		PodName:     "runtime-rt-recreate",
		ServiceName: "runtime-rt-recreate",
		IngressName: "runtime-rt-recreate",
	}

	err := c.RecreatePod(context.Background(), req, runtimeInfo)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWaitForPodReady_Timeout(t *testing.T) {
	fakeClient := fake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod-wait", Namespace: "test-ns"},
		Status:     corev1.PodStatus{Phase: corev1.PodPending},
	})
	cfg := testConfig()
	c := NewClientFromInterface(fakeClient, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := c.WaitForPodReady(ctx, "pod-wait", 50*time.Millisecond)
	if err == nil {
		t.Error("expected timeout error, got nil")
	}
}

func TestDiscoverRuntimeBySessionID_NoRuntimeIDLabel(t *testing.T) {
	// Pod exists but has no runtime-id label
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-no-rid",
			Namespace: "test-ns",
			Labels: map[string]string{
				"app":        "openhands-runtime",
				"session-id": "session-no-rid",
				// no runtime-id label
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "agent"}},
		},
	}
	fakeClient := fake.NewSimpleClientset(pod)
	c := NewClientFromInterface(fakeClient, testConfig())

	result, err := c.DiscoverRuntimeBySessionID(context.Background(), "session-no-rid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil for pod without runtime-id label, got: %+v", result)
	}
}

func TestDiscoverRuntimeByRuntimeID_NoSessionIDLabel(t *testing.T) {
	// Pod exists but has no session-id label
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-no-sid",
			Namespace: "test-ns",
			Labels: map[string]string{
				"app":        "openhands-runtime",
				"runtime-id": "rt-no-sid",
				// no session-id label
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "agent"}},
		},
	}
	fakeClient := fake.NewSimpleClientset(pod)
	c := NewClientFromInterface(fakeClient, testConfig())

	result, err := c.DiscoverRuntimeByRuntimeID(context.Background(), "rt-no-sid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil for pod without session-id label, got: %+v", result)
	}
}

func TestCreateSandbox_WithNoCommand(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	cfg := testConfig()
	c := NewClientFromInterface(fakeClient, cfg)

	req := &types.StartRequest{
		Image:     "test-image",
		SessionID: "session-no-cmd",
		// No command
	}
	runtimeInfo := &state.RuntimeInfo{
		RuntimeID:   "rt-no-cmd",
		SessionID:   "session-no-cmd",
		PodName:     "runtime-rt-no-cmd",
		ServiceName: "runtime-rt-no-cmd",
		IngressName: "runtime-rt-no-cmd",
	}

	err := c.CreateSandbox(context.Background(), req, runtimeInfo)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

