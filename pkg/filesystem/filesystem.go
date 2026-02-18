// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 evroc

package filesystem

import (
	"context"
	"time"
)

// Operations defines the interface for filesystem operations.
// This abstraction allows for easier testing and platform-specific implementations.
type Operations interface {
	// Device operations
	WaitForDevice(ctx context.Context, devicePath string, timeout time.Duration) error
	IsDeviceFormatted(ctx context.Context, devicePath string) (bool, error)
	FormatDevice(ctx context.Context, devicePath, fsType string) error
	IsBlockDevice(path string) (bool, error)

	// Mount operations
	Mount(source, target, fsType string, flags uintptr, options string) error
	Unmount(target string, flags int) error
	IsMountPoint(path string) (bool, error)

	// File operations
	MkdirAll(path string, perm uint32) error
	RemoveAll(path string) error
	CreateFile(path string, perm uint32) error
	PathExists(path string) (bool, error)

	// Filesystem stats
	GetFilesystemStats(path string) (*FilesystemStats, error)
}

// FilesystemStats represents filesystem statistics.
type FilesystemStats struct {
	TotalBytes      int64
	AvailableBytes  int64
	UsedBytes       int64
	TotalInodes     int64
	AvailableInodes int64
	UsedInodes      int64
}
