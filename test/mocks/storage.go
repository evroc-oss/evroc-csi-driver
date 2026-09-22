package mocks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sync"

	evrocpkg "github.com/evroc-oss/evroc-csi-driver/pkg/evroc"
	"github.com/evroc-oss/evroc-go-sdk/compute"
	computetypes "github.com/evroc-oss/evroc-go-sdk/types/compute"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// MockStorageBackend is a mock implementation of evroc.StorageBackend for testing
type MockStorageBackend struct {
	logger      *slog.Logger
	deviceMgr   *MockDeviceManager
	disks       map[string]*computetypes.Disk
	attachments map[string]*computetypes.HotswapDiskAttachment
	nodes       map[string]bool // Track valid nodes for testing
	mu          sync.Mutex
}

// NewMockStorageBackend creates a new mock storage backend
func NewMockStorageBackend(logger *slog.Logger, deviceMgr *MockDeviceManager) *MockStorageBackend {
	nodes := make(map[string]bool)
	nodes["test-node-1"] = true // Default test node
	return &MockStorageBackend{
		logger:      logger,
		deviceMgr:   deviceMgr,
		disks:       make(map[string]*computetypes.Disk),
		attachments: make(map[string]*computetypes.HotswapDiskAttachment),
		nodes:       nodes,
	}
}

// AddNode registers a valid node for testing.
func (m *MockStorageBackend) AddNode(vmName string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nodes[vmName] = true
}

// EnsureDiskCreated creates or updates a disk in the mock storage
func (m *MockStorageBackend) EnsureDiskCreated(ctx context.Context, name string, sizeMB int32, storageClass, zone string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if existing, exists := m.disks[name]; exists {
		if existing.Spec.DiskSize != nil {
			existingMB := existing.Spec.DiskSize.Amount
			if existingMB != sizeMB {
				return false, status.Errorf(codes.AlreadyExists,
					"volume %s already exists with different capacity (existing: %d MB, requested: %d MB)",
					name, existingMB, sizeMB)
			}
		}
		m.logger.Info("Mock disk already exists", "name", name, "sizeMB", sizeMB)
		return false, nil
	}

	disk := &computetypes.Disk{
		Metadata: computetypes.RegionalMetadataResponse{
			Id: name,
		},
		Spec: computetypes.DiskSpec{
			DiskSize: &computetypes.DiskSpecDiskSize{
				Amount: sizeMB,
				Unit:   computetypes.DiskSpecDiskSizeUnitMB,
			},
		},
		Status: computetypes.DiskStatus{
			DiskSize: &computetypes.DiskStatusDiskSize{
				Amount: sizeMB,
				Unit:   computetypes.DiskStatusDiskSizeUnitMB,
			},
		},
	}

	m.disks[name] = disk
	m.logger.Info("Created mock disk", "name", name, "sizeMB", sizeMB)
	return true, nil
}

// EnsureDiskDeleted removes a disk from the mock storage
func (m *MockStorageBackend) EnsureDiskDeleted(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.disks[name]; !exists {
		m.logger.Info("Mock disk already deleted", "name", name)
		return nil
	}

	delete(m.disks, name)
	m.logger.Info("Deleted mock disk", "name", name)
	return nil
}

// GetDisk retrieves a specific disk from the mock storage
func (m *MockStorageBackend) GetDisk(ctx context.Context, name string) (*computetypes.Disk, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	disk, exists := m.disks[name]
	if !exists {
		return nil, status.Errorf(codes.NotFound, "disk %s not found", name)
	}

	return disk, nil
}

func (m *MockStorageBackend) ListDisks(ctx context.Context) (*compute.DiskList, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	items := make([]computetypes.Disk, 0, len(m.disks))
	for _, disk := range m.disks {
		items = append(items, *disk)
	}

	return &compute.DiskList{
		Items: items,
	}, nil
}

// GetAttachment retrieves a specific attachment
func (m *MockStorageBackend) GetAttachment(ctx context.Context, diskName, vmName string) (*computetypes.HotswapDiskAttachment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := fmt.Sprintf("%s-to-%s", diskName, vmName)
	attachment, exists := m.attachments[key]
	if !exists {
		return nil, status.Errorf(codes.NotFound, "attachment %s not found", key)
	}

	return attachment, nil
}

// ListAttachments returns all attachments in the mock storage
func (m *MockStorageBackend) ListAttachments(ctx context.Context) (*compute.HotswapDiskAttachmentList, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	items := make([]computetypes.HotswapDiskAttachment, 0, len(m.attachments))
	for _, attachment := range m.attachments {
		items = append(items, *attachment)
	}

	return &compute.HotswapDiskAttachmentList{
		Items: items,
	}, nil
}

// CountNodeAttachments returns the number of attachments for a specific node/VM
func (m *MockStorageBackend) CountNodeAttachments(ctx context.Context, vmName string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	count := 0
	for _, attachment := range m.attachments {
		if evrocpkg.ExtractResourceName(attachment.Spec.VirtualMachineRef) == vmName {
			count++
		}
	}

	m.logger.Debug("Counted attachments for node", "vmName", vmName, "count", count)
	return count, nil
}

// EnsureAttachmentCreated creates an attachment and creates the mock device.
// The serial number is automatically generated (first 20 chars of disk name hash).
func (m *MockStorageBackend) EnsureAttachmentCreated(ctx context.Context, diskName, vmName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	attachmentKey := fmt.Sprintf("%s-%s", diskName, vmName)

	if _, exists := m.attachments[attachmentKey]; exists {
		m.logger.Info("Mock attachment already exists", "disk", diskName, "vm", vmName)
		return nil
	}

	// Verify disk exists
	if _, exists := m.disks[diskName]; !exists {
		return status.Errorf(codes.NotFound, "volume %s does not exist", diskName)
	}

	// Verify node exists
	if !m.nodes[vmName] {
		return status.Errorf(codes.NotFound, "node %s does not exist", vmName)
	}

	// Generate serial number from disk name (first 20 chars of SHA256 hash)
	hash := sha256.Sum256([]byte(diskName))
	serial := hex.EncodeToString(hash[:])[:20]

	attachment := &computetypes.HotswapDiskAttachment{
		Metadata: computetypes.RegionalMetadataResponse{
			Id: attachmentKey,
		},
		Spec: computetypes.HotswapDiskAttachmentSpec{
			DiskRef:           "/compute/projects/mock-project/regions/se-sto/disks/" + diskName,
			VirtualMachineRef: "/compute/projects/mock-project/regions/se-sto/virtualMachines/" + vmName,
		},
		Status: computetypes.HotswapDiskAttachmentStatus{
			Serial: &serial,
		},
	}

	m.attachments[attachmentKey] = attachment

	// Create the mock device file for the volume
	if _, err := m.deviceMgr.CreateMockDevice(diskName); err != nil {
		m.logger.Error("Failed to create mock device", "disk", diskName, "error", err)
		return status.Errorf(codes.Internal, "failed to create mock device: %v", err)
	}

	m.logger.Info("Created mock attachment and device", "disk", diskName, "vm", vmName, "serial", serial)
	return nil
}

// WaitForAttachmentSerial waits for the serial to be populated (in mock, it's immediate)
func (m *MockStorageBackend) WaitForAttachmentSerial(ctx context.Context, diskName, vmName string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	attachmentKey := fmt.Sprintf("%s-%s", diskName, vmName)

	att, exists := m.attachments[attachmentKey]
	if !exists {
		return "", status.Errorf(codes.NotFound, "attachment %s not found", attachmentKey)
	}

	if att.Status.Serial == nil {
		return "", status.Errorf(codes.Unavailable, "serial not available for %s", attachmentKey)
	}

	m.logger.Info("Mock serial retrieved immediately", "disk", diskName, "vm", vmName, "serial", *att.Status.Serial)
	return *att.Status.Serial, nil
}

// EnsureAttachmentDeleted removes an attachment and deletes the mock device
func (m *MockStorageBackend) EnsureAttachmentDeleted(ctx context.Context, diskName, vmName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	attachmentKey := fmt.Sprintf("%s-%s", diskName, vmName)

	if _, exists := m.attachments[attachmentKey]; !exists {
		m.logger.Info("Mock attachment already deleted", "disk", diskName, "vm", vmName)
		return nil
	}

	delete(m.attachments, attachmentKey)

	// Delete the mock device file
	if err := m.deviceMgr.DeleteMockDevice(diskName); err != nil {
		m.logger.Warn("Failed to delete mock device", "disk", diskName, "error", err)
	}

	m.logger.Info("Deleted mock attachment and device", "disk", diskName, "vm", vmName)
	return nil
}

// DiskExists checks if a disk exists (efficient check for single disk)
func (m *MockStorageBackend) DiskExists(ctx context.Context, name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, exists := m.disks[name]
	return exists
}

// EnsureDiskResized resizes a disk in the mock storage, mirroring the new
// size into both spec and status so that status-based resize polling sees
// the updated size immediately.
func (m *MockStorageBackend) EnsureDiskResized(ctx context.Context, name string, sizeMB int32) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	existing, exists := m.disks[name]

	if exists {
		if existing.Spec.DiskSize != nil {
			existing.Spec.DiskSize.Amount = sizeMB
		}
		// Mirror the new size into status so that status-based resize polling
		// sees the updated size immediately (the mock backend is synchronous).
		existing.Status.DiskSize = &computetypes.DiskStatusDiskSize{
			Amount: sizeMB,
			Unit:   computetypes.DiskStatusDiskSizeUnitMB,
		}
		m.disks[name] = existing
		m.logger.Info("Resized mock disk", "name", name, "sizeMB", sizeMB)
		return nil
	}
	return fmt.Errorf("disk not found")
}
