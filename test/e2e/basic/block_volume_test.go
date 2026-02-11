package basic

import (
	"fmt"
	"strings"
	"testing"
)

// TestBlockVolume tests raw block device access
func TestBlockVolume(t *testing.T) {
	bt := NewBasicTest(t)
	defer bt.Cleanup()

	t.Log("=== Testing raw block volume ===")

	if err := bt.CreatePVCAndPod("test-pvc-block", "test-pod-block", "block-volume", nil); err != nil {
		t.Fatal(err)
	}
	t.Log("✓ Pod running with block device")

	// Verify block device exists
	t.Log("Verifying block device exists...")
	output, err := bt.Exec(bt.ctx, "default", "test-pod-block", "ls", "-l", "/dev/xvda")
	if err != nil {
		t.Fatalf("Block device not found: %v", err)
	}
	t.Logf("Block device info: %s", strings.TrimSpace(output))

	// Write and read data
	t.Log("Writing data to block device...")
	testData := "BLOCK-DEVICE-TEST-DATA"
	_, err = bt.Exec(bt.ctx, "default", "test-pod-block", "sh", "-c",
		fmt.Sprintf("echo '%s' | dd of=/dev/xvda bs=512 count=1", testData))
	if err != nil {
		t.Fatalf("Failed to write to block device: %v", err)
	}
	t.Log("✓ Data written to block device")

	t.Log("Reading data from block device...")
	output, err = bt.Exec(bt.ctx, "default", "test-pod-block", "dd", "if=/dev/xvda", "bs=512", "count=1")
	if err != nil {
		t.Fatalf("Failed to read from block device: %v", err)
	}
	if !strings.Contains(output, testData) {
		t.Fatalf("Data mismatch. Expected to find: %q in output", testData)
	}

	t.Log("✓ Data read successfully from block device")
	t.Log("✓ Block volume test passed!")
}
