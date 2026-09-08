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
package pinup

import (
	"context"
	"fmt"
	"strconv"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/command"
)

// execFrontend starts Popper's programs the way its own scripts do: through
// cmd's start, detached, with the install folder as the working directory.
type execFrontend struct {
	exec command.Executor
}

// NewFrontend returns the real Popper frontend driver.
func NewFrontend(exec command.Executor) Frontend {
	return &execFrontend{exec: exec}
}

func (f *execFrontend) StartMenu(ctx context.Context, inst *Install) error {
	return f.start(ctx, inst.Dir, inst.MenuExe)
}

func (f *execFrontend) StartServer(ctx context.Context, inst *Install, port int) error {
	if inst.ServerExe == "" {
		return ErrWebRemoteMissing
	}
	return f.start(ctx, inst.Dir, inst.ServerExe,
		"-wwwport", strconv.Itoa(port), "-sockport", strconv.Itoa(ServerSocketPort))
}

// start launches exe detached from Core through "cmd /c start". The empty
// first argument is start's window title slot; /D sets the working directory.
func (f *execFrontend) start(ctx context.Context, dir, exe string, args ...string) error {
	cmdArgs := append([]string{"/c", "start", "", "/D", dir, exe}, args...)
	err := f.exec.StartWithOptions(ctx, command.StartOptions{HideWindow: true}, helpers.ComSpec(), cmdArgs...)
	if err != nil {
		return fmt.Errorf("start %s: %w", exe, err)
	}
	return nil
}
