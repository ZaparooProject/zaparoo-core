//go:build linux

// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

package steamos

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxbase/procscanner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKnownEmulatorProcesses(t *testing.T) {
	t.Parallel()

	procs := KnownEmulatorProcesses()

	// Should return a non-empty list
	require.NotEmpty(t, procs)

	// Verify it's a copy, not the original slice
	procs[0] = "modified"
	originalProcs := KnownEmulatorProcesses()
	assert.NotEqual(t, "modified", originalProcs[0], "KnownEmulatorProcesses should return a copy")

	// Check for some expected emulators
	expectedEmulators := []string{
		"retroarch",
		"dolphin-emu",
		"pcsx2",
		"mame",
		"scummvm",
	}

	for _, expected := range expectedEmulators {
		found := false
		for _, proc := range originalProcs {
			if proc == expected {
				found = true
				break
			}
		}
		assert.True(t, found, "expected emulator %s not found in list", expected)
	}
}

func TestEmulatorMatcher_Match(t *testing.T) {
	t.Parallel()

	matcher := newEmulatorMatcher()

	tests := []struct {
		name     string
		procInfo procscanner.ProcessInfo
		want     bool
	}{
		{
			name:     "exact match retroarch",
			procInfo: procscanner.ProcessInfo{Comm: "retroarch", PID: 1234},
			want:     true,
		},
		{
			name:     "exact match case insensitive",
			procInfo: procscanner.ProcessInfo{Comm: "RETROARCH", PID: 1234},
			want:     true,
		},
		{
			name:     "exact match dolphin-emu",
			procInfo: procscanner.ProcessInfo{Comm: "dolphin-emu", PID: 1234},
			want:     true,
		},
		{
			name:     "exact match PCSX2 uppercase",
			procInfo: procscanner.ProcessInfo{Comm: "PCSX2", PID: 1234},
			want:     true,
		},
		{
			name:     "exact match pcsx2 lowercase",
			procInfo: procscanner.ProcessInfo{Comm: "pcsx2", PID: 1234},
			want:     true,
		},
		{
			name:     "mixed case match",
			procInfo: procscanner.ProcessInfo{Comm: "MeLonDS", PID: 1234},
			want:     true,
		},
		{
			name:     "no match for unknown process",
			procInfo: procscanner.ProcessInfo{Comm: "firefox", PID: 1234},
			want:     false,
		},
		{
			name:     "no match for empty string",
			procInfo: procscanner.ProcessInfo{Comm: "", PID: 1234},
			want:     false,
		},
		{
			name:     "no match for steam",
			procInfo: procscanner.ProcessInfo{Comm: "steam", PID: 1234},
			want:     false,
		},
		{
			name:     "match for mame",
			procInfo: procscanner.ProcessInfo{Comm: "mame", PID: 1234},
			want:     true,
		},
		{
			name:     "match for duckstation-qt",
			procInfo: procscanner.ProcessInfo{Comm: "duckstation-qt", PID: 1234},
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := matcher.Match(tt.procInfo)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNewEmulatorTracker(t *testing.T) {
	t.Parallel()

	var startCalled bool
	var stopCalled bool

	onStart := func(_ string, _ int, _ string) {
		startCalled = true
	}
	onStop := func(_ string, _ int) {
		stopCalled = true
	}

	// Create tracker without a scanner (nil is acceptable for construction)
	tracker := NewEmulatorTracker(nil, onStart, onStop)

	require.NotNil(t, tracker)
	assert.NotNil(t, tracker.tracked, "tracked map should be initialized")
	assert.NotNil(t, tracker.onStart, "onStart callback should be set")
	assert.NotNil(t, tracker.onStop, "onStop callback should be set")

	// Verify callbacks are stored (not called yet)
	assert.False(t, startCalled)
	assert.False(t, stopCalled)
}

func TestEmulatorTracker_TrackedEmulators(t *testing.T) {
	t.Parallel()

	tracker := NewEmulatorTracker(nil, nil, nil)

	// Initially empty
	emulators := tracker.TrackedEmulators()
	assert.Empty(t, emulators)

	// Manually add tracked emulators to test the method
	tracker.tracked[1234] = &EmulatorProcess{
		Name:    "retroarch",
		PID:     1234,
		Cmdline: "retroarch /path/to/game.rom",
	}
	tracker.tracked[5678] = &EmulatorProcess{
		Name:    "dolphin-emu",
		PID:     5678,
		Cmdline: "dolphin-emu /path/to/game.iso",
	}

	emulators = tracker.TrackedEmulators()
	assert.Len(t, emulators, 2)

	// Verify the internal map maintains state (checking copy behavior)
	assert.Len(t, tracker.tracked, 2)
}

func TestEmulatorTracker_HandleProcessStart(t *testing.T) {
	t.Parallel()

	var receivedName string
	var receivedPID int
	var receivedCmdline string
	callbackChan := make(chan struct{}, 1)

	onStart := func(name string, pid int, cmdline string) {
		receivedName = name
		receivedPID = pid
		receivedCmdline = cmdline
		callbackChan <- struct{}{}
	}

	tracker := NewEmulatorTracker(nil, onStart, nil)

	// Simulate process start
	proc := procscanner.ProcessInfo{
		Comm:    "retroarch",
		PID:     1234,
		Cmdline: "retroarch\x00/path/to/game.rom",
	}
	tracker.handleProcessStart(proc)

	// Wait for callback
	<-callbackChan

	// Verify tracking
	assert.Len(t, tracker.tracked, 1)
	assert.Equal(t, "retroarch", receivedName)
	assert.Equal(t, 1234, receivedPID)
	assert.Equal(t, "retroarch /path/to/game.rom", receivedCmdline)

	// Duplicate start should be ignored
	tracker.handleProcessStart(proc)
	assert.Len(t, tracker.tracked, 1)
}

func TestEmulatorTracker_HandleProcessStop(t *testing.T) {
	t.Parallel()

	var receivedName string
	var receivedPID int
	callbackChan := make(chan struct{}, 1)

	onStop := func(name string, pid int) {
		receivedName = name
		receivedPID = pid
		callbackChan <- struct{}{}
	}

	tracker := NewEmulatorTracker(nil, nil, onStop)

	// Pre-populate tracked emulator
	tracker.tracked[1234] = &EmulatorProcess{
		Name:    "retroarch",
		PID:     1234,
		Cmdline: "retroarch /path/to/game.rom",
	}

	// Simulate process stop
	tracker.handleProcessStop(1234)

	// Wait for callback
	<-callbackChan

	// Verify removal from tracking
	assert.Empty(t, tracker.tracked)
	assert.Equal(t, "retroarch", receivedName)
	assert.Equal(t, 1234, receivedPID)

	// Stop for unknown PID should be no-op
	tracker.handleProcessStop(9999)
	assert.Empty(t, tracker.tracked)
}

func TestEmulatorTracker_CmdlineSanitization(t *testing.T) {
	t.Parallel()

	var receivedCmdline string
	callbackChan := make(chan struct{}, 1)

	onStart := func(_ string, _ int, cmdline string) {
		receivedCmdline = cmdline
		callbackChan <- struct{}{}
	}

	tracker := NewEmulatorTracker(nil, onStart, nil)

	tests := []struct {
		name     string
		cmdline  string
		expected string
	}{
		{
			name:     "null bytes replaced with spaces",
			cmdline:  "retroarch\x00-L\x00/path/core.so\x00/path/game.rom",
			expected: "retroarch -L /path/core.so /path/game.rom",
		},
		{
			name:     "trailing whitespace trimmed",
			cmdline:  "retroarch /path/game.rom  \n",
			expected: "retroarch /path/game.rom",
		},
		{
			name:     "empty cmdline",
			cmdline:  "",
			expected: "",
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proc := procscanner.ProcessInfo{
				Comm:    "retroarch",
				PID:     1000 + i,
				Cmdline: tt.cmdline,
			}
			tracker.handleProcessStart(proc)
			<-callbackChan

			assert.Equal(t, tt.expected, receivedCmdline)
		})
	}
}

func TestEmulatorTracker_NilCallbacks(t *testing.T) {
	t.Parallel()

	// Should not panic with nil callbacks
	tracker := NewEmulatorTracker(nil, nil, nil)

	proc := procscanner.ProcessInfo{
		Comm:    "retroarch",
		PID:     1234,
		Cmdline: "retroarch /game.rom",
	}

	// These should not panic
	tracker.handleProcessStart(proc)
	assert.Len(t, tracker.tracked, 1)

	tracker.handleProcessStop(1234)
	assert.Empty(t, tracker.tracked)
}
