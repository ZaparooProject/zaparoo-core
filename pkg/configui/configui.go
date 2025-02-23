package configui

import (
	"encoding/json"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/database/gamesdb"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
)

type PrimitiveWithSetBorder interface {
	tview.Primitive
	SetBorder(arg bool) *tview.Box
}

func pageDefaults[S PrimitiveWithSetBorder](name string, pages *tview.Pages, widget S) S {
	widget.SetBorder(true)
	widget.SetRect(0, 0, 75, 20)
	pages.RemovePage(name)
	pages.AddAndSwitchToPage(name, widget, false)
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
*/

func BuildMainMenu(cfg *config.Instance, pages *tview.Pages, app *tview.Application) *tview.List {
	pages.RemovePage("main")
	debugLogging := "DISABLED"
	if cfg.DebugLogging() {
		debugLogging = "ENABLED"
	}
	mainMenu := tview.NewList().
		AddItem("Debug Logging", "Change the status of debug logging currently "+debugLogging, '1', func() {
			cfg.SetDebugLogging(!cfg.DebugLogging())
			BuildMainMenu(cfg, pages, app)
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
		AddItem("Systems", "Not implemented yet", '6', func() {
		}).
		AddItem("Launchers", "Not implemented yet", '7', func() {
		}).
		AddItem("ZapScript", "Not implemented yet", '8', func() {
		}).
		AddItem("Service", "Not implemented yet", '9', func() {
		}).
		AddItem("Mappings", "Not implemented yet", '0', func() {
		}).
		AddItem("Save and exit", "Press to save", 's', func() {
			cfg.Save()
			app.Stop()
		}).
		AddItem("Quit Without saving", "Press to exit", 'q', func() {
			app.Stop()
		})
	mainMenu.SetTitle(" Zaparoo config editor - Main menu ")
	mainMenu.SetSecondaryTextColor(tcell.ColorYellow)
	pageDefaults("main", pages, mainMenu)
	return mainMenu
}

func BuildTagsMenu(cfg *config.Instance, pages *tview.Pages, app *tview.Application) *tview.List {

	tagsMenu := tview.NewList().
		AddItem("Read", "Check the content of a tag", '1', func() {
			pages.SwitchToPage("tags_read")
		}).
		AddItem("Write", "Write a tag without running it", '2', func() {
			pages.SwitchToPage("tags_write")
		}).
		AddItem("Go back", "Go back to main menu", 'b', func() {
			pages.SwitchToPage("main")
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
			json.Unmarshal([]byte(resp), &data)
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

func BuildTagsWriteMenu(cfg *config.Instance, pages *tview.Pages, app *tview.Application) *tview.Form {

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
			pages.SwitchToPage("main")
		})
	audioMenu.SetTitle(" Zaparoo config editor - Audio menu ")
	audioMenu.SetSecondaryTextColor(tcell.ColorYellow)
	pageDefaults("audio", pages, audioMenu)
	return audioMenu
}

/*
type Readers struct {
	AutoDetect bool             `toml:"auto_detect"`
	Connect    []ReadersConnect `toml:"connect,omitempty"`
}
*/

func BuildReadersMenu(cfg *config.Instance, pages *tview.Pages, app *tview.Application) *tview.Form {

	autoDetect := cfg.AutoDetect()

	connectionStrings := []string{}
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
			newConnect := []config.ReadersConnect{}
			connStrings := strings.Split(textArea.GetText(), "\n")
			for _, item := range connStrings {
				couple := strings.SplitN(item, ":", 2)
				if len(couple) == 2 {
					newConnect = append(newConnect, config.ReadersConnect{Driver: couple[0], Path: couple[1]})
				}
			}

			cfg.SetReaderConnections(newConnect)
			pages.SwitchToPage("main")
		})

	readersMenu.SetTitle(" Zaparoo config editor - Readers menu ")
	pageDefaults("readers", pages, readersMenu)
	return readersMenu
}

/* type ReadersScan struct {
	Mode         string   `toml:"mode"`
	ExitDelay    float32  `toml:"exit_delay,omitempty"`
	IgnoreSystem []string `toml:"ignore_system,omitempty"`
} */

func BuildScanModeMenu(cfg *config.Instance, pages *tview.Pages, app *tview.Application) *tview.Form {

	scanMode := int(0)
	if cfg.ReadersScan().Mode == config.ScanModeHold {
		scanMode = int(1)
	}

	scanModes := []string{"Tap", "Hold"}

	systems := []string{""}
	for _, item := range gamesdb.AllSystems() {
		systems = append(systems, item.Id)
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
		AddDropDown("Ignore systems", systems, 0, func(option string, optionIndex int) {
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
			pages.SwitchToPage("main")
		})
	scanMenu.SetTitle(" Zaparoo config editor - Scan mode menu ")
	pageDefaults("scan", pages, scanMenu)
	return scanMenu
}

func MisterScreenWorkaround(
	app *tview.Application,
	pl platforms.Platform,
) {
	log.Debug().Msg("checking for mister")
	if pl.Id() != "mister" { // TODO: use a const id for this
		return
	}

	hasTty := false
	if _, err := os.Stat("/dev/tty"); err == nil {
		log.Debug().Msg("running on mister, using /dev/tty")
		hasTty = true
	}

	// on mister, when running from scripts menu, /dev/tty is not available
	if !hasTty || os.Getenv("ZAPAROO_RUN_SCRIPT") == "1" {
		log.Debug().Msg("running on mister, using /dev/tty2")
		tty, err := tcell.NewDevTtyFromDev("/dev/tty2")
		if err != nil {
			panic(err)
		}

		screen, err := tcell.NewTerminfoScreenFromTty(tty)
		if err != nil {
			panic(err)
		}

		app.SetScreen(screen)
	}
}

func SetTheme(theme *tview.Theme) {
	theme.BorderColor = tcell.ColorLightYellow
	theme.PrimaryTextColor = tcell.ColorWhite
	theme.ContrastSecondaryTextColor = tcell.ColorFuchsia
	theme.PrimitiveBackgroundColor = tcell.ColorDarkBlue
	theme.ContrastBackgroundColor = tcell.ColorFuchsia
}

func ConfigUi(cfg *config.Instance, pl platforms.Platform) {
	app := tview.NewApplication()
	pages := tview.NewPages()

	SetTheme(&tview.Styles)

	BuildMainMenu(cfg, pages, app)
	BuildTagsMenu(cfg, pages, app)
	BuildTagsReadMenu(cfg, pages, app)
	BuildTagsWriteMenu(cfg, pages, app)
	BuildAudionMenu(cfg, pages, app)
	BuildReadersMenu(cfg, pages, app)
	BuildScanModeMenu(cfg, pages, app)
	pages.SwitchToPage("main")

	MisterScreenWorkaround(app, pl)

	if err := app.SetRoot(pages, true).EnableMouse(true).Run(); err != nil {
		panic(err)
	}
}
