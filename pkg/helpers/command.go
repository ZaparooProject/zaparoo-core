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

package helpers

import (
	"context"
	"os/exec"
)

// CommandExecutor provides an abstraction over exec.Command for testability.
// This allows commands to be mocked in tests without executing real system commands.
type CommandExecutor interface {
	// Run executes a command and waits for it to complete.
	// Returns an error if the command fails to start or exits with non-zero status.
	Run(ctx context.Context, name string, args ...string) error

	// Start starts a command without waiting for it to complete (fire-and-forget).
	// Returns an error if the command fails to start.
	Start(ctx context.Context, name string, args ...string) error
}

// RealCommandExecutor uses actual exec.Command to execute system commands.
// This is the production implementation used in normal operation.
type RealCommandExecutor struct{}

// Run executes a system command using exec.CommandContext.
//
//nolint:wrapcheck // Wrapping exec errors loses important context
func (*RealCommandExecutor) Run(ctx context.Context, name string, args ...string) error {
	return exec.CommandContext(ctx, name, args...).Run()
}

// Start starts a command without waiting for it to complete.
//
//nolint:wrapcheck // Wrapping exec errors loses important context
func (*RealCommandExecutor) Start(ctx context.Context, name string, args ...string) error {
	return exec.CommandContext(ctx, name, args...).Start()
}
