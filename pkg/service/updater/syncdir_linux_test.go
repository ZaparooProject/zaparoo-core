//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package updater

import (
	"errors"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeDirectorySyncHandle struct {
	syncErr    error
	closeErr   error
	fd         uintptr
	syncCalls  int
	closeCalls int
}

func (h *fakeDirectorySyncHandle) Sync() error {
	h.syncCalls++
	return h.syncErr
}

func (h *fakeDirectorySyncHandle) Close() error {
	h.closeCalls++
	return h.closeErr
}

func (h *fakeDirectorySyncHandle) Fd() uintptr {
	return h.fd
}

func TestSyncDirWithOps_SyncsAndClosesDirectory(t *testing.T) {
	t.Parallel()

	handle := &fakeDirectorySyncHandle{fd: 42}
	syncfsCalls := 0
	err := syncDirWithOps("update-dir", linuxDirectorySyncOps{
		open: func(path string) (directorySyncHandle, error) {
			assert.Equal(t, "update-dir", path)
			return handle, nil
		},
		syncfs: func(int) error {
			syncfsCalls++
			return nil
		},
	})

	require.NoError(t, err)
	assert.Equal(t, 1, handle.syncCalls)
	assert.Equal(t, 1, handle.closeCalls)
	assert.Zero(t, syncfsCalls)
}

func TestDirectorySyncUnsupported(t *testing.T) {
	t.Parallel()

	for _, candidate := range []error{
		syscall.EINVAL,
		syscall.EPERM,
		syscall.EACCES,
		syscall.EBADF,
		syscall.ENOTSUP,
	} {
		t.Run(candidate.Error(), func(t *testing.T) {
			t.Parallel()
			assert.True(t, directorySyncUnsupported(candidate))
		})
	}
	assert.False(t, directorySyncUnsupported(syscall.EIO))
}

func TestSyncDirWithOps_PropagatesSyncfsFailure(t *testing.T) {
	t.Parallel()

	syncfsErr := errors.New("syncfs failed")
	handle := &fakeDirectorySyncHandle{syncErr: syscall.ENOTSUP, fd: 42}
	gotFD := -1
	err := syncDirWithOps("update-dir", linuxDirectorySyncOps{
		open: func(string) (directorySyncHandle, error) { return handle, nil },
		syncfs: func(fd int) error {
			gotFD = fd
			return syncfsErr
		},
	})

	require.ErrorIs(t, err, syncfsErr)
	assert.Equal(t, 42, gotFD)
	assert.Equal(t, 1, handle.closeCalls)
}
