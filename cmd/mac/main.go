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
	"github.com/ZaparooProject/zaparoo-core/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/cli"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms/mac"
	"github.com/ZaparooProject/zaparoo-core/pkg/simplegui"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	"github.com/nixinwang/dialog"
	"github.com/rs/zerolog/log"
	"golang.design/x/clipboard"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/service"

	_ "embed"
)

import "fyne.io/systray"

//go:embed app/systrayicon.png
var systrayIcon []byte

func isServiceRunning(cfg *config.Instance) bool {
	_, err := client.LocalClient(cfg, models.MethodVersion, "")
	if err != nil {
		log.Debug().Err(err).Msg("error checking if service running")
		return false
	}
	return true
}

func systrayOnReady(cfg *config.Instance, pl platforms.Platform) func() {
	return func() {
		systray.SetIcon(systrayIcon)

		mWebUI := systray.AddMenuItem("Web UI", "Open Zaparoo web UI")
		address := "Unknown"
		ip, err := utils.GetLocalIp()
		if err == nil {
			address = ip.String()
		}
		mAddress := systray.AddMenuItem("Address: "+address, "")
		systray.AddSeparator()

		mEditConfig := systray.AddMenuItem("Edit Config", "Edit Core config file")
		mReloadConfig := systray.AddMenuItem("Reload Config", "Reload Core config file and mappings")
		mOpenMappings := systray.AddMenuItem("Open Mappings", "Open Core mappings directory")
		mOpenLog := systray.AddMenuItem("Open Log", "Open Core log file")

		if cfg.DebugLogging() {
			systray.AddSeparator()
		}
		mOpenDataDir := systray.AddMenuItem("Open Data (Debug)", "Open Core data directory")
		mOpenDataDir.Hide()
		if cfg.DebugLogging() {
			mOpenDataDir.Show()
		}

		systray.AddSeparator()
		mVersion := systray.AddMenuItem("Version "+config.AppVersion, "")
		mVersion.Disable()
		mAbout := systray.AddMenuItem("About Zaparoo Core", "")

		systray.AddSeparator()
		mQuit := systray.AddMenuItem("Quit", "Quit and stop Zaparoo service")

		go func() {
			for {
				select {
				case <-mAddress.ClickedCh:
					err := clipboard.Init()
					if err != nil {
						log.Error().Err(err).Msg("failed to initialize clipboard")
						continue
					}
					clipboard.Write(clipboard.FmtText, []byte(address))
					// TODO: send notification
				case <-mWebUI.ClickedCh:
					url := fmt.Sprintf("http://localhost:%d/app/", cfg.ApiPort())
					err := exec.Command("open", url).Start()
					if err != nil {
						log.Error().Err(err).Msg("failed to open web page")
					}
				case <-mOpenLog.ClickedCh:
					err := exec.Command("open", filepath.Join(pl.Settings().TempDir, config.LogFile)).Start()
					if err != nil {
						log.Error().Err(err).Msg("failed to open log file")
					}
				case <-mEditConfig.ClickedCh:
					err := exec.Command("open", filepath.Join(pl.Settings().ConfigDir, config.CfgFile)).Start()
					if err != nil {
						log.Error().Err(err).Msg("failed to open config file")
					}
				case <-mOpenMappings.ClickedCh:
					err := exec.Command("open", filepath.Join(pl.Settings().DataDir, platforms.MappingsDir)).Start()
					if err != nil {
						log.Error().Err(err).Msg("failed to open mappings dir")
					}
				case <-mReloadConfig.ClickedCh:
					_, err := client.LocalClient(cfg, models.MethodSettingsReload, "")
					if err != nil {
						log.Error().Err(err).Msg("failed to reload config")
					} else {
						log.Info().Msg("reloaded config")
					}
				case <-mOpenDataDir.ClickedCh:
					err := exec.Command("open", pl.Settings().DataDir).Start()
					if err != nil {
						log.Error().Err(err).Msg("failed to open data dir")
					}
				case <-mAbout.ClickedCh:
					msg := "Zaparoo Core\n" +
						"Version v%s\n\n" +
						"© %d Zaparoo Contributors\n" +
						"License: GPLv3\n\n" +
						"www.zaparoo.org"
					dialog.Message(msg, config.AppVersion, time.Now().Year()).Title("About Zaparoo Core").Info()
				case <-mQuit.ClickedCh:
					systray.Quit()
				}
			}
		}()
	}
}

func main() {
	if os.Geteuid() == 0 {
		_, _ = fmt.Fprintf(os.Stderr, "Zaparoo cannot be run as root\n")
		os.Exit(1)
	}

	pl := &mac.Platform{}
	flags := cli.SetupFlags()

	daemonMode := flag.Bool(
		"daemon",
		false,
		"run Zaparoo service in foreground with no GUI",
	)
	appMode := flag.Bool(
		"app",
		false,
		"run Zaparoo service as daemon in menu bar",
	)

	flags.Pre(pl)

	var logWriters []io.Writer
	if *daemonMode || *appMode {
		logWriters = []io.Writer{os.Stderr}
	}

	cfg := cli.Setup(
		pl,
		config.BaseDefaults,
		logWriters,
	)

	defer func() {
		if err := recover(); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Panic: %s\n", err)
			log.Fatal().Msgf("panic: %v", err)
		}
	}()

	flags.Post(cfg, pl)

	if *daemonMode || *appMode {
		sigs := make(chan os.Signal, 1)
		defer close(sigs)
		signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

		exit := make(chan bool, 1)
		defer close(exit)

		stopSvc, err := service.Start(pl, cfg)
		if err != nil {
			// TODO: send notification if failed or succeeded
			log.Error().Err(err).Msg("error starting service")
			_, _ = fmt.Fprintf(os.Stderr, "Error starting service: %s\n", err)
			os.Exit(1)
		}

		if *appMode {
			systray.Run(systrayOnReady(cfg, pl), func() {
				exit <- true
			})
		}

		select {
		case <-sigs:
		case <-exit:
		}

		err = stopSvc()
		if err != nil {
			log.Error().Err(err).Msgf("error stopping service")
			_, _ = fmt.Fprintf(os.Stderr, "Error stopping service: %s\n", err)
			os.Exit(1)
		}

		os.Exit(0)
	}

	if !isServiceRunning(cfg) {
		stopSvc, err := service.Start(pl, cfg)
		if err != nil {
			log.Error().Msgf("error starting service: %s", err)
			_, _ = fmt.Fprintf(os.Stderr, "Error starting service: %s\n", err)
			os.Exit(1)
		}

		defer func() {
			err := stopSvc()
			if err != nil {
				log.Error().Msgf("error stopping service: %s", err)
			}
		}()
	}

	app, err := simplegui.BuildTheUi(
		pl, isServiceRunning(cfg), cfg,
		filepath.Join(os.Getenv("HOME"), "Desktop", "core.log"),
	)
	if err != nil {
		log.Error().Err(err).Msgf("error building UI")
		_, _ = fmt.Fprintf(os.Stderr, "Error building UI: %s\n", err)
		os.Exit(1)
	}

	err = app.Run()
	if err != nil {
		log.Error().Err(err).Msg("error running UI")
		_, _ = fmt.Fprintf(os.Stderr, "Error running UI: %s\n", err)
		os.Exit(1)
	}

	os.Exit(0)
}
