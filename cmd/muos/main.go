//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later
//
// This file is part of Zaparoo Core.
//
// Zaparoo Core is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// Zaparoo Core is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"syscall"

	"github.com/ZaparooProject/zaparoo-core/v2/internal/telemetry"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/cli"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/muos"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/daemon"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/restart"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/updater"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/tui"
	"github.com/rivo/tview"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(cli.ExitCodeFor(err))
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

	pl := &muos.Platform{}
	flags := cli.SetupFlags()

	asDaemon := flag.Bool("daemon", false, "run zaparoo in daemon mode")
	doInstall := flag.Bool("install", false, "install the zaparoo muOS user init script")
	doUninstall := flag.Bool("uninstall", false, "uninstall the zaparoo muOS user init script")

	flags.Pre(pl)

	if *doInstall {
		return install(pl)
	}
	if *doUninstall {
		return uninstall(pl)
	}

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

	if *asDaemon {
		return runDaemon(pl, cfg)
	}

	return runTUI(pl, cfg)
}

func runDaemon(pl *muos.Platform, cfg *config.Instance) error {
	runtime.GOMAXPROCS(2)

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigs)

	svcResult, err := service.Start(pl, cfg)
	if err != nil {
		// The previous version is back on disk, but this process is still the
		// image that failed and nothing here would start the restored one.
		if errors.Is(err, updater.ErrRolledBack) {
			return fmt.Errorf("restarting after update rollback: %w", restart.ExecAfterRollback(err))
		}

		// A taken API port and a database newer than this build were already
		// logged at Warn where they happened.
		if !api.IsAddrInUse(err) && !errors.Is(err, database.ErrSchemaAhead) {
			log.Error().Err(err).Msg("error starting service")
		}
		return fmt.Errorf("error starting service: %w", err)
	}

	select {
	case <-sigs:
		err = svcResult.Stop()
		if err != nil {
			log.Error().Err(err).Msg("error stopping service")
			return fmt.Errorf("error stopping service: %w", err)
		}
	case <-svcResult.Done:
		log.Info().Msg("service shut down internally")
		if err := restart.ExecIfRequested(svcResult.RestartRequested); err != nil {
			return fmt.Errorf("failed to re-exec for restart: %w", err)
		}
	}

	return nil
}

// runTUI connects to an existing daemon or spawns one, then displays the TUI.
func runTUI(pl *muos.Platform, cfg *config.Instance) error {
	stopDaemon, err := daemon.SpawnDaemon(cfg)
	if err != nil {
		return fmt.Errorf("error spawning daemon: %w", err)
	}
	defer stopDaemon()

	err = tui.BuildAndRetry(cfg, pl, func() (*tview.Application, error) {
		logDestPath := filepath.Join(helpers.DataDir(pl), config.LogFile)
		return tui.BuildMain(
			cfg, pl,
			func() bool { return client.IsServiceRunning(cfg) },
			logDestPath, "SD card",
		)
	})
	if err != nil {
		return fmt.Errorf("error displaying TUI: %w", err)
	}

	return nil
}

func executable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("finding zaparoo binary: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("resolving zaparoo binary: %w", err)
	}
	return resolved, nil
}

func install(pl *muos.Platform) error {
	exe, err := executable()
	if err != nil {
		return err
	}
	if stopErr := pl.StopService(exe); stopErr != nil {
		return fmt.Errorf("error stopping running zaparoo service: %w", stopErr)
	}
	path, err := muos.Install(afero.NewOsFs(), exe)
	if err != nil {
		return fmt.Errorf("error installing init script: %w", err)
	}

	//nolint:gosec // G204: path is the init script this binary just wrote
	cmd := exec.CommandContext(context.Background(), "sh", path)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	// The service outlives this command; inheriting its terminal or pipe
	// would kill Core with SIGPIPE once the caller goes away.
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("error opening %s: %w", os.DevNull, err)
	}
	defer func() { _ = devNull.Close() }()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = devNull, devNull, devNull
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("error starting zaparoo service: %w", err)
	}
	_ = cmd.Process.Release()

	_, _ = fmt.Printf("Zaparoo init script installed to %s and the service started.\n", path)
	_, _ = fmt.Println("In Configuration, highlight General and press X for Advanced,")
	_, _ = fmt.Println("then turn on User Init Scripts so it starts at boot.")
	return nil
}

func uninstall(pl *muos.Platform) error {
	exe, err := executable()
	if err != nil {
		return err
	}
	if err := pl.Uninstall(exe); err != nil {
		return fmt.Errorf("error uninstalling init script: %w", err)
	}
	_, _ = fmt.Println("Zaparoo init script removed and the service stopped.")
	return nil
}
