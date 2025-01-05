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

package main

import (
	"os/exec"
	"path"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms/mister"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
	// mrextMister "github.com/wizzomafizzo/mrext/pkg/mister"
)

// func tryAddStartup(stdscr *goncurses.Window) error {
// 	var startup mrextMister.Startup

// 	err := startup.Load()
// 	if err != nil {
// 		log.Error().Msgf("failed to load startup file: %s", err)
// 	}

// 	// migration from tapto name
// 	if startup.Exists("mrext/tapto") {
// 		err = startup.Remove("mrext/tapto")
// 		if err != nil {
// 			return err
// 		}

// 		err = startup.Save()
// 		if err != nil {
// 			return err
// 		}
// 	}

// 	if !startup.Exists("mrext/" + config.AppName) {
// 		win, err := curses.NewWindow(stdscr, 6, 43, "", -1)
// 		if err != nil {
// 			return err
// 		}
// 		defer func(win *goncurses.Window) {
// 			err := win.Delete()
// 			if err != nil {
// 				log.Error().Msgf("failed to delete window: %s", err)
// 			}
// 		}(win)

// 		var ch goncurses.Key
// 		selected := 0

// 		for {
// 			win.MovePrint(1, 3, "Add Zaparoo service to MiSTer startup?")
// 			win.MovePrint(2, 2, "This won't impact MiSTer's performance.")
// 			curses.DrawActionButtons(win, []string{"Yes", "No"}, selected, 10)

// 			win.NoutRefresh()
// 			err := goncurses.Update()
// 			if err != nil {
// 				return err
// 			}

// 			ch = win.GetChar()

// 			if ch == goncurses.KEY_LEFT {
// 				if selected == 0 {
// 					selected = 1
// 				} else {
// 					selected = 0
// 				}
// 			} else if ch == goncurses.KEY_RIGHT {
// 				if selected == 0 {
// 					selected = 1
// 				} else {
// 					selected = 0
// 				}
// 			} else if ch == goncurses.KEY_ENTER || ch == 10 || ch == 13 {
// 				break
// 			} else if ch == goncurses.KEY_ESC {
// 				selected = 1
// 				break
// 			}
// 		}

// 		if selected == 0 {
// 			err = startup.AddService("mrext/" + config.AppName)
// 			if err != nil {
// 				return err
// 			}

// 			err = startup.Save()
// 			if err != nil {
// 				return err
// 			}
// 		}
// 	}

// 	return nil
// }

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

func uploadLog(pl platforms.Platform, pages *tview.Pages) string {

	logPath := path.Join(pl.LogDir(), config.LogFile)
	modal := genericModal("Uploading log file...", "Log upload", func(buttonIndex int, buttonLabel string) {})
	pages.RemovePage("export")
	// fixme: this is not updating, too busy
	pages.AddPage("temp_upload", modal, true, true)
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

func genericModal(message string, title string, action func(buttonIndex int, buttonLabel string)) *tview.Modal {
	modal := tview.NewModal()
	modal.SetTitle(title).
		SetBorder(true).
		SetTitleAlign(tview.AlignCenter)
	modal.SetText(message).
		AddButtons([]string{"OK"}).
		SetDoneFunc(action)
	return modal
}

func displayServiceInfo(pl platforms.Platform, cfg *config.Instance, service *utils.Service) error {

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
			outcome := uploadLog(pl, pages)
			modal := genericModal(outcome, "Log upload", func(buttonIndex int, buttonLabel string) {
				pages.RemovePage("upload")
			})
			pages.AddPage("upload", modal, true, true)
		}).
		AddItem("Copy to SD card", "", 'b', func() {
			pages.RemovePage("export")
			outcome := copyLogToSd(pl)
			modal := genericModal(outcome, "Log copy", func(buttonIndex int, buttonLabel string) {
				pages.RemovePage("copy")
			})
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
		AddButtons([]string{"Export log", "Exit"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			if buttonLabel == "Exit" {
				app.Stop()
			}
			if buttonLabel == "Export log" {
				widget := modalBuilder(logExport, 42, 8)
				pages.AddPage("export", widget, true, true)
				// exportLog(pl, pages)
			}
		})
	// if pl.Id() == "mister" {
	// 	tty, err := tcell.NewDevTtyFromDev("/dev/tty2")
	// 	if err != nil {
	// 		panic(err)
	// 	}

	// 	screen, err := tcell.NewTerminfoScreenFromTty(tty)
	// 	if err != nil {
	// 		panic(err)
	// 	}

	// 	app.SetScreen(screen)
	// }

	if err := app.SetRoot(pages, true).EnableMouse(true).Run(); err != nil {
		panic(err)
	}

	return err
}
