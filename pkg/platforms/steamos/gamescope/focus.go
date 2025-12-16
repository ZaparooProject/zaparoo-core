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

package gamescope

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/rs/zerolog/log"
)

const (
	// windowFindTimeout is how long to wait for the game window to appear.
	windowFindTimeout = 5 * time.Second

	// windowPollInterval is how often to poll for the game window.
	windowPollInterval = 200 * time.Millisecond

	// steamGameAtom is the X property that marks a window as a game.
	steamGameAtom = "STEAM_GAME"

	// baselayerAtom is the X property controlling focus priority.
	baselayerAtom = "GAMESCOPECTRL_BASELAYER_APPID"

	// fakeAppID is the app ID we use for external game windows.
	fakeAppID = "1"
)

// steamWindowPatterns are substrings to exclude when finding game windows.
var steamWindowPatterns = []string{
	"steam",
	"Steam",
	"SteamOverlay",
	"steamwebhelper",
	"Steam Big Picture Mode",
	"mangoapp overlay window",
}

// windowLineRegex matches xwininfo output lines with window IDs and dimensions.
var windowLineRegex = regexp.MustCompile(`^\s*(0x[0-9a-fA-F]+)\s+"[^"]*":\s+\(.*?\)\s+(\d+)x(\d+)`)

// FocusManager handles gamescope focus for a launched process.
type FocusManager struct {
	display       string
	windowID      string
	originalLayer string
	mu            syncutil.Mutex
	reverted      bool
}

// activeFocusManager tracks the current focus manager for cleanup.
var (
	activeFocusManager   *FocusManager
	activeFocusManagerMu syncutil.Mutex
)

// ManageFocus sets up gamescope focus for a process launched in Gaming Mode.
// This should be called in a goroutine after the process starts.
// It finds the game window, sets focus properties, and registers for cleanup.
func ManageFocus(proc *os.Process) {
	if !IsGamingMode() {
		return
	}

	display := GamescopeDisplay()
	if display == "" {
		log.Warn().Msg("gamescope display not found, cannot manage focus")
		return
	}

	log.Debug().
		Int("pid", proc.Pid).
		Str("display", display).
		Msg("setting up gamescope focus management")

	// Find the game window
	windowID, err := findGameWindow(display)
	if err != nil {
		log.Warn().
			Err(err).
			Int("pid", proc.Pid).
			Msg("failed to find game window for focus")
		return
	}

	// Get original baselayer value
	originalLayer, err := getBaselayerValue(display)
	if err != nil {
		log.Warn().Err(err).Msg("failed to get original baselayer value")
		originalLayer = ""
	}

	// Set STEAM_GAME property on the window
	if err := setSteamGameProperty(display, windowID); err != nil {
		log.Warn().Err(err).Msg("failed to set STEAM_GAME property")
		return
	}

	// Set baselayer to give our window focus
	if err := setBaselayerValue(display, fakeAppID, originalLayer); err != nil {
		log.Warn().Err(err).Msg("failed to set baselayer property")
		return
	}

	// Create and register focus manager
	fm := &FocusManager{
		display:       display,
		windowID:      windowID,
		originalLayer: originalLayer,
	}

	activeFocusManagerMu.Lock()
	// Revert any previous focus manager
	if activeFocusManager != nil {
		activeFocusManager.Revert()
	}
	activeFocusManager = fm
	activeFocusManagerMu.Unlock()

	log.Info().
		Str("windowID", windowID).
		Str("display", display).
		Msg("gamescope focus set for game window")
}

// RevertFocus reverts the active focus manager's changes.
// Safe to call even if no focus manager is active.
func RevertFocus() {
	activeFocusManagerMu.Lock()
	fm := activeFocusManager
	activeFocusManager = nil
	activeFocusManagerMu.Unlock()

	if fm != nil {
		fm.Revert()
	}
}

// Revert restores the original baselayer property.
func (fm *FocusManager) Revert() {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	if fm.reverted {
		return
	}
	fm.reverted = true

	if fm.originalLayer == "" {
		log.Debug().Msg("no original baselayer to restore")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), detectTimeout)
	defer cancel()

	//nolint:gosec // G204: display and originalLayer are validated internal values
	cmd := exec.CommandContext(ctx,
		"xprop", "-display", fm.display,
		"-root",
		"-format", baselayerAtom, "32co",
		"-set", baselayerAtom, fm.originalLayer,
	)

	if err := cmd.Run(); err != nil {
		log.Warn().
			Err(err).
			Str("display", fm.display).
			Msg("failed to revert baselayer property")
		return
	}

	log.Debug().
		Str("display", fm.display).
		Str("originalLayer", fm.originalLayer).
		Msg("reverted gamescope baselayer")
}

// findGameWindow waits for and finds a non-Steam game window.
func findGameWindow(display string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), windowFindTimeout)
	defer cancel()

	ticker := time.NewTicker(windowPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("timeout waiting for game window: %w", ctx.Err())
		case <-ticker.C:
			windowID, err := findNonSteamWindow(display)
			if err != nil {
				log.Debug().Err(err).Msg("window search iteration failed")
				continue
			}
			if windowID != "" {
				return windowID, nil
			}
		}
	}
}

// findNonSteamWindow finds a window that isn't Steam-related.
func findNonSteamWindow(display string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), detectTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "xwininfo", "-display", display, "-root", "-tree")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("xwininfo failed: %w", err)
	}

	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := scanner.Text()

		// Skip lines that don't look like window entries
		if !strings.HasPrefix(strings.TrimSpace(line), "0x") {
			continue
		}

		// Skip Steam-related windows
		isSteam := false
		for _, pattern := range steamWindowPatterns {
			if strings.Contains(line, pattern) {
				isSteam = true
				break
			}
		}
		if isSteam {
			continue
		}

		// Check if line has reasonable dimensions (not tiny overlay windows)
		matches := windowLineRegex.FindStringSubmatch(line)
		if matches == nil {
			continue
		}

		windowID := matches[1]

		log.Debug().
			Str("windowID", windowID).
			Str("line", line).
			Msg("found potential game window")

		return windowID, nil
	}

	return "", nil // No window found yet
}

// getBaselayerValue gets the current baselayer property value.
func getBaselayerValue(display string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), detectTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "xprop", "-display", display, "-root", baselayerAtom)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get baselayer: %w", err)
	}

	// Parse output like: GAMESCOPECTRL_BASELAYER_APPID(CARDINAL) = 769, 0
	parts := strings.SplitN(string(output), "=", 2)
	if len(parts) < 2 {
		return "", nil // Property not set
	}

	return strings.TrimSpace(parts[1]), nil
}

// setSteamGameProperty sets the STEAM_GAME property on a window.
func setSteamGameProperty(display, windowID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), detectTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx,
		"xprop", "-display", display,
		"-id", windowID,
		"-f", steamGameAtom, "32c",
		"-set", steamGameAtom, "1",
	)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to set STEAM_GAME: %w", err)
	}

	log.Debug().
		Str("windowID", windowID).
		Msg("set STEAM_GAME property")

	return nil
}

// setBaselayerValue sets the baselayer property to focus our window.
func setBaselayerValue(display, appID, original string) error {
	ctx, cancel := context.WithTimeout(context.Background(), detectTimeout)
	defer cancel()

	// Prepend our app ID to give it priority
	value := appID
	if original != "" {
		value = appID + ", " + original
	}

	//nolint:gosec // G204: display and value are validated internal values
	cmd := exec.CommandContext(ctx,
		"xprop", "-display", display,
		"-root",
		"-format", baselayerAtom, "32co",
		"-set", baselayerAtom, value,
	)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to set baselayer: %w", err)
	}

	log.Debug().
		Str("value", value).
		Msg("set baselayer property")

	return nil
}
