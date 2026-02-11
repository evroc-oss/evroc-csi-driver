package basic

import (
	"fmt"
	"strings"
	"testing"
)

// TestTopologyAwareness tests that volumes are created in the correct zone
func TestTopologyAwareness(t *testing.T) {
	bt := NewBasicTest(t)
	defer bt.Cleanup()

	t.Log("=== Testing topology-aware volume placement ===")

	targetZone := topologyZone

	// Verify nodes have the expected zone label
	t.Log("Verifying node topology labels...")
	if err := bt.VerifyAllNodesInZone(targetZone); err != nil {
		t.Fatal(err)
	}

	pvcName := "test-pvc-topology"
	podName := "test-pod-topology"

	// Create PVC and pod with zone constraint
	t.Logf("Creating pod with zone constraint (zone=%s)...", targetZone)
	if err := bt.CreatePVCAndPod(pvcName, podName, "topology", map[string]string{"ZONE": targetZone}); err != nil {
		t.Fatal(err)
	}

	// Verify pod is on the correct zone
	t.Log("Verifying pod placement...")
	scheduledZone, err := bt.GetPodZone(podName)
	if err != nil {
		t.Fatal(err)
	}

	if scheduledZone != targetZone {
		t.Fatalf("Pod scheduled in wrong zone. Expected: %s, Got: %s", targetZone, scheduledZone)
	}
	t.Logf("✓ Pod correctly scheduled in zone %s", scheduledZone)

	// Verify the volume is accessible
	t.Log("Verifying volume is accessible...")
	output, err := bt.ReadFile(podName, "/data/zone.txt")
	if err != nil {
		t.Fatalf("Failed to read from volume: %v", err)
	}

	expectedText := fmt.Sprintf("Running in zone %s", targetZone)
	if !strings.Contains(output, expectedText) {
		t.Fatalf("Unexpected data. Expected to find: %q, Got: %q", expectedText, output)
	}

	t.Log("✓ Volume accessible and working in correct zone")
	t.Log("✓ Topology-aware volume placement test passed!")
}
