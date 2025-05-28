package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
)

func BuildMainMenu(cfg *config.Instance, pages *tview.Pages, app *tview.Application, exitFunc func()) *tview.List {
	debugLogging := "DISABLED"
	if cfg.DebugLogging() {
		debugLogging = "ENABLED"
	}
	mainMenu := tview.NewList().
		AddItem("Debug Logging", "Change the status of debug logging currently "+debugLogging, '1', func() {
			cfg.SetDebugLogging(!cfg.DebugLogging())
			BuildMainMenu(cfg, pages, app, exitFunc)
		}).
		AddItem("Audio", "Set audio options like the feedback", '2', func() {
			pages.SwitchToPage("audio")
		}).
		AddItem("Readers", "Set nfc readers options", '3', func() {
			pages.SwitchToPage("readers")
		}).
		AddItem("Scan mode", "Set scanning options", '4', func() {
			pages.SwitchToPage("scan")
		}).
		AddItem("Manage tags", "Read and write nfc tags", '5', func() {
			pages.SwitchToPage("tags")
		}).
		// AddItem("Systems", "Not implemented yet", '6', func() {
		// }).
		// AddItem("Launchers", "Not implemented yet", '7', func() {
		// }).
		// AddItem("ZapScript", "Not implemented yet", '8', func() {
		// }).
		// AddItem("Service", "Not implemented yet", '9', func() {
		// }).
		// AddItem("Mappings", "Not implemented yet", '0', func() {
		// }).
		// AddItem("Groovy", "Not implemented yet", 'g', func() {
		// }).
		AddItem("Save and exit", "Press to save", 's', func() {
			err := cfg.Save()
			if err != nil {
				log.Error().Err(err).Msg("error saving config")
			}
			exitFunc()
		}).
		AddItem("Quit Without saving", "Press to exit", 'q', func() {
			exitFunc()
		})
	mainMenu.SetTitle(" Zaparoo config editor - Main menu ")
	mainMenu.SetSecondaryTextColor(tcell.ColorYellow)
	pageDefaults("mainconfig", pages, mainMenu)
	return mainMenu
}

func BuildTagsMenu(_ *config.Instance, pages *tview.Pages, _ *tview.Application) *tview.List {
	tagsMenu := tview.NewList().
		AddItem("Read", "Check the content of a tag", '1', func() {
			pages.SwitchToPage("tags_read")
		}).
		AddItem("Write", "Write a tag without running it", '2', func() {
			pages.SwitchToPage("tags_write")
		}).
		AddItem("Search", "Search a game and write it", '3', func() {
			pages.SwitchToPage("tags_search")
		}).
		AddItem("Go back", "Go back to main menu", 'b', func() {
			pages.SwitchToPage("mainconfig")
		})
	tagsMenu.SetTitle(" Zaparoo config editor - Tags menu ")
	tagsMenu.SetSecondaryTextColor(tcell.ColorYellow)
	pageDefaults("tags", pages, tagsMenu)
	return tagsMenu
}

func BuildTagsReadMenu(cfg *config.Instance, pages *tview.Pages, app *tview.Application) *tview.Form {
	topTextView := tview.NewTextView().
		SetLabel("").
		SetText("Press Enter to scan a card, Esc to Exit")

	tagsReadMenu := tview.NewForm().
		AddFormItem(topTextView)
	tagsReadMenu.SetTitle(" Zaparoo config editor - Read Tags ")
	tagsReadMenu.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		k := event.Key()
		if k == tcell.KeyEnter {
			// remove all the previous text if any. Add back the instructions
			tagsReadMenu.Clear(false).AddFormItem(topTextView)
			topTextView.SetText("Tap a card to read content")
			// if we don't force a redraw, the waitNotification will keep the thread busy
			// and the app won't update the screen
			app.ForceDraw()
			resp, _ := client.WaitNotification(context.Background(), cfg, models.NotificationTokensAdded)
			var data models.TokenResponse
			err := json.Unmarshal([]byte(resp), &data)
			if err != nil {
				log.Error().Err(err).Msg("error unmarshalling token")
				return nil
			}
			tagsReadMenu.AddTextView("ID", data.UID, 50, 1, true, false)
			tagsReadMenu.AddTextView("Data", data.Data, 50, 1, true, false)
			tagsReadMenu.AddTextView("Value", data.Text, 50, 4, true, false)
			topTextView.SetText("Press Enter to scan another card, Esc to Exit")
		}
		if k == tcell.KeyEscape {
			pages.SwitchToPage("tags")
		}
		return event
	})
	pageDefaults("tags_read", pages, tagsReadMenu)
	return tagsReadMenu
}

func BuildTagsSearchMenu(cfg *config.Instance, pages *tview.Pages, app *tview.Application) {
	mediaList := tview.NewList()
	searchButton := tview.NewButton("Search")
	statusText := tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetText("Enter a name to search and select below to write tag.")
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

	tsm.AddItem(searchInput, 1, 1, true)
	tsm.AddItem(systemDropdown, 1, 1, false)
	tsm.AddItem(tview.NewTextView(), 1, 1, false)

	controls := tview.NewFlex().
		AddItem(tview.NewTextView(), 0, 1, false).
		AddItem(searchButton, 0, 1, true).
		AddItem(tview.NewTextView(), 0, 1, false)
	tsm.AddItem(controls, 1, 1, false)
	tsm.AddItem(statusText, 1, 1, false)
	tsm.AddItem(tview.NewTextView(), 1, 1, false)

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
			pages.SwitchToPage("tags")
		}
		return event
	})

	pageDefaults("tags_search", pages, tsm)
}

func BuildTagsWriteMenu(cfg *config.Instance, pages *tview.Pages, _ *tview.Application) *tview.Form {
	topTextView := tview.NewTextView().
		SetLabel("").
		SetText("Put a card on the reader, type or paste your text record and press enter to write. Esc to exit")
	zapScriptTextArea := tview.NewTextArea().
		SetLabel("ZapScript")

	tagsWriteMenu := tview.NewForm().
		AddFormItem(topTextView).
		AddFormItem(zapScriptTextArea)
	tagsWriteMenu.SetTitle(" Zaparoo config editor - Write Tags ")
	tagsWriteMenu.SetFocus(1)
	tagsWriteMenu.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		k := event.Key()
		if k == tcell.KeyEnter {
			text := zapScriptTextArea.GetText()
			strings.Trim(text, "\r\n ")
			data, _ := json.Marshal(&models.ReaderWriteParams{
				Text: text,
			})
			_, _ = client.LocalClient(context.Background(), cfg, models.MethodReadersWrite, string(data))
			zapScriptTextArea.SetText("", true)
		} else if k == tcell.KeyEscape {
			pages.SwitchToPage("tags")
		}
		return event
	})
	pageDefaults("tags_write", pages, tagsWriteMenu)
	return tagsWriteMenu
}

func BuildAudionMenu(cfg *config.Instance, pages *tview.Pages, app *tview.Application) *tview.List {
	audioFeedback := " "
	if cfg.AudioFeedback() {
		audioFeedback = "X"
	}

	audioMenu := tview.NewList().
		AddItem("["+audioFeedback+"] Audio feedback", "Enable or disable the audio notification on scan", '1', func() {
			cfg.SetAudioFeedback(!cfg.AudioFeedback())
			BuildAudionMenu(cfg, pages, app)
		}).
		AddItem("Go back", "Go back to main menu", 'b', func() {
			pages.SwitchToPage("mainconfig")
		})
	audioMenu.SetTitle(" Zaparoo config editor - Audio menu ")
	audioMenu.SetSecondaryTextColor(tcell.ColorYellow)
	pageDefaults("audio", pages, audioMenu)
	return audioMenu
}

func BuildReadersMenu(cfg *config.Instance, pages *tview.Pages, _ *tview.Application) *tview.Form {
	autoDetect := cfg.AutoDetect()

	var connectionStrings []string
	for _, item := range cfg.Readers().Connect {
		connectionStrings = append(connectionStrings, item.Driver+":"+item.Path)
	}

	textArea := tview.NewTextArea().
		SetLabel("Connection strings (1 per line)").
		SetText(strings.Join(connectionStrings, "\n"), false).
		SetSize(5, 40).
		SetMaxLength(200)

	readersMenu := tview.NewForm()
	readersMenu.AddCheckbox("Autodetect reader", autoDetect, func(checked bool) {
		cfg.SetAutoDetect(checked)
	}).
		AddFormItem(textArea).
		AddButton("Confirm", func() {
			var newConnect []config.ReadersConnect
			connStrings := strings.Split(textArea.GetText(), "\n")
			for _, item := range connStrings {
				couple := strings.SplitN(item, ":", 2)
				if len(couple) == 2 {
					newConnect = append(newConnect, config.ReadersConnect{Driver: couple[0], Path: couple[1]})
				}
			}

			cfg.SetReaderConnections(newConnect)
			pages.SwitchToPage("mainconfig")
		})

	readersMenu.SetTitle(" Zaparoo config editor - Readers menu ")
	pageDefaults("readers", pages, readersMenu)
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
	scanMenu.AddDropDown("Scan Mode", scanModes, scanMode, func(option string, optionIndex int) {
		cfg.SetScanMode(option)
	}).
		AddInputField("Exit Delay", strconv.FormatFloat(float64(exitDelay), 'f', 0, 32), 2, tview.InputFieldInteger, func(value string) {
			delay, _ := strconv.ParseFloat(value, 32)
			cfg.SetScanExitDelay(float32(delay))
		}).
		AddDropDown("Ignore systems", allSystems, 0, func(option string, optionIndex int) {
			currentSystems := cfg.ReadersScan().IgnoreSystem
			if optionIndex > 0 {
				if !slices.Contains(currentSystems, option) {
					newSystems := append(currentSystems, option)
					cfg.SetScanIgnoreSystem(newSystems)
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
		AddButton("Confirm", func() {
			pages.SwitchToPage("mainconfig")
		})
	scanMenu.SetTitle(" Zaparoo config editor - Scan mode menu ")
	pageDefaults("scan", pages, scanMenu)
	return scanMenu
}

func ConfigUiBuilder(cfg *config.Instance, app *tview.Application, pages *tview.Pages, exitFunc func()) (*tview.Application, error) {
	SetTheme(&tview.Styles)

	BuildMainMenu(cfg, pages, app, exitFunc)
	BuildTagsMenu(cfg, pages, app)
	BuildTagsReadMenu(cfg, pages, app)
	BuildTagsSearchMenu(cfg, pages, app)
	BuildTagsWriteMenu(cfg, pages, app)
	BuildAudionMenu(cfg, pages, app)
	BuildReadersMenu(cfg, pages, app)
	BuildScanModeMenu(cfg, pages, app)

	pages.SwitchToPage("mainconfig")
	centeredPages := centerWidget(70, 20, pages)
	return app.SetRoot(centeredPages, true).EnableMouse(true), nil
}

func ConfigUi(cfg *config.Instance, _ platforms.Platform) error {
	return BuildAppAndRetry(func() (*tview.Application, error) {
		app := tview.NewApplication()
		pages := tview.NewPages()
		exitFunc := func() { app.Stop() }
		return ConfigUiBuilder(cfg, app, pages, exitFunc)
	})
}

func BuildTheUi(pl platforms.Platform, running bool, cfg *config.Instance, logDestinationPath string) (*tview.Application, error) {
	app := tview.NewApplication()
	modal := tview.NewModal()
	logExport := tview.NewList()

	var statusText string
	if running {
		statusText = "RUNNING"
	} else {
		statusText = "NOT RUNNING"
	}

	ip := utils.GetLocalIP()
	var ipDisplay string
	if ip == "" {
		ipDisplay = "Unknown"
	} else {
		ipDisplay = ip
	}

	// ugly text for the modal content. sorry.
	text := ""
	text = text + "  Visit zaparoo.org for guides and help!  \n"
	text = text + "──────────────────────────────────────────\n"
	text = text + "  Service:        " + statusText + "\n"
	text = text + "  Device address: " + ipDisplay + "\n"
	text = text + "──────────────────────────────────────────\n"

	pages := tview.NewPages().
		AddPage("main", modal, true, true)

	// create the small log export modal
	logExport.
		AddItem("Upload to termbin.com", "", 'a', func() {
			pages.RemovePage("export")
			outcome := uploadLog(pl, pages, app)
			modal := genericModal(outcome, "Log upload", func(buttonIndex int, buttonLabel string) {
				pages.RemovePage("upload")
			}, true)
			pages.AddPage("upload", modal, true, true)
		})
	if logDestinationPath != "" {
		logExport.AddItem("Copy to SD card", "", 'b', func() {
			pages.RemovePage("export")
			outcome := copyLogToSd(pl, logDestinationPath)
			modal := genericModal(outcome, "Log copy", func(buttonIndex int, buttonLabel string) {
				pages.RemovePage("copy")
			}, true)
			pages.AddPage("copy", modal, true, true)
		})
	}
	logExport.AddItem("Cancel", "", 'q', func() {
		pages.RemovePage("export")
	}).
		ShowSecondaryText(false)
	// Coloring will require some effort
	// SetBackgroundColor(modal.GetBackgroundColor())
	logExport.
		SetBorder(true).
		SetBorderPadding(1, 1, 1, 1).
		SetTitle("Log export")

	// create the main modal
	modal.SetTitle("Zaparoo Core v" + config.AppVersion + " (" + pl.ID() + ")").
		SetBorder(true).
		SetTitleAlign(tview.AlignCenter)
	modal.SetText(text).
		AddButtons([]string{"Config", "Export log", "Exit"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			if buttonLabel == "Exit" {
				app.Stop()
			}
			if buttonLabel == "Config" {
				enabler := client.PauseZapScript(cfg)
				ConfigUiBuilder(cfg, app, pages, func() {
					enabler()
					pages.SwitchToPage("main")
				})
			}
			if buttonLabel == "Export log" {
				widget := modalBuilder(logExport, 42, 8)
				pages.AddPage("export", widget, true, true)
			}
		})

	return app.SetRoot(pages, true).EnableMouse(true), nil
}
