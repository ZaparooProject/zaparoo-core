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
	app := tview.NewApplication()
	pages := tview.NewPages()

	mainMenu := tview.NewList().
		AddItem("Debug Logging", "Change the status of debug logging", '1', func() {

		}).
		AddItem("Audio", "Set audio options like the feedback", '2', func() {
			pages.SwitchToPage("audio")
		}).
		AddItem("Quit", "Press to exit", 'q', func() {
			app.Stop()
		})
	mainMenu.SetBorder(true)
	mainMenu.SetTitle(" Zaparoo config editor - Main menu ")

	audioMenu := tview.NewList().
		AddItem("Audio feedback", "Enable or disable the audio notification on scan", '1', func() {

		}).
		AddItem("Go back", "Go back to main menu", 'b', func() {
			pages.SwitchToPage("main")
		})
	audioMenu.SetBorder(true)
	audioMenu.SetTitle(" Zaparoo config editor - Audio menu ")

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
