package basic

import (
	"fmt"
	"testing"
)

// TestMultipleVolumesOnePod tests mounting multiple volumes in a single pod
func TestMultipleVolumesOnePod(t *testing.T) {
	bt := NewBasicTest(t)
	defer bt.Cleanup()

	t.Log("=== Testing multiple volumes on one pod ===")

	// Create 3 PVCs
	pvcNames := bt.CreateMultiplePVCs("test-pvc-multi", 3, "multiple-volumes")
	t.Log("✓ All PVCs created and bound")

	// Create pod with all 3 volumes
	t.Log("Creating pod with 3 volumes...")
	if err := bt.CreatePod("test-pod-multi", "testdata/multiple-volumes/pod.yaml", map[string]string{
		"PVC_NAME_1": pvcNames[0],
		"PVC_NAME_2": pvcNames[1],
		"PVC_NAME_3": pvcNames[2],
	}); err != nil {
		t.Fatal(err)
	}
	t.Log("✓ Pod running with 3 volumes")

	// Verify each volume independently
	for i, mountPath := range []string{"/data1", "/data2", "/data3"} {
		t.Logf("Verifying volume %d at %s...", i+1, mountPath)
		testData := fmt.Sprintf("data%d", i+1)

		if err := bt.WriteFile("test-pod-multi", fmt.Sprintf("%s/test.txt", mountPath), testData); err != nil {
			t.Fatalf("Failed to write to %s: %v", mountPath, err)
		}

		if err := bt.VerifyFileContents("test-pod-multi", fmt.Sprintf("%s/test.txt", mountPath), testData); err != nil {
			t.Fatal(err)
		}

		t.Logf("✓ Volume %d verified", i+1)
	}

	t.Log("✓ All volumes mounted and working independently")
	t.Log("✓ Multiple volumes test passed!")
}
