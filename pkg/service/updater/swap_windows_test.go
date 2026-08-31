//go:build windows

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package updater

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

func TestHideFile_ClearsReadOnlySoTheSidecarCanBeSwept(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "zaparoo.zaparoo-update-old.exe")
	require.NoError(t, os.WriteFile(path, []byte("old binary"), 0o600))
	name, err := windows.UTF16PtrFromString(path)
	require.NoError(t, err)

	attrs, err := windows.GetFileAttributes(name)
	require.NoError(t, err)
	require.NoError(t, windows.SetFileAttributes(name, attrs|windows.FILE_ATTRIBUTE_READONLY))
	t.Cleanup(func() {
		cleanupAttrs, cleanupErr := windows.GetFileAttributes(name)
		if cleanupErr == nil {
			_ = windows.SetFileAttributes(name, cleanupAttrs&^windows.FILE_ATTRIBUTE_READONLY)
		}
		_ = os.Remove(path)
	})

	require.NoError(t, hideFile(path))
	got, err := windows.GetFileAttributes(name)
	require.NoError(t, err)
	assert.NotZero(t, got&windows.FILE_ATTRIBUTE_HIDDEN)
	assert.Zero(t, got&windows.FILE_ATTRIBUTE_READONLY)
	require.NoError(t, os.Remove(path), "a later sweep must be able to delete the sidecar")
}
