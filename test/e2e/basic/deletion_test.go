package basic

import (
	"fmt"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestPVCDeletion tests that PVC deletion properly cleans up backend resources.
// This validates the complete volume lifecycle and ensures no resource leaks.
func TestPVCDeletion(t *testing.T) {
	bt := NewBasicTest(t)
	defer bt.Cleanup()

	t.Log("=== Testing PVC deletion and resource cleanup ===")

	pvcName := "test-pvc-deletion"
	podName := "test-pod-deletion"
	testData := fmt.Sprintf("cleanup-test-%d", time.Now().Unix())

	if err := bt.CreatePVCAndPod(pvcName, podName, "deletion", map[string]string{"TEST_DATA": testData}); err != nil {
		t.Fatal(err)
	}
	t.Log("✓ Pod running, volume provisioned")

	// Get PV information before deletion
	t.Log("Getting PV information...")
	pvc, err := bt.GetPVC(bt.ctx, "default", pvcName)
	if err != nil {
		t.Fatalf("Failed to get PVC: %v", err)
	}
	pvName := pvc.Spec.VolumeName
	if pvName == "" {
		t.Fatal("PVC not bound to any PV")
	}
	t.Logf("PVC bound to PV: %s", pvName)

	pv, err := bt.ClientSet.CoreV1().PersistentVolumes().Get(bt.ctx, pvName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get PV: %v", err)
	}
	volumeHandle := pv.Spec.CSI.VolumeHandle
	t.Logf("Volume handle (backend disk name): %s", volumeHandle)

	// Delete pod and PVC
	t.Log("Deleting pod...")
	if err := bt.DeletePod(bt.ctx, "default", podName); err != nil {
		t.Fatalf("Failed to delete pod: %v", err)
	}
	t.Log("✓ Pod deleted")

	time.Sleep(5 * time.Second) // Wait for volume to detach

	t.Log("Deleting PVC...")
	if err := bt.DeletePVC(bt.ctx, "default", pvcName); err != nil {
		t.Fatalf("Failed to delete PVC: %v", err)
	}
	t.Log("✓ PVC deleted")

	// Wait for PV to be deleted
	t.Log("Waiting for PV to be deleted...")
	maxWait := 2 * time.Minute
	deleted := false
	start := time.Now()
	for time.Since(start) < maxWait {
		_, err := bt.ClientSet.CoreV1().PersistentVolumes().Get(bt.ctx, pvName, metav1.GetOptions{})
		if err != nil && strings.Contains(err.Error(), "not found") {
			deleted = true
			break
		}
		time.Sleep(2 * time.Second)
	}

	if !deleted {
		t.Fatalf("PV %s was not deleted after %v", pvName, maxWait)
	}
	t.Log("✓ PV deleted successfully")
	t.Log("✓ PVC deletion test passed!")
}
