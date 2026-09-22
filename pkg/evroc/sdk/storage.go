// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 evroc

package sdk

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	evroc "github.com/evroc-oss/evroc-go-sdk"
	"github.com/evroc-oss/evroc-go-sdk/compute"
	computetypes "github.com/evroc-oss/evroc-go-sdk/types/compute"

	"github.com/evroc-oss/evroc-csi-driver/pkg/config"
	evrocpkg "github.com/evroc-oss/evroc-csi-driver/pkg/evroc"
	"github.com/evroc-oss/evroc-csi-driver/pkg/metrics"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	managedByLabel      = "managed-by"
	attachmentDiskLabel = "compute.evroc.com/disk-attachment-disk"
)

var _ evrocpkg.StorageBackend = (*SDKStorageBackend)(nil)

// labelFilter implements the SDK's rest.ListFilter interface via Go structural typing.
type labelFilter struct {
	labels map[string]string
}

func (f labelFilter) Apply(v url.Values) {
	if len(f.labels) == 0 {
		return
	}
	var parts []string
	for k, val := range f.labels {
		parts = append(parts, fmt.Sprintf("%s=%s", k, val))
	}
	v.Set("labelSelector", strings.Join(parts, ","))
}

// SDKStorageBackend implements evroc.StorageBackend using the evroc-go-sdk.
type SDKStorageBackend struct {
	client                 *evroc.Client
	identifier             string
	logger                 *slog.Logger
	metrics                *metrics.Manager
	attachmentPollTimeout  time.Duration
	diskResizePollTimeout  time.Duration
	diskResizePollInterval time.Duration
}

// NewSDKStorageBackend creates a StorageBackend backed by the evroc-go-sdk.
func NewSDKStorageBackend(ctx context.Context, cfg *config.Config, logger *slog.Logger, metricsManager *metrics.Manager) (*SDKStorageBackend, error) {
	client, err := evroc.New(ctx, cfg.SDKConfig())
	if err != nil {
		return nil, fmt.Errorf("create evroc SDK client: %w", err)
	}

	identifier := cfg.CSI.Identifier
	if identifier == "" {
		identifier = cfg.Evroc.Project
	}

	logger.Info("SDK storage backend initialized")

	return &SDKStorageBackend{
		client:                 client,
		identifier:             identifier,
		logger:                 logger,
		metrics:                metricsManager,
		attachmentPollTimeout:  cfg.CSI.AttachmentPollTimeout,
		diskResizePollTimeout:  cfg.CSI.DiskResizePollTimeout,
		diskResizePollInterval: cfg.CSI.DiskResizePollInterval,
	}, nil
}

func (s *SDKStorageBackend) managedByLabels() map[string]string {
	return map[string]string{managedByLabel: s.identifier}
}

func (s *SDKStorageBackend) managedByFilter() labelFilter {
	return labelFilter{labels: s.managedByLabels()}
}

func (s *SDKStorageBackend) diskAttachmentsFilter(diskName string) labelFilter {
	return labelFilter{labels: map[string]string{
		managedByLabel:      s.identifier,
		attachmentDiskLabel: diskName,
	}}
}

func (s *SDKStorageBackend) EnsureDiskCreated(ctx context.Context, name string, sizeMB int32, storageClass, zone string) (bool, error) {
	startTime := time.Now()

	s.logger.Info("Ensuring disk exists", "name", name, "sizeMB", sizeMB, "storageClass", storageClass, "zone", zone)

	existing, err := s.client.Compute().Disks().Get(ctx, name)
	if err == nil {
		s.logger.Info("Disk already exists (from GET), validating parameters", "name", name)
		if err := s.validateExistingDisk(existing, name, sizeMB, startTime); err != nil {
			return false, err
		}
		s.recordAPISuccess("CreateDisk", startTime)
		return false, nil
	}

	if !isNotFoundOrForbidden(err) {
		s.logger.Info("GET check failed, will try POST", "error", err.Error())
	}

	diskReq := compute.NewDiskBuilder(name).
		WithSize(sizeMB, compute.DiskSizeUnit("MB")).
		WithZone(zone).
		WithLabels(s.managedByLabels()).
		Build()

	_, err = s.client.Compute().Disks().Create(ctx, diskReq)
	if err != nil {
		if errors.Is(err, evroc.ErrConflict) {
			s.logger.Info("Disk already exists (from POST conflict), will validate", "name", name)
			conflictDisk, getErr := s.client.Compute().Disks().Get(ctx, name)
			if getErr != nil {
				s.recordAPIError("CreateDisk", startTime, getErr)
				return false, sdkErrorToGRPC(getErr)
			}
			if err := s.validateExistingDisk(conflictDisk, name, sizeMB, startTime); err != nil {
				return false, err
			}
			s.recordAPISuccess("CreateDisk", startTime)
			return false, nil
		}
		s.recordAPIError("CreateDisk", startTime, err)
		return false, sdkErrorToGRPC(err)
	}

	s.recordAPISuccess("CreateDisk", startTime)
	s.logger.Info("Disk created successfully", "name", name)
	return true, nil
}

func (s *SDKStorageBackend) EnsureDiskDeleted(ctx context.Context, name string) error {
	startTime := time.Now()

	s.logger.Info("Ensuring disk is deleted", "name", name)

	existing, err := s.client.Compute().Disks().Get(ctx, name)
	if isNotFoundOrForbidden(err) {
		s.logger.Info("Disk already deleted", "name", name)
		s.recordAPISuccess("DeleteDisk", startTime)
		return nil
	}
	if err != nil {
		s.recordAPIError("DeleteDisk", startTime, err)
		return sdkErrorToGRPC(err)
	}

	if err := s.validateOwnership(sdkUserLabels(existing.Metadata.UserLabels), "disk", name, "delete", "DeleteDisk", startTime); err != nil {
		return err
	}

	err = s.client.Compute().Disks().Delete(ctx, name)
	if err == nil || isNotFoundOrForbidden(err) {
		s.logger.Info("Disk deleted successfully", "name", name)
		s.recordAPISuccess("DeleteDisk", startTime)
		return nil
	}

	s.recordAPIError("DeleteDisk", startTime, err)
	return sdkErrorToGRPC(err)
}

func (s *SDKStorageBackend) GetDisk(ctx context.Context, name string) (*computetypes.Disk, error) {
	startTime := time.Now()

	s.logger.Info("Getting disk", "name", name)

	disk, err := s.client.Compute().Disks().Get(ctx, name)
	if err != nil {
		if isNotFoundOrForbidden(err) {
			s.logger.Info("Disk not found", "name", name)
		}
		s.recordAPIError("GetDisk", startTime, err)
		return nil, sdkErrorToGRPC(err)
	}

	if err := s.validateOwnership(sdkUserLabels(disk.Metadata.UserLabels), "disk", name, "get", "GetDisk", startTime); err != nil {
		return nil, err
	}

	s.logger.Info("Disk retrieved successfully", "name", name)
	s.recordAPISuccess("GetDisk", startTime)
	return disk, nil
}

func (s *SDKStorageBackend) ListDisks(ctx context.Context) (*compute.DiskList, error) {
	startTime := time.Now()

	s.logger.Info("Listing disks with label selector", "labelSelector", fmt.Sprintf("%s=%s", managedByLabel, s.identifier))

	result, err := s.client.Compute().Disks().List(ctx, s.managedByFilter())
	if err != nil {
		s.recordAPIError("ListDisks", startTime, err)
		return nil, sdkErrorToGRPC(err)
	}

	s.logger.Info("Disks listed successfully", "count", len(result.Items))
	s.recordAPISuccess("ListDisks", startTime)
	return result, nil
}

func (s *SDKStorageBackend) GetAttachment(ctx context.Context, diskName, vmName string) (*computetypes.HotswapDiskAttachment, error) {
	startTime := time.Now()
	attName := attachmentName(diskName, vmName)

	s.logger.Info("Getting attachment", "diskName", diskName, "vmName", vmName, "attachmentName", attName)

	att, err := s.client.Compute().HotswapDiskAttachments().Get(ctx, attName)
	if err != nil {
		if isNotFoundOrForbidden(err) {
			s.logger.Info("Attachment not found", "name", attName)
		}
		s.recordAPIError("GetAttachment", startTime, err)
		return nil, sdkErrorToGRPC(err)
	}

	if err := s.validateOwnership(sdkUserLabels(att.Metadata.UserLabels), "attachment", attName, "get", "GetAttachment", startTime); err != nil {
		return nil, err
	}

	s.logger.Info("Attachment retrieved successfully", "name", attName, "serial", att.Status.Serial)
	s.recordAPISuccess("GetAttachment", startTime)
	return att, nil
}

func (s *SDKStorageBackend) ListAttachments(ctx context.Context) (*compute.HotswapDiskAttachmentList, error) {
	startTime := time.Now()

	s.logger.Info("Listing disk attachments with label selector", "labelSelector", fmt.Sprintf("%s=%s", managedByLabel, s.identifier))

	result, err := s.client.Compute().HotswapDiskAttachments().List(ctx, s.managedByFilter())
	if err != nil {
		s.recordAPIError("ListAttachments", startTime, err)
		return nil, sdkErrorToGRPC(err)
	}

	s.logger.Info("Attachments listed successfully", "count", len(result.Items))
	s.recordAPISuccess("ListAttachments", startTime)
	return result, nil
}

func (s *SDKStorageBackend) CountNodeAttachments(ctx context.Context, vmName string) (int, error) {
	s.logger.Info("Counting attachments for VM", "vmName", vmName)

	attachmentList, err := s.ListAttachments(ctx)
	if err != nil {
		return 0, err
	}

	count := 0
	for _, attachment := range attachmentList.Items {
		attachedVM := evrocpkg.ExtractResourceName(attachment.Spec.VirtualMachineRef)
		if attachedVM == vmName {
			count++
		}
	}

	s.logger.Info("Counted attachments for VM", "vmName", vmName, "count", count)
	return count, nil
}

func (s *SDKStorageBackend) EnsureAttachmentCreated(ctx context.Context, diskName, vmName string) error {
	startTime := time.Now()
	attName := attachmentName(diskName, vmName)

	s.logger.Info("Ensuring attachment exists", "diskName", diskName, "vmName", vmName, "attachmentName", attName)

	// Kubernetes is authoritative for the requested node. Heal the rare case
	// where a single stale attachment remains on the previous node by deleting
	// it before creating the normally named attachment below.
	if err := s.ensureNoOtherAttachmentsUseThisDisk(ctx, diskName, vmName, startTime); err != nil {
		return err
	}

	diskRef := s.client.Compute().DiskRef(diskName)
	vmRef := s.client.Compute().VMRef(vmName)

	attReq := compute.NewHotswapDiskAttachmentBuilder(attName, vmRef, diskRef).
		WithLabels(s.managedByLabels()).
		Build()

	_, err := s.client.Compute().HotswapDiskAttachments().Create(ctx, attReq)
	if err != nil {
		if errors.Is(err, evroc.ErrConflict) {
			existing, getErr := s.client.Compute().HotswapDiskAttachments().Get(ctx, attName)
			if getErr != nil {
				return status.Errorf(codes.Aborted, "attachment %s conflicted but could not be verified: %v", attName, getErr)
			}
			if err := s.validateOwnership(sdkUserLabels(existing.Metadata.UserLabels), "attachment", attName, "get", "CreateAttachment", startTime); err != nil {
				return err
			}
			if evrocpkg.ExtractResourceName(existing.Spec.DiskRef) != diskName ||
				evrocpkg.ExtractResourceName(existing.Spec.VirtualMachineRef) != vmName {
				return status.Errorf(codes.FailedPrecondition,
					"attachment %s exists but does not target disk %s on VM %s", attName, diskName, vmName)
			}
			s.recordAPISuccess("CreateAttachment", startTime)
			return nil
		}
		s.recordAPIError("CreateAttachment", startTime, err)
		return sdkErrorToGRPC(err)
	}

	s.logger.Info("Attachment created successfully", "name", attName)
	s.recordAPISuccess("CreateAttachment", startTime)
	return nil
}

func (s *SDKStorageBackend) WaitForAttachmentSerial(ctx context.Context, diskName, vmName string) (string, error) {
	attName := attachmentName(diskName, vmName)

	s.logger.Info("Waiting for serial", "diskName", diskName, "vmName", vmName, "attachmentName", attName)

	readyAtt, err := s.client.Compute().HotswapDiskAttachments().WaitForReady(ctx, attName, s.attachmentPollTimeout)
	if err != nil {
		s.logger.Error("Timeout or error waiting for attachment ready", "diskName", diskName, "vmName", vmName, "error", err)
		return "", status.Errorf(codes.Unavailable, "attachment not ready after waiting %v: %v", s.attachmentPollTimeout, err)
	}

	if readyAtt.Status.Serial == nil || *readyAtt.Status.Serial == "" {
		return "", status.Errorf(codes.Unavailable, "attachment ready but serial is empty for %s", attName)
	}

	s.logger.Info("Serial retrieved", "diskName", diskName, "vmName", vmName, "serial", *readyAtt.Status.Serial)
	return *readyAtt.Status.Serial, nil
}

func (s *SDKStorageBackend) EnsureAttachmentDeleted(ctx context.Context, diskName, vmName string) error {
	startTime := time.Now()
	attName := attachmentName(diskName, vmName)

	s.logger.Info("Ensuring attachment is deleted", "diskName", diskName, "vmName", vmName, "attachmentName", attName)

	existing, err := s.client.Compute().HotswapDiskAttachments().Get(ctx, attName)
	if errors.Is(err, evroc.ErrNotFound) {
		s.logger.Info("Attachment already deleted", "name", attName)
		s.recordAPISuccess("DeleteAttachment", startTime)
		return nil
	}
	if err != nil {
		s.recordAPIError("DeleteAttachment", startTime, err)
		return sdkErrorToGRPC(err)
	}

	if err := s.validateOwnership(sdkUserLabels(existing.Metadata.UserLabels), "attachment", attName, "delete", "DeleteAttachment", startTime); err != nil {
		return err
	}

	err = s.client.Compute().HotswapDiskAttachments().Delete(ctx, attName)
	if errors.Is(err, evroc.ErrNotFound) {
		s.logger.Info("Attachment deleted successfully", "name", attName)
		s.recordAPISuccess("DeleteAttachment", startTime)
		return nil
	}
	if err != nil {
		s.recordAPIError("DeleteAttachment", startTime, err)
		return sdkErrorToGRPC(err)
	}
	if err := s.client.Compute().HotswapDiskAttachments().WaitForDeleted(ctx, attName, s.attachmentPollTimeout); err != nil {
		s.recordAPIError("DeleteAttachment", startTime, err)
		return status.Errorf(codes.Unavailable, "attachment %s was not deleted: %v", attName, err)
	}

	s.logger.Info("Attachment deleted successfully", "name", attName)
	s.recordAPISuccess("DeleteAttachment", startTime)
	return nil
}

func (s *SDKStorageBackend) EnsureDiskResized(ctx context.Context, name string, sizeMB int32) error {
	startTime := time.Now()

	s.logger.Info("Ensuring disk is resized", "name", name, "sizeMB", sizeMB)

	disk, err := s.GetDisk(ctx, name)
	if err != nil {
		s.logger.Info("Failed to get disk", "name", name)
		return err
	}

	// If the disk status already reports the requested size, the resize has
	// already been applied (e.g. a previous call succeeded but the response
	// was lost). Skip the Patch and return early.
	if diskStatusMatchesSize(disk, sizeMB) {
		s.logger.Info("Disk already at requested size", "name", name, "sizeMB", sizeMB)
		s.recordAPISuccess("ResizeDisk", startTime)
		return nil
	}

	// Shrinking a disk is not supported. Reject the request with an
	// explicit error rather than relying on the Patch request to fail,
	// which would surface an opaque backend error to the caller.
	if disk.Status.DiskSize != nil {
		currentMB := diskSizeToMB(disk.Status.DiskSize.Amount, disk.Status.DiskSize.Unit)
		if sizeMB < currentMB {
			err := status.Errorf(codes.OutOfRange,
				"disk %s cannot be shrunk from %dMB to %dMB: shrinking is not supported",
				name, currentMB, sizeMB)
			s.recordAPIErrorWithType("ResizeDisk", startTime, "out_of_range")
			return err
		}
	}
	if disk.Spec.DiskSize == nil {
		disk.Spec.DiskSize = &computetypes.DiskSpecDiskSize{}
	}

	disk.Spec.DiskSize.Amount = sizeMB
	disk.Spec.DiskSize.Unit = computetypes.DiskSpecDiskSizeUnitMB

	_, err = s.client.Compute().Disks().Patch(ctx, name, disk)
	if err != nil {
		// Requeue on conflicts
		if errors.Is(err, evroc.ErrConflict) {
			s.logger.Info("Conflict on PATCH when resizing disk", "name", name)
			s.recordAPIError("ResizeDisk", startTime, err)
			return status.Errorf(codes.Aborted, "%v", err)
		}
		s.logger.Info("Unexpected error when resizing disk", "name", name)
		s.recordAPIError("ResizeDisk", startTime, err)
		return sdkErrorToGRPC(err)
	}

	// The Patch request updates the spec; the backend reconciles the disk
	// and eventually reports the new size in status. Poll until the status
	// reflects the requested size so that the subsequent NodeExpandVolume
	// sees the enlarged block device.
	if err := s.waitForDiskResize(ctx, name, sizeMB, startTime); err != nil {
		return err
	}

	s.recordAPISuccess("ResizeDisk", startTime)
	s.logger.Info("Disk resized successfully", "name", name, "sizeMB", sizeMB)
	return nil
}

// waitForDiskResize polls the disk's status until the reported disk size
// matches the requested size, or the resize poll timeout is reached.
func (s *SDKStorageBackend) waitForDiskResize(ctx context.Context, name string, sizeMB int32, startTime time.Time) error {
	deadline := time.Now().Add(s.diskResizePollTimeout)
	ticker := time.NewTicker(s.diskResizePollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return status.Errorf(codes.DeadlineExceeded,
				"context cancelled while waiting for disk %s to resize to %dMB: %v",
				name, sizeMB, ctx.Err())
		case <-ticker.C:
		}

		if time.Now().After(deadline) {
			s.recordAPIError("ResizeDisk", startTime, fmt.Errorf("timeout"))
			return status.Errorf(codes.DeadlineExceeded,
				"disk %s did not report size %dMB within %v",
				name, sizeMB, s.diskResizePollTimeout)
		}

		disk, err := s.client.Compute().Disks().Get(ctx, name)
		if err != nil {
			s.logger.Debug("Error getting disk during resize poll; will retry",
				"name", name, "error", err)
			continue
		}

		if diskStatusMatchesSize(disk, sizeMB) {
			s.logger.Info("Disk status reports requested size",
				"name", name, "sizeMB", sizeMB)
			return nil
		}

		s.logger.Debug("Disk status does not yet reflect requested size; will retry",
			"name", name,
			"requestedMB", sizeMB,
			"pollInterval", s.diskResizePollInterval)
	}
}

// diskStatusMatchesSize returns true if the disk's status.DiskSize (converted
// to MiB) matches the requested size in MiB.
func diskStatusMatchesSize(disk *computetypes.Disk, sizeMB int32) bool {
	if disk == nil || disk.Status.DiskSize == nil {
		return false
	}
	return diskSizeToMB(disk.Status.DiskSize.Amount, disk.Status.DiskSize.Unit) == sizeMB
}

// diskSizeToMB converts a disk size amount+unit to MiB.
func diskSizeToMB(amount int32, unit computetypes.DiskStatusDiskSizeUnit) int32 {
	switch unit {
	case computetypes.DiskStatusDiskSizeUnitMB:
		return amount
	case computetypes.DiskStatusDiskSizeUnitGB:
		return amount * 1024
	case computetypes.DiskStatusDiskSizeUnitTB:
		return amount * 1024 * 1024
	case computetypes.DiskStatusDiskSizeUnitKB:
		return amount / 1024
	default:
		return amount
	}
}

// --- Helpers ---

// ensureNoOtherAttachmentsUseThisDisk removes a single stale attachment from
// another VM. Multiple attachments are ambiguous and are left untouched.
func (s *SDKStorageBackend) ensureNoOtherAttachmentsUseThisDisk(ctx context.Context, diskName, vmName string, startTime time.Time) error {
	attachmentList, err := s.client.Compute().HotswapDiskAttachments().List(ctx, s.diskAttachmentsFilter(diskName))
	if err != nil {
		s.recordAPIError("ListAttachments", startTime, err)
		return sdkErrorToGRPC(err)
	}

	var found []computetypes.HotswapDiskAttachment
	for _, attachment := range attachmentList.Items {
		if evrocpkg.ExtractResourceName(attachment.Spec.DiskRef) == diskName {
			found = append(found, attachment)
		}
	}

	if len(found) > 1 {
		holders := make([]string, 0, len(found))
		for _, attachment := range found {
			holders = append(holders, evrocpkg.ExtractResourceName(attachment.Spec.VirtualMachineRef))
		}
		s.recordAPIErrorWithType("CreateAttachment", startTime, "failed_precondition")
		return status.Errorf(codes.FailedPrecondition,
			"disk %s has %d attachments (VMs %v); it must hold at most one before it can be attached to VM %s",
			diskName, len(found), holders, vmName)
	}

	if len(found) == 0 {
		return nil
	}

	attachment := found[0]
	attachedVM := evrocpkg.ExtractResourceName(attachment.Spec.VirtualMachineRef)
	if attachedVM == vmName {
		return nil
	}

	name := attachment.Metadata.Id
	s.logger.Warn("Removing stale disk attachment before reattaching",
		"diskName", diskName, "fromVM", attachedVM, "toVM", vmName, "attachmentName", name)
	if err := s.client.Compute().HotswapDiskAttachments().Delete(ctx, name); err != nil && !errors.Is(err, evroc.ErrNotFound) {
		s.recordAPIError("DeleteAttachment", startTime, err)
		return sdkErrorToGRPC(err)
	}
	if err := s.client.Compute().HotswapDiskAttachments().WaitForDeleted(ctx, name, s.attachmentPollTimeout); err != nil {
		s.recordAPIError("DeleteAttachment", startTime, err)
		return status.Errorf(codes.Unavailable, "stale attachment %s was not deleted: %v", name, err)
	}

	return nil
}

func attachmentName(diskName, vmName string) string {
	fullName := fmt.Sprintf("%s-to-%s", diskName, vmName)
	if len(fullName) > 63 {
		hash := sha256.Sum256([]byte(fullName))
		return fmt.Sprintf("hsda-%x", hash)[:61]
	}
	return fullName
}

func isNotFoundOrForbidden(err error) bool {
	return errors.Is(err, evroc.ErrNotFound) || errors.Is(err, evroc.ErrForbidden)
}

func sdkUserLabels(ul *computetypes.UserLabels) map[string]string {
	if ul == nil {
		return make(map[string]string)
	}
	return map[string]string(*ul)
}

func (s *SDKStorageBackend) validateExistingDisk(disk *computetypes.Disk, name string, sizeMB int32, startTime time.Time) error {
	labels := sdkUserLabels(disk.Metadata.UserLabels)
	if managedBy := labels[managedByLabel]; managedBy != s.identifier {
		s.recordAPIErrorWithType("CreateDisk", startTime, "already_exists")
		return status.Errorf(codes.AlreadyExists, "disk %s already exists but was created by %s, not by this CSI driver (%s)",
			name, managedBy, s.identifier)
	}

	if disk.Spec.DiskSize != nil {
		if disk.Spec.DiskSize.Amount != sizeMB || string(disk.Spec.DiskSize.Unit) != "MB" {
			s.recordAPIErrorWithType("CreateDisk", startTime, "already_exists")
			return status.Errorf(codes.AlreadyExists, "disk %s already exists but with different size: %d%s vs %dMB",
				name, disk.Spec.DiskSize.Amount, disk.Spec.DiskSize.Unit, sizeMB)
		}
	}

	return nil
}

func (s *SDKStorageBackend) validateOwnership(labels map[string]string, resourceType, resourceName, operation, metricsMethod string, startTime time.Time) error {
	managedBy := labels[managedByLabel]
	if managedBy == s.identifier {
		return nil
	}

	s.recordAPIErrorWithType(metricsMethod, startTime, "forbidden")

	if operation == "delete" {
		s.logger.Warn(fmt.Sprintf("Refusing to delete %s managed by different driver", resourceType),
			"name", resourceName,
			"managedBy", managedBy,
			"ourIdentifier", s.identifier)
		return status.Errorf(codes.PermissionDenied, "refusing to delete %s %s created by %s (not created by this CSI driver: %s)",
			resourceType, resourceName, managedBy, s.identifier)
	}

	capitalizedType := resourceType
	if len(resourceType) > 0 {
		capitalizedType = strings.ToUpper(resourceType[:1]) + resourceType[1:]
	}

	s.logger.Info(fmt.Sprintf("%s exists but not managed by this driver", capitalizedType),
		"name", resourceName,
		"managedBy", managedBy)
	return status.Errorf(codes.NotFound, "%s %s exists but not managed by this CSI driver (managed by: %s)",
		resourceType, resourceName, managedBy)
}

// --- Metrics ---

func (s *SDKStorageBackend) recordAPISuccess(method string, startTime time.Time) {
	s.metrics.RecordAPICall(method, time.Since(startTime).Seconds())
}

func (s *SDKStorageBackend) recordAPIError(method string, startTime time.Time, err error) {
	s.metrics.RecordAPICallError(method, time.Since(startTime).Seconds(), classifySDKError(err))
}

func (s *SDKStorageBackend) recordAPIErrorWithType(method string, startTime time.Time, errorType string) {
	s.metrics.RecordAPICallError(method, time.Since(startTime).Seconds(), errorType)
}

func classifySDKError(err error) string {
	if err == nil {
		return ""
	}
	if isNotFoundOrForbidden(err) {
		return "not_found"
	}
	if errors.Is(err, evroc.ErrConflict) {
		return "conflict"
	}
	if errors.Is(err, evroc.ErrBadRequest) {
		return "bad_request"
	}
	errStr := err.Error()
	if strings.Contains(errStr, "timeout") || strings.Contains(errStr, "Timeout") {
		return "timeout"
	}
	return "internal"
}

func sdkErrorToGRPC(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, evroc.ErrNotFound) || errors.Is(err, evroc.ErrForbidden) {
		return status.Errorf(codes.NotFound, "%v", err)
	}
	if errors.Is(err, evroc.ErrConflict) {
		return status.Errorf(codes.AlreadyExists, "%v", err)
	}
	if errors.Is(err, evroc.ErrBadRequest) {
		return status.Errorf(codes.InvalidArgument, "%v", err)
	}
	return status.Errorf(codes.Internal, "%v", err)
}
