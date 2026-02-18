// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 evroc

package rest

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/evroc-oss/evroc-csi-driver/pkg/evroc/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// API version and group constants for REST API calls
	computeAPIGroup   = "compute"
	computeAPIVersion = "v1beta1"

	// ManagedByLabel is the label key for CSI driver instance identification.
	ManagedByLabel = "managed-by"

	// labelSelectorParam is the query parameter name for label-based filtering.
	labelSelectorParam = "labelSelector"
)

// Ensure Client implements StorageBackend interface (uncomment when interface is stable).
// var _ evroc.StorageBackend = (*Client)(nil)

// REST API specific types to match the REST API structure

// RestDiskMetadata represents metadata for REST API disk creation (v1beta1)
type RestDiskMetadata struct {
	UserLabels map[string]string `json:"userLabels,omitempty"`
	ID         string            `json:"id,omitempty"`
}

// RestDiskPlacement represents placement for REST API v1beta1
type RestDiskPlacement struct {
	Zone *string `json:"zone,omitempty"`
}

// RestDiskSize represents disk size for REST API v1beta1
type RestDiskSize struct {
	Amount int32  `json:"amount"`
	Unit   string `json:"unit"`
}

// RestDiskSpec represents the spec for REST API disk creation (v1beta1)
type RestDiskSpec struct {
	DiskImageRef *string           `json:"diskImageRef,omitempty"`
	Placement    RestDiskPlacement `json:"placement"`
	DiskSize     *RestDiskSize     `json:"diskSize,omitempty"`
}

// RestDisk represents a disk for REST API operations (v1beta1)
type RestDisk struct {
	APIVersion string           `json:"apiVersion"`
	Kind       string           `json:"kind"`
	Metadata   RestDiskMetadata `json:"metadata"`
	Spec       RestDiskSpec     `json:"spec"`
}

// RestHotswapMetadata represents metadata for REST API hotswap attachment creation (v1beta1)
type RestHotswapMetadata struct {
	UserLabels map[string]string `json:"userLabels,omitempty"`
	ID         string            `json:"id,omitempty"`
}

// RestHotswapSpec represents the spec for REST API hotswap attachment (v1beta1)
type RestHotswapSpec struct {
	DiskRef           string `json:"diskRef"`
	VirtualMachineRef string `json:"virtualMachineRef"`
}

// RestHotswapAttachment represents a hotswap disk attachment for REST API operations (v1beta1)
type RestHotswapAttachment struct {
	APIVersion string              `json:"apiVersion"`
	Kind       string              `json:"kind"`
	Metadata   RestHotswapMetadata `json:"metadata"`
	Spec       RestHotswapSpec     `json:"spec"`
}

// resolveResourceRef converts a short resource name to a fully qualified resource path.
// If the ref already starts with "/", it's returned as-is (already qualified).
// Format: /{service}/projects/{project}/regions/{region}/{resourceType}/{name}
func (c *Client) resolveResourceRef(ref, resourceType string) string {
	if strings.HasPrefix(ref, "/") {
		return ref
	}
	return fmt.Sprintf("/%s/projects/%s/regions/%s/%s/%s",
		computeAPIGroup, c.projectID, c.region, resourceType, ref)
}

// attachmentName generates a deterministic attachment name from disk and VM names.
// Format: {diskName}-to-{vmName}, but hashed if exceeds 64 chars (Kubernetes name limit).
// Backend will use this name to generate serial numbers.
func attachmentName(diskName, vmName string) string {
	fullName := fmt.Sprintf("%s-to-%s", diskName, vmName)

	// If name exceeds 64 chars, hash it to fit within Kubernetes resource name limit
	if len(fullName) > 64 {
		hash := sha256.Sum256([]byte(fullName))
		// Use first 56 chars of hex hash + "hsda-" prefix = 61 chars total
		return fmt.Sprintf("hsda-%x", hash)[:61]
	}

	return fullName
}

// classifyError categorizes errors for metrics reporting.
func classifyError(err error) string {
	if err == nil {
		return ""
	}
	if IsNotFound(err) {
		return "not_found"
	}
	if IsAlreadyExists(err) || IsConflict(err) {
		return "already_exists"
	}
	if IsServerError(err) {
		return "server_error"
	}
	errStr := err.Error()
	if strings.Contains(errStr, "timeout") || strings.Contains(errStr, "Timeout") {
		return "timeout"
	}
	return "internal"
}

// recordAPISuccess records a successful API call with automatic duration calculation.
func (c *Client) recordAPISuccess(method string, startTime time.Time) {
	duration := time.Since(startTime).Seconds()
	c.metrics.RecordAPICall(method, duration)
}

// recordAPIError records a failed API call with automatic error classification and duration.
func (c *Client) recordAPIError(method string, startTime time.Time, err error) {
	duration := time.Since(startTime).Seconds()
	c.metrics.RecordAPICallError(method, duration, classifyError(err))
}

// recordAPIErrorWithType records a failed API call with explicit error type and duration.
func (c *Client) recordAPIErrorWithType(method string, startTime time.Time, errorType string) {
	duration := time.Since(startTime).Seconds()
	c.metrics.RecordAPICallError(method, duration, errorType)
}

// validateExistingDisk validates that an existing disk matches the expected parameters.
func (c *Client) validateExistingDisk(disk *types.Disk, name string, sizeMB int32, storageClass string, startTime time.Time) error {
	// Validate managed-by label
	if managedBy := disk.Labels()[ManagedByLabel]; managedBy != c.identifier {
		c.recordAPIErrorWithType("CreateDisk", startTime, "already_exists")
		return status.Errorf(codes.AlreadyExists, "disk %s already exists but was created by %s, not by this CSI driver (%s)",
			name, managedBy, c.identifier)
	}

	// Validate size matches
	if disk.Spec.DiskSize.Amount != sizeMB || disk.Spec.DiskSize.Unit != "MB" {
		c.recordAPIErrorWithType("CreateDisk", startTime, "already_exists")
		return status.Errorf(codes.AlreadyExists, "disk %s already exists but with different size: %d%s vs %dMB",
			name, disk.Spec.DiskSize.Amount, disk.Spec.DiskSize.Unit, sizeMB)
	}

	return nil
}

// validateOwnership verifies that a resource is managed by this CSI driver.
// Returns error if resource is managed by another driver.
// operation should be "delete" or "get" to format appropriate messages.
func (c *Client) validateOwnership(labels map[string]string, resourceType, resourceName, operation, metricsMethod string, startTime time.Time) error {
	managedBy := labels[ManagedByLabel]
	if managedBy == c.identifier {
		return nil
	}

	c.recordAPIErrorWithType(metricsMethod, startTime, "forbidden")

	if operation == "delete" {
		c.logger.Warn(fmt.Sprintf("Refusing to delete %s managed by different driver", resourceType),
			"name", resourceName,
			"managedBy", managedBy,
			"ourIdentifier", c.identifier)
		return status.Errorf(codes.PermissionDenied, "refusing to delete %s %s created by %s (not created by this CSI driver: %s)",
			resourceType, resourceName, managedBy, c.identifier)
	}

	// Capitalize first letter for log message
	capitalizedType := resourceType
	if len(resourceType) > 0 {
		capitalizedType = strings.ToUpper(resourceType[:1]) + resourceType[1:]
	}

	c.logger.Info(fmt.Sprintf("%s exists but not managed by this driver", capitalizedType),
		"name", resourceName,
		"managedBy", managedBy)
	return status.Errorf(codes.NotFound, "%s %s exists but not managed by this CSI driver (managed by: %s)",
		resourceType, resourceName, managedBy)
}

// EnsureDiskCreated creates a disk or verifies it already exists.
// Returns (true, nil) if the disk was created, (false, nil) if it already existed.
func (c *Client) EnsureDiskCreated(ctx context.Context, name string, sizeMB int32, storageClass, zone string) (bool, error) {
	startTime := time.Now()

	// Refresh token if needed before making any API calls
	if err := c.refreshTokenIfNeeded(ctx); err != nil {
		return false, err
	}

	listPath := c.buildResourcePath(computeAPIGroup, computeAPIVersion, "disks")
	itemPath := c.buildResourceItemPath(computeAPIGroup, computeAPIVersion, "disks", name)

	c.logger.Info("Ensuring disk exists",
		"name", name,
		"sizeMB", sizeMB,
		"storageClass", storageClass,
		"zone", zone)

	// Try to check if disk already exists
	var existingDisk types.Disk
	err := c.doRequest(ctx, "GET", itemPath, nil, &existingDisk)
	if err == nil {
		// Disk already exists - validate it matches our requirements
		c.logger.Info("Disk already exists (from GET), validating parameters", "name", name)

		if err := c.validateExistingDisk(&existingDisk, name, sizeMB, storageClass, startTime); err != nil {
			return false, err
		}

		// All validations passed
		c.recordAPISuccess("CreateDisk", startTime)
		return false, nil
	}

	// Log the GET error but continue to POST anyway
	c.logger.Info("GET check failed, will try POST", "error", err.Error())

	// Create disk with REST API format
	restDisk := &RestDisk{
		APIVersion: computeAPIGroup + "/" + computeAPIVersion,
		Kind:       "Disk",
		Metadata: RestDiskMetadata{
			ID: name,
			UserLabels: map[string]string{
				ManagedByLabel: c.identifier,
			},
		},
		Spec: RestDiskSpec{
			DiskSize: &RestDiskSize{
				Amount: sizeMB,
				Unit:   "MB",
			},
			Placement: RestDiskPlacement{},
		},
	}

	// Add zone if specified
	if zone != "" {
		restDisk.Spec.Placement.Zone = &zone
	}

	c.logger.Info("Creating disk", "name", name, "path", listPath)

	var result types.Disk
	err = c.doRequest(ctx, "POST", listPath, restDisk, &result)
	if err != nil {
		// If disk already exists (409 Conflict), validate it matches our requirements
		if IsConflict(err) {
			c.logger.Info("Disk already exists (from POST conflict), will validate", "name", name)

			// Fetch the existing disk to validate
			var conflictDisk types.Disk
			if getErr := c.doRequest(ctx, "GET", itemPath, nil, &conflictDisk); getErr != nil {
				c.recordAPIError("CreateDisk", startTime, getErr)
				return false, getErr
			}

			if err := c.validateExistingDisk(&conflictDisk, name, sizeMB, storageClass, startTime); err != nil {
				return false, err
			}

			// All validations passed
			c.recordAPISuccess("CreateDisk", startTime)
			return false, nil
		}

		// Other error
		c.recordAPIError("CreateDisk", startTime, err)
		return false, err
	}

	// Success - disk created
	c.recordAPISuccess("CreateDisk", startTime)
	c.logger.Info("Disk created successfully", "name", name)
	return true, nil
}

// EnsureDiskDeleted deletes a disk or verifies it's already deleted.
func (c *Client) EnsureDiskDeleted(ctx context.Context, name string) error {
	startTime := time.Now()

	// Refresh token if needed
	if err := c.refreshTokenIfNeeded(ctx); err != nil {
		return err
	}

	path := c.buildResourceItemPath(computeAPIGroup, computeAPIVersion, "disks", name)

	c.logger.Info("Ensuring disk is deleted", "name", name, "path", path)

	// Get disk first to verify ownership before deleting
	var existingDisk types.Disk
	err := c.doRequest(ctx, "GET", path, nil, &existingDisk)

	if IsNotFound(err) {
		// Disk already deleted
		c.logger.Info("Disk already deleted", "name", name)
		c.recordAPISuccess("DeleteDisk", startTime)
		return nil
	}

	if err != nil {
		// Unexpected error during GET
		c.recordAPIError("DeleteDisk", startTime, err)
		return err
	}

	// Verify ownership before deleting (if label enforcement is enabled)
	if err := c.validateOwnership(existingDisk.Labels(), "disk", name, "delete", "DeleteDisk", startTime); err != nil {
		return err
	}

	// Now delete the disk
	err = c.doRequest(ctx, "DELETE", path, nil, nil)
	if err == nil {
		c.logger.Info("Disk deleted successfully", "name", name)
		c.recordAPISuccess("DeleteDisk", startTime)
		return nil
	}

	// If disk doesn't exist (404), that's fine - it's already deleted
	if IsNotFound(err) {
		c.logger.Info("Disk already deleted", "name", name)
		c.recordAPISuccess("DeleteDisk", startTime)
		return nil
	}

	// Any other error is a real error
	c.recordAPIError("DeleteDisk", startTime, err)
	return err
}

// GetDisk retrieves disk information.
func (c *Client) GetDisk(ctx context.Context, name string) (*types.Disk, error) {
	startTime := time.Now()

	// Refresh token if needed
	if err := c.refreshTokenIfNeeded(ctx); err != nil {
		return nil, err
	}

	path := c.buildResourceItemPath(computeAPIGroup, computeAPIVersion, "disks", name)

	c.logger.Info("Getting disk", "name", name, "path", path)

	var result types.Disk
	err := c.doRequest(ctx, "GET", path, nil, &result)
	if err != nil {
		if IsNotFound(err) {
			c.logger.Info("Disk not found", "name", name)
		}
		c.recordAPIError("GetDisk", startTime, err)
		return nil, err
	}

	// Verify the disk was created by this CSI driver (if label enforcement is enabled)
	if err := c.validateOwnership(result.Labels(), "disk", name, "get", "GetDisk", startTime); err != nil {
		return nil, err
	}

	c.logger.Info("Disk retrieved successfully", "name", name)
	c.recordAPISuccess("GetDisk", startTime)
	return &result, nil
}

// ListDisks lists all disks managed by this driver.
func (c *Client) ListDisks(ctx context.Context) (*types.DiskList, error) {
	startTime := time.Now()

	// Refresh token if needed
	if err := c.refreshTokenIfNeeded(ctx); err != nil {
		return nil, err
	}

	// Use server-side filtering by managed-by label
	labelSelector := fmt.Sprintf("%s=%s", ManagedByLabel, c.identifier)
	query := url.Values{}
	query.Set(labelSelectorParam, labelSelector)

	fullURL := c.buildResourcePathWithQuery(computeAPIGroup, computeAPIVersion, "disks", query)
	c.logger.Info("Listing disks with label selector", "labelSelector", labelSelector)

	var result types.DiskList
	if err := c.doRequestWithFullURL(ctx, "GET", fullURL, nil, &result); err != nil {
		c.recordAPIError("ListDisks", startTime, err)
		return nil, err
	}

	c.logger.Info("Disks listed successfully", "count", len(result.Items))
	c.recordAPISuccess("ListDisks", startTime)
	return &result, nil
}

// ListAttachments lists all disk attachments.
func (c *Client) ListAttachments(ctx context.Context) (*types.HotswapDiskAttachmentList, error) {
	startTime := time.Now()

	// Refresh token if needed
	if err := c.refreshTokenIfNeeded(ctx); err != nil {
		return nil, err
	}

	// Use server-side filtering by managed-by label
	labelSelector := fmt.Sprintf("%s=%s", ManagedByLabel, c.identifier)
	query := url.Values{}
	query.Set(labelSelectorParam, labelSelector)

	fullURL := c.buildResourcePathWithQuery(computeAPIGroup, computeAPIVersion, "hotswapDiskAttachments", query)
	c.logger.Info("Listing disk attachments with label selector", "labelSelector", labelSelector)

	var result types.HotswapDiskAttachmentList
	if err := c.doRequestWithFullURL(ctx, "GET", fullURL, nil, &result); err != nil {
		c.recordAPIError("ListAttachments", startTime, err)
		return nil, err
	}

	c.logger.Info("Attachments listed successfully", "count", len(result.Items))
	c.recordAPISuccess("ListAttachments", startTime)
	return &result, nil
}

// CountNodeAttachments counts the number of disk attachments for a specific node.
func (c *Client) CountNodeAttachments(ctx context.Context, vmName string) (int, error) {
	c.logger.Info("Counting attachments for VM", "vmName", vmName)

	// List all attachments and filter by VM
	attachmentList, err := c.ListAttachments(ctx)
	if err != nil {
		return 0, err
	}

	count := 0
	for _, attachment := range attachmentList.Items {
		if attachment.Spec.VMRef == vmName {
			count++
		}
	}

	c.logger.Info("Counted attachments for VM", "vmName", vmName, "count", count)
	return count, nil
}

// EnsureAttachmentCreated creates a disk attachment or verifies it already exists.
func (c *Client) EnsureAttachmentCreated(ctx context.Context, diskName, vmName string) error {
	startTime := time.Now()

	// Refresh token if needed
	if err := c.refreshTokenIfNeeded(ctx); err != nil {
		return err
	}

	path := c.buildResourcePath(computeAPIGroup, computeAPIVersion, "hotswapDiskAttachments")

	// Generate attachment name using SHA256 hash
	attName := attachmentName(diskName, vmName)

	c.logger.Info("Ensuring attachment exists",
		"diskName", diskName,
		"vmName", vmName,
		"attachmentName", attName)

	// Create attachment with REST API format
	// Convert short names to fully qualified resource paths
	restAttachment := &RestHotswapAttachment{
		APIVersion: computeAPIGroup + "/" + computeAPIVersion,
		Kind:       "HotswapDiskAttachment",
		Metadata: RestHotswapMetadata{
			ID: attName,
			UserLabels: map[string]string{
				ManagedByLabel: c.identifier,
			},
		},
		Spec: RestHotswapSpec{
			DiskRef:           c.resolveResourceRef(diskName, "disks"),
			VirtualMachineRef: c.resolveResourceRef(vmName, "virtualMachines"),
		},
	}

	c.logger.Info("Creating attachment", "name", attName, "path", path)

	var result types.HotswapDiskAttachment
	err := c.doRequest(ctx, "POST", path, restAttachment, &result)
	if err != nil {
		// If attachment already exists (409 Conflict), that's fine
		if IsConflict(err) {
			c.logger.Info("Attachment already exists", "name", attName)
			c.recordAPISuccess("CreateAttachment", startTime)
			return nil
		}
		c.recordAPIError("CreateAttachment", startTime, err)
		return err
	}

	c.logger.Info("Attachment created successfully", "name", attName)
	c.recordAPISuccess("CreateAttachment", startTime)
	return nil
}

// WaitForAttachmentSerial waits for the serial to be populated in the attachment status.
func (c *Client) WaitForAttachmentSerial(ctx context.Context, diskName, vmName string) (string, error) {
	attName := attachmentName(diskName, vmName)

	c.logger.Info("Waiting for serial",
		"diskName", diskName,
		"vmName", vmName,
		"attachmentName", attName)

	// Poll for serial with timeout (from config) using exponential backoff
	pollTimeout := c.cfg.CSI.AttachmentPollTimeout
	backoff := 200 * time.Millisecond // Start fast
	maxBackoff := 5 * time.Second     // Cap at 5s

	pollCtx, cancel := context.WithTimeout(ctx, pollTimeout)
	defer cancel()

	for {
		attachment, err := c.GetAttachment(pollCtx, diskName, vmName)
		if err != nil {
			c.logger.Warn("Failed to get attachment while polling for serial",
				"diskName", diskName,
				"vmName", vmName,
				"error", err)
		} else {
			// Check if the attachment is Ready
			isReady := false
			for _, cond := range attachment.Status.Conditions {
				if cond.Type == "Ready" && cond.Status == "True" {
					isReady = true
					break
				}
			}

			if isReady && attachment.Status.Serial != "" {
				c.logger.Info("Serial retrieved",
					"diskName", diskName,
					"vmName", vmName,
					"serial", attachment.Status.Serial)
				return attachment.Status.Serial, nil
			}

			c.logger.Debug("Attachment not yet ready or serial not available, continuing to poll",
				"diskName", diskName,
				"vmName", vmName,
				"ready", isReady,
				"hasSerial", attachment.Status.Serial != "")
		}

		// Wait with exponential backoff
		select {
		case <-pollCtx.Done():
			c.logger.Error("Timeout waiting for serial",
				"diskName", diskName,
				"vmName", vmName,
				"timeout", pollTimeout)
			return "", status.Errorf(codes.Unavailable, "serial not available after waiting %v", pollTimeout)
		case <-time.After(backoff):
			// Increase backoff exponentially
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}

// EnsureAttachmentDeleted deletes a disk attachment or verifies it's already deleted.
func (c *Client) EnsureAttachmentDeleted(ctx context.Context, diskName, vmName string) error {
	startTime := time.Now()

	// Refresh token if needed
	if err := c.refreshTokenIfNeeded(ctx); err != nil {
		return err
	}

	// Generate attachment name using SHA256 hash
	attName := attachmentName(diskName, vmName)
	path := c.buildResourceItemPath(computeAPIGroup, computeAPIVersion, "hotswapDiskAttachments", attName)

	c.logger.Info("Ensuring attachment is deleted",
		"diskName", diskName,
		"vmName", vmName,
		"attachmentName", attName,
		"path", path)

	// Get attachment first to verify ownership before deleting
	var existingAttachment types.HotswapDiskAttachment
	err := c.doRequest(ctx, "GET", path, nil, &existingAttachment)

	if IsNotFound(err) {
		// Attachment already deleted
		c.logger.Info("Attachment already deleted", "name", attName)
		c.recordAPISuccess("DeleteAttachment", startTime)
		return nil
	}

	if err != nil {
		// Unexpected error during GET
		c.recordAPIError("DeleteAttachment", startTime, err)
		return err
	}

	// Verify ownership before deleting (if label enforcement is enabled)
	if err := c.validateOwnership(existingAttachment.Labels(), "attachment", attName, "delete", "DeleteAttachment", startTime); err != nil {
		return err
	}

	// Now delete the attachment
	err = c.doRequest(ctx, "DELETE", path, nil, nil)
	if err == nil {
		c.logger.Info("Attachment deleted successfully", "name", attName)
		c.recordAPISuccess("DeleteAttachment", startTime)
		return nil
	}

	// If attachment doesn't exist (404), that's fine - it's already deleted
	if IsNotFound(err) {
		c.logger.Info("Attachment already deleted", "name", attName)
		c.recordAPISuccess("DeleteAttachment", startTime)
		return nil
	}

	// Any other error is a real error
	c.recordAPIError("DeleteAttachment", startTime, err)
	return err
}

// GetAttachment retrieves information about a specific disk attachment.
func (c *Client) GetAttachment(ctx context.Context, diskName, vmName string) (*types.HotswapDiskAttachment, error) {
	startTime := time.Now()

	// Refresh token if needed
	if err := c.refreshTokenIfNeeded(ctx); err != nil {
		return nil, err
	}

	// Generate attachment name using SHA256 hash
	attName := attachmentName(diskName, vmName)
	path := c.buildResourceItemPath(computeAPIGroup, computeAPIVersion, "hotswapDiskAttachments", attName)

	c.logger.Info("Getting attachment",
		"diskName", diskName,
		"vmName", vmName,
		"attachmentName", attName,
		"path", path)

	var result types.HotswapDiskAttachment
	err := c.doRequest(ctx, "GET", path, nil, &result)
	if err != nil {
		if IsNotFound(err) {
			c.logger.Info("Attachment not found", "name", attName)
		}
		c.recordAPIError("GetAttachment", startTime, err)
		return nil, err
	}

	// Verify the attachment was created by this CSI driver (if label enforcement is enabled)
	if err := c.validateOwnership(result.Labels(), "attachment", attName, "get", "GetAttachment", startTime); err != nil {
		return nil, err
	}

	c.logger.Info("Attachment retrieved successfully",
		"name", attName,
		"serial", result.Status.Serial)
	c.recordAPISuccess("GetAttachment", startTime)
	return &result, nil
}
