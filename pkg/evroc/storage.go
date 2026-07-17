// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 evroc

package evroc

import (
	"context"
	"strings"

	"github.com/evroc-oss/evroc-go-sdk/compute"
	computetypes "github.com/evroc-oss/evroc-go-sdk/types/compute"
)

// StorageBackend defines the interface for evroc storage operations.
// All operations only affect resources managed by this CSI driver.
type StorageBackend interface {
	// EnsureDiskCreated creates a disk if it doesn't exist.
	// Returns true if the disk was created, false if it already existed.
	EnsureDiskCreated(ctx context.Context, name string, sizeMB int32, storageClass, zone string) (bool, error)

	// EnsureDiskDeleted ensures a disk is deleted.
	EnsureDiskDeleted(ctx context.Context, name string) error

	// GetDisk retrieves a disk by name. Only returns disks managed by this CSI driver.
	GetDisk(ctx context.Context, name string) (*computetypes.Disk, error)

	// ListDisks lists all disks managed by this CSI driver.
	ListDisks(ctx context.Context) (*compute.DiskList, error)

	// GetAttachment retrieves a HotswapDiskAttachment by disk and VM name.
	// Only returns attachments managed by this CSI driver.
	GetAttachment(ctx context.Context, diskName, vmName string) (*computetypes.HotswapDiskAttachment, error)

	// ListAttachments lists all HotswapDiskAttachments managed by this CSI driver.
	ListAttachments(ctx context.Context) (*compute.HotswapDiskAttachmentList, error)

	// CountNodeAttachments returns the number of volume attachments for a specific node/VM.
	// Only counts attachments managed by this CSI driver.
	CountNodeAttachments(ctx context.Context, vmName string) (int, error)

	// EnsureAttachmentCreated creates a HotswapDiskAttachment.
	EnsureAttachmentCreated(ctx context.Context, diskName, vmName string) error

	// WaitForAttachmentSerial waits for the serial to be populated in the attachment status.
	// Returns the serial once available, or an error if timeout is reached.
	WaitForAttachmentSerial(ctx context.Context, diskName, vmName string) (string, error)

	// EnsureAttachmentDeleted ensures a HotswapDiskAttachment is deleted.
	EnsureAttachmentDeleted(ctx context.Context, diskName, vmName string) error
}

// ExtractResourceName extracts the resource name from a full resource reference path.
// For example: "/compute/projects/abc/regions/region/virtualMachines/vm-name" -> "vm-name"
// If the input is already a short name (doesn't contain "/"), it returns it unchanged.
func ExtractResourceName(ref string) string {
	if !strings.Contains(ref, "/") {
		return ref
	}
	parts := strings.Split(ref, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return ref
}
