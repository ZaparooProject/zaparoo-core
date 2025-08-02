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
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/ZaparooProject/zaparoo-core/pkg/cli"
	"github.com/ZaparooProject/zaparoo-core/pkg/config/migrate"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms/mistex"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"

	"github.com/rs/zerolog/log"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/service"
)

func tryAddToStartup() (bool, error) {
	unitPath := "/etc/systemd/system/zaparoo.service"
	unitFile := `[Unit]
Description=Zaparoo Core service

[Service]
Type=forking
Restart=no
ExecStart=/media/fat/Scripts/zaparoo_service -service start

[Install]
WantedBy=multi-user.target
`

	_, err := os.Stat(unitPath)
	if err == nil {
		return false, nil
	}

	err = os.WriteFile(unitPath, []byte(unitFile), 0644)
	if err != nil {
		return false, err
	}

	cmd := exec.Command("systemctl", "daemon-reload")
	err = cmd.Run()
	if err != nil {
		return false, err
	}

	cmd = exec.Command("systemctl", "enable", "zaparoo.service")
	err = cmd.Run()
	if err != nil {
		return false, err
	}

	return true, nil
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

	pl := &mistex.Platform{}
	flags.Pre(pl)

	if *addStartupFlag {
		_, err := tryAddToStartup()
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Error adding to startup: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	defaults := config.BaseDefaults
	iniPath := "/media/fat/Scripts/tapto.ini"
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

	fmt.Println("Zaparoo v" + config.AppVersion)

	added, err := tryAddToStartup()
	if err != nil {
		log.Error().Msgf("error adding to startup: %s", err)
		fmt.Println("Error adding to startup:", err)
		os.Exit(1)
	} else if added {
		log.Info().Msg("added to startup")
		fmt.Println("Added Zaparoo to MiSTeX startup.")
	}

	if !svc.Running() {
		err := svc.Start()
		fmt.Println("Zaparoo service not running, starting...")
		if err != nil {
			log.Error().Msgf("error starting service: %s", err)
			fmt.Println("Error starting Zaparoo service:", err)
		} else {
			log.Info().Msg("service started manually")
			fmt.Println("Zaparoo service started.")
		}
	} else {
		fmt.Println("Zaparoo service is running.")
	}

	ip := utils.GetLocalIP()
	if ip == "" {
		fmt.Println("Device address: Unknown")
	} else {
		fmt.Println("Device address:", ip)
	}
}
