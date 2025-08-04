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

package tui

import (
	"fmt"
	"os"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
)

func tryRunApp(
	app *tview.Application,
	builder func() (*tview.Application, error),
) error {
	if err := app.Run(); err != nil {
		appTty2, err := builder()
		if err != nil {
			return err
		}

		ttyPath := "/dev/tty2"
		if os.Getenv("ZAPAROO_RUN_SCRIPT") == "2" {
			log.Debug().Msg("alternate tty for widgets from zapscript")
			ttyPath = "/dev/tty4"
		}

		tty, err := tcell.NewDevTtyFromDev(ttyPath)
		if err != nil {
			return fmt.Errorf("failed to create tty from device %s: %w", ttyPath, err)
		}

		screen, err := tcell.NewTerminfoScreenFromTty(tty)
		if err != nil {
			return fmt.Errorf("failed to create screen from tty: %w", err)
		}

		appTty2.SetScreen(screen)

		if err := appTty2.Run(); err != nil {
			return fmt.Errorf("failed to run TUI application: %w", err)
		}
	}
	return nil
}
