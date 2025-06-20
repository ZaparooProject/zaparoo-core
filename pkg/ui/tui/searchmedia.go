package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/ZaparooProject/zaparoo-core/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
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
	systemDropdown := tview.NewDropDown()

	name := ""
	filterSystem := ""
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

	systemDropdown.SetLabel("System")
	systemDropdown.AddOption("All", func() {
		filterSystem = ""
	})
	systemDropdown.SetLabelWidth(7)

	resp, err := client.LocalClient(context.Background(), cfg, models.MethodSystems, "")
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
			for _, v := range results.Systems {
				systemDropdown.AddOption(v.Name, func() {
					filterSystem = v.Id
				})
			}
		}
	}

	systemDropdown.SetCurrentOption(0)
	systemDropdown.SetFieldWidth(0)

	mediaList.SetWrapAround(false)
	mediaList.SetSelectedFocusOnly(true)

	searchInput.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		k := event.Key()
		if k == tcell.KeyTab || k == tcell.KeyDown {
			app.SetFocus(systemDropdown)
			return nil
		} else if k == tcell.KeyBacktab || k == tcell.KeyUp {
			if mediaList.GetItemCount() > 0 {
				mediaList.SetCurrentItem(-1)
				app.SetFocus(mediaList)
			} else {
				app.SetFocus(searchButton)
			}
			return nil
		} else if k == tcell.KeyEnter {
			app.SetFocus(searchButton)
		}
		return event
	})
	systemDropdown.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if systemDropdown.IsOpen() {
			return event
		}
		k := event.Key()
		if k == tcell.KeyTab || k == tcell.KeyRight || k == tcell.KeyDown {
			app.SetFocus(searchButton)
			return nil
		} else if k == tcell.KeyBacktab || k == tcell.KeyLeft || k == tcell.KeyUp {
			app.SetFocus(searchInput)
			return nil
		}
		return event
	})
	searchButton.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		k := event.Key()
		if k == tcell.KeyTab || k == tcell.KeyRight || k == tcell.KeyDown {
			if mediaList.GetItemCount() > 0 {
				mediaList.SetCurrentItem(0)
				app.SetFocus(mediaList)
			} else {
				app.SetFocus(searchInput)
			}
			return nil
		} else if k == tcell.KeyBacktab || k == tcell.KeyUp || k == tcell.KeyLeft {
			app.SetFocus(systemDropdown)
			return nil
		}
		return event
	})
	mediaList.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		k := event.Key()
		if k == tcell.KeyRight {
			app.SetFocus(searchInput)
			return nil
		} else if k == tcell.KeyLeft {
			app.SetFocus(searchButton)
			return nil
		} else if k == tcell.KeyUp && mediaList.GetCurrentItem() == 0 {
			app.SetFocus(searchButton)
			return nil
		} else if k == tcell.KeyDown && mediaList.GetCurrentItem() == mediaList.GetItemCount()-1 {
			app.SetFocus(searchInput)
			return nil
		}
		return event
	})

	searchArea := tview.NewFlex()
	searchArea.SetDirection(tview.FlexColumn)

	searchForm := tview.NewFlex()
	searchForm.SetDirection(tview.FlexRow)

	searchForm.AddItem(searchInput, 1, 1, true)
	searchForm.AddItem(systemDropdown, 1, 1, false)

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
		ctx, cancel := context.WithCancel(context.Background())
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
			data, err := json.Marshal(&models.ReaderWriteParams{
				Text: value,
			})
			if err != nil {
				log.Error().Err(err).Msg("error marshalling write params")
				errorModal.SetText("Error writing to tag.")
				mediaPages.HidePage("write_modal")
				mediaPages.ShowPage("error_modal")
				app.SetFocus(errorModal).ForceDraw()
				return
			}

			_, err = client.LocalClient(ctx, cfg, models.MethodReadersWrite, string(data))
			if err != nil {
				log.Error().Err(err).Msg("error writing tag")
				errorModal.SetText("Error writing to tag:\n" + err.Error())
				mediaPages.HidePage("write_modal")
				mediaPages.ShowPage("error_modal")
				app.SetFocus(errorModal).ForceDraw()
				return
			}

			mediaPages.HidePage("write_modal")
			mediaPages.ShowPage("success_modal")
			app.SetFocus(successModal).ForceDraw()
		}()
	}

	search := func() {
		if searching {
			return
		}

		params := models.SearchParams{
			Query: name,
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
		for _, result := range results.Results {
			mediaList.AddItem(result.Name, result.System.Name, 0, func() {
				writeTag(result.Path)
			})
		}

		statusText.SetText(fmt.Sprintf("Found %d results.", len(results.Results)))
		app.SetFocus(mediaList)
	}

	searchButton.SetSelectedFunc(search)

	tsm.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		k := event.Key()
		if k == tcell.KeyEscape && !systemDropdown.IsOpen() {
			pages.SwitchToPage(PageMain)
		}
		return event
	})

	pageDefaults(PageSearchMedia, pages, tsm)
}
