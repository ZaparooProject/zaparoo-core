//go:build linux

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

package cli

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/daemon"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/restart"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/updater"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/tui"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
)

// applianceDeps are the process and terminal boundaries the appliance
// runners cross. Tests replace them.
type applianceDeps struct {
	startService      func(platforms.Platform, *config.Instance) (*service.StartResult, error)
	execAfterRollback func(error) error
	execIfRequested   func(func() bool) error
	spawnDaemon       func(*config.Instance) (func(), error)
	showTUI           func(*config.Instance, platforms.Platform, func() (*tview.Application, error)) error
	buildTUI          func(
		cfg *config.Instance, pl platforms.Platform, isRunning func() bool, logDestPath, logDestName string,
	) (*tview.Application, error)
	isServiceRunning func(*config.Instance) bool
}

func defaultApplianceDeps() applianceDeps {
	return applianceDeps{
		startService:      service.Start,
		execAfterRollback: restart.ExecAfterRollback,
		execIfRequested:   restart.ExecIfRequested,
		spawnDaemon:       daemon.SpawnDaemon,
		showTUI:           tui.BuildAndRetry,
		buildTUI:          tui.BuildMain,
		isServiceRunning:  client.IsServiceRunning,
	}
}

// applianceLogPath is where the TUI offers to copy the log on appliance
// platforms, which keep user data on a storage partition.
func applianceLogPath(pl platforms.Platform) (logPath, label string) {
	return filepath.Join(helpers.DataDir(pl), config.LogFile), "storage"
}

// RunApplianceDaemon runs the service in the foreground for an init system
// such as systemd, until a signal arrives or the service shuts itself down.
func RunApplianceDaemon(pl platforms.Platform, cfg *config.Instance) error {
	runtime.GOMAXPROCS(2)

	sigs, stop := notifyShutdown()
	defer stop()

	return runApplianceDaemonWith(defaultApplianceDeps(), pl, cfg, sigs)
}

// notifyShutdown delivers SIGINT and SIGTERM on the returned channel until
// stop is called.
func notifyShutdown() (sigs <-chan os.Signal, stop func()) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	return ch, func() { signal.Stop(ch) }
}

func runApplianceDaemonWith(
	deps applianceDeps,
	pl platforms.Platform,
	cfg *config.Instance,
	sigs <-chan os.Signal,
) error {
	svcResult, err := deps.startService(pl, cfg)
	if err != nil {
		// The previous version is back on disk, but this process is still the
		// image that failed and nothing here would start the restored one.
		if errors.Is(err, updater.ErrRolledBack) {
			return fmt.Errorf("restarting after update rollback: %w", deps.execAfterRollback(err))
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
		if err := svcResult.Stop(); err != nil {
			log.Error().Err(err).Msg("error stopping service")
			return fmt.Errorf("error stopping service: %w", err)
		}
	case <-svcResult.Done:
		log.Info().Msg("service shut down internally")
		if err := deps.execIfRequested(svcResult.RestartRequested); err != nil {
			return fmt.Errorf("failed to re-exec for restart: %w", err)
		}
	}

	return nil
}

// RunApplianceTUI connects to the running daemon, or spawns one for the
// session, and shows the TUI.
func RunApplianceTUI(pl platforms.Platform, cfg *config.Instance) error {
	return runApplianceTUIWith(defaultApplianceDeps(), pl, cfg)
}

func runApplianceTUIWith(deps applianceDeps, pl platforms.Platform, cfg *config.Instance) error {
	stopDaemon, err := deps.spawnDaemon(cfg)
	if err != nil {
		return fmt.Errorf("error spawning daemon: %w", err)
	}
	defer stopDaemon()

	logPath, label := applianceLogPath(pl)
	err = deps.showTUI(cfg, pl, func() (*tview.Application, error) {
		return deps.buildTUI(
			cfg, pl,
			func() bool { return deps.isServiceRunning(cfg) },
			logPath, label,
		)
	})
	if err != nil {
		return fmt.Errorf("error displaying TUI: %w", err)
	}

	return nil
}

// BackgroundService is a service that runs in its own process while the TUI
// watches it, such as daemon.Service.
type BackgroundService interface {
	RunningForAutoStart() (bool, error)
	Start() error
	Running() (bool, error)
}

// RunBackgroundServiceTUI starts svc if it is not already running and shows
// the TUI. Without a terminal the service keeps running on its own, so only a
// failed start is an error then.
func RunBackgroundServiceTUI(pl platforms.Platform, cfg *config.Instance, svc BackgroundService) error {
	return runBackgroundServiceTUIWith(defaultApplianceDeps(), pl, cfg, svc)
}

func runBackgroundServiceTUIWith(
	deps applianceDeps,
	pl platforms.Platform,
	cfg *config.Instance,
	svc BackgroundService,
) error {
	running, err := svc.RunningForAutoStart()
	if err != nil {
		return fmt.Errorf("error checking service status: %w", err)
	}
	var startErr error
	if !running {
		startErr = svc.Start()
		if startErr != nil {
			log.Error().Err(startErr).Msg("could not start service")
		}
	}
	isRunning := func() bool {
		running, runningErr := svc.Running()
		if runningErr != nil {
			log.Error().Err(runningErr).Msg("service PID file conflict")
			return false
		}
		return running
	}

	logPath, label := applianceLogPath(pl)
	err = deps.showTUI(cfg, pl, func() (*tview.Application, error) {
		return deps.buildTUI(cfg, pl, isRunning, logPath, label)
	})
	if err != nil {
		// With no service running there is nothing to fall back to.
		if startErr != nil {
			return fmt.Errorf("could not start service: %w", startErr)
		}
		// Launched without a terminal (no tty or TERM), there is no TUI to
		// show. The service runs in its own process, so this is not a fault.
		log.Warn().Err(err).Msg("TUI unavailable, service continues in the background")
		_, _ = fmt.Fprintf(os.Stderr, "TUI unavailable (%s), service continues in the background.\n", err)
	}

	return nil
}
