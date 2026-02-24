package k8s

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/zparnold/openhands-kubernetes-remote-runtime/pkg/config"
	"github.com/zparnold/openhands-kubernetes-remote-runtime/pkg/logger"
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
	clientset kubernetes.Interface
	config    *config.Config
	namespace string
}

// NewClient creates a new Kubernetes client
func NewClient(cfg *config.Config) (*Client, error) {
	var k8sConfig *rest.Config
	var err error

	logger.Debug("NewClient: Initializing Kubernetes client")

	// Try in-cluster config first
	k8sConfig, err = rest.InClusterConfig()
	if err != nil {
		logger.Debug("NewClient: In-cluster config not available, falling back to kubeconfig")
		// Fall back to kubeconfig
		k8sConfig, err = clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
		if err != nil {
			return nil, fmt.Errorf("failed to create kubernetes config: %w", err)
		}
	} else {
		logger.Debug("NewClient: Using in-cluster configuration")
	}

	clientset, err := kubernetes.NewForConfig(k8sConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	logger.Debug("NewClient: Kubernetes client created successfully for namespace %s", cfg.Namespace)

	return &Client{
		clientset: clientset,
		config:    cfg,
		namespace: cfg.Namespace,
	}, nil
}

// NewClientFromInterface creates a Client using an existing kubernetes.Interface.
// Intended for use in tests where a fake clientset is injected.
func NewClientFromInterface(clientset kubernetes.Interface, cfg *config.Config) *Client {
	return &Client{
		clientset: clientset,
		config:    cfg,
		namespace: cfg.Namespace,
	}
}

// portToInt32 converts a port number to int32 for Kubernetes APIs.
// Valid port range is 1-65535; values outside this range are clamped to avoid overflow (gosec G115).
func portToInt32(port int) int32 {
	if port < 1 {
		return 1
	}
	if port > 65535 {
		return 65535
	}
	return int32(port)
}

// CreateSandbox creates a complete sandbox environment (pod, service, ingress)
func (c *Client) CreateSandbox(ctx context.Context, req *types.StartRequest, runtimeInfo *state.RuntimeInfo) error {
	logger.Debug("CreateSandbox: Creating sandbox for runtime %s", runtimeInfo.RuntimeID)

	// Create Pod
	logger.Debug("CreateSandbox: Creating pod %s", runtimeInfo.PodName)
	if err := c.createPod(ctx, req, runtimeInfo); err != nil {
		return fmt.Errorf("failed to create pod: %w", err)
	}
	logger.Debug("CreateSandbox: Pod created successfully")

	// Create Service
	logger.Debug("CreateSandbox: Creating service %s", runtimeInfo.ServiceName)
	if err := c.createService(ctx, runtimeInfo); err != nil {
		// Clean up pod on failure
		_ = c.DeletePod(ctx, runtimeInfo.PodName)
		return fmt.Errorf("failed to create service: %w", err)
	}
	logger.Debug("CreateSandbox: Service created successfully")

	// Create Ingress
	logger.Debug("CreateSandbox: Creating ingress %s", runtimeInfo.IngressName)
	if err := c.createIngress(ctx, runtimeInfo); err != nil {
		// Clean up pod and service on failure
		_ = c.DeletePod(ctx, runtimeInfo.PodName)
		_ = c.DeleteService(ctx, runtimeInfo.ServiceName)
		return fmt.Errorf("failed to create ingress: %w", err)
	}
	logger.Debug("CreateSandbox: Ingress created successfully")

	logger.Debug("CreateSandbox: Sandbox created successfully for runtime %s", runtimeInfo.RuntimeID)
	return nil
}

func (c *Client) createPod(ctx context.Context, req *types.StartRequest, runtimeInfo *state.RuntimeInfo) error {
	labels := map[string]string{
		"app":        "openhands-runtime",
		"runtime-id": runtimeInfo.RuntimeID,
		"session-id": runtimeInfo.SessionID,
	}

	// Build environment variables.
	// Set both OH_SESSION_API_KEYS_0 (app_server convention) and SESSION_API_KEY
	// (agent server / action_execution_server and webhook client may read either).
	envVars := []corev1.EnvVar{
		{Name: "OH_SESSION_API_KEYS_0", Value: runtimeInfo.SessionAPIKey},
		{Name: "SESSION_API_KEY", Value: runtimeInfo.SessionAPIKey},
		{Name: "OH_RUNTIME_ID", Value: runtimeInfo.RuntimeID},
		{Name: "OH_VSCODE_PORT", Value: fmt.Sprintf("%d", c.config.VSCodePort)},
		{Name: "WORKER_1", Value: fmt.Sprintf("%d", c.config.Worker1Port)},
		{Name: "WORKER_2", Value: fmt.Sprintf("%d", c.config.Worker2Port)},
	}
	// If custom CA certificate is mounted, point Python/httpx at the system bundle.
	// The entrypoint runs update-ca-certificates, which merges the mounted cert
	// into /etc/ssl/certs/ca-certificates.crt. Use that merged bundle so both
	// system CAs (e.g. for Azure LLM) and the corporate CA are trusted.
	if c.config.CACertSecretName != "" {
		envVars = append(envVars, corev1.EnvVar{
			Name:  "SSL_CERT_FILE",
			Value: "/etc/ssl/certs/ca-certificates.crt",
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

	// Add webhook URL if app server URL is configured.
	// This is set AFTER custom env vars so the runtime API's internal
	// cluster URL overrides the app-server's external URL. In Kubernetes,
	// when duplicate env var names exist the last one wins.
	if c.config.AppServerURL != "" {
		webhookURL := fmt.Sprintf("%s/api/v1/webhooks", c.config.AppServerURL)
		envVars = append(envVars, corev1.EnvVar{
			Name:  "OH_WEBHOOKS_0_BASE_URL",
			Value: webhookURL,
		})
	}

	// Use image ENTRYPOINT (e.g. /openhands/entrypoint.sh for update-ca-certificates)
	// and pass request command as Args so the entrypoint receives them as "$@".
	// If we set Command we would replace the image ENTRYPOINT and the entrypoint would never run.
	var command []string
	var args []string
	if len(req.Command) > 1 {
		command = nil
		args = []string(req.Command)
	} else if len(req.Command) == 1 && req.Command[0] != "" {
		// Single string: run via bash -c (no image entrypoint)
		command = []string{"/bin/bash", "-c"}
		args = []string{req.Command[0]}
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
					ImagePullPolicy: corev1.PullAlways,
					Ports: []corev1.ContainerPort{
						//nolint:gosec // Port values are validated to be in valid range (1-65535)
						{ContainerPort: portToInt32(c.config.AgentServerPort), Name: "agent", Protocol: corev1.ProtocolTCP},
						//nolint:gosec // Port values are validated to be in valid range (1-65535)
						{ContainerPort: portToInt32(c.config.VSCodePort), Name: "vscode", Protocol: corev1.ProtocolTCP},
						//nolint:gosec // Port values are validated to be in valid range (1-65535)
						{ContainerPort: portToInt32(c.config.Worker1Port), Name: "worker1", Protocol: corev1.ProtocolTCP},
						//nolint:gosec // Port values are validated to be in valid range (1-65535)
						{ContainerPort: portToInt32(c.config.Worker2Port), Name: "worker2", Protocol: corev1.ProtocolTCP},
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
						PeriodSeconds:       10,
						TimeoutSeconds:      10,
						SuccessThreshold:    1,
						FailureThreshold:    6,
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

	// Set image pull secrets when using a private registry
	if len(c.config.ImagePullSecrets) > 0 {
		pod.Spec.ImagePullSecrets = make([]corev1.LocalObjectReference, 0, len(c.config.ImagePullSecrets))
		for _, name := range c.config.ImagePullSecrets {
			pod.Spec.ImagePullSecrets = append(pod.Spec.ImagePullSecrets, corev1.LocalObjectReference{Name: name})
		}
	}

	// Mount optional CA certificate for sandbox pods (e.g. corporate/proxy CAs).
	// The runtime image runs update-ca-certificates at startup, which merges certs
	// from /usr/local/share/ca-certificates/*.crt into the system trust store.
	if c.config.CACertSecretName != "" {
		secretKey := c.config.CACertSecretKey
		if secretKey == "" {
			secretKey = "ca-certificates.crt"
		}
		const caCertMountPath = "/usr/local/share/ca-certificates/additional-ca.crt"
		vol := corev1.Volume{
			Name: "ca-certificates",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: c.config.CACertSecretName,
				},
			},
		}
		pod.Spec.Volumes = append(pod.Spec.Volumes, vol)
		pod.Spec.Containers[0].VolumeMounts = append(pod.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{
			Name:      c.config.CACertSecretName,
			MountPath: caCertMountPath,
			SubPath:   secretKey,
			ReadOnly:  true,
		})
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
					Name: "agent",
					//nolint:gosec // Port values are validated to be in valid range (1-65535)
					Port:       portToInt32(c.config.AgentServerPort),
					TargetPort: intstr.FromInt(c.config.AgentServerPort),
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name: "vscode",
					//nolint:gosec // Port values are validated to be in valid range (1-65535)
					Port:       portToInt32(c.config.VSCodePort),
					TargetPort: intstr.FromInt(c.config.VSCodePort),
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name: "worker1",
					//nolint:gosec // Port values are validated to be in valid range (1-65535)
					Port:       portToInt32(c.config.Worker1Port),
					TargetPort: intstr.FromInt(c.config.Worker1Port),
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "worker2",
					Port:       portToInt32(c.config.Worker2Port),
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

	// Ingress hostnames must be RFC 1123 subdomains (lowercase alphanumeric, '-' or '.')
	sessionIDForHost := strings.ToLower(runtimeInfo.SessionID)
	// Create ingress for agent server (main subdomain)
	agentHost := fmt.Sprintf("%s.%s", sessionIDForHost, c.config.BaseDomain)
	// Create ingress for vscode (vscode- prefix)
	vscodeHost := fmt.Sprintf("vscode-%s.%s", sessionIDForHost, c.config.BaseDomain)
	// Create ingress for workers
	worker1Host := fmt.Sprintf("work-1-%s.%s", sessionIDForHost, c.config.BaseDomain)
	worker2Host := fmt.Sprintf("work-2-%s.%s", sessionIDForHost, c.config.BaseDomain)

	annotations := map[string]string{
		"nginx.ingress.kubernetes.io/ssl-redirect":       "true",
		"nginx.ingress.kubernetes.io/websocket-services": runtimeInfo.ServiceName,
	}
	for k, v := range c.config.SandboxIngressAnnotations {
		annotations[k] = v
	}
	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        runtimeInfo.IngressName,
			Namespace:   c.namespace,
			Labels:      labels,
			Annotations: annotations,
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
												Number: portToInt32(c.config.AgentServerPort),
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
												Number: portToInt32(c.config.VSCodePort),
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
												Number: portToInt32(c.config.Worker1Port),
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
												Number: portToInt32(c.config.Worker2Port),
											},
										},
									},
								},
							},
						},
					},
				},
			},
			TLS: []networkingv1.IngressTLS{
				{
					Hosts:      []string{agentHost, vscodeHost, worker1Host, worker2Host},
					SecretName: fmt.Sprintf("runtime-%s-tls", runtimeInfo.RuntimeID),
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
	logger.Debug("DeleteSandbox: Deleting sandbox for runtime %s", runtimeInfo.RuntimeID)
	var deleteErrors []error

	// Delete in reverse order: ingress, service, pod
	logger.Debug("DeleteSandbox: Deleting ingress %s", runtimeInfo.IngressName)
	if err := c.DeleteIngress(ctx, runtimeInfo.IngressName); err != nil && !errors.IsNotFound(err) {
		deleteErrors = append(deleteErrors, fmt.Errorf("failed to delete ingress: %w", err))
		logger.Info("DeleteSandbox: Error deleting ingress: %v", err)
	}

	logger.Debug("DeleteSandbox: Deleting service %s", runtimeInfo.ServiceName)
	if err := c.DeleteService(ctx, runtimeInfo.ServiceName); err != nil && !errors.IsNotFound(err) {
		deleteErrors = append(deleteErrors, fmt.Errorf("failed to delete service: %w", err))
		logger.Info("DeleteSandbox: Error deleting service: %v", err)
	}

	logger.Debug("DeleteSandbox: Deleting pod %s", runtimeInfo.PodName)
	if err := c.DeletePod(ctx, runtimeInfo.PodName); err != nil && !errors.IsNotFound(err) {
		deleteErrors = append(deleteErrors, fmt.Errorf("failed to delete pod: %w", err))
		logger.Info("DeleteSandbox: Error deleting pod: %v", err)
	}

	if len(deleteErrors) > 0 {
		return fmt.Errorf("errors deleting sandbox: %v", deleteErrors)
	}

	logger.Debug("DeleteSandbox: Sandbox deleted successfully for runtime %s", runtimeInfo.RuntimeID)
	return nil
}

// ScalePodToZero scales the pod to zero replicas (pause simulation)
func (c *Client) ScalePodToZero(ctx context.Context, podName string) error {
	logger.Debug("ScalePodToZero: Scaling pod %s to zero", podName)
	// For now, we'll just delete the pod for pause
	// A more sophisticated approach would use deployments/statefulsets
	return c.DeletePod(ctx, podName)
}

// RecreatePod recreates a pod (resume simulation)
func (c *Client) RecreatePod(ctx context.Context, req *types.StartRequest, runtimeInfo *state.RuntimeInfo) error {
	logger.Debug("RecreatePod: Recreating pod %s", runtimeInfo.PodName)
	return c.createPod(ctx, req, runtimeInfo)
}

// buildRuntimeInfoFromPod reconstructs RuntimeInfo from a sandbox pod. Used by discovery functions.
func (c *Client) buildRuntimeInfoFromPod(ctx context.Context, pod *corev1.Pod, runtimeID, sessionID string) *state.RuntimeInfo {
	sessionAPIKey := ""
	for _, env := range pod.Spec.Containers[0].Env {
		if env.Name == "OH_SESSION_API_KEYS_0" {
			sessionAPIKey = env.Value
			break
		}
	}
	sessionIDForHost := strings.ToLower(sessionID)
	baseURL := fmt.Sprintf("https://%s.%s", sessionIDForHost, c.config.BaseDomain)
	workHosts := map[string]int{
		fmt.Sprintf("https://work-1-%s.%s", sessionIDForHost, c.config.BaseDomain): c.config.Worker1Port,
		fmt.Sprintf("https://work-2-%s.%s", sessionIDForHost, c.config.BaseDomain): c.config.Worker2Port,
	}
	statusInfo, err := c.GetPodStatus(ctx, pod.Name)
	podStatus := types.PodStatusUnknown
	restartCount := 0
	restartReasons := []string{}
	if err == nil {
		podStatus = statusInfo.Status
		restartCount = statusInfo.RestartCount
		restartReasons = statusInfo.RestartReasons
	}
	// Use the pod's actual creation time so that cleanup thresholds are measured
	// from when the pod was originally created, not from when it was discovered.
	// This prevents discovered pods from being immediately reaped as idle
	// (zero-value CreatedAt/LastActivityTime would look like 2000+ years idle).
	createdAt := pod.CreationTimestamp.Time
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	return &state.RuntimeInfo{
		RuntimeID:        runtimeID,
		SessionID:        sessionID,
		URL:              baseURL,
		SessionAPIKey:    sessionAPIKey,
		Status:           types.StatusRunning,
		PodStatus:        podStatus,
		WorkHosts:        workHosts,
		PodName:          pod.Name,
		ServiceName:      pod.Name,
		IngressName:      pod.Name,
		RestartCount:     restartCount,
		RestartReasons:   restartReasons,
		CreatedAt:        createdAt,
		LastActivityTime: time.Now(),
	}
}

// DiscoverRuntimeBySessionID finds a running sandbox pod by session-id label and
// reconstructs RuntimeInfo. Used when in-memory state was lost (e.g. runtime API restart).
// Returns nil if no matching pod exists.
//
//nolint:dupl // Mirrors DiscoverRuntimeByRuntimeID; differs only in selector and label extraction
func (c *Client) DiscoverRuntimeBySessionID(ctx context.Context, sessionID string) (*state.RuntimeInfo, error) {
	selector := fmt.Sprintf("app=openhands-runtime,session-id=%s", sessionID)
	list, err := c.clientset.CoreV1().Pods(c.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		return nil, fmt.Errorf("list pods: %w", err)
	}
	if len(list.Items) == 0 {
		return nil, nil
	}
	pod := &list.Items[0]
	runtimeID, ok := pod.Labels["runtime-id"]
	if !ok || runtimeID == "" {
		return nil, nil
	}
	if len(pod.Spec.Containers) == 0 {
		return nil, nil
	}
	return c.buildRuntimeInfoFromPod(ctx, pod, runtimeID, sessionID), nil
}

// DiscoverRuntimeByRuntimeID finds a sandbox pod by runtime-id label and
// reconstructs RuntimeInfo. Used when in-memory state was lost (e.g. runtime API restart).
// Returns nil if no matching pod exists.
//
//nolint:dupl // Mirrors DiscoverRuntimeBySessionID; differs only in selector and label extraction
func (c *Client) DiscoverRuntimeByRuntimeID(ctx context.Context, runtimeID string) (*state.RuntimeInfo, error) {
	selector := fmt.Sprintf("app=openhands-runtime,runtime-id=%s", runtimeID)
	list, err := c.clientset.CoreV1().Pods(c.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		return nil, fmt.Errorf("list pods: %w", err)
	}
	if len(list.Items) == 0 {
		return nil, nil
	}
	pod := &list.Items[0]
	sessionID, ok := pod.Labels["session-id"]
	if !ok || sessionID == "" {
		return nil, nil
	}
	if len(pod.Spec.Containers) == 0 {
		return nil, nil
	}
	return c.buildRuntimeInfoFromPod(ctx, pod, runtimeID, sessionID), nil
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
