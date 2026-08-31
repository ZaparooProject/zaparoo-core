//go:build windows

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package updater

import (
	"fmt"

	"golang.org/x/sys/windows"
)

type targetRenameAccessOps struct {
	createFile func(
		name *uint16,
		access uint32,
		mode uint32,
		sa *windows.SecurityAttributes,
		createMode uint32,
		attrs uint32,
		templateFile windows.Handle,
	) (windows.Handle, error)
	closeHandle func(windows.Handle) error
	retry       swapOps
}

// checkTargetRenameAllowed asks Windows for DELETE access to the target. A
// same-volume rename needs that access even when the parent directory allows
// creating and removing other files. Sharing failures are retried because a
// scanner can briefly hold the executable without sharing delete.
func checkTargetRenameAllowed(targetPath string) error {
	return checkTargetRenameAllowedWith(targetPath, targetRenameAccessOps{
		createFile:  windows.CreateFile,
		closeHandle: windows.CloseHandle,
		retry:       defaultSwapOps(),
	})
}

func checkTargetRenameAllowedWith(targetPath string, ops targetRenameAccessOps) error {
	name, err := windows.UTF16PtrFromString(targetPath)
	if err != nil {
		return fmt.Errorf("encoding executable path: %w", err)
	}

	return retrySwap(ops.retry, swapAttempts, func() error {
		handle, openErr := ops.createFile(
			name,
			windows.DELETE,
			windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
			nil,
			windows.OPEN_EXISTING,
			windows.FILE_ATTRIBUTE_NORMAL,
			0,
		)
		if openErr != nil {
			return fmt.Errorf("opening executable with rename access: %w", openErr)
		}
		if closeErr := ops.closeHandle(handle); closeErr != nil {
			return fmt.Errorf("closing executable rename probe: %w", closeErr)
		}
		return nil
	})
}
