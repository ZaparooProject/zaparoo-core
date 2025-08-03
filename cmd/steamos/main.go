//go:build linux

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
	_ "embed"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/ZaparooProject/zaparoo-core/pkg/cli"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/config/migrate"
	"github.com/ZaparooProject/zaparoo-core/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms/steamos"
	"github.com/ZaparooProject/zaparoo-core/pkg/service"
	"github.com/adrg/xdg"
	"github.com/rs/zerolog/log"
)

// TODO: fix permissions on files in ~/zaparoo so root doesn't lock them

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}

func run() error {
	sigs := make(chan os.Signal, 1)
	defer close(sigs)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	pl := &steamos.Platform{}
	flags := cli.SetupFlags()

	doInstall := flag.Bool("install", false, "install zaparoo service")
	doUninstall := flag.Bool("uninstall", false, "uninstall zaparoo service")

	flags.Pre(pl)

	uid := os.Getuid()
	if *doInstall {
		if uid != 0 {
			return fmt.Errorf("install must be run as root")
		}
		err := install()
		if err != nil {
			return fmt.Errorf("error installing service: %w", err)
		}
		return nil
	} else if *doUninstall {
		if uid != 0 {
			return fmt.Errorf("uninstall must be run as root")
		}
		err := uninstall()
		if err != nil {
			return fmt.Errorf("error uninstalling service: %w", err)
		}
		return nil
	}

	if uid == 0 {
		return fmt.Errorf("service must not be run as root")
	}

	err := os.MkdirAll(filepath.Join(xdg.DataHome, config.AppName), 0o755)
	if err != nil {
		return fmt.Errorf("error creating data directory: %w", err)
	}

	defaults := config.BaseDefaults
	iniPath := filepath.Join(helpers.ExeDir(), "tapto.ini")
	if migrate.Required(iniPath, filepath.Join(helpers.ConfigDir(pl), config.CfgFile)) {
		migrated, err := migrate.IniToToml(iniPath)
		if err != nil {
			return fmt.Errorf("error migrating config: %w", err)
		} else {
			defaults = migrated
		}
	}

	cfg := cli.Setup(
		pl,
		defaults,
		[]io.Writer{os.Stderr},
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
