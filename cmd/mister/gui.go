//go:build linux

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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	misterconfig "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	misterstartup "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/startup"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/tui"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
)

func buildTheInstallRequestApp() (*tview.Application, error) {
	var startup misterstartup.Startup

	// Load existing startup file before modifying
	err := startup.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load startup file: %w", err)
	}

	app := tview.NewApplication()

	// create the main modal
	modal := tview.NewModal()
	modal.SetTitle("Autostart Service").
		SetBorder(true).
		SetTitleAlign(tview.AlignCenter)
	modal.SetText("Add Zaparoo service to MiSTer startup?\nThis won't impact MiSTer's performance.").
		AddButtons([]string{"Yes", "No"}).
		SetDoneFunc(func(_ int, buttonLabel string) {
			switch buttonLabel {
			case "Yes":
				err := startup.AddService("mrext/" + config.AppName)
				if err != nil {
					log.Error().Err(err).Msg("failed to add service to startup")
					_, _ = fmt.Fprintf(os.Stderr, "Error adding to startup: %v\n", err)
					app.Stop()
					return
				}
				if len(startup.Entries) > 0 {
					err = startup.Save()
					if err != nil {
						log.Error().Err(err).Msg("failed to save startup file")
						_, _ = fmt.Fprintf(os.Stderr, "Error saving startup: %v\n", err)
						app.Stop()
						return
					}
				}
				app.Stop()
			case "No":
				app.Stop()
			}
		})

	return app.SetRoot(modal, true).EnableMouse(true), nil
}

func tryAddStartup() error {
	var startup misterstartup.Startup

	err := startup.Load()
	if err != nil {
		log.Error().Msgf("failed to load startup file: %s", err)
	}

	// migration from tapto name
	if startup.Exists("mrext/tapto") {
		err = startup.Remove("mrext/tapto")
		if err != nil {
			return fmt.Errorf("failed to remove tapto from startup: %w", err)
		}
	}

	if !startup.Exists("mrext/" + config.AppName) {
		err := tui.BuildAndRetry(nil, buildTheInstallRequestApp)
		if err != nil {
			return fmt.Errorf("failed to show startup prompt: %w", err)
		}
	}

	return nil
}

func displayServiceInfo(pl platforms.Platform, cfg *config.Instance, service *helpers.Service) error {
	// Asturur > Wizzo
	err := tui.BuildAndRetry(cfg, func() (*tview.Application, error) {
		logDestinationPath := path.Join(misterconfig.DataDir, config.LogFile)
		return tui.BuildMain(cfg, pl, service.Running, logDestinationPath, "SD card")
	})
	if err != nil {
		return fmt.Errorf("failed to build and display service info: %w", err)
	}
	return nil
}
