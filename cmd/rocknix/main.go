//go:build linux && !android

/*
Zaparoo Core
Copyright (C) 2026 Callan Barrett

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
	"io"
	"os"

	"github.com/ZaparooProject/zaparoo-core/v2/internal/telemetry"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/cli"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/rocknix"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esbase"
	"github.com/rs/zerolog"
)

//go:embed conf/zaparoo.service
var serviceFile string

const (
	// installDir is where the unit expects the binary.
	installDir      = "/storage/zaparoo"
	binaryPath      = installDir + "/zaparoo"
	serviceName     = "zaparoo.service"
	serviceFilePath = "/storage/.config/system.d/" + serviceName
)

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(cli.ExitCodeFor(err))
	}
}

func run() error {
	defer telemetry.Close()
	defer cli.ExitOnPanic()

	pl := rocknix.NewPlatform()
	flags := cli.SetupFlags()

	asDaemon := flag.Bool("daemon", false, "run zaparoo in daemon mode")
	doInstall := flag.Bool("install", false, "install zaparoo systemd service")
	doUninstall := flag.Bool("uninstall", false, "uninstall zaparoo systemd service")

	flags.Pre(pl)

	if *doInstall {
		if err := serviceInstaller().InstallCommand(context.Background(), binaryPath, os.Stdout); err != nil {
			return fmt.Errorf("error installing service: %w", err)
		}
		return nil
	}
	if *doUninstall {
		if err := serviceInstaller().UninstallCommand(context.Background(), os.Stdout); err != nil {
			return fmt.Errorf("error uninstalling service: %w", err)
		}
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
		if err := cli.RunApplianceDaemon(pl, cfg); err != nil {
			return fmt.Errorf("run failed: %w", err)
		}
		return nil
	}

	if err := cli.RunApplianceTUI(pl, cfg); err != nil {
		return fmt.Errorf("run failed: %w", err)
	}
	return nil
}

// serviceInstaller puts the unit in the system.d directory on the storage
// partition, which systemd loads at boot.
func serviceInstaller() *esbase.ServiceInstaller {
	return esbase.SystemdUnitInstaller(serviceFilePath, serviceName, serviceFile)
}
