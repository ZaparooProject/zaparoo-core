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

package windows

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// taskKillPath returns the absolute path to taskkill.exe, resolving it without
// relying on %PATH% for the same reasons helpers.ComSpec does: a stripped
// %PATH% makes a bare-name lookup fail, and an absolute path avoids Go's
// exec.ErrDot protection.
func taskKillPath() string {
	if sr := os.Getenv("SystemRoot"); sr != "" {
		return filepath.Join(sr, "System32", "taskkill.exe")
	}
	return `C:\Windows\System32\taskkill.exe`
}

// taskKillTree ends a process and everything it spawned.
//
// Without force, taskkill posts WM_CLOSE to the tree's windows, which lets a
// game save and shut down on its own terms. Console and windowless processes
// ignore that, so callers escalate to a forced kill afterwards.
func taskKillTree(ctx context.Context, pid uint32, force bool) error {
	args := []string{"/PID", strconv.FormatUint(uint64(pid), 10), "/T"}
	if force {
		args = append(args, "/F")
	}

	//nolint:gosec // Fixed binary path; PID comes from local process enumeration.
	cmd := exec.CommandContext(ctx, taskKillPath(), args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("taskkill pid %d: %w: %s", pid, err, strings.TrimSpace(string(output)))
	}
	return nil
}

// runTaskKillPIDTree forcibly ends a process tree.
func runTaskKillPIDTree(ctx context.Context, pid uint32) error {
	return taskKillTree(ctx, pid, true)
}

// runTaskKillPIDTreeGraceful asks a process tree to close.
func runTaskKillPIDTreeGraceful(ctx context.Context, pid uint32) error {
	return taskKillTree(ctx, pid, false)
}
