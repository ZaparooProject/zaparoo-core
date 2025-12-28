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
	"path/filepath"
	"sort"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
)

// truncateSystemName truncates a system name to fit in the left column.
func truncateSystemName(name string) string {
	const maxLen = 18
	if len(name) <= maxLen {
		return name
	}
	return name[:maxLen-3] + "..."
}

// BuildSearchMedia creates the search media page.
func BuildSearchMedia(cfg *config.Instance, pages *tview.Pages, app *tview.Application) {
	// Create page frame
	frame := NewPageFrame(app).
		SetTitle("Search Media").
		SetHelpText("Type query and press Enter to search")

	goBack := func() {
		pages.SwitchToPage(PageMain)
	}
	frame.SetOnEscape(goBack)

	// State variables
	name := ""
	filterSystem := ""
	filterSystemName := "All"
	searching := false

	// Create components
	scrollList := NewScrollIndicatorList()
	mediaList := scrollList.GetList()
	mediaList.SetMainTextColor(tcell.ColorWhite)

	nameLabel := tview.NewTextView().SetText("Name:")

	searchInput := tview.NewInputField()
	searchInput.SetChangedFunc(func(value string) {
		name = value
	})
	setupInputFieldFocus(searchInput)

	systemLabel := tview.NewTextView().SetText("System:")

	// System selector button
	systemButton := tview.NewButton(truncateSystemName(filterSystemName))

	// Load systems for the selector
	var systemItems []SystemItem
	ctx, cancel := tuiContext()
	resp, err := client.LocalClient(ctx, cfg, models.MethodSystems, "")
	cancel()
	if err != nil {
		log.Error().Err(err).Msg("error getting system list")
	} else {
		var results models.SystemsResponse
		err = json.Unmarshal([]byte(resp), &results)
		if err != nil {
			log.Error().Err(err).Msg("error unmarshalling system results")
		} else {
			sort.Slice(results.Systems, func(i, j int) bool {
				return results.Systems[i].Name < results.Systems[j].Name
			})
			systemItems = make([]SystemItem, len(results.Systems))
			for i, v := range results.Systems {
				systemItems[i] = SystemItem{ID: v.ID, Name: v.Name}
			}
		}
	}

	// Function to open system selector modal
	openSystemSelector := func() {
		selectorPage := "search_system_selector"

		layout := tview.NewFlex().SetDirection(tview.FlexRow)
		layout.SetTitle(" Select System ")
		layout.SetBorder(true)

		selector := NewSystemSelector(&SystemSelectorConfig{
			Mode:        SystemSelectorSingle,
			IncludeAll:  true,
			AutoConfirm: true,
			Systems:     systemItems,
			Selected:    []string{filterSystem},
			OnSingle: func(systemID string) {
				filterSystem = systemID
				if filterSystem == "" {
					filterSystemName = "All"
				} else {
					for _, item := range systemItems {
						if item.ID == filterSystem {
							filterSystemName = item.Name
							break
						}
					}
				}
				systemButton.SetLabel(truncateSystemName(filterSystemName))
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

	var writeCancel context.CancelFunc

	// Write tag with modal status
	writeTag := func(value string) {
		writeModalPage := "write_modal"

		ctx, ctxCancel := tagReadContext()
		writeCancel = ctxCancel

		// Create waiting modal
		modal := tview.NewModal().
			SetText("Place tag on the reader...").
			AddButtons([]string{"Cancel"}).
			SetDoneFunc(func(_ int, _ string) {
				if writeCancel != nil {
					writeCancel()
					writeCancel = nil
				}
				_, _ = client.LocalClient(context.Background(), cfg, models.MethodReadersWriteCancel, "")
				pages.RemovePage(writeModalPage)
				app.SetFocus(mediaList)
			})

		pages.AddPage(writeModalPage, modal, true, true)
		app.SetFocus(modal)

		go func() {
			defer func() {
				writeCancel = nil
			}()

			data, err := json.Marshal(&models.ReaderWriteParams{
				Text: value,
			})
			if err != nil {
				log.Error().Err(err).Msg("error marshalling write params")
				app.QueueUpdateDraw(func() {
					pages.RemovePage(writeModalPage)
					showErrorModal(pages, app, "Error: "+err.Error())
				})
				return
			}

			_, err = client.LocalClient(ctx, cfg, models.MethodReadersWrite, string(data))
			if err != nil {
				log.Error().Err(err).Msg("error writing tag")
				app.QueueUpdateDraw(func() {
					pages.RemovePage(writeModalPage)
					showErrorModal(pages, app, "Write failed: "+err.Error())
				})
				return
			}

			app.QueueUpdateDraw(func() {
				pages.RemovePage(writeModalPage)
				successModal := tview.NewModal().
					SetText("Tag written successfully!").
					AddButtons([]string{"OK"}).
					SetDoneFunc(func(_ int, _ string) {
						pages.RemovePage("success_modal")
						app.SetFocus(mediaList)
					})
				pages.AddPage("success_modal", successModal, true, true)
				app.SetFocus(successModal)
			})
		}()
	}

	search := func() {
		if searching {
			return
		}

		params := models.SearchParams{
			Query: &name,
		}

		if filterSystem != "" {
			systems := []string{filterSystem}
			params.Systems = &systems
		}

		payload, err := json.Marshal(params)
		if err != nil {
			log.Error().Err(err).Msg("error marshalling search params")
			frame.SetHelpText("An error occurred during search")
			return
		}

		frame.SetHelpText("Searching...")
		searching = true
		app.ForceDraw()
		defer func() {
			searching = false
		}()

		resp, err := client.LocalClient(context.Background(), cfg, models.MethodMediaSearch, string(payload))
		if err != nil {
			log.Error().Err(err).Msg("error executing search query")
			frame.SetHelpText("An error occurred during search")
			return
		}

		var results models.SearchResults
		err = json.Unmarshal([]byte(resp), &results)
		if err != nil {
			log.Error().Err(err).Msg("error unmarshalling search results")
			frame.SetHelpText("An error occurred during search")
			return
		}

		mediaList.Clear()
		mediaList.SetCurrentItem(0)
		tuiCfg := config.GetTUIConfig()
		for i := range results.Results {
			result := &results.Results[i]
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
			displayText := fmt.Sprintf("%s [gray](%s)[-]", displayName, result.System.Name)
			mediaList.AddItem(displayText, "", 0, func() {
				writeTag(writeValue)
			})
		}

		frame.SetHelpText(fmt.Sprintf("Found %d results. Select to write tag", len(results.Results)))
		if results.Total > 0 {
			app.SetFocus(mediaList)
		}
	}

	// Track last focused left column item for returning from results
	var lastLeftFocus tview.Primitive = searchInput

	// Helper to focus results column, or button bar if no results
	focusResults := func() {
		if mediaList.GetItemCount() > 0 {
			app.SetFocus(mediaList)
		} else {
			frame.FocusButtonBar()
		}
	}

	// Helper to focus left column (returns to last focused item)
	focusLeftColumn := func() {
		app.SetFocus(lastLeftFocus)
	}

	// Navigation: 2-column layout
	// Left column: name → system → search (vertical with Up/Down)
	// Right column: results list
	// Right arrow from system/search → results
	// Left arrow from results → left column
	// Tab/Shift+Tab also switch columns

	searchInput.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEnter:
			search()
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

	// Button bar with Search and Back
	buttonBar := NewButtonBar(app)
	buttonBar.AddButton("Search", search).
		AddButton("Back", goBack).
		SetupNavigation(goBack)
	frame.SetButtonBar(buttonBar)

	// Up from button bar goes to results if any, otherwise left column
	buttonBar.SetOnUp(func() {
		if mediaList.GetItemCount() > 0 {
			app.SetFocus(mediaList)
		} else {
			app.SetFocus(systemButton)
		}
	})
	// Down from button bar wraps to name input (top of left column)
	buttonBar.SetOnDown(func() {
		app.SetFocus(searchInput)
	})
	// Tab wrap from button bar back to search input
	buttonBar.SetOnWrap(func() {
		app.SetFocus(searchInput)
	})
	// Left from button bar goes to results if any, otherwise left column
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
