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
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"

	"github.com/ZaparooProject/zaparoo-core/v2/internal/telemetry"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/cli"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config/migrate"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/steamos"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/steamos/steamruntime"
	"github.com/rs/zerolog/log"
)

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}

func steamOSDefaults(freshInstall bool) config.Values {
	defaults := config.BaseDefaults
	defaults.Service.Encryption = freshInstall
	return defaults
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

	if steamruntime.IsInvocation(os.Args[0]) {
		if err := steamruntime.Run(context.Background()); err != nil {
			return fmt.Errorf("run Steam Runtime: %w", err)
		}
		return nil
	}

	install := flag.String(
		"install",
		"",
		"install component: application, desktop, service, hardware, steam-runtime",
	)
	uninstall := flag.String(
		"uninstall",
		"",
		"uninstall component: application, desktop, service, hardware, steam-runtime",
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
	steamRuntimeStatus := flag.Bool("steam-runtime-status", false, "report Steam Runtime integration status")

	flags.Pre(pl)

	if *steamRuntimeStatus {
		status, err := steamruntime.Status()
		if err != nil {
			return fmt.Errorf("inspect Steam Runtime: %w", err)
		}
		_, _ = fmt.Fprintf(
			os.Stdout, "State: %s\nRuntime: %s\nDesktop: %s\nShortcuts: %d\n",
			status.State, status.RuntimePath, status.DesktopPath, len(status.ShortcutIDs),
		)
		return nil
	}

	if *install != "" {
		if *install == "steam-runtime" {
			result, err := steamruntime.Install(context.Background())
			if err != nil {
				return fmt.Errorf("install Steam Runtime: %w", err)
			}
			if result.SteamRestartNeeded {
				_, _ = fmt.Fprintln(os.Stdout, "Steam Runtime installed; restart Steam to load its shortcut")
			} else {
				_, _ = fmt.Fprintf(os.Stdout, "Steam Runtime installed as shortcut %d\n", result.ShortcutID)
			}
			return nil
		}
		if err := cli.HandleInstall(*install); err != nil {
			return fmt.Errorf("install failed: %w", err)
		}
		return nil
	}
	if *uninstall != "" {
		if *uninstall == "steam-runtime" {
			if err := steamruntime.Uninstall(); err != nil {
				return fmt.Errorf("uninstall Steam Runtime: %w", err)
			}
			_, _ = fmt.Fprintln(os.Stdout, "Steam Runtime files removed; remove its Steam shortcut manually")
			return nil
		}
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

	// SteamOS-specific: Migrate config from legacy tapto.ini if present.
	configPath := filepath.Join(helpers.ConfigDir(pl), config.CfgFile)
	iniPath := filepath.Join(helpers.ExeDir(), "tapto.ini")
	migrationRequired := migrate.Required(iniPath, configPath)
	_, configErr := os.Stat(configPath)
	freshInstall := errors.Is(configErr, os.ErrNotExist) && !migrationRequired
	defaults := steamOSDefaults(freshInstall)
	if migrationRequired {
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
