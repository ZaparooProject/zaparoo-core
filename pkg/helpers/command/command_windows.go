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

package command

import (
	"context"
	"os/exec"
	"syscall"
)

// StartWithOptions starts a command with platform-specific options on Windows.
// Supports HideWindow to prevent console window flash.
//
//nolint:wrapcheck // Wrapping exec errors loses important context
func (*RealExecutor) StartWithOptions(
	ctx context.Context,
	opts StartOptions,
	name string,
	args ...string,
) error {
	cmd := exec.CommandContext(ctx, name, args...)
	if opts.HideWindow {
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	}
	return cmd.Start()
}
