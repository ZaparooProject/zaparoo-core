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
	debugLogging := "Enable"
	if cfg.DebugLogging() {
		debugLogging = "Disable"
	}

	mainMenu := tview.NewList().
		AddItem("Manage NFC tags", "Read and write NFC tags", '1', func() {
			pages.SwitchToPage(PageSettingsTags)
		}).
		AddItem("Scanning", "Manage reader scan behavior", '2', func() {
			pages.SwitchToPage(PageSettingsScanMode)
		}).
		AddItem("Readers", "Manage connected readers", '3', func() {
			pages.SwitchToPage(PageSettingsReaders)
		}).
		AddItem("Audio", "Set audio options", '4', func() {
			pages.SwitchToPage(PageSettingsAudio)
		}).
		AddItem("Debug", debugLogging+" debug logging mode", '5', func() {
			cfg.SetDebugLogging(!cfg.DebugLogging())
			BuildSettingsMainMenu(cfg, pages, app)
		}).
		AddItem("Save", "Save changes to config file", 's', func() {
			err := cfg.Save()
			if err != nil {
				log.Error().Err(err).Msg("error saving config")
			}
		}).
		AddItem("Go back", "Back to main menu", 'b', func() {
			pages.SwitchToPage(PageMain)
		})

	mainMenu.SetTitle("Settings")
	mainMenu.SetSecondaryTextColor(tcell.ColorYellow)

	pageDefaults(PageSettingsMain, pages, mainMenu)
	return mainMenu
}

func BuildTagsMenu(_ *config.Instance, pages *tview.Pages, _ *tview.Application) *tview.List {
	tagsMenu := tview.NewList().
		AddItem("Read", "Check the content of a tag", '1', func() {
			pages.SwitchToPage(PageSettingsTagsRead)
		}).
		AddItem("Write", "Write a tag without running it", '2', func() {
			pages.SwitchToPage(PageSettingsTagsWrite)
		}).
		AddItem("Go back", "Back to settings menu", 'b', func() {
			pages.SwitchToPage(PageSettingsMain)
		})

	tagsMenu.SetTitle("Settings - NFC Tags")
	tagsMenu.SetSecondaryTextColor(tcell.ColorYellow)

	pageDefaults(PageSettingsTags, pages, tagsMenu)
	return tagsMenu
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
			topTextView.SetText("Press Enter to scan another card, Esc to Exit")
		}
		if k == tcell.KeyEscape {
			pages.SwitchToPage(PageSettingsTags)
		}
		return event
	})

	pageDefaults(PageSettingsTagsRead, pages, tagsReadMenu)
	return tagsReadMenu
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

	tagsWriteMenu.SetTitle("Settings - NFC Tags - Write")
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
			pages.SwitchToPage(PageSettingsTags)
		}
		return event
	})

	pageDefaults(PageSettingsTagsWrite, pages, tagsWriteMenu)
	return tagsWriteMenu
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
		AddItem("Go back", "Go back to main menu", 'b', func() {
			pages.SwitchToPage(PageSettingsMain)
		})

	audioMenu.SetTitle("Settings - Audio")
	audioMenu.SetSecondaryTextColor(tcell.ColorYellow)

	pageDefaults(PageSettingsAudio, pages, audioMenu)
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
	readersMenu.AddCheckbox("Auto-detect readers", autoDetect, func(checked bool) {
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
			pages.SwitchToPage(PageSettingsMain)
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
	scanMenu.AddDropDown("Scan mode", scanModes, scanMode, func(option string, optionIndex int) {
		cfg.SetScanMode(strings.ToLower(option))
	}).
		AddInputField("Exit delay", strconv.FormatFloat(float64(exitDelay), 'f', 0, 32), 2, tview.InputFieldInteger, func(value string) {
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
			pages.SwitchToPage(PageSettingsMain)
		})

	scanMenu.SetTitle("Settings - Scanning")

	pageDefaults(PageSettingsScanMode, pages, scanMenu)
	return scanMenu
}
