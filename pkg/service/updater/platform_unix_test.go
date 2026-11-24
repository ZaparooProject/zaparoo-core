//go:build !windows

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package updater

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The probe has to see the permissions the kernel enforces, not just what an
// in-memory filesystem was told, since that is what a package-owned directory
// such as /usr/bin looks like to a service running as an ordinary user.
func TestPreflightPlatform_RefusesARealDirectoryWithoutWriteAccess(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}

	dir := filepath.Join(t.TempDir(), "bin")
	require.NoError(t, os.Mkdir(dir, 0o750))
	target := filepath.Join(dir, "zaparoo")
	//nolint:gosec // executable stand-in owned by this test
	require.NoError(t, os.WriteFile(target, []byte("binary"), 0o755))
	//nolint:gosec // a read-only install directory is the point of the test
	require.NoError(t, os.Chmod(dir, 0o555))
	t.Cleanup(func() {
		//nolint:gosec // restores access so TempDir cleanup can remove it
		_ = os.Chmod(dir, 0o750)
	})

	err := preflightPlatform(afero.NewOsFs(), runtime.GOOS, target)

	require.ErrorIs(t, err, ErrPlatformUnsupported)
	entries, readErr := os.ReadDir(dir)
	require.NoError(t, readErr)
	assert.Len(t, entries, 1, "a refused probe must not leave anything behind")
}

func TestPreflightPlatform_AllowsARealWritableDirectory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "zaparoo")
	//nolint:gosec // executable stand-in owned by this test
	require.NoError(t, os.WriteFile(target, []byte("binary"), 0o755))

	require.NoError(t, preflightPlatform(afero.NewOsFs(), runtime.GOOS, target))
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "the probe must remove what it created")
}
