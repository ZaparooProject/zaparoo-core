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
