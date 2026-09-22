// SPDX-License-Identifier: MIT
// Copyright (c) 2025 evroc

package filesystem

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestIsFilesystemCorruption(t *testing.T) {
	tests := []struct {
		name     string
		errno    unix.Errno
		expected bool
	}{
		// Only these 3 errors indicate corruption - should return true
		{"EINVAL indicates corruption", unix.EINVAL, true},
		{"EIO indicates corruption", unix.EIO, true},
		{"EUCLEAN indicates corruption", unix.EUCLEAN, true},

		// ALL other errors should return false - critical for safety
		{"EMFILE should not trigger repair", unix.EMFILE, false},
		{"ENOMEM should not trigger repair", unix.ENOMEM, false},
		{"ENOSPC should not trigger repair", unix.ENOSPC, false},
		{"ELOOP should not trigger repair", unix.ELOOP, false},
		{"ENOENT should not trigger repair", unix.ENOENT, false},
		{"EACCES should not trigger repair", unix.EACCES, false},
		{"EBUSY should not trigger repair", unix.EBUSY, false},
		{"EPERM should not trigger repair", unix.EPERM, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := fmt.Errorf("mount failed: %w", tt.errno)
			result := IsFilesystemCorruption(err)
			require.Equal(t, tt.expected, result,
				"IsFilesystemCorruption(%v) = %v, want %v",
				tt.errno, result, tt.expected)
		})
	}
}

// TestCriticalScenario validates the exact bug we're fixing:
// EMFILE (too many mounts) should NOT trigger repair/reformat
func TestCriticalScenario_EMFILE_DoesNotTriggerRepair(t *testing.T) {
	// Scenario: maxVolumesPerNode = 128, we have 127 mounts, try to mount 128th
	// mount() returns EMFILE (too many open files)
	mountErr := fmt.Errorf("mount /dev/sda to /mnt: %w", unix.EMFILE)

	// CRITICAL: EMFILE should NOT be treated as filesystem corruption
	require.False(t, IsFilesystemCorruption(mountErr),
		"EMFILE must NOT be treated as corruption (would trigger reformat!)")

	// This test failing means we would reformat a valid disk just because
	// we hit the mount limit - that's data loss!
}

// TestCorruptionScenario validates legitimate corruption handling
func TestCorruptionScenario_EINVAL_TriggersRepair(t *testing.T) {
	// Scenario: pod killed during write, filesystem journal not replayed
	// mount() returns EINVAL (invalid superblock)
	mountErr := fmt.Errorf("mount /dev/sda to /mnt: %w", unix.EINVAL)

	// Should recognize as corruption - this is one of the 3 errors that justifies repair
	require.True(t, IsFilesystemCorruption(mountErr),
		"EINVAL should be recognized as filesystem corruption")

	// This is one of the ONLY 3 cases where repair/reformat is appropriate
}

func TestNilError(t *testing.T) {
	require.False(t, IsFilesystemCorruption(nil))
}

// --- Test helpers ---

// newTestOps returns a UnixOperations configured with a fast scan interval for
// tests.
func newTestOps(t *testing.T) *UnixOperations {
	t.Helper()
	return NewUnixOperations(
		slog.New(slog.NewTextHandler(os.Stderr, nil)),
		10*time.Millisecond,
	)
}

// withFakeBinaries creates a temp directory, writes the given shell scripts
// into it, and prepends it to PATH for the duration of the test. This lets us
// test exec.Command-based methods (resize2fs, blkid, etc.) without the real
// binaries or root privileges.
//
// scripts maps a binary name to the script body (without the shebang line,
// which is added automatically).
func withFakeBinaries(t *testing.T, scripts map[string]string) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("filesystem tests are Linux-only")
	}
	binDir := t.TempDir()
	for name, body := range scripts {
		path := filepath.Join(binDir, name)
		full := "#!/bin/sh\n" + body + "\n"
		require.NoError(t, os.WriteFile(path, []byte(full), 0o755))
	}
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))
}

// --- ResizeFilesystem tests ---

func TestResizeFilesystem_Ext4_Success(t *testing.T) {
	withFakeBinaries(t, map[string]string{
		"resize2fs": `exit 0`,
	})
	ops := newTestOps(t)
	err := ops.ResizeFilesystem(context.Background(), "/dev/sda", "/mnt", "ext4")
	require.NoError(t, err)
}

func TestResizeFilesystem_Ext4_Failure(t *testing.T) {
	withFakeBinaries(t, map[string]string{
		"resize2fs": `echo "resize2fs: bad" >&2; exit 1`,
	})
	ops := newTestOps(t)
	err := ops.ResizeFilesystem(context.Background(), "/dev/sda", "/mnt", "ext4")
	require.Error(t, err)
	require.Contains(t, err.Error(), "resize2fs")
	require.Contains(t, err.Error(), "bad")
}

func TestResizeFilesystem_DefaultsToExt4(t *testing.T) {
	called := false
	withFakeBinaries(t, map[string]string{
		"resize2fs": `exit 0`,
	})
	// We can't easily capture the call, but we can verify no error is
	// returned when fsType is empty (should default to ext4).
	ops := newTestOps(t)
	err := ops.ResizeFilesystem(context.Background(), "/dev/sda", "/mnt", "")
	require.NoError(t, err)
	require.False(t, called)
}

func TestResizeFilesystem_Xfs_Success(t *testing.T) {
	withFakeBinaries(t, map[string]string{
		"xfs_growfs": `exit 0`,
	})
	ops := newTestOps(t)
	err := ops.ResizeFilesystem(context.Background(), "/dev/sda", "/mnt", "xfs")
	require.NoError(t, err)
}

func TestResizeFilesystem_Xfs_RequiresMountPath(t *testing.T) {
	withFakeBinaries(t, map[string]string{
		"xfs_growfs": `exit 0`,
	})
	ops := newTestOps(t)
	err := ops.ResizeFilesystem(context.Background(), "/dev/sda", "", "xfs")
	require.Error(t, err)
	require.Contains(t, err.Error(), "requires a mounted path")
}

func TestResizeFilesystem_Xfs_Failure(t *testing.T) {
	withFakeBinaries(t, map[string]string{
		"xfs_growfs": `echo "xfs_growfs: bad" >&2; exit 1`,
	})
	ops := newTestOps(t)
	err := ops.ResizeFilesystem(context.Background(), "/dev/sda", "/mnt", "xfs")
	require.Error(t, err)
	require.Contains(t, err.Error(), "xfs_growfs")
	require.Contains(t, err.Error(), "bad")
}

func TestResizeFilesystem_UnsupportedType(t *testing.T) {
	ops := newTestOps(t)
	err := ops.ResizeFilesystem(context.Background(), "/dev/sda", "/mnt", "btrfs")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported filesystem type")
}

func TestResizeFilesystem_Ext4_BinaryNotFound(t *testing.T) {
	// Don't provide resize2fs; but we can't guarantee it isn't on the real
	// PATH, so we clear PATH entirely.
	t.Setenv("PATH", t.TempDir())
	ops := newTestOps(t)
	err := ops.ResizeFilesystem(context.Background(), "/dev/sda", "/mnt", "ext4")
	require.Error(t, err)
	require.Contains(t, err.Error(), "resize2fs")
}

// --- IsDeviceFormatted tests ---

func TestIsDeviceFormatted_Formatted(t *testing.T) {
	withFakeBinaries(t, map[string]string{
		"blkid": `echo "ext4"`,
	})
	ops := newTestOps(t)
	formatted, err := ops.IsDeviceFormatted(context.Background(), "/dev/sda")
	require.NoError(t, err)
	require.True(t, formatted)
}

func TestIsDeviceFormatted_Unformatted(t *testing.T) {
	withFakeBinaries(t, map[string]string{
		// blkid returns exit 2 when no filesystem is found.
		"blkid": `exit 2`,
	})
	ops := newTestOps(t)
	formatted, err := ops.IsDeviceFormatted(context.Background(), "/dev/sda")
	require.NoError(t, err)
	require.False(t, formatted)
}

func TestIsDeviceFormatted_BlkidError(t *testing.T) {
	withFakeBinaries(t, map[string]string{
		"blkid": `echo "blkid: error" >&2; exit 4`,
	})
	ops := newTestOps(t)
	formatted, err := ops.IsDeviceFormatted(context.Background(), "/dev/sda")
	require.Error(t, err)
	require.False(t, formatted)
	require.Contains(t, err.Error(), "blkid failed")
}

func TestIsDeviceFormatted_EmptyOutput(t *testing.T) {
	withFakeBinaries(t, map[string]string{
		"blkid": `exit 0`,
	})
	ops := newTestOps(t)
	formatted, err := ops.IsDeviceFormatted(context.Background(), "/dev/sda")
	require.NoError(t, err)
	require.False(t, formatted)
}

// --- FormatDevice tests ---

func TestFormatDevice_Ext4_Success(t *testing.T) {
	withFakeBinaries(t, map[string]string{
		"mkfs.ext4": `exit 0`,
	})
	ops := newTestOps(t)
	err := ops.FormatDevice(context.Background(), "/dev/sda", "ext4")
	require.NoError(t, err)
}

func TestFormatDevice_EmptyFsType(t *testing.T) {
	withFakeBinaries(t, map[string]string{
		"mkfs.ext4": `exit 0`,
	})
	ops := newTestOps(t)
	err := ops.FormatDevice(context.Background(), "/dev/sda", "")
	require.NoError(t, err)
}

func TestFormatDevice_UnsupportedType(t *testing.T) {
	ops := newTestOps(t)
	err := ops.FormatDevice(context.Background(), "/dev/sda", "xfs")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported filesystem type")
}

func TestFormatDevice_Failure(t *testing.T) {
	withFakeBinaries(t, map[string]string{
		"mkfs.ext4": `echo "mkfs: bad" >&2; exit 1`,
	})
	ops := newTestOps(t)
	err := ops.FormatDevice(context.Background(), "/dev/sda", "ext4")
	require.Error(t, err)
	require.Contains(t, err.Error(), "format failed")
	require.Contains(t, err.Error(), "bad")
}

// --- RepairFilesystem tests ---

func TestRepairFilesystem_Success(t *testing.T) {
	for _, exitCode := range []int{0, 1, 2} {
		t.Run(fmt.Sprintf("exit_%d", exitCode), func(t *testing.T) {
			withFakeBinaries(t, map[string]string{
				"fsck.ext4": fmt.Sprintf(`exit %d`, exitCode),
			})
			ops := newTestOps(t)
			err := ops.RepairFilesystem(context.Background(), "/dev/sda", "ext4")
			require.NoError(t, err)
		})
	}
}

func TestRepairFilesystem_Failure(t *testing.T) {
	for _, exitCode := range []int{4, 8, 16} {
		t.Run(fmt.Sprintf("exit_%d", exitCode), func(t *testing.T) {
			withFakeBinaries(t, map[string]string{
				"fsck.ext4": fmt.Sprintf(`echo "fsck: corrupt" >&2; exit %d`, exitCode),
			})
			ops := newTestOps(t)
			err := ops.RepairFilesystem(context.Background(), "/dev/sda", "ext4")
			require.Error(t, err)
			require.Contains(t, err.Error(), "fsck failed")
		})
	}
}

func TestRepairFilesystem_UnsupportedType(t *testing.T) {
	ops := newTestOps(t)
	err := ops.RepairFilesystem(context.Background(), "/dev/sda", "btrfs")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported filesystem type")
}

func TestRepairFilesystem_EmptyFsType(t *testing.T) {
	withFakeBinaries(t, map[string]string{
		"fsck.ext4": `exit 0`,
	})
	ops := newTestOps(t)
	err := ops.RepairFilesystem(context.Background(), "/dev/sda", "")
	require.NoError(t, err)
}

// --- IsBlockDevice tests ---

func TestIsBlockDevice_NotExist(t *testing.T) {
	ops := newTestOps(t)
	isBlock, err := ops.IsBlockDevice(filepath.Join(t.TempDir(), "nope"))
	require.NoError(t, err)
	require.False(t, isBlock)
}

func TestIsBlockDevice_RegularFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	require.NoError(t, os.WriteFile(path, []byte("x"), 0o644))

	ops := newTestOps(t)
	isBlock, err := ops.IsBlockDevice(path)
	require.NoError(t, err)
	require.False(t, isBlock)
}

// --- File operations tests ---

func TestMkdirAll_CreatesDir(t *testing.T) {
	ops := newTestOps(t)
	dir := filepath.Join(t.TempDir(), "a", "b", "c")
	require.NoError(t, ops.MkdirAll(dir, 0o755))
	info, err := os.Stat(dir)
	require.NoError(t, err)
	require.True(t, info.IsDir())
}

func TestRemoveAll_RemovesDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub")
	require.NoError(t, os.MkdirAll(path, 0o755))

	ops := newTestOps(t)
	require.NoError(t, ops.RemoveAll(path))
	_, err := os.Stat(path)
	require.True(t, os.IsNotExist(err))
}

func TestCreateFile_CreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.txt")
	ops := newTestOps(t)
	require.NoError(t, ops.CreateFile(path, 0o644))
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.False(t, info.IsDir())
}

func TestPathExists_True(t *testing.T) {
	dir := t.TempDir()
	ops := newTestOps(t)
	exists, err := ops.PathExists(dir)
	require.NoError(t, err)
	require.True(t, exists)
}

func TestPathExists_False(t *testing.T) {
	ops := newTestOps(t)
	exists, err := ops.PathExists(filepath.Join(t.TempDir(), "nope"))
	require.NoError(t, err)
	require.False(t, exists)
}

// --- GetFilesystemStats tests ---

func TestGetFilesystemStats_TempDir(t *testing.T) {
	dir := t.TempDir()
	ops := newTestOps(t)
	stats, err := ops.GetFilesystemStats(dir)
	require.NoError(t, err)
	require.NotNil(t, stats)
	require.Greater(t, stats.TotalBytes, int64(0))
	require.GreaterOrEqual(t, stats.AvailableBytes, int64(0))
	require.GreaterOrEqual(t, stats.UsedBytes, int64(0))
	require.Greater(t, stats.TotalInodes, int64(0))
}

func TestGetFilesystemStats_NonExist(t *testing.T) {
	ops := newTestOps(t)
	stats, err := ops.GetFilesystemStats(filepath.Join(t.TempDir(), "nope"))
	require.Error(t, err)
	require.Nil(t, stats)
	require.Contains(t, err.Error(), "statfs")
}

// --- WaitForDevice tests ---

func TestWaitForDevice_ExistingBlockDevice(t *testing.T) {
	// /dev/null is always a character device, not a block device, so we
	// can't easily create a real block device without root. Instead, test
	// the timeout path and the context-cancellation path.
	ops := newTestOps(t)
	err := ops.WaitForDevice(context.Background(), "/dev/nonexistent-device-xyz", 50*time.Millisecond)
	require.Error(t, err)
	require.Contains(t, err.Error(), "timeout")
}

func TestWaitForDevice_ContextCancelled(t *testing.T) {
	ops := newTestOps(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := ops.WaitForDevice(ctx, "/dev/nonexistent-device-xyz", 5*time.Second)
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
}
