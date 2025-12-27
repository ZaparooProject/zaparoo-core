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

func BuildSearchMedia(cfg *config.Instance, pages *tview.Pages, app *tview.Application) {
	mediaList := tview.NewList()
	searchButton := tview.NewButton("Search")
	statusText := tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetText("Search and select media to write. ESC to exit.")

	name := ""
	filterSystem := ""
	filterSystemName := "All"
	searching := false

	tsm := tview.NewFlex()
	tsm.SetTitle("Search Media")
	tsm.SetDirection(tview.FlexRow)

	searchInput := tview.NewInputField()
	searchInput.SetLabel("Name")
	searchInput.SetLabelWidth(7)
	searchInput.SetChangedFunc(func(value string) {
		name = value
	})
	setupInputFieldFocus(searchInput)

	// System selector: label + button
	systemLabel := tview.NewTextView().SetText("System ")
	systemButton := tview.NewButton(filterSystemName)

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
		layout.SetTitle("Select System")
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
				systemButton.SetLabel(filterSystemName)
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

		pages.AddPage(selectorPage, CenterWidget(40, 12, layout), true, true)
		app.SetFocus(selector)
	}

	mediaList.SetWrapAround(false)
	mediaList.SetSelectedFocusOnly(true)

	searchInput.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		k := event.Key()
		switch k {
		case tcell.KeyTab, tcell.KeyDown:
			app.SetFocus(systemButton)
			return nil
		case tcell.KeyBacktab, tcell.KeyUp:
			if mediaList.GetItemCount() > 0 {
				mediaList.SetCurrentItem(-1)
				app.SetFocus(mediaList)
			} else {
				app.SetFocus(searchButton)
			}
			return nil
		case tcell.KeyEnter:
			app.SetFocus(searchButton)
			return nil
		case tcell.KeyEscape:
			pages.SwitchToPage(PageMain)
			return nil
		default:
			return event
		}
	})
	systemButton.SetSelectedFunc(openSystemSelector)
	systemButton.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		k := event.Key()
		switch k {
		case tcell.KeyTab, tcell.KeyDown:
			app.SetFocus(searchButton)
			return nil
		case tcell.KeyBacktab, tcell.KeyUp:
			app.SetFocus(searchInput)
			return nil
		case tcell.KeyEscape:
			pages.SwitchToPage(PageMain)
			return nil
		default:
			return event
		}
	})
	searchButton.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		k := event.Key()
		switch k {
		case tcell.KeyTab, tcell.KeyDown:
			if mediaList.GetItemCount() > 0 {
				mediaList.SetCurrentItem(0)
				app.SetFocus(mediaList)
			} else {
				app.SetFocus(searchInput)
			}
			return nil
		case tcell.KeyBacktab, tcell.KeyUp:
			app.SetFocus(systemButton)
			return nil
		case tcell.KeyEscape:
			pages.SwitchToPage(PageMain)
			return nil
		default:
			return event
		}
	})
	mediaList.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		k := event.Key()
		switch {
		case k == tcell.KeyEscape:
			pages.SwitchToPage(PageMain)
			return nil
		case k == tcell.KeyTab:
			app.SetFocus(searchInput)
			return nil
		case k == tcell.KeyBacktab:
			app.SetFocus(searchButton)
			return nil
		case k == tcell.KeyUp && mediaList.GetCurrentItem() == 0:
			app.SetFocus(searchButton)
			return nil
		case k == tcell.KeyDown && mediaList.GetCurrentItem() == mediaList.GetItemCount()-1:
			app.SetFocus(searchInput)
			return nil
		}
		return event
	})

	searchArea := tview.NewFlex()
	searchArea.SetDirection(tview.FlexColumn)

	searchForm := tview.NewFlex()
	searchForm.SetDirection(tview.FlexRow)

	systemRow := tview.NewFlex().SetDirection(tview.FlexColumn)
	systemRow.AddItem(systemLabel, 7, 0, false)
	systemRow.AddItem(systemButton, 0, 1, false)

	searchForm.AddItem(searchInput, 1, 1, true)
	searchForm.AddItem(systemRow, 1, 1, false)

	searchArea.AddItem(searchForm, 0, 2, true)
	searchArea.AddItem(statusText, 0, 1, false)

	tsm.AddItem(searchArea, 2, 1, true)

	controls := tview.NewFlex().
		AddItem(tview.NewTextView(), 0, 1, false).
		AddItem(searchButton, 0, 1, true).
		AddItem(tview.NewTextView(), 0, 1, false)
	tsm.AddItem(controls, 1, 1, false)

	mediaPages := tview.NewPages()

	writeModal := tview.NewModal().
		AddButtons([]string{"Cancel"}).
		SetText("Place tag on reader...")

	successModal := tview.NewModal().
		AddButtons([]string{"OK"}).
		SetText("Tag written successfully.").
		SetDoneFunc(func(_ int, _ string) {
			mediaPages.SwitchToPage("media_list")
			app.SetFocus(mediaList)
		})

	errorModal := tview.NewModal().
		AddButtons([]string{"OK"}).
		SetText("Error writing to tag.").
		SetDoneFunc(func(_ int, _ string) {
			mediaPages.SwitchToPage("media_list")
			app.SetFocus(mediaList)
		})

	mediaPages.AddPage("media_list", mediaList, true, true)
	mediaPages.AddPage("write_modal", writeModal, true, false)
	mediaPages.AddPage("success_modal", successModal, true, false)
	mediaPages.AddPage("error_modal", errorModal, true, false)

	tsm.AddItem(mediaPages, 0, 1, false)

	writeTag := func(value string) {
		ctx, cancel := tagReadContext()
		writeModal.SetDoneFunc(func(_ int, _ string) {
			log.Info().Msg("user cancelled write")
			cancel()
			_, err := client.LocalClient(context.Background(), cfg, models.MethodReadersWriteCancel, "")
			if err != nil {
				log.Error().Err(err).Msg("error cancelling write")
			}
			mediaPages.SwitchToPage("media_list")
			app.SetFocus(mediaList)
		})

		mediaPages.ShowPage("write_modal")
		app.SetFocus(writeModal)

		go func() {
			defer cancel()

			data, err := json.Marshal(&models.ReaderWriteParams{
				Text: value,
			})
			if err != nil {
				log.Error().Err(err).Msg("error marshalling write params")
				app.QueueUpdateDraw(func() {
					errorModal.SetText("Error writing to tag.")
					mediaPages.HidePage("write_modal")
					mediaPages.ShowPage("error_modal")
					app.SetFocus(errorModal)
				})
				return
			}

			_, err = client.LocalClient(ctx, cfg, models.MethodReadersWrite, string(data))
			if err != nil {
				log.Error().Err(err).Msg("error writing tag")
				app.QueueUpdateDraw(func() {
					errorModal.SetText("Error writing to tag:\n" + err.Error())
					mediaPages.HidePage("write_modal")
					mediaPages.ShowPage("error_modal")
					app.SetFocus(errorModal)
				})
				return
			}

			app.QueueUpdateDraw(func() {
				mediaPages.HidePage("write_modal")
				mediaPages.ShowPage("success_modal")
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
			statusText.SetText("An error occurred during search.")
			return
		}

		searchButton.SetLabel("Searching...")
		searching = true
		app.ForceDraw()
		defer func() {
			searchButton.SetLabel("Search")
			searching = false
		}()

		resp, err := client.LocalClient(context.Background(), cfg, models.MethodMediaSearch, string(payload))
		if err != nil {
			log.Error().Err(err).Msg("error executing search query")
			statusText.SetText("An error occurred during search.")
			return
		}

		var results models.SearchResults
		err = json.Unmarshal([]byte(resp), &results)
		if err != nil {
			log.Error().Err(err).Msg("error unmarshalling search results")
			statusText.SetText("An error occurred during search.")
			return
		}

		mediaList.Clear()
		mediaList.SetCurrentItem(0)
		tuiCfg := config.GetTUIConfig()
		for i := range results.Results {
			result := &results.Results[i]
			var displayName, writeValue string
			if tuiCfg.WriteFormat == "path" {
				base := filepath.Base(result.Path)
				ext := filepath.Ext(base)
				displayName = strings.TrimSuffix(base, ext)
				writeValue = result.Path
			} else {
				displayName = result.Name
				writeValue = result.ZapScript
			}
			mediaList.AddItem(displayName, result.System.Name, 0, func() {
				writeTag(writeValue)
			})
		}

		statusText.SetText(fmt.Sprintf("Found %d results.", len(results.Results)))
		app.SetFocus(mediaList)
	}

	searchButton.SetSelectedFunc(search)

	pageDefaults(PageSearchMedia, pages, tsm)
}
