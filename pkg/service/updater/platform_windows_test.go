//go:build windows

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package updater

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

func TestCheckTargetRenameAllowed_RequestsDeleteAccessAndRetriesSharingViolation(t *testing.T) {
	t.Parallel()

	const targetPath = `C:\Apps\Zaparoo\zaparoo.exe`
	openCalls := 0
	closeCalls := 0
	sleeps := 0
	ops := targetRenameAccessOps{
		createFile: func(
			name *uint16,
			access uint32,
			mode uint32,
			sa *windows.SecurityAttributes,
			createMode uint32,
			attrs uint32,
			templateFile windows.Handle,
		) (windows.Handle, error) {
			openCalls++
			assert.Equal(t, targetPath, windows.UTF16PtrToString(name))
			assert.Equal(t, uint32(windows.DELETE), access)
			assert.Equal(t, uint32(windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE), mode)
			assert.Nil(t, sa)
			assert.Equal(t, uint32(windows.OPEN_EXISTING), createMode)
			assert.Equal(t, uint32(windows.FILE_ATTRIBUTE_NORMAL), attrs)
			assert.Zero(t, templateFile)
			if openCalls == 1 {
				return windows.InvalidHandle, windows.ERROR_SHARING_VIOLATION
			}
			return windows.Handle(42), nil
		},
		closeHandle: func(handle windows.Handle) error {
			closeCalls++
			assert.Equal(t, windows.Handle(42), handle)
			return nil
		},
		retry: swapOps{
			transient: transientSwapError,
			sleep:     func(time.Duration) { sleeps++ },
		},
	}

	require.NoError(t, checkTargetRenameAllowedWith(targetPath, ops))
	assert.Equal(t, 2, openCalls)
	assert.Equal(t, 1, closeCalls)
	assert.Equal(t, 1, sleeps)
}

func TestCheckTargetRenameAllowed_ReportsOpenFailure(t *testing.T) {
	t.Parallel()

	openErr := errors.New("open failed")
	closeCalls := 0
	ops := targetRenameAccessOps{
		createFile: func(
			*uint16, uint32, uint32, *windows.SecurityAttributes, uint32, uint32, windows.Handle,
		) (windows.Handle, error) {
			return windows.InvalidHandle, openErr
		},
		closeHandle: func(windows.Handle) error {
			closeCalls++
			return nil
		},
		retry: swapOps{transient: func(error) bool { return false }},
	}

	err := checkTargetRenameAllowedWith(`C:\zaparoo.exe`, ops)
	require.ErrorIs(t, err, openErr)
	assert.Zero(t, closeCalls)
}

func TestCheckTargetRenameAllowed_ReportsCloseFailure(t *testing.T) {
	t.Parallel()

	closeErr := errors.New("close failed")
	ops := targetRenameAccessOps{
		createFile: func(
			*uint16, uint32, uint32, *windows.SecurityAttributes, uint32, uint32, windows.Handle,
		) (windows.Handle, error) {
			return windows.Handle(42), nil
		},
		closeHandle: func(handle windows.Handle) error {
			assert.Equal(t, windows.Handle(42), handle)
			return closeErr
		},
		retry: swapOps{transient: func(error) bool { return false }},
	}

	err := checkTargetRenameAllowedWith(`C:\zaparoo.exe`, ops)
	require.ErrorIs(t, err, closeErr)
}
