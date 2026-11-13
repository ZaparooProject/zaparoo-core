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
	_ "embed"
	"flag"
	"fmt"
	"os"

	"github.com/ZaparooProject/zaparoo-core/v2/internal/telemetry"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/cli"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/command"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/knulli"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esbase"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/daemon"
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
	defer cli.ExitOnPanic()

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

	handled, err := cli.RunServiceInstallFlags(serviceInstaller(), binaryPath, *doInstall, *doUninstall, os.Stdout)
	if handled || err != nil {
		return err //nolint:wrapcheck // already wrapped by cli
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

	if err := cli.RunBackgroundServiceTUI(pl, cfg, svc); err != nil {
		return fmt.Errorf("run failed: %w", err)
	}
	return nil
}
