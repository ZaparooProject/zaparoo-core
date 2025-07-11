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
	"github.com/ZaparooProject/zaparoo-core/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/pkg/cli"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms/libreelec"
	"github.com/ZaparooProject/zaparoo-core/pkg/service"
	"github.com/ZaparooProject/zaparoo-core/pkg/ui/tui"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
	"os"
	"path"
)

func main() {
	flags := cli.SetupFlags()
	serviceFlag := flag.String(
		"service",
		"",
		"manage Zaparoo service (start|stop|restart|status)",
	)

	pl := &libreelec.Platform{}
	flags.Pre(pl)

	cfg := cli.Setup(pl, config.BaseDefaults, nil)

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

	// display main info gui
	enableZapScript := client.DisableZapScript(cfg)
	err = tui.BuildAndRetry(func() (*tview.Application, error) {
		logDestinationPath := path.Join("/storage", config.LogFile)
		return tui.BuildMain(
			cfg, pl, svc.Running,
			logDestinationPath, "storage",
		)
	})
	if err != nil {
		enableZapScript()
		_, _ = fmt.Fprintf(os.Stderr, "Error displaying TUI: %v\n", err)
		os.Exit(1)
	}
	enableZapScript()
}
