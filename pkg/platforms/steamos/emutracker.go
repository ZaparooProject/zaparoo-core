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
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxbase/procscanner"
	"github.com/rs/zerolog/log"
)

// EmulatorStartCallback is called when an emulator process is detected.
type EmulatorStartCallback func(name string, pid int, cmdline string)

// EmulatorStopCallback is called when an emulator process exits.
type EmulatorStopCallback func(name string, pid int)

// knownEmulatorProcesses is a list of process names to monitor for emulator detection.
// These are the typical process names for emulators installed via EmuDeck/RetroDECK.
//
//nolint:gochecknoglobals // Package-level configuration
var knownEmulatorProcesses = []string{
	// RetroArch (most common for EmuDeck)
	"retroarch",

	// Nintendo standalone emulators
	"dolphin-emu",    // GameCube/Wii
	"dolphin-emu-qt", // GameCube/Wii Qt frontend
	"citra",          // 3DS
	"citra-qt",       // 3DS Qt frontend
	"yuzu",           // Switch (deprecated but may still be in use)
	"ryujinx",        // Switch
	"cemu",           // Wii U
	"melonDS",        // DS
	"mgba",           // GBA standalone
	"snes9x",         // SNES standalone
	"snes9x-gtk",     // SNES GTK frontend
	"mupen64plus",    // N64 standalone
	"mGBA",           // GBA

	// Sony emulators
	"duckstation-qt",    // PSX
	"duckstation-nogui", // PSX headless
	"PCSX2",             // PS2
	"pcsx2",             // PS2 (lowercase)
	"pcsx2-qt",          // PS2 Qt frontend
	"rpcs3",             // PS3
	"ppsspp",            // PSP
	"PPSSPP",            // PSP (uppercase)
	"ppsspp-qt",         // PSP Qt frontend
	"vita3k",            // PS Vita

	// Sega emulators
	"flycast",         // Dreamcast/NAOMI
	"kronos",          // Saturn
	"mednafen",        // Multi-system including Saturn
	"yabause",         // Saturn
	"yabause-qt",      // Saturn Qt frontend
	"blastem",         // Genesis/Mega Drive
	"genesis-plus-gx", // Genesis/Mega Drive (RA core name for process matching)

	// Arcade emulators
	"mame",       // MAME standalone
	"mame64",     // MAME 64-bit
	"fbneo",      // FinalBurn Neo standalone
	"model2emu",  // Sega Model 2
	"Supermodel", // Sega Model 3

	// Other emulators
	"scummvm",  // ScummVM
	"dosbox",   // DOSBox
	"dosbox-x", // DOSBox-X
	"xemu",     // Original Xbox
	"xenia",    // Xbox 360
	"hatari",   // Atari ST
	"stella",   // Atari 2600
	"fsuae",    // Amiga
	"fs-uae",   // Amiga (hyphenated)
	"bluemsx",  // MSX
	"openmsx",  // MSX
	"vice",     // C64
	"x64",      // VICE C64
	"fuse",     // ZX Spectrum
	"86Box",    // PC/DOS
	"pcem",     // PC/DOS
	"bsnes",    // SNES (higan/bsnes)
	"ares",     // Multi-system
}

// EmulatorProcess represents a detected emulator process.
type EmulatorProcess struct {
	Name    string
	Cmdline string
	PID     int
}

// EmulatorTracker monitors emulator processes for game lifecycle tracking.
type EmulatorTracker struct {
	onStart EmulatorStartCallback
	onStop  EmulatorStopCallback
	scanner *procscanner.Scanner
	tracked map[int]*EmulatorProcess
	watchID procscanner.WatchID
	mu      syncutil.Mutex
}

// NewEmulatorTracker creates a new emulator process tracker.
// scanner must be a running process scanner.
func NewEmulatorTracker(
	scanner *procscanner.Scanner,
	onStart EmulatorStartCallback,
	onStop EmulatorStopCallback,
) *EmulatorTracker {
	return &EmulatorTracker{
		scanner: scanner,
		onStart: onStart,
		onStop:  onStop,
		tracked: make(map[int]*EmulatorProcess),
	}
}

// emulatorMatcher matches known emulator processes.
type emulatorMatcher struct {
	names map[string]bool
}

func newEmulatorMatcher() *emulatorMatcher {
	m := &emulatorMatcher{
		names: make(map[string]bool, len(knownEmulatorProcesses)),
	}
	for _, name := range knownEmulatorProcesses {
		m.names[strings.ToLower(name)] = true
	}
	return m
}

func (m *emulatorMatcher) Match(proc procscanner.ProcessInfo) bool {
	return m.names[strings.ToLower(proc.Comm)]
}

// Start begins monitoring for emulator processes.
func (t *EmulatorTracker) Start() {
	t.watchID = t.scanner.Watch(
		newEmulatorMatcher(),
		procscanner.Callbacks{
			OnStart: t.handleProcessStart,
			OnStop:  t.handleProcessStop,
		},
	)
	log.Info().Msg("emulator tracker started")
}

// Stop stops the emulator tracker.
func (t *EmulatorTracker) Stop() {
	t.scanner.Unwatch(t.watchID)
	log.Info().Msg("emulator tracker stopped")
}

// TrackedEmulators returns a copy of currently tracked emulators.
func (t *EmulatorTracker) TrackedEmulators() []EmulatorProcess {
	t.mu.Lock()
	defer t.mu.Unlock()

	emulators := make([]EmulatorProcess, 0, len(t.tracked))
	for _, emu := range t.tracked {
		emulators = append(emulators, *emu)
	}
	return emulators
}

// handleProcessStart is called when an emulator process is detected.
func (t *EmulatorTracker) handleProcessStart(proc procscanner.ProcessInfo) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Skip if already tracking this PID
	if _, exists := t.tracked[proc.PID]; exists {
		return
	}

	// Track the emulator
	cmdline := strings.ReplaceAll(proc.Cmdline, "\x00", " ")
	cmdline = strings.TrimSpace(cmdline)

	emu := &EmulatorProcess{
		Name:    proc.Comm,
		PID:     proc.PID,
		Cmdline: cmdline,
	}

	t.tracked[proc.PID] = emu

	log.Info().
		Str("name", proc.Comm).
		Int("pid", proc.PID).
		Msg("detected emulator start")

	// Call callback
	if t.onStart != nil {
		go t.onStart(proc.Comm, proc.PID, cmdline)
	}
}

// handleProcessStop is called when a tracked emulator process exits.
func (t *EmulatorTracker) handleProcessStop(pid int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	emu, exists := t.tracked[pid]
	if !exists {
		return
	}

	name := emu.Name

	// Clean up tracking state
	delete(t.tracked, pid)

	log.Info().
		Str("name", name).
		Int("pid", pid).
		Msg("detected emulator exit")

	// Call callback
	if t.onStop != nil {
		go t.onStop(name, pid)
	}
}

// KnownEmulatorProcesses returns the list of known emulator process names.
// This can be used for reference or extending the list via configuration.
func KnownEmulatorProcesses() []string {
	result := make([]string, len(knownEmulatorProcesses))
	copy(result, knownEmulatorProcesses)
	return result
}
