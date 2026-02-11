package basic

import (
	"testing"
	"time"
)

// TestReadOnlyVolume tests read-only volume enforcement
func TestReadOnlyVolume(t *testing.T) {
	bt := NewBasicTest(t)
	defer bt.Cleanup()

	t.Log("=== Testing read-only volume ===")

	testData := "readonly-test-data"
	pvcName := "test-pvc-readonly"

	// Create PVC
	if err := bt.CreatePVC(pvcName, "readonly-volume", nil); err != nil {
		t.Fatal(err)
	}

	// Create first pod to write data (read-write)
	t.Log("Creating pod to write data...")
	if err := bt.CreatePod("test-pod-write-ro", "testdata/readonly-volume/pod-write.yaml",
		map[string]string{"PVC_NAME": pvcName, "TEST_DATA": testData}); err != nil {
		t.Fatal(err)
	}

	// Verify data was written
	if err := bt.VerifyFileContents("test-pod-write-ro", "/data/readonly.txt", testData); err != nil {
		t.Fatal(err)
	}
	t.Log("✓ Data written successfully")

	// Delete write pod
	t.Log("Deleting write pod...")
	if err := bt.DeletePod(bt.ctx, "default", "test-pod-write-ro"); err != nil {
		t.Fatalf("Failed to delete pod: %v", err)
	}
	time.Sleep(5 * time.Second)

	// Create second pod with read-only mount
	t.Log("Creating pod with read-only mount...")
	if err := bt.CreatePod("test-pod-readonly", "testdata/readonly-volume/pod-readonly.yaml",
		map[string]string{"PVC_NAME": pvcName}); err != nil {
		t.Fatal(err)
	}
	t.Log("✓ Read-only pod running")

	// Verify we can read data
	if err := bt.VerifyFileContents("test-pod-readonly", "/data/readonly.txt", testData); err != nil {
		t.Fatal(err)
	}
	t.Log("✓ Data readable in read-only mode")

	// Verify writes fail
	t.Log("Verifying write operations fail...")
	if err := bt.VerifyReadOnly("test-pod-readonly", "/data"); err != nil {
		t.Fatal(err)
	}

	t.Log("✓ Write operations correctly fail on read-only volume")
	t.Log("✓ Read-only volume test passed!")
}
