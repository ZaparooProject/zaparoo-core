//go:build windows

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package updater

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func syncDir(dir string) error {
	handle, err := os.Open(dir) //nolint:gosec // callers pass updater-owned or executable parent directories
	if err != nil {
		return fmt.Errorf("opening directory for sync: %w", err)
	}
	syncErr := handle.Sync()
	closeErr := handle.Close()
	// Windows does not expose directory fsync through os.File. Atomic replacement
	// uses MoveFileEx; a permission result here means no stronger barrier exists.
	if syncErr != nil && !windowsDirectorySyncUnsupported(syncErr) {
		return fmt.Errorf("syncing directory: %w", syncErr)
	}
	if closeErr != nil {
		return fmt.Errorf("closing synced directory: %w", closeErr)
	}
	return nil
}

func windowsDirectorySyncUnsupported(err error) bool {
	return errors.Is(err, os.ErrPermission) ||
		errors.Is(err, windows.ERROR_INVALID_FUNCTION) ||
		errors.Is(err, windows.ERROR_NOT_SUPPORTED)
}
