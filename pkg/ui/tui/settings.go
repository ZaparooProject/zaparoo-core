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
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
)

// getSettings fetches current settings from the API.
func getSettings(ctx context.Context, cfg *config.Instance) (*models.SettingsResponse, error) {
	resp, err := client.LocalClient(ctx, cfg, models.MethodSettings, "")
	if err != nil {
		return nil, fmt.Errorf("failed to get settings: %w", err)
	}
	var settings models.SettingsResponse
	if err := json.Unmarshal([]byte(resp), &settings); err != nil {
		return nil, fmt.Errorf("failed to parse settings: %w", err)
	}
	return &settings, nil
}

// updateSettings sends a settings update to the API.
func updateSettings(ctx context.Context, cfg *config.Instance, params models.UpdateSettingsParams) error {
	data, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("failed to marshal params: %w", err)
	}
	_, err = client.LocalClient(ctx, cfg, models.MethodSettingsUpdate, string(data))
	if err != nil {
		return fmt.Errorf("failed to update settings: %w", err)
	}
	return nil
}

func BuildSettingsMainMenu(cfg *config.Instance, pages *tview.Pages, app *tview.Application) *tview.List {
	// Fetch current settings from API
	settings, err := getSettings(context.Background(), cfg)
	if err != nil {
		log.Error().Err(err).Msg("error fetching settings")
		settings = &models.SettingsResponse{}
	}

	debugLogging := settings.DebugLogging

	debugLabel := func() string {
		if debugLogging {
			return "Disable"
		}
		return "Enable"
	}

	mainMenu := tview.NewList().
		AddItem("Scanning", "Change reader scan behavior", '1', func() {
			BuildScanModeMenu(cfg, pages, app)
		}).
		AddItem("Readers", "Manage connected readers", '2', func() {
			BuildReadersMenu(cfg, pages, app)
		}).
		AddItem("Audio", "Set audio options", '3', func() {
			BuildAudioMenu(cfg, pages, app)
		})

	mainMenu.AddItem("Debug", debugLabel()+" debug logging mode", '4', func() {
		newValue := !debugLogging
		err := updateSettings(context.Background(), cfg, models.UpdateSettingsParams{
			DebugLogging: &newValue,
		})
		if err != nil {
			log.Error().Err(err).Msg("error updating debug logging")
			return
		}
		debugLogging = newValue
		mainMenu.SetItemText(3, "Debug", debugLabel()+" debug logging mode")
	})

	mainMenu.AddItem("Go back", "Back to main menu", 'b', func() {
		pages.SwitchToPage(PageMain)
	})

	mainMenu.SetTitle("Settings")
	mainMenu.SetSecondaryTextColor(tcell.ColorYellow)

	mainMenu.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			pages.SwitchToPage(PageMain)
		}
		return event
	})

	pageDefaults(PageSettingsMain, pages, mainMenu)
	return mainMenu
}

func BuildTagsReadMenu(cfg *config.Instance, pages *tview.Pages, app *tview.Application) *tview.Form {
	topTextView := tview.NewTextView().
		SetLabel("").
		SetText("Press Enter to scan a card, Esc to Exit")

	tagsReadMenu := tview.NewForm().
		AddFormItem(topTextView)
	tagsReadMenu.SetTitle("Settings - NFC Tags - Read")

	tagsReadMenu.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		k := event.Key()
		if k == tcell.KeyEnter {
			// remove all the previous text if any. Add back the instructions
			tagsReadMenu.Clear(false).AddFormItem(topTextView)
			topTextView.SetText("Tap a card to read content")
			// if we don't force a redraw, the waitNotification will keep the thread busy
			// and the app won't update the screen
			app.ForceDraw()
			resp, _ := client.WaitNotification(
				context.Background(), 0,
				cfg, models.NotificationTokensAdded,
			)
			var data models.TokenResponse
			err := json.Unmarshal([]byte(resp), &data)
			if err != nil {
				log.Error().Err(err).Msg("error unmarshalling token")
				return nil
			}
			tagsReadMenu.AddTextView("ID", data.UID, 50, 1, true, false)
			tagsReadMenu.AddTextView("Data", data.Data, 50, 1, true, false)
			tagsReadMenu.AddTextView("Value", data.Text, 50, 4, true, false)
			topTextView.SetText("Press ENTER to scan another card. ESC to exit")
		}
		if k == tcell.KeyEscape {
			pages.SwitchToPage(PageSettingsMain)
		}
		return event
	})

	pageDefaults(PageSettingsTagsRead, pages, tagsReadMenu)
	return tagsReadMenu
}

func BuildAudioMenu(cfg *config.Instance, pages *tview.Pages, app *tview.Application) *tview.List {
	// Fetch current settings from API
	settings, err := getSettings(context.Background(), cfg)
	if err != nil {
		log.Error().Err(err).Msg("error fetching settings")
		settings = &models.SettingsResponse{}
	}

	audioFeedback := settings.AudioScanFeedback

	checkMark := func() string {
		if audioFeedback {
			return "X"
		}
		return " "
	}

	audioMenu := tview.NewList().
		AddItem("["+checkMark()+"] Audio feedback", "Enable or disable the audio notification on scan", '1', func() {
			newValue := !audioFeedback
			err := updateSettings(context.Background(), cfg, models.UpdateSettingsParams{
				AudioScanFeedback: &newValue,
			})
			if err != nil {
				log.Error().Err(err).Msg("error updating audio feedback")
				return
			}
			BuildAudioMenu(cfg, pages, app)
		}).
		AddItem("Go back", "Back to main menu", 'b', func() {
			pages.SwitchToPage(PageSettingsMain)
		})

	audioMenu.SetTitle("Settings - Audio")
	audioMenu.SetSecondaryTextColor(tcell.ColorYellow)

	audioMenu.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			pages.SwitchToPage(PageSettingsMain)
		}
		return event
	})

	pageDefaults(PageSettingsAudio, pages, audioMenu)
	return audioMenu
}

func BuildReadersMenu(cfg *config.Instance, pages *tview.Pages, _ *tview.Application) *tview.Form {
	// Fetch current settings from API
	settings, err := getSettings(context.Background(), cfg)
	if err != nil {
		log.Error().Err(err).Msg("error fetching settings")
		settings = &models.SettingsResponse{}
	}

	connectionStrings := make([]string, 0, len(settings.ReadersConnect))
	for _, item := range settings.ReadersConnect {
		connectionStrings = append(connectionStrings, item.Driver+":"+item.Path)
	}

	textArea := tview.NewTextArea().
		SetLabel("Connection strings (1 per line)").
		SetText(strings.Join(connectionStrings, "\n"), false).
		SetSize(5, 40).
		SetMaxLength(200)

	readersMenu := tview.NewForm()
	readersMenu.AddCheckbox("Auto-detect readers", settings.ReadersAutoDetect, func(checked bool) {
		err := updateSettings(context.Background(), cfg, models.UpdateSettingsParams{
			ReadersAutoDetect: &checked,
		})
		if err != nil {
			log.Error().Err(err).Msg("error updating auto-detect")
		}
	}).
		AddFormItem(textArea).
		AddButton("Go back", func() {
			// Parse and save reader connections via API
			var newConnect []models.ReaderConnection
			connStrings := strings.Split(textArea.GetText(), "\n")
			for _, item := range connStrings {
				couple := strings.SplitN(item, ":", 2)
				if len(couple) == 2 {
					newConnect = append(newConnect, models.ReaderConnection{
						Driver: couple[0],
						Path:   couple[1],
					})
				}
			}

			err := updateSettings(context.Background(), cfg, models.UpdateSettingsParams{
				ReadersConnect: &newConnect,
			})
			if err != nil {
				log.Error().Err(err).Msg("error updating reader connections")
			}
			pages.SwitchToPage(PageSettingsMain)
		})

	readersMenu.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			pages.SwitchToPage(PageSettingsMain)
		}
		return event
	})

	readersMenu.SetTitle("Settings - Readers")

	pageDefaults(PageSettingsReaders, pages, readersMenu)
	return readersMenu
}

func BuildScanModeMenu(cfg *config.Instance, pages *tview.Pages, app *tview.Application) *tview.Form {
	// Fetch current settings from API
	settings, err := getSettings(context.Background(), cfg)
	if err != nil {
		log.Error().Err(err).Msg("error fetching settings")
		settings = &models.SettingsResponse{}
	}

	scanMode := 0
	if settings.ReadersScanMode == config.ScanModeHold {
		scanMode = 1
	}

	scanModes := []string{"Tap", "Hold"}

	systems := systemdefs.AllSystems()
	allSystems := make([]string, 0, len(systems)+1)
	allSystems = append(allSystems, "")
	for _, item := range systems {
		allSystems = append(allSystems, item.ID)
	}

	// Local copy of ignored systems for UI updates
	ignoredSystems := make([]string, 0, len(settings.ReadersScanIgnoreSystem))
	ignoredSystems = append(ignoredSystems, settings.ReadersScanIgnoreSystem...)

	scanMenu := tview.NewForm()
	scanMenu.AddDropDown("Scan mode", scanModes, scanMode, func(option string, _ int) {
		mode := strings.ToLower(option)
		err := updateSettings(context.Background(), cfg, models.UpdateSettingsParams{
			ReadersScanMode: &mode,
		})
		if err != nil {
			log.Error().Err(err).Msg("error updating scan mode")
		}
	}).
		AddInputField("Exit delay", strconv.FormatFloat(float64(settings.ReadersScanExitDelay), 'f', 0, 32), 2,
			tview.InputFieldInteger, func(value string) {
				delay, _ := strconv.ParseFloat(value, 32)
				delayFloat := float32(delay)
				err := updateSettings(context.Background(), cfg, models.UpdateSettingsParams{
					ReadersScanExitDelay: &delayFloat,
				})
				if err != nil {
					log.Error().Err(err).Msg("error updating exit delay")
				}
			}).
		AddDropDown("Ignore systems", allSystems, 0, func(option string, optionIndex int) {
			if optionIndex > 0 {
				if !slices.Contains(ignoredSystems, option) {
					ignoredSystems = append(ignoredSystems, option)
				} else {
					index := slices.Index(ignoredSystems, option)
					ignoredSystems = slices.Delete(ignoredSystems, index, index+1)
				}
				err := updateSettings(context.Background(), cfg, models.UpdateSettingsParams{
					ReadersScanIgnoreSystem: &ignoredSystems,
				})
				if err != nil {
					log.Error().Err(err).Msg("error updating ignored systems")
				}
				BuildScanModeMenu(cfg, pages, app)
				scanMenu.SetFocus(scanMenu.GetFormItemIndex("Ignore systems"))
			}
		}).
		AddTextView("Ignored system list", strings.Join(ignoredSystems, ", "), 30, 2, false, false).
		AddButton("Go back", func() {
			pages.SwitchToPage(PageSettingsMain)
		})

	scanMenu.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			pages.SwitchToPage(PageSettingsMain)
		}
		return event
	})

	scanMenu.SetTitle("Settings - Scanning")

	pageDefaults(PageSettingsScanMode, pages, scanMenu)
	return scanMenu
}
