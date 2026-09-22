package basic

import (
	"fmt"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
)

// TestResizing tests that volumes can be resized and validates that the
// requested storage size is reflected inside the pod.
//
// It runs `lsblk` inside the pod and checks the SIZE column of the block
// device mounted at /data. This reflects the block-device size reported by
// the kernel, which grows once ControllerExpandVolume + NodeExpandVolume have
// completed.
func TestResizing(t *testing.T) {
	bt := NewBasicTest(t)
	defer bt.Cleanup()

	t.Log("=== Testing volume resizing ===")

	requestedSize := "2Gi"
	pvcName := "test-pvc-resizing"
	podName := "test-pod-resizing"

	t.Logf("Creating PVC with size %s...", requestedSize)
	if err := bt.CreatePVCAndPod(pvcName, podName, "capacity", map[string]string{"SIZE": requestedSize}); err != nil {
		t.Fatal(err)
	}
	t.Log("✓ Pod running with volume mounted")

	// Check the initial volume capacity with lsblk inside the pod.
	initialSizeMB := bt.getMountedDeviceSizeMB(t, podName, "/data")
	t.Logf("Initial lsblk size: %d MiB", initialSizeMB)

	// 2Gi request -> ~2048 MiB device. Allow tolerance for rounding/overhead.
	if !inRange(initialSizeMB, 1900, 2100) {
		t.Fatalf("Initial volume size mismatch. Requested %s, lsblk reported %d MiB (expected ~2048)",
			requestedSize, initialSizeMB)
	}
	t.Logf("✓ Initial volume size correct: requested %s, lsblk reported %d MiB",
		requestedSize, initialSizeMB)

	// Verify PVC status capacity.
	bt.verifyPVCCapacity(t, pvcName, 2)

	t.Log("Increasing the PVC to 4Gi")
	increasedSize := "4Gi"
	if err := bt.UpdatePVC(pvcName, "capacity", map[string]string{"SIZE": increasedSize}); err != nil {
		t.Fatal(err)
	}

	// Wait for the PVC status capacity to reflect the new size. This can take
	// a while (often tens of seconds) because the resize flows through several
	// async reconciliation loops, each on its own interval:
	//  1. external-resizer observes the PVC spec change and drives
	//     ControllerExpandVolume, then updates PV.Spec.Capacity.
	//  2. kube-controller-manager's PV controller observes the PV capacity
	//     change and mirrors it to PVC.Status.Capacity via its informer/resync
	//     loop.
	//  3. The actual filesystem resize (NodeExpandVolume) is driven by the
	//     kubelet's volume expand controller, which polls for volumes needing
	//     expansion on its own periodic sync — this is usually the bottleneck.
	// Even though the controller grows the cloud disk quickly, the node-side
	// resize and the PVC status reflection lag behind those sync intervals.
	bt.waitForPVCCapacity(t, pvcName, 4, 120*time.Second)

	// Now check lsblk inside the pod. The device should reflect the resize.
	resizedSizeMB := bt.getMountedDeviceSizeMB(t, podName, "/data")
	t.Logf("Resized lsblk size: %d MiB", resizedSizeMB)

	if !inRange(resizedSizeMB, 3900, 4100) {
		t.Fatalf("Resized volume size mismatch. Requested %s, lsblk reported %d MiB (expected ~4096)",
			increasedSize, resizedSizeMB)
	}
	t.Logf("✓ Resized volume size correct: requested %s, lsblk reported %d MiB",
		increasedSize, resizedSizeMB)

	t.Log("✓ Volume Resizing test passed!")
}

// getMountedDeviceSizeMB runs `lsblk` inside the pod and returns the SIZE
// (in MiB) of the block device mounted at the given mountpoint.
//
// The test pod image uses BusyBox lsblk, which does not support flags (-b,
// -l, -n, -o) but does print the columns: NAME MAJ:MIN SIZE TYPE MOUNTPOINTS.
// Sizes are human-readable with suffixes (e.g. "4096M", "1.9G", "15.0G").
func (bt *BasicTest) getMountedDeviceSizeMB(t *testing.T, podName, mountpoint string) int64 {
	t.Helper()
	output, err := bt.Exec(bt.ctx, "default", podName, "lsblk")
	if err != nil {
		t.Fatalf("Failed to run lsblk in pod %s: %v", podName, err)
	}
	t.Logf("lsblk output:\n%s", output)

	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		fields := strings.Fields(line)
		// Expect: NAME MAJ:MIN SIZE TYPE MOUNTPOINT
		if len(fields) < 5 {
			continue
		}
		if fields[len(fields)-1] != mountpoint {
			continue
		}
		sizeMB, err := parseLsblkSize(fields[2])
		if err != nil {
			t.Fatalf("Could not parse size %q from lsblk line: %s (%v)", fields[2], line, err)
		}
		return sizeMB
	}

	t.Fatalf("Could not find device mounted at %s in lsblk output:\n%s", mountpoint, output)
	return 0
}

// waitForPVCCapacity polls the PVC status.capacity.storage until it equals
// expectedGi, or timeout is reached. The PVC status is updated by Kubernetes
// once the external-resizer has driven ControllerExpandVolume +
// NodeExpandVolume to completion.
func (bt *BasicTest) waitForPVCCapacity(t *testing.T, pvcName string, expectedGi int64, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		pvc, err := bt.GetPVC(bt.ctx, "default", pvcName)
		if err != nil {
			t.Fatalf("Failed to get PVC %s: %v", pvcName, err)
		}
		capacity := pvc.Status.Capacity[corev1.ResourceStorage]
		capacityGi := capacity.Value() / (1024 * 1024 * 1024)
		if capacityGi == expectedGi {
			t.Logf("PVC %s capacity updated to %d Gi", pvcName, capacityGi)
			return
		}
		t.Logf("Waiting for PVC %s capacity to reach %d Gi (currently %d Gi)...",
			pvcName, expectedGi, capacityGi)
		time.Sleep(5 * time.Second)
	}
	t.Fatalf("PVC %s capacity did not reach %d Gi within %v", pvcName, expectedGi, timeout)
}

// parseLsblkSize parses a BusyBox lsblk SIZE string (e.g. "4096M", "1.9G",
// "15.0G", "512K", "0") into MiB.
func parseLsblkSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" {
		return 0, nil
	}

	last := s[len(s)-1]
	numStr := s[:len(s)-1]

	var num float64
	if _, err := fmt.Sscanf(numStr, "%f", &num); err != nil {
		return 0, fmt.Errorf("parse number %q: %w", numStr, err)
	}

	switch last {
	case 'K', 'k':
		return int64(num / 1024), nil
	case 'M', 'm':
		return int64(num), nil
	case 'G', 'g':
		return int64(num * 1024), nil
	case 'T', 't':
		return int64(num * 1024 * 1024), nil
	default:
		// No suffix — assume bytes.
		var bytes int64
		if _, err := fmt.Sscanf(s, "%d", &bytes); err != nil {
			return 0, fmt.Errorf("parse bytes %q: %w", s, err)
		}
		return bytes / (1024 * 1024), nil
	}
}

// verifyPVCCapacity checks the PVC status.capacity.storage equals expectedGi.
func (bt *BasicTest) verifyPVCCapacity(t *testing.T, pvcName string, expectedGi int64) {
	t.Helper()
	pvc, err := bt.GetPVC(bt.ctx, "default", pvcName)
	if err != nil {
		t.Fatalf("Failed to get PVC %s: %v", pvcName, err)
	}
	capacity := pvc.Status.Capacity[corev1.ResourceStorage]
	capacityGi := capacity.Value() / (1024 * 1024 * 1024)
	t.Logf("PVC %s reports capacity: %d Gi", pvcName, capacityGi)
	if capacityGi != expectedGi {
		t.Fatalf("PVC %s capacity mismatch. Expected: %d Gi, Got: %d Gi",
			pvcName, expectedGi, capacityGi)
	}
}

// inRange returns true if v is within [lo, hi] inclusive.
func inRange(v, lo, hi int64) bool {
	return v >= lo && v <= hi
}
