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
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms/linux"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms/linux/installer"
	"github.com/ZaparooProject/zaparoo-core/pkg/service"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"io"
	"os"
	"os/signal"
	"syscall"

	_ "embed"
)

func main() {
	sigs := make(chan os.Signal, 1)
	defer close(sigs)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	pl := &linux.Platform{}
	flags := cli.SetupFlags()

	doInstall := flag.Bool("install", false, "configure system for zaparoo")
	doUninstall := flag.Bool("uninstall", false, "revert zaparoo system configuration")
	asDaemon := flag.Bool("daemon", false, "run zaparoo in daemon mode")

	flags.Pre(pl)

	if *doInstall {
		err := installer.CLIInstall()
		if err != nil {
			os.Exit(1)
		} else {
			os.Exit(0)
		}
	} else if *doUninstall {
		err := installer.CLIUninstall()
		if err != nil {
			os.Exit(1)
		} else {
			os.Exit(0)
		}
	}

	if os.Geteuid() == 0 {
		_, _ = fmt.Fprintf(os.Stderr, "Zaparoo must not be run as root\n")
		os.Exit(1)
	}

	// only difference with daemon mode right now is no log pretty printing
	// TODO: launch simple gui
	// TODO: fork service if it's not running
	logWriters := []io.Writer{zerolog.ConsoleWriter{Out: os.Stderr}}
	if *asDaemon {
		logWriters = []io.Writer{os.Stderr}
	}

	cfg := cli.Setup(
		pl,
		config.BaseDefaults,
		logWriters,
	)

	flags.Post(cfg, pl)

	stop, err := service.Start(pl, cfg)
	if err != nil {
		log.Error().Err(err).Msg("error starting service")
		os.Exit(1)
	}

	<-sigs
	err = stop()
	if err != nil {
		log.Error().Err(err).Msg("error stopping service")
		os.Exit(1)
	}

	os.Exit(0)
}
