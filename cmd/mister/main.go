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
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/pkg/cli"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/config/migrate"
	"github.com/ZaparooProject/zaparoo-core/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms/mister"
	mrextmister "github.com/ZaparooProject/zaparoo-core/pkg/platforms/mister/startup"
	"github.com/ZaparooProject/zaparoo-core/pkg/service"
	"github.com/ZaparooProject/zaparoo-core/pkg/ui/widgets"
	"github.com/rs/zerolog/log"
)

func addToStartup() error {
	var startup mrextmister.Startup

	err := startup.Load()
	if err != nil {
		return fmt.Errorf("failed to load startup config: %w", err)
	}

	changed := false

	// migration from tapto name
	if startup.Exists("mrext/tapto") {
		err = startup.Remove("mrext/tapto")
		if err != nil {
			return fmt.Errorf("failed to remove tapto from startup: %w", err)
		}
		changed = true
	}

	if !startup.Exists("mrext/" + config.AppName) {
		err = startup.AddService("mrext/" + config.AppName)
		if err != nil {
			return fmt.Errorf("failed to add service to startup: %w", err)
		}
		changed = true
	}

	if changed && len(startup.Entries) > 0 {
		err = startup.Save()
		if err != nil {
			return fmt.Errorf("failed to save startup config: %w", err)
		}
	}

	return nil
}

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}

func run() error {
	flags := cli.SetupFlags()
	serviceFlag := flag.String(
		"service",
		"",
		"manage Zaparoo service (start|stop|restart|status)",
	)
	addStartupFlag := flag.Bool(
		"add-startup",
		false,
		"add Zaparoo service to MiSTer startup if not already added",
	)
	showLoader := flag.String(
		"show-loader",
		"",
		"display a generic loading widget",
	)
	showNotice := flag.String(
		"show-notice",
		"",
		"display a generic notice widget",
	)
	showPicker := flag.String(
		"show-picker",
		"",
		"display a generic list picker widget",
	)

	pl := mister.NewPlatform()
	flags.Pre(pl)

	if *addStartupFlag {
		err := addToStartup()
		if err != nil {
			return fmt.Errorf("error adding to startup: %w", err)
		}
		return nil
	}

	if _, err := os.Stat("/media/fat/Scripts/tapto.sh"); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = exec.CommandContext(ctx, "/media/fat/Scripts/tapto.sh", "-service", "stop").Run()
	}

	defaults := config.BaseDefaults
	iniPath := "/media/fat/Scripts/tapto.ini"
	if migrate.Required(iniPath, filepath.Join(helpers.ConfigDir(pl), config.CfgFile)) {
		migrated, err := migrate.IniToToml(iniPath)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Error migrating config: %v\n", err)
		} else {
			defaults = migrated
		}
	}

	cfg := cli.Setup(pl, defaults, nil)

	defer func() {
		if err := recover(); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Panic: %v\n", err)
			log.Fatal().Msgf("panic: %v", err)
		}
	}()

	switch {
	case *showLoader != "":
		err := widgets.NoticeUI(pl, *showLoader, true)
		if err != nil {
			return fmt.Errorf("error showing loader: %w", err)
		}
		return nil
	case *showPicker != "":
		err := widgets.PickerUI(cfg, pl, *showPicker)
		if err != nil {
			return fmt.Errorf("error showing picker: %w", err)
		}
		return nil
	case *showNotice != "":
		err := widgets.NoticeUI(pl, *showNotice, false)
		if err != nil {
			return fmt.Errorf("error showing notice: %w", err)
		}
		return nil
	}

	svc, err := helpers.NewService(helpers.ServiceArgs{
		Entry: func() (func() error, error) {
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

	// offer to add service to MiSTer startup if it's not already there
	err = tryAddStartup()
	if err != nil {
		return fmt.Errorf("error adding startup: %w", err)
	}

	// try to auto-start service if it's not running already
	if !svc.Running() {
		startErr := svc.Start()
		if startErr != nil {
			log.Error().Err(startErr).Msg("could not start service")
		}
		time.Sleep(1 * time.Second)
	}

	// display main info gui
	enableZapScript := client.DisableZapScript(cfg)
	err = displayServiceInfo(pl, cfg, svc)
	if err != nil {
		enableZapScript()
		return fmt.Errorf("error displaying TUI: %w", err)
	}
	enableZapScript()

	return nil
}
