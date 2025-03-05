package widgets

import (
	"encoding/json"
	"errors"
	"fmt"
	widgetModels "github.com/ZaparooProject/zaparoo-core/pkg/configui/widgets/models"
	"os"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/configui"
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
		app.QueueUpdateDraw(func() {
			app.Stop()
		})
	})

	return timer, to
}

// LoaderUI is a simple TUI screen that indicates something is happening to the
// user. The text displayed can be customized with the text field.
func LoaderUIBuilder(pl platforms.Platform, argsPath string) (*tview.Application, error) {
	log.Debug().Str("args", argsPath).Msg("showing loader")

	var loaderArgs widgetModels.LoaderArgs

	args, err := os.ReadFile(argsPath)
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(args, &loaderArgs)
	if err != nil {
		return nil, err
	}

	if loaderArgs.Text == "" {
		loaderArgs.Text = "Loading..."
	}

	app := tview.NewApplication()
	configui.SetTheme(&tview.Styles)

	view := tview.NewTextView().
		SetText(loaderArgs.Text).
		SetTextAlign(tview.AlignCenter)

	view.SetDrawFunc(func(screen tcell.Screen, x, y, w, h int) (int, int, int, int) {
		y += h / 2
		return x, y, w, h
	})

	go func() {
		frames := []string{"|", "/", "-", "\\"}
		frameIndex := 0
		for app != nil {
			app.QueueUpdateDraw(func() {
				view.SetText(frames[frameIndex] + " " + loaderArgs.Text)
			})
			frameIndex = (frameIndex + 1) % len(frames)
			time.Sleep(100 * time.Millisecond)
		}
	}()

	handleTimeout(app, loaderArgs.Timeout)

	if loaderArgs.Complete != "" {
		go func() {
			ticker := time.NewTicker(1 * time.Second)
			for range ticker.C {
				if _, err := os.Stat(loaderArgs.Complete); err == nil {
					app.Stop()
					err := os.Remove(loaderArgs.Complete)
					if err != nil {
						log.Error().Err(err).Msg("error removing complete file")
					}
					break
				}
			}
		}()
	}

	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEsc ||
			event.Rune() == 'q' ||
			event.Key() == tcell.KeyEnter {
			app.Stop()
		}
		return event
	})

	return app.SetRoot(view, true), nil
}

func LoaderUI(pl platforms.Platform, argsPath string) error {
	return configui.BuildAppAndRetry(func() (*tview.Application, error) {
		return LoaderUIBuilder(pl, argsPath)
	})
}

type pickerAction struct {
	label   string
	preview string
	action  models.ZapLinkAction
}

func PickerUIBuilder(cfg *config.Instance, pl platforms.Platform, argsPath string) (*tview.Application, error) {
	log.Debug().Str("args", argsPath).Msg("showing picker")

	args, err := os.ReadFile(argsPath)
	if err != nil {
		return nil, err
	}

	var pickerArgs widgetModels.PickerArgs
	err = json.Unmarshal(args, &pickerArgs)
	if err != nil {
		return nil, err
	}

	if len(pickerArgs.Actions) < 1 {
		return nil, errors.New("no actions were specified")
	}

	var actions []pickerAction
	for _, la := range pickerArgs.Actions {
		action := pickerAction{
			action: la,
		}

		method := strings.ToLower(la.Method)
		switch method {
		case models.ZapLinkActionZapScript:
			var zsp models.ZapScriptParams
			err := json.Unmarshal(la.Params, &zsp)
			if err != nil {
				return nil, fmt.Errorf("error unmarshalling zapscript params: %w", err)
			}
			if zsp.Name != "" {
				action.label = zsp.Name
				action.preview = zsp.ZapScript
			} else {
				action.label = zsp.ZapScript
			}
		case models.ZapLinkActionMedia:
			var zm models.MediaParams
			err := json.Unmarshal(la.Params, &zm)
			if err != nil {
				return nil, fmt.Errorf("error unmarshalling zapscript params: %w", err)
			}
			action.label = zm.Name
			if zm.Url != nil {
				action.preview = *zm.Url
			}
		default:
			log.Error().Msgf("unkown link action method: %s", la.Method)
			continue
		}

		actions = append(actions, action)
	}

	app := tview.NewApplication()
	configui.SetTheme(&tview.Styles)

	run := func(action models.ZapLinkAction) {
		log.Info().Msgf("running picker selection: %v", action)
		ps, err := json.Marshal(action)
		if err != nil {
			log.Error().Err(err).Msg("error creating run params")
		}
		_, err = client.LocalClient(cfg, models.MethodRunLinkAction, string(ps))
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
		for _, action := range actions {
			if len(action.preview) > longest {
				longest = len(action.preview)
			}
			if len(action.label) > longest {
				longest = len(action.label)
			}
		}

		listWidth := longest + 4
		if listWidth < w {
			x += (w - listWidth) / 2
			w = listWidth
		}

		return x, y, w, h
	})

	for _, action := range actions {
		list.AddItem(action.label, action.preview, 0, func() {
			run(action.action)
		})
	}

	list.AddItem("Cancel", "", 0, func() {
		app.Stop()
	})

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

	return app.SetRoot(flex, true), nil
}

// PickerUI displays a list picker of Zap Link Actions to run via the API.
func PickerUI(cfg *config.Instance, pl platforms.Platform, argsPath string) error {
	return configui.BuildAppAndRetry(func() (*tview.Application, error) {
		return PickerUIBuilder(cfg, pl, argsPath)
	})
}
