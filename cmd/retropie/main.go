//go:build linux && !android

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
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"syscall"

	"github.com/ZaparooProject/zaparoo-core/v2/internal/telemetry"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/cli"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/retropie"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/daemon"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/restart"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/updater"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/tui"
	"github.com/rivo/tview"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
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

	pl := &retropie.Platform{}
	flags := cli.SetupFlags()

	asDaemon := flag.Bool("daemon", false, "run zaparoo in daemon mode")
	doInstall := flag.Bool("install", false, "install the zaparoo service and RetroPie hooks, as the RetroPie user")
	doUninstall := flag.Bool("uninstall", false, "uninstall the zaparoo service and RetroPie hooks")
	installSystem := flag.String(retropie.InstallSystemFlag, "", "")
	uninstallSystem := flag.Bool(retropie.UninstallSystemFlag, false, "")

	flags.Pre(pl)

	ctx := context.Background()

	// The root half of install and uninstall, run by -install and
	// -uninstall through sudo.
	if *installSystem != "" || *uninstallSystem {
		exe, err := findExecutable()
		if err != nil {
			return err
		}
		if *uninstallSystem {
			if err := uninstallSystemSide(ctx, runCommand); err != nil {
				return fmt.Errorf("uninstall service: %w", err)
			}
			return nil
		}
		if err := installSystemSide(ctx, *installSystem, exe, runCommand); err != nil {
			return fmt.Errorf("install service: %w", err)
		}
		return nil
	}

	if geteuid() == 0 {
		return errors.New("zaparoo must not be run as root")
	}

	if *doInstall || *doUninstall {
		exe, err := findExecutable()
		if err != nil {
			return err
		}
		if *doUninstall {
			if err := userUninstall(pl, ctx, exe, runCommand); err != nil {
				return fmt.Errorf("uninstall: %w", err)
			}
			_, _ = fmt.Println("Zaparoo service and RetroPie hooks removed.")
			return nil
		}
		if err := userInstall(pl, ctx, exe, runCommand); err != nil {
			return fmt.Errorf("install: %w", err)
		}
		_, _ = fmt.Println("Zaparoo installed. Reboot the console so EmulationStation " +
			"starts with Zaparoo launch support.")
		return nil
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

// Tests replace these; the defaults act on the real system.
var (
	geteuid             = os.Geteuid
	findExecutable      = executablePath
	runCommand          = runInteractive
	userInstall         = (*retropie.Platform).Install
	userUninstall       = (*retropie.Platform).Uninstall
	installSystemSide   = retropie.InstallSystem
	uninstallSystemSide = retropie.UninstallSystem
)

func executablePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("find zaparoo binary: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("resolve zaparoo binary: %w", err)
	}
	return exe, nil
}

func runInteractive(ctx context.Context, name string, args ...string) error {
	//nolint:gosec // G204: only sudo and systemctl with arguments built by the installer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

func runDaemon(pl *retropie.Platform, cfg *config.Instance) error {
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
func runTUI(pl *retropie.Platform, cfg *config.Instance) error {
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
			logDestPath, "storage",
		)
	})
	if err != nil {
		return fmt.Errorf("error displaying TUI: %w", err)
	}

	return nil
}
