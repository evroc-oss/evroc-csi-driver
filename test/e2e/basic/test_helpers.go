package basic

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/evroc-oss/evroc-csi-driver/test/e2e/framework"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BasicTest wraps common test operations and setup
type BasicTest struct {
	*framework.Framework
	t   *testing.T
	ctx context.Context
}

// NewBasicTest creates a test helper with standard setup
func NewBasicTest(t *testing.T) *BasicTest {
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}

	return &BasicTest{
		Framework: framework.NewFramework(t, "default"),
		t:         t,
		ctx:       context.Background(),
	}
}

// CreatePVCAndPod creates a PVC and Pod using manifests from testdata
func (bt *BasicTest) CreatePVCAndPod(pvcName, podName, testDataDir string, extraVars map[string]string) error {
	vars := map[string]string{
		"PVC_NAME":  pvcName,
		"POD_NAME":  podName,
		"NAMESPACE": "default",
	}
	for k, v := range extraVars {
		vars[k] = v
	}

	bt.t.Log("Creating PVC...")
	if err := bt.LoadAndApplyManifest(bt.ctx, fmt.Sprintf("testdata/%s/pvc.yaml", testDataDir), vars); err != nil {
		return fmt.Errorf("failed to create PVC: %w", err)
	}
	bt.AddPVCCleanup("default", pvcName)

	bt.t.Log("Creating pod...")
	if err := bt.LoadAndApplyManifest(bt.ctx, fmt.Sprintf("testdata/%s/pod.yaml", testDataDir), vars); err != nil {
		return fmt.Errorf("failed to create pod: %w", err)
	}
	bt.AddPodCleanup("default", podName)

	bt.t.Log("Waiting for pod to be ready...")
	if err := bt.WaitForPodReady(bt.ctx, "default", podName, 3*time.Minute); err != nil {
		return fmt.Errorf("pod not ready: %w", err)
	}

	return nil
}

// CreatePVC creates a PVC using manifest from testdata
func (bt *BasicTest) CreatePVC(pvcName, testDataDir string, extraVars map[string]string) error {
	vars := map[string]string{
		"PVC_NAME":  pvcName,
		"NAMESPACE": "default",
	}
	for k, v := range extraVars {
		vars[k] = v
	}

	bt.t.Log("Creating PVC...")
	if err := bt.LoadAndApplyManifest(bt.ctx, fmt.Sprintf("testdata/%s/pvc.yaml", testDataDir), vars); err != nil {
		return fmt.Errorf("failed to create PVC: %w", err)
	}
	bt.AddPVCCleanup("default", pvcName)

	return nil
}

// CreatePod creates a Pod using manifest from testdata
func (bt *BasicTest) CreatePod(podName, manifestPath string, extraVars map[string]string) error {
	vars := map[string]string{
		"POD_NAME":  podName,
		"NAMESPACE": "default",
	}
	for k, v := range extraVars {
		vars[k] = v
	}

	bt.t.Log("Creating pod...")
	if err := bt.LoadAndApplyManifest(bt.ctx, manifestPath, vars); err != nil {
		return fmt.Errorf("failed to create pod: %w", err)
	}
	bt.AddPodCleanup("default", podName)

	bt.t.Log("Waiting for pod to be ready...")
	if err := bt.WaitForPodReady(bt.ctx, "default", podName, 3*time.Minute); err != nil {
		return fmt.Errorf("pod not ready: %w", err)
	}

	return nil
}

// WriteFile writes data to a file in a pod
func (bt *BasicTest) WriteFile(podName, path, data string) error {
	_, err := bt.Exec(bt.ctx, "default", podName, "sh", "-c", fmt.Sprintf("echo '%s' > %s", data, path))
	return err
}

// ReadFile reads a file from a pod
func (bt *BasicTest) ReadFile(podName, path string) (string, error) {
	output, err := bt.Exec(bt.ctx, "default", podName, "cat", path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

// VerifyFileContents verifies a file contains expected data
func (bt *BasicTest) VerifyFileContents(podName, path, expected string) error {
	actual, err := bt.ReadFile(podName, path)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	if actual != expected {
		return fmt.Errorf("data mismatch: expected %q, got %q", expected, actual)
	}

	return nil
}

// VerifyReadOnly verifies that a pod cannot write to a path
func (bt *BasicTest) VerifyReadOnly(podName, path string) error {
	output, err := bt.Exec(bt.ctx, "default", podName, "sh", "-c", fmt.Sprintf("touch %s/test.txt 2>&1 || true", path))
	if err != nil {
		return fmt.Errorf("failed to execute command: %w", err)
	}

	output = strings.ToLower(output)
	if !strings.Contains(output, "read-only") && !strings.Contains(output, "readonly") {
		return fmt.Errorf("expected read-only error, got: %s", output)
	}

	return nil
}

// CreateMultiplePVCs creates multiple PVCs with sequential naming
func (bt *BasicTest) CreateMultiplePVCs(basePrefix string, count int, testDataDir string) []string {
	pvcNames := make([]string, count)
	for i := 0; i < count; i++ {
		pvcName := fmt.Sprintf("%s-%d", basePrefix, i+1)
		pvcNames[i] = pvcName
		bt.t.Logf("Creating PVC %d/%d: %s...", i+1, count, pvcName)
		if err := bt.CreatePVC(pvcName, testDataDir, nil); err != nil {
			bt.t.Fatalf("Failed to create PVC %d: %v", i+1, err)
		}
	}
	return pvcNames
}

// GetPodNode returns the node where a pod is scheduled
func (bt *BasicTest) GetPodNode(podName string) (*corev1.Node, error) {
	pod, err := bt.GetPod(bt.ctx, "default", podName)
	if err != nil {
		return nil, err
	}
	if pod.Spec.NodeName == "" {
		return nil, fmt.Errorf("pod not scheduled to any node")
	}
	return bt.GetNode(bt.ctx, pod.Spec.NodeName)
}

// GetPodZone returns the zone where a pod is scheduled
func (bt *BasicTest) GetPodZone(podName string) (string, error) {
	node, err := bt.GetPodNode(podName)
	if err != nil {
		return "", err
	}
	zone, ok := node.Labels["topology.kubernetes.io/zone"]
	if !ok {
		return "", fmt.Errorf("node %s missing zone label", node.Name)
	}
	return zone, nil
}

// VerifyAllNodesInZone verifies all nodes have the expected zone label
func (bt *BasicTest) VerifyAllNodesInZone(expectedZone string) error {
	nodes, err := bt.GetNodes(bt.ctx)
	if err != nil {
		return fmt.Errorf("failed to get nodes: %w", err)
	}
	if len(nodes) == 0 {
		return fmt.Errorf("no nodes found in cluster")
	}
	for _, node := range nodes {
		zone, ok := node.Labels["topology.kubernetes.io/zone"]
		if !ok {
			return fmt.Errorf("node %s missing topology.kubernetes.io/zone label", node.Name)
		}
		if zone != expectedZone {
			return fmt.Errorf("node %s has zone %s, expected %s", node.Name, zone, expectedZone)
		}
	}
	bt.t.Logf("✓ All %d nodes labeled with zone %s", len(nodes), expectedZone)
	return nil
}

// WaitForPodPending waits and verifies a pod remains in Pending state (negative test)
func (bt *BasicTest) WaitForPodPending(podName string, waitTime time.Duration) error {
	time.Sleep(waitTime)
	pod, err := bt.GetPod(bt.ctx, "default", podName)
	if err != nil {
		return err
	}
	if pod.Status.Phase != corev1.PodPending {
		return fmt.Errorf("expected pod to be Pending, but is: %s", pod.Status.Phase)
	}
	if pod.Spec.NodeName != "" {
		return fmt.Errorf("pod should not be scheduled, but was scheduled to: %s", pod.Spec.NodeName)
	}
	return nil
}

// VerifySchedulingFailure verifies that a pod has FailedScheduling events
func (bt *BasicTest) VerifySchedulingFailure(podName string) error {
	events, err := bt.ClientSet.CoreV1().Events("default").List(bt.ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("involvedObject.name=%s,involvedObject.namespace=default", podName),
	})
	if err != nil {
		return fmt.Errorf("failed to get events: %w", err)
	}
	for _, event := range events.Items {
		if event.Reason == "FailedScheduling" {
			bt.t.Logf("✓ Found FailedScheduling event: %s", event.Message)
			return nil
		}
	}
	return fmt.Errorf("no FailedScheduling event found")
}
