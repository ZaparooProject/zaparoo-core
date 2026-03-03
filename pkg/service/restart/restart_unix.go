//go:build !windows

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
	"syscall"

	"github.com/rs/zerolog/log"
)

// Exec replaces the current process with the updated binary. On Unix this
// uses syscall.Exec which atomically replaces the process (same PID). It
// does not return on success.
func Exec() error {
	binPath, err := BinaryPath()
	if err != nil {
		return err
	}

	log.Info().
		Str("binary", binPath).
		Strs("args", os.Args).
		Msg("re-executing binary for update restart")

	//nolint:gosec // Safe: binPath is from os.Executable() or ZAPAROO_APP env var
	err = syscall.Exec(binPath, os.Args, os.Environ())
	return fmt.Errorf("exec failed: %w", err)
}
