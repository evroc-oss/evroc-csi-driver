package mocks

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/evroc-oss/evroc-csi-driver/pkg/filesystem"
)

// MockFilesystemOperations is a mock implementation of filesystem.Operations for testing
type MockFilesystemOperations struct {
	logger        *slog.Logger
	deviceMgr     *MockDeviceManager
	formattedDevs map[string]string // devicePath -> fsType
	mountPoints   map[string]string // target -> source
	mu            sync.Mutex
}

// NewMockFilesystemOperations creates a new mock filesystem operations
func NewMockFilesystemOperations(logger *slog.Logger, deviceMgr *MockDeviceManager) *MockFilesystemOperations {
	return &MockFilesystemOperations{
		logger:        logger,
		deviceMgr:     deviceMgr,
		formattedDevs: make(map[string]string),
		mountPoints:   make(map[string]string),
	}
}

// WaitForDevice waits for a device to appear (mock just checks if it exists)
func (m *MockFilesystemOperations) WaitForDevice(ctx context.Context, devicePath string, timeout time.Duration) error {
	// Check if the mock device file exists
	if _, err := os.Stat(devicePath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("device %s not found", devicePath)
		}
		return err
	}
	m.logger.Info("Mock device found", "path", devicePath)
	return nil
}

// ScanForDeviceBySerial scans for a device with the given serial number
func (m *MockFilesystemOperations) ScanForDeviceBySerial(deviceByIDPath, serial, pattern string) (string, error) {
	// Use the same logic as the real implementation to find the device
	deviceName := fmt.Sprintf("%s%s", pattern, serial)
	devicePath := filepath.Join(deviceByIDPath, deviceName)

	if _, err := os.Stat(devicePath); err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("device with serial %s not found", serial)
		}
		return "", err
	}

	m.logger.Info("Mock device found by serial", "serial", serial, "path", devicePath)
	return devicePath, nil
}

// IsDeviceFormatted checks if a device is formatted (mock tracks this in memory)
func (m *MockFilesystemOperations) IsDeviceFormatted(ctx context.Context, devicePath string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, formatted := m.formattedDevs[devicePath]
	m.logger.Info("Checking if mock device is formatted", "path", devicePath, "formatted", formatted)
	return formatted, nil
}

// FormatDevice formats a device (mock just records it in memory)
func (m *MockFilesystemOperations) FormatDevice(ctx context.Context, devicePath, fsType string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Verify device exists
	if _, err := os.Stat(devicePath); err != nil {
		return fmt.Errorf("device does not exist: %w", err)
	}

	m.formattedDevs[devicePath] = fsType
	m.logger.Info("Formatted mock device", "path", devicePath, "fsType", fsType)
	return nil
}

// RepairFilesystem attempts to repair a filesystem (mock always succeeds)
func (m *MockFilesystemOperations) RepairFilesystem(ctx context.Context, devicePath, fsType string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Verify device exists
	if _, err := os.Stat(devicePath); err != nil {
		return fmt.Errorf("device does not exist: %w", err)
	}

	// Mock fsck always succeeds - in real tests we would simulate failures
	m.logger.Info("Mock filesystem repair successful", "path", devicePath, "fsType", fsType)
	return nil
}

// ResizeFilesystem resizes the filesystem on a device (mock records the
// operation and always succeeds, as long as the device exists).
func (m *MockFilesystemOperations) ResizeFilesystem(ctx context.Context, devicePath, mountPath, fsType string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Verify device exists
	if _, err := os.Stat(devicePath); err != nil {
		return fmt.Errorf("device does not exist: %w", err)
	}

	m.logger.Info("Mock resized filesystem", "devicePath", devicePath, "mountPath", mountPath, "fsType", fsType)
	return nil
}

// IsBlockDevice checks if a path is a block device (mock checks if it's in our device manager)
func (m *MockFilesystemOperations) IsBlockDevice(path string) (bool, error) {
	// In our mock, all files in the mock device directory are considered block devices
	if filepath.Dir(path) == MockDeviceByIDPath {
		if _, err := os.Stat(path); err == nil {
			return true, nil
		}
	}
	return false, nil
}

// Mount performs a mount operation (mock just records it)
func (m *MockFilesystemOperations) Mount(source, target, fsType string, flags uintptr, options string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Don't create directories - the test framework or driver MkdirAll handles that
	// Just verify source exists
	if _, err := os.Stat(source); err != nil {
		return fmt.Errorf("source does not exist: %w", err)
	}

	// Record the mount
	m.mountPoints[target] = source
	m.logger.Info("Mock mounted", "source", source, "target", target, "fsType", fsType)
	return nil
}

// Unmount performs an unmount operation (mock just removes from records)
func (m *MockFilesystemOperations) Unmount(target string, flags int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.mountPoints[target]; !exists {
		m.logger.Warn("Mock unmount called on non-mounted path", "target", target)
		// Don't return error, just like real unmount
	} else {
		delete(m.mountPoints, target)
		m.logger.Info("Mock unmounted", "target", target)
	}
	return nil
}

// IsMountPoint checks if a path is a mount point (mock checks our records)
func (m *MockFilesystemOperations) IsMountPoint(path string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, isMounted := m.mountPoints[path]
	m.logger.Debug("Checking if path is mount point", "path", path, "isMounted", isMounted)
	return isMounted, nil
}

// FindDeviceForPath resolves the block device backing a mount path by walking
// the in-memory mount records (the mock analogue of /proc/mounts). It follows
// bind-mount hops until it reaches a source that is not itself a mount target,
// which it treats as the device.
func (m *MockFilesystemOperations) FindDeviceForPath(path string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	current := path
	for i := 0; i < mockMaxBindMountHops; i++ {
		source, ok := m.mountPoints[current]
		if !ok {
			return "", fmt.Errorf("mount point %s not found", path)
		}

		// If the source is itself a mount target it is a bind hop; otherwise
		// it is the device.
		if _, isMount := m.mountPoints[source]; !isMount {
			m.logger.Info("Mock resolved device for path", "path", path, "device", source)
			return source, nil
		}
		current = source
	}

	return "", fmt.Errorf("could not resolve device for path %s (bind-mount chain too deep)", path)
}

// mockMaxBindMountHops bounds the bind-mount chain walk in the mock, mirroring
// maxBindMountHops in the real implementation.
const mockMaxBindMountHops = 10

// MkdirAll creates a directory and all parents
func (m *MockFilesystemOperations) MkdirAll(path string, perm uint32) error {
	// Actually create the directory - NodePublishVolume needs it to exist
	// But only if it's not in the test framework's forbidden paths
	if err := os.MkdirAll(path, os.FileMode(perm)); err != nil {
		return err
	}
	m.logger.Debug("Mock MkdirAll created directory", "path", path, "perm", perm)
	return nil
}

// RemoveAll removes a path and all children
func (m *MockFilesystemOperations) RemoveAll(path string) error {
	if err := os.RemoveAll(path); err != nil {
		return err
	}
	m.logger.Debug("Removed directory", "path", path)
	return nil
}

// CreateFile creates a regular file
func (m *MockFilesystemOperations) CreateFile(path string, perm uint32) error {
	// Create parent directory if needed
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("failed to close file: %w", err)
	}

	if err := os.Chmod(path, os.FileMode(perm)); err != nil {
		return err
	}

	m.logger.Debug("Created file", "path", path)
	return nil
}

// PathExists checks if a path exists
func (m *MockFilesystemOperations) PathExists(path string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if this path is a mount point (target or source)
	if _, mounted := m.mountPoints[path]; mounted {
		return true, nil
	}

	// Check real filesystem
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// GetFilesystemStats returns filesystem statistics
func (m *MockFilesystemOperations) GetFilesystemStats(path string) (*filesystem.FilesystemStats, error) {
	// Return mock stats - 100GB total, 50GB available
	stats := &filesystem.FilesystemStats{
		TotalBytes:      100 * 1024 * 1024 * 1024,
		AvailableBytes:  50 * 1024 * 1024 * 1024,
		UsedBytes:       50 * 1024 * 1024 * 1024,
		TotalInodes:     1000000,
		AvailableInodes: 500000,
		UsedInodes:      500000,
	}
	m.logger.Debug("Returning mock filesystem stats", "path", path)
	return stats, nil
}
