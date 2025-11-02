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

package mister

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/rs/zerolog/log"
)

// setupConsoleEnvironment handles common console initialization for console-based launchers.
// This includes:
//   - Checking if FPGA core is active and returning to menu if needed
//   - Opening the console (switching to launcher VT)
//   - Cleaning both F9 console (tty1) and launcher console (tty7)
//
// Returns the ConsoleManager instance for later cleanup, or an error if setup fails.
//
// This function is reusable for any launcher that needs console/framebuffer access
// (video playback, ScummVM, DOSBox, etc.).
func setupConsoleEnvironment(pl *Platform) (platforms.ConsoleManager, error) {
	// Check if FPGA core is active and return to menu if needed
	if pl.isFPGAActive() {
		log.Debug().Msg("FPGA core active, returning to menu before console switch")
		if err := pl.ReturnToMenu(); err != nil {
			return nil, fmt.Errorf("failed to return to menu: %w", err)
		}
	}

	// Get console manager
	cm := pl.ConsoleManager()

	// Switch to console mode (F9 + chvt to launcher VT)
	if err := cm.Open(launcherConsoleVT); err != nil {
		return nil, fmt.Errorf("failed to open console: %w", err)
	}

	// Prepare consoles (hide cursor and clear screen for clean display)
	// Clean F9 console (tty1) - where F9 takes us initially
	if err := cm.Clean(f9ConsoleVT); err != nil {
		log.Debug().Err(err).Msg("failed to clean f9 console")
	}

	// Clean launcher console (tty7) - where content actually displays
	if err := cm.Clean(launcherConsoleVT); err != nil {
		return nil, fmt.Errorf("failed to clean launcher console: %w", err)
	}

	return cm, nil
}

// createConsoleRestoreFunc builds a standard console cleanup function for console-based launchers.
// The returned function handles:
//   - Launching the MiSTer menu core
//   - Restoring cursor state on both F9 and launcher consoles
//   - Pressing F12 to exit console mode
//   - Clearing the console active flag
//   - Grace period for transitions to complete
//
// This cleanup function should be called when:
//   - The launcher process completes naturally
//   - The launcher process crashes (non-signal exit)
//
// Do NOT call this when the process is killed by SIGKILL/SIGTERM (preemption) as the
// new launcher will handle console setup.
//
// This function is reusable for any console-based launcher.
func createConsoleRestoreFunc(pl *Platform, cm platforms.ConsoleManager) func() {
	return func() {
		// Launch menu core to reset video mode and FPGA state
		if err := pl.ReturnToMenu(); err != nil {
			log.Warn().Err(err).Msg("error launching menu during console restore")
		}

		// Restore cursor state on F9 console (tty1)
		if err := cm.Restore(f9ConsoleVT); err != nil {
			log.Warn().Err(err).Msg("error restoring tty1")
		}

		// Restore cursor state on launcher console (tty7)
		if err := cm.Restore(launcherConsoleVT); err != nil {
			log.Warn().Err(err).Msgf("error restoring tty%s", launcherConsoleVT)
		}

		// Exit console mode back to OSD
		if err := pl.KeyboardPress("{f12}"); err != nil {
			log.Warn().Err(err).Msg("error pressing F12 to exit console")
		}

		// Clear console active flag to allow next console launcher
		pl.consoleManager.mu.Lock()
		pl.consoleManager.active = false
		pl.consoleManager.mu.Unlock()

		// Grace period for console/menu transition to complete
		time.Sleep(200 * time.Millisecond)
	}
}

// runTrackedProcess manages the lifecycle of a console-based launcher process.
// This function handles:
//   - Starting the process non-blocking
//   - Tracking the process for StopActiveLauncher
//   - Cleanup goroutine with sophisticated exit handling:
//   - Staleness detection (launcher context cancelled)
//   - Signal detection (SIGKILL/SIGTERM from preemption)
//   - Crash handling (non-zero exit without signal)
//   - Natural completion (zero exit code)
//
// Parameters:
//   - launcherCtx: Context from launcherManager for staleness detection
//   - pl: Platform instance for process tracking
//   - cmd: Prepared exec.Cmd (not yet started)
//   - restoreFunc: Cleanup function (from createConsoleRestoreFunc)
//   - logPrefix: Prefix for log messages (e.g., "fvp", "scummvm")
//
// Returns the process handle for tracking, or error if start fails.
//
// This function is reusable for any console-based launcher.
func runTrackedProcess(
	launcherCtx context.Context,
	pl *Platform,
	cmd *exec.Cmd,
	restoreFunc func(),
	logPrefix string,
) (*os.Process, error) {
	// Start process non-blocking
	if err := cmd.Start(); err != nil {
		restoreFunc()
		return nil, fmt.Errorf("failed to start %s: %w", logPrefix, err)
	}

	// Track process so it can be killed by StopActiveLauncher
	pl.SetTrackedProcess(cmd.Process)

	// Cleanup in goroutine (non-blocking)
	go func() {
		waitErr := cmd.Wait()

		// Check if launcher context is stale (new launcher started)
		if launcherCtx.Err() != nil {
			log.Debug().Msgf("%s cleanup cancelled - launcher superseded", logPrefix)
			return
		}

		// Handle different exit scenarios
		if waitErr != nil {
			// Check if process was killed by signal
			isKilled := false
			exitErr := &exec.ExitError{}
			if errors.As(waitErr, &exitErr) {
				if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
					sig := status.Signal()
					if status.Signaled() && (sig == syscall.SIGKILL || sig == syscall.SIGTERM) {
						isKilled = true
					}
				}
			}

			if isKilled {
				// Process was killed (likely by StopActiveLauncher for new media)
				log.Debug().Msgf("%s stopped by new media launch", logPrefix)
				// Don't restore console - new launcher will handle it
				// Just ensure cursor is restored on our VT in case we need it later
				cm := pl.ConsoleManager()
				if err := cm.Restore(launcherConsoleVT); err != nil {
					log.Warn().Err(err).Msg("error restoring console cursor")
				}
				pl.SetTrackedProcess(nil)
			} else {
				// Process crashed (non-zero exit without signal)
				log.Error().Err(waitErr).Msgf("%s crashed", logPrefix)
				pl.SetTrackedProcess(nil)
				restoreFunc() // Full cleanup on crash
			}
		} else {
			// Process completed normally
			log.Debug().Msgf("%s completed normally", logPrefix)
			pl.SetTrackedProcess(nil)
			restoreFunc() // Full cleanup on natural completion
		}
	}()

	return cmd.Process, nil
}
