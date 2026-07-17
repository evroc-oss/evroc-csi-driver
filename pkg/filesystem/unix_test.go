// SPDX-License-Identifier: MIT
// Copyright (c) 2025 evroc

package filesystem

import (
	"fmt"
	"testing"

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
