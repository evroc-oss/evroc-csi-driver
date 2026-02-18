package basic

import (
	"fmt"
	"os"
	"testing"
)

const (
	// topologyZone is the expected topology zone label for test cluster nodes
	// Valid zones are: a, b, c
	topologyZone = "a"
)

var (
	// Shared cluster kubeconfig for all tests
	testKubeconfig string
)

// TestMain verifies prerequisites and runs tests
// NOTE: Cluster provisioning is handled externally via setup-csi-driver.sh
func TestMain(m *testing.M) {
	fmt.Println("=== E2E Test Suite ===")

	// Get kubeconfig from environment
	testKubeconfig = os.Getenv("KUBECONFIG")
	if testKubeconfig == "" {
		fmt.Println("ERROR: KUBECONFIG environment variable not set")
		fmt.Println("Run setup-csi-driver.sh first to provision cluster and set up CSI driver")
		os.Exit(1)
	}

	// Verify kubeconfig file exists
	if _, err := os.Stat(testKubeconfig); os.IsNotExist(err) {
		fmt.Printf("ERROR: Kubeconfig file not found: %s\n", testKubeconfig)
		os.Exit(1)
	}

	fmt.Printf("✓ Using kubeconfig: %s\n\n", testKubeconfig)

	// Run all tests
	code := m.Run()

	// Note: Cluster teardown is handled externally via:
	// - teardown-tests.sh (clean test workloads, keep cluster)
	// - deployment/cleanup.sh (destroy cluster VMs)

	os.Exit(code)
}
