//go:build !windows

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

package command

import (
	"context"
	"os/exec"
)

// StartWithOptions starts a command with platform-specific options on Unix.
// HideWindow option is ignored on non-Windows platforms.
//
//nolint:wrapcheck // Wrapping exec errors loses important context
func (*RealExecutor) StartWithOptions(
	ctx context.Context,
	_ StartOptions,
	name string,
	args ...string,
) error {
	return exec.CommandContext(ctx, name, args...).Start()
}
