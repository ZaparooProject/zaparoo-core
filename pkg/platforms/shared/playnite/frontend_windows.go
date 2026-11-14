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

package playnite

import (
	"context"
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/command"
)

// execFrontend runs Playnite's own executable. It is started directly rather
// than through cmd's start: the install path comes from the user's profile
// folder, and cmd would split it at an ampersand or expand a percent sign.
type execFrontend struct {
	exec command.Executor
}

// NewFrontend returns the real Playnite frontend driver.
func NewFrontend(exec command.Executor) Frontend {
	return &execFrontend{exec: exec}
}

// StartGame passes a validated game ID to Playnite. No window is hidden:
// Playnite is a windowed program with no console to suppress, and a hidden
// start would carry over to its main window.
func (f *execFrontend) StartGame(ctx context.Context, inst *Install, gameID string) error {
	id, err := NormalizeGameID(gameID)
	if err != nil {
		return err
	}
	err = f.exec.StartWithOptions(ctx, command.StartOptions{}, inst.DesktopExe, "--start", id)
	if err != nil {
		return fmt.Errorf("start Playnite: %w", err)
	}
	return nil
}
