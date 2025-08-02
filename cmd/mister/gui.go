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
	"fmt"
	"os"
	"path"

	"github.com/ZaparooProject/zaparoo-core/pkg/ui/tui"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms/mister"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
	mrextMister "github.com/wizzomafizzo/mrext/pkg/mister"
)

func buildTheInstallRequestApp() (*tview.Application, error) {
	var startup mrextMister.Startup
	app := tview.NewApplication()
	// create the main modal
	modal := tview.NewModal()
	modal.SetTitle("Autostart Service").
		SetBorder(true).
		SetTitleAlign(tview.AlignCenter)
	modal.SetText("Add Zaparoo service to MiSTer startup?\nThis won't impact MiSTer's performance.").
		AddButtons([]string{"Yes", "No"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			if buttonLabel == "Yes" {
				err := startup.AddService("mrext/" + config.AppName)
				if err != nil {
					_, _ = fmt.Fprintf(os.Stderr, "Error adding to startup: %v\n", err)
					os.Exit(1)
				}
				if len(startup.Entries) > 0 {
					err = startup.Save()
					if err != nil {
						_, _ = fmt.Fprintf(os.Stderr, "Error saving startup: %v\n", err)
						os.Exit(1)
					}
				}
				app.Stop()
			} else if buttonLabel == "No" {
				app.Stop()
			}
		})

	return app.SetRoot(modal, true).EnableMouse(true), nil
}

func tryAddStartup() error {
	var startup mrextMister.Startup

	err := startup.Load()
	if err != nil {
		log.Error().Msgf("failed to load startup file: %s", err)
	}

	// migration from tapto name
	if startup.Exists("mrext/tapto") {
		err = startup.Remove("mrext/tapto")
		if err != nil {
			return err
		}
	}

	if !startup.Exists("mrext/" + config.AppName) {
		err := tui.BuildAndRetry(func() (*tview.Application, error) {
			return buildTheInstallRequestApp()
		})
		if err != nil {
			log.Error().Msgf("failed to build app: %s", err)
		}
	}

	return nil
}

func displayServiceInfo(pl platforms.Platform, cfg *config.Instance, service *utils.Service) error {
	// Asturur > Wizzo
	return tui.BuildAndRetry(func() (*tview.Application, error) {
		logDestinationPath := path.Join(mister.DataDir, config.LogFile)
		return tui.BuildMain(cfg, pl, service.Running, logDestinationPath, "SD card")
	})
}
