// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later
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
	"path/filepath"
	"runtime"
	"time"

	"fyne.io/systray"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
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
		mUploadLog := systray.AddMenuItem("Upload Log", "Upload the log file and copy a link to share")

		systray.AddSeparator()
		mPair := systray.AddMenuItem("Pair Device...", "Show a PIN to pair a phone or tablet")
		// Titled from status rather than fixed, so the menu says whether there
		// is anything to install before it is opened. Refreshed on a timer from
		// update.status, which reads the last check off disk instead of asking
		// the release server every time the menu is drawn.
		mUpdate := systray.AddMenuItem(updateMenuTitle(nil), "")
		go watchUpdateStatus(cfg, mUpdate)

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
					if err := copyToClipboard(ip, clipboard.Init, clipboard.Write); err != nil {
						log.Error().Err(err).Msg("failed to copy address to clipboard")
						notify("Error copying address to clipboard.")
						continue
					}
					notify("Copied address to clipboard.")
				case <-mWebUI.ClickedCh:
					url := fmt.Sprintf("http://localhost:%d/app/", cfg.APIPort())
					if err := openPath(url); err != nil {
						log.Error().Err(err).Msg("failed to open web page")
						notify("Error opening Web UI.")
					}
				case <-mOpenLog.ClickedCh:
					logPath := filepath.Join(pl.Settings().LogDir, config.LogFile)
					if err := openPath(logPath); err != nil {
						log.Error().Err(err).Msg("failed to open log file")
						notify("Error opening log file.")
					}
				case <-mUploadLog.ClickedCh:
					// Its own goroutine: every other handler here runs inline
					// on this select loop, and an upload can take the better
					// part of a minute, which would freeze the whole menu.
					go uploadLogFromMenu(pl, helpers.UploadLog, copyURLToClipboard, nativeDialog)
				case <-mEditConfig.ClickedCh:
					configPath := filepath.Join(helpers.ConfigDir(pl), config.CfgFile)
					if err := openPath(configPath); err != nil {
						log.Error().Err(err).Msg("failed to open config file")
						notify("Error opening config file.")
					}
				case <-mOpenMappings.ClickedCh:
					mappingsPath := filepath.Join(helpers.DataDir(pl), config.MappingsDir)
					if err := openPath(mappingsPath); err != nil {
						log.Error().Err(err).Msg("failed to open mappings dir")
						notify("Error opening mappings directory.")
					}
				case <-mOpenLaunchers.ClickedCh:
					launchersPath := filepath.Join(helpers.DataDir(pl), config.LaunchersDir)
					if err := openPath(launchersPath); err != nil {
						log.Error().Err(err).Msg("failed to open launchers dir")
						notify("Error opening launchers directory.")
					}
				case <-mReloadConfig.ClickedCh:
					err := reloadCore(cfg, client.LocalClient)
					if err != nil {
						log.Warn().Err(err).Msg("failed to reload config")
						notify("Error reloading Core config.")
					} else {
						log.Info().Msg("reloaded config")
						notify("Core config successfully reloaded.")
					}
				case <-mOpenDataDir.ClickedCh:
					if err := openPath(helpers.DataDir(pl)); err != nil {
						log.Error().Err(err).Msg("failed to open data dir")
						notify("Error opening data directory.")
					}
				case <-mPair.ClickedCh:
					go startPairing(cfg, notify)
				case <-mUpdate.ClickedCh:
					go applyUpdateFromMenu(cfg, mUpdate, notify)
				case <-mAbout.ClickedCh:
					msg := "Zaparoo Core\n" +
						"Version v%s\n\n" +
						"© %d Zaparoo Contributors\n" +
						"License: GPLv3\n\n" +
						"www.zaparoo.org"
					nativeDialog("About Zaparoo Core",
						fmt.Sprintf(msg, config.AppVersion, time.Now().Year()))
				case <-mQuit.ClickedCh:
					systray.Quit()
				}
			}
		}()
	}
}

// clipboardWriter is the signature of clipboard.Write, taken as a parameter so
// the failure paths can be tested without a display server.
type clipboardWriter func(
	ctx context.Context, format clipboard.Format, buf []byte, opts ...clipboard.Option,
) (<-chan struct{}, error)

// copyToClipboard puts text on the system clipboard.
//
// Both steps can fail for the same reason — a machine with no clipboard to
// write to — and the caller says the same thing either way, so they are
// reported as one error rather than two branches.
func copyToClipboard(text string, initFn func() error, writeFn clipboardWriter) error {
	if err := initFn(); err != nil {
		return fmt.Errorf("initialising the clipboard: %w", err)
	}
	// The returned channel only fires if something else later overwrites the
	// clipboard, which is not this menu's business; the text is on the
	// clipboard as soon as Write returns.
	if _, err := writeFn(context.Background(), clipboard.FmtText, []byte(text)); err != nil {
		return fmt.Errorf("writing to the clipboard: %w", err)
	}
	return nil
}

// reloadCore reloads settings and mappings, then reloads the custom launcher
// files and rebuilds the launcher cache. Both calls are needed: settings.reload
// does not read the launchers directory, so editing a custom launcher TOML has
// no effect until launchers.refresh runs.
func reloadCore(
	cfg *config.Instance,
	localClient func(context.Context, *config.Instance, string, string) (string, error),
) error {
	ctx := context.Background()
	if _, err := localClient(ctx, cfg, models.MethodSettingsReload, ""); err != nil {
		return fmt.Errorf("reload settings: %w", err)
	}
	if _, err := localClient(ctx, cfg, models.MethodLaunchersRefresh, ""); err != nil {
		return fmt.Errorf("refresh launchers: %w", err)
	}
	return nil
}

// Run shows the tray icon and does not return until the tray quits, because the
// native event loop it starts owns the calling thread. Anything that has to
// react to the service ending has to watch for it on another goroutine and call
// Quit.
func Run(
	cfg *config.Instance,
	pl platforms.Platform,
	icon []byte,
	notify func(string),
	exit func(),
) {
	systray.Run(systrayOnReady(cfg, pl, icon, notify), exit)
}

// Quit ends the tray's event loop, which lets Run return. It is safe to call
// from another goroutine and does nothing if the tray has already quit.
func Quit() {
	systray.Quit()
}

// copyURLToClipboard is the real clipboard write, separated so the menu action
// can be tested without a display server.
func copyURLToClipboard(text string) error {
	return copyToClipboard(text, clipboard.Init, clipboard.Write)
}

// uploadLogFromMenu uploads the log bundle, puts the link on the clipboard and
// shows it.
//
// Outcomes go through showDialog rather than notify: notify logs at Debug on
// Windows and is a no-op on macOS, so a user would see nothing either way. The
// dialog is a plain message box whose text cannot be selected, which is why
// the clipboard write is the part that actually hands over the link — and why
// a clipboard that will not open is worth saying out loud rather than leaving
// the user to discover on paste.
func uploadLogFromMenu(
	pl platforms.Platform,
	upload func(platforms.Platform) (string, error),
	copyText func(string) error,
	show showDialog,
) {
	url, err := upload(pl)
	if err != nil {
		log.Error().Err(err).Msg("log upload failed")
		show("Upload Log", helpers.DescribeUploadFailure(err)+
			"\n\nThe log file is at:\n"+helpers.LogPath(pl))
		return
	}

	message := url
	if copyErr := copyText(url); copyErr != nil {
		log.Warn().Err(copyErr).Msg("failed to copy log URL to clipboard")
		message += "\n\nCopy this link by hand: it could not be put on your clipboard."
	} else {
		message += "\n\nThis link has been copied to your clipboard."
	}

	show("Upload Log", message)
}
