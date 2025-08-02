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
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms/steamos"
	"github.com/ZaparooProject/zaparoo-core/pkg/service"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	"github.com/adrg/xdg"
	"github.com/rs/zerolog/log"

	_ "embed"
)

// TODO: fix permissions on files in ~/zaparoo so root doesn't lock them

func main() {
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
			_, _ = fmt.Fprintf(os.Stderr, "Install must be run as root\n")
			os.Exit(1)
		}
		err := install()
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Error installing service: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	} else if *doUninstall {
		if uid != 0 {
			_, _ = fmt.Fprintf(os.Stderr, "Uninstall must be run as root\n")
			os.Exit(1)
		}
		err := uninstall()
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Error uninstalling service: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	if uid == 0 {
		_, _ = fmt.Fprintf(os.Stderr, "Service must not be run as root\n")
		os.Exit(1)
	}

	err := os.MkdirAll(filepath.Join(xdg.DataHome, config.AppName), 0755)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error creating data directory: %v\n", err)
		os.Exit(1)
	}

	defaults := config.BaseDefaults
	iniPath := filepath.Join(utils.ExeDir(), "tapto.ini")
	if migrate.Required(iniPath, filepath.Join(utils.ConfigDir(pl), config.CfgFile)) {
		migrated, err := migrate.IniToToml(iniPath)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Error migrating config: %v\n", err)
			os.Exit(1)
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
