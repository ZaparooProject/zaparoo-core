// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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
	"strconv"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
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

// getSystems fetches available systems from the API.
func getSystems(ctx context.Context, cfg *config.Instance) ([]models.System, error) {
	resp, err := client.LocalClient(ctx, cfg, models.MethodSystems, "")
	if err != nil {
		return nil, fmt.Errorf("failed to get systems: %w", err)
	}
	var systems models.SystemsResponse
	if err := json.Unmarshal([]byte(resp), &systems); err != nil {
		return nil, fmt.Errorf("failed to parse systems: %w", err)
	}
	return systems.Systems, nil
}

// BuildSettingsMainMenu creates the top-level settings menu with Audio, Readers, and Advanced options.
func BuildSettingsMainMenu(cfg *config.Instance, pages *tview.Pages, app *tview.Application) *tview.List {
	mainMenu := NewSettingsList(pages, PageMain)
	mainMenu.SetTitle("Settings")

	mainMenu.
		AddAction("Readers", "Reader connections and scanning", func() {
			BuildReadersSettingsMenu(cfg, pages, app)
		}).
		AddAction("Audio", "Sound and feedback settings", func() {
			BuildAudioSettingsMenu(cfg, pages, app)
		}).
		AddAction("Advanced", "Debug and system options", func() {
			BuildAdvancedSettingsMenu(cfg, pages, app)
		}).
		AddBackWithDesc("Back to main menu")

	pageDefaults(PageSettingsMain, pages, mainMenu.List)
	return mainMenu.List
}

// BuildAudioSettingsMenu creates the audio settings submenu.
func BuildAudioSettingsMenu(cfg *config.Instance, pages *tview.Pages, app *tview.Application) *tview.List {
	settings, err := getSettings(context.Background(), cfg)
	if err != nil {
		log.Error().Err(err).Msg("error fetching settings")
		settings = &models.SettingsResponse{}
	}

	audioFeedback := settings.AudioScanFeedback

	menu := NewSettingsList(pages, PageSettingsMain)
	menu.SetTitle("Settings - Audio")

	menu.AddToggle("Audio feedback on scan", "Play sound when token is scanned", &audioFeedback, func(value bool) {
		err := updateSettings(context.Background(), cfg, models.UpdateSettingsParams{
			AudioScanFeedback: &value,
		})
		if err != nil {
			log.Error().Err(err).Msg("error updating audio feedback")
			showErrorModal(pages, app, "Failed to save audio settings")
		}
	})

	menu.AddBack()

	pageDefaults(PageSettingsAudioMenu, pages, menu.List)
	return menu.List
}

// Exit delay options with seconds suffix for display.
var exitDelayOptions = []string{
	"0 seconds", "1 second", "2 seconds", "3 seconds", "5 seconds",
	"10 seconds", "15 seconds", "20 seconds", "30 seconds",
}

// BuildReadersSettingsMenu creates the readers settings submenu.
func BuildReadersSettingsMenu(cfg *config.Instance, pages *tview.Pages, app *tview.Application) *tview.List {
	settings, err := getSettings(context.Background(), cfg)
	if err != nil {
		log.Error().Err(err).Msg("error fetching settings")
		settings = &models.SettingsResponse{}
	}

	autoDetect := settings.ReadersAutoDetect

	scanModeOptions := []string{"Tap", "Hold"}
	scanModeIndex := 0
	if settings.ReadersScanMode == config.ScanModeHold {
		scanModeIndex = 1
	}

	// Find current exit delay index by matching the number prefix
	exitDelayIndex := 0
	currentDelay := strconv.Itoa(int(settings.ReadersScanExitDelay))
	for i, opt := range exitDelayOptions {
		if strings.HasPrefix(opt, currentDelay+" ") {
			exitDelayIndex = i
			break
		}
	}

	menu := NewSettingsList(pages, PageSettingsMain)
	menu.SetTitle("Settings - Readers")

	scanModeIdx := menu.GetItemCount()
	scanModeDesc := "Tap: tap to launch, Hold: exits when removed"
	menu.AddCycle("Scan mode", scanModeDesc, scanModeOptions, &scanModeIndex, func(option string, _ int) {
		mode := strings.ToLower(option)
		err := updateSettings(context.Background(), cfg, models.UpdateSettingsParams{
			ReadersScanMode: &mode,
		})
		if err != nil {
			log.Error().Err(err).Msg("error updating scan mode")
			showErrorModal(pages, app, "Failed to save scan mode")
		}
	})

	menu.AddToggle("Auto-detect readers", "Automatically find connected readers", &autoDetect, func(value bool) {
		err := updateSettings(context.Background(), cfg, models.UpdateSettingsParams{
			ReadersAutoDetect: &value,
		})
		if err != nil {
			log.Error().Err(err).Msg("error updating auto-detect")
			showErrorModal(pages, app, "Failed to save auto-detect setting")
		}
	})

	exitDelayIdx := menu.GetItemCount()
	exitDelayDesc := "Time to wait before exiting in Hold mode"
	menu.AddCycle("Exit delay", exitDelayDesc, exitDelayOptions, &exitDelayIndex, func(option string, _ int) {
		// Parse just the number from "X seconds"
		numStr := strings.Split(option, " ")[0]
		delay, err := strconv.ParseFloat(numStr, 32)
		if err != nil {
			log.Error().Err(err).Str("value", numStr).Msg("failed to parse exit delay")
			return
		}
		delayF := float32(delay)
		err = updateSettings(context.Background(), cfg, models.UpdateSettingsParams{
			ReadersScanExitDelay: &delayF,
		})
		if err != nil {
			log.Error().Err(err).Msg("error updating exit delay")
			showErrorModal(pages, app, "Failed to save exit delay")
		}
	})

	menu.AddAction("Manage readers", "Add, edit, or remove manual reader connections", func() {
		BuildReaderListPage(cfg, pages, app)
	})

	menu.AddBack()

	// Set up Left/Right key handling for cycles only
	cycleIndices := map[int]func(delta int){
		scanModeIdx: func(delta int) {
			scanModeIndex = (scanModeIndex + delta + len(scanModeOptions)) % len(scanModeOptions)
			mode := strings.ToLower(scanModeOptions[scanModeIndex])
			err := updateSettings(context.Background(), cfg, models.UpdateSettingsParams{
				ReadersScanMode: &mode,
			})
			if err != nil {
				log.Error().Err(err).Msg("error updating scan mode")
				showErrorModal(pages, app, "Failed to save scan mode")
			}
			menu.refreshAllItems(menu.GetCurrentItem())
		},
		exitDelayIdx: func(delta int) {
			exitDelayIndex = (exitDelayIndex + delta + len(exitDelayOptions)) % len(exitDelayOptions)
			// Parse just the number from "X seconds"
			numStr := strings.Split(exitDelayOptions[exitDelayIndex], " ")[0]
			delay, err := strconv.ParseFloat(numStr, 32)
			if err != nil {
				log.Error().Err(err).Str("value", numStr).Msg("failed to parse exit delay")
				return
			}
			delayF := float32(delay)
			err = updateSettings(context.Background(), cfg, models.UpdateSettingsParams{
				ReadersScanExitDelay: &delayF,
			})
			if err != nil {
				log.Error().Err(err).Msg("error updating exit delay")
				showErrorModal(pages, app, "Failed to save exit delay")
			}
			menu.refreshAllItems(menu.GetCurrentItem())
		},
	}

	menu.SetupCycleKeys(cycleIndices)

	pageDefaults(PageSettingsReadersMenu, pages, menu.List)
	return menu.List
}

// BuildAdvancedSettingsMenu creates the advanced settings menu.
func BuildAdvancedSettingsMenu(cfg *config.Instance, pages *tview.Pages, app *tview.Application) *tview.List {
	settings, err := getSettings(context.Background(), cfg)
	if err != nil {
		log.Error().Err(err).Msg("error fetching settings")
		settings = &models.SettingsResponse{}
	}

	debugLogging := settings.DebugLogging

	// Build ignore systems label with count indicator
	ignoreLabel := "Ignore systems"
	ignoreCount := len(settings.ReadersScanIgnoreSystem)
	if ignoreCount > 0 {
		ignoreLabel = fmt.Sprintf("Ignore systems (%d selected)", ignoreCount)
	}

	menu := NewSettingsList(pages, PageSettingsMain)
	menu.SetTitle("Settings - Advanced")

	menu.AddAction(ignoreLabel, "Systems to ignore exiting in Hold mode", func() {
		BuildIgnoreSystemsPage(cfg, pages, app)
	})

	menu.AddToggle("Debug logging", "Enable verbose debug output", &debugLogging, func(value bool) {
		err := updateSettings(context.Background(), cfg, models.UpdateSettingsParams{
			DebugLogging: &value,
		})
		if err != nil {
			log.Error().Err(err).Msg("error updating debug logging")
			showErrorModal(pages, app, "Failed to save debug logging setting")
		}
	})

	menu.AddBack()

	pageDefaults(PageSettingsAdvanced, pages, menu.List)
	return menu.List
}

// BuildReaderListPage creates the reader list management page.
func BuildReaderListPage(cfg *config.Instance, pages *tview.Pages, app *tview.Application) tview.Primitive {
	settings, err := getSettings(context.Background(), cfg)
	if err != nil {
		log.Error().Err(err).Msg("error fetching settings")
		settings = &models.SettingsResponse{}
	}

	readers := settings.ReadersConnect

	// Create the main layout
	layout := tview.NewFlex().SetDirection(tview.FlexRow)
	layout.SetTitle("Manage Readers")
	layout.SetBorder(true)

	// Reader list
	readerList := tview.NewList()
	readerList.SetSecondaryTextColor(tcell.ColorYellow)
	readerList.ShowSecondaryText(true)

	refreshList := func() {
		readerList.Clear()
		for i, reader := range readers {
			idx := i
			display := reader.Driver
			if reader.Path != "" {
				display += ":" + reader.Path
			}
			secondary := ""
			if reader.IDSource != "" {
				secondary = "ID Source: " + reader.IDSource
			}
			readerList.AddItem(display, secondary, 0, func() {
				BuildReaderEditPage(cfg, pages, app, &readers, idx)
			})
		}
		if len(readers) == 0 {
			readerList.AddItem("(no readers configured)", "Press Add to create one", 0, nil)
		}
	}
	refreshList()

	// Button bar
	buttonBar := NewButtonBar(app)

	buttonBar.AddButton("Add", func() {
		// Add a new empty reader
		readers = append(readers, models.ReaderConnection{Driver: "pn532"})
		BuildReaderEditPage(cfg, pages, app, &readers, len(readers)-1)
	})

	buttonBar.AddButton("Delete", func() {
		if len(readers) == 0 {
			return
		}
		idx := readerList.GetCurrentItem()
		if idx >= 0 && idx < len(readers) {
			readers = append(readers[:idx], readers[idx+1:]...)
			err := updateSettings(context.Background(), cfg, models.UpdateSettingsParams{
				ReadersConnect: &readers,
			})
			if err != nil {
				log.Error().Err(err).Msg("error deleting reader")
				showErrorModal(pages, app, "Failed to delete reader")
			}
			refreshList()
		}
	})

	buttonBar.AddButton("Back", func() {
		pages.SwitchToPage(PageSettingsReadersMenu)
	})

	buttonBar.SetupNavigation(func() {
		pages.SwitchToPage(PageSettingsReadersMenu)
	})

	// Navigation between list and buttons
	inButtonBar := false

	readerList.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEscape:
			pages.SwitchToPage(PageSettingsReadersMenu)
			return nil
		case tcell.KeyTab, tcell.KeyDown:
			if readerList.GetCurrentItem() == readerList.GetItemCount()-1 || event.Key() == tcell.KeyTab {
				inButtonBar = true
				app.SetFocus(buttonBar.GetFirstButton())
				return nil
			}
		default:
			// Let other keys pass through
		}
		return event
	})

	// Allow going back to list from button bar
	for _, btn := range buttonBar.buttons {
		originalCapture := btn.GetInputCapture()
		btn.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
			if event.Key() == tcell.KeyUp || event.Key() == tcell.KeyBacktab {
				if inButtonBar {
					inButtonBar = false
					app.SetFocus(readerList)
					return nil
				}
			}
			if originalCapture != nil {
				return originalCapture(event)
			}
			return event
		})
	}

	layout.AddItem(readerList, 0, 1, true)
	layout.AddItem(buttonBar.Flex, 1, 0, false)

	pageDefaults(PageSettingsReaderList, pages, layout)
	return layout
}

// BuildReaderEditPage creates the reader edit form.
func BuildReaderEditPage(
	cfg *config.Instance,
	pages *tview.Pages,
	app *tview.Application,
	readers *[]models.ReaderConnection,
	index int,
) tview.Primitive {
	isNew := index >= len(*readers)
	var reader models.ReaderConnection
	if !isNew {
		reader = (*readers)[index]
	} else {
		reader = models.ReaderConnection{Driver: "pn532"}
	}

	layout := tview.NewFlex().SetDirection(tview.FlexRow)
	if isNew {
		layout.SetTitle("Add Reader")
	} else {
		layout.SetTitle("Edit Reader")
	}
	layout.SetBorder(true)

	// Driver selector (as a list for easy cycling)
	driverIndex := 0
	for i, d := range AvailableDrivers {
		if d == reader.Driver {
			driverIndex = i
			break
		}
	}

	driverDisplay := tview.NewTextView().SetDynamicColors(true)
	updateDriverDisplay := func() {
		driverDisplay.SetText(fmt.Sprintf("[yellow]Driver:[white] < %s >", AvailableDrivers[driverIndex]))
	}
	updateDriverDisplay()

	pathInput := tview.NewInputField().
		SetLabel("Path: ").
		SetText(reader.Path).
		SetFieldWidth(30)

	idSourceInput := tview.NewInputField().
		SetLabel("ID Source: ").
		SetText(reader.IDSource).
		SetFieldWidth(20)

	// Button bar
	buttonBar := NewButtonBar(app)

	buttonBar.AddButton("Save", func() {
		reader.Driver = AvailableDrivers[driverIndex]
		reader.Path = pathInput.GetText()
		reader.IDSource = idSourceInput.GetText()

		if isNew || index >= len(*readers) {
			*readers = append(*readers, reader)
		} else {
			(*readers)[index] = reader
		}

		err := updateSettings(context.Background(), cfg, models.UpdateSettingsParams{
			ReadersConnect: readers,
		})
		if err != nil {
			log.Error().Err(err).Msg("error saving reader")
			showErrorModal(pages, app, "Failed to save reader")
			return
		}
		BuildReaderListPage(cfg, pages, app)
	})

	buttonBar.AddButton("Cancel", func() {
		// If we added a new reader but cancelled, remove it
		if isNew && index < len(*readers) {
			*readers = (*readers)[:index]
		}
		BuildReaderListPage(cfg, pages, app)
	})

	buttonBar.SetupNavigation(func() {
		if isNew && index < len(*readers) {
			*readers = (*readers)[:index]
		}
		BuildReaderListPage(cfg, pages, app)
	})

	// Focus navigation order: driver -> path -> idSource -> buttons
	focusOrder := []tview.Primitive{driverDisplay, pathInput, idSourceInput, buttonBar.GetFirstButton()}

	setFocus := func(idx int) {
		if idx < 0 {
			idx = len(focusOrder) - 1
		} else if idx >= len(focusOrder) {
			idx = 0
		}
		app.SetFocus(focusOrder[idx])
	}

	driverDisplay.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyLeft:
			driverIndex = (driverIndex - 1 + len(AvailableDrivers)) % len(AvailableDrivers)
			updateDriverDisplay()
			return nil
		case tcell.KeyRight:
			driverIndex = (driverIndex + 1) % len(AvailableDrivers)
			updateDriverDisplay()
			return nil
		case tcell.KeyDown, tcell.KeyEnter, tcell.KeyTab:
			setFocus(1)
			return nil
		case tcell.KeyEscape:
			if isNew && index < len(*readers) {
				*readers = (*readers)[:index]
			}
			BuildReaderListPage(cfg, pages, app)
			return nil
		default:
			return event
		}
	})

	pathInput.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyUp, tcell.KeyBacktab:
			setFocus(0)
			return nil
		case tcell.KeyDown, tcell.KeyTab:
			setFocus(2)
			return nil
		case tcell.KeyEscape:
			if isNew && index < len(*readers) {
				*readers = (*readers)[:index]
			}
			BuildReaderListPage(cfg, pages, app)
			return nil
		default:
			return event
		}
	})

	idSourceInput.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyUp, tcell.KeyBacktab:
			setFocus(1)
			return nil
		case tcell.KeyDown, tcell.KeyTab:
			setFocus(3)
			return nil
		case tcell.KeyEscape:
			if isNew && index < len(*readers) {
				*readers = (*readers)[:index]
			}
			BuildReaderListPage(cfg, pages, app)
			return nil
		default:
			return event
		}
	})

	// Update button bar to go back to inputs on Up
	for _, btn := range buttonBar.buttons {
		originalCapture := btn.GetInputCapture()
		btn.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
			if event.Key() == tcell.KeyUp || event.Key() == tcell.KeyBacktab {
				setFocus(2)
				return nil
			}
			if originalCapture != nil {
				return originalCapture(event)
			}
			return event
		})
	}

	layout.AddItem(driverDisplay, 1, 0, true)
	layout.AddItem(pathInput, 1, 0, false)
	layout.AddItem(idSourceInput, 1, 0, false)
	layout.AddItem(tview.NewBox(), 0, 1, false) // spacer
	layout.AddItem(buttonBar.Flex, 1, 0, false)

	pageDefaults(PageSettingsReaderEdit, pages, layout)
	app.SetFocus(driverDisplay)
	return layout
}

// BuildIgnoreSystemsPage creates the ignore systems multi-select page.
func BuildIgnoreSystemsPage(cfg *config.Instance, pages *tview.Pages, app *tview.Application) tview.Primitive {
	settings, err := getSettings(context.Background(), cfg)
	if err != nil {
		log.Error().Err(err).Msg("error fetching settings")
		settings = &models.SettingsResponse{}
	}

	// Get systems from API (filtered for platform)
	systems, err := getSystems(context.Background(), cfg)
	if err != nil {
		log.Error().Err(err).Msg("error fetching systems")
		systems = []models.System{}
	}

	// Build items with display name and system ID
	checkItems := make([]CheckListItem, len(systems))
	for i, sys := range systems {
		label := sys.Name
		if label == "" {
			label = sys.ID
		}
		checkItems[i] = CheckListItem{Label: label, Value: sys.ID}
	}

	layout := tview.NewFlex().SetDirection(tview.FlexRow)
	layout.SetTitle("Ignore Systems")
	layout.SetBorder(true)

	// Create checklist - no onChange callback (save on Done instead)
	checkList := NewCheckListWithValues(checkItems, settings.ReadersScanIgnoreSystem, nil)

	// Done button saves and navigates back
	doneBtn := tview.NewButton("Done").SetSelectedFunc(func() {
		selected := checkList.GetSelected()
		err := updateSettings(context.Background(), cfg, models.UpdateSettingsParams{
			ReadersScanIgnoreSystem: &selected,
		})
		if err != nil {
			log.Error().Err(err).Msg("error updating ignored systems")
			showErrorModal(pages, app, "Failed to save ignored systems")
			return
		}
		pages.SwitchToPage(PageSettingsAdvanced)
	})

	updateDoneLabel := func(count int) {
		if count > 0 {
			doneBtn.SetLabel(fmt.Sprintf("Done (%d selected)", count))
		} else {
			doneBtn.SetLabel("Done")
		}
	}

	// Sync button label with selection count
	checkList.SetSelectionSyncFunc(updateDoneLabel)
	updateDoneLabel(checkList.GetSelectedCount())

	checkList.SetupNavigation(pages, PageSettingsAdvanced)

	checkList.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEscape:
			pages.SwitchToPage(PageSettingsAdvanced)
			return nil
		case tcell.KeyTab, tcell.KeyLeft, tcell.KeyRight:
			app.SetFocus(doneBtn)
			return nil
		case tcell.KeyDown:
			// If at last item, navigate to Done button
			if checkList.GetCurrentItem() == checkList.GetItemCount()-1 {
				app.SetFocus(doneBtn)
				return nil
			}
		default:
			// Let other keys pass through
		}
		return event
	})

	doneBtn.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyUp, tcell.KeyBacktab:
			app.SetFocus(checkList)
			return nil
		case tcell.KeyEscape:
			pages.SwitchToPage(PageSettingsAdvanced)
			return nil
		default:
			return event
		}
	})

	layout.AddItem(checkList, 0, 1, true)
	layout.AddItem(doneBtn, 1, 0, false)

	pageDefaults(PageSettingsIgnoreSystems, pages, layout)
	return layout
}

// BuildTagsReadMenu creates the NFC tag read menu (unchanged from original).
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
			tagsReadMenu.Clear(false).AddFormItem(topTextView)
			topTextView.SetText("Tap a card to read content")
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
