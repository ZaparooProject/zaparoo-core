//go:build windows

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

	"github.com/rs/zerolog/log"
)

// Exec spawns the updated binary as a new process and exits the current one.
// Windows does not support syscall.Exec, so we use os.StartProcess instead.
func Exec() error {
	binPath, err := BinaryPath()
	if err != nil {
		return err
	}

	log.Info().
		Str("binary", binPath).
		Strs("args", os.Args).
		Msg("spawning new process for update restart")

	proc, err := os.StartProcess(binPath, os.Args, &os.ProcAttr{
		Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
		Env:   os.Environ(),
	})
	if err != nil {
		return fmt.Errorf("failed to start new process: %w", err)
	}

	if err := proc.Release(); err != nil {
		return fmt.Errorf("failed to release new process: %w", err)
	}

	os.Exit(0)
	return nil // unreachable
}
