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
	"fmt"
	"strings"
	"time"

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
	logDestPath string,
	logDestName string,
) {
	svc := NewSettingsService(client.NewLocalAPIClient(cfg))
	BuildSettingsMainMenuWithService(cfg, svc, pages, app, pl, rebuildMainPage, logDestPath, logDestName)
}

// BuildSettingsMainMenuWithService creates the settings menu using the given SettingsService.
func BuildSettingsMainMenuWithService(
	cfg *config.Instance,
	svc SettingsService,
	pages *tview.Pages,
	app *tview.Application,
	pl platforms.Platform,
	rebuildMainPage func(),
	logDestPath string,
	logDestName string,
) {
	// Create page frame
	frame := NewPageFrame(app).
		SetTitle("Settings")

	goBack := func() {
		if rebuildMainPage != nil {
			rebuildMainPage()
		} else {
			pages.SwitchToPage(PageMain)
		}
	}
	frame.SetOnEscape(goBack)

	// Create button bar
	buttonBar := NewButtonBar(app).
		AddButton("Back", goBack).
		SetupNavigation(goBack)
	frame.SetButtonBar(buttonBar)

	// Create settings list
	mainMenu := NewSettingsList(pages, PageMain)
	if rebuildMainPage != nil {
		mainMenu.SetRebuildPrevious(rebuildMainPage)
	}

	// Enable dynamic help mode
	mainMenu.SetDynamicHelpMode(true).
		SetHelpCallback(func(desc string) {
			frame.SetHelpText(desc)
		})

	rebuildSettingsMain := func() {
		BuildSettingsMainMenuWithService(cfg, svc, pages, app, pl, rebuildMainPage, logDestPath, logDestName)
	}

	mainMenu.
		AddNavAction("Readers", "Reader connections and scanning", func() {
			buildReadersSettingsMenu(cfg, svc, pages, app, pl)
		}).
		AddNavAction("Audio", "Sound and feedback settings", func() {
			buildAudioSettingsMenu(svc, pages, app)
		}).
		AddNavAction("TUI", "Theme and display preferences", func() {
			buildTUISettingsMenu(pages, app, pl, rebuildSettingsMain)
		}).
		AddNavAction("Advanced", "Debug and system options", func() {
			buildAdvancedSettingsMenu(svc, pages, app)
		}).
		AddNavAction("Logs", "View and export log files", func() {
			BuildExportLogModal(pages, app, pl, logDestPath, logDestName)
		}).
		AddNavAction("About", "Version, license, and credits", func() {
			buildAboutPage(pages, app)
		})

	// Set content and trigger initial help
	frame.SetContent(mainMenu.List)
	mainMenu.TriggerInitialHelp()
	frame.SetupContentToButtonNavigation()

	pages.AddAndSwitchToPage(PageSettingsMain, frame, true)
}

// buildAudioSettingsMenu creates the audio settings submenu.
func buildAudioSettingsMenu(svc SettingsService, pages *tview.Pages, app *tview.Application) {
	ctx, cancel := tuiContext()
	defer cancel()
	settings, err := svc.GetSettings(ctx)
	if err != nil {
		log.Error().Err(err).Msg("error fetching settings")
		ShowErrorModal(pages, app, "Failed to load audio settings", func() {
			pages.SwitchToPage(PageSettingsMain)
		})
		return
	}

	// Create page frame
	frame := NewPageFrame(app).
		SetTitle("Settings", "Audio")

	goBack := func() {
		pages.SwitchToPage(PageSettingsMain)
	}
	frame.SetOnEscape(goBack)

	// Create button bar
	buttonBar := NewButtonBar(app).
		AddButton("Back", goBack).
		SetupNavigation(goBack)
	frame.SetButtonBar(buttonBar)

	audioFeedback := settings.AudioScanFeedback

	menu := NewSettingsList(pages, PageSettingsMain)

	// Enable dynamic help mode
	menu.SetDynamicHelpMode(true).
		SetHelpCallback(func(desc string) {
			frame.SetHelpText(desc)
		})

	menu.AddToggle("Audio feedback on scan", "Play sound when token is scanned", &audioFeedback, func(value bool) {
		ctx, cancel := tuiContext()
		defer cancel()
		err := svc.UpdateSettings(ctx, models.UpdateSettingsParams{
			AudioScanFeedback: &value,
		})
		if err != nil {
			log.Error().Err(err).Msg("error updating audio feedback")
			ShowErrorModal(pages, app, "Failed to save audio settings", func() {
				app.SetFocus(menu.List)
			})
		}
	})

	// Set content and trigger initial help
	frame.SetContent(menu.List)
	menu.TriggerInitialHelp()
	frame.SetupContentToButtonNavigation()

	pages.AddAndSwitchToPage(PageSettingsAudioMenu, frame, true)
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
) {
	ctx, cancel := tuiContext()
	defer cancel()
	settings, err := svc.GetSettings(ctx)
	if err != nil {
		log.Error().Err(err).Msg("error fetching settings")
		ShowErrorModal(pages, app, "Failed to load reader settings", func() {
			pages.SwitchToPage(PageSettingsMain)
		})
		return
	}

	// Create page frame
	frame := NewPageFrame(app).
		SetTitle("Settings", "Readers")

	goBack := func() {
		pages.SwitchToPage(PageSettingsMain)
	}
	frame.SetOnEscape(goBack)

	// Create button bar
	buttonBar := NewButtonBar(app).
		AddButton("Back", goBack).
		SetupNavigation(goBack)
	frame.SetButtonBar(buttonBar)

	autoDetect := settings.ReadersAutoDetect

	scanModeOptions := []string{"Tap", "Hold"}
	scanModeIndex := 0
	if settings.ReadersScanMode == config.ScanModeHold {
		scanModeIndex = 1
	}

	exitDelayIndex := findExitDelayIndex(settings.ReadersScanExitDelay)

	menu := NewSettingsList(pages, PageSettingsMain)

	// Enable dynamic help mode
	menu.SetDynamicHelpMode(true).
		SetHelpCallback(func(desc string) {
			frame.SetHelpText(desc)
		})

	menu.AddToggle("Auto-detect readers", "Automatically find connected readers", &autoDetect, func(value bool) {
		ctx, cancel := tuiContext()
		defer cancel()
		err := svc.UpdateSettings(ctx, models.UpdateSettingsParams{
			ReadersAutoDetect: &value,
		})
		if err != nil {
			log.Error().Err(err).Msg("error updating auto-detect")
			ShowErrorModal(pages, app, "Failed to save auto-detect setting", func() {
				app.SetFocus(menu.List)
			})
		}
	})

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
			ShowErrorModal(pages, app, "Failed to save scan mode", func() {
				app.SetFocus(menu.List)
			})
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
			ShowErrorModal(pages, app, "Failed to save exit delay", func() {
				app.SetFocus(menu.List)
			})
		}
	})

	menu.AddNavAction("Manage readers", "Add, edit, or remove manual reader connections", func() {
		buildReaderListPage(cfg, svc, pages, app, pl)
	})

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
				ShowErrorModal(pages, app, "Failed to save scan mode", func() {
					app.SetFocus(menu.List)
				})
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
				ShowErrorModal(pages, app, "Failed to save exit delay", func() {
					app.SetFocus(menu.List)
				})
			}
			menu.refreshAllItems(menu.GetCurrentItem())
		},
	}

	menu.SetupCycleKeys(cycleIndices)

	// Set content and trigger initial help
	frame.SetContent(menu.List)
	menu.TriggerInitialHelp()
	frame.SetupContentToButtonNavigation()

	pages.AddAndSwitchToPage(PageSettingsReadersMenu, frame, true)
}

// buildAdvancedSettingsMenu creates the advanced settings menu.
func buildAdvancedSettingsMenu(svc SettingsService, pages *tview.Pages, app *tview.Application) {
	ctx, cancel := tuiContext()
	defer cancel()
	settings, err := svc.GetSettings(ctx)
	if err != nil {
		log.Error().Err(err).Msg("error fetching settings")
		ShowErrorModal(pages, app, "Failed to load advanced settings", func() {
			pages.SwitchToPage(PageSettingsMain)
		})
		return
	}

	// Create page frame
	frame := NewPageFrame(app).
		SetTitle("Settings", "Advanced")

	goBack := func() {
		pages.SwitchToPage(PageSettingsMain)
	}
	frame.SetOnEscape(goBack)

	// Create button bar
	buttonBar := NewButtonBar(app).
		AddButton("Back", goBack).
		SetupNavigation(goBack)
	frame.SetButtonBar(buttonBar)

	debugLogging := settings.DebugLogging

	// Build ignore systems label with count indicator
	ignoreLabel := "Ignore systems"
	ignoreCount := len(settings.ReadersScanIgnoreSystem)
	if ignoreCount > 0 {
		ignoreLabel = fmt.Sprintf("Ignore systems (%d selected)", ignoreCount)
	}

	menu := NewSettingsList(pages, PageSettingsMain)

	// Enable dynamic help mode
	menu.SetDynamicHelpMode(true).
		SetHelpCallback(func(desc string) {
			frame.SetHelpText(desc)
		})

	menu.AddNavAction(ignoreLabel, "Systems to ignore exiting in Hold mode", func() {
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
			ShowErrorModal(pages, app, "Failed to save debug logging setting", func() {
				app.SetFocus(menu.List)
			})
		}
	})

	// Set content and trigger initial help
	frame.SetContent(menu.List)
	menu.TriggerInitialHelp()
	frame.SetupContentToButtonNavigation()

	pages.AddAndSwitchToPage(PageSettingsAdvanced, frame, true)
}

// buildReaderListPage creates the reader list management page.
func buildReaderListPage(
	cfg *config.Instance,
	svc SettingsService,
	pages *tview.Pages,
	app *tview.Application,
	pl platforms.Platform,
) {
	ctx, cancel := tuiContext()
	defer cancel()
	settings, err := svc.GetSettings(ctx)
	if err != nil {
		log.Error().Err(err).Msg("error fetching settings")
		ShowErrorModal(pages, app, "Failed to load reader list", func() {
			pages.SwitchToPage(PageSettingsReadersMenu)
		})
		return
	}

	readers := settings.ReadersConnect

	// Create page frame
	frame := NewPageFrame(app).
		SetTitle("Settings", "Readers", "Manage").
		SetHelpText("Select a reader to edit, or use Add/Delete")

	goBack := func() {
		pages.SwitchToPage(PageSettingsReadersMenu)
	}
	frame.SetOnEscape(goBack)

	readerList := tview.NewList()
	readerList.SetSecondaryTextColor(CurrentTheme().SecondaryTextColor)
	readerList.ShowSecondaryText(true)
	readerList.SetSelectedFocusOnly(true)
	readerList.SetFocusFunc(func() {
		frame.SetHelpText("Select a reader to edit, or use Add/Delete")
	})
	readerList.SetBlurFunc(func() {
		// Keep help text visible when moving to buttons
	})

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
				t := CurrentTheme()
				secondary = fmt.Sprintf("[%s::b]ID Source:[-::-] %s", t.LabelColorName, reader.IDSource)
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

	buttonBar.AddButtonWithHelp("Add", "Add a new reader connection", func() {
		buildReaderEditPage(cfg, svc, pages, app, pl, &readers, len(readers))
	})

	buttonBar.AddButtonWithHelp("Delete", "Remove the selected reader", func() {
		if len(readers) == 0 {
			return
		}
		idx := readerList.GetCurrentItem()
		if idx >= 0 && idx < len(readers) {
			readerName := readers[idx].Driver
			if readers[idx].Path != "" {
				readerName += ":" + readers[idx].Path
			}
			ShowConfirmModal(pages, app, "Delete reader "+readerName+"?", func() {
				readers = append(readers[:idx], readers[idx+1:]...)
				ctx, cancel := tuiContext()
				defer cancel()
				err := svc.UpdateSettings(ctx, models.UpdateSettingsParams{
					ReadersConnect: &readers,
				})
				if err != nil {
					log.Error().Err(err).Msg("error deleting reader")
					ShowErrorModal(pages, app, "Failed to delete reader", func() {
						app.SetFocus(readerList)
					})
				}
				refreshList()
			}, nil)
		}
	})

	buttonBar.AddButtonWithHelp("Back", "Return to reader settings", goBack)
	buttonBar.SetupNavigation(goBack)
	buttonBar.SetHelpCallback(func(help string) {
		frame.SetHelpText(help)
	})

	frame.SetContent(readerList)
	frame.SetButtonBar(buttonBar)
	frame.SetupContentToButtonNavigation()

	pages.AddAndSwitchToPage(PageSettingsReaderList, frame, true)
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
) {
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
		ShowErrorModal(pages, app, "No reader drivers available for this platform", func() {
			buildReaderListPage(cfg, svc, pages, app, pl)
		})
		return
	}

	goBack := func() {
		buildReaderListPage(cfg, svc, pages, app, pl)
	}

	// Create page frame
	var titlePart string
	if isNew {
		titlePart = "Add"
	} else {
		titlePart = "Edit"
	}
	frame := NewPageFrame(app).
		SetTitle("Settings", "Readers", titlePart).
		SetHelpText("Use ←→ to change driver, Tab to move between fields")

	frame.SetOnEscape(goBack)

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
			"[%s::b]Driver:[-::-] < %s >",
			t.LabelColorName, availableDrivers[driverIndex],
		))
	}
	updateDriverDisplay()

	pathInput := tview.NewInputField().
		SetText(reader.Path).
		SetFieldWidth(30)
	SetInputLabel(pathInput, "Path")
	setupInputFieldFocus(pathInput)

	idSourceInput := tview.NewInputField().
		SetText(reader.IDSource).
		SetFieldWidth(20)
	SetInputLabel(idSourceInput, "ID Source")
	setupInputFieldFocus(idSourceInput)

	buttonBar := NewButtonBar(app)

	buttonBar.AddButtonWithHelp("Save", "Save reader configuration", func() {
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
			ShowErrorModal(pages, app, "Failed to save reader", func() {
				app.SetFocus(driverDisplay)
			})
			return
		}
		goBack()
	})

	buttonBar.AddButtonWithHelp("Cancel", "Discard changes and go back", goBack)
	buttonBar.SetupNavigation(goBack)
	buttonBar.SetHelpCallback(func(help string) {
		frame.SetHelpText(help)
	})

	// Create form content wrapper
	formContent := tview.NewFlex().SetDirection(tview.FlexRow)
	formContent.AddItem(driverDisplay, 1, 0, true)
	formContent.AddItem(pathInput, 1, 0, false)
	formContent.AddItem(idSourceInput, 1, 0, false)

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
		key := event.Key()
		if key == tcell.KeyLeft {
			driverIndex = (driverIndex - 1 + len(availableDrivers)) % len(availableDrivers)
			updateDriverDisplay()
			return nil
		}
		if key == tcell.KeyRight {
			driverIndex = (driverIndex + 1) % len(availableDrivers)
			updateDriverDisplay()
			return nil
		}
		if key == tcell.KeyDown || key == tcell.KeyEnter || key == tcell.KeyTab {
			setFocus(1)
			return nil
		}
		if key == tcell.KeyUp || key == tcell.KeyBacktab {
			frame.FocusButtonBar()
			return nil
		}
		if key == tcell.KeyEscape {
			goBack()
			return nil
		}
		return event
	})

	pathInput.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		key := event.Key()
		if key == tcell.KeyEnter && config.GetTUIConfig().OnScreenKeyboard {
			ShowOSKModal(
				pages,
				app,
				pathInput.GetText(),
				func(text string) {
					pathInput.SetText(text)
					app.SetFocus(pathInput)
				},
				func() {
					app.SetFocus(pathInput)
				},
			)
			return nil
		}
		if key == tcell.KeyUp || key == tcell.KeyBacktab {
			setFocus(0)
			return nil
		}
		if key == tcell.KeyDown || key == tcell.KeyTab {
			setFocus(2)
			return nil
		}
		if key == tcell.KeyEscape {
			goBack()
			return nil
		}
		return event
	})

	idSourceInput.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		key := event.Key()
		if key == tcell.KeyEnter && config.GetTUIConfig().OnScreenKeyboard {
			ShowOSKModal(
				pages,
				app,
				idSourceInput.GetText(),
				func(text string) {
					idSourceInput.SetText(text)
					app.SetFocus(idSourceInput)
				},
				func() {
					app.SetFocus(idSourceInput)
				},
			)
			return nil
		}
		if key == tcell.KeyUp || key == tcell.KeyBacktab {
			setFocus(1)
			return nil
		}
		if key == tcell.KeyDown || key == tcell.KeyTab {
			setFocus(3)
			return nil
		}
		if key == tcell.KeyEscape {
			goBack()
			return nil
		}
		return event
	})

	buttonBar.SetOnUp(func() {
		setFocus(2) // idSourceInput
	})
	buttonBar.SetOnDown(func() {
		setFocus(0) // driverDisplay (wrap)
	})

	frame.SetContent(formContent)
	frame.SetButtonBar(buttonBar)

	pages.AddAndSwitchToPage(PageSettingsReaderEdit, frame, true)
	app.SetFocus(driverDisplay)
}

// buildIgnoreSystemsPage creates the ignore systems multi-select page.
func buildIgnoreSystemsPage(svc SettingsService, pages *tview.Pages, app *tview.Application) {
	ctx, cancel := tuiContext()
	defer cancel()
	settings, err := svc.GetSettings(ctx)
	if err != nil {
		log.Error().Err(err).Msg("error fetching settings")
		ShowErrorModal(pages, app, "Failed to load settings", func() {
			pages.SwitchToPage(PageSettingsAdvanced)
		})
		return
	}

	systems, err := svc.GetSystems(ctx)
	if err != nil {
		log.Error().Err(err).Msg("error fetching systems")
		ShowErrorModal(pages, app, "Failed to load systems list", func() {
			pages.SwitchToPage(PageSettingsAdvanced)
		})
		return
	}

	items := make([]SystemItem, len(systems))
	for i, sys := range systems {
		label := sys.Name
		if label == "" {
			label = sys.ID
		}
		items[i] = SystemItem{ID: sys.ID, Name: label}
	}

	// Create page frame
	frame := NewPageFrame(app).
		SetTitle("Settings", "Advanced", "Ignore Systems").
		SetHelpText("Select systems to ignore during media scanning")

	goBack := func() {
		buildAdvancedSettingsMenu(svc, pages, app)
	}
	frame.SetOnEscape(goBack)

	// Create button bar
	buttonBar := NewButtonBar(app)

	var systemSelector *SystemSelector
	systemSelector = NewSystemSelector(&SystemSelectorConfig{
		Mode:     SystemSelectorMulti,
		Systems:  items,
		Selected: settings.ReadersScanIgnoreSystem,
		OnMulti: func(_ []string) {
			// Update button label when selection changes
			count := systemSelector.GetSelectedCount()
			if count > 0 {
				buttonBar.UpdateButtonLabel(0, fmt.Sprintf("Done (%d)", count))
			} else {
				buttonBar.UpdateButtonLabel(0, "Done")
			}
		},
	})

	saveAndExit := func() {
		selected := systemSelector.GetSelected()
		ctx, cancel := tuiContext()
		defer cancel()
		err := svc.UpdateSettings(ctx, models.UpdateSettingsParams{
			ReadersScanIgnoreSystem: &selected,
		})
		if err != nil {
			log.Error().Err(err).Msg("error updating ignored systems")
			ShowErrorModal(pages, app, "Failed to save ignored systems", func() {
				app.SetFocus(systemSelector)
			})
			return
		}
		buildAdvancedSettingsMenu(svc, pages, app)
	}

	// Update initial button label if items are selected
	count := systemSelector.GetSelectedCount()
	initialLabel := "Done"
	if count > 0 {
		initialLabel = fmt.Sprintf("Done (%d)", count)
	}

	buttonBar.AddButtonWithHelp(initialLabel, "Save ignored systems and return", saveAndExit).
		AddButtonWithHelp("Back", "Discard changes and return", goBack).
		SetupNavigation(goBack)
	buttonBar.SetHelpCallback(func(help string) {
		frame.SetHelpText(help)
	})
	frame.SetButtonBar(buttonBar)

	// Setup navigation from list to button bar (with wrap)
	systemSelector.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		key := event.Key()
		if key == tcell.KeyTab {
			frame.FocusButtonBar()
			return nil
		}
		if key == tcell.KeyDown {
			if systemSelector.GetCurrentItem() == systemSelector.GetItemCount()-1 {
				frame.FocusButtonBar()
				return nil
			}
		}
		if key == tcell.KeyUp {
			if systemSelector.GetCurrentItem() == 0 {
				frame.FocusButtonBar()
				return nil
			}
		}
		return event
	})

	// Setup navigation from button bar back to list (with wrap and correct position)
	buttonBar.SetOnUp(func() {
		systemSelector.SetCurrentItem(systemSelector.GetItemCount() - 1) // Last item
		app.SetFocus(systemSelector)
	})
	buttonBar.SetOnDown(func() {
		systemSelector.SetCurrentItem(0) // First item (wrap)
		app.SetFocus(systemSelector)
	})

	frame.SetContent(systemSelector)
	pages.AddAndSwitchToPage(PageSettingsIgnoreSystems, frame, true)
}

// buildAboutPage creates the About page with version, license, and credits.
func buildAboutPage(pages *tview.Pages, app *tview.Application) {
	frame := NewPageFrame(app).
		SetTitle("About")

	goBack := func() {
		pages.SwitchToPage(PageSettingsMain)
	}
	frame.SetOnEscape(goBack)

	buttonBar := NewButtonBar(app).
		AddButton("Back", goBack).
		SetupNavigation(goBack)
	frame.SetButtonBar(buttonBar)

	t := CurrentTheme()

	content := tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(false)

	aboutText := fmt.Sprintf(`[%s::b]Zaparoo Core[-::-]
Version %s

[%s::b]Copyright[-::-]
© %d The Zaparoo Project Contributors

[%s::b]License[-::-]
GNU General Public License v3.0 or later (GPL-3.0-or-later)

This is free software: you are free to change and redistribute it.
There is NO WARRANTY, to the extent permitted by law.`,
		t.AccentColorName,
		config.AppVersion,
		t.AccentColorName,
		time.Now().Year(),
		t.AccentColorName,
	)

	content.SetText(aboutText)

	frame.SetContent(content)
	pages.AddAndSwitchToPage(PageSettingsAbout, frame, true)
	app.SetFocus(buttonBar)
}
