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

package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
)

const (
	PageMain                  = "main"
	PageSettingsMain          = "settings_main"
	PageSettingsBasic         = "settings_basic"
	PageSettingsAdvanced      = "settings_advanced"
	PageSettingsReaderList    = "settings_reader_list"
	PageSettingsReaderEdit    = "settings_reader_edit"
	PageSettingsIgnoreSystems = "settings_ignore_systems"
	PageSettingsTagsRead      = "settings_tags_read"
	PageSettingsTagsWrite     = "settings_tags_write"
	PageSettingsAudio         = "settings_audio"
	PageSettingsReaders       = "settings_readers"
	PageSettingsScanMode      = "settings_readers_scanMode"
	PageSettingsAudioMenu     = "settings_audio_menu"
	PageSettingsReadersMenu   = "settings_readers_menu"
	PageSettingsTUI           = "settings_tui"
	PageSearchMedia           = "search_media"
	PageExportLog             = "export_log"
	PageGenerateDB            = "generate_db"
)

func getTokens(ctx context.Context, cfg *config.Instance) (models.TokensResponse, error) {
	resp, err := client.LocalClient(ctx, cfg, models.MethodTokens, "")
	if err != nil {
		return models.TokensResponse{}, fmt.Errorf("failed to get tokens from local client: %w", err)
	}
	var tokens models.TokensResponse
	err = json.Unmarshal([]byte(resp), &tokens)
	if err != nil {
		return models.TokensResponse{}, fmt.Errorf("failed to unmarshal tokens response: %w", err)
	}
	return tokens, nil
}

func getReaders(ctx context.Context, cfg *config.Instance) (models.ReadersResponse, error) {
	resp, err := client.LocalClient(ctx, cfg, models.MethodReaders, "")
	if err != nil {
		return models.ReadersResponse{}, fmt.Errorf("failed to get readers from local client: %w", err)
	}
	var readers models.ReadersResponse
	err = json.Unmarshal([]byte(resp), &readers)
	if err != nil {
		return models.ReadersResponse{}, fmt.Errorf("failed to unmarshal readers response: %w", err)
	}
	return readers, nil
}

func formatReaderStatus(readers []models.ReaderInfo) string {
	count := len(readers)
	if count == 0 {
		return "No readers connected"
	}
	if count == 1 {
		return fmt.Sprintf("1 reader connected (%s)", readers[0].Driver)
	}
	return fmt.Sprintf("%d readers connected", count)
}

func setupButtonNavigation(
	app *tview.Application,
	buttons ...*tview.Button,
) {
	for i, button := range buttons {
		prevIndex := (i - 1 + len(buttons)) % len(buttons)
		nextIndex := (i + 1) % len(buttons)

		button.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
			k := event.Key()
			switch k {
			case tcell.KeyUp, tcell.KeyLeft:
				app.SetFocus(buttons[prevIndex])
				return event
			case tcell.KeyDown, tcell.KeyRight:
				app.SetFocus(buttons[nextIndex])
				return event
			case tcell.KeyEscape:
				app.Stop()
				return nil
			default:
				return event
			}
		})
	}
}

var mainPageNotifyCancel context.CancelFunc

func BuildMainPage(
	cfg *config.Instance,
	pages *tview.Pages,
	app *tview.Application,
	pl platforms.Platform,
	isRunning func() bool,
	logDestPath string,
	logDestName string,
) tview.Primitive {
	if mainPageNotifyCancel != nil {
		mainPageNotifyCancel()
	}

	notifyCtx, notifyCancel := context.WithCancel(context.Background())
	mainPageNotifyCancel = notifyCancel

	main := tview.NewFlex()

	introText := tview.NewTextView().SetText(
		"Visit [::bu:https://zaparoo.org]zaparoo.org[::-:-] for guides and support.",
	).SetDynamicColors(true)
	statusText := tview.NewTextView().SetDynamicColors(true)

	svcRunning := isRunning()
	log.Debug().Bool("svcRunning", svcRunning).Msg("TUI: service status check")
	var svcStatus string
	if svcRunning {
		svcStatus = "RUNNING"
	} else {
		svcStatus = "NOT RUNNING\nThe Zaparoo Core service may not have started. Check Logs for more information."
	}

	ip := helpers.GetLocalIP()
	var ipDisplay string
	if ip == "" {
		ipDisplay = "Unknown"
	} else {
		ipDisplay = ip
	}

	webUI := fmt.Sprintf("http://%s:%d/app/", ip, cfg.APIPort())

	var readerStatus string
	if svcRunning {
		ctx, cancel := tuiContext()
		readers, err := getReaders(ctx, cfg)
		cancel()
		if err != nil {
			log.Error().Err(err).Msg("failed to get readers")
			readerStatus = "-"
		} else {
			readerStatus = formatReaderStatus(readers.Readers)
		}
	} else {
		readerStatus = "-"
	}

	updateStatusText := func(readerStatus string) {
		statusText.SetText(fmt.Sprintf(
			"[::b]Status:[::-]  %s\n[::b]Address:[::-] %s\n[::b]Web UI:[::-]  [:::%s]%s[:::-]\n"+
				"[::b]Readers:[::-] %s",
			svcStatus, ipDisplay, webUI, webUI, readerStatus,
		))
	}
	updateStatusText(readerStatus)

	helpText := tview.NewTextView()
	lastScanned := tview.NewTextView()
	lastScanned.SetDynamicColors(true)
	lastScanned.SetBorder(true).SetTitle("Last Scanned")

	if svcRunning {
		ctx, cancel := tuiContext()
		tokens, err := getTokens(ctx, cfg)
		cancel()
		switch {
		case err != nil:
			lastScanned.SetText("Error checking last scanned:\n" + err.Error())
		case tokens.Last != nil:
			lastScanned.SetText(fmt.Sprintf(
				"[::b]Time:[::-]  %s\n[::b]ID:[::-]    %s\n[::b]Value:[::-] %s",
				tokens.Last.ScanTime.Format("2006-01-02 15:04:05"),
				tokens.Last.UID,
				tokens.Last.Text,
			))
		default:
			lastScanned.SetText("[::b]Time:[::-]  -\n[::b]ID:[::-]    -\n[::b]Value:[::-] -")
		}

		go func() {
			log.Debug().Msg("starting notification listener")
			for {
				select {
				case <-notifyCtx.Done():
					log.Debug().Msg("notification listener cancelled")
					return
				default:
				}

				notifyType, resp, err := client.WaitNotifications(
					notifyCtx, -1, cfg,
					models.NotificationTokensAdded,
					models.NotificationReadersConnected,
					models.NotificationReadersDisconnected,
				)
				switch {
				case errors.Is(err, client.ErrRequestTimeout):
					continue
				case errors.Is(err, client.ErrRequestCancelled):
					log.Debug().Msg("notification listener: request cancelled")
					return
				case err != nil:
					log.Error().Err(err).Msg("notification listener error")
					return
				}

				log.Debug().Str("type", notifyType).Msg("received notification")

				switch notifyType {
				case models.NotificationTokensAdded:
					var token models.TokenResponse
					if err := json.Unmarshal([]byte(resp), &token); err != nil {
						log.Error().Err(err).Str("resp", resp).Msg("error unmarshalling token notification")
						continue
					}
					app.QueueUpdateDraw(func() {
						lastScanned.SetText(fmt.Sprintf(
							"[::b]Time:[::-]  %s\n[::b]ID:[::-]    %s\n[::b]Value:[::-] %s",
							token.ScanTime.Format("2006-01-02 15:04:05"),
							token.UID,
							token.Text,
						))
					})

				case models.NotificationReadersConnected, models.NotificationReadersDisconnected:
					ctx, cancel := tuiContext()
					readers, err := getReaders(ctx, cfg)
					cancel()
					if err != nil {
						log.Error().Err(err).Msg("failed to refresh reader status")
						continue
					}
					newStatus := formatReaderStatus(readers.Readers)
					app.QueueUpdateDraw(func() {
						updateStatusText(newStatus)
					})
				}
			}
		}()
	} else {
		lastScanned.SetText("[::b]Time:[::-]  -\n[::b]ID:[::-]    -\n[::b]Value:[::-] -")
	}

	displayCol := tview.NewFlex().SetDirection(tview.FlexRow)
	displayCol.AddItem(introText, 1, 1, false)
	displayCol.AddItem(statusText, 0, 1, false)
	displayCol.AddItem(lastScanned, 6, 1, false)
	displayCol.AddItem(helpText, 1, 1, false)

	main.SetTitle("Zaparoo Core v" + config.AppVersion + " (" + pl.ID() + ")").
		SetBorder(true).
		SetTitleAlign(tview.AlignCenter)

	main.AddItem(displayCol, 0, 1, false)

	searchButton := tview.NewButton("Search media").SetSelectedFunc(func() {
		BuildSearchMedia(cfg, pages, app)
	})
	searchButton.SetFocusFunc(func() {
		helpText.SetText("Search for media and write to an NFC tag.")
	})

	writeButton := tview.NewButton("Custom write").SetSelectedFunc(func() {
		BuildTagsWriteMenu(cfg, pages, app)
	})
	writeButton.SetFocusFunc(func() {
		helpText.SetText("Write custom ZapScript to an NFC tag.")
	})

	updateDBButton := tview.NewButton("Update media DB").SetSelectedFunc(func() {
		BuildGenerateDBPage(cfg, pages, app)
	})
	updateDBButton.SetFocusFunc(func() {
		helpText.SetText("Scan disk to create index of games.")
	})

	rebuildMainPage := func() {
		BuildMainPage(cfg, pages, app, pl, isRunning, logDestPath, logDestName)
	}

	settingsButton := tview.NewButton("Settings").SetSelectedFunc(func() {
		BuildSettingsMainMenu(cfg, pages, app, pl, rebuildMainPage)
	})
	settingsButton.SetFocusFunc(func() {
		helpText.SetText("Manage settings for Core service.")
	})

	exportButton := tview.NewButton("Logs").SetSelectedFunc(func() {
		BuildExportLogModal(pages, app, pl, logDestPath, logDestName)
	})
	exportButton.SetFocusFunc(func() {
		helpText.SetText("View and export Core log file.")
	})

	exitButton := tview.NewButton("Exit").SetSelectedFunc(func() {
		notifyCancel() // Cancel notification goroutine before exiting
		app.Stop()
	})
	exitButton.SetFocusFunc(func() {
		if svcRunning {
			helpText.SetText("Exit TUI app. (service will continue running)")
		} else {
			helpText.SetText("Exit TUI app.")
		}
	})

	if svcRunning {
		setupButtonNavigation(
			app,
			searchButton,
			writeButton,
			updateDBButton,
			settingsButton,
			exportButton,
			exitButton,
		)
	} else {
		setupButtonNavigation(
			app,
			exportButton,
			exitButton,
		)
		searchButton.SetDisabled(true)
		writeButton.SetDisabled(true)
		updateDBButton.SetDisabled(true)
		settingsButton.SetDisabled(true)
	}

	main.AddItem(tview.NewTextView(), 1, 1, false)

	buttonNav := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(tview.NewTextView(), 0, 1, false).
		AddItem(searchButton, 1, 1, svcRunning).
		AddItem(tview.NewTextView(), 1, 1, false).
		AddItem(writeButton, 1, 1, false).
		AddItem(tview.NewTextView(), 1, 1, false).
		AddItem(updateDBButton, 1, 1, false).
		AddItem(tview.NewTextView(), 1, 1, false).
		AddItem(settingsButton, 1, 1, false).
		AddItem(tview.NewTextView(), 1, 1, false).
		AddItem(exportButton, 1, 1, false).
		AddItem(tview.NewTextView(), 1, 1, false).
		AddItem(exitButton, 1, 1, !svcRunning).
		AddItem(tview.NewTextView(), 0, 1, false)
	main.AddItem(buttonNav, 20, 1, true)

	pageDefaults(PageMain, pages, main)
	return main
}

func BuildMain(
	cfg *config.Instance,
	pl platforms.Platform,
	isRunning func() bool,
	logDestPath string,
	logDestName string,
) (*tview.Application, error) {
	if err := config.LoadTUIConfig(helpers.ConfigDir(pl), pl.ID()); err != nil {
		log.Warn().Err(err).Msg("failed to load TUI config, using defaults")
	}

	tuiCfg := config.GetTUIConfig()
	if !SetCurrentTheme(tuiCfg.Theme) {
		log.Warn().Str("theme", tuiCfg.Theme).Msg("unknown theme, using default")
		SetCurrentTheme("default")
	}

	app := tview.NewApplication()
	app.EnableMouse(tuiCfg.Mouse)

	pages := tview.NewPages()
	BuildMainPage(cfg, pages, app, pl, isRunning, logDestPath, logDestName)

	var rootWidget tview.Primitive
	if tuiCfg.CRTMode {
		rootWidget = CenterWidget(75, 15, pages)
	} else {
		rootWidget = ResponsiveMaxWidget(DefaultMaxWidth, DefaultMaxHeight, pages)
	}
	return app.SetRoot(rootWidget, true), nil
}
