// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-only
//
// This file is part of Zaparoo Core.
//
// Zaparoo Core is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// Zaparoo Core is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.

package systray

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"fyne.io/systray"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/nixinwang/dialog"
	"github.com/rs/zerolog/log"
	"golang.design/x/clipboard"
)

func systrayOnReady(
	cfg *config.Instance,
	pl platforms.Platform,
	icon []byte,
	notify func(string),
) func() {
	return func() {
		openCmd := ""
		switch runtime.GOOS {
		case "windows":
			openCmd = "explorer"
		case "darwin":
			openCmd = "open"
		default:
			openCmd = "xdg-open"
		}

		systray.SetIcon(icon)
		if runtime.GOOS != "darwin" {
			systray.SetTitle("Zaparoo Core")
		}
		systray.SetTooltip("Zaparoo Core")

		mWebUI := systray.AddMenuItem("Open", "Open Zaparoo web UI")
		ip := helpers.GetLocalIP()
		if ip == "" {
			ip = "Unknown"
		}
		mAddress := systray.AddMenuItem("Address: "+ip, "")
		systray.AddSeparator()

		mEditConfig := systray.AddMenuItem("Edit Config", "Edit Core config file")
		mOpenMappings := systray.AddMenuItem("Mappings", "Open Core mappings directory")
		mOpenLaunchers := systray.AddMenuItem("Launchers", "Open Core custom launchers directory")
		mReloadConfig := systray.AddMenuItem("Reload", "Reload Core settings and files")
		mOpenLog := systray.AddMenuItem("View Log", "View Core log file")

		if cfg.DebugLogging() {
			systray.AddSeparator()
		}
		mOpenDataDir := systray.AddMenuItem("Data (Debug)", "Open Core data directory")
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
						notify("Error copying address to clipboard.")
						continue
					}
					clipboard.Write(clipboard.FmtText, []byte(ip))
					notify("Copied address to clipboard.")
				case <-mWebUI.ClickedCh:
					url := fmt.Sprintf("http://localhost:%d/app/", cfg.APIPort())
					//nolint:gosec // Safe: opens system file manager with localhost URL
					err := exec.CommandContext(context.Background(), openCmd, url).Start()
					if err != nil {
						log.Error().Err(err).Msg("failed to open web page")
						notify("Error opening Web UI.")
					}
				case <-mOpenLog.ClickedCh:
					logPath := filepath.Join(pl.Settings().TempDir, config.LogFile)
					//nolint:gosec // Safe: opens system file manager with internal log path
					err := exec.CommandContext(context.Background(), openCmd, logPath).Start()
					if err != nil {
						log.Error().Err(err).Msg("failed to open log file")
						notify("Error opening log file.")
					}
				case <-mEditConfig.ClickedCh:
					configPath := filepath.Join(helpers.ConfigDir(pl), config.CfgFile)
					//nolint:gosec // Safe: opens system file manager with internal config path
					err := exec.CommandContext(context.Background(), openCmd, configPath).Start()
					if err != nil {
						log.Error().Err(err).Msg("failed to open config file")
						notify("Error opening config file.")
					}
				case <-mOpenMappings.ClickedCh:
					mappingsPath := filepath.Join(helpers.DataDir(pl), config.MappingsDir)
					//nolint:gosec // Safe: opens system file manager with internal mappings directory
					err := exec.CommandContext(context.Background(), openCmd, mappingsPath).Start()
					if err != nil {
						log.Error().Err(err).Msg("failed to open mappings dir")
						notify("Error opening mappings directory.")
					}
				case <-mOpenLaunchers.ClickedCh:
					launchersPath := filepath.Join(helpers.DataDir(pl), config.LaunchersDir)
					//nolint:gosec // Safe: opens system file manager with internal launchers directory
					err := exec.CommandContext(context.Background(), openCmd, launchersPath).Start()
					if err != nil {
						log.Error().Err(err).Msg("failed to open launchers dir")
						notify("Error opening launchers directory.")
					}
				case <-mReloadConfig.ClickedCh:
					_, err := client.LocalClient(context.Background(), cfg, models.MethodSettingsReload, "")
					if err != nil {
						log.Error().Err(err).Msg("failed to reload config")
						notify("Error reloading Core config.")
					} else {
						log.Info().Msg("reloaded config")
						notify("Core config successfully reloaded.")
					}
				case <-mOpenDataDir.ClickedCh:
					//nolint:gosec // Safe: opens file manager to internal data directory
					err := exec.CommandContext(context.Background(), openCmd, helpers.DataDir(pl)).Start()
					if err != nil {
						log.Error().Err(err).Msg("failed to open data dir")
						notify("Error opening data directory.")
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

func Run(
	cfg *config.Instance,
	pl platforms.Platform,
	icon []byte,
	notify func(string),
	exit func(),
) {
	systray.Run(systrayOnReady(cfg, pl, icon, notify), exit)
}
