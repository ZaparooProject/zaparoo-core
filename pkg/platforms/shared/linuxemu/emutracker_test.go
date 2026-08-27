//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package linuxemu

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxbase/procscanner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKnownEmulatorProcessesReturnsCopy(t *testing.T) {
	t.Parallel()

	processes := KnownEmulatorProcesses()
	require.NotEmpty(t, processes)
	processes[0] = "modified"
	assert.NotEqual(t, "modified", KnownEmulatorProcesses()[0])
}

func TestEmulatorMatcher(t *testing.T) {
	t.Parallel()

	matcher := newEmulatorMatcher()
	assert.True(t, matcher.Match(procscanner.ProcessInfo{Comm: "RETROARCH"}))
	assert.True(t, matcher.Match(procscanner.ProcessInfo{Comm: "MeLonDS"}))
	assert.False(t, matcher.Match(procscanner.ProcessInfo{Comm: "steam"}))
}

func TestEmulatorTrackerCallbacksAndState(t *testing.T) {
	t.Parallel()

	started := make(chan EmulatorProcess, 1)
	stopped := make(chan EmulatorProcess, 1)
	tracker := NewEmulatorTracker(nil,
		func(name string, pid int, cmdline string) {
			started <- EmulatorProcess{Name: name, PID: pid, Cmdline: cmdline}
		},
		func(name string, pid int) {
			stopped <- EmulatorProcess{Name: name, PID: pid}
		},
	)
	process := procscanner.ProcessInfo{
		Comm: "retroarch", PID: 1234, Cmdline: "retroarch\x00-L\x00core.so\x00game.rom",
	}

	tracker.handleProcessStart(process)
	assert.Equal(t, EmulatorProcess{
		Name: "retroarch", PID: 1234, Cmdline: "retroarch -L core.so game.rom",
	}, <-started)
	assert.Equal(t, []EmulatorProcess{{
		Name: "retroarch", PID: 1234, Cmdline: "retroarch -L core.so game.rom",
	}}, tracker.TrackedEmulators())

	tracker.handleProcessStart(process)
	select {
	case duplicate := <-started:
		t.Fatalf("unexpected duplicate callback: %+v", duplicate)
	default:
	}

	tracker.handleProcessStop(process.PID)
	assert.Equal(t, EmulatorProcess{Name: "retroarch", PID: 1234}, <-stopped)
	assert.Empty(t, tracker.TrackedEmulators())
}

func TestEmulatorTrackerNilScannerAndCallbacks(t *testing.T) {
	t.Parallel()

	tracker := NewEmulatorTracker(nil, nil, nil)
	assert.NotPanics(t, tracker.Start)
	tracker.handleProcessStart(procscanner.ProcessInfo{Comm: "retroarch", PID: 42})
	tracker.handleProcessStop(42)
	assert.Empty(t, tracker.TrackedEmulators())
	assert.NotPanics(t, tracker.Stop)
}
