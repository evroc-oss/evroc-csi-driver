// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 evroc

package node

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/evroc-oss/evroc-csi-driver/pkg/common"
	"github.com/evroc-oss/evroc-csi-driver/pkg/filesystem"
	"github.com/evroc-oss/evroc-csi-driver/pkg/metrics"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// DeviceByIDPath is the path where device symlinks are created by udev.
	DeviceByIDPath = "/dev/disk/by-id"
	// DefaultFSType is the default filesystem type.
	DefaultFSType = "ext4"

	// KubeVirt SCSI hotplug disk prefix
	// KubeVirt creates hotplugged disks that appear as SCSI devices with this naming pattern.
	// Update this constant if KubeVirt changes its device naming convention.
	QEMUSCSIDiskPrefix = "scsi-0QEMU_QEMU_HARDDISK_"

	// MaxVolumesPerNode is the maximum number of volumes that can be attached to a single node.
	// This is a conservative limit based on:
	// - QEMU virtio-scsi controller supports up to 255 targets by default
	// - Practical resource limits (memory, I/O bandwidth)
	// - KubeVirt hotplug disk attachment capabilities
	//
	// This is NOT a hard SCSI limit - virtio-scsi can support more devices.
	// Adjust this value based on your KubeVirt cluster configuration and workload requirements.
	// Set to 0 in NodeGetInfo to indicate unlimited (not recommended for production).
	MaxVolumesPerNode = 128
)

// Service implements the CSI Node Service.
type Service struct {
	csi.UnimplementedNodeServer
	fs                 filesystem.Operations
	kubeClient         kubernetes.Interface
	nodeID             string
	deviceByIDPath     string // Overridable device path (for testing)
	logger             *slog.Logger
	metrics            *metrics.Manager
	maxVolumesPerNode  int64         // Maximum volumes per node
	deviceScanTimeout  time.Duration // Maximum time to wait for device to appear
	deviceScanInterval time.Duration // Polling interval for device scanning
}

// NewNodeService creates a new Node Service.
// The fs parameter is required and must not be nil.
// The kubeClient parameter is required for zone detection via Kubernetes API.
func NewNodeService(nodeID string, logger *slog.Logger, fs filesystem.Operations, maxVolumesPerNode int64, deviceScanTimeout, deviceScanInterval time.Duration, m *metrics.Manager, kubeClient kubernetes.Interface) *Service {
	if fs == nil {
		panic("filesystem operations cannot be nil")
	}
	if kubeClient == nil {
		panic("kubernetes client cannot be nil")
	}

	return &Service{
		nodeID:             nodeID,
		logger:             logger,
		fs:                 fs,
		deviceByIDPath:     DeviceByIDPath, // Default to system path
		maxVolumesPerNode:  maxVolumesPerNode,
		deviceScanTimeout:  deviceScanTimeout,
		deviceScanInterval: deviceScanInterval,
		metrics:            m,
		kubeClient:         kubeClient,
	}
}

// SetDeviceByIDPath allows overriding the device by-id path (for testing).
func (s *Service) SetDeviceByIDPath(path string) {
	s.deviceByIDPath = path
}

// NodeStageVolume mounts the volume to a staging path.
func (s *Service) NodeStageVolume(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	startTime := time.Now()
	s.logger.Info("NodeStageVolume called",
		"volumeID", req.GetVolumeId(),
		"stagingTargetPath", req.GetStagingTargetPath(),
		"publishContext", req.GetPublishContext())

	if err := common.ValidateRequiredField(req.GetVolumeId(), "volume ID"); err != nil {
		return nil, err
	}
	// Validate staging path for path traversal attacks
	if err := common.ValidatePath(req.GetStagingTargetPath(), "staging target path"); err != nil {
		return nil, err
	}
	if req.GetVolumeCapability() == nil {
		return nil, status.Error(codes.InvalidArgument, "volume capability is required")
	}

	volumeID := req.GetVolumeId()
	stagingPath := req.GetStagingTargetPath()

	// Check if already staged (mounted)
	isMounted, err := s.fs.IsMountPoint(stagingPath)
	if err != nil {
		s.logger.Warn("Failed to check mount point", "path", stagingPath, "error", err)
	}
	if isMounted {
		s.logger.Info("Volume already staged", "volumeID", volumeID, "stagingPath", stagingPath)
		return &csi.NodeStageVolumeResponse{}, nil
	}

	// Get serial number from PublishContext
	serial, ok := req.GetPublishContext()["serial"]
	if !ok || serial == "" {
		return nil, status.Error(codes.InvalidArgument, "serial number not found in PublishContext")
	}

	s.logger.Debug("Using serial from PublishContext", "serial", serial)

	// Construct device path using serial
	devicePath := filepath.Join(s.deviceByIDPath, fmt.Sprintf("%s%s", QEMUSCSIDiskPrefix, serial))

	s.logger.Info("Looking for device", "volumeID", volumeID, "serial", serial, "expectedDevicePath", devicePath)

	// Wait for the device to appear
	if err := s.fs.WaitForDevice(ctx, devicePath, s.deviceScanTimeout); err != nil {
		s.logger.Warn("Device not found - disk may not be attached to VM",
			"volumeID", volumeID,
			"devicePath", devicePath,
			"timeout", s.deviceScanTimeout,
			"error", err)
		return nil, status.Errorf(codes.Unavailable, "device not found at %s: %v", devicePath, err)
	}

	// Check if this is a block volume
	volumeCapability := req.GetVolumeCapability()
	isBlock := volumeCapability.GetBlock() != nil

	if isBlock {
		// For block volumes, create the staging path as a file and bind mount the device to it
		s.logger.Info("Staging raw block device",
			"volumeID", volumeID,
			"devicePath", devicePath,
			"stagingPath", stagingPath)

		// Kubernetes may have created the staging path as a directory
		// For block volumes, we need it to be a file so we can bind mount the device
		if info, err := os.Stat(stagingPath); err == nil {
			if info.IsDir() {
				s.logger.Info("Removing existing directory at staging path for block volume",
					"volumeID", volumeID,
					"stagingPath", stagingPath)
				if err := common.RemoveWithinKubeletRoot(stagingPath); err != nil {
					s.logger.Error("Failed to remove existing directory", "error", err)
					return nil, status.Errorf(codes.Internal, "remove existing directory: %v", err)
				}
			}
		}

		// Create parent directory for staging path
		if err := s.fs.MkdirAll(filepath.Dir(stagingPath), 0o750); err != nil {
			s.logger.Error("Failed to create staging parent dir", "error", err)
			return nil, status.Errorf(codes.Internal, "create staging parent dir: %v", err)
		}

		// Create the staging file for bind mount
		if err := s.fs.CreateFile(stagingPath, 0o660); err != nil {
			s.logger.Error("Failed to create staging file", "error", err)
			return nil, status.Errorf(codes.Internal, "create staging file: %v", err)
		}

		// Bind mount the raw device to staging path
		s.logger.Info("Bind mounting block device", "source", devicePath, "target", stagingPath)
		if err := s.fs.Mount(devicePath, stagingPath, "", unix.MS_BIND, ""); err != nil {
			s.logger.Error("Failed to bind mount block device", "error", err, "source", devicePath, "target", stagingPath)
			s.metrics.RecordNodeOperationError("stage", time.Since(startTime).Seconds(), "mount_failed")
			return nil, status.Errorf(codes.Internal, "bind mount block device: %v", err)
		}

		s.logger.Info("Successfully staged block volume",
			"volumeID", volumeID,
			"devicePath", devicePath,
			"stagingPath", stagingPath)
	} else {
		// For filesystem volumes, format and mount
		s.logger.Info("Staging as filesystem volume",
			"volumeID", volumeID,
			"devicePath", devicePath,
			"stagingPath", stagingPath)

		// Determine filesystem type
		fsType := DefaultFSType
		if volCap := req.GetVolumeCapability(); volCap != nil {
			if mount := volCap.GetMount(); mount != nil && mount.GetFsType() != "" {
				fsType = mount.GetFsType()
			}
		}

		// Create staging directory
		if err := s.fs.MkdirAll(stagingPath, 0o750); err != nil {
			return nil, status.Errorf(codes.Internal, "create staging path: %v", err)
		}

		// Try to mount first - this is the ultimate test of filesystem validity
		// If the device is already formatted with a valid filesystem, this will succeed
		var flags uintptr
		err = s.fs.Mount(devicePath, stagingPath, fsType, flags, "")
		if err != nil {
			s.logger.Info("Initial mount failed",
				"volumeID", volumeID,
				"devicePath", devicePath,
				"error", err)

			// Only handle the 3 specific errors that indicate filesystem corruption:
			// EINVAL (invalid superblock), EIO (I/O error), EUCLEAN (needs fsck)
			// For brand new volumes: format is allowed
			// For volumes with data: repair only (no reformat), error if repair fails
			// ALL other errors (EMFILE, ENOMEM, EACCES, EBUSY, etc.) should NOT trigger format/repair
			if !filesystem.IsFilesystemCorruption(err) {
				s.logger.Error("Mount failed - not attempting format or repair (not filesystem corruption)",
					"volumeID", volumeID,
					"devicePath", devicePath,
					"error", err)
				s.metrics.RecordNodeOperationError("stage", time.Since(startTime).Seconds(), "mount_failed")
				return nil, status.Errorf(codes.Internal,
					"mount failed: %v", err)
			}

			// Mount failed with corruption indicator (EINVAL/EIO/EUCLEAN)
			// Check if device has filesystem signature
			formatted, checkErr := s.fs.IsDeviceFormatted(ctx, devicePath)
			if checkErr != nil {
				s.logger.Warn("Failed to check device format status",
					"devicePath", devicePath,
					"error", checkErr)
				formatted = false
			}

			if formatted {
				// Volume has been formatted before - it may contain user data
				// We MUST NOT automatically reformat as that would cause data loss
				s.logger.Info("Volume has been formatted before, attempting repair only (will NOT reformat)",
					"volumeID", volumeID,
					"devicePath", devicePath,
					"fsType", fsType)

				// Try to repair filesystem
				repairErr := s.fs.RepairFilesystem(ctx, devicePath, fsType)
				if repairErr == nil {
					// Repair succeeded, retry mount
					s.logger.Info("Filesystem repair successful, retrying mount",
						"volumeID", volumeID,
						"devicePath", devicePath)

					if err := s.fs.Mount(devicePath, stagingPath, fsType, flags, ""); err == nil {
						// Mount succeeded after repair - data preserved!
						s.logger.Info("Successfully staged volume after filesystem repair",
							"volumeID", volumeID,
							"devicePath", devicePath,
							"stagingPath", stagingPath,
							"fsType", fsType)

						s.metrics.RecordNodeOperation("stage", time.Since(startTime).Seconds())
						return &csi.NodeStageVolumeResponse{}, nil
					}

					s.logger.Error("Mount failed even after successful repair",
						"volumeID", volumeID,
						"devicePath", devicePath)
				}

				// Repair failed or mount still failed after repair
				// This volume may contain data - we CANNOT reformat automatically
				s.logger.Error("Volume is corrupted and cannot be repaired automatically",
					"volumeID", volumeID,
					"devicePath", devicePath,
					"fsType", fsType,
					"repairError", repairErr)

				// Check if repair failed due to context cancellation/deadline
				if repairErr != nil && ctx.Err() != nil {
					s.logger.Error("Filesystem repair failed due to context error",
						"volumeID", volumeID,
						"devicePath", devicePath,
						"contextError", ctx.Err(),
						"repairError", repairErr)
					s.metrics.RecordNodeOperationError("stage", time.Since(startTime).Seconds(), "repair_context_error")
					return nil, status.Errorf(codes.DeadlineExceeded, "repair operation failed: %v", ctx.Err())
				}

				s.metrics.RecordNodeOperationError("stage", time.Since(startTime).Seconds(), "corruption_unrecoverable")

				// Return error indicating admin intervention required
				return nil, status.Errorf(codes.FailedPrecondition,
					"volume %s is corrupted and cannot be mounted or repaired automatically. "+
						"The volume may contain user data. Manual recovery required. "+
						"Admin should attempt data recovery before reformatting. "+
						"Device: %s, FSType: %s",
					volumeID, devicePath, fsType)
			}

			// Volume is brand new (never formatted) - safe to format
			s.logger.Info("Volume has never been formatted, formatting now",
				"volumeID", volumeID,
				"devicePath", devicePath,
				"fsType", fsType)

			// Format the device
			if err := s.fs.FormatDevice(ctx, devicePath, fsType); err != nil {
				return nil, status.Errorf(codes.Internal, "format device: %v", err)
			}

			// Retry mount after format
			s.logger.Info("Retrying mount after format",
				"volumeID", volumeID,
				"devicePath", devicePath,
				"stagingPath", stagingPath)

			if err := s.fs.Mount(devicePath, stagingPath, fsType, flags, ""); err != nil {
				s.metrics.RecordNodeOperationError("stage", time.Since(startTime).Seconds(), "mount_failed")
				return nil, status.Errorf(codes.Internal, "mount device after format: %v", err)
			}

			s.logger.Info("Successfully staged volume after initial format",
				"volumeID", volumeID,
				"devicePath", devicePath,
				"stagingPath", stagingPath,
				"fsType", fsType)
		} else {
			// Mount succeeded on first try - device was already formatted
			s.logger.Info("Successfully staged volume (device was already formatted)",
				"volumeID", volumeID,
				"devicePath", devicePath,
				"stagingPath", stagingPath,
				"fsType", fsType)
		}
	}

	s.metrics.RecordNodeOperation("stage", time.Since(startTime).Seconds())

	return &csi.NodeStageVolumeResponse{}, nil
}

// NodeUnstageVolume unmounts the volume from the staging path.
func (s *Service) NodeUnstageVolume(ctx context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	startTime := time.Now()
	s.logger.Info("NodeUnstageVolume called",
		"volumeID", req.GetVolumeId(),
		"stagingTargetPath", req.GetStagingTargetPath())

	if err := common.ValidateRequiredField(req.GetVolumeId(), "volume ID"); err != nil {
		return nil, err
	}
	// Validate staging path for path traversal attacks
	if err := common.ValidatePath(req.GetStagingTargetPath(), "staging target path"); err != nil {
		return nil, err
	}

	stagingPath := req.GetStagingTargetPath()

	// Check if staging path is mounted
	isMounted, err := s.fs.IsMountPoint(stagingPath)
	if err != nil {
		s.logger.Warn("Failed to check mount point", "path", stagingPath, "error", err)
	}

	if isMounted {
		if err := s.fs.Unmount(stagingPath, 0); err != nil {
			s.metrics.RecordNodeOperationError("unstage", time.Since(startTime).Seconds(), "unmount_failed")
			return nil, status.Errorf(codes.Internal, "unmount staging path: %v", err)
		}
	}

	// Remove the staging path (CSI spec requires cleanup)
	if err := s.fs.RemoveAll(stagingPath); err != nil {
		s.logger.Warn("Failed to remove staging path", "path", stagingPath, "error", err)
		// Don't fail the operation if removal fails - unmount succeeded
	}

	s.logger.Info("Successfully unstaged volume",
		"volumeID", req.GetVolumeId(),
		"stagingTargetPath", stagingPath)

	s.metrics.RecordNodeOperation("unstage", time.Since(startTime).Seconds())

	return &csi.NodeUnstageVolumeResponse{}, nil
}

// NodePublishVolume mounts the volume to the target path (bind mount from staging).
func (s *Service) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	startTime := time.Now()
	s.logger.Info("NodePublishVolume called",
		"volumeID", req.GetVolumeId(),
		"targetPath", req.GetTargetPath(),
		"stagingTargetPath", req.GetStagingTargetPath(),
		"readonly", req.GetReadonly())

	if err := common.ValidateRequiredField(req.GetVolumeId(), "volume ID"); err != nil {
		return nil, err
	}
	// Validate target path for path traversal attacks
	if err := common.ValidatePath(req.GetTargetPath(), "target path"); err != nil {
		return nil, err
	}
	// Validate staging path for path traversal attacks (if provided)
	if req.GetStagingTargetPath() != "" {
		if err := common.ValidatePath(req.GetStagingTargetPath(), "staging target path"); err != nil {
			return nil, err
		}
	}
	if req.GetVolumeCapability() == nil {
		return nil, status.Error(codes.InvalidArgument, "volume capability is required")
	}

	targetPath := req.GetTargetPath()
	stagingPath := req.GetStagingTargetPath()

	// Check if already published
	isMounted, err := s.fs.IsMountPoint(targetPath)
	if err != nil {
		s.logger.Warn("Failed to check mount point", "path", targetPath, "error", err)
	}
	if isMounted {
		s.logger.Info("Volume already published", "volumeID", req.GetVolumeId(), "targetPath", targetPath)
		return &csi.NodePublishVolumeResponse{}, nil
	}

	// Determine if this is a block volume or filesystem volume
	volumeCapability := req.GetVolumeCapability()
	isBlock := volumeCapability.GetBlock() != nil

	// Validate readonly support
	if isBlock && req.GetReadonly() {
		return nil, status.Error(codes.InvalidArgument,
			"readonly access mode is not supported for block volumes")
	}

	if isBlock {
		s.logger.Info("Publishing as raw block device",
			"volumeID", req.GetVolumeId(),
			"stagingPath", stagingPath,
			"targetPath", targetPath)
		// For block volumes, use the staging path as the device
		// The staging path was already resolved in NodeStageVolume
		devicePath := stagingPath

		// Create parent directory for target
		if err := s.fs.MkdirAll(filepath.Dir(targetPath), 0o750); err != nil {
			s.metrics.RecordNodeOperationError("publish", time.Since(startTime).Seconds(), "create_dir_failed")
			return nil, status.Errorf(codes.Internal, "create parent dir: %v", err)
		}

		// Create the target file for bind mount
		if err := s.fs.CreateFile(targetPath, 0o660); err != nil {
			s.metrics.RecordNodeOperationError("publish", time.Since(startTime).Seconds(), "create_file_failed")
			return nil, status.Errorf(codes.Internal, "create target file: %v", err)
		}

		// Bind mount the device
		if err := s.fs.Mount(devicePath, targetPath, "", unix.MS_BIND, ""); err != nil {
			s.metrics.RecordNodeOperationError("publish", time.Since(startTime).Seconds(), "mount_failed")
			return nil, status.Errorf(codes.Internal, "bind mount block device: %v", err)
		}
	} else {
		s.logger.Info("Publishing as filesystem mount (bind from staging)",
			"volumeID", req.GetVolumeId(),
			"stagingPath", stagingPath,
			"targetPath", targetPath)

		// Create target directory
		if err := s.fs.MkdirAll(targetPath, 0o750); err != nil {
			s.metrics.RecordNodeOperationError("publish", time.Since(startTime).Seconds(), "create_dir_failed")
			return nil, status.Errorf(codes.Internal, "create target path: %v", err)
		}

		// Bind mount from staging to target
		var flags uintptr = unix.MS_BIND
		if req.GetReadonly() {
			flags |= unix.MS_RDONLY
		}

		if err := s.fs.Mount(stagingPath, targetPath, "", flags, ""); err != nil {
			s.metrics.RecordNodeOperationError("publish", time.Since(startTime).Seconds(), "mount_failed")
			return nil, status.Errorf(codes.Internal, "bind mount staging to target: %v", err)
		}

		// If readonly, remount with readonly flag
		if req.GetReadonly() {
			if err := s.fs.Mount("", targetPath, "", unix.MS_BIND|unix.MS_REMOUNT|unix.MS_RDONLY, ""); err != nil {
				s.logger.Warn("Failed to remount readonly", "error", err)
			}
		}
	}

	s.logger.Info("Successfully published volume",
		"volumeID", req.GetVolumeId(),
		"targetPath", targetPath)

	s.metrics.RecordNodeOperation("publish", time.Since(startTime).Seconds())

	return &csi.NodePublishVolumeResponse{}, nil
}

// NodeUnpublishVolume unmounts the volume from the target path.
func (s *Service) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	startTime := time.Now()
	s.logger.Info("NodeUnpublishVolume called",
		"volumeID", req.GetVolumeId(),
		"targetPath", req.GetTargetPath())

	if err := common.ValidateRequiredField(req.GetVolumeId(), "volume ID"); err != nil {
		return nil, err
	}
	// Validate target path for path traversal attacks
	if err := common.ValidatePath(req.GetTargetPath(), "target path"); err != nil {
		return nil, err
	}

	targetPath := req.GetTargetPath()

	// Check if mounted
	isMounted, err := s.fs.IsMountPoint(targetPath)
	if err != nil {
		s.logger.Warn("Failed to check mount point", "path", targetPath, "error", err)
	}

	if isMounted {
		if err := s.fs.Unmount(targetPath, 0); err != nil {
			s.metrics.RecordNodeOperationError("unpublish", time.Since(startTime).Seconds(), "unmount_failed")
			return nil, status.Errorf(codes.Internal, "unmount target: %v", err)
		}
	}

	// Remove the target path (CSI spec requires cleanup)
	if err := s.fs.RemoveAll(targetPath); err != nil {
		s.logger.Warn("Failed to remove target path", "path", targetPath, "error", err)
		// Don't fail the operation if removal fails - unmount succeeded
	}

	s.logger.Info("Successfully unpublished volume",
		"volumeID", req.GetVolumeId(),
		"targetPath", targetPath)

	s.metrics.RecordNodeOperation("unpublish", time.Since(startTime).Seconds())

	return &csi.NodeUnpublishVolumeResponse{}, nil
}

// NodeGetVolumeStats returns statistics about a volume.
func (s *Service) NodeGetVolumeStats(ctx context.Context, req *csi.NodeGetVolumeStatsRequest) (*csi.NodeGetVolumeStatsResponse, error) {
	s.logger.Debug("NodeGetVolumeStats called",
		"volumeID", req.GetVolumeId(),
		"volumePath", req.GetVolumePath())

	if err := common.ValidateRequiredField(req.GetVolumeId(), "volume ID"); err != nil {
		return nil, err
	}
	if err := common.ValidateRequiredField(req.GetVolumePath(), "volume path"); err != nil {
		return nil, err
	}

	volumePath := req.GetVolumePath()

	// Check if path exists (provides natural protection - invalid paths return NotFound)
	exists, err := s.fs.PathExists(volumePath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to check path %s: %v", volumePath, err)
	}
	if !exists {
		return nil, status.Errorf(codes.NotFound, "volume path does not exist: %s", volumePath)
	}

	// Get filesystem stats
	stats, err := s.fs.GetFilesystemStats(volumePath)
	if err != nil {
		// If we can't get stats, the volume might be corrupted or unmounted
		s.logger.Warn("Failed to get volume stats - volume may be unhealthy",
			"volumeID", req.GetVolumeId(),
			"volumePath", volumePath,
			"error", err)

		return &csi.NodeGetVolumeStatsResponse{
			VolumeCondition: &csi.VolumeCondition{
				Abnormal: true,
				Message:  fmt.Sprintf("Failed to get volume statistics: %v. Volume may be corrupted or unmounted.", err),
			},
		}, nil
	}

	// Check for volume health issues
	volumeCondition := s.checkVolumeHealth(req.GetVolumeId(), volumePath, stats)

	return &csi.NodeGetVolumeStatsResponse{
		Usage: []*csi.VolumeUsage{
			{
				Unit:      csi.VolumeUsage_BYTES,
				Available: stats.AvailableBytes,
				Total:     stats.TotalBytes,
				Used:      stats.UsedBytes,
			},
			{
				Unit:      csi.VolumeUsage_INODES,
				Available: stats.AvailableInodes,
				Total:     stats.TotalInodes,
				Used:      stats.UsedInodes,
			},
		},
		VolumeCondition: volumeCondition,
	}, nil
}

// NodeGetCapabilities returns the capabilities of the node.
func (s *Service) NodeGetCapabilities(ctx context.Context, req *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
	s.logger.Debug("NodeGetCapabilities called")

	return &csi.NodeGetCapabilitiesResponse{
		Capabilities: []*csi.NodeServiceCapability{
			{
				Type: &csi.NodeServiceCapability_Rpc{
					Rpc: &csi.NodeServiceCapability_RPC{
						Type: csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME,
					},
				},
			},
			{
				Type: &csi.NodeServiceCapability_Rpc{
					Rpc: &csi.NodeServiceCapability_RPC{
						Type: csi.NodeServiceCapability_RPC_GET_VOLUME_STATS,
					},
				},
			},
			{
				Type: &csi.NodeServiceCapability_Rpc{
					Rpc: &csi.NodeServiceCapability_RPC{
						Type: csi.NodeServiceCapability_RPC_VOLUME_CONDITION,
					},
				},
			},
			{
				Type: &csi.NodeServiceCapability_Rpc{
					Rpc: &csi.NodeServiceCapability_RPC{
						Type: csi.NodeServiceCapability_RPC_EXPAND_VOLUME,
					},
				},
			},
		},
	}, nil
}

// checkVolumeHealth checks the health of a volume and returns a VolumeCondition.
// This is called by NodeGetVolumeStats to report volume health to Kubernetes.
func (s *Service) checkVolumeHealth(volumeID, volumePath string, stats *filesystem.FilesystemStats) *csi.VolumeCondition {
	// Check if volume path is a mount point
	isMounted, err := s.fs.IsMountPoint(volumePath)
	if err != nil {
		s.logger.Warn("Failed to check if volume is mounted",
			"volumeID", volumeID,
			"volumePath", volumePath,
			"error", err)
		return &csi.VolumeCondition{
			Abnormal: true,
			Message:  fmt.Sprintf("Failed to check mount status: %v", err),
		}
	}

	if !isMounted {
		s.logger.Warn("Volume is not mounted",
			"volumeID", volumeID,
			"volumePath", volumePath)
		return &csi.VolumeCondition{
			Abnormal: true,
			Message:  "Volume is not mounted",
		}
	}

	// Check if volume is critically full (>95% usage)
	if stats.TotalBytes > 0 {
		usagePercent := float64(stats.UsedBytes) / float64(stats.TotalBytes) * 100
		if usagePercent > 95 {
			s.logger.Warn("Volume is critically full",
				"volumeID", volumeID,
				"volumePath", volumePath,
				"usagePercent", usagePercent)
			return &csi.VolumeCondition{
				Abnormal: true,
				Message:  fmt.Sprintf("Volume is critically full (%.1f%% used)", usagePercent),
			}
		}
	}

	// Check if inodes are critically low (>95% usage)
	if stats.TotalInodes > 0 {
		inodeUsagePercent := float64(stats.UsedInodes) / float64(stats.TotalInodes) * 100
		if inodeUsagePercent > 95 {
			s.logger.Warn("Volume inodes critically low",
				"volumeID", volumeID,
				"volumePath", volumePath,
				"inodeUsagePercent", inodeUsagePercent)
			return &csi.VolumeCondition{
				Abnormal: true,
				Message:  fmt.Sprintf("Volume inodes critically low (%.1f%% used)", inodeUsagePercent),
			}
		}
	}

	// Volume is healthy
	return &csi.VolumeCondition{
		Abnormal: false,
		Message:  "Volume is healthy",
	}
}

// GetNodeZoneFromKubernetes retrieves the zone label from a Kubernetes node.
// This is a standalone utility function that can be used for early validation or runtime zone detection.
// It reads the topology.kubernetes.io/zone label from the node object.
// Returns an error if the node is not found or if the label is not set.
func GetNodeZoneFromKubernetes(ctx context.Context, kubeClient kubernetes.Interface, nodeID string) (string, error) {
	node, err := kubeClient.CoreV1().Nodes().Get(ctx, nodeID, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get node %s from Kubernetes API: %w", nodeID, err)
	}

	zone, ok := node.Labels["topology.kubernetes.io/zone"]
	if !ok || zone == "" {
		return "", fmt.Errorf("node %s missing topology.kubernetes.io/zone label - zone information is required for multi-zone deployments", nodeID)
	}

	return zone, nil
}

// getNodeZone returns the zone for this node by querying the Kubernetes API.
// It reads the topology.kubernetes.io/zone label from the node object.
// Returns an error if the label is not set, as zone information is mandatory in multi-zone environments.
func (s *Service) getNodeZone(ctx context.Context) (string, error) {
	zone, err := GetNodeZoneFromKubernetes(ctx, s.kubeClient, s.nodeID)
	if err != nil {
		return "", err
	}

	s.logger.Debug("Using zone from node label",
		"nodeID", s.nodeID,
		"zone", zone)
	return zone, nil
}

// NodeGetInfo returns information about the node.
func (s *Service) NodeGetInfo(ctx context.Context, req *csi.NodeGetInfoRequest) (*csi.NodeGetInfoResponse, error) {
	// Get zone from node labels via Kubernetes API
	// Zone information is mandatory - the driver will not start without it
	// The topology.kubernetes.io/zone label must be set on the node
	zone, err := s.getNodeZone(ctx)
	if err != nil {
		s.logger.Error("Failed to get node zone - driver cannot start without zone information", "error", err)
		return nil, status.Errorf(codes.FailedPrecondition, "zone information required: %v", err)
	}

	topology := &csi.Topology{
		Segments: map[string]string{
			"topology.kubernetes.io/zone": zone,
		},
	}

	s.logger.Info("NodeGetInfo",
		"nodeID", s.nodeID,
		"maxVolumesPerNode", s.maxVolumesPerNode,
		"zone", zone)

	return &csi.NodeGetInfoResponse{
		NodeId:             s.nodeID,
		MaxVolumesPerNode:  s.maxVolumesPerNode,
		AccessibleTopology: topology,
	}, nil
}

// NodeExpandVolume expands the filesystem on the node after the backing block
// device has been grown by ControllerExpandVolume.
//
// The controller side only resizes the cloud disk; the filesystem on the node
// is not automatically grown by the kernel, so it must be expanded here (e.g.
// with resize2fs for ext4). Without this, df inside the pod keeps showing the
// old size even though lsblk shows the larger device.
//
// For raw block volumes there is no filesystem to resize, so the request is a
// no-op and the requested capacity is echoed back.
func (s *Service) NodeExpandVolume(ctx context.Context, req *csi.NodeExpandVolumeRequest) (*csi.NodeExpandVolumeResponse, error) {
	startTime := time.Now()
	s.logger.Info("NodeExpandVolume called",
		"volumeID", req.GetVolumeId(),
		"volumePath", req.GetVolumePath(),
		"stagingTargetPath", req.GetStagingTargetPath())

	if err := common.ValidateRequiredField(req.GetVolumeId(), "volume ID"); err != nil {
		return nil, err
	}
	if err := common.ValidateRequiredField(req.GetVolumePath(), "volume path"); err != nil {
		return nil, err
	}
	// Validate staging path (optional) for path traversal attacks, matching
	// the validation done in NodeStageVolume/NodePublishVolume.
	if sp := req.GetStagingTargetPath(); sp != "" {
		if err := common.ValidatePath(sp, "staging target path"); err != nil {
			return nil, err
		}
	}

	volumePath := req.GetVolumePath()

	// The volume must be available (published/staged) on this node. A missing
	// path indicates the volume is not known here.
	exists, err := s.fs.PathExists(volumePath)
	if err != nil {
		s.metrics.RecordNodeOperationError("expand", time.Since(startTime).Seconds(), "path_check_failed")
		return nil, status.Errorf(codes.Internal, "failed to check volume path %s: %v", volumePath, err)
	}
	if !exists {
		s.metrics.RecordNodeOperationError("expand", time.Since(startTime).Seconds(), "volume_not_found")
		return nil, status.Errorf(codes.NotFound, "volume path does not exist: %s", volumePath)
	}

	// Determine the requested capacity to echo back in the response.
	var capacityBytes int64
	if cr := req.GetCapacityRange(); cr != nil {
		capacityBytes = cr.GetRequiredBytes()
		if capacityBytes == 0 {
			capacityBytes = cr.GetLimitBytes()
		}
	}

	// Determine access type and filesystem type. The capability may be omitted
	// (the CSI spec allows the SP to infer the access type from the path); in
	// that case we default to a mounted filesystem volume, which is the only
	// access type this driver provisions.
	volumeCapability := req.GetVolumeCapability()
	isBlock := volumeCapability != nil && volumeCapability.GetBlock() != nil

	if isBlock {
		// Raw block volumes expose the device directly to the pod; there is no
		// filesystem to grow.
		s.logger.Info("Skipping filesystem resize for raw block volume",
			"volumeID", req.GetVolumeId(),
			"volumePath", volumePath)
		s.metrics.RecordNodeOperation("expand", time.Since(startTime).Seconds())
		return &csi.NodeExpandVolumeResponse{CapacityBytes: capacityBytes}, nil
	}

	// Filesystem volume: grow the filesystem to fill the (already-resized) device.
	fsType := DefaultFSType
	if volumeCapability != nil {
		if mount := volumeCapability.GetMount(); mount != nil && mount.GetFsType() != "" {
			fsType = mount.GetFsType()
		}
	}

	// Resolve the backing block device. Prefer the staging path (a direct
	// mount of the device) when provided; otherwise resolve from the volume
	// path by following the bind-mount chain.
	lookupPath := volumePath
	if sp := req.GetStagingTargetPath(); sp != "" {
		lookupPath = sp
	}

	devicePath, err := s.fs.FindDeviceForPath(lookupPath)
	if err != nil {
		// The volume's staging/target mount may not be visible in this
		// container's /proc/mounts. This happens when the volume was staged
		// before this driver pod started (e.g. after a node-pod restart):
		// the mount still exists on the host, so the kubelet does not
		// re-stage, but a freshly created container mount namespace does not
		// inherit pre-existing submounts of the bind-mounted kubelet dir.
		//
		// Fall back to resolving the backing device from the
		// VolumeAttachment, whose status.publishContext["serial"] was set by
		// ControllerPublishVolume. resize2fs operates on the block device
		// directly, so it does not matter that the filesystem is mounted only
		// on the host and not in this container's namespace.
		s.logger.Info("Backing mount not visible in /proc/mounts; resolving device via VolumeAttachment",
			"volumeID", req.GetVolumeId(),
			"lookupPath", lookupPath,
			"error", err)

		devicePath, err = s.resolveDeviceFromVolumeAttachment(ctx, req.GetVolumeId())
		if err != nil {
			s.logger.Error("Failed to resolve backing device for volume",
				"volumeID", req.GetVolumeId(),
				"lookupPath", lookupPath,
				"error", err)
			s.metrics.RecordNodeOperationError("expand", time.Since(startTime).Seconds(), "device_resolve_failed")
			return nil, status.Errorf(codes.FailedPrecondition,
				"could not resolve backing device for volume %s at %s: %v", req.GetVolumeId(), lookupPath, err)
		}
	}

	s.logger.Info("Resizing filesystem",
		"volumeID", req.GetVolumeId(),
		"devicePath", devicePath,
		"volumePath", volumePath,
		"fsType", fsType)

	if err := s.fs.ResizeFilesystem(ctx, devicePath, lookupPath, fsType); err != nil {
		s.logger.Error("Failed to resize filesystem",
			"volumeID", req.GetVolumeId(),
			"devicePath", devicePath,
			"fsType", fsType,
			"error", err)
		s.metrics.RecordNodeOperationError("expand", time.Since(startTime).Seconds(), "resize_failed")
		return nil, status.Errorf(codes.Internal, "resize filesystem on %s: %v", devicePath, err)
	}

	s.logger.Info("Filesystem resized successfully",
		"volumeID", req.GetVolumeId(),
		"devicePath", devicePath,
		"fsType", fsType,
		"capacityBytes", capacityBytes)

	s.metrics.RecordNodeOperation("expand", time.Since(startTime).Seconds())

	return &csi.NodeExpandVolumeResponse{CapacityBytes: capacityBytes}, nil
}

// resolveDeviceFromVolumeAttachment resolves the backing block device for a
// volume by reading the disk serial from the volume's VolumeAttachment.
//
// This is used as a fallback for NodeExpandVolume when the volume's staging
// mount is not visible in the driver container's /proc/mounts (which happens
// for volumes staged before the current driver pod started). The
// VolumeAttachment's status.attachmentMetadata["serial"] is populated by the
// external-attacher from the PublishContext returned by
// ControllerPublishVolume, and persists in the cluster, independent of any
// container's mount namespace.
//
// The returned device path follows the same convention as NodeStageVolume:
// /dev/disk/by-id/scsi-0QEMU_QEMU_HARDDISK_<serial>.
func (s *Service) resolveDeviceFromVolumeAttachment(ctx context.Context, volumeID string) (string, error) {
	// CreateVolume sets the CSI volume ID to "csi-" + <PVC name>, and
	// Kubernetes names a dynamically-provisioned PV after its PVC, so the PV
	// name is the volume ID without the "csi-" prefix.
	pvName := strings.TrimPrefix(volumeID, "csi-")
	if pvName == volumeID || pvName == "" {
		return "", fmt.Errorf("cannot derive PV name from volume ID %q (missing \"csi-\" prefix)", volumeID)
	}

	// Verify the PV's CSI volume handle matches, guarding against non-default
	// PV naming or a stale/mismatched volume ID.
	pv, err := s.kubeClient.CoreV1().PersistentVolumes().Get(ctx, pvName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("get persistent volume %s for volume %s: %w", pvName, volumeID, err)
	}
	if pv.Spec.CSI == nil || pv.Spec.CSI.VolumeHandle != volumeID {
		pvHandle := ""
		if pv.Spec.CSI != nil {
			pvHandle = pv.Spec.CSI.VolumeHandle
		}
		return "", fmt.Errorf("persistent volume %s handle %q does not match volume %q", pvName, pvHandle, volumeID)
	}

	// Find the VolumeAttachment for this PV on this node and read the serial
	// the controller recorded at publish time.
	vaList, err := s.kubeClient.StorageV1().VolumeAttachments().List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("list volume attachments: %w", err)
	}

	for _, va := range vaList.Items {
		if va.Spec.NodeName != s.nodeID {
			continue
		}
		if va.Spec.Source.PersistentVolumeName == nil || *va.Spec.Source.PersistentVolumeName != pvName {
			continue
		}
		serial := va.Status.AttachmentMetadata["serial"]
		if serial == "" {
			continue
		}

		devicePath := filepath.Join(s.deviceByIDPath, fmt.Sprintf("%s%s", QEMUSCSIDiskPrefix, serial))
		s.logger.Debug("Resolved device from VolumeAttachment",
			"volumeID", volumeID,
			"pvName", pvName,
			"serial", serial,
			"devicePath", devicePath)
		return devicePath, nil
	}

	return "", fmt.Errorf("no volume attachment with a disk serial found for volume %s on node %s", volumeID, s.nodeID)
}
