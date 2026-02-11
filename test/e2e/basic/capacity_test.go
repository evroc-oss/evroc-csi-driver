package basic

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

// TestVolumeCapacity tests that volumes have the correct capacity.
// This validates that the requested storage size is properly honored.
func TestVolumeCapacity(t *testing.T) {
	bt := NewBasicTest(t)
	defer bt.Cleanup()

	t.Log("=== Testing volume capacity ===")

	requestedSize := "5Gi"
	pvcName := "test-pvc-capacity"
	podName := "test-pod-capacity"

	t.Logf("Creating PVC with size %s...", requestedSize)
	if err := bt.CreatePVCAndPod(pvcName, podName, "capacity", map[string]string{"SIZE": requestedSize}); err != nil {
		t.Fatal(err)
	}
	t.Log("✓ Pod running with volume mounted")

	// Check the actual volume capacity
	t.Log("Checking volume capacity with df...")
	output, err := bt.Exec(bt.ctx, "default", podName, "df", "-h", "/data")
	if err != nil {
		t.Fatalf("Failed to check disk capacity: %v", err)
	}
	t.Logf("df output:\n%s", output)

	// Parse df output to verify size
	fields := strings.Fields(output)
	var actualSize string
	for _, field := range fields {
		if strings.HasSuffix(field, "G") || strings.HasSuffix(field, "M") ||
			strings.HasSuffix(field, "K") || strings.HasSuffix(field, "T") {
			actualSize = field
			break
		}
	}
	if actualSize == "" {
		t.Fatalf("Could not find size in df output: %s", output)
	}

	// Verify size is approximately correct (filesystem overhead means it won't be exactly 5G)
	if !strings.HasPrefix(actualSize, "4.") && !strings.HasPrefix(actualSize, "5.") {
		t.Fatalf("Volume size mismatch. Requested: %s, Actual: %s (expected ~4.8-4.9G)", requestedSize, actualSize)
	}
	t.Logf("✓ Volume size correct: requested %s, actual %s", requestedSize, actualSize)

	// Also verify using PVC status
	t.Log("Verifying PVC capacity...")
	pvc, err := bt.GetPVC(bt.ctx, "default", pvcName)
	if err != nil {
		t.Fatalf("Failed to get PVC: %v", err)
	}

	capacity := pvc.Status.Capacity[corev1.ResourceStorage]
	capacityGi := capacity.Value() / (1024 * 1024 * 1024)
	t.Logf("PVC reports capacity: %d Gi", capacityGi)

	if capacityGi != 5 {
		t.Fatalf("PVC capacity mismatch. Expected: 5 Gi, Got: %d Gi", capacityGi)
	}
	t.Log("✓ PVC capacity matches request")
	t.Log("✓ Volume capacity test passed!")
}
