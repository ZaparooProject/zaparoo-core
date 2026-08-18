//go:build windows

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package updater

import (
	"errors"
	"fmt"
	"os"
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
	if syncErr != nil && !errors.Is(syncErr, os.ErrPermission) {
		return fmt.Errorf("syncing directory: %w", syncErr)
	}
	if closeErr != nil {
		return fmt.Errorf("closing synced directory: %w", closeErr)
	}
	return nil
}
