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
	"fmt"
	"os"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/rs/zerolog/log"
)

// ExecIfRequested checks whether a restart was requested and, if so, re-execs
// the binary. Returns nil if no restart was requested. On success, the process
// is replaced (Unix) or a new process is spawned and the old one exits
// (Windows), so this function does not return on success.
func ExecIfRequested(restartRequested func() bool) error {
	if restartRequested == nil || !restartRequested() {
		return nil
	}
	log.Info().Msg("restart requested, re-executing binary")
	return Exec()
}

// WaitForShutdown waits for whatever ends a run and calls quit, so a UI that
// owns the main thread lets go of it. The returned channel yields whether the
// process should re-exec into the binary that replaced it.
//
// This has to run alongside the UI rather than after it. A tray or a terminal UI
// blocks its caller until the user closes it, and an update stops the service
// from underneath that loop. Waiting for the service afterwards means an
// installed update never restarts: the service shuts down, the UI stays up in
// front of nothing, and the new binary only runs if someone quits or reboots.
//
// A nil done or sigs channel simply never fires, which is what a mode with no
// service of its own wants.
func WaitForShutdown(
	sigs <-chan os.Signal,
	exit <-chan bool,
	done <-chan struct{},
	restartRequested func() bool,
	quit func(),
) <-chan bool {
	restarting := make(chan bool, 1)
	go func() {
		reExec := false
		select {
		case <-sigs:
		case <-exit:
		case <-done:
			log.Info().Msg("service shut down internally")
			reExec = restartRequested != nil && restartRequested()
		}
		restarting <- reExec
		// Always quit, including when the UI is what ended the run: quitting an
		// already-stopped UI does nothing, while skipping it would leave the
		// loop running whenever the service is the thing that stopped.
		if quit != nil {
			quit()
		}
	}()
	return restarting
}

// ExecAfterRollback re-execs the restored binary. A successful Exec never
// returns, so every return preserves the rollback error for diagnosis.
func ExecAfterRollback(rollbackErr error) error {
	return execAfterRollbackWith(rollbackErr, Exec)
}

func execAfterRollbackWith(rollbackErr error, execFn func() error) error {
	execErr := execFn()
	if execErr == nil {
		return fmt.Errorf("failed to re-exec after rolling back an update: restart returned unexpectedly: %w",
			rollbackErr)
	}
	return fmt.Errorf("failed to re-exec after rolling back an update: %w",
		errors.Join(rollbackErr, execErr))
}

// BinaryPath returns the path to the binary that should be exec'd on restart.
// For daemon subprocesses (ZAPAROO_APP set), this is the original binary path.
// Otherwise it is the current executable path.
func BinaryPath() (string, error) {
	if appPath := os.Getenv(config.AppEnv); appPath != "" {
		return appPath, nil
	}

	exePath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("failed to get executable path: %w", err)
	}
	return exePath, nil
}
