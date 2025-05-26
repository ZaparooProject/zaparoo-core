/*
Zaparoo Core
Copyright (C) 2023 Gareth Jones
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
	"flag"
	"fmt"
	"github.com/ZaparooProject/zaparoo-core/pkg/cli"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms/mac"
	"github.com/ZaparooProject/zaparoo-core/pkg/service"
	"github.com/ZaparooProject/zaparoo-core/pkg/ui"
	"github.com/ZaparooProject/zaparoo-core/pkg/ui/systray"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	"github.com/rs/zerolog/log"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	_ "embed"
)

//go:embed app/systrayicon.png
var systrayIcon []byte

func main() {
	if os.Geteuid() == 0 {
		_, _ = fmt.Fprintf(os.Stderr, "Zaparoo cannot be run as root\n")
		os.Exit(1)
	}

	pl := &mac.Platform{}
	flags := cli.SetupFlags()

	daemonMode := flag.Bool(
		"daemon",
		false,
		"run service in foreground with no UI",
	)
	guiMode := flag.Bool(
		"gui",
		false,
		"run service as daemon with GUI",
	)

	flags.Pre(pl)

	var logWriters []io.Writer
	if *daemonMode || *guiMode {
		logWriters = []io.Writer{os.Stderr}
	}

	cfg := cli.Setup(
		pl,
		config.BaseDefaults,
		logWriters,
	)

	defer func() {
		if err := recover(); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Panic: %s\n", err)
			log.Fatal().Msgf("panic: %v", err)
		}
	}()

	flags.Post(cfg, pl)

	if !utils.IsServiceRunning(cfg) {
		stopSvc, err := service.Start(pl, cfg)
		if err != nil {
			log.Error().Msgf("error starting service: %s", err)
			_, _ = fmt.Fprintf(os.Stderr, "Error starting service: %s\n", err)
			os.Exit(1)
		}

		defer func() {
			err := stopSvc()
			if err != nil {
				log.Error().Msgf("error stopping service: %s", err)
			}
		}()
	}

	sigs := make(chan os.Signal, 1)
	defer close(sigs)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	exit := make(chan bool, 1)
	defer close(exit)

	if *daemonMode {
		log.Info().Msg("started in daemon mode")
	} else if *guiMode {
		systray.Run(cfg, pl, systrayIcon, func(string) {}, func() {
			exit <- true
		})
	} else {
		// default to showing the TUI
		app, err := ui.BuildTheUi(
			pl, utils.IsServiceRunning(cfg), cfg,
			filepath.Join(os.Getenv("HOME"), "Desktop", "core.log"),
		)
		if err != nil {
			log.Error().Err(err).Msgf("error building UI")
			_, _ = fmt.Fprintf(os.Stderr, "Error building UI: %s\n", err)
			os.Exit(1)
		}

		err = app.Run()
		if err != nil {
			log.Error().Err(err).Msg("error running UI")
			_, _ = fmt.Fprintf(os.Stderr, "Error running UI: %s\n", err)
			os.Exit(1)
		}

		exit <- true
	}

	select {
	case <-sigs:
	case <-exit:
	}

	os.Exit(0)
}
