package simplegui

/*
Zaparoo Core
Copyright (C) 2023, 2024 Callan Barrett

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

import (
	"os/exec"
	"path"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/configui"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms/mister"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
)

func copyLogToSd(pl platforms.Platform) string {
	logPath := path.Join(pl.LogDir(), config.LogFile)
	newPath := path.Join(mister.DataDir, config.LogFile)
	err := utils.CopyFile(logPath, newPath)
	outcome := ""
	if err != nil {
		outcome = "Unable to copy log file to SD card."
		log.Error().Err(err).Msgf("error copying log file")
	} else {
		outcome = "Copied " + config.LogFile + " to SD card."
	}
	return outcome
}

func uploadLog(pl platforms.Platform, pages *tview.Pages, app *tview.Application) string {

	logPath := path.Join(pl.LogDir(), config.LogFile)
	modal := genericModal("Uploading log file...", "Log upload", func(buttonIndex int, buttonLabel string) {}, false)
	pages.RemovePage("export")
	// fixme: this is not updating, too busy
	pages.AddPage("temp_upload", modal, true, true)
	app.ForceDraw()
	uploadCmd := "cat '" + logPath + "' | nc termbin.com 9999"
	out, err := exec.Command("bash", "-c", uploadCmd).Output()
	pages.RemovePage("temp_upload")
	if err != nil {
		log.Error().Err(err).Msgf("error uploading log file to termbin")
		return "Unable to upload log file."
	} else {
		return "Log file URL:\n" + strings.TrimSpace(string(out))
	}
}

func modalBuilder(content tview.Primitive, width int, height int) tview.Primitive {

	itemHeight := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).
		AddItem(content, height, 1, true).
		AddItem(nil, 0, 1, false)

	return tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(itemHeight, width, 1, true).
		AddItem(nil, 0, 1, false)
}

func genericModal(message string, title string, action func(buttonIndex int, buttonLabel string), withButton bool) *tview.Modal {
	modal := tview.NewModal()
	modal.SetTitle(title).
		SetBorder(true).
		SetTitleAlign(tview.AlignCenter)
	modal.SetText(message)
	if withButton {
		modal.AddButtons([]string{"OK"}).
			SetDoneFunc(action)
	}

	return modal
}

func buildTheUi(pl platforms.Platform, service *utils.Service, cfg *config.Instance) (*tview.Application, error) {
	app := tview.NewApplication()
	modal := tview.NewModal()
	logExport := tview.NewList()

	var statusText string
	running := service.Running()
	if running {
		statusText = "RUNNING"
	} else {
		statusText = "NOT RUNNING"
	}

	ip, err := utils.GetLocalIp()
	var ipDisplay string
	if err != nil {
		ipDisplay = "Unknown"
	} else {
		ipDisplay = ip.String()
	}

	// ugly text for the modal content. sorry.
	text := ""
	text = text + "  Visit zaparoo.org for guides and help!  \n"
	text = text + "──────────────────────────────────────────\n"
	text = text + "  Service:        " + statusText + "\n"
	text = text + "  Device address: " + ipDisplay + "\n"
	text = text + "──────────────────────────────────────────\n"

	pages := tview.NewPages().
		AddPage("main", modal, true, true)

	// create the small log export modal
	logExport.
		AddItem("Upload to termbin.com", "", 'a', func() {
			pages.RemovePage("export")
			outcome := uploadLog(pl, pages, app)
			modal := genericModal(outcome, "Log upload", func(buttonIndex int, buttonLabel string) {
				pages.RemovePage("upload")
			}, true)
			pages.AddPage("upload", modal, true, true)
		}).
		AddItem("Copy to SD card", "", 'b', func() {
			pages.RemovePage("export")
			outcome := copyLogToSd(pl)
			modal := genericModal(outcome, "Log copy", func(buttonIndex int, buttonLabel string) {
				pages.RemovePage("copy")
			}, true)
			pages.AddPage("copy", modal, true, true)
		}).
		AddItem("Cancel", "", 'q', func() {
			pages.RemovePage("export")
		}).
		ShowSecondaryText(false)
	// Coloring will require some effort
	// SetBackgroundColor(modal.GetBackgroundColor())
	logExport.
		SetBorder(true).
		SetBorderPadding(1, 1, 1, 1).
		SetTitle("Log export")

	// create the main modal
	modal.SetTitle("Zaparoo Core v" + config.AppVersion + " (" + pl.Id() + ")").
		SetBorder(true).
		SetTitleAlign(tview.AlignCenter)
	modal.SetText(text).
		AddButtons([]string{"Config", "Export log", "Exit"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			if buttonLabel == "Exit" {
				app.Stop()
			}
			if buttonLabel == "Config" {
				configui.ConfigUiBuilder(cfg, app, pages, func() {
					pages.SwitchToPage("main")
				})
			}
			if buttonLabel == "Export log" {
				widget := modalBuilder(logExport, 42, 8)
				pages.AddPage("export", widget, true, true)
			}
		})

	return app.SetRoot(pages, true).EnableMouse(true), nil
}
