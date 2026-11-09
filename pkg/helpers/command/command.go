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

// Package command provides an abstraction over exec.Command for testability.
package command

import (
	"context"
	"os"
	"os/exec"
	"strings"
)

// ShellQuote wraps a value in POSIX single quotes so a shell reads it as one
// literal word. Double quotes would leave $, ` and \ active.
func ShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// StartOptions configures command startup behavior.
type StartOptions struct {
	// Env holds KEY=value entries that override the inherited environment.
	// Nil or empty leaves the child with Core's own environment.
	Env []string
	// HideWindow prevents a console window from appearing (Windows-only).
	// On non-Windows platforms, this field is ignored.
	HideWindow bool
}

// applyEnv appends overrides after the inherited environment. exec.Cmd keeps
// the last value for a duplicated key, so the overrides win.
func applyEnv(cmd *exec.Cmd, env []string) {
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
}

// StartAndReap starts cmd and waits for it in the background. Use it for
// fire-and-forget commands whose caller never calls cmd.Wait: without the
// wait every exited child stays a zombie (and on Windows keeps its process
// handle) until Core exits. The caller must not call cmd.Wait itself.
func StartAndReap(cmd *exec.Cmd) error {
	_, err := startReaped(cmd)
	return err
}

// startReaped is StartAndReap with a channel that closes once cmd.Wait has
// returned, after which cmd.ProcessState is safe to read.
//
//nolint:wrapcheck // Wrapping exec errors loses important context
func startReaped(cmd *exec.Cmd) (<-chan struct{}, error) {
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	reaped := make(chan struct{})
	go func() {
		defer close(reaped)
		_ = cmd.Wait()
	}()
	return reaped, nil
}

// Executor provides an abstraction over exec.Command for testability.
// This allows commands to be mocked in tests without executing real system commands.
type Executor interface {
	// Run executes a command and waits for it to complete.
	// Returns an error if the command fails to start or exits with non-zero status.
	Run(ctx context.Context, name string, args ...string) error

	// Output runs a command and returns its standard output.
	// Returns the output bytes and an error if the command fails.
	Output(ctx context.Context, name string, args ...string) ([]byte, error)

	// Start starts a command without waiting for it to complete (fire-and-forget).
	// Returns an error if the command fails to start.
	Start(ctx context.Context, name string, args ...string) error

	// StartWithOptions starts a command with platform-specific options.
	// Returns an error if the command fails to start.
	StartWithOptions(ctx context.Context, opts StartOptions, name string, args ...string) error
}

// RealExecutor uses actual exec.Command to execute system commands.
// This is the production implementation used in normal operation.
type RealExecutor struct{}

// Run executes a system command using exec.CommandContext.
//
//nolint:wrapcheck,gosec // Wrapping exec errors loses important context; G204: command executor's purpose
func (*RealExecutor) Run(ctx context.Context, name string, args ...string) error {
	return exec.CommandContext(ctx, name, args...).Run()
}

// Output runs a command and returns its standard output.
//
//nolint:wrapcheck,gosec // Wrapping exec errors loses important context; G204: command executor's purpose
func (*RealExecutor) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

// Start starts a command without waiting for it to complete.
//
//nolint:wrapcheck,gosec // Wrapping exec errors loses important context; G204: command executor's purpose
func (*RealExecutor) Start(ctx context.Context, name string, args ...string) error {
	return StartAndReap(exec.CommandContext(ctx, name, args...))
}
