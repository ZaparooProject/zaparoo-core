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
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
)

// writeTagForMedia is a helper for writing media search results to tags.
func writeTagForMedia(
	pages *tview.Pages,
	app *tview.Application,
	svc SettingsService,
	writeValue string,
	mediaList *tview.List,
) {
	WriteTagWithModal(pages, app, svc, writeValue, func(_ bool) {
		app.SetFocus(mediaList)
	})
}

// truncateSystemName truncates a system name to fit in the left column.
func truncateSystemName(name string) string {
	const maxLen = 18
	if len(name) <= maxLen {
		return name
	}
	return name[:maxLen-3] + "..."
}

// Session state for media search - persists across page navigation.
var (
	searchMediaName       string
	searchMediaSystem     string
	searchMediaSystemName = "All"
)

// BuildSearchMedia creates the search media page.
func BuildSearchMedia(svc SettingsService, pages *tview.Pages, app *tview.Application) {
	frame := NewPageFrame(app).
		SetTitle("Search Media").
		SetHelpText("Type query in Name and press Search")

	goBack := func() {
		pages.SwitchToPage(PageMain)
	}
	frame.SetOnEscape(goBack)

	searching := false
	var resultPaths []string

	scrollList := NewScrollIndicatorList()
	mediaList := scrollList.GetList()
	mediaList.SetMainTextColor(CurrentTheme().PrimaryTextColor)
	mediaList.SetChangedFunc(func(index int, _, _ string, _ rune) {
		if index >= 0 && index < len(resultPaths) {
			frame.SetInfoText(fmt.Sprintf("[%s]%s[-]", CurrentTheme().SecondaryColor, resultPaths[index]))
		} else {
			frame.SetInfoText("")
		}
	})

	nameLabel := NewLabel("Name")

	searchInput := tview.NewInputField()
	searchInput.SetText(searchMediaName)
	searchInput.SetChangedFunc(func(value string) {
		searchMediaName = value
	})
	setupInputFieldFocus(searchInput)

	systemLabel := NewLabel("System")

	systemButton := tview.NewButton(truncateSystemName(searchMediaSystemName))

	var systemItems []SystemItem
	ctx, cancel := tuiContext()
	systems, err := svc.GetSystems(ctx)
	cancel()
	if err != nil {
		log.Error().Err(err).Msg("error getting system list")
	} else {
		sort.Slice(systems, func(i, j int) bool {
			return systems[i].Name < systems[j].Name
		})
		systemItems = make([]SystemItem, len(systems))
		for i, v := range systems {
			systemItems[i] = SystemItem{ID: v.ID, Name: v.Name}
		}
	}

	openSystemSelector := func() {
		selectorPage := "search_system_selector"

		layout := tview.NewFlex().SetDirection(tview.FlexRow)
		layout.SetBorder(true)
		SetBoxTitle(layout, "Select System")

		selector := NewSystemSelector(&SystemSelectorConfig{
			Mode:        SystemSelectorSingle,
			IncludeAll:  true,
			AutoConfirm: true,
			Systems:     systemItems,
			Selected:    []string{searchMediaSystem},
			OnSingle: func(systemID string) {
				searchMediaSystem = systemID
				if searchMediaSystem == "" {
					searchMediaSystemName = "All"
				} else {
					for _, item := range systemItems {
						if item.ID == searchMediaSystem {
							searchMediaSystemName = item.Name
							break
						}
					}
				}
				systemButton.SetLabel(truncateSystemName(searchMediaSystemName))
				pages.RemovePage(selectorPage)
				app.SetFocus(systemButton)
			},
		})

		selector.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
			if event.Key() == tcell.KeyEscape {
				pages.RemovePage(selectorPage)
				app.SetFocus(systemButton)
				return nil
			}
			return event
		})

		layout.AddItem(selector, 0, 1, true)
		pages.AddPage(selectorPage, CenterWidget(35, 10, layout), true, true)
		app.SetFocus(selector)
	}

	search := func() {
		if searching {
			return
		}

		params := models.SearchParams{
			Query: &searchMediaName,
		}

		if searchMediaSystem != "" {
			systemsFilter := []string{searchMediaSystem}
			params.Systems = &systemsFilter
		}

		frame.SetHelpText("Searching...")
		searching = true
		app.ForceDraw()
		defer func() {
			searching = false
		}()

		searchCtx, searchCancel := context.WithTimeout(context.Background(), config.APIRequestTimeout)
		results, err := svc.SearchMedia(searchCtx, params)
		searchCancel()
		if err != nil {
			log.Error().Err(err).Msg("error executing search query")
			frame.SetHelpText("An error occurred during search")
			return
		}

		mediaList.Clear()
		mediaList.SetCurrentItem(0)
		resultPaths = make([]string, len(results.Results))
		tuiCfg := config.GetTUIConfig()
		for i := range results.Results {
			result := &results.Results[i]
			resultPaths[i] = result.Path
			var displayName, writeValue string
			if tuiCfg.WriteFormat == "path" {
				// Check if path is a virtual path (contains ://)
				// Virtual paths like "steam://123/Name" don't work with filepath.Base
				if strings.Contains(result.Path, "://") {
					displayName = result.Name
				} else {
					base := filepath.Base(result.Path)
					ext := filepath.Ext(base)
					displayName = strings.TrimSuffix(base, ext)
				}
				writeValue = result.Path
			} else {
				displayName = result.Name
				writeValue = result.ZapScript
			}
			displayText := fmt.Sprintf("%s [%s](%s)[-]", displayName, CurrentTheme().SecondaryColor, result.System.Name)
			value := writeValue // capture for closure
			mediaList.AddItem(displayText, "", 0, func() {
				writeTagForMedia(pages, app, svc, value, mediaList)
			})
		}
		// Update info text for first result
		if len(resultPaths) > 0 {
			frame.SetInfoText(fmt.Sprintf("[%s]%s[-]", CurrentTheme().SecondaryColor, resultPaths[0]))
		} else {
			frame.SetInfoText("")
		}

		resultWord := "results"
		if len(results.Results) == 1 {
			resultWord = "result"
		}
		frame.SetHelpText(fmt.Sprintf("Found %d %s. Select to write token", len(results.Results), resultWord))
		if results.Total > 0 {
			app.SetFocus(mediaList)
		}
	}

	var lastLeftFocus tview.Primitive = searchInput

	focusResults := func() {
		if mediaList.GetItemCount() > 0 {
			app.SetFocus(mediaList)
		} else {
			frame.FocusButtonBar()
		}
	}

	focusLeftColumn := func() {
		app.SetFocus(lastLeftFocus)
	}

	searchInput.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEnter:
			if config.GetTUIConfig().OnScreenKeyboard {
				ShowOSKModal(
					pages,
					app,
					searchInput.GetText(),
					func(text string) {
						searchInput.SetText(text)
						searchMediaName = text
						app.SetFocus(searchInput)
					},
					func() {
						app.SetFocus(searchInput)
					},
				)
			} else {
				search()
			}
			return nil
		case tcell.KeyDown:
			app.SetFocus(systemButton)
			return nil
		case tcell.KeyUp, tcell.KeyBacktab:
			frame.FocusButtonBar()
			return nil
		case tcell.KeyTab:
			focusResults()
			return nil
		case tcell.KeyEscape:
			goBack()
			return nil
		default:
			return event
		}
	})

	systemButton.SetSelectedFunc(openSystemSelector)
	systemButton.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		lastLeftFocus = systemButton
		switch event.Key() {
		case tcell.KeyDown:
			frame.FocusButtonBar()
			return nil
		case tcell.KeyUp:
			app.SetFocus(searchInput)
			return nil
		case tcell.KeyRight, tcell.KeyTab:
			focusResults()
			return nil
		case tcell.KeyLeft, tcell.KeyBacktab:
			frame.FocusButtonBar()
			return nil
		case tcell.KeyEscape:
			goBack()
			return nil
		default:
			return event
		}
	})

	mediaList.SetWrapAround(true)
	mediaList.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyLeft, tcell.KeyBacktab:
			focusLeftColumn()
			return nil
		case tcell.KeyRight, tcell.KeyTab:
			frame.FocusButtonBar()
			return nil
		case tcell.KeyEscape:
			goBack()
			return nil
		default:
			return event
		}
	})

	clearSearch := func() {
		searchMediaName = ""
		searchMediaSystem = ""
		searchMediaSystemName = "All"
		searchInput.SetText("")
		systemButton.SetLabel(truncateSystemName("All"))
		mediaList.Clear()
		resultPaths = nil
		frame.SetInfoText("")
		frame.SetHelpText("Type query in Name and press Search")
		app.SetFocus(searchInput)
	}

	buttonBar := NewButtonBar(app)
	buttonBar.AddButton("Search", search).
		AddButton("Clear", clearSearch).
		AddButton("Back", goBack).
		SetupNavigation(goBack)
	frame.SetButtonBar(buttonBar)

	buttonBar.SetOnUp(func() {
		if mediaList.GetItemCount() > 0 {
			mediaList.SetCurrentItem(mediaList.GetItemCount() - 1)
			app.SetFocus(mediaList)
		} else {
			app.SetFocus(systemButton)
		}
	})
	buttonBar.SetOnDown(func() {
		app.SetFocus(searchInput)
	})
	buttonBar.SetOnWrap(func() {
		app.SetFocus(searchInput)
	})
	buttonBar.SetOnLeft(func() {
		if mediaList.GetItemCount() > 0 {
			app.SetFocus(mediaList)
		} else {
			app.SetFocus(systemButton)
		}
	})

	// Build 2-column layout: left (inputs) | divider | right (results)
	leftColumn := tview.NewFlex().SetDirection(tview.FlexRow)
	leftColumn.AddItem(nameLabel, 1, 0, false)
	leftColumn.AddItem(searchInput, 1, 0, true)
	leftColumn.AddItem(systemLabel, 1, 0, false)
	leftColumn.AddItem(systemButton, 1, 0, false)
	leftColumn.AddItem(tview.NewBox(), 0, 1, false) // spacer

	// Vertical divider between columns
	divider := NewVerticalDivider()

	rightColumn := tview.NewFlex().SetDirection(tview.FlexRow)
	rightColumn.AddItem(scrollList, 0, 1, true)

	// Main content: 1/3 left, divider, 2/3 right
	contentFlex := tview.NewFlex().SetDirection(tview.FlexColumn)
	contentFlex.AddItem(leftColumn, 0, 1, true)   // 1/3 width
	contentFlex.AddItem(divider, 1, 0, false)     // 1 char divider
	contentFlex.AddItem(rightColumn, 0, 2, false) // 2/3 width

	frame.SetContent(contentFlex)
	pages.AddAndSwitchToPage(PageSearchMedia, frame, true)
}
