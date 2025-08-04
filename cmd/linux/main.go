//go:build linux

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
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/ZaparooProject/zaparoo-core/pkg/cli"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms/linux"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms/linux/installer"
	"github.com/ZaparooProject/zaparoo-core/pkg/service"
	"github.com/ZaparooProject/zaparoo-core/pkg/ui/tui"
	"github.com/rs/zerolog/log"
)

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}

func run() error {
	pl := &linux.Platform{}
	flags := cli.SetupFlags()

	doInstall := flag.Bool(
		"install",
		false,
		"configure system for Zaparoo",
	)
	doUninstall := flag.Bool(
		"uninstall",
		false,
		"revert Zaparoo system configuration",
	)
	daemonMode := flag.Bool(
		"daemon",
		false,
		"run service in foreground with no UI",
	)

	flags.Pre(pl)

	if *doInstall {
		err := installer.CLIInstall()
		if err != nil {
			return errors.New("installation failed")
		}
		return nil
	} else if *doUninstall {
		err := installer.CLIUninstall()
		if err != nil {
			return errors.New("uninstallation failed")
		}
		return nil
	}

	if os.Geteuid() == 0 {
		return errors.New("zaparoo cannot be run as root")
	}

	var logWriters []io.Writer
	if *daemonMode {
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

	if !helpers.IsServiceRunning(cfg) {
		stopSvc, err := service.Start(pl, cfg)
		if err != nil {
			log.Error().Msgf("error starting service: %s", err)
			return fmt.Errorf("error starting service: %w", err)
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
	} else {
		// default to showing the TUI
		app, err := tui.BuildMain(
			cfg, pl,
			func() bool { return helpers.IsServiceRunning(cfg) },
			filepath.Join(os.Getenv("HOME"), "Desktop", "core.log"),
			"desktop",
		)
		if err != nil {
			log.Error().Err(err).Msgf("error building UI")
			return fmt.Errorf("error building UI: %w", err)
		}

		err = app.Run()
		if err != nil {
			log.Error().Err(err).Msg("error running UI")
			return fmt.Errorf("error running UI: %w", err)
		}

		exit <- true
	}

	select {
	case <-sigs:
	case <-exit:
	}

	return nil
}
