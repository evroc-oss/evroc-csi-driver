package mocks

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
)

const (
	// MockDevRoot is the root /dev directory for testing
	MockDevRoot = "/tmp/csi-sanity-dev"
	// MockDeviceByIDPath is where we create mock device symlinks for testing
	MockDeviceByIDPath = "/tmp/csi-sanity-dev/disk/by-id"
)

// MockDeviceManager handles creating and cleaning up mock block devices for testing
type MockDeviceManager struct {
	devices map[string]string // volumeID -> devicePath
}

// NewMockDeviceManager creates a new mock device manager
func NewMockDeviceManager() *MockDeviceManager {
	return &MockDeviceManager{
		devices: make(map[string]string),
	}
}

// Setup initializes the mock device directory structure
func (m *MockDeviceManager) Setup() error {
	// Create the mock /dev/disk/by-id directory structure
	if err := os.MkdirAll(MockDeviceByIDPath, 0755); err != nil {
		return fmt.Errorf("failed to create mock device directory: %w", err)
	}

	// Also create other common /dev subdirectories that might be needed
	dirs := []string{
		filepath.Join(MockDevRoot, "disk"),
		filepath.Join(MockDevRoot, "block"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	return nil
}

// volumeIDToSerial converts a volume ID to a device serial number.
// This duplicates the controller's logic for testing purposes.
func volumeIDToSerial(volumeID string) string {
	// Use first 20 chars of SHA256 hash for the serial
	hash := sha256.Sum256([]byte(volumeID))
	return fmt.Sprintf("%x", hash)[:20]
}

// CreateMockDevice creates a mock block device file for a volume
func (m *MockDeviceManager) CreateMockDevice(volumeID string) (string, error) {
	// Generate the serial number using the same logic as the controller
	serial := volumeIDToSerial(volumeID)

	// Create the device path matching what the driver expects
	deviceName := fmt.Sprintf("scsi-0QEMU_QEMU_HARDDISK_%s", serial)
	devicePath := filepath.Join(MockDeviceByIDPath, deviceName)

	// Create a regular file (not a real block device, but good enough for testing)
	// In a real test environment, you'd use loop devices or similar
	f, err := os.Create(devicePath)
	if err != nil {
		return "", fmt.Errorf("failed to create mock device file: %w", err)
	}

	// Pre-allocate 100MB for the mock device
	if err := f.Truncate(100 * 1024 * 1024); err != nil {
		return "", fmt.Errorf("failed to allocate mock device space: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("failed to close mock device file: %w", err)
	}

	// Make it appear more like a block device
	if err := os.Chmod(devicePath, 0660); err != nil {
		return "", fmt.Errorf("failed to chmod mock device: %w", err)
	}

	m.devices[volumeID] = devicePath
	return devicePath, nil
}

// GetDevicePath returns the device path for a volume (creates it if needed)
func (m *MockDeviceManager) GetDevicePath(volumeID string) (string, error) {
	if devicePath, exists := m.devices[volumeID]; exists {
		return devicePath, nil
	}
	return m.CreateMockDevice(volumeID)
}

// DeleteMockDevice removes a mock device for a volume
func (m *MockDeviceManager) DeleteMockDevice(volumeID string) error {
	devicePath, exists := m.devices[volumeID]
	if !exists {
		return nil // Already deleted or never created
	}

	if err := os.Remove(devicePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove mock device: %w", err)
	}

	delete(m.devices, volumeID)
	return nil
}

// Cleanup removes all mock devices and the mock directory
func (m *MockDeviceManager) Cleanup() error {
	// Remove all mock devices
	for volumeID := range m.devices {
		if err := m.DeleteMockDevice(volumeID); err != nil {
			return fmt.Errorf("failed to delete mock device %s: %w", volumeID, err)
		}
	}

	// Remove the entire mock /dev root directory
	if err := os.RemoveAll(MockDevRoot); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove mock device directory: %w", err)
	}

	return nil
}

// GetDeviceByIDPath returns the mock device by-id path
func (m *MockDeviceManager) GetDeviceByIDPath() string {
	return MockDeviceByIDPath
}
