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
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
)

func BuildExportLogModal(
	pl platforms.Platform,
	app *tview.Application,
	pages *tview.Pages,
	logDestPath string,
	logDestName string,
) tview.Primitive {
	exportPages := tview.NewPages()
	exportPages.SetTitle("Export Log File")

	exportMenu := tview.NewList()
	exportPages.AddAndSwitchToPage("export", exportMenu, true)

	exportMenu.AddItem(
		"Upload to termbin.com",
		"Upload log file to termbin.com and display URL",
		'1', func() {
			outcome := uploadLog(pl, exportPages, app)
			resultModal := genericModal(outcome, "Upload Log File",
				func(_ int, _ string) {
					exportPages.RemovePage("upload")
				}, true)
			exportPages.AddPage("upload", resultModal, true, true)
			app.SetFocus(resultModal)
		})
	if logDestPath != "" {
		exportMenu.AddItem(
			"Copy to "+logDestName,
			"Copy log file to a permanent location on disk",
			'2',
			func() {
				outcome := copyLogToSd(pl, logDestPath, logDestName)
				resultModal := genericModal(outcome, "Copy Log File",
					func(_ int, _ string) {
						exportPages.RemovePage("copy")
					}, true)
				exportPages.AddPage("copy", resultModal, true, true)
				app.SetFocus(resultModal)
			})
	}
	exportMenu.AddItem("Go back", "Back to main menu", 'b', func() {
		pages.SwitchToPage(PageMain)
	})
	exportMenu.SetSecondaryTextColor(tcell.ColorYellow)

	exportPages.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			pages.SwitchToPage(PageMain)
			return nil
		}
		return event
	})

	pageDefaults(PageExportLog, pages, exportPages)
	return exportMenu
}

func copyLogToSd(pl platforms.Platform, logDestPath, logDestName string) string {
	logPath := path.Join(pl.Settings().LogDir, config.LogFile)
	newPath := logDestPath
	err := helpers.CopyFile(logPath, newPath)
	outcome := ""
	if err != nil {
		outcome = fmt.Sprintf("Unable to copy log file to %s.", logDestName)
		log.Error().Err(err).Msgf("error copying log file")
	} else {
		outcome = fmt.Sprintf("Copied %s to %s.", config.LogFile, logDestName)
	}
	return outcome
}

func uploadLog(pl platforms.Platform, pages *tview.Pages, app *tview.Application) string {
	logPath := path.Join(pl.Settings().LogDir, config.LogFile)
	modal := genericModal("Uploading log file...", "Log upload", func(_ int, _ string) {}, false)
	pages.AddPage("temp_upload", modal, true, true)
	app.SetFocus(modal)
	app.ForceDraw()

	// Create a pipe to safely pass file content to nc without shell injection
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Read the log file
	//nolint:gosec // Safe: logPath is from internal platform settings, not user input
	logContent, err := os.ReadFile(logPath)
	if err != nil {
		log.Error().Err(err).Msg("failed to read log file")
		return "Error reading log file."
	}

	// Execute nc command with stdin pipe
	cmd := exec.CommandContext(ctx, "nc", "termbin.com", "9999")
	cmd.Stdin = bytes.NewReader(logContent)
	out, err := cmd.Output()
	pages.RemovePage("temp_upload")
	if err != nil {
		log.Error().Err(err).Msgf("error uploading log file to termbin")
		return "Error uploading log file."
	}
	return "Log file URL:\n\n" + strings.TrimSpace(string(out))
}
