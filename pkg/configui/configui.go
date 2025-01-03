package configui

import (
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

func ConfigUi() {
	pages := tview.NewPages()
	app := tview.NewApplication()
	mainMenu := tview.NewList().
		AddItem("Debug Logging", "Change the status of debug logging", 'a', nil).
		AddItem("Audio", "Set audio options ex: feedback", 'b', func() {
			pages.SwitchToPage("audio")
		}).
		AddItem("Quit", "Press to exit", 'q', func() {
			app.Stop()
		}).
		SetBorder(true).
		SetTitle(" Zaparoo config editor - Main menu ")

	audioMenu := tview.NewList().
		AddItem("Scan feedback", "Enable or disable the audio notification on scan", 'a', nil).
		AddItem("Back", "Go back to main menu", 'q', func() {
			pages.SwitchToPage("main")
		}).
		SetBorder(true).
		SetTitle(" Zaparoo config editor - Audio menu ")

	pages.AddAndSwitchToPage(
		"main",
		mainMenu,
		true,
	)
	pages.AddPage(
		"audio",
		audioMenu,
		true,
		false,
	)
	if err := app.SetRoot(pages, true).EnableMouse(true).Run(); err != nil {
		panic(err)
	}
}
