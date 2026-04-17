//go:build linux

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

package replayos

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/rs/zerolog/log"
)

const (
	screenshotTimeout      = 3 * time.Second
	screenshotPollInterval = 50 * time.Millisecond
	screenshotKeyDelay     = 100 * time.Millisecond
	// screenshotOSDDelay is how long to wait after toggling Caps Lock before
	// sending 's', to allow the "KBD REAL MODE: OFF" OSD banner to disappear.
	screenshotOSDDelay = 1500 * time.Millisecond
)

// Screenshot captures a screenshot of the current RePlayOS display.
//
// RePlayOS writes screenshots via the 's' keyboard hotkey, but that hotkey
// only fires when Keyboard Real Mode is OFF. Since Real Mode defaults to ON,
// this method sends {capslock} to temporarily disable Real Mode, sends 's' to
// trigger the capture, then sends {capslock} again to restore Real Mode. If
// the replay.cfg indicates Real Mode was already OFF at startup, only 's' is
// sent.
//
// RePlayOS saves screenshots to {storage}/captures/{system}/{rom}_{date}_{time}.png.
//
// Limitations:
//   - Requires a libretro game core to be loaded. Screenshots are core-driven;
//     menu captures are not supported by RePlayOS.
//   - Assumes the Real Mode state matches the configured value in replay.cfg.
//     If the user has manually toggled Caps Lock since the last service start,
//     the 's' keypress may reach the emulator as a literal character instead.
func (p *Platform) Screenshot() (*platforms.ScreenshotResult, error) {
	if p.activeStorage == "" {
		return nil, errors.New("no ReplayOS storage detected")
	}

	capturesDir := filepath.Join(p.activeStorage, "captures")
	if err := os.MkdirAll(capturesDir, 0o755); err != nil { //nolint:gosec // System directory
		return nil, fmt.Errorf("create captures dir: %w", err)
	}

	triggerTime := time.Now()
	if err := p.triggerScreenshot(); err != nil {
		return nil, err
	}

	return waitForScreenshot(capturesDir, triggerTime, screenshotTimeout)
}

// triggerScreenshot sends the key sequence that makes RePlayOS take a
// screenshot. When Keyboard Real Mode is ON, wraps 's' with Caps Lock
// presses to temporarily switch to hotkey command mode.
func (p *Platform) triggerScreenshot() error {
	if p.keyboardRealMode {
		if err := p.KeyboardPress("{capslock}"); err != nil {
			return fmt.Errorf("toggle real mode off: %w", err)
		}
		time.Sleep(screenshotOSDDelay)
	}

	if err := p.KeyboardPress("s"); err != nil {
		if p.keyboardRealMode {
			// Best-effort: try to restore Real Mode even after the screenshot key failed.
			if kbErr := p.KeyboardPress("{capslock}"); kbErr != nil {
				log.Trace().Err(kbErr).Msg("best-effort restore keyboard real mode after failed screenshot")
			}
		}
		return fmt.Errorf("send screenshot key: %w", err)
	}

	if p.keyboardRealMode {
		time.Sleep(screenshotKeyDelay)
		if err := p.KeyboardPress("{capslock}"); err != nil {
			log.Warn().Err(err).Msg("failed to restore keyboard real mode after screenshot")
		}
	}

	return nil
}

// waitForScreenshot polls capturesDir for a new .png file with mtime >=
// triggerTime. Once found, it waits for the IEND chunk to confirm the file is
// fully written before reading and returning it.
func waitForScreenshot(
	capturesDir string, triggerTime time.Time, timeout time.Duration,
) (*platforms.ScreenshotResult, error) {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		path, err := findNewestPNG(capturesDir, triggerTime)
		if err != nil {
			return nil, fmt.Errorf("scan captures dir: %w", err)
		}

		if path != "" {
			complete, checkErr := shared.PNGFileComplete(path)
			if checkErr != nil {
				return nil, fmt.Errorf("check screenshot file: %w", checkErr)
			}
			if complete {
				//nolint:gosec // Controlled captures directory
				data, readErr := os.ReadFile(path)
				if readErr != nil {
					return nil, fmt.Errorf("read screenshot file: %w", readErr)
				}
				log.Info().Str("path", path).Msg("screenshot captured")
				return &platforms.ScreenshotResult{Path: path, Data: data}, nil
			}
		}

		time.Sleep(screenshotPollInterval)
	}

	return nil, fmt.Errorf("screenshot timed out after %s", timeout)
}

// findNewestPNG walks capturesDir/{system}/ looking for the newest .png file
// with mtime >= since. Returns its path, or "" if none found.
func findNewestPNG(capturesDir string, since time.Time) (string, error) {
	var bestPath string
	var bestTime time.Time

	systemDirs, err := os.ReadDir(capturesDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("read captures dir: %w", err)
	}

	for _, sysEntry := range systemDirs {
		if !sysEntry.IsDir() {
			continue
		}

		subDir := filepath.Join(capturesDir, sysEntry.Name())
		files, err := os.ReadDir(subDir)
		if err != nil {
			continue
		}

		for _, f := range files {
			if f.IsDir() || filepath.Ext(f.Name()) != ".png" {
				continue
			}
			info, err := f.Info()
			if err != nil {
				continue
			}
			mtime := info.ModTime()
			if !mtime.Before(since) && mtime.After(bestTime) {
				bestPath = filepath.Join(subDir, f.Name())
				bestTime = mtime
			}
		}
	}

	return bestPath, nil
}
