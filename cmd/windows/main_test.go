//go:build windows

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"errors"
	"os"
	"syscall"
	"testing"
	"time"

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

// An installed update stops the service from underneath the tray's event loop.
// The loop owns the main thread until it quits, so unless something notices the
// service ending and quits the tray, the process keeps a tray icon in front of a
// stopped service and never re-execs into the binary that replaced it.
func TestWatchForShutdown_QuitsTheTrayAndRestartsWhenTheServiceStopsItself(t *testing.T) {
	t.Parallel()

	done := make(chan struct{})
	quit := make(chan struct{})

	restarting := watchForShutdown(
		nil, nil, done,
		func() bool { return true },
		func() { close(quit) },
	)

	close(done)
	assert.True(t, <-restarting, "an internal shutdown that wants a restart must ask for one")
	select {
	case <-quit:
	case <-time.After(time.Second):
		t.Fatal("the tray was never told to quit, so its loop would hold the process open")
	}
}

// A service that stopped on its own without asking to be restarted is an
// ordinary shutdown, and re-execing would resurrect a service the user stopped.
func TestWatchForShutdown_DoesNotRestartWhenNoneWasRequested(t *testing.T) {
	t.Parallel()

	for name, restartRequested := range map[string]func() bool{
		"nothing asked": func() bool { return false },
		"no reporter":   nil,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			done := make(chan struct{})
			close(done)

			restarting := watchForShutdown(nil, nil, done, restartRequested, func() {})
			assert.False(t, <-restarting)
		})
	}
}

// Quitting from the tray, or a signal, ends the run without a restart. The tray
// is quit again in the first case, which is a no-op, because skipping it would
// mean the service-stopped case left the loop running.
func TestWatchForShutdown_EndsWithoutRestartingOnSignalAndOnTrayExit(t *testing.T) {
	t.Parallel()

	t.Run("signal", func(t *testing.T) {
		t.Parallel()
		sigs := make(chan os.Signal, 1)
		quit := make(chan struct{})

		restarting := watchForShutdown(sigs, nil, nil, func() bool { return true }, func() { close(quit) })
		sigs <- syscall.SIGTERM

		assert.False(t, <-restarting, "a signal is not an update")
		<-quit
	})

	t.Run("tray exit", func(t *testing.T) {
		t.Parallel()
		exit := make(chan bool, 1)
		quit := make(chan struct{})

		restarting := watchForShutdown(nil, exit, nil, func() bool { return true }, func() { close(quit) })
		exit <- true

		assert.False(t, <-restarting)
		<-quit
	})
}
