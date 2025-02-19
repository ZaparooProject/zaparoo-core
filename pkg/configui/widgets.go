package configui

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
)

const DefaultTimeout = 30 // seconds

// handleTimeout adds a background timer which quits the app once ended. It's
// used to make sure there aren't hanging processes running in the background
// if a core gets loaded while it's open.
func handleTimeout(app *tview.Application, timeout int) (*time.Timer, int) {
	to := 0
	if timeout == 0 {
		to = DefaultTimeout
	} else if timeout < 0 {
		// no timeout
		return nil, -1
	} else {
		to = timeout
	}

	timer := time.AfterFunc(time.Duration(to)*time.Second, func() {
		app.Stop()
	})

	return timer, to
}

type LoaderArgs struct {
	Text    string `json:"text"`
	Timeout int    `json:"timeout"`
}

// LoaderUI is a simple TUI screen that indicates something is happening to the
// user. The text displayed can be customized with the text field.
func LoaderUI(pl platforms.Platform, args string) error {
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

	handleTimeout(app, loaderArgs.Timeout)

	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEsc ||
			event.Rune() == 'q' ||
			event.Key() == tcell.KeyEnter {
			app.Stop()
		}
		return event
	})

	misterScreenWorkaround(app, pl)

	if err := app.SetRoot(view, true).Run(); err != nil {
		return err
	}

	return nil
}

type PickerAction struct {
	ZapScript string  `json:"zapscript"`
	Label     *string `json:"label"`
}

type PickerArgs struct {
	Actions []PickerAction `json:"actions"`
	Title   string         `json:"title"`
	Timeout int            `json:"timeout"`
	Trusted *bool          `json:"trusted"`
}

// PickerUI displays a list picker of ZapScript to run via the API. Each action
// can have an optional label.
func PickerUI(cfg *config.Instance, pl platforms.Platform, args string) error {
	var pickerArgs PickerArgs
	err := json.Unmarshal([]byte(args), &pickerArgs)
	if err != nil {
		return err
	}

	if len(pickerArgs.Actions) < 1 {
		return errors.New("no actions were specified")
	}

	app := tview.NewApplication()
	setTheme(&tview.Styles)

	run := func(zapscript string) {
		log.Info().Msgf("running picker zapscript: %s", zapscript)
		zs := zapscript
		apiArgs := models.RunParams{
			Text: &zs,
		}
		ps, err := json.Marshal(apiArgs)
		if err != nil {
			log.Error().Err(err).Msg("error creating run params")
		}
		_, err = client.LocalClient(cfg, "run", string(ps))
		if err != nil {
			log.Error().Err(err).Msg("error running local client")
		}
		app.Stop()
	}

	flex := tview.NewFlex().SetDirection(tview.FlexRow)

	title := pickerArgs.Title
	if title == "" {
		title = "Select Action"
	}

	titleText := tview.NewTextView().
		SetText(title).
		SetTextAlign(tview.AlignCenter)
	padding := tview.NewTextView()
	list := tview.NewList()

	flex.AddItem(titleText, 1, 0, false)
	flex.AddItem(padding, 1, 0, false)
	flex.AddItem(list, 0, 1, true)

	list.SetDrawFunc(func(screen tcell.Screen, x, y, w, h int) (int, int, int, int) {
		longest := 2
		for _, action := range pickerArgs.Actions {
			if len(action.ZapScript) > longest {
				longest = len(action.ZapScript)
			}
			if action.Label != nil && len(*action.Label) > longest {
				longest = len(*action.Label)
			}
		}

		listWidth := longest + 4
		if listWidth < w {
			x += (w - listWidth) / 2
			w = listWidth
		}

		return x, y, w, h
	})

	for _, action := range pickerArgs.Actions {
		if action.Label != nil {
			list.AddItem(*action.Label, action.ZapScript, 0, func() {
				run(action.ZapScript)
			})
		} else {
			list.AddItem(action.ZapScript, "", 0, func() {
				run(action.ZapScript)
			})
		}
	}

	timer, cto := handleTimeout(app, pickerArgs.Timeout)

	list.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEsc || event.Rune() == 'q' {
			app.Stop()
		}
		// reset the timeout timer if a key was pressed
		timer.Stop()
		timer, cto = handleTimeout(app, cto)
		return event
	})

	misterScreenWorkaround(app, pl)

	if err := app.SetRoot(flex, true).Run(); err != nil {
		return err
	}

	return nil
}
