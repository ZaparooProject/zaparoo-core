package configui

import (
	"strconv"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type PrimitiveWithSetBorder interface {
	tview.Primitive
	SetBorder(arg bool) *tview.Box
}

func pageDefaults[S PrimitiveWithSetBorder](name string, pages *tview.Pages, widget S) S {
	widget.SetBorder(true)
	widget.SetRect(0, 0, 80, 25)
	pages.RemovePage(name)
	pages.AddAndSwitchToPage(name, widget, false)
	return widget
}

/*
	DebugLogging bool      `toml:"debug_logging"`
	Audio        Audio     `toml:"audio,omitempty"`
	Readers      Readers   `toml:"readers,omitempty"`
	Systems      Systems   `toml:"systems,omitempty"`
	Launchers    Launchers `toml:"launchers,omitempty"`
	ZapScript    ZapScript `toml:"zapscript,omitempty"`
	Service      Service   `toml:"service,omitempty"`
	Mappings     Mappings  `toml:"mappings,omitempty"`
*/

func BuildMainMenu(cfg *config.Instance, pages *tview.Pages, app *tview.Application) *tview.List {
	pages.RemovePage("main")
	debugLogging := " "
	if cfg.DebugLogging() {
		debugLogging = "x"
	}
	mainMenu := tview.NewList().
		AddItem("["+debugLogging+"] Debug Logging", "Change the status of debug logging", '1', func() {
			cfg.SetDebugLogging(!cfg.DebugLogging())
			BuildMainMenu(cfg, pages, app)
		}).
		AddItem("Audio", "Set audio options like the feedback", '2', func() {
			pages.SwitchToPage("audio")
		}).
		AddItem("Readers", "Set nfc readers options", '3', func() {
			pages.SwitchToPage("readers")
		}).
		AddItem("Quit", "Press to exit", 'q', func() {
			app.Stop()
		})
	mainMenu.SetTitle(" Zaparoo config editor - Main menu ")
	pageDefaults("main", pages, mainMenu)
	return mainMenu
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
	pageDefaults("audio", pages, audioMenu)
	return audioMenu
}

/*
type Readers struct {
	AutoDetect bool             `toml:"auto_detect"`
	Scan       ReadersScan      `toml:"scan,omitempty"`
	Connect    []ReadersConnect `toml:"connect,omitempty"`
}
*/

func BuildReadersMenu(cfg *config.Instance, pages *tview.Pages, app *tview.Application) *tview.List {
	autoDetect := " "
	if cfg.AutoDetect() {
		autoDetect = "X"
	}

	readersMenu := tview.NewList().
		AddItem("["+autoDetect+"] Auto detect", "Enable or disable the auto detection for readers", '1', func() {
			cfg.SetAutoDetect(!cfg.AutoDetect())
			BuildReadersMenu(cfg, pages, app)
		}).
		AddItem("Scan mode", "Enter scan mode sub menu", '2', func() {
			pages.SwitchToPage("scan")
		}).
		AddItem("Connection strings", "Input each device's connection string", '3', func() {
			pages.SwitchToPage("connect")
		}).
		AddItem("Go back", "Go back to main menu", 'b', func() {
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

	// scanMode := int(0)
	// if cfg.ReadersScan().Mode == config.ScanModeHold {
	// 	scanMode = int(1)
	// }

	scanMenu := tview.NewForm().
		// AddDropDown("Scan Mode", []string{"Tap", "Hold"}, scanMode, func(option string, optionIndex int) {
		// 	cfg.SetScanMode(option)
		// 	pages.RemovePage("scan")
		// 	pages.AddAndSwitchToPage(
		// 		"scan",
		// 		BuildScanModeMenu(cfg, pages, app),
		// 		true,
		// 	)
		// }).
		AddInputField("Exit Delay", "1", 2, tview.InputFieldInteger, func(value string) {
			delay, _ := strconv.ParseFloat(value, 32)
			cfg.SetScanExitDelay(float32(delay))
			pages.RemovePage("scan")
			pages.AddAndSwitchToPage(
				"scan",
				BuildScanModeMenu(cfg, pages, app),
				true,
			)
		})
	// // AddDropDown("Ignore systems", []string{"Nes", "Snes"}, 0, func(option string, optionIndex int) {
	// // 	cfg.SetScanIgnoreSystem(append(cfg.ReadersScan().IgnoreSystem, option))
	// // 	pages.RemovePage("scan")
	// // 	pages.AddAndSwitchToPage(
	// // 		"scan",
	// // 		BuildScanModeMenu(cfg, pages, app),
	// // 		true,
	// // 	)
	// // }).
	// // AddTextArea("Ignored", strings.Join(cfg.ReadersScan().IgnoreSystem, ", "), 30, 10, 0, nil).
	// // AddButton("Back", func() {
	// // 	pages.SwitchToPage("main")
	// // })
	scanMenu.SetTitle(" Zaparoo config editor - Scan mode menu ")
	pageDefaults("scan", pages, scanMenu)
	return scanMenu
}

func ConfigUi(cfg *config.Instance, pl platforms.Platform) {
	app := tview.NewApplication()
	pages := tview.NewPages()

	BuildMainMenu(cfg, pages, app)
	BuildAudionMenu(cfg, pages, app)
	BuildReadersMenu(cfg, pages, app)
	BuildScanModeMenu(cfg, pages, app)
	pages.SwitchToPage("main")

	if pl.Id() == "mister" {
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

	if err := app.SetRoot(pages, true).EnableMouse(true).Run(); err != nil {
		panic(err)
	}
}
