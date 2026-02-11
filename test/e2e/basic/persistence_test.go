package basic

import (
	"fmt"
	"testing"
	"time"
)

// TestVolumeDataPersistence tests that data persists across pod deletions
func TestVolumeDataPersistence(t *testing.T) {
	bt := NewBasicTest(t)
	defer bt.Cleanup()

	t.Log("=== Testing volume data persistence ===")

	testData := fmt.Sprintf("test-data-%d", time.Now().Unix())
	pvcName := "test-pvc-persistence"

	// Create PVC
	if err := bt.CreatePVC(pvcName, "volume-persistence", nil); err != nil {
		t.Fatal(err)
	}

	// Create first pod to write data
	t.Log("Creating pod to write data...")
	if err := bt.CreatePod("test-pod-write", "testdata/volume-persistence/pod-write.yaml",
		map[string]string{"PVC_NAME": pvcName, "TEST_DATA": testData}); err != nil {
		t.Fatal(err)
	}

	// Verify data was written
	if err := bt.VerifyFileContents("test-pod-write", "/data/test.txt", testData); err != nil {
		t.Fatal(err)
	}
	t.Log("✓ Data written successfully")

	// Delete first pod
	t.Log("Deleting write pod...")
	if err := bt.DeletePod(bt.ctx, "default", "test-pod-write"); err != nil {
		t.Fatalf("Failed to delete pod: %v", err)
	}
	time.Sleep(5 * time.Second)

	// Create second pod to read data
	t.Log("Creating pod to read persisted data...")
	if err := bt.CreatePod("test-pod-read", "testdata/volume-persistence/pod-read.yaml",
		map[string]string{"PVC_NAME": pvcName}); err != nil {
		t.Fatal(err)
	}

	// Verify data persisted
	if err := bt.VerifyFileContents("test-pod-read", "/data/test.txt", testData); err != nil {
		t.Fatal(err)
	}

	t.Log("✓ Data persisted across pod deletion")
	t.Log("✓ Volume data persistence test passed!")
}
