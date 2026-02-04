package k8s

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/zparnold/openhands-kubernetes-remote-runtime/pkg/config"
	"github.com/zparnold/openhands-kubernetes-remote-runtime/pkg/state"
	"github.com/zparnold/openhands-kubernetes-remote-runtime/pkg/types"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Client wraps Kubernetes client operations
type Client struct {
	clientset *kubernetes.Clientset
	config    *config.Config
	namespace string
}

// NewClient creates a new Kubernetes client
func NewClient(cfg *config.Config) (*Client, error) {
	var k8sConfig *rest.Config
	var err error

	// Try in-cluster config first
	k8sConfig, err = rest.InClusterConfig()
	if err != nil {
		// Fall back to kubeconfig
		k8sConfig, err = clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
		if err != nil {
			return nil, fmt.Errorf("failed to create kubernetes config: %w", err)
		}
	}

	clientset, err := kubernetes.NewForConfig(k8sConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return &Client{
		clientset: clientset,
		config:    cfg,
		namespace: cfg.Namespace,
	}, nil
}

// CreateSandbox creates a complete sandbox environment (pod, service, ingress)
func (c *Client) CreateSandbox(ctx context.Context, req *types.StartRequest, runtimeInfo *state.RuntimeInfo) error {
	// Create Pod
	if err := c.createPod(ctx, req, runtimeInfo); err != nil {
		return fmt.Errorf("failed to create pod: %w", err)
	}

	// Create Service
	if err := c.createService(ctx, runtimeInfo); err != nil {
		// Clean up pod on failure
		_ = c.DeletePod(ctx, runtimeInfo.PodName)
		return fmt.Errorf("failed to create service: %w", err)
	}

	// Create Ingress
	if err := c.createIngress(ctx, runtimeInfo); err != nil {
		// Clean up pod and service on failure
		_ = c.DeletePod(ctx, runtimeInfo.PodName)
		_ = c.DeleteService(ctx, runtimeInfo.ServiceName)
		return fmt.Errorf("failed to create ingress: %w", err)
	}

	return nil
}

func (c *Client) createPod(ctx context.Context, req *types.StartRequest, runtimeInfo *state.RuntimeInfo) error {
	labels := map[string]string{
		"app":        "openhands-runtime",
		"runtime-id": runtimeInfo.RuntimeID,
		"session-id": runtimeInfo.SessionID,
	}

	// Build environment variables
	envVars := []corev1.EnvVar{
		{Name: "OH_SESSION_API_KEYS_0", Value: runtimeInfo.SessionAPIKey},
		{Name: "OH_VSCODE_PORT", Value: fmt.Sprintf("%d", c.config.VSCodePort)},
		{Name: "WORKER_1", Value: fmt.Sprintf("%d", c.config.Worker1Port)},
		{Name: "WORKER_2", Value: fmt.Sprintf("%d", c.config.Worker2Port)},
	}

	// Add webhook URL if app server URL is configured
	if c.config.AppServerURL != "" {
		webhookURL := fmt.Sprintf("%s/api/v1/webhooks", c.config.AppServerURL)
		envVars = append(envVars, corev1.EnvVar{
			Name:  "OH_WEBHOOKS_0_BASE_URL",
			Value: webhookURL,
		})
	}

	// Add CORS origins if app server public URL is configured
	if c.config.AppServerPublicURL != "" {
		envVars = append(envVars, corev1.EnvVar{
			Name:  "OH_ALLOW_CORS_ORIGINS_0",
			Value: c.config.AppServerPublicURL,
		})
	}

	// Add custom environment variables from request
	for key, value := range req.Environment {
		envVars = append(envVars, corev1.EnvVar{
			Name:  key,
			Value: value,
		})
	}

	// Parse command
	var command []string
	var args []string
	if req.Command != "" {
		parts := strings.Fields(req.Command)
		if len(parts) > 0 {
			command = []string{"/bin/bash", "-c"}
			args = []string{req.Command}
		}
	}

	// Set resource requests/limits based on resource_factor
	resourceFactor := req.ResourceFactor
	if resourceFactor == 0 {
		resourceFactor = 1.0
	}

	cpuRequest := fmt.Sprintf("%.0fm", 1000*resourceFactor)
	memoryRequest := fmt.Sprintf("%.0fMi", 2048*resourceFactor)
	cpuLimit := fmt.Sprintf("%.0fm", 2000*resourceFactor)
	memoryLimit := fmt.Sprintf("%.0fMi", 4096*resourceFactor)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      runtimeInfo.PodName,
			Namespace: c.namespace,
			Labels:    labels,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:            "openhands-agent",
					Image:           req.Image,
					Command:         command,
					Args:            args,
					WorkingDir:      req.WorkingDir,
					Env:             envVars,
					ImagePullPolicy: corev1.PullIfNotPresent,
					Ports: []corev1.ContainerPort{
						//nolint:gosec // Port values are validated to be in valid range (1-65535)
						{ContainerPort: int32(c.config.AgentServerPort), Name: "agent", Protocol: corev1.ProtocolTCP},
						//nolint:gosec // Port values are validated to be in valid range (1-65535)
						{ContainerPort: int32(c.config.VSCodePort), Name: "vscode", Protocol: corev1.ProtocolTCP},
						//nolint:gosec // Port values are validated to be in valid range (1-65535)
						{ContainerPort: int32(c.config.Worker1Port), Name: "worker1", Protocol: corev1.ProtocolTCP},
						//nolint:gosec // Port values are validated to be in valid range (1-65535)
						{ContainerPort: int32(c.config.Worker2Port), Name: "worker2", Protocol: corev1.ProtocolTCP},
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse(cpuRequest),
							corev1.ResourceMemory: resource.MustParse(memoryRequest),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse(cpuLimit),
							corev1.ResourceMemory: resource.MustParse(memoryLimit),
						},
					},
					ReadinessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Path: "/alive",
								Port: intstr.FromInt(c.config.AgentServerPort),
							},
						},
						InitialDelaySeconds: 5,
						PeriodSeconds:       5,
						TimeoutSeconds:      3,
						SuccessThreshold:    1,
						FailureThreshold:    3,
					},
				},
			},
			RestartPolicy: corev1.RestartPolicyAlways,
		},
	}

	// Set runtime class if specified
	if req.RuntimeClass != "" {
		pod.Spec.RuntimeClassName = &req.RuntimeClass
	}

	_, err := c.clientset.CoreV1().Pods(c.namespace).Create(ctx, pod, metav1.CreateOptions{})
	return err
}

func (c *Client) createService(ctx context.Context, runtimeInfo *state.RuntimeInfo) error {
	labels := map[string]string{
		"app":        "openhands-runtime",
		"runtime-id": runtimeInfo.RuntimeID,
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      runtimeInfo.ServiceName,
			Namespace: c.namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"runtime-id": runtimeInfo.RuntimeID,
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "agent",
					Port:       int32(c.config.AgentServerPort),
					TargetPort: intstr.FromInt(c.config.AgentServerPort),
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "vscode",
					Port:       int32(c.config.VSCodePort),
					TargetPort: intstr.FromInt(c.config.VSCodePort),
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "worker1",
					Port:       int32(c.config.Worker1Port),
					TargetPort: intstr.FromInt(c.config.Worker1Port),
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "worker2",
					Port:       int32(c.config.Worker2Port),
					TargetPort: intstr.FromInt(c.config.Worker2Port),
					Protocol:   corev1.ProtocolTCP,
				},
			},
			Type: corev1.ServiceTypeClusterIP,
		},
	}

	_, err := c.clientset.CoreV1().Services(c.namespace).Create(ctx, service, metav1.CreateOptions{})
	return err
}

func (c *Client) createIngress(ctx context.Context, runtimeInfo *state.RuntimeInfo) error {
	labels := map[string]string{
		"app":        "openhands-runtime",
		"runtime-id": runtimeInfo.RuntimeID,
	}

	pathTypePrefix := networkingv1.PathTypePrefix
	ingressClassName := c.config.IngressClass

	// Create ingress for agent server (main subdomain)
	agentHost := fmt.Sprintf("%s.%s", runtimeInfo.SessionID, c.config.BaseDomain)

	// Create ingress for vscode (vscode- prefix)
	vscodeHost := fmt.Sprintf("vscode-%s.%s", runtimeInfo.SessionID, c.config.BaseDomain)

	// Create ingress for workers
	worker1Host := fmt.Sprintf("work-1-%s.%s", runtimeInfo.SessionID, c.config.BaseDomain)
	worker2Host := fmt.Sprintf("work-2-%s.%s", runtimeInfo.SessionID, c.config.BaseDomain)

	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      runtimeInfo.IngressName,
			Namespace: c.namespace,
			Labels:    labels,
			Annotations: map[string]string{
				"nginx.ingress.kubernetes.io/ssl-redirect":       "true",
				"nginx.ingress.kubernetes.io/websocket-services": runtimeInfo.ServiceName,
			},
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: &ingressClassName,
			Rules: []networkingv1.IngressRule{
				// Agent server rule
				{
					Host: agentHost,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: &pathTypePrefix,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: runtimeInfo.ServiceName,
											Port: networkingv1.ServiceBackendPort{
												Number: int32(c.config.AgentServerPort),
											},
										},
									},
								},
							},
						},
					},
				},
				// VSCode rule
				{
					Host: vscodeHost,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: &pathTypePrefix,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: runtimeInfo.ServiceName,
											Port: networkingv1.ServiceBackendPort{
												Number: int32(c.config.VSCodePort),
											},
										},
									},
								},
							},
						},
					},
				},
				// Worker 1 rule
				{
					Host: worker1Host,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: &pathTypePrefix,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: runtimeInfo.ServiceName,
											Port: networkingv1.ServiceBackendPort{
												Number: int32(c.config.Worker1Port),
											},
										},
									},
								},
							},
						},
					},
				},
				// Worker 2 rule
				{
					Host: worker2Host,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: &pathTypePrefix,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: runtimeInfo.ServiceName,
											Port: networkingv1.ServiceBackendPort{
												Number: int32(c.config.Worker2Port),
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	_, err := c.clientset.NetworkingV1().Ingresses(c.namespace).Create(ctx, ingress, metav1.CreateOptions{})
	return err
}

// GetPodStatus retrieves the current status of a pod
func (c *Client) GetPodStatus(ctx context.Context, podName string) (*PodStatusInfo, error) {
	pod, err := c.clientset.CoreV1().Pods(c.namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return &PodStatusInfo{
				Status: types.PodStatusNotFound,
			}, nil
		}
		return nil, err
	}

	status := types.PodStatusPending
	restartCount := 0
	restartReasons := []string{}

	// Check container statuses
	for _, containerStatus := range pod.Status.ContainerStatuses {
		restartCount += int(containerStatus.RestartCount)

		if containerStatus.State.Waiting != nil {
			if containerStatus.State.Waiting.Reason == "CrashLoopBackOff" {
				status = types.PodStatusCrashLoopBackOff
			}
			restartReasons = append(restartReasons, containerStatus.State.Waiting.Reason)
		}

		if containerStatus.State.Terminated != nil {
			restartReasons = append(restartReasons, containerStatus.State.Terminated.Reason)
		}
	}

	// Determine pod status
	switch pod.Status.Phase {
	case corev1.PodPending:
		status = types.PodStatusPending
	case corev1.PodRunning:
		// Check if all containers are ready
		allReady := true
		for _, containerStatus := range pod.Status.ContainerStatuses {
			if !containerStatus.Ready {
				allReady = false
				break
			}
		}
		if allReady && len(pod.Status.ContainerStatuses) > 0 {
			status = types.PodStatusReady
		} else {
			status = types.PodStatusRunning
		}
	case corev1.PodFailed:
		status = types.PodStatusFailed
	case corev1.PodUnknown:
		status = types.PodStatusUnknown
	}

	return &PodStatusInfo{
		Status:         status,
		RestartCount:   restartCount,
		RestartReasons: restartReasons,
	}, nil
}

// PodStatusInfo contains pod status information
type PodStatusInfo struct {
	Status         types.PodStatus
	RestartCount   int
	RestartReasons []string
}

// DeletePod deletes a pod
func (c *Client) DeletePod(ctx context.Context, podName string) error {
	gracePeriodSeconds := int64(0)
	deleteOptions := metav1.DeleteOptions{
		GracePeriodSeconds: &gracePeriodSeconds,
	}
	return c.clientset.CoreV1().Pods(c.namespace).Delete(ctx, podName, deleteOptions)
}

// DeleteService deletes a service
func (c *Client) DeleteService(ctx context.Context, serviceName string) error {
	return c.clientset.CoreV1().Services(c.namespace).Delete(ctx, serviceName, metav1.DeleteOptions{})
}

// DeleteIngress deletes an ingress
func (c *Client) DeleteIngress(ctx context.Context, ingressName string) error {
	return c.clientset.NetworkingV1().Ingresses(c.namespace).Delete(ctx, ingressName, metav1.DeleteOptions{})
}

// DeleteSandbox deletes all resources for a sandbox
func (c *Client) DeleteSandbox(ctx context.Context, runtimeInfo *state.RuntimeInfo) error {
	var deleteErrors []error

	// Delete in reverse order: ingress, service, pod
	if err := c.DeleteIngress(ctx, runtimeInfo.IngressName); err != nil && !errors.IsNotFound(err) {
		deleteErrors = append(deleteErrors, fmt.Errorf("failed to delete ingress: %w", err))
	}

	if err := c.DeleteService(ctx, runtimeInfo.ServiceName); err != nil && !errors.IsNotFound(err) {
		deleteErrors = append(deleteErrors, fmt.Errorf("failed to delete service: %w", err))
	}

	if err := c.DeletePod(ctx, runtimeInfo.PodName); err != nil && !errors.IsNotFound(err) {
		deleteErrors = append(deleteErrors, fmt.Errorf("failed to delete pod: %w", err))
	}

	if len(deleteErrors) > 0 {
		return fmt.Errorf("errors deleting sandbox: %v", deleteErrors)
	}

	return nil
}

// ScalePodToZero scales the pod to zero replicas (pause simulation)
func (c *Client) ScalePodToZero(ctx context.Context, podName string) error {
	// For now, we'll just delete the pod for pause
	// A more sophisticated approach would use deployments/statefulsets
	return c.DeletePod(ctx, podName)
}

// RecreatePod recreates a pod (resume simulation)
func (c *Client) RecreatePod(ctx context.Context, req *types.StartRequest, runtimeInfo *state.RuntimeInfo) error {
	return c.createPod(ctx, req, runtimeInfo)
}

// WaitForPodReady waits for a pod to become ready
func (c *Client) WaitForPodReady(ctx context.Context, podName string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for pod to be ready")
		case <-ticker.C:
			statusInfo, err := c.GetPodStatus(ctx, podName)
			if err != nil {
				return err
			}

			if statusInfo.Status == types.PodStatusReady {
				return nil
			}

			if statusInfo.Status == types.PodStatusFailed || statusInfo.Status == types.PodStatusCrashLoopBackOff {
				return fmt.Errorf("pod failed with status: %s", statusInfo.Status)
			}
		}
	}
}
