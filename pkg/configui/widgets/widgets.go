package widgets

import (
	"encoding/json"
	"errors"
	"fmt"
	widgetModels "github.com/ZaparooProject/zaparoo-core/pkg/configui/widgets/models"
	zapScriptModels "github.com/ZaparooProject/zaparoo-core/pkg/zapscript/models"
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
		os.Exit(0)
	})

	return timer, to
}

func NoticeUIBuilder(_ platforms.Platform, argsPath string, loader bool) (*tview.Application, error) {
	log.Debug().Str("args", argsPath).Msg("showing notice")

	var noticeArgs widgetModels.NoticeArgs

	args, err := os.ReadFile(argsPath)
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(args, &noticeArgs)
	if err != nil {
		return nil, err
	}

	if noticeArgs.Text == "" && loader {
		noticeArgs.Text = "Loading..."
	}

	app := tview.NewApplication()
	configui.SetTheme(&tview.Styles)

	view := tview.NewTextView().
		SetText(noticeArgs.Text).
		SetTextAlign(tview.AlignCenter)

	view.SetDrawFunc(func(screen tcell.Screen, x, y, w, h int) (int, int, int, int) {
		y += h / 2
		return x, y, w, h
	})

	if loader {
		go func() {
			frames := []string{"|", "/", "-", "\\"}
			frameIndex := 0
			for app != nil {
				app.QueueUpdateDraw(func() {
					view.SetText(frames[frameIndex] + " " + noticeArgs.Text)
				})
				frameIndex = (frameIndex + 1) % len(frames)
				time.Sleep(100 * time.Millisecond)
			}
		}()
	}

	handleTimeout(app, noticeArgs.Timeout)

	ticker := time.NewTicker(1 * time.Second)
	if noticeArgs.Complete != "" {
		go func() {
			for range ticker.C {
				if _, err := os.Stat(noticeArgs.Complete); err == nil {
					log.Debug().Msg("notice complete file exists, stopping")
					app.QueueUpdateDraw(func() {
						app.Stop()
					})
					err := os.Remove(noticeArgs.Complete)
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

// NoticeUI is a simple TUI screen that displays a message on screen. It can
// also optionally include a loading indicator spinner next to the message.
func NoticeUI(pl platforms.Platform, argsPath string, loader bool) error {
	return configui.BuildAppAndRetry(func() (*tview.Application, error) {
		return NoticeUIBuilder(pl, argsPath, loader)
	})
}

type pickerAction struct {
	label   string
	preview string
	action  zapScriptModels.ZapScriptCmd
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

	if len(pickerArgs.Cmds) < 1 {
		return nil, errors.New("no actions were specified")
	}

	var actions []pickerAction
	for _, la := range pickerArgs.Cmds {
		action := pickerAction{
			action: la,
		}

		cmdName := strings.ToLower(la.Cmd)
		switch cmdName {
		case zapScriptModels.ZapScriptCmdEvaluate:
			var zsp zapScriptModels.CmdEvaluateArgs
			err := json.Unmarshal(la.Args, &zsp)
			if err != nil {
				return nil, fmt.Errorf("error unmarshalling zapscript params: %w", err)
			}
			if la.Name != nil && *la.Name != "" {
				action.label = *la.Name
				action.preview = zsp.ZapScript
			} else {
				action.label = zsp.ZapScript
			}
		case zapScriptModels.ZapScriptCmdLaunch:
			var zm zapScriptModels.CmdLaunchArgs
			err := json.Unmarshal(la.Args, &zm)
			if err != nil {
				return nil, fmt.Errorf("error unmarshalling zapscript params: %w", err)
			}
			if la.Name != nil && *la.Name != "" {
				action.label = *la.Name
			}
			if zm.Name != nil && *zm.Name != "" {
				action.label = *zm.Name
			}
			if zm.URL != nil {
				action.preview = *zm.URL
			}
		default:
			log.Error().Msgf("unknown cmd: %s", la.Cmd)
			continue
		}

		actions = append(actions, action)
	}

	app := tview.NewApplication()
	configui.SetTheme(&tview.Styles)

	run := func(action zapScriptModels.ZapScriptCmd) {
		log.Info().Msgf("running picker selection: %v", action)
		ps, err := json.Marshal(action)
		if err != nil {
			log.Error().Err(err).Msg("error creating run params")
		}
		_, err = client.LocalClient(cfg, models.MethodRunCommand, string(ps))
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

	flex.AddItem(padding, 1, 0, false)
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
		if action.label == "" {
			continue
		}

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

// PickerUI displays a list picker of Zap Link Cmds to run via the API.
func PickerUI(cfg *config.Instance, pl platforms.Platform, argsPath string) error {
	return configui.BuildAppAndRetry(func() (*tview.Application, error) {
		return PickerUIBuilder(cfg, pl, argsPath)
	})
}
