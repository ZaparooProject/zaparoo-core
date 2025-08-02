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
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/pkg/ui/widgets"

	"github.com/ZaparooProject/zaparoo-core/pkg/cli"
	"github.com/ZaparooProject/zaparoo-core/pkg/config/migrate"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	"github.com/rs/zerolog/log"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms/mister"
	"github.com/ZaparooProject/zaparoo-core/pkg/service"

	mrextMister "github.com/wizzomafizzo/mrext/pkg/mister"
)

func addToStartup() error {
	var startup mrextMister.Startup

	err := startup.Load()
	if err != nil {
		return err
	}

	changed := false

	// migration from tapto name
	if startup.Exists("mrext/tapto") {
		err = startup.Remove("mrext/tapto")
		if err != nil {
			return err
		}
		changed = true
	}

	if !startup.Exists("mrext/" + config.AppName) {
		err = startup.AddService("mrext/" + config.AppName)
		if err != nil {
			return err
		}
		changed = true
	}

	if changed && len(startup.Entries) > 0 {
		err = startup.Save()
		if err != nil {
			return err
		}
	}

	return nil
}

func main() {
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
			_, _ = fmt.Fprintf(os.Stderr, "Error adding to startup: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	if _, err := os.Stat("/media/fat/Scripts/tapto.sh"); err == nil {
		_ = exec.Command("/media/fat/Scripts/tapto.sh", "-service", "stop").Run()
	}

	defaults := config.BaseDefaults
	iniPath := "/media/fat/Scripts/tapto.ini"
	if migrate.Required(iniPath, filepath.Join(utils.ConfigDir(pl), config.CfgFile)) {
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

	if *showLoader != "" {
		err := widgets.NoticeUI(pl, *showLoader, true)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Error showing loader: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	} else if *showPicker != "" {
		err := widgets.PickerUI(cfg, pl, *showPicker)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Error showing picker: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	} else if *showNotice != "" {
		err := widgets.NoticeUI(pl, *showNotice, false)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Error showing notice: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	svc, err := utils.NewService(utils.ServiceArgs{
		Entry: func() (func() error, error) {
			return service.Start(pl, cfg)
		},
		Platform: pl,
	})
	if err != nil {
		log.Error().Err(err).Msg("error creating service")
		_, _ = fmt.Fprintf(os.Stderr, "Error creating service: %v\n", err)
		os.Exit(1)
	}
	svc.ServiceHandler(serviceFlag)

	flags.Post(cfg, pl)

	// offer to add service to MiSTer startup if it's not already there
	err = tryAddStartup()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error adding startup: %v\n", err)
		os.Exit(1)
	}

	// try to auto-start service if it's not running already
	if !svc.Running() {
		err := svc.Start()
		if err != nil {
			log.Error().Err(err).Msg("could not start service")
		}
		time.Sleep(1 * time.Second)
	}

	// display main info gui
	enableZapScript := client.DisableZapScript(cfg)
	err = displayServiceInfo(pl, cfg, svc)
	if err != nil {
		enableZapScript()
		_, _ = fmt.Fprintf(os.Stderr, "Error displaying TUI: %v\n", err)
		os.Exit(1)
	}
	enableZapScript()
}
