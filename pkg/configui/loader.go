package configui

import (
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func LoaderUI(text string) {
	app := tview.NewApplication()

	setTheme()

	if text == "" {
		text = "Loading..."
	}

	loader := tview.NewTextView().
		SetText(text).
		SetTextAlign(tview.AlignCenter)

	loader.SetDrawFunc(func(screen tcell.Screen, x, y, w, h int) (int, int, int, int) {
		y += h / 2
		return x, y, w, h
	})

	frames := []string{"|", "/", "-", "\\"}
	frameIndex := 0

	go func() {
		for {
			app.QueueUpdateDraw(func() {
				loader.SetText(frames[frameIndex] + " " + text)
			})
			frameIndex = (frameIndex + 1) % len(frames)
			time.Sleep(100 * time.Millisecond)
		}
	}()

	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEsc ||
			event.Rune() == 'q' ||
			event.Rune() == ' ' ||
			event.Key() == tcell.KeyEnter {
			app.Stop()
		}
		return event
	})

	// add a fallback to quit in case dialog gets lost behind a game
	go func() {
		time.Sleep(30 * time.Second)
		app.Stop()
	}()

	if err := app.SetRoot(loader, true).Run(); err != nil {
		panic(err)
	}
}
