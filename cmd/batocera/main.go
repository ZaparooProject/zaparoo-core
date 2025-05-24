/*
Zaparoo Core
Copyright (C) 2023 Gareth Jones
Copyright (C) 2023-2025 Callan Barrett

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
	"flag"
	"fmt"
	"github.com/ZaparooProject/zaparoo-core/pkg/cli"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms/batocera"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms/mister"
	"github.com/ZaparooProject/zaparoo-core/pkg/service"
	"github.com/ZaparooProject/zaparoo-core/pkg/ui"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	"github.com/rs/zerolog/log"
	"os"
	"path"

	_ "embed"
)

// api: https://github.com/batocera-linux/batocera-emulationstation/blob/master/es-app/src/services/HttpServerThread.cpp
//      api access works locally with no changes

//go:embed scripts/services/zaparoo_service
var serviceFile string

const serviceFilePath = "/userdata/system/services/zaparoo_service"

func main() {
	pl := &batocera.Platform{}
	flags := cli.SetupFlags()

	serviceFlag := flag.String(
		"service",
		"",
		"manage Zaparoo service (start|stop|restart|status)",
	)
	doInstall := flag.Bool(
		"install",
		false,
		"install Zaparoo service",
	)
	doUninstall := flag.Bool(
		"uninstall",
		false,
		"uninstall Zaparoo service",
	)

	flags.Pre(pl)

	if *doInstall {
		err := os.MkdirAll(path.Dir(serviceFilePath), 0755)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Error creating service directory: %v\n", err)
			os.Exit(1)
		}
		err = os.WriteFile(serviceFilePath, []byte(serviceFile), 0755)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Error writing service file: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Zaparoo service installed successfully.")
		os.Exit(0)
	} else if *doUninstall {
		if _, err := os.Stat(serviceFilePath); err == nil {
			err := os.Remove(serviceFilePath)
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "Error removing service file: %v\n", err)
				os.Exit(1)
			}
			fmt.Println("Zaparoo service uninstalled successfully.")
			os.Exit(0)
		}
	}

	cfg := cli.Setup(
		pl,
		config.BaseDefaults,
		nil,
	)

	defer func() {
		if err := recover(); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Panic: %v\n", err)
			log.Fatal().Msgf("panic: %v", err)
		}
	}()

	svc, err := utils.NewService(utils.ServiceArgs{
		Entry: func() (func() error, error) {
			return service.Start(pl, cfg)
		},
		Platform: pl,
	})
	if err != nil {
		log.Error().Err(err).Msg("error creating service")
		_, _ = fmt.Fprintf(os.Stderr, "Error creating service: %v\n", err)
		os.Exit(1)
	}
	svc.ServiceHandler(serviceFlag)

	flags.Post(cfg, pl)

	// try to auto-start service if it's not running already
	if !svc.Running() {
		err := svc.Start()
		if err != nil {
			log.Error().Err(err).Msg("could not start service")
		}
	}

	// start the tui
	logDestinationPath := path.Join(mister.DataDir, config.LogFile)
	app, err := ui.BuildTheUi(pl, svc.Running(), cfg, logDestinationPath)
	if err != nil {
		log.Error().Msgf("error setting up UI: %s", err)
		_, _ = fmt.Fprintf(os.Stderr, "Error setting up UI: %v\n", err)
		os.Exit(1)
	}

	err = app.Run()
	if err != nil {
		log.Error().Msgf("error running UI: %s", err)
		_, _ = fmt.Fprintf(os.Stderr, "Error running UI: %v\n", err)
		os.Exit(1)
	}

	os.Exit(0)
}
