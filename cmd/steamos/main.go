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
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/cli"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config/migrate"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/steamos"
)

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}

func run() error {
	install := flag.String(
		"install",
		"",
		"install component: application, desktop, service, hardware",
	)
	uninstall := flag.String(
		"uninstall",
		"",
		"uninstall component: application, desktop, service, hardware",
	)

	pl := steamos.NewPlatform()
	flags := cli.SetupFlags()

	daemonMode := flag.Bool(
		"daemon",
		false,
		"run service in foreground with no UI",
	)
	start := flag.Bool(
		"start",
		false,
		"start service and open web UI in browser",
	)

	flags.Pre(pl)

	if *install != "" {
		if err := cli.HandleInstall(*install); err != nil {
			return fmt.Errorf("install failed: %w", err)
		}
		return nil
	}
	if *uninstall != "" {
		if err := cli.HandleUninstall(*uninstall); err != nil {
			return fmt.Errorf("uninstall failed: %w", err)
		}
		return nil
	}

	if os.Geteuid() == 0 {
		return errors.New("zaparoo cannot be run as root")
	}

	var logWriters []io.Writer
	if *daemonMode {
		logWriters = []io.Writer{os.Stderr}
	}

	// SteamOS-specific: Migrate config from legacy tapto.ini if present
	defaults := config.BaseDefaults
	iniPath := filepath.Join(helpers.ExeDir(), "tapto.ini")
	if migrate.Required(iniPath, filepath.Join(helpers.ConfigDir(pl), config.CfgFile)) {
		migrated, migrateErr := migrate.IniToToml(iniPath)
		if migrateErr != nil {
			return fmt.Errorf("error migrating config: %w", migrateErr)
		}
		defaults = migrated
	}

	cfg := cli.Setup(pl, defaults, logWriters)

	if *start {
		if err := cli.StartAndOpenBrowser(cfg); err != nil {
			return fmt.Errorf("start failed: %w", err)
		}
		return nil
	}

	flags.Post(cfg, pl)

	if err := cli.RunApp(pl, cfg, *daemonMode); err != nil {
		return fmt.Errorf("run failed: %w", err)
	}
	return nil
}
