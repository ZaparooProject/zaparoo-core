//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package updater

import (
	"errors"
	"fmt"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

type directorySyncHandle interface {
	Sync() error
	Close() error
	Fd() uintptr
}

type linuxDirectorySyncOps struct {
	open   func(string) (directorySyncHandle, error)
	syncfs func(int) error
}

var defaultLinuxDirectorySyncOps = linuxDirectorySyncOps{
	open: func(path string) (directorySyncHandle, error) {
		return os.Open(path) //nolint:gosec // callers pass updater-owned or executable parent directories
	},
	syncfs: unix.Syncfs,
}

func syncDir(dir string) error {
	return syncDirWithOps(dir, defaultLinuxDirectorySyncOps)
}

func syncDirWithOps(dir string, ops linuxDirectorySyncOps) error {
	handle, err := ops.open(dir)
	if err != nil {
		return fmt.Errorf("opening directory for sync: %w", err)
	}

	syncErr := handle.Sync()
	if syncErr != nil && directorySyncUnsupported(syncErr) {
		// vfat/exFAT may reject fsync on a directory handle. syncfs provides the
		// filesystem-wide durability barrier needed before destructive cleanup.
		syncErr = ops.syncfs(int(handle.Fd()))
	}
	closeErr := handle.Close()
	if syncErr != nil {
		return fmt.Errorf("syncing directory: %w", syncErr)
	}
	if closeErr != nil {
		return fmt.Errorf("closing synced directory: %w", closeErr)
	}
	return nil
}

func directorySyncUnsupported(err error) bool {
	return errors.Is(err, syscall.EINVAL) ||
		errors.Is(err, syscall.EPERM) ||
		errors.Is(err, syscall.EACCES) ||
		errors.Is(err, syscall.EBADF) ||
		errors.Is(err, syscall.ENOTSUP)
}
