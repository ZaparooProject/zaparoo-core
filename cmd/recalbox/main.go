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
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"github.com/ZaparooProject/zaparoo-core/v2/internal/telemetry"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/cli"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/recalbox"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// home: /recalbox/share/system
// user: root/recalboxroot (ssh on by default)
// https://wiki.recalbox.com/en/advanced-usage/scripts-on-emulationstation-events
// most of fs is read-only? no +x allowed
// /recalbox/share/userscripts allows writes
// mount -o remount,rw /
// put in /recalbox/scripts

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}

func run() error {
	defer telemetry.Close()
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			_, _ = fmt.Fprintf(os.Stderr, "Panic: %v\n%s\n", r, stack)
			log.Error().
				Interface("panic", r).
				Bytes("stack", stack).
				Msg("recovered from panic")
			telemetry.Flush()
			os.Exit(1)
		}
	}()

	sigs := make(chan os.Signal, 1)
	defer close(sigs)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	pl := &recalbox.Platform{}
	flags := cli.SetupFlags()

	asDaemon := flag.Bool("daemon", false, "run zaparoo in daemon mode")

	flags.Pre(pl)

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
		return fmt.Errorf("error starting service: %w", err)
	}

	<-sigs
	err = stop()
	if err != nil {
		log.Error().Err(err).Msg("error stopping service")
		return fmt.Errorf("error stopping service: %w", err)
	}

	return nil
}
