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
	_ "embed"
	"flag"
	"fmt"
	"os"
	"path"
	"runtime/debug"

	"github.com/ZaparooProject/zaparoo-core/v2/internal/telemetry"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/cli"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/command"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/knulli"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esbase"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/daemon"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/tui"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

//go:embed scripts/services/zaparoo_service
var serviceFile string

const (
	serviceName     = "zaparoo_service"
	serviceFilePath = "/userdata/system/services/" + serviceName
	// binaryPath is the binary the service script runs.
	binaryPath = "/userdata/system/zaparoo"
)

// serviceInstaller registers the service with knulli-services, which starts
// enabled services at boot.
func serviceInstaller() *esbase.ServiceInstaller {
	return &esbase.ServiceInstaller{
		FS:      afero.NewOsFs(),
		Cmd:     &command.RealExecutor{},
		Path:    serviceFilePath,
		Content: serviceFile,
		Mode:    0o755,
		Stop: [][]string{
			{"knulli-services", "stop", serviceName},
		},
		Enable: [][]string{
			{"knulli-services", "enable", serviceName},
			{"knulli-services", "start", serviceName},
		},
		Disable: [][]string{
			{"knulli-services", "stop", serviceName},
			{"knulli-services", "disable", serviceName},
		},
	}
}

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

	pl := knulli.NewPlatform()
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
		if err := esbase.CheckRunningInstallPath(binaryPath); err != nil {
			return fmt.Errorf("error installing service: %w", err)
		}
		if err := serviceInstaller().Install(context.Background()); err != nil {
			return fmt.Errorf("error installing service: %w", err)
		}
		_, _ = fmt.Println("Zaparoo service installed and started.")
		return nil
	} else if *doUninstall {
		if err := serviceInstaller().Uninstall(context.Background()); err != nil {
			return fmt.Errorf("error uninstalling service: %w", err)
		}
		_, _ = fmt.Println("Zaparoo service uninstalled.")
		return nil
	}

	cfg := cli.Setup(
		pl,
		config.BaseDefaults,
		nil,
	)

	svc, err := daemon.NewService(daemon.ServiceArgs{
		Config: cfg,
		Entry: func() (*service.StartResult, error) {
			return service.Start(pl, cfg)
		},
		Platform: pl,
	})
	if err != nil {
		log.Error().Err(err).Msg("error creating service")
		return fmt.Errorf("error creating service: %w", err)
	}
	err = svc.ServiceHandler(serviceFlag)
	if err != nil {
		return fmt.Errorf("service handler failed: %w", err)
	}

	flags.Post(cfg, pl)

	// try to auto-start service if it's not running already
	running, runningErr := svc.RunningForAutoStart()
	if runningErr != nil {
		return fmt.Errorf("error checking service status: %w", runningErr)
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

	// start the tui
	err = tui.BuildAndRetry(cfg, pl, func() (*tview.Application, error) {
		return tui.BuildMain(
			cfg, pl,
			isRunning,
			path.Join(helpers.DataDir(pl), config.LogFile),
			"storage",
		)
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
