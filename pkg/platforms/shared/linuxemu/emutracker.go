//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package linuxemu

import (
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxbase/procscanner"
	"github.com/rs/zerolog/log"
)

// EmulatorStartCallback is called when an emulator process is detected.
type EmulatorStartCallback func(name string, pid int, cmdline string)

// EmulatorStopCallback is called when an emulator process exits.
type EmulatorStopCallback func(name string, pid int)

//nolint:gochecknoglobals // Static process-name catalog.
var knownEmulatorProcesses = []string{
	"retroarch", "dolphin-emu", "dolphin-emu-qt", "citra", "citra-qt", "azahar", "ryujinx", "Ryubing",
	"cemu", "melonDS", "mgba", "snes9x", "snes9x-gtk", "mupen64plus", "mGBA", "duckstation-qt",
	"duckstation-nogui", "PCSX2", "pcsx2", "pcsx2-qt", "rpcs3", "ppsspp", "PPSSPP", "ppsspp-qt",
	"vita3k", "flycast", "kronos", "mednafen", "yabause", "yabause-qt", "blastem", "mame", "mame64",
	"fbneo", "model2emu", "Supermodel", "scummvm", "dosbox", "dosbox-x", "xemu", "xenia", "XeniaCanary",
	"shadps4", "hatari", "stella", "fsuae", "fs-uae", "bluemsx", "openmsx", "vice", "x64", "fuse",
	"86Box", "pcem", "bsnes", "ares",
}

// EmulatorProcess describes one detected emulator process.
type EmulatorProcess struct {
	Name    string
	Cmdline string
	PID     int
}

// EmulatorTracker monitors emulator process lifecycle through a shared scanner.
type EmulatorTracker struct {
	onStart EmulatorStartCallback
	onStop  EmulatorStopCallback
	scanner *procscanner.Scanner
	tracked map[int]*EmulatorProcess
	watchID procscanner.WatchID
	mu      syncutil.Mutex
	watchMu syncutil.Mutex
}

// NewEmulatorTracker creates a tracker using a running process scanner.
func NewEmulatorTracker(
	scanner *procscanner.Scanner,
	onStart EmulatorStartCallback,
	onStop EmulatorStopCallback,
) *EmulatorTracker {
	return &EmulatorTracker{scanner: scanner, onStart: onStart, onStop: onStop, tracked: make(map[int]*EmulatorProcess)}
}

type emulatorMatcher struct{ names map[string]struct{} }

func newEmulatorMatcher() *emulatorMatcher {
	matcher := &emulatorMatcher{names: make(map[string]struct{}, len(knownEmulatorProcesses))}
	for _, name := range knownEmulatorProcesses {
		matcher.names[strings.ToLower(name)] = struct{}{}
	}
	return matcher
}

func (m *emulatorMatcher) Match(proc procscanner.ProcessInfo) bool {
	_, ok := m.names[strings.ToLower(proc.Comm)]
	return ok
}

// Start begins process monitoring.
func (t *EmulatorTracker) Start() {
	if t == nil || t.scanner == nil {
		return
	}
	t.watchMu.Lock()
	defer t.watchMu.Unlock()
	if t.watchID != 0 {
		return
	}
	t.watchID = t.scanner.Watch(newEmulatorMatcher(), procscanner.Callbacks{
		OnStart: t.handleProcessStart, OnStop: t.handleProcessStop,
	})
	log.Info().Msg("emulator tracker started")
}

// Stop ends process monitoring.
func (t *EmulatorTracker) Stop() {
	if t == nil || t.scanner == nil {
		return
	}
	t.watchMu.Lock()
	watchID := t.watchID
	t.watchID = 0
	t.watchMu.Unlock()
	if watchID == 0 {
		return
	}
	t.scanner.Unwatch(watchID)
	log.Info().Msg("emulator tracker stopped")
}

// TrackedEmulators returns a snapshot of active emulator processes.
func (t *EmulatorTracker) TrackedEmulators() []EmulatorProcess {
	t.mu.Lock()
	defer t.mu.Unlock()
	result := make([]EmulatorProcess, 0, len(t.tracked))
	for _, emulator := range t.tracked {
		result = append(result, *emulator)
	}
	return result
}

func (t *EmulatorTracker) handleProcessStart(proc procscanner.ProcessInfo) {
	t.mu.Lock()
	if _, exists := t.tracked[proc.PID]; exists {
		t.mu.Unlock()
		return
	}
	cmdline := strings.TrimSpace(strings.ReplaceAll(proc.Cmdline, "\x00", " "))
	t.tracked[proc.PID] = &EmulatorProcess{Name: proc.Comm, PID: proc.PID, Cmdline: cmdline}
	t.mu.Unlock()
	if t.onStart != nil {
		go t.onStart(proc.Comm, proc.PID, cmdline)
	}
}

func (t *EmulatorTracker) handleProcessStop(pid int) {
	t.mu.Lock()
	emulator, exists := t.tracked[pid]
	if exists {
		delete(t.tracked, pid)
	}
	t.mu.Unlock()
	if exists && t.onStop != nil {
		go t.onStop(emulator.Name, pid)
	}
}

// KnownEmulatorProcesses returns the monitored process-name catalog.
func KnownEmulatorProcesses() []string {
	return append([]string(nil), knownEmulatorProcesses...)
}
