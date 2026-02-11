// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 evroc


package filesystem

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// UnixOperations implements Operations for Unix-like systems.
type UnixOperations struct {
	logger             *slog.Logger
	deviceScanInterval time.Duration
}

// NewUnixOperations creates a new Unix filesystem operations implementation.
func NewUnixOperations(logger *slog.Logger, deviceScanInterval time.Duration) *UnixOperations {
	return &UnixOperations{
		logger:             logger,
		deviceScanInterval: deviceScanInterval,
	}
}

// WaitForDevice waits for a block device to appear at the given path.
func (u *UnixOperations) WaitForDevice(ctx context.Context, devicePath string, timeout time.Duration) error {
	u.logger.Info("Waiting for device", "devicePath", devicePath, "timeout", timeout)

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		// Check if device exists and is a block device
		isBlock, err := u.IsBlockDevice(devicePath)
		if err == nil && isBlock {
			u.logger.Info("Device found", "devicePath", devicePath)
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(u.deviceScanInterval):
			u.logger.Debug("Device not yet available, retrying...", "devicePath", devicePath)
		}
	}

	return fmt.Errorf("timeout waiting for device %s after %v", devicePath, timeout)
}

// IsDeviceFormatted checks if a block device has a filesystem.
func (u *UnixOperations) IsDeviceFormatted(ctx context.Context, devicePath string) (bool, error) {
	// Use blkid to check for filesystem signature
	// -p: use low-level probing (bypasses cache, direct device access)
	// -s TYPE: only show the TYPE tag (filesystem type)
	// -o value: output only the value without tag name
	cmd := exec.CommandContext(ctx, "blkid", "-p", "-s", "TYPE", "-o", "value", devicePath)
	output, err := cmd.Output()
	if err != nil {
		// blkid returns exit code 2 if no filesystem found
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 2 {
			return false, nil
		}
		return false, fmt.Errorf("blkid failed: %w", err)
	}

	fsType := strings.TrimSpace(string(output))
	u.logger.Debug("Device filesystem check", "devicePath", devicePath, "fsType", fsType)
	return fsType != "", nil
}

// FormatDevice formats a block device with the specified filesystem type.
func (u *UnixOperations) FormatDevice(ctx context.Context, devicePath, fsType string) error {
	if fsType != "ext4" && fsType != "" {
		return fmt.Errorf("unsupported filesystem type: %s (only ext4 is supported)", fsType)
	}

	u.logger.Info("Formatting device", "devicePath", devicePath, "fsType", "ext4")
	// -F: Force creation even if device appears to be in use
	// -m0: Reserve 0% of blocks for root (maximize usable space for containers)
	cmd := exec.CommandContext(ctx, "mkfs.ext4", "-F", "-m0", devicePath)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("format failed: %w, output: %s", err, string(output))
	}

	u.logger.Info("Device formatted successfully", "devicePath", devicePath)
	return nil
}

// IsBlockDevice checks if a path is a block device.
func (u *UnixOperations) IsBlockDevice(path string) (bool, error) {
	var stat unix.Stat_t
	if err := unix.Stat(path, &stat); err != nil {
		if err == unix.ENOENT {
			return false, nil
		}
		return false, err
	}
	return stat.Mode&unix.S_IFBLK != 0, nil
}

// Mount mounts a filesystem.
func (u *UnixOperations) Mount(source, target, fsType string, flags uintptr, options string) error {
	u.logger.Info("Mounting", "source", source, "target", target, "fsType", fsType, "flags", flags)

	if err := unix.Mount(source, target, fsType, flags, options); err != nil {
		return fmt.Errorf("mount %s to %s: %w", source, target, err)
	}

	u.logger.Info("Mounted successfully", "source", source, "target", target)
	return nil
}

// Unmount unmounts a filesystem.
func (u *UnixOperations) Unmount(target string, flags int) error {
	u.logger.Info("Unmounting", "target", target)

	if err := unix.Unmount(target, flags); err != nil {
		// Check if already unmounted
		if err == unix.EINVAL {
			u.logger.Debug("Path not mounted, ignoring", "target", target)
			return nil
		}
		return fmt.Errorf("unmount %s: %w", target, err)
	}

	u.logger.Info("Unmounted successfully", "target", target)
	return nil
}

// IsMountPoint checks if a path is a mount point by reading /proc/mounts.
func (u *UnixOperations) IsMountPoint(path string) (bool, error) {
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return false, fmt.Errorf("read /proc/mounts: %w", err)
	}

	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == path {
			return true, nil
		}
	}
	return false, nil
}

// MkdirAll creates a directory and all necessary parents.
func (u *UnixOperations) MkdirAll(path string, perm uint32) error {
	if err := os.MkdirAll(path, os.FileMode(perm)); err != nil {
		return fmt.Errorf("create directory %s: %w", path, err)
	}
	return nil
}

// RemoveAll removes a path and all its contents.
func (u *UnixOperations) RemoveAll(path string) error {
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
}

// CreateFile creates an empty file.
func (u *UnixOperations) CreateFile(path string, perm uint32) error {
	file, err := os.OpenFile(path, os.O_CREATE, os.FileMode(perm))
	if err != nil {
		return fmt.Errorf("create file %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close file %s: %w", path, err)
	}
	return nil
}

// PathExists checks if a path exists.
func (u *UnixOperations) PathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// GetFilesystemStats returns filesystem statistics for a path.
func (u *UnixOperations) GetFilesystemStats(path string) (*FilesystemStats, error) {
	var statfs unix.Statfs_t
	if err := unix.Statfs(path, &statfs); err != nil {
		return nil, fmt.Errorf("statfs %s: %w", path, err)
	}

	totalBytes := int64(statfs.Blocks) * int64(statfs.Bsize)
	availableBytes := int64(statfs.Bavail) * int64(statfs.Bsize)
	usedBytes := totalBytes - availableBytes

	totalInodes := int64(statfs.Files)
	availableInodes := int64(statfs.Ffree)
	usedInodes := totalInodes - availableInodes

	return &FilesystemStats{
		TotalBytes:      totalBytes,
		AvailableBytes:  availableBytes,
		UsedBytes:       usedBytes,
		TotalInodes:     totalInodes,
		AvailableInodes: availableInodes,
		UsedInodes:      usedInodes,
	}, nil
}
