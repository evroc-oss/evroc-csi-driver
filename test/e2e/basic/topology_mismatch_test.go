package basic

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
)

// TestTopologyMismatch tests that pods with zone constraints that don't match
// available nodes remain unschedulable
func TestTopologyMismatch(t *testing.T) {
	bt := NewBasicTest(t)
	defer bt.Cleanup()

	t.Log("=== Testing topology mismatch (negative test) ===")

	wrongZone := "b"
	pvcName := "test-pvc-wrong-zone"
	podName := "test-pod-wrong-zone"

	// Create PVC
	if err := bt.CreatePVC(pvcName, "topology", nil); err != nil {
		t.Fatal(err)
	}

	// Create pod with wrong zone constraint - should remain unschedulable
	t.Logf("Creating pod with zone constraint (zone=%s) - should remain unschedulable...", wrongZone)
	if err := bt.LoadAndApplyManifest(bt.ctx, "testdata/topology/pod.yaml", map[string]string{
		"POD_NAME":  podName,
		"PVC_NAME":  pvcName,
		"NAMESPACE": "default",
		"ZONE":      wrongZone,
	}); err != nil {
		t.Fatalf("Failed to create pod: %v", err)
	}
	bt.AddPodCleanup("default", podName)

	// Wait and verify pod remains Pending
	t.Log("Waiting 30 seconds for scheduling attempts...")
	if err := bt.WaitForPodPending(podName, 30*time.Second); err != nil {
		t.Fatal(err)
	}
	t.Log("Verifying pod remains unscheduled...")

	// Check for scheduling failure events
	t.Log("Checking for FailedScheduling events...")
	if err := bt.VerifySchedulingFailure(podName); err != nil {
		t.Fatal(err)
	}

	// Verify PVC is also still Pending (WaitForFirstConsumer)
	pvc, err := bt.GetPVC(bt.ctx, "default", pvcName)
	if err != nil {
		t.Fatalf("Failed to get PVC: %v", err)
	}

	if pvc.Status.Phase != corev1.ClaimPending {
		t.Logf("Note: PVC is in state %s (expected Pending with WaitForFirstConsumer)", pvc.Status.Phase)
	} else {
		t.Log("✓ PVC correctly remains Pending (waiting for pod to be scheduled)")
	}

	t.Logf("✓ Pod correctly remains unschedulable due to zone mismatch (zone=%s)", wrongZone)
	t.Log("✓ Topology constraint enforcement test passed!")
}
