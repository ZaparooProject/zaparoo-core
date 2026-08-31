// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later
//
// This file is part of Zaparoo Core.
//
// Zaparoo Core is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// Zaparoo Core is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.

package restart

import (
	"errors"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecAfterRollback_PreservesExecFailure(t *testing.T) {
	t.Parallel()

	rollbackErr := errors.New("startup failed after rollback")
	execErr := errors.New("exec denied")
	err := execAfterRollbackWith(rollbackErr, func() error { return execErr })

	require.Error(t, err)
	require.ErrorIs(t, err, rollbackErr)
	require.ErrorIs(t, err, execErr)
}

func TestExecAfterRollback_HandlesUnexpectedNil(t *testing.T) {
	t.Parallel()

	rollbackErr := errors.New("startup failed after rollback")
	err := execAfterRollbackWith(rollbackErr, func() error { return nil })

	require.Error(t, err)
	require.ErrorIs(t, err, rollbackErr)
	assert.Contains(t, err.Error(), "restart returned unexpectedly")
}

func TestBinaryPath_WithAppEnv(t *testing.T) {
	t.Setenv(config.AppEnv, "/usr/bin/zaparoo")

	path, err := BinaryPath()
	require.NoError(t, err)
	assert.Equal(t, "/usr/bin/zaparoo", path)
}

func TestBinaryPath_WithoutAppEnv(t *testing.T) {
	t.Setenv(config.AppEnv, "")

	path, err := BinaryPath()
	require.NoError(t, err)

	exe, err := os.Executable()
	require.NoError(t, err)
	assert.Equal(t, exe, path)
}

func TestBinaryPath_AppEnvTakesPrecedence(t *testing.T) {
	t.Setenv(config.AppEnv, "/custom/path/zaparoo")

	path, err := BinaryPath()
	require.NoError(t, err)
	assert.Equal(t, "/custom/path/zaparoo", path)

	exe, err := os.Executable()
	require.NoError(t, err)
	assert.NotEqual(t, exe, path)
}

// An installed update stops the service from underneath the UI event loop.
// The loop owns the main thread until it quits, so unless something notices the
// service ending and quits the UI, the process keeps a UI in front of a
// stopped service and never re-execs into the binary that replaced it.
func TestWaitForShutdown_QuitsTheUIAndRestartsWhenTheServiceStopsItself(t *testing.T) {
	t.Parallel()

	done := make(chan struct{})
	quit := make(chan struct{})

	restarting := WaitForShutdown(
		nil, nil, done,
		func() bool { return true },
		func() { close(quit) },
	)

	close(done)
	assert.True(t, <-restarting, "an internal shutdown that wants a restart must ask for one")
	select {
	case <-quit:
	case <-time.After(time.Second):
		t.Fatal("the UI was never told to quit, so its loop would hold the process open")
	}
}

// A service that stopped on its own without asking to be restarted is an
// ordinary shutdown, and re-execing would resurrect a service the user stopped.
func TestWaitForShutdown_DoesNotRestartWhenNoneWasRequested(t *testing.T) {
	t.Parallel()

	for name, restartRequested := range map[string]func() bool{
		"nothing asked": func() bool { return false },
		"no reporter":   nil,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			done := make(chan struct{})
			close(done)

			restarting := WaitForShutdown(nil, nil, done, restartRequested, func() {})
			assert.False(t, <-restarting)
		})
	}
}

// Quitting from the UI, or a signal, ends the run without a restart. The tray
// is quit again in the first case, which is a no-op, because skipping it would
// mean the service-stopped case left the loop running.
func TestWaitForShutdown_EndsWithoutRestartingOnSignalAndOnUIExit(t *testing.T) {
	t.Parallel()

	t.Run("signal", func(t *testing.T) {
		t.Parallel()
		sigs := make(chan os.Signal, 1)
		quit := make(chan struct{})

		restarting := WaitForShutdown(sigs, nil, nil, func() bool { return true }, func() { close(quit) })
		sigs <- syscall.SIGTERM

		assert.False(t, <-restarting, "a signal is not an update")
		<-quit
	})

	t.Run("ui exit", func(t *testing.T) {
		t.Parallel()
		exit := make(chan bool, 1)
		quit := make(chan struct{})

		restarting := WaitForShutdown(nil, exit, nil, func() bool { return true }, func() { close(quit) })
		exit <- true

		assert.False(t, <-restarting)
		<-quit
	})
}
