package nodescore

import (
	"context"
	"sort"

	"github.com/zparnold/openhands-kubernetes-remote-runtime/pkg/logger"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	metricsv1beta1 "k8s.io/metrics/pkg/client/clientset/versioned/typed/metrics/v1beta1"
)

// NodeScore holds a node's name and utilization metrics.
type NodeScore struct {
	Name       string
	CPUPercent float64 // 0.0–1.0
	MemPercent float64 // 0.0–1.0
	Score      float64 // lower is better
}

// Scorer evaluates cluster nodes and picks the least loaded one.
type Scorer struct {
	metricsClient metricsv1beta1.NodeMetricsInterface
	nodeClient    corev1client.NodeInterface
	cpuThreshold  float64 // 0.0–1.0
	memThreshold  float64 // 0.0–1.0
	labelSelector string
}

// NewScorer creates a scorer with the given thresholds (as percentages 0–100).
func NewScorer(
	metricsClient metricsv1beta1.NodeMetricsInterface,
	nodeClient corev1client.NodeInterface,
	cpuThresholdPct int,
	memThresholdPct int,
	labelSelector string,
) *Scorer {
	return &Scorer{
		metricsClient: metricsClient,
		nodeClient:    nodeClient,
		cpuThreshold:  float64(cpuThresholdPct) / 100.0,
		memThreshold:  float64(memThresholdPct) / 100.0,
		labelSelector: labelSelector,
	}
}

// SelectNode returns the name of the least loaded eligible node, or "" if
// scoring should be skipped (metrics unavailable, no eligible nodes, etc.).
// Errors are logged but never returned — the caller falls back to the default scheduler.
func (s *Scorer) SelectNode(ctx context.Context) string {
	// Fetch node metrics
	metricsList, err := s.metricsClient.List(ctx, metav1.ListOptions{})
	if err != nil {
		logger.Debug("Node scoring: failed to list node metrics: %v", err)
		return ""
	}

	// Fetch nodes (with optional label selector) for allocatable resources
	listOpts := metav1.ListOptions{}
	if s.labelSelector != "" {
		listOpts.LabelSelector = s.labelSelector
	}
	nodeList, err := s.nodeClient.List(ctx, listOpts)
	if err != nil {
		logger.Debug("Node scoring: failed to list nodes: %v", err)
		return ""
	}

	// Build lookup: node name → allocatable resources
	type allocatable struct {
		cpuMillis int64
		memBytes  int64
	}
	nodeAlloc := make(map[string]allocatable, len(nodeList.Items))
	for i := range nodeList.Items {
		node := &nodeList.Items[i]
		if node.Spec.Unschedulable {
			continue
		}
		// Skip nodes with NoSchedule taints
		if hasNoScheduleTaint(node) {
			continue
		}
		cpu := node.Status.Allocatable.Cpu()
		mem := node.Status.Allocatable.Memory()
		if cpu != nil && mem != nil {
			nodeAlloc[node.Name] = allocatable{
				cpuMillis: cpu.MilliValue(),
				memBytes:  mem.Value(),
			}
		}
	}

	// Score each node
	var scores []NodeScore
	for i := range metricsList.Items {
		m := &metricsList.Items[i]
		alloc, ok := nodeAlloc[m.Name]
		if !ok || alloc.cpuMillis == 0 || alloc.memBytes == 0 {
			continue
		}

		cpuUsage := m.Usage.Cpu().MilliValue()
		memUsage := m.Usage.Memory().Value()

		cpuPct := float64(cpuUsage) / float64(alloc.cpuMillis)
		memPct := float64(memUsage) / float64(alloc.memBytes)

		// Exclude overloaded nodes
		if cpuPct > s.cpuThreshold || memPct > s.memThreshold {
			continue
		}

		scores = append(scores, NodeScore{
			Name:       m.Name,
			CPUPercent: cpuPct,
			MemPercent: memPct,
			Score:      (cpuPct + memPct) / 2.0,
		})
	}

	if len(scores) == 0 {
		logger.Debug("Node scoring: no eligible nodes found")
		return ""
	}

	sort.Slice(scores, func(i, j int) bool {
		return scores[i].Score < scores[j].Score
	})

	selected := scores[0]
	logger.Info("Node scoring: selected %s (cpu=%.0f%% mem=%.0f%% score=%.2f) from %d eligible nodes",
		selected.Name, selected.CPUPercent*100, selected.MemPercent*100, selected.Score, len(scores))
	return selected.Name
}

// ApplyNodePreference sets a preferred node affinity on the pod spec.
// Uses preferredDuringSchedulingIgnoredDuringExecution so the scheduler
// can fall back to other nodes if the preferred one becomes unavailable.
func ApplyNodePreference(pod *corev1.Pod, nodeName string) {
	if nodeName == "" {
		return
	}
	pod.Spec.Affinity = &corev1.Affinity{
		NodeAffinity: &corev1.NodeAffinity{
			PreferredDuringSchedulingIgnoredDuringExecution: []corev1.PreferredSchedulingTerm{
				{
					Weight: 100,
					Preference: corev1.NodeSelectorTerm{
						MatchExpressions: []corev1.NodeSelectorRequirement{
							{
								Key:      "kubernetes.io/hostname",
								Operator: corev1.NodeSelectorOpIn,
								Values:   []string{nodeName},
							},
						},
					},
				},
			},
		},
	}
}

func hasNoScheduleTaint(node *corev1.Node) bool {
	for _, taint := range node.Spec.Taints {
		if taint.Effect == corev1.TaintEffectNoSchedule {
			return true
		}
	}
	return false
}
