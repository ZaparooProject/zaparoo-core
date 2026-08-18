//go:build !windows

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package updater

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

const restrictiveUmaskHelperEnv = "ZAPAROO_TEST_RESTRICTIVE_UPDATE_UMASK"

func TestPreserveCurrentBinary_RestoresExecuteBitsAfterRestrictiveUmask(t *testing.T) {
	if os.Getenv(restrictiveUmaskHelperEnv) == "1" {
		dir := t.TempDir()
		targetPath := filepath.Join(dir, "zaparoo")
		backupPath := installSidecarPath(targetPath, installBackupSuffix)
		//nolint:gosec // executable stand-in owned by this test
		require.NoError(t, os.WriteFile(targetPath, []byte("old binary"), 0o755))

		previousUmask := syscall.Umask(0o777)
		defer syscall.Umask(previousUmask)

		require.NoError(t, preserveCurrentBinary(targetPath, backupPath))
		info, err := os.Stat(backupPath)
		require.NoError(t, err)
		require.NotZero(t, info.Mode().Perm()&0o111)
		return
	}

	//nolint:gosec // controlled self-exec of current test binary with a fixed test selector
	cmd := exec.CommandContext(t.Context(), os.Args[0],
		"-test.run=^TestPreserveCurrentBinary_RestoresExecuteBitsAfterRestrictiveUmask$")
	cmd.Env = append(os.Environ(), restrictiveUmaskHelperEnv+"=1")
	output, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "restrictive-umask helper failed:\n%s", output)
}
