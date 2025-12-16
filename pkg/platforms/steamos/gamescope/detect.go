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

// Package gamescope provides utilities for integrating with Steam's Gaming Mode
// (gamescope compositor) to enable proper window focus for external applications.
package gamescope

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/command"
	"github.com/rs/zerolog/log"
)

const (
	// gamescopeAtom is the X property set by gamescope on its Xwayland displays.
	gamescopeAtom = "GAMESCOPE_XWAYLAND_SERVER_ID"

	// x11SocketDir is the directory containing X11 unix sockets.
	x11SocketDir = "/tmp/.X11-unix"

	// detectTimeout is the maximum time to spend checking for gamescope.
	detectTimeout = 2 * time.Second
)

var (
	// cachedGamingMode stores the cached result of IsGamingMode.
	cachedGamingMode     bool
	cachedGamingModeOnce sync.Once
	// gamescopeDisplay stores the display where gamescope was detected.
	gamescopeDisplay string

	// cmdExecutor is the command executor used by this package.
	// It can be replaced for testing.
	cmdExecutor command.Executor = &command.RealExecutor{}
)

// IsGamingMode returns true if running in a gamescope session (Steam Gaming Mode).
// It detects gamescope by scanning X displays for the GAMESCOPE_XWAYLAND_SERVER_ID atom.
// The result is cached after the first call.
func IsGamingMode() bool {
	cachedGamingModeOnce.Do(func() {
		cachedGamingMode, gamescopeDisplay = detectGamingMode()
		if cachedGamingMode {
			log.Info().
				Str("display", gamescopeDisplay).
				Msg("gamescope Gaming Mode detected")
		} else {
			log.Debug().Msg("not running in Gaming Mode")
		}
	})
	return cachedGamingMode
}

// GamescopeDisplay returns the X display where gamescope was detected.
// Returns empty string if not in Gaming Mode.
func GamescopeDisplay() string {
	_ = IsGamingMode() // Ensure detection has run
	return gamescopeDisplay
}

// detectGamingMode scans X displays for the gamescope atom.
// Returns (true, display) if gamescope is detected.
func detectGamingMode() (found bool, display string) {
	// Find X11 sockets
	sockets, err := filepath.Glob(filepath.Join(x11SocketDir, "X*"))
	if err != nil {
		log.Debug().Err(err).Msg("failed to glob X11 sockets")
		return false, ""
	}

	if len(sockets) == 0 {
		log.Debug().Msg("no X11 sockets found")
		return false, ""
	}

	// Check each display for gamescope atom
	for _, socket := range sockets {
		displayNum := strings.TrimPrefix(filepath.Base(socket), "X")
		display := ":" + displayNum

		if hasGamescopeAtom(display) {
			return true, display
		}
	}

	return false, ""
}

// hasGamescopeAtom checks if a display has the gamescope X atom.
func hasGamescopeAtom(display string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), detectTimeout)
	defer cancel()

	output, err := cmdExecutor.Output(ctx, "xprop", "-display", display, "-root", gamescopeAtom)
	if err != nil {
		// Expected to fail on non-gamescope displays
		log.Debug().
			Str("display", display).
			Err(err).
			Msg("xprop check failed (expected for non-gamescope)")
		return false
	}

	// Gamescope sets this as CARDINAL type
	hasAtom := strings.Contains(string(output), "CARDINAL")
	if hasAtom {
		log.Debug().
			Str("display", display).
			Msg("found gamescope atom")
	}

	return hasAtom
}

// ResetCache clears the cached Gaming Mode detection result.
// Useful for testing or if the session state might have changed.
func ResetCache() {
	cachedGamingModeOnce = sync.Once{}
	cachedGamingMode = false
	gamescopeDisplay = ""
}

// SetExecutor sets the command executor used by this package.
// This is intended for testing purposes only.
func SetExecutor(exec command.Executor) {
	cmdExecutor = exec
}

// ResetExecutor restores the default command executor.
// This should be called after tests that use SetExecutor.
func ResetExecutor() {
	cmdExecutor = &command.RealExecutor{}
}
