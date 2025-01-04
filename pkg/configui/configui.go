package configui

import (
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

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
	debugLogging := " "
	if cfg.DebugLogging() {
		debugLogging = "X"
	}
	mainMenu := tview.NewList().
		AddItem("["+debugLogging+"] Debug Logging", "Change the status of debug logging", '1', func() {
			cfg.SetDebugLogging(!cfg.DebugLogging())
			pages.RemovePage("main")
			pages.AddAndSwitchToPage("main", BuildMainMenu(cfg, pages, app), true)
		}).
		AddItem("Audio", "Set audio options like the feedback", '2', func() {
			pages.SwitchToPage("audio")
		}).
		AddItem("Quit", "Press to exit", 'q', func() {
			app.Stop()
		})
	mainMenu.SetBorder(true)
	mainMenu.SetTitle(" Zaparoo config editor - Main menu ")
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
			pages.RemovePage("audio")
			pages.AddAndSwitchToPage("audio", BuildAudionMenu(cfg, pages, app), true)
		}).
		AddItem("Go back", "Go back to main menu", 'b', func() {
			pages.SwitchToPage("main")
		})
	audioMenu.SetBorder(true)
	audioMenu.SetTitle(" Zaparoo config editor - Audio menu ")
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
			pages.RemovePage("readers")
			pages.AddAndSwitchToPage("readers", BuildReadersMenu(cfg, pages, app), true)
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
	readersMenu.SetBorder(true)
	readersMenu.SetTitle(" Zaparoo config editor - Readers menu ")
	return readersMenu
}

/* type ReadersScan struct {
	Mode         string   `toml:"mode"`
	ExitDelay    float32  `toml:"exit_delay,omitempty"`
	IgnoreSystem []string `toml:"ignore_system,omitempty"`
} */

func BuildScanModeMenu(cfg *config.Instance, pages *tview.Pages, app *tview.Application) *tview.List {
	scanMode := config.ScanModeTap
	if cfg.ReadersScan().Mode == config.ScanModeHold {
		scanMode = config.ScanModeHold
	}

	readersMenu := tview.NewList().
		AddItem("ScanMode ["+scanMode+"]", "Enable or disable the auto detection for readers", '1', func() {
			cfg.SetAutoDetect(!cfg.AutoDetect())
			pages.RemovePage("readers")
			pages.AddAndSwitchToPage("readers", BuildReadersMenu(cfg, pages, app), true)
		}).
		AddItem("Exit delay", "Enter scan mode sub menu", '2', func() {
			pages.SwitchToPage("scan")
		}).
		AddItem("Ignore system", "Input each device's connection string", '3', func() {
			pages.SwitchToPage("connect")
		}).
		AddItem("Go back", "Go back to main menu", 'b', func() {
			pages.SwitchToPage("main")
		})
	readersMenu.SetBorder(true)
	readersMenu.SetTitle(" Zaparoo config editor - Readers menu ")
	return readersMenu
}

func ConfigUi(cfg *config.Instance, pl platforms.Platform) {
	app := tview.NewApplication()
	pages := tview.NewPages()

	pages.AddAndSwitchToPage(
		"main",
		BuildMainMenu(cfg, pages, app),
		true,
	)

	pages.AddPage(
		"audio",
		BuildAudionMenu(cfg, pages, app),
		true,
		false,
	)

	pages.AddPage(
		"readers",
		BuildReadersMenu(cfg, pages, app),
		true,
		false,
	)

	pages.AddPage(
		"scan",
		BuildScanModeMenu(cfg, pages, app),
		true,
		false,
	)

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
