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
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/ZaparooProject/zaparoo-core/pkg/cli"
	"github.com/ZaparooProject/zaparoo-core/pkg/config/migrate"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/getlantern/systray"
	"github.com/rs/zerolog"

	"github.com/rs/zerolog/log"

	"github.com/ZaparooProject/zaparoo-core/pkg/platforms/windows"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/service"

	_ "embed"
)

//go:embed systrayicon.ico
var icon []byte

func main() {
	sigs := make(chan os.Signal, 1)
	doStop := make(chan bool, 1)
	stopped := make(chan bool, 1)
	defer close(sigs)
	defer close(doStop)
	defer close(stopped)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	pl := &windows.Platform{}
	flags := cli.SetupFlags()

	flags.Pre(pl)

	defaults := config.BaseDefaults
	defaults.DebugLogging = true
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
		[]io.Writer{zerolog.ConsoleWriter{Out: os.Stderr}},
	)

	flags.Post(cfg, pl)

	stopSvc, err := service.Start(pl, cfg)
	if err != nil {
		log.Error().Msgf("error starting service: %s", err)
		fmt.Println("Error starting service:", err)
		os.Exit(1)
	}

	go func() {
		// just wait for either of these
		select {
		case <-sigs:
			break
		case <-doStop:
			break
		}

		err := stopSvc()
		if err != nil {
			log.Error().Msgf("error stopping service: %s", err)
			os.Exit(1)
		}

		stopped <- true
	}()

	ip, err := utils.GetLocalIp()
	if err != nil {
		fmt.Println("Device address: Unknown")
	} else {
		fmt.Println("Device address:", ip.String())
		fmt.Printf("Web App: http://%s:%d/app/\n", ip.String(), cfg.ApiPort())
	}

	systray.Run(onReady(cfg, ip, pl), func() {
		os.Exit(0)
	})

	fmt.Println("Press any key to exit")
	_, _ = fmt.Scanln()
	doStop <- true
	<-stopped

	os.Exit(0)
}

func onReady(cfg *config.Instance, ip net.IP, pl platforms.Platform) func() {
	return func() {
		systray.SetIcon(icon)
		systray.SetTitle("Zaparoo Core")
		systray.SetTooltip("Zaparoo Core v" + config.AppVersion)

		addr := systray.AddMenuItem(fmt.Sprintf("Address: %s:%d", ip.String(), cfg.ApiPort()), "")
		addr.Disable()

		conf := systray.AddMenuItem("Open config folder", "Open config folder")
		logs := systray.AddMenuItem("View log file", "View log file")
		webui := systray.AddMenuItem("Open web UI", "Open web UI")
		systray.AddSeparator()
		quit := systray.AddMenuItem("Quit", "Stop the Zaparoo service")

		go func() {
			for {
				select {
				case <-quit.ClickedCh:
					systray.Quit()
					return
				case <-webui.ClickedCh:
					addr := fmt.Sprintf("http://%s:%d/app/", ip.String(), cfg.ApiPort())
					err := exec.Command("explorer", addr).Start()
					if err != nil {
						log.Error().Msgf("error opening web UI: %s", err)
					}
				case <-logs.ClickedCh:
					logFile := filepath.Join(pl.Settings().TempDir, "core.log")
					err := exec.Command("explorer", logFile).Start()
					if err != nil {
						log.Error().Msgf("error opening log file: %s", err)
					}
				case <-conf.ClickedCh:
					folder := utils.ConfigDir(pl)
					err := exec.Command("explorer", folder).Start()
					if err != nil {
						log.Error().Msgf("error opening config folder: %s", err)
					}
				}
			}
		}()
	}
}
