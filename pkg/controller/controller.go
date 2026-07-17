// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 evroc

package controller

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/evroc-oss/evroc-csi-driver/pkg/common"
	"github.com/evroc-oss/evroc-csi-driver/pkg/evroc"
	"github.com/evroc-oss/evroc-csi-driver/pkg/metrics"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Service implements the CSI Controller Service.
type Service struct {
	csi.UnimplementedControllerServer
	logger                    *slog.Logger
	storage                   evroc.StorageBackend
	metrics                   *metrics.Manager
	nodeLocks                 sync.Map
	totalCapacityGB           int64         // Total storage capacity quota in GB
	maxVolumesPerNode         int64         // Maximum volumes per node
	metricsCollectionInterval time.Duration // Interval for collecting attachment metrics
	restAPIMaxRetries         int           // Maximum retry attempts for REST API errors
	restAPIRetryInitialDelay  time.Duration // Initial retry delay for REST API errors
	restAPIRetryMaxDelay      time.Duration // Maximum retry delay with exponential backoff
}

// NewControllerService creates a new Controller Service.
// The storage parameter is required and must not be nil.
func NewControllerService(logger *slog.Logger, storage evroc.StorageBackend, totalCapacityGB, maxVolumesPerNode int64, metricsCollectionInterval time.Duration, restAPIMaxRetries int, restAPIRetryInitialDelay, restAPIRetryMaxDelay time.Duration, m *metrics.Manager) *Service {
	if storage == nil {
		panic("storage backend cannot be nil")
	}
	s := &Service{
		logger:                    logger,
		storage:                   storage,
		totalCapacityGB:           totalCapacityGB,
		maxVolumesPerNode:         maxVolumesPerNode,
		metricsCollectionInterval: metricsCollectionInterval,
		restAPIMaxRetries:         restAPIMaxRetries,
		restAPIRetryInitialDelay:  restAPIRetryInitialDelay,
		restAPIRetryMaxDelay:      restAPIRetryMaxDelay,
		metrics:                   m,
	}

	// Start periodic metrics collection for attachment state
	if m != nil && metricsCollectionInterval > 0 {
		go s.collectAttachmentMetrics()
	}

	return s
}

// collectAttachmentMetrics periodically queries actual attachment state and updates the gauge.
// This prevents the gauge from resetting on pod restart.
func (s *Service) collectAttachmentMetrics() {
	ticker := time.NewTicker(s.metricsCollectionInterval)
	defer ticker.Stop()

	for range ticker.C {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		attachments, err := s.storage.ListAttachments(ctx)
		cancel()

		if err != nil {
			s.logger.Warn("Failed to collect attachment metrics", "error", err)
			continue
		}

		// Count attachments by state
		attachedCount := 0
		for i := range attachments.Items {
			if evroc.IsAttachmentReady(&attachments.Items[i]) {
				attachedCount++
			}
		}

		// Set the gauge to the actual count (not increment/decrement)
		s.metrics.AttachmentsTotal.WithLabelValues("attached").Set(float64(attachedCount))
	}
}

// retryOnTransient retries an operation with exponential backoff for transient errors.
func (s *Service) retryOnTransient(ctx context.Context, operation string, fn func() error) error {
	var lastErr error
	delay := s.restAPIRetryInitialDelay

	for attempt := 0; attempt <= s.restAPIMaxRetries; attempt++ {
		if attempt > 0 {
			s.logger.Info("Retrying operation",
				"operation", operation,
				"attempt", attempt,
				"delay", delay)

			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return ctx.Err()
			}

			// Exponential backoff with cap
			delay *= 2
			if delay > s.restAPIRetryMaxDelay {
				delay = s.restAPIRetryMaxDelay
			}
		}

		err := fn()
		if err == nil {
			return nil
		}

		lastErr = err

		// Don't retry permanent errors (NotFound, InvalidArgument, AlreadyExists, etc.)
		if !isTransientError(err) {
			return err
		}

		s.logger.Warn("Operation failed with transient error, will retry",
			"operation", operation,
			"attempt", attempt,
			"error", err)
	}

	// Preserve gRPC status code from last error while adding retry context
	if st, ok := status.FromError(lastErr); ok {
		return status.Errorf(st.Code(), "operation %s failed after %d retries: %s", operation, s.restAPIMaxRetries, st.Message())
	}
	return fmt.Errorf("operation %s failed after %d retries: %w", operation, s.restAPIMaxRetries, lastErr)
}

// isTransientError determines if an error is transient and should be retried.
// Checks gRPC status codes (HTTP errors are converted to gRPC codes by REST client).
func isTransientError(err error) bool {
	if err == nil {
		return false
	}

	if st, ok := status.FromError(err); ok {
		switch st.Code() {
		case codes.Unavailable, codes.DeadlineExceeded, codes.ResourceExhausted, codes.Aborted, codes.Internal:
			return true
		case codes.NotFound, codes.InvalidArgument, codes.AlreadyExists,
			codes.PermissionDenied, codes.FailedPrecondition, codes.Unauthenticated:
			return false
		default:
			return false
		}
	}

	return false
}

// acquireNodeLock acquires a mutex for the given node ID and locks it.
// Returns the locked mutex or an error if acquisition fails.
// Caller is responsible for unlocking the mutex.
func (s *Service) acquireNodeLock(nodeID string) (*sync.Mutex, error) {
	mu, _ := s.nodeLocks.LoadOrStore(nodeID, &sync.Mutex{})
	nodeMutex, ok := mu.(*sync.Mutex)
	if !ok {
		return nil, status.Error(codes.Internal, "failed to acquire node lock")
	}
	nodeMutex.Lock()
	return nodeMutex, nil
}

// validateVolumeCapabilities validates volume capabilities and access modes.
// Returns error if capabilities are invalid. If volumeID is non-empty, logs warnings before errors.
func (s *Service) validateVolumeCapabilities(caps []*csi.VolumeCapability, volumeID string) error {
	for _, cap := range caps {
		// Check access mode is not nil
		if cap.GetAccessMode() == nil {
			return status.Error(codes.InvalidArgument, "access mode is required")
		}

		accessMode := cap.GetAccessMode().GetMode()

		// Check if access mode is supported
		if accessMode != csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER &&
			accessMode != csi.VolumeCapability_AccessMode_SINGLE_NODE_SINGLE_WRITER &&
			accessMode != csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY {
			if volumeID != "" {
				s.logger.Warn("Unsupported access mode", "volumeID", volumeID, "accessMode", accessMode)
			}
			return status.Errorf(codes.InvalidArgument,
				"unsupported access mode %v; only SINGLE_NODE_WRITER (RWO), SINGLE_NODE_SINGLE_WRITER (RWOPod), and SINGLE_NODE_READER_ONLY (ROX) are supported",
				accessMode)
		}

		// Check readonly + block combination
		// NOTE: this implementation only supports Readonly access mode for filesystem volumes.
		// Block devices would require setting up a readonly loop device, which is much more complex.
		if accessMode == csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY {
			if cap.GetBlock() != nil {
				if volumeID != "" {
					s.logger.Warn("Readonly not supported for block volumes", "volumeID", volumeID)
				}
				return status.Error(codes.InvalidArgument,
					"SINGLE_NODE_READER_ONLY (ReadOnlyMany) is only supported for filesystem volumes, not block volumes")
			}
		}
	}
	return nil
}

// CreateVolume creates a new volume.
func (s *Service) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	startTime := time.Now()
	s.logger.Debug("CreateVolume called",
		"name", req.GetName(),
		"capacityRange", req.GetCapacityRange(),
		"parameters", req.GetParameters())

	if err := common.ValidateRequiredField(req.GetName(), "volume name"); err != nil {
		return nil, err
	}

	// Validate volume capabilities are provided
	if len(req.GetVolumeCapabilities()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "volume capabilities are required")
	}

	// Validate access modes
	if err := s.validateVolumeCapabilities(req.GetVolumeCapabilities(), ""); err != nil {
		return nil, err
	}

	volumeID := "csi-" + req.GetName()

	// Handle capacity calculation correctly
	capacityRange := req.GetCapacityRange()
	capacityBytes := int64(0)

	if capacityRange != nil {
		capacityBytes = capacityRange.GetRequiredBytes()

		// If RequiredBytes is 0, use LimitBytes instead (CSI spec allows this)
		if capacityBytes == 0 {
			capacityBytes = capacityRange.GetLimitBytes()
		}
	}

	// Validate we have a valid capacity
	if capacityBytes == 0 {
		return nil, status.Error(codes.InvalidArgument,
			"capacity range must specify required_bytes or limit_bytes")
	}

	// Round UP to ensure disk is AT LEAST RequiredBytes
	// Formula: (capacityBytes + 1024*1024 - 1) / (1024*1024)
	// This ensures we never create a disk smaller than requested
	capacityMB := int32((capacityBytes + 1024*1024 - 1) / (1024 * 1024))

	s.logger.Debug("Calculated disk size",
		"requestedBytes", capacityBytes,
		"calculatedMB", capacityMB,
		"actualBytes", int64(capacityMB)*1024*1024)

	storageClass := req.GetParameters()["storageClass"]
	if storageClass == "" {
		storageClass = "persistent"
	}

	// Get zone from topology if available
	zone := ""
	if topReq := req.GetAccessibilityRequirements(); topReq != nil {
		for _, topo := range topReq.GetPreferred() {
			if z, ok := topo.GetSegments()["topology.kubernetes.io/zone"]; ok {
				zone = z
				break
			}
		}
	}

	// Create disk via storage backend
	s.logger.Info("Creating disk", "volumeID", volumeID, "sizeMB", capacityMB, "storageClass", storageClass, "zone", zone)
	err := s.retryOnTransient(ctx, "create disk", func() error {
		created, err := s.storage.EnsureDiskCreated(ctx, volumeID, capacityMB, storageClass, zone)
		if err != nil {
			return err
		}
		if created {
			s.logger.Info("Disk created", "volumeID", volumeID)
		} else {
			s.logger.Info("Disk already exists", "volumeID", volumeID)
		}
		return nil
	})
	if err != nil {
		s.logger.Error("Failed to create disk", "error", err)
		s.metrics.RecordVolumeOperationError("create", time.Since(startTime).Seconds(), "internal")
		s.metrics.UpdateVolumeState("error", 1)
		// Check if the error is already a gRPC status error (e.g. AlreadyExists)
		if st, ok := status.FromError(err); ok {
			return nil, st.Err()
		}
		return nil, status.Errorf(codes.Internal, "failed to create disk: %v", err)
	}

	s.metrics.RecordVolumeOperation("create", time.Since(startTime).Seconds())

	// Use actual capacity (after rounding up) for metrics and response
	actualCapacityBytes := int64(capacityMB) * 1024 * 1024
	s.metrics.SetVolumeSize(volumeID, float64(actualCapacityBytes))
	s.metrics.UpdateVolumeState("created", 1)

	// Build volume response
	volume := &csi.Volume{
		VolumeId:      volumeID,
		CapacityBytes: actualCapacityBytes, // Return actual size, not requested size
		VolumeContext: req.GetParameters(),
	}

	// Set accessible topology if zone was specified
	// This tells Kubernetes which zone(s) this volume can be accessed from
	if zone != "" {
		volume.AccessibleTopology = []*csi.Topology{
			{
				Segments: map[string]string{
					"topology.kubernetes.io/zone": zone,
				},
			},
		}
		s.logger.Info("Volume created with topology", "volumeID", volumeID, "zone", zone)
	}

	return &csi.CreateVolumeResponse{
		Volume: volume,
	}, nil
}

// DeleteVolume deletes a volume.
func (s *Service) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	startTime := time.Now()
	s.logger.Debug("DeleteVolume called", "volumeID", req.GetVolumeId())

	if err := common.ValidateRequiredField(req.GetVolumeId(), "volume ID"); err != nil {
		return nil, err
	}

	// Check for active attachments before deleting
	attachmentList, err := s.storage.ListAttachments(ctx)
	if err != nil {
		s.logger.Error("Failed to list attachments", "error", err)
		return nil, status.Errorf(codes.Internal, "failed to check attachments before delete: %v", err)
	}

	var attachedNodes []string
	for _, attachment := range attachmentList.Items {
		if attachment.Spec.DiskRef == req.GetVolumeId() {
			attachedNodes = append(attachedNodes, attachment.Spec.VirtualMachineRef)
		}
	}

	if len(attachedNodes) > 0 {
		s.logger.Error("Cannot delete volume with active attachments",
			"volumeID", req.GetVolumeId(),
			"attachedNodes", attachedNodes)
		return nil, status.Errorf(codes.FailedPrecondition,
			"volume %s has active attachments to nodes %v; must be unpublished before deletion",
			req.GetVolumeId(), attachedNodes)
	}

	s.logger.Info("Deleting disk", "volumeID", req.GetVolumeId())
	err = s.retryOnTransient(ctx, "delete disk", func() error {
		return s.storage.EnsureDiskDeleted(ctx, req.GetVolumeId())
	})
	if err != nil {
		s.logger.Error("Failed to delete disk", "error", err)
		s.metrics.RecordVolumeOperationError("delete", time.Since(startTime).Seconds(), "internal")
		return nil, status.Errorf(codes.Internal, "failed to delete disk: %v", err)
	}
	s.logger.Info("Disk deleted", "volumeID", req.GetVolumeId())

	s.metrics.RecordVolumeOperation("delete", time.Since(startTime).Seconds())
	s.metrics.DeleteVolumeSize(req.GetVolumeId())
	s.metrics.UpdateVolumeState("created", -1)

	return &csi.DeleteVolumeResponse{}, nil
}

// ControllerPublishVolume attaches a volume to a node.
func (s *Service) ControllerPublishVolume(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
	startTime := time.Now()
	s.logger.Debug("ControllerPublishVolume called",
		"volumeID", req.GetVolumeId(),
		"nodeID", req.GetNodeId())

	if err := common.ValidateRequiredField(req.GetVolumeId(), "volume ID"); err != nil {
		return nil, err
	}
	if err := common.ValidateRequiredField(req.GetNodeId(), "node ID"); err != nil {
		return nil, err
	}
	if req.GetVolumeCapability() == nil {
		return nil, status.Error(codes.InvalidArgument, "volume capability is required")
	}

	// Lock attach operation per node to ensure only one attach/detach
	// can run simultaneously on a given node.
	nodeMutex, err := s.acquireNodeLock(req.GetNodeId())
	if err != nil {
		return nil, err
	}
	defer nodeMutex.Unlock()

	// Check MaxVolumesPerNode limit after acquiring lock to prevent race condition
	if s.maxVolumesPerNode > 0 {
		attachmentCount, err := s.storage.CountNodeAttachments(ctx, req.GetNodeId())
		if err != nil {
			s.logger.Error("Failed to count node attachments", "error", err)
			return nil, status.Errorf(codes.Internal, "failed to check volume attach limit: %v", err)
		}

		if int64(attachmentCount) >= s.maxVolumesPerNode {
			s.logger.Warn("Node has reached max volume attach limit",
				"nodeID", req.GetNodeId(),
				"currentCount", attachmentCount,
				"maxVolumesPerNode", s.maxVolumesPerNode)
			return nil, status.Errorf(codes.ResourceExhausted,
				"node %s has reached maximum volume attach limit (%d volumes)",
				req.GetNodeId(), s.maxVolumesPerNode)
		}
	}

	s.logger.Info("Creating disk attachment",
		"volumeID", req.GetVolumeId(),
		"vmName", req.GetNodeId())

	err = s.retryOnTransient(ctx, "create attachment", func() error {
		return s.storage.EnsureAttachmentCreated(ctx, req.GetVolumeId(), req.GetNodeId())
	})
	if err != nil {
		s.logger.Error("Failed to create disk attachment", "error", err)
		s.metrics.RecordVolumeOperationError("attach", time.Since(startTime).Seconds(), "internal")
		// Check if the error is already a gRPC status error (e.g. NotFound)
		if st, ok := status.FromError(err); ok {
			return nil, st.Err()
		}
		return nil, status.Errorf(codes.Internal, "failed to create disk attachment: %v", err)
	}

	s.logger.Info("Disk attachment resource created, waiting for serial",
		"volumeID", req.GetVolumeId(),
		"vmName", req.GetNodeId())

	// Wait for the serial to be populated by the controller
	serial, err := s.storage.WaitForAttachmentSerial(ctx, req.GetVolumeId(), req.GetNodeId())
	if err != nil {
		s.logger.Error("Failed to get serial", "error", err)
		return nil, status.Errorf(codes.Unavailable, "failed to get serial: %v", err)
	}

	s.logger.Info("Serial retrieved",
		"volumeID", req.GetVolumeId(),
		"vmName", req.GetNodeId(),
		"serial", serial)

	s.metrics.RecordVolumeOperation("attach", time.Since(startTime).Seconds())
	s.metrics.UpdateAttachmentState("attached", 1)

	// Return the serial in PublishContext so the node can find the device
	return &csi.ControllerPublishVolumeResponse{
		PublishContext: map[string]string{
			"serial": serial,
		},
	}, nil
}

// ControllerUnpublishVolume detaches a volume from a node.
func (s *Service) ControllerUnpublishVolume(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
	startTime := time.Now()
	s.logger.Debug("ControllerUnpublishVolume called",
		"volumeID", req.GetVolumeId(),
		"nodeID", req.GetNodeId())

	if err := common.ValidateRequiredField(req.GetVolumeId(), "volume ID"); err != nil {
		return nil, err
	}
	if err := common.ValidateRequiredField(req.GetNodeId(), "node ID"); err != nil {
		return nil, err
	}

	// Lock detach operation per node to ensure only one attach/detach
	// can run simultaneously on a given node.
	nodeMutex, err := s.acquireNodeLock(req.GetNodeId())
	if err != nil {
		return nil, err
	}
	defer nodeMutex.Unlock()

	// Delete disk attachment via storage backend
	s.logger.Info("Deleting disk attachment",
		"volumeID", req.GetVolumeId(),
		"vmName", req.GetNodeId())

	err = s.retryOnTransient(ctx, "delete attachment", func() error {
		return s.storage.EnsureAttachmentDeleted(ctx, req.GetVolumeId(), req.GetNodeId())
	})
	if err != nil {
		s.logger.Error("Failed to delete disk attachment", "error", err)
		s.metrics.RecordVolumeOperationError("detach", time.Since(startTime).Seconds(), "internal")
		return nil, status.Errorf(codes.Internal, "failed to delete disk attachment: %v", err)
	}

	s.logger.Info("Disk attachment deleted",
		"volumeID", req.GetVolumeId(),
		"vmName", req.GetNodeId())

	s.metrics.RecordVolumeOperation("detach", time.Since(startTime).Seconds())
	s.metrics.UpdateAttachmentState("attached", -1)

	return &csi.ControllerUnpublishVolumeResponse{}, nil
}

// ValidateVolumeCapabilities checks if a volume has the requested capabilities.
func (s *Service) ValidateVolumeCapabilities(ctx context.Context, req *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {
	s.logger.Debug("ValidateVolumeCapabilities called",
		"volumeID", req.GetVolumeId(),
		"capabilities", req.GetVolumeCapabilities())

	if err := common.ValidateRequiredField(req.GetVolumeId(), "volume ID"); err != nil {
		return nil, err
	}

	if len(req.GetVolumeCapabilities()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "volume capabilities are required")
	}

	// Check if volume exists
	_, err := s.storage.GetDisk(ctx, req.GetVolumeId())
	if err != nil {
		s.logger.Error("Failed to get disk", "volumeID", req.GetVolumeId(), "error", err)
		return nil, status.Errorf(codes.NotFound, "volume %s does not exist", req.GetVolumeId())
	}

	// Validate access modes - support RWO, RWOPod, and ROX
	if err := s.validateVolumeCapabilities(req.GetVolumeCapabilities(), req.GetVolumeId()); err != nil {
		return nil, err
	}

	return &csi.ValidateVolumeCapabilitiesResponse{
		Confirmed: &csi.ValidateVolumeCapabilitiesResponse_Confirmed{
			VolumeCapabilities: req.GetVolumeCapabilities(),
		},
	}, nil
}

// ListVolumes lists all volumes.
func (s *Service) ListVolumes(ctx context.Context, req *csi.ListVolumesRequest) (*csi.ListVolumesResponse, error) {
	s.logger.Debug("ListVolumes called",
		"maxEntries", req.GetMaxEntries(),
		"startingToken", req.GetStartingToken())

	// Parse starting token (if provided)
	startIndex := 0
	if req.GetStartingToken() != "" {
		// Starting token is the index to start from
		var err error
		startIndex, err = strconv.Atoi(req.GetStartingToken())
		if err != nil || startIndex < 0 {
			return nil, status.Errorf(codes.Aborted, "invalid starting_token: %s", req.GetStartingToken())
		}
	}

	// List all disks managed by this CSI driver
	diskList, err := s.storage.ListDisks(ctx)
	if err != nil {
		s.logger.Error("Failed to list disks", "error", err)
		return nil, status.Errorf(codes.Internal, "failed to list disks: %v", err)
	}

	// List all attachments to get published node information
	attachmentList, err := s.storage.ListAttachments(ctx)
	if err != nil {
		s.logger.Error("Failed to list attachments", "error", err)
		return nil, status.Errorf(codes.Internal, "failed to list attachments: %v", err)
	}

	// Build a map of disk name -> list of VM names (only for ready attachments)
	diskToVMs := make(map[string][]string)
	for _, attachment := range attachmentList.Items {
		// Only include ready attachments
		if !evroc.IsAttachmentReady(&attachment) {
			continue
		}
		diskName := attachment.Spec.DiskRef
		// Extract node name from full VM reference path for CSI node ID compatibility
		// VMRef is "/compute/projects/.../virtualMachines/vm-name", extract "vm-name"
		vmName := evroc.ExtractResourceName(attachment.Spec.VirtualMachineRef)
		diskToVMs[diskName] = append(diskToVMs[diskName], vmName)
	}

	// Build all entries
	var allEntries []*csi.ListVolumesResponse_Entry
	for _, disk := range diskList.Items {
		volumeID := disk.Metadata.Id
		var capacityBytes int64
		if disk.Spec.DiskSize != nil {
			capacityBytes = int64(disk.Spec.DiskSize.Amount) * 1024 * 1024
		}

		entry := &csi.ListVolumesResponse_Entry{
			Volume: &csi.Volume{
				VolumeId:      volumeID,
				CapacityBytes: capacityBytes,
			},
		}

		// Add published node IDs if the volume is attached to any VMs
		if vms, ok := diskToVMs[volumeID]; ok && len(vms) > 0 {
			entry.Status = &csi.ListVolumesResponse_VolumeStatus{
				PublishedNodeIds: vms,
			}
		}

		allEntries = append(allEntries, entry)
	}

	// Handle pagination
	totalVolumes := len(allEntries)

	// If starting index is beyond the list, return empty
	if startIndex >= totalVolumes {
		return &csi.ListVolumesResponse{
			Entries: []*csi.ListVolumesResponse_Entry{},
		}, nil
	}

	// Determine end index based on maxEntries
	endIndex := totalVolumes
	var nextToken string

	if req.GetMaxEntries() > 0 {
		maxEntries := int(req.GetMaxEntries())
		if startIndex+maxEntries < totalVolumes {
			endIndex = startIndex + maxEntries
			nextToken = strconv.Itoa(endIndex)
		}
	}

	// Slice the entries for this page
	entries := allEntries[startIndex:endIndex]

	s.logger.Debug("Listed volumes",
		"total", totalVolumes,
		"returned", len(entries),
		"startIndex", startIndex,
		"endIndex", endIndex,
		"nextToken", nextToken)

	return &csi.ListVolumesResponse{
		Entries:   entries,
		NextToken: nextToken,
	}, nil
}

// GetCapacity returns the available capacity.
func (s *Service) GetCapacity(ctx context.Context, req *csi.GetCapacityRequest) (*csi.GetCapacityResponse, error) {
	s.logger.Debug("GetCapacity called", "parameters", req.GetParameters())

	// List all disks managed by this CSI driver to calculate used capacity
	diskList, err := s.storage.ListDisks(ctx)
	if err != nil {
		s.logger.Error("Failed to list disks for capacity calculation", "error", err)
		return nil, status.Errorf(codes.Internal, "failed to query disk usage: %v", err)
	}

	// Calculate total used capacity
	var usedBytes int64
	for _, disk := range diskList.Items {
		if disk.Spec.DiskSize != nil {
			usedBytes += int64(disk.Spec.DiskSize.Amount) * 1024 * 1024
		}
	}

	// Use configured total capacity (cannot be queried from API currently)
	totalBytes := s.totalCapacityGB * 1024 * 1024 * 1024

	availableBytes := totalBytes - usedBytes
	if availableBytes < 0 {
		availableBytes = 0
	}

	s.logger.Debug("Calculated storage capacity",
		"totalBytes", totalBytes,
		"usedBytes", usedBytes,
		"availableBytes", availableBytes,
		"diskCount", len(diskList.Items))

	return &csi.GetCapacityResponse{
		AvailableCapacity: availableBytes,
	}, nil
}

// ControllerGetCapabilities returns the capabilities of the controller.
func (s *Service) ControllerGetCapabilities(ctx context.Context, req *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {
	s.logger.Debug("ControllerGetCapabilities called")

	return &csi.ControllerGetCapabilitiesResponse{
		Capabilities: []*csi.ControllerServiceCapability{
			{
				Type: &csi.ControllerServiceCapability_Rpc{
					Rpc: &csi.ControllerServiceCapability_RPC{
						Type: csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
					},
				},
			},
			{
				Type: &csi.ControllerServiceCapability_Rpc{
					Rpc: &csi.ControllerServiceCapability_RPC{
						Type: csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME,
					},
				},
			},
			{
				Type: &csi.ControllerServiceCapability_Rpc{
					Rpc: &csi.ControllerServiceCapability_RPC{
						Type: csi.ControllerServiceCapability_RPC_LIST_VOLUMES,
					},
				},
			},
			{
				Type: &csi.ControllerServiceCapability_Rpc{
					Rpc: &csi.ControllerServiceCapability_RPC{
						Type: csi.ControllerServiceCapability_RPC_LIST_VOLUMES_PUBLISHED_NODES,
					},
				},
			},
			{
				Type: &csi.ControllerServiceCapability_Rpc{
					Rpc: &csi.ControllerServiceCapability_RPC{
						Type: csi.ControllerServiceCapability_RPC_GET_CAPACITY,
					},
				},
			},
		},
	}, nil
}
