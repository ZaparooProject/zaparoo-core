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
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
)

// BuildSettingsMainMenu creates the top-level settings menu with Audio, Readers, and Advanced options.
func BuildSettingsMainMenu(
	cfg *config.Instance,
	pages *tview.Pages,
	app *tview.Application,
	pl platforms.Platform,
	rebuildMainPage func(),
) {
	svc := NewSettingsService(client.NewLocalAPIClient(cfg))
	BuildSettingsMainMenuWithService(cfg, svc, pages, app, pl, rebuildMainPage)
}

// BuildSettingsMainMenuWithService creates the settings menu using the given SettingsService.
func BuildSettingsMainMenuWithService(
	cfg *config.Instance,
	svc SettingsService,
	pages *tview.Pages,
	app *tview.Application,
	pl platforms.Platform,
	rebuildMainPage func(),
) {
	mainMenu := NewSettingsList(pages, PageMain)
	mainMenu.SetTitle("Settings")
	if rebuildMainPage != nil {
		mainMenu.SetRebuildPrevious(rebuildMainPage)
	}

	rebuildSettingsMain := func() {
		BuildSettingsMainMenuWithService(cfg, svc, pages, app, pl, rebuildMainPage)
	}

	mainMenu.
		AddAction("Readers", "Reader connections and scanning", func() {
			buildReadersSettingsMenu(cfg, svc, pages, app, pl)
		}).
		AddAction("Audio", "Sound and feedback settings", func() {
			buildAudioSettingsMenu(svc, pages, app)
		}).
		AddAction("TUI", "Theme and display preferences", func() {
			buildTUISettingsMenu(pages, pl, rebuildSettingsMain)
		}).
		AddAction("Advanced", "Debug and system options", func() {
			buildAdvancedSettingsMenu(svc, pages, app)
		}).
		AddBackWithDesc("Back to main menu")

	pageDefaults(PageSettingsMain, pages, mainMenu.List)
}

// buildAudioSettingsMenu creates the audio settings submenu.
func buildAudioSettingsMenu(svc SettingsService, pages *tview.Pages, app *tview.Application) *tview.List {
	ctx, cancel := tuiContext()
	defer cancel()
	settings, err := svc.GetSettings(ctx)
	if err != nil {
		log.Error().Err(err).Msg("error fetching settings")
		showErrorModal(pages, app, "Failed to load audio settings")
		pages.SwitchToPage(PageSettingsMain)
		return nil
	}

	audioFeedback := settings.AudioScanFeedback

	menu := NewSettingsList(pages, PageSettingsMain)
	menu.SetTitle("Settings - Audio")

	menu.AddToggle("Audio feedback on scan", "Play sound when token is scanned", &audioFeedback, func(value bool) {
		ctx, cancel := tuiContext()
		defer cancel()
		err := svc.UpdateSettings(ctx, models.UpdateSettingsParams{
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

// exitDelayLabels extracts display labels from ExitDelayOptions.
func exitDelayLabels() []string {
	labels := make([]string, len(ExitDelayOptions))
	for i, opt := range ExitDelayOptions {
		labels[i] = opt.Label
	}
	return labels
}

// findExitDelayIndex finds the index of the given delay value in ExitDelayOptions.
func findExitDelayIndex(delay float32) int {
	for i, opt := range ExitDelayOptions {
		if opt.Value == delay {
			return i
		}
	}
	return 0
}

// buildReadersSettingsMenu creates the readers settings submenu.
func buildReadersSettingsMenu(
	cfg *config.Instance,
	svc SettingsService,
	pages *tview.Pages,
	app *tview.Application,
	pl platforms.Platform,
) *tview.List {
	ctx, cancel := tuiContext()
	defer cancel()
	settings, err := svc.GetSettings(ctx)
	if err != nil {
		log.Error().Err(err).Msg("error fetching settings")
		showErrorModal(pages, app, "Failed to load reader settings")
		pages.SwitchToPage(PageSettingsMain)
		return nil
	}

	autoDetect := settings.ReadersAutoDetect

	scanModeOptions := []string{"Tap", "Hold"}
	scanModeIndex := 0
	if settings.ReadersScanMode == config.ScanModeHold {
		scanModeIndex = 1
	}

	exitDelayIndex := findExitDelayIndex(settings.ReadersScanExitDelay)

	menu := NewSettingsList(pages, PageSettingsMain)
	menu.SetTitle("Settings - Readers")

	scanModeIdx := menu.GetItemCount()
	scanModeDesc := "Tap: tap to launch, Hold: exits when removed"
	menu.AddCycle("Scan mode", scanModeDesc, scanModeOptions, &scanModeIndex, func(option string, _ int) {
		mode := strings.ToLower(option)
		ctx, cancel := tuiContext()
		defer cancel()
		err := svc.UpdateSettings(ctx, models.UpdateSettingsParams{
			ReadersScanMode: &mode,
		})
		if err != nil {
			log.Error().Err(err).Msg("error updating scan mode")
			showErrorModal(pages, app, "Failed to save scan mode")
		}
	})

	menu.AddToggle("Auto-detect readers", "Automatically find connected readers", &autoDetect, func(value bool) {
		ctx, cancel := tuiContext()
		defer cancel()
		err := svc.UpdateSettings(ctx, models.UpdateSettingsParams{
			ReadersAutoDetect: &value,
		})
		if err != nil {
			log.Error().Err(err).Msg("error updating auto-detect")
			showErrorModal(pages, app, "Failed to save auto-detect setting")
		}
	})

	exitDelayIdx := menu.GetItemCount()
	exitDelayDesc := "Time to wait before exiting in Hold mode"
	exitLabels := exitDelayLabels()
	menu.AddCycle("Exit delay", exitDelayDesc, exitLabels, &exitDelayIndex, func(_ string, idx int) {
		delayF := ExitDelayOptions[idx].Value
		ctx, cancel := tuiContext()
		defer cancel()
		err := svc.UpdateSettings(ctx, models.UpdateSettingsParams{
			ReadersScanExitDelay: &delayF,
		})
		if err != nil {
			log.Error().Err(err).Msg("error updating exit delay")
			showErrorModal(pages, app, "Failed to save exit delay")
		}
	})

	menu.AddAction("Manage readers", "Add, edit, or remove manual reader connections", func() {
		buildReaderListPage(cfg, svc, pages, app, pl)
	})

	menu.AddBack()

	cycleIndices := map[int]func(delta int){
		scanModeIdx: func(delta int) {
			scanModeIndex = (scanModeIndex + delta + len(scanModeOptions)) % len(scanModeOptions)
			mode := strings.ToLower(scanModeOptions[scanModeIndex])
			ctx, cancel := tuiContext()
			defer cancel()
			err := svc.UpdateSettings(ctx, models.UpdateSettingsParams{
				ReadersScanMode: &mode,
			})
			if err != nil {
				log.Error().Err(err).Msg("error updating scan mode")
				showErrorModal(pages, app, "Failed to save scan mode")
			}
			menu.refreshAllItems(menu.GetCurrentItem())
		},
		exitDelayIdx: func(delta int) {
			exitDelayIndex = (exitDelayIndex + delta + len(ExitDelayOptions)) % len(ExitDelayOptions)
			delayF := ExitDelayOptions[exitDelayIndex].Value
			ctx, cancel := tuiContext()
			defer cancel()
			err := svc.UpdateSettings(ctx, models.UpdateSettingsParams{
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

// buildAdvancedSettingsMenu creates the advanced settings menu.
func buildAdvancedSettingsMenu(svc SettingsService, pages *tview.Pages, app *tview.Application) *tview.List {
	ctx, cancel := tuiContext()
	defer cancel()
	settings, err := svc.GetSettings(ctx)
	if err != nil {
		log.Error().Err(err).Msg("error fetching settings")
		showErrorModal(pages, app, "Failed to load advanced settings")
		pages.SwitchToPage(PageSettingsMain)
		return nil
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
		buildIgnoreSystemsPage(svc, pages, app)
	})

	menu.AddToggle("Debug logging", "Enable verbose debug output", &debugLogging, func(value bool) {
		ctx, cancel := tuiContext()
		defer cancel()
		err := svc.UpdateSettings(ctx, models.UpdateSettingsParams{
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

// buildReaderListPage creates the reader list management page.
func buildReaderListPage(
	cfg *config.Instance,
	svc SettingsService,
	pages *tview.Pages,
	app *tview.Application,
	pl platforms.Platform,
) tview.Primitive {
	ctx, cancel := tuiContext()
	defer cancel()
	settings, err := svc.GetSettings(ctx)
	if err != nil {
		log.Error().Err(err).Msg("error fetching settings")
		showErrorModal(pages, app, "Failed to load reader list")
		pages.SwitchToPage(PageSettingsReadersMenu)
		return nil
	}

	readers := settings.ReadersConnect

	layout := tview.NewFlex().SetDirection(tview.FlexRow)
	layout.SetTitle("Manage Readers")
	layout.SetBorder(true)

	readerList := tview.NewList()
	readerList.SetSecondaryTextColor(CurrentTheme().SecondaryTextColor)
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
				buildReaderEditPage(cfg, svc, pages, app, pl, &readers, idx)
			})
		}
		if len(readers) == 0 {
			readerList.AddItem("(no readers configured)", "Press Add to create one", 0, nil)
		}
	}
	refreshList()

	buttonBar := NewButtonBar(app)

	buttonBar.AddButton("Add", func() {
		buildReaderEditPage(cfg, svc, pages, app, pl, &readers, len(readers))
	})

	buttonBar.AddButton("Delete", func() {
		if len(readers) == 0 {
			return
		}
		idx := readerList.GetCurrentItem()
		if idx >= 0 && idx < len(readers) {
			readerName := readers[idx].Driver
			if readers[idx].Path != "" {
				readerName += ":" + readers[idx].Path
			}
			showConfirmModal(pages, app, "Delete reader "+readerName+"?", func() {
				readers = append(readers[:idx], readers[idx+1:]...)
				ctx, cancel := tuiContext()
				defer cancel()
				err := svc.UpdateSettings(ctx, models.UpdateSettingsParams{
					ReadersConnect: &readers,
				})
				if err != nil {
					log.Error().Err(err).Msg("error deleting reader")
					showErrorModal(pages, app, "Failed to delete reader")
				}
				refreshList()
			})
		}
	})

	buttonBar.AddButton("Back", func() {
		pages.SwitchToPage(PageSettingsReadersMenu)
	})

	buttonBar.SetupNavigation(func() {
		pages.SwitchToPage(PageSettingsReadersMenu)
	})

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

// buildReaderEditPage creates the reader edit form.
func buildReaderEditPage(
	cfg *config.Instance,
	svc SettingsService,
	pages *tview.Pages,
	app *tview.Application,
	pl platforms.Platform,
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

	// Get available drivers from platform
	supportedReaders := pl.SupportedReaders(cfg)
	availableDrivers := make([]string, 0, len(supportedReaders))
	for _, r := range supportedReaders {
		availableDrivers = append(availableDrivers, r.Metadata().ID)
	}

	if len(availableDrivers) == 0 {
		showErrorModal(pages, app, "No reader drivers available for this platform")
		buildReaderListPage(cfg, svc, pages, app, pl)
		return nil
	}

	layout := tview.NewFlex().SetDirection(tview.FlexRow)
	if isNew {
		layout.SetTitle("Add Reader")
	} else {
		layout.SetTitle("Edit Reader")
	}
	layout.SetBorder(true)

	driverIndex := 0
	for i, d := range availableDrivers {
		if d == reader.Driver {
			driverIndex = i
			break
		}
	}

	driverDisplay := tview.NewTextView().SetDynamicColors(true)
	updateDriverDisplay := func() {
		t := CurrentTheme()
		driverDisplay.SetText(fmt.Sprintf(
			"[%s]Driver:[%s] < %s >",
			t.AccentColorName, t.TextColorName, availableDrivers[driverIndex],
		))
	}
	updateDriverDisplay()

	pathInput := tview.NewInputField().
		SetLabel("Path: ").
		SetText(reader.Path).
		SetFieldWidth(30)
	setupInputFieldFocus(pathInput)

	idSourceInput := tview.NewInputField().
		SetLabel("ID Source: ").
		SetText(reader.IDSource).
		SetFieldWidth(20)
	setupInputFieldFocus(idSourceInput)

	buttonBar := NewButtonBar(app)

	buttonBar.AddButton("Save", func() {
		reader.Driver = availableDrivers[driverIndex]
		reader.Path = pathInput.GetText()
		reader.IDSource = idSourceInput.GetText()

		if isNew || index >= len(*readers) {
			*readers = append(*readers, reader)
		} else {
			(*readers)[index] = reader
		}

		ctx, cancel := tuiContext()
		defer cancel()
		err := svc.UpdateSettings(ctx, models.UpdateSettingsParams{
			ReadersConnect: readers,
		})
		if err != nil {
			log.Error().Err(err).Msg("error saving reader")
			showErrorModal(pages, app, "Failed to save reader")
			return
		}
		buildReaderListPage(cfg, svc, pages, app, pl)
	})

	buttonBar.AddButton("Cancel", func() {
		buildReaderListPage(cfg, svc, pages, app, pl)
	})

	buttonBar.SetupNavigation(func() {
		buildReaderListPage(cfg, svc, pages, app, pl)
	})

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
			driverIndex = (driverIndex - 1 + len(availableDrivers)) % len(availableDrivers)
			updateDriverDisplay()
			return nil
		case tcell.KeyRight:
			driverIndex = (driverIndex + 1) % len(availableDrivers)
			updateDriverDisplay()
			return nil
		case tcell.KeyDown, tcell.KeyEnter, tcell.KeyTab:
			setFocus(1)
			return nil
		case tcell.KeyEscape:
			buildReaderListPage(cfg, svc, pages, app, pl)
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
			buildReaderListPage(cfg, svc, pages, app, pl)
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
			buildReaderListPage(cfg, svc, pages, app, pl)
			return nil
		default:
			return event
		}
	})

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

// buildIgnoreSystemsPage creates the ignore systems multi-select page.
func buildIgnoreSystemsPage(svc SettingsService, pages *tview.Pages, app *tview.Application) tview.Primitive {
	ctx, cancel := tuiContext()
	defer cancel()
	settings, err := svc.GetSettings(ctx)
	if err != nil {
		log.Error().Err(err).Msg("error fetching settings")
		showErrorModal(pages, app, "Failed to load settings")
		pages.SwitchToPage(PageSettingsAdvanced)
		return nil
	}

	systems, err := svc.GetSystems(ctx)
	if err != nil {
		log.Error().Err(err).Msg("error fetching systems")
		showErrorModal(pages, app, "Failed to load systems list")
		pages.SwitchToPage(PageSettingsAdvanced)
		return nil
	}

	items := make([]SystemItem, len(systems))
	for i, sys := range systems {
		label := sys.Name
		if label == "" {
			label = sys.ID
		}
		items[i] = SystemItem{ID: sys.ID, Name: label}
	}

	layout := tview.NewFlex().SetDirection(tview.FlexRow)
	layout.SetTitle("Ignore Systems")
	layout.SetBorder(true)

	doneBtn := tview.NewButton("Done")

	var systemSelector *SystemSelector
	systemSelector = NewSystemSelector(&SystemSelectorConfig{
		Mode:     SystemSelectorMulti,
		Systems:  items,
		Selected: settings.ReadersScanIgnoreSystem,
		OnMulti: func(_ []string) {
			// Update button label when selection changes
			count := systemSelector.GetSelectedCount()
			if count > 0 {
				doneBtn.SetLabel(fmt.Sprintf("Done (%d selected)", count))
			} else {
				doneBtn.SetLabel("Done")
			}
		},
	})

	doneBtn.SetSelectedFunc(func() {
		selected := systemSelector.GetSelected()
		ctx, cancel := tuiContext()
		defer cancel()
		err := svc.UpdateSettings(ctx, models.UpdateSettingsParams{
			ReadersScanIgnoreSystem: &selected,
		})
		if err != nil {
			log.Error().Err(err).Msg("error updating ignored systems")
			showErrorModal(pages, app, "Failed to save ignored systems")
			return
		}
		buildAdvancedSettingsMenu(svc, pages, app)
	})

	count := systemSelector.GetSelectedCount()
	if count > 0 {
		doneBtn.SetLabel(fmt.Sprintf("Done (%d selected)", count))
	}

	systemSelector.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEscape:
			pages.SwitchToPage(PageSettingsAdvanced)
			return nil
		case tcell.KeyTab:
			app.SetFocus(doneBtn)
			return nil
		case tcell.KeyDown:
			// If at last item, navigate to Done button
			if systemSelector.GetCurrentItem() == systemSelector.GetItemCount()-1 {
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
			app.SetFocus(systemSelector)
			return nil
		case tcell.KeyEscape:
			pages.SwitchToPage(PageSettingsAdvanced)
			return nil
		default:
			return event
		}
	})

	layout.AddItem(systemSelector, 0, 1, true)
	layout.AddItem(doneBtn, 1, 0, false)

	pageDefaults(PageSettingsIgnoreSystems, pages, layout)
	return layout
}

// BuildTagsReadMenu creates the NFC tag read menu.
func BuildTagsReadMenu(cfg *config.Instance, pages *tview.Pages, app *tview.Application) {
	topTextView := tview.NewTextView().
		SetLabel("").
		SetText("Press Enter to scan a card, Esc to Exit")

	tagsReadMenu := tview.NewForm().
		AddFormItem(topTextView)
	tagsReadMenu.SetTitle("Settings - NFC Tags - Read")

	var readCancel context.CancelFunc
	reading := false

	tagsReadMenu.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		k := event.Key()
		if k == tcell.KeyEnter && !reading {
			tagsReadMenu.Clear(false).AddFormItem(topTextView)
			topTextView.SetText("Tap a card to read content... (ESC to cancel)")
			reading = true

			var ctx context.Context
			ctx, readCancel = tagReadContext()

			go func() {
				resp, err := client.WaitNotification(
					ctx, 0,
					cfg, models.NotificationTokensAdded,
				)
				if err != nil {
					log.Error().Err(err).Msg("error waiting for tag")
					app.QueueUpdateDraw(func() {
						reading = false
						readCancel = nil
						topTextView.SetText("Failed to read tag. Press ENTER to try again, ESC to exit")
					})
					return
				}

				var data models.TokenResponse
				err = json.Unmarshal([]byte(resp), &data)
				if err != nil {
					log.Error().Err(err).Msg("error unmarshalling token")
					app.QueueUpdateDraw(func() {
						reading = false
						readCancel = nil
						topTextView.SetText("Failed to parse tag data. Press ENTER to try again, ESC to exit")
					})
					return
				}

				app.QueueUpdateDraw(func() {
					reading = false
					readCancel = nil
					tagsReadMenu.AddTextView("ID", data.UID, 50, 1, true, false)
					tagsReadMenu.AddTextView("Data", data.Data, 50, 1, true, false)
					tagsReadMenu.AddTextView("Value", data.Text, 50, 4, true, false)
					topTextView.SetText("Press ENTER to scan another card. ESC to exit")
				})
			}()
			return nil
		}
		if k == tcell.KeyEscape {
			if readCancel != nil {
				readCancel()
			}
			pages.SwitchToPage(PageSettingsMain)
			return nil
		}
		return event
	})

	pageDefaults(PageSettingsTagsRead, pages, tagsReadMenu)
}
