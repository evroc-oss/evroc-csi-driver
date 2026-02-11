package mocks

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

// NewMockKubernetesClient creates a fake Kubernetes client for testing.
// It pre-populates a node with the specified zone label.
func NewMockKubernetesClient(nodeID, zone string) kubernetes.Interface {
	// Create a fake node with the zone label
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeID,
			Labels: map[string]string{
				"topology.kubernetes.io/zone": zone,
			},
		},
	}

	// Create fake clientset with the node
	return fake.NewSimpleClientset(node)
}

// NewMockKubernetesClientWithoutZone creates a fake Kubernetes client
// with a node that has NO zone label (for testing error cases).
func NewMockKubernetesClientWithoutZone(nodeID string) kubernetes.Interface {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   nodeID,
			Labels: map[string]string{},
		},
	}
	return fake.NewSimpleClientset(node)
}

// GetNodeZone is a test helper to verify zone detection.
func GetNodeZone(ctx context.Context, client kubernetes.Interface, nodeID string) (string, bool) {
	node, err := client.CoreV1().Nodes().Get(ctx, nodeID, metav1.GetOptions{})
	if err != nil {
		return "", false
	}
	zone, ok := node.Labels["topology.kubernetes.io/zone"]
	return zone, ok
}
