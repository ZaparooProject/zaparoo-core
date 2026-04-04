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

package systray

import (
	"context"
	"os/exec"
	"runtime"
)

func openPath(path string) error {
	openCmd := "xdg-open"
	if runtime.GOOS == "darwin" {
		openCmd = "open"
	}
	//nolint:gosec // Safe: opens file/URL with system default handler
	return exec.CommandContext(context.Background(), openCmd, path).Start() //nolint:wrapcheck // internal helper
}
