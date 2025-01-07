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
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms/mister"
	"os/exec"
	"path"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	"github.com/rs/zerolog/log"
	"github.com/rthornton128/goncurses"
	"github.com/wizzomafizzo/mrext/pkg/curses"
	mrextMister "github.com/wizzomafizzo/mrext/pkg/mister"
)

func tryAddStartup(stdscr *goncurses.Window) error {
	var startup mrextMister.Startup

	err := startup.Load()
	if err != nil {
		log.Error().Msgf("failed to load startup file: %s", err)
	}

	changed := false

	// migration from tapto name
	if startup.Exists("mrext/tapto") {
		err = startup.Remove("mrext/tapto")
		if err != nil {
			return err
		}
		changed = true
	}

	if !startup.Exists("mrext/" + config.AppName) {
		win, err := curses.NewWindow(stdscr, 6, 43, "", -1)
		if err != nil {
			return err
		}
		defer func(win *goncurses.Window) {
			err := win.Delete()
			if err != nil {
				log.Error().Msgf("failed to delete window: %s", err)
			}
		}(win)

		var ch goncurses.Key
		selected := 0

		for {
			win.MovePrint(1, 3, "Add Zaparoo service to MiSTer startup?")
			win.MovePrint(2, 2, "This won't impact MiSTer's performance.")
			curses.DrawActionButtons(win, []string{"Yes", "No"}, selected, 10)

			win.NoutRefresh()
			err := goncurses.Update()
			if err != nil {
				return err
			}

			ch = win.GetChar()

			if ch == goncurses.KEY_LEFT {
				if selected == 0 {
					selected = 1
				} else {
					selected = 0
				}
			} else if ch == goncurses.KEY_RIGHT {
				if selected == 0 {
					selected = 1
				} else {
					selected = 0
				}
			} else if ch == goncurses.KEY_ENTER || ch == 10 || ch == 13 {
				break
			} else if ch == goncurses.KEY_ESC {
				selected = 1
				break
			}
		}

		if selected == 0 {
			err = startup.AddService("mrext/" + config.AppName)
			if err != nil {
				return err
			}
			changed = true
		}

		if changed && len(startup.Entries) > 0 {
			err = startup.Save()
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func copyLogToSd(pl platforms.Platform, stdscr *goncurses.Window) error {
	width := 46
	win, err := curses.NewWindow(stdscr, 6, width, "", -1)
	if err != nil {
		return err
	}
	defer func(win *goncurses.Window) {
		err := win.Delete()
		if err != nil {
			log.Error().Msgf("failed to delete window: %s", err)
		}
	}(win)

	logPath := path.Join(pl.LogDir(), config.LogFile)
	newPath := path.Join(mister.DataDir, config.LogFile)
	err = utils.CopyFile(logPath, newPath)

	printCenter := func(y int, text string) {
		win.MovePrint(y, (width-len(text))/2, text)
	}

	if err != nil {
		printCenter(1, "Unable to copy log file to SD card.")
		log.Error().Err(err).Msgf("error copying log file")
	} else {
		printCenter(1, "Copied "+config.LogFile+" to SD card.")
	}
	win.NoutRefresh()

	curses.DrawActionButtons(win, []string{"OK"}, 0, 2)
	win.NoutRefresh()

	err = goncurses.Update()
	if err != nil {
		return err
	}

	_ = win.GetChar()
	return nil
}

func uploadLog(pl platforms.Platform, stdscr *goncurses.Window) error {
	width := 46
	win, err := curses.NewWindow(stdscr, 6, width, "", -1)
	if err != nil {
		return err
	}
	defer func(win *goncurses.Window) {
		err := win.Delete()
		if err != nil {
			log.Error().Msgf("failed to delete window: %s", err)
		}
	}(win)

	logPath := path.Join(pl.LogDir(), config.LogFile)

	printCenter := func(y int, text string) {
		win.MovePrint(y, (width-len(text))/2, text)
	}

	clearLine := func(y int) {
		win.MovePrint(y, 2, strings.Repeat(" ", width-3))
	}

	printCenter(1, "Uploading log file...")
	win.NoutRefresh()
	err = goncurses.Update()
	if err != nil {
		return err
	}

	uploadCmd := "cat '" + logPath + "' | nc termbin.com 9999"
	out, err := exec.Command("bash", "-c", uploadCmd).Output()
	clearLine(1)
	clearLine(2)
	if err != nil {
		printCenter(1, "Unable to upload log file.")
		log.Error().Err(err).Msgf("error uploading log file to termbin")
	} else {
		printCenter(1, "Log file URL:")
		printCenter(2, strings.TrimSpace(string(out)))
	}
	// no idea why but if i don't draw this box part of the windows border is
	// cleared by the url being displayed
	_ = win.Box(goncurses.ACS_VLINE, goncurses.ACS_HLINE)
	win.NoutRefresh()

	curses.DrawActionButtons(win, []string{"OK"}, 0, 2)
	win.NoutRefresh()

	err = goncurses.Update()
	if err != nil {
		return err
	}

	_ = win.GetChar()
	return nil
}

func exportLog(pl platforms.Platform, stdscr *goncurses.Window) error {
	width := 46
	win, err := curses.NewWindow(stdscr, 6, width, "", -1)
	if err != nil {
		return err
	}
	defer func(win *goncurses.Window) {
		err := win.Delete()
		if err != nil {
			log.Error().Msgf("failed to delete window: %s", err)
		}
	}(win)

	printLeft := func(y int, text string) {
		win.MovePrint(y, 2, text)
	}

	clearLine := func(y int) {
		win.MovePrint(y, 2, strings.Repeat(" ", width-4))
	}

	var ch goncurses.Key
	selectedButton := 1
	selectedMenu := 0
	display := true

	for display {
		menuOnline := "Upload to termbin.com"
		clearLine(1)
		if selectedMenu == 0 {
			printLeft(1, "> "+menuOnline)
		} else {
			printLeft(1, "  "+menuOnline)
		}
		win.NoutRefresh()

		menuSd := "Copy to SD card"
		clearLine(2)
		if selectedMenu == 1 {
			printLeft(2, "> "+menuSd)
		} else {
			printLeft(2, "  "+menuSd)
		}
		win.NoutRefresh()

		curses.DrawActionButtons(win, []string{"Cancel", "OK"}, selectedButton, 2)
		win.NoutRefresh()

		err := goncurses.Update()
		if err != nil {
			return err
		}

		ch = win.GetChar()

		switch ch {
		case goncurses.KEY_LEFT:
			if selectedButton == 0 {
				selectedButton = 1
			} else {
				selectedButton = 0
			}
		case goncurses.KEY_RIGHT:
			if selectedButton == 1 {
				selectedButton = 0
			} else {
				selectedButton = 1
			}
		case goncurses.KEY_UP:
			if selectedMenu == 0 {
				selectedMenu = 1
			} else {
				selectedMenu = 0
			}
		case goncurses.KEY_DOWN:
			if selectedMenu == 0 {
				selectedMenu = 1
			} else {
				selectedMenu = 0
			}
		case goncurses.KEY_ESC:
			return nil
		case goncurses.KEY_ENTER, 10, 13:
			if selectedButton == 0 {
				return nil
			} else {
				display = false
			}
		}
	}

	if selectedButton == 1 {
		if selectedMenu == 0 {
			return uploadLog(pl, stdscr)
		} else {
			return copyLogToSd(pl, stdscr)
		}
	}

	return nil
}

func displayServiceInfo(pl platforms.Platform, cfg *config.Instance, stdscr *goncurses.Window, service *utils.Service) error {
	width := 50
	height := 8

	win, err := curses.NewWindow(stdscr, height, width, "", -1)
	if err != nil {
		return err
	}
	defer func(win *goncurses.Window) {
		err := win.Delete()
		if err != nil {
			log.Error().Msgf("failed to delete window: %s", err)
		}
	}(win)

	win.Timeout(300)

	printLeft := func(y int, text string) {
		win.MovePrint(y, 2, text)
	}

	printCenter := func(y int, text string) {
		win.MovePrint(y, (width-len(text))/2, text)
	}

	clearLine := func(y int) {
		win.MovePrint(y, 2, strings.Repeat(" ", width-4))
	}

	var ch goncurses.Key
	selected := 1

	for {
		var statusText string
		running := service.Running()
		if running {
			statusText = "Service:        RUNNING"
		} else {
			statusText = "Service:        NOT RUNNING"
		}

		printCenter(0, "Zaparoo Core v"+config.AppVersion+" ("+pl.Id()+")")

		clearLine(1)
		printCenter(1, "Visit zaparoo.org for guides and help!")

		win.HLine(2, 1, goncurses.ACS_HLINE, width-2)
		win.MoveAddChar(2, 0, goncurses.ACS_LTEE)
		win.MoveAddChar(2, width-1, goncurses.ACS_RTEE)

		clearLine(3)
		printLeft(3, statusText)

		ip, err := utils.GetLocalIp()
		var ipDisplay string
		if err != nil {
			ipDisplay = "Unknown"
		} else {
			ipDisplay = ip.String()
		}

		clearLine(4)
		printLeft(4, "Device address: "+ipDisplay)

		clearLine(height - 2)
		curses.DrawActionButtons(win, []string{"Export Log", "Exit"}, selected, 2)

		win.NoutRefresh()
		err = goncurses.Update()
		if err != nil {
			return err
		}

		ch = win.GetChar()

		if ch == goncurses.KEY_LEFT {
			if selected == 0 {
				selected = 1
			} else {
				selected = 0
			}
		} else if ch == goncurses.KEY_RIGHT {
			if selected == 0 {
				selected = 1
			} else {
				selected = 0
			}
		} else if ch == goncurses.KEY_ENTER || ch == 10 || ch == 13 {
			if selected == 0 {
				err := exportLog(pl, stdscr)
				if err != nil {
					log.Error().Msgf("failed to display export log window: %s", err)
				}
			} else {
				break
			}
		} else if ch == goncurses.KEY_ESC {
			break
		}
	}

	return nil
}
