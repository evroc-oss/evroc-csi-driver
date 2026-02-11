package basic

import (
	"fmt"
	"testing"
	"time"
)

// TestPodReschedulingWithVolume tests that when a pod dies, it can only be
// rescheduled on nodes in the same zone as its attached volume.
// This verifies that volume topology constraints are respected during rescheduling.
func TestPodReschedulingWithVolume(t *testing.T) {
	bt := NewBasicTest(t)
	defer bt.Cleanup()

	t.Log("=== Testing pod rescheduling with volume topology constraints ===")

	pvcName := "test-pvc-reschedule"
	podName := "test-pod-reschedule"
	testData := fmt.Sprintf("test-data-%d", time.Now().Unix())

	// Create PVC
	if err := bt.CreatePVC(pvcName, "volume-persistence", nil); err != nil {
		t.Fatal(err)
	}

	// Create initial pod to write data and trigger volume provisioning
	t.Log("Creating initial pod...")
	if err := bt.CreatePod(podName, "testdata/volume-persistence/pod-write.yaml",
		map[string]string{"PVC_NAME": pvcName, "TEST_DATA": testData}); err != nil {
		t.Fatal(err)
	}

	// Get the zone where the pod is running
	originalZone, err := bt.GetPodZone(podName)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("✓ Pod scheduled in zone: %s", originalZone)

	// Verify data was written
	if err := bt.VerifyFileContents(podName, "/data/test.txt", testData); err != nil {
		t.Fatal(err)
	}
	t.Log("✓ Data written successfully")

	// Delete the pod to simulate pod failure
	t.Log("Deleting pod to simulate failure...")
	if err := bt.DeletePod(bt.ctx, "default", podName); err != nil {
		t.Fatalf("Failed to delete pod: %v", err)
	}

	if err := bt.WaitForPodDeleted(bt.ctx, "default", podName, 2*time.Minute); err != nil {
		t.Fatalf("Pod not deleted: %v", err)
	}
	t.Log("✓ Pod deleted")

	time.Sleep(5 * time.Second) // Wait for volume to detach

	// Recreate the pod with the same PVC
	t.Log("Recreating pod with same PVC...")
	if err := bt.CreatePod(podName, "testdata/volume-persistence/pod-read.yaml",
		map[string]string{"PVC_NAME": pvcName}); err != nil {
		t.Fatal(err)
	}

	// Get the zone of the rescheduled pod
	newZone, err := bt.GetPodZone(podName)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("✓ Pod rescheduled in zone: %s", newZone)

	// Verify the pod was scheduled in the same zone as the volume
	if newZone != originalZone {
		t.Fatalf("Pod rescheduled in different zone! Original zone: %s, New zone: %s. Volume topology constraints not respected!",
			originalZone, newZone)
	}
	t.Logf("✓ Pod correctly rescheduled in same zone as volume (zone=%s)", newZone)

	// Verify data persistence
	t.Log("Verifying data persisted after rescheduling...")
	if err := bt.VerifyFileContents(podName, "/data/test.txt", testData); err != nil {
		t.Fatal(err)
	}
	t.Log("✓ Data persisted correctly after rescheduling")

	t.Log("✓ Pod rescheduling with volume topology test passed!")
}
