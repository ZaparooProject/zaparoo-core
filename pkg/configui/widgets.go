package configui

import (
	"time"
	"encoding/json"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type LoaderArgs struct {
	Text string `json:"text"`
	Timeout int `json:"timeout"`
}

func LoaderUI(args string) error {
	var loaderArgs LoaderArgs
	err := json.Unmarshal([]byte(args), &loaderArgs)
	if err != nil {
		return err
	}

	if loaderArgs.Text == "" {
		loaderArgs.Text = "Loading..."
	}
	
	app := tview.NewApplication()
	setTheme(&tview.Styles)

	view := tview.NewTextView().
		SetText(loaderArgs.Text).
		SetTextAlign(tview.AlignCenter)

	view.SetDrawFunc(func(screen tcell.Screen, x, y, w, h int) (int, int, int, int) {
		y += h / 2
		return x, y, w, h
	})

	frames := []string{"|", "/", "-", "\\"}
	frameIndex := 0
	go func() {
		for {
			app.QueueUpdateDraw(func() {
				view.SetText(frames[frameIndex] + " " + loaderArgs.Text)
			})
			frameIndex = (frameIndex + 1) % len(frames)
			time.Sleep(100 * time.Millisecond)
		}
	}()

	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEsc ||
			event.Rune() == 'q' ||
			event.Key() == tcell.KeyEnter {
			app.Stop()
		}
		return event
	})

	// add a fallback to quit in case dialog gets lost behind a game
	go func() {
		to := 0
		if loaderArgs.Timeout == 0 {
			to = 30
		} else if loaderArgs.Timeout < 0 {
			return
		} else {
			to = loaderArgs.Timeout
		}
		
		time.Sleep(time.Duration(to) * time.Second)
		app.Stop()
	}()

	if err := app.SetRoot(view, true).Run(); err != nil {
		return err
	}

	return nil
}

func PickerUI() {
	items := []string{
		"Option 1",
		"Option 2",
		"Option 3",
		"Option 4",
		"Option 5",
	}

	app := tview.NewApplication()

	setTheme(&tview.Styles)

	list := tview.NewList()

	for _, item := range items {
		list.AddItem(item, "asdf", 0, func() {
			app.Stop()
		})
	}

	go func() {
		time.Sleep(30 * time.Second)
		app.Stop()
	}()

	list.SetDrawFunc(func(screen tcell.Screen, x, y, w, h int) (int, int, int, int) {
		longest := 2
		for _, item := range items {
			if len(item) > longest {
				longest = len(item)
			}
		}
		x += (w - longest) / 2
		y += (h - (list.GetItemCount() * 2)) / 2
		return x, y, w, h
	})

	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEsc || event.Rune() == 'q' {
			app.Stop()
		}
		return event
	})

	if err := app.SetRoot(list, true).Run(); err != nil {
		panic(err)
	}
}
