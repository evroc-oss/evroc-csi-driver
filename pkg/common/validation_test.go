// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 evroc

package common

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRemoveWithinRoot(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "root")
	require.NoError(t, os.Mkdir(root, 0o750))

	t.Run("removes path inside root", func(t *testing.T) {
		path := filepath.Join(root, "empty-dir")
		require.NoError(t, os.Mkdir(path, 0o750))
		require.NoError(t, removeWithinRoot(root, path))
		require.NoDirExists(t, path)
	})

	t.Run("rejects path outside root", func(t *testing.T) {
		path := filepath.Join(parent, "victim")
		require.NoError(t, os.Mkdir(path, 0o750))
		require.Error(t, removeWithinRoot(root, path))
		require.DirExists(t, path)
	})

	t.Run("does not follow symlink outside root", func(t *testing.T) {
		outside := filepath.Join(parent, "outside")
		require.NoError(t, os.Mkdir(outside, 0o750))
		link := filepath.Join(root, "link")
		require.NoError(t, os.Symlink(outside, link))

		require.NoError(t, removeWithinRoot(root, link))
		require.DirExists(t, outside)
		require.NoFileExists(t, link)
	})
}
