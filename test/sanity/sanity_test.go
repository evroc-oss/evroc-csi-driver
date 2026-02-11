package sanity

import (
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/evroc-oss/evroc-csi-driver/pkg/config"
	"github.com/evroc-oss/evroc-csi-driver/pkg/driver"
	"github.com/evroc-oss/evroc-csi-driver/pkg/metrics"
	"github.com/evroc-oss/evroc-csi-driver/test/mocks"
	csiSanity "github.com/kubernetes-csi/csi-test/v5/pkg/sanity"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	socket      = "/tmp/csi-sanity.sock"
	endpoint    = "unix://" + socket
	targetPath  = "/tmp/csi-sanity-target"
	stagingPath = "/tmp/csi-sanity-staging"
	mountPath   = "/tmp/csi-sanity-mount"
)

func TestCSISanity(t *testing.T) {
	// Clean up any existing test artifacts more aggressively
	_ = os.RemoveAll(targetPath)
	_ = os.RemoveAll(stagingPath)
	_ = os.RemoveAll(mountPath)
	_ = os.Remove(socket)

	// Clean up mock device directory
	_ = os.RemoveAll(mocks.MockDevRoot)

	// Set up mock device manager
	deviceMgr := mocks.NewMockDeviceManager()
	if err := deviceMgr.Setup(); err != nil {
		t.Fatalf("Failed to setup mock device manager: %v", err)
	}

	defer func() {
		_ = os.RemoveAll(targetPath)
		_ = os.RemoveAll(stagingPath)
		_ = os.RemoveAll(mountPath)
		_ = os.Remove(socket)
		_ = deviceMgr.Cleanup()
	}()

	// Create a logger
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	// Create mock storage backend, filesystem operations, and Kubernetes client
	mockStorage := mocks.NewMockStorageBackend(logger, deviceMgr)
	mockFS := mocks.NewMockFilesystemOperations(logger, deviceMgr)
	mockKube := mocks.NewMockKubernetesClient("test-node-1", "test-zone-a")

	// Create minimal EvrocConfig for CSI parameters
	testConfig := &config.Config{
		CSI: config.CSIConfig{
			TotalCapacityGB:   1000, // 1TB for testing
			MaxVolumesPerNode: 128,  // Standard limit
		},
	}

	// Create driver configuration with mock dependencies
	driverConfig := &driver.Config{
		Logger:         logger,
		Endpoint:       endpoint,
		NodeID:         "test-node-1",
		Mode:           driver.AllMode,
		EvrocConfig:    testConfig,
		StorageBackend: mockStorage,
		FilesystemOps:  mockFS,
		KubeClient:     mockKube,
		Metrics:        metrics.NewNoOpManager(),
		DeviceByIDPath: deviceMgr.GetDeviceByIDPath(),
	}

	// Create and start the driver
	d, err := driver.NewDriver(driverConfig)
	if err != nil {
		t.Fatalf("Failed to create driver: %v", err)
	}

	// Run driver in background
	go func() {
		if err := d.Run(); err != nil {
			logger.Error("Driver failed", "error", err)
		}
	}()

	// Configure sanity test
	config := csiSanity.TestConfig{
		Address:                   endpoint,
		TargetPath:                targetPath,
		StagingPath:               stagingPath,
		DialOptions:               []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())},
		TestVolumeSize:            1 * 1024 * 1024 * 1024, // 1 GiB
		TestVolumeParametersFile:  "",
		TestNodeVolumeAttachLimit: true, // Test MaxVolumesPerNode support
		IDGen:                     &csiSanity.DefaultIDGenerator{},
	}

	// Run sanity tests
	csiSanity.Test(t, config)

	// Stop the driver
	d.Stop()

	// Give the driver a moment to fully shut down before cleanup
	// This ensures all file handles are released
	time.Sleep(100 * time.Millisecond)
}
