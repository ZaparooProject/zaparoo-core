//go:build windows

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	syswindows "golang.org/x/sys/windows"
)

func TestAcquireSingleInstance_Success(t *testing.T) {
	t.Parallel()

	closeCalls := 0
	ops := singleInstanceOps{
		createMutex: func(
			sa *syswindows.SecurityAttributes,
			initialOwner bool,
			name *uint16,
		) (syswindows.Handle, error) {
			assert.Nil(t, sa)
			assert.False(t, initialOwner)
			assert.Equal(t, "MUTEX: Zaparoo Core", syswindows.UTF16PtrToString(name))
			return syswindows.Handle(42), nil
		},
		closeHandle: func(handle syswindows.Handle) error {
			closeCalls++
			assert.Equal(t, syswindows.Handle(42), handle)
			return nil
		},
	}

	instance, running := acquireSingleInstanceWith(ops)

	assert.False(t, running)
	require.NotNil(t, instance)
	assert.Equal(t, syswindows.Handle(42), instance.handle)
	require.NoError(t, instance.release())
	assert.Equal(t, 1, closeCalls)
}

func TestAcquireSingleInstance_AlreadyExistsClosesDuplicateHandle(t *testing.T) {
	t.Parallel()

	closeCalls := 0
	ops := singleInstanceOps{
		createMutex: func(
			*syswindows.SecurityAttributes, bool, *uint16,
		) (syswindows.Handle, error) {
			return syswindows.Handle(42), syswindows.ERROR_ALREADY_EXISTS
		},
		closeHandle: func(handle syswindows.Handle) error {
			closeCalls++
			assert.Equal(t, syswindows.Handle(42), handle)
			return nil
		},
	}

	instance, running := acquireSingleInstanceWith(ops)

	assert.True(t, running)
	assert.Nil(t, instance)
	assert.Equal(t, 1, closeCalls)
}

func TestAcquireSingleInstance_CreationFailureAllowsStartup(t *testing.T) {
	t.Parallel()

	createErr := errors.New("create failed")
	closeCalls := 0
	ops := singleInstanceOps{
		createMutex: func(
			*syswindows.SecurityAttributes, bool, *uint16,
		) (syswindows.Handle, error) {
			return 0, createErr
		},
		closeHandle: func(syswindows.Handle) error {
			closeCalls++
			return nil
		},
	}

	instance, running := acquireSingleInstanceWith(ops)

	assert.False(t, running)
	assert.Nil(t, instance)
	assert.Zero(t, closeCalls)
}

func TestRestartAfterReleasing_ReleasesSingletonBeforeRestart(t *testing.T) {
	t.Parallel()

	var events []string
	instance := &singleInstance{
		handle: 1,
		closeHandle: func(syswindows.Handle) error {
			events = append(events, "release")
			return nil
		},
	}

	err := restartAfterReleasing(instance, func() error {
		events = append(events, "restart")
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, []string{"release", "restart"}, events)
	assert.Zero(t, instance.handle)
}

func TestRestartAfterReleasing_DoesNotRestartWhenReleaseFails(t *testing.T) {
	t.Parallel()

	releaseErr := errors.New("close failed")
	restarted := false
	instance := &singleInstance{
		handle:      1,
		closeHandle: func(syswindows.Handle) error { return releaseErr },
	}

	err := restartAfterReleasing(instance, func() error {
		restarted = true
		return nil
	})

	require.ErrorIs(t, err, releaseErr)
	assert.False(t, restarted)
	assert.NotZero(t, instance.handle)
}
