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
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
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
// The provided context can be used to cancel console operations if the launcher is superseded.
//
// Returns the ConsoleManager instance for later cleanup, or an error if setup fails.
//
// This function is reusable for any launcher that needs console/framebuffer access
// (video playback, ScummVM, DOSBox, etc.).
func setupConsoleEnvironment(ctx context.Context, pl *Platform) (platforms.ConsoleManager, error) {
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
	if err := cm.Open(ctx, launcherConsoleVT); err != nil {
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
		// Exit console mode FIRST before loading menu
		// If we call LaunchMenu() while in console mode, MiSTer Main switches to tty2
		if err := pl.KeyboardPress("{f12}"); err != nil {
			log.Error().Err(err).Msg("error pressing F12 to exit console")
		}
		time.Sleep(100 * time.Millisecond)

		// Restore cursor state on F9 console (tty1) and launcher console (tty7)
		if err := cm.Restore(f9ConsoleVT); err != nil {
			log.Warn().Err(err).Msg("error restoring tty1 cursor")
		}
		if err := cm.Restore(launcherConsoleVT); err != nil {
			log.Warn().Err(err).Msgf("error restoring tty%s cursor", launcherConsoleVT)
		}

		// NOW load menu core after exiting console mode
		if err := pl.ReturnToMenu(); err != nil {
			log.Error().Err(err).Msg("error launching menu")
		}

		// Clear console active flag
		pl.consoleManager.mu.Lock()
		pl.consoleManager.active = false
		pl.consoleManager.mu.Unlock()

		time.Sleep(200 * time.Millisecond)
	}
}

// runTrackedProcess manages the lifecycle of a console-based launcher process.
// This function handles:
//   - Starting the process non-blocking
//   - Tracking the process and cleanup completion channel for StopActiveLauncher
//   - Cleanup goroutine that ALWAYS runs restoreFunc regardless of preemption
//   - Signaling cleanup completion via channel for synchronization
//
// The cleanup goroutine runs restoreFunc unconditionally because console state
// (VT mode, cursor, video mode) must be restored even if the launcher was preempted.
// StopActiveLauncher waits on the completion channel before starting a new launcher.
//
// Parameters:
//   - pl: Platform instance for process tracking
//   - cmd: Prepared exec.Cmd (not yet started)
//   - restoreFunc: Cleanup function (from createConsoleRestoreFunc)
//   - logPrefix: Prefix for log messages (e.g., "fvp", "scummvm")
//
// Returns the process handle for tracking, or error if start fails.
//
// This function is reusable for any console-based launcher.
func runTrackedProcess(
	pl *Platform,
	cmd *exec.Cmd,
	restoreFunc func(),
	logPrefix string,
) (*os.Process, error) {
	// Create cleanup completion channel BEFORE starting process
	done := make(chan struct{})

	// Redirect stdin/stdout to /dev/null to prevent console text interference
	devNull, devErr := os.OpenFile("/dev/null", os.O_RDWR, 0)
	if devErr != nil {
		close(done) // Signal immediate completion on error
		restoreFunc()
		return nil, fmt.Errorf("failed to open /dev/null: %w", devErr)
	}
	defer func() {
		if err := devNull.Close(); err != nil {
			log.Debug().Err(err).Msg("failed to close /dev/null")
		}
	}()
	cmd.Stdin = devNull
	cmd.Stdout = devNull

	// Capture stderr for logging
	stderrPipe, pipeErr := cmd.StderrPipe()
	if pipeErr != nil {
		close(done) // Signal immediate completion on error
		restoreFunc()
		return nil, fmt.Errorf("failed to create stderr pipe: %w", pipeErr)
	}

	// Log stderr output in background
	go func() {
		scanner := bufio.NewScanner(stderrPipe)
		for scanner.Scan() {
			log.Debug().Str("source", logPrefix).Msg(scanner.Text())
		}
	}()

	log.Debug().
		Strs("args", cmd.Args).
		Msgf("%s: starting console launcher", logPrefix)

	// Start process non-blocking
	if err := cmd.Start(); err != nil {
		close(done) // Signal immediate completion on error
		restoreFunc()
		return nil, fmt.Errorf("failed to start %s: %w", logPrefix, err)
	}

	// Track process and completion channel together BEFORE cleanup goroutine starts
	pl.setTrackedProcessWithCleanup(cmd.Process, done)

	// Cleanup in goroutine (non-blocking)
	go func() {
		// Signal completion when this goroutine exits, no matter what happens
		defer close(done)

		waitErr := cmd.Wait()
		log.Debug().Msgf("%s: process exited, waitErr=%v", logPrefix, waitErr)

		// Handle different exit scenarios
		if waitErr != nil {
			// Process exited with error (crash, SIGTERM, or SIGKILL)
			log.Info().Err(waitErr).Msgf("%s exited with error", logPrefix)
		} else {
			// Process completed normally (exit code 0)
			log.Info().Msgf("%s completed normally", logPrefix)
		}

		// CRITICAL: Always run cleanup for console launchers
		// Console state (VT, cursor, video mode) must be restored regardless
		// of whether the context was cancelled or the process was preempted
		restoreFunc()

		// Clear tracked process after cleanup completes
		pl.clearTrackedProcess()
	}()

	return cmd.Process, nil
}
