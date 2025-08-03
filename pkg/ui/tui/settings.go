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

package tui

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/database/systemdefs"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
)

func BuildSettingsMainMenu(cfg *config.Instance, pages *tview.Pages, app *tview.Application) *tview.List {
	debugLabel := func() string {
		debugLogging := "Enable"
		if cfg.DebugLogging() {
			debugLogging = "Disable"
		}
		return debugLogging
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
		cfg.SetDebugLogging(!cfg.DebugLogging())
		mainMenu.SetItemText(3, "Debug", debugLabel()+" debug logging mode")
	})

	mainMenu.AddItem("Save", "Save changes", 's', func() {
		err := cfg.Save()
		if err != nil {
			log.Error().Err(err).Msg("error saving config")
		}
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
	audioFeedback := " "
	if cfg.AudioFeedback() {
		audioFeedback = "X"
	}

	audioMenu := tview.NewList().
		AddItem("["+audioFeedback+"] Audio feedback", "Enable or disable the audio notification on scan", '1', func() {
			cfg.SetAudioFeedback(!cfg.AudioFeedback())
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
	autoDetect := cfg.AutoDetect()

	connectionStrings := make([]string, 0, len(cfg.Readers().Connect))
	for _, item := range cfg.Readers().Connect {
		connectionStrings = append(connectionStrings, item.Driver+":"+item.Path)
	}

	textArea := tview.NewTextArea().
		SetLabel("Connection strings (1 per line)").
		SetText(strings.Join(connectionStrings, "\n"), false).
		SetSize(5, 40).
		SetMaxLength(200)

	readersMenu := tview.NewForm()
	readersMenu.AddCheckbox("Auto-detect readers", autoDetect, func(checked bool) {
		cfg.SetAutoDetect(checked)
	}).
		AddFormItem(textArea).
		AddButton("Go back", func() {
			var newConnect []config.ReadersConnect
			connStrings := strings.Split(textArea.GetText(), "\n")
			for _, item := range connStrings {
				couple := strings.SplitN(item, ":", 2)
				if len(couple) == 2 {
					newConnect = append(newConnect, config.ReadersConnect{Driver: couple[0], Path: couple[1]})
				}
			}

			cfg.SetReaderConnections(newConnect)
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
	scanMode := 0
	if cfg.ReadersScan().Mode == config.ScanModeHold {
		scanMode = 1
	}

	scanModes := []string{"Tap", "Hold"}

	allSystems := []string{""}
	for _, item := range systemdefs.AllSystems() {
		allSystems = append(allSystems, item.ID)
	}

	exitDelay := cfg.ReadersScan().ExitDelay

	scanMenu := tview.NewForm()
	scanMenu.AddDropDown("Scan mode", scanModes, scanMode, func(option string, _ int) {
		cfg.SetScanMode(strings.ToLower(option))
	}).
		AddInputField("Exit delay", strconv.FormatFloat(float64(exitDelay), 'f', 0, 32), 2,
			tview.InputFieldInteger, func(value string) {
				delay, _ := strconv.ParseFloat(value, 32)
				cfg.SetScanExitDelay(float32(delay))
			}).
		AddDropDown("Ignore systems", allSystems, 0, func(option string, optionIndex int) {
			currentSystems := cfg.ReadersScan().IgnoreSystem
			if optionIndex > 0 {
				if !slices.Contains(currentSystems, option) {
					currentSystems = append(currentSystems, option)
					cfg.SetScanIgnoreSystem(currentSystems)
				} else {
					index := slices.Index(currentSystems, option)
					newSystems := slices.Delete(currentSystems, index, index+1)
					cfg.SetScanIgnoreSystem(newSystems)
				}
				BuildScanModeMenu(cfg, pages, app)
				scanMenu.SetFocus(scanMenu.GetFormItemIndex("Ignore systems"))
			}
		}).
		AddTextView("Ignored system list", strings.Join(cfg.ReadersScan().IgnoreSystem, ", "), 30, 2, false, false).
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
