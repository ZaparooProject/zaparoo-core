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
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/cli"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/config/migrate"
	"github.com/ZaparooProject/zaparoo-core/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms/mistex"
	"github.com/ZaparooProject/zaparoo-core/pkg/service"
	"github.com/rs/zerolog/log"
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

	//nolint:gosec // Systemd unit file needs to be readable by system
	err = os.WriteFile(unitPath, []byte(unitFile), 0o644)
	if err != nil {
		return false, fmt.Errorf("failed to write unit file: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "systemctl", "daemon-reload")
	err = cmd.Run()
	if err != nil {
		return false, fmt.Errorf("failed to reload systemd daemon: %w", err)
	}

	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd = exec.CommandContext(ctx, "systemctl", "enable", "zaparoo.service")
	err = cmd.Run()
	if err != nil {
		return false, fmt.Errorf("failed to enable zaparoo service: %w", err)
	}

	return true, nil
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

	pl := &mistex.Platform{}
	flags.Pre(pl)

	if *addStartupFlag {
		_, err := tryAddToStartup()
		if err != nil {
			return fmt.Errorf("error adding to startup: %w", err)
		}
		return nil
	}

	defaults := config.BaseDefaults
	iniPath := "/media/fat/Scripts/tapto.ini"
	if migrate.Required(iniPath, filepath.Join(helpers.ConfigDir(pl), config.CfgFile)) {
		migrated, err := migrate.IniToToml(iniPath)
		if err != nil {
			return fmt.Errorf("error migrating config: %w", err)
		}
		defaults = migrated
	}

	cfg := cli.Setup(
		pl,
		defaults,
		[]io.Writer{os.Stderr},
	)

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
		return fmt.Errorf("service handler error: %w", err)
	}

	flags.Post(cfg, pl)

	_, _ = fmt.Println("Zaparoo v" + config.AppVersion)

	added, err := tryAddToStartup()
	if err != nil {
		log.Error().Msgf("error adding to startup: %s", err)
		return fmt.Errorf("error adding to startup: %w", err)
	} else if added {
		log.Info().Msg("added to startup")
		_, _ = fmt.Println("Added Zaparoo to MiSTeX startup.")
	}

	if !svc.Running() {
		err := svc.Start()
		_, _ = fmt.Println("Zaparoo service not running, starting...")
		if err != nil {
			log.Error().Msgf("error starting service: %s", err)
			_, _ = fmt.Println("Error starting Zaparoo service:", err)
		} else {
			log.Info().Msg("service started manually")
			_, _ = fmt.Println("Zaparoo service started.")
		}
	} else {
		_, _ = fmt.Println("Zaparoo service is running.")
	}

	ip := helpers.GetLocalIP()
	if ip == "" {
		_, _ = fmt.Println("Device address: Unknown")
	} else {
		_, _ = fmt.Println("Device address:", ip)
	}

	return nil
}

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}
