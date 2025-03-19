package configui

import (
	"encoding/json"
	"slices"
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

type PrimitiveWithSetBorder interface {
	tview.Primitive
	SetBorder(arg bool) *tview.Box
}

func BuildAppAndRetry(
	builder func() (*tview.Application, error),
) error {
	app, err := builder()
	if err != nil {
		return err
	}
	return tryRunApp(app, builder)
}

func centerWidget(width, height int, p tview.Primitive) tview.Primitive {
	return tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().
			SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(p, height, 1, true).
			AddItem(nil, 0, 1, false), width, 1, true).
		AddItem(nil, 0, 1, false)
}

func pageDefaults[S PrimitiveWithSetBorder](name string, pages *tview.Pages, widget S) tview.Primitive {
	widget.SetBorder(true)
	pages.RemovePage(name)
	pages.AddAndSwitchToPage(name, widget, true)
	return widget
}

/*
	DebugLogging bool      `toml:"debug_logging"`
	Audio        Audio     `toml:"audio,omitempty"`
	Readers      Readers   `toml:"readers,omitempty"`
	Scan       ReadersScan      `toml:"scan,omitempty"`
	Systems      Systems   `toml:"systems,omitempty"`
	Launchers    Launchers `toml:"launchers,omitempty"`
	ZapScript    ZapScript `toml:"zapscript,omitempty"`
	Service      Service   `toml:"service,omitempty"`
	Mappings     Mappings  `toml:"mappings,omitempty"`
	Groovy       Groovy    `toml:"groovy:omitempty"`
*/

func BuildMainMenu(cfg *config.Instance, pages *tview.Pages, app *tview.Application, exitFunc func()) *tview.List {
	mainMenu := tview.NewList().
		AddItem("Readers", "Set nfc readers options", '1', func() {
			pages.SwitchToPage("readers")
		}).
		AddItem("Scan mode", "Set scanning options", '2', func() {
			pages.SwitchToPage("scan")
		}).
		AddItem("Manage tags", "Read and write nfc tags", '3', func() {
			pages.SwitchToPage("tags")
		}).
		AddItem("Misc", "Set audio, debug and db options", '4', func() {
			pages.SwitchToPage("misc")
		}).
		AddItem("Index media", "Rebuild the index for the media db", '5', func() {
			pages.SwitchToPage("media")
		}).
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
			resp, _ := client.WaitNotification(cfg, models.NotificationTokensAdded)
			var data models.TokenResponse
			err := json.Unmarshal([]byte(resp), &data)
			if err != nil {
				log.Error().Err(err).Msg("error unmarshalling token")
				return nil
			}
			tagsReadMenu.AddTextView("UID", data.UID, 50, 1, true, false)
			tagsReadMenu.AddTextView("data", data.Data, 50, 1, true, false)
			tagsReadMenu.AddTextView("text", data.Text, 50, 4, true, false)
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

func BuildTagsSearchMenu(cfg *config.Instance, pages *tview.Pages, _ *tview.Application) *tview.Form {
	tagsSearchMenu := tview.NewForm()
	dropdown := tview.NewDropDown()
	tagsSearchMenu.AddInputField("Search param", "", 20, func(value string, lastChar rune) bool {
		var params models.SearchParams
		params.Query = value
		payload, _ := json.Marshal(params)
		resp, _ := client.LocalClient(cfg, models.MethodMediaSearch, string(payload))
		var response models.SearchResults
		json.Unmarshal([]byte(resp), &response)
		for _, result := range response.Results {
			dropdown.AddOption(result.Name, func() {

			})
		}
		return true
	}, func(value string) {})
	tagsSearchMenu.AddFormItem(dropdown)
	pageDefaults("tags_search", pages, tagsSearchMenu)
	return tagsSearchMenu
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
			_, _ = client.LocalClient(cfg, models.MethodReadersWrite, string(data))
			zapScriptTextArea.SetText("", true)
		} else if k == tcell.KeyEscape {
			pages.SwitchToPage("tags")
		}
		return event
	})
	pageDefaults("tags_write", pages, tagsWriteMenu)
	return tagsWriteMenu
}

/*
type Audio struct {
	ScanFeedback bool `toml:"scan_feedback,omitempty"`
}
*/

func BuildMiscMenu(cfg *config.Instance, pages *tview.Pages, app *tview.Application) *tview.Form {
	audioFeedback := cfg.AudioFeedback()
	debugLogging := cfg.DebugLogging()
	audioMenu := tview.NewForm().
		AddCheckbox("Debug Logging", debugLogging, func(checked bool) {
			cfg.SetDebugLogging(checked)
		}).
		AddCheckbox("Audio feedback", audioFeedback, func(checked bool) {
			cfg.SetAudioFeedback(checked)
		}).
		AddButton("Go Back", func() {
			pages.SwitchToPage("mainconfig")
		})
	audioMenu.SetFocus(0)
	audioMenu.SetTitle(" Zaparoo config editor - Misc menu ")
	pageDefaults("misc", pages, audioMenu)
	return audioMenu
}

/*
type Readers struct {
	AutoDetect bool             `toml:"auto_detect"`
	Connect    []ReadersConnect `toml:"connect,omitempty"`
}
*/

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

func BuildMediaIndexMenu(cfg *config.Instance, pages *tview.Pages, _ *tview.Application) *tview.Form {
	allSystems := []string{"All"}
	for _, item := range systemdefs.AllSystems() {
		allSystems = append(allSystems, item.Id)
	}
	mediaIndexMenu := tview.NewForm()
	mediaIndexMenu.AddDropDown("Select a system", allSystems, 0, func(option string, optionIndex int) {

	})
	mediaIndexMenu.AddButton("Start indexing", func() {

	})
	mediaIndexMenu.AddButton("Go Back", func() {
		pages.SwitchToPage("mainconfig")
	})
	mediaIndexMenu.SetFocus(0)
	mediaIndexMenu.SetTitle(" Zaparoo config editor - Media index menu ")
	pageDefaults("media", pages, mediaIndexMenu)
	return mediaIndexMenu
}

/* type ReadersScan struct {
	Mode         string   `toml:"mode"`
	ExitDelay    float32  `toml:"exit_delay,omitempty"`
	IgnoreSystem []string `toml:"ignore_system,omitempty"`
} */

func BuildScanModeMenu(cfg *config.Instance, pages *tview.Pages, app *tview.Application) *tview.Form {

	scanMode := 0
	if cfg.ReadersScan().Mode == config.ScanModeHold {
		scanMode = 1
	}

	scanModes := []string{"Tap", "Hold"}

	allSystems := []string{""}
	for _, item := range systemdefs.AllSystems() {
		allSystems = append(allSystems, item.Id)
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

func SetTheme(theme *tview.Theme) {
	theme.BorderColor = tcell.ColorLightYellow
	theme.PrimaryTextColor = tcell.ColorWhite
	theme.ContrastSecondaryTextColor = tcell.ColorFuchsia
	theme.PrimitiveBackgroundColor = tcell.ColorDarkBlue
	theme.ContrastBackgroundColor = tcell.ColorFuchsia
}

func ConfigUiBuilder(cfg *config.Instance, app *tview.Application, pages *tview.Pages, exitFunc func()) (*tview.Application, error) {

	SetTheme(&tview.Styles)

	BuildMainMenu(cfg, pages, app, exitFunc)
	BuildTagsMenu(cfg, pages, app)
	BuildTagsReadMenu(cfg, pages, app)
	BuildTagsSearchMenu(cfg, pages, app)
	BuildTagsWriteMenu(cfg, pages, app)
	BuildMiscMenu(cfg, pages, app)
	BuildReadersMenu(cfg, pages, app)
	BuildScanModeMenu(cfg, pages, app)
	BuildMediaIndexMenu(cfg, pages, app)

	pages.SwitchToPage("mainconfig")
	centeredPages := centerWidget(70, 20, pages)
	return app.SetRoot(centeredPages, true).EnableMouse(true), nil
}

func ConfigUi(cfg *config.Instance, pl platforms.Platform) error {
	return BuildAppAndRetry(func() (*tview.Application, error) {
		app := tview.NewApplication()
		pages := tview.NewPages()
		exitFunc := func() { app.Stop() }
		return ConfigUiBuilder(cfg, app, pages, exitFunc)
	})
}
