// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later
//
// This file is part of Zaparoo Core.
//
// Zaparoo Core is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// Zaparoo Core is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.

package widgets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/tui"
	widgetmodels "github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/widgets/models"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const (
	DefaultTimeout    = 30 // seconds
	PIDFilename       = "widget.pid"
	uiResponseTimeout = 5 * time.Second
)

type localClientFunc func(context.Context, *config.Instance, string, string) (string, error)

func runningFromZapScript() bool {
	return os.Getenv("ZAPAROO_RUN_SCRIPT") == "2"
}

func isProcessRunning(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if runtime.GOOS != "windows" {
		err = process.Signal(syscall.Signal(0))
	}
	return err == nil
}

func pidPath(pl platforms.Platform) string {
	return filepath.Join(pl.Settings().TempDir, PIDFilename)
}

func createPIDFile(pl platforms.Platform) error {
	path := pidPath(pl)
	if _, err := os.Stat(path); err == nil {
		return errors.New("PID file already exists")
	}
	pid := os.Getpid()
	//nolint:gosec // Safe: PID file may be read by other processes
	err := os.WriteFile(path, []byte(strconv.Itoa(pid)), 0o644)
	if err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}
	return nil
}

func removePIDFile(pl platforms.Platform) error {
	path := pidPath(pl)
	_, err := os.Stat(path)
	if err == nil {
		err = os.Remove(path)
		if err != nil {
			return fmt.Errorf("failed to remove PID file: %w", err)
		}
		return nil
	} else if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return fmt.Errorf("failed to stat PID file: %w", err)
}

func killWidgetIfRunning(pl platforms.Platform) (bool, error) {
	path := pidPath(pl)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to stat PID file: %w", err)
	}

	//nolint:gosec // Safe: reads PID files for process management
	pidBytes, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("failed to read PID file: %w", err)
	}
	pid, err := strconv.Atoi(string(pidBytes))
	if err != nil {
		return false, fmt.Errorf("failed to parse PID: %w", err)
	}

	if !isProcessRunning(pid) {
		if removeErr := os.Remove(path); removeErr != nil {
			return false, fmt.Errorf("failed to remove stale PID file: %w", removeErr)
		}
		return false, nil
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return false, fmt.Errorf("failed to find process: %w", err)
	}

	err = proc.Signal(syscall.SIGTERM)
	if err != nil {
		return false, fmt.Errorf("failed to send SIGTERM: %w", err)
	}

	time.Sleep(100 * time.Millisecond)
	if _, err := os.Stat(path); err == nil {
		err := os.Remove(path)
		if err != nil {
			return true, fmt.Errorf("failed to remove PID file after kill: %w", err)
		}
	}

	return true, nil
}

// handleTimeout creates a timer that exits the app after the specified timeout.
// Prevents hanging widget processes if the parent application closes unexpectedly.
func handleTimeout(_ *tview.Application, timeout int) (timer *time.Timer, actualTimeout int) {
	switch {
	case timeout == 0:
		actualTimeout = DefaultTimeout
	case timeout < 0:
		return nil, -1
	default:
		actualTimeout = timeout
	}

	timer = time.AfterFunc(time.Duration(actualTimeout)*time.Second, func() {
		os.Exit(0)
	})

	return timer, actualTimeout
}

func sendUIResponse(
	cfg *config.Instance,
	eventID string,
	action models.UIResponseAction,
	choiceID string,
	localClient localClientFunc,
) error {
	params, err := json.Marshal(models.UIRespondParams{
		ID:       eventID,
		Action:   action,
		ChoiceID: choiceID,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal UI response: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), uiResponseTimeout)
	defer cancel()
	if _, err = localClient(ctx, cfg, models.MethodUIRespond, string(params)); err != nil {
		return fmt.Errorf("failed to send UI response: %w", err)
	}
	return nil
}

func watchCompletion(app *tview.Application, fs afero.Fs, completePath string) {
	if completePath == "" {
		return
	}
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if _, err := fs.Stat(completePath); err != nil {
				continue
			}
			if err := fs.Remove(completePath); err != nil {
				log.Error().Err(err).Msg("error removing UI completion file")
			}
			app.QueueUpdateDraw(app.Stop)
			return
		}
	}()
}

func NoticeUIBuilder(
	cfg *config.Instance,
	pl platforms.Platform,
	argsPath string,
	loader bool,
) (*tview.Application, error) {
	return buildNoticeUI(cfg, pl, afero.NewOsFs(), argsPath, loader, client.LocalClient)
}

func buildNoticeUI(
	cfg *config.Instance,
	_ platforms.Platform,
	fs afero.Fs,
	argsPath string,
	loader bool,
	localClient localClientFunc,
) (*tview.Application, error) {
	var noticeArgs widgetmodels.NoticeArgs

	args, err := afero.ReadFile(fs, argsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read args file: %w", err)
	}

	err = json.Unmarshal(args, &noticeArgs)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal notice args: %w", err)
	}

	if noticeArgs.Text == "" && loader {
		noticeArgs.Text = "Loading..."
	}

	app := tview.NewApplication()
	tui.ApplyTheme(tui.CurrentTheme())

	view := tview.NewTextView().
		SetText(noticeArgs.Text).
		SetTextAlign(tview.AlignCenter)
	view.SetBorder(true)
	view.SetWrap(true)
	view.SetWordWrap(true)

	view.SetDrawFunc(func(_ tcell.Screen, x, y, w, h int) (int, int, int, int) {
		y += h / 2
		return x, y, w, h
	})

	handleTimeout(app, noticeArgs.Timeout)
	watchCompletion(app, fs, noticeArgs.Complete)

	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() != tcell.KeyEsc && event.Rune() != 'q' && event.Key() != tcell.KeyEnter {
			return event
		}
		if noticeArgs.EventID == "" {
			app.Stop()
			return nil
		}
		if !noticeArgs.Dismissible {
			return nil
		}
		go func() {
			if err := sendUIResponse(
				cfg, noticeArgs.EventID, models.UIResponseActionDismiss, "", localClient,
			); err != nil {
				log.Error().Err(err).Msg("failed to dismiss UI notice")
				return
			}
			app.QueueUpdateDraw(app.Stop)
		}()
		return nil
	})

	centeredPages := tui.CenterWidget(75, 15, view)
	return app.SetRoot(centeredPages, true), nil
}

// NoticeUI displays a message with an optional loading spinner.
func NoticeUI(cfg *config.Instance, pl platforms.Platform, argsPath string, loader bool) error {
	log.Info().Str("args", argsPath).Msg("showing notice")

	pidFileCreated := false
	if runningFromZapScript() {
		killed, err := killWidgetIfRunning(pl)
		if err != nil {
			return fmt.Errorf("notice widget: %w", err)
		}
		if killed {
			log.Info().Msg("killed open widget")
		}
		err = createPIDFile(pl)
		if err != nil {
			return fmt.Errorf("notice widget: %w", err)
		}
		pidFileCreated = true
	}

	if pidFileCreated {
		defer func() {
			log.Info().Msg("cleaning up PID file on exit")
			err := removePIDFile(pl)
			if err != nil {
				log.Error().Err(err).Msg("error removing PID file")
			}
		}()

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			sig := <-sigCh
			log.Info().Str("signal", sig.String()).Msg("received signal, cleaning up PID file")
			err := removePIDFile(pl)
			if err != nil {
				log.Error().Err(err).Msg("error removing PID file")
			}
			os.Exit(2)
		}()
	}

	err := tui.BuildAndRetry(nil, func() (*tview.Application, error) {
		return NoticeUIBuilder(cfg, pl, argsPath, loader)
	})
	log.Debug().Msg("exiting notice widget")
	if err != nil {
		return fmt.Errorf("failed to build and retry notice widget: %w", err)
	}
	return nil
}

func PickerUIBuilder(
	cfg *config.Instance,
	pl platforms.Platform,
	argsPath string,
) (*tview.Application, error) {
	return buildPickerUI(cfg, pl, afero.NewOsFs(), argsPath, client.LocalClient)
}

func buildPickerUI(
	cfg *config.Instance,
	_ platforms.Platform,
	fs afero.Fs,
	argsPath string,
	localClient localClientFunc,
) (*tview.Application, error) {
	args, err := afero.ReadFile(fs, argsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read picker args file: %w", err)
	}

	var pickerArgs widgetmodels.PickerArgs
	err = json.Unmarshal(args, &pickerArgs)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal picker args: %w", err)
	}

	if len(pickerArgs.Items) < 1 {
		return nil, errors.New("no items were specified")
	}

	app := tview.NewApplication()
	tui.ApplyTheme(tui.CurrentTheme())

	run := func(item widgetmodels.PickerItem) {
		log.Info().Msgf("running picker selection: %v", item)

		if pickerArgs.EventID != "" {
			action := item.Action
			if action == "" {
				action = models.UIResponseActionSelect
			}
			go func() {
				if responseErr := sendUIResponse(
					cfg, pickerArgs.EventID, action, item.ID, localClient,
				); responseErr != nil {
					log.Error().Err(responseErr).Msg("failed to send picker response")
					return
				}
				app.QueueUpdateDraw(app.Stop)
			}()
			return
		}

		zsrp := models.RunParams{
			Text:   &item.ZapScript,
			Unsafe: pickerArgs.Unsafe,
		}

		ps, marshalErr := json.Marshal(zsrp)
		if marshalErr != nil {
			log.Error().Err(marshalErr).Msg("error creating run params")
			app.Stop()
			return
		}

		if _, runErr := localClient(context.Background(), cfg, models.MethodRun, string(ps)); runErr != nil {
			log.Error().Err(runErr).Msg("error running local client")
		}

		app.Stop()
	}

	dismiss := func() {
		if pickerArgs.EventID == "" {
			app.Stop()
			return
		}
		if !pickerArgs.Dismissible {
			return
		}
		go func() {
			if responseErr := sendUIResponse(
				cfg, pickerArgs.EventID, models.UIResponseActionDismiss, "", localClient,
			); responseErr != nil {
				log.Error().Err(responseErr).Msg("failed to dismiss picker")
				return
			}
			app.QueueUpdateDraw(app.Stop)
		}()
	}

	flex := tview.NewFlex().SetDirection(tview.FlexRow)
	flex.SetBorder(true)

	title := pickerArgs.Title

	for i, v := range pickerArgs.Items {
		if strings.TrimSpace(v.Name) == "" {
			pickerArgs.Items[i].Name = v.ZapScript
		}

		if len(pickerArgs.Items[i].Name) > 60 {
			pickerArgs.Items[i].Name = pickerArgs.Items[i].Name[:57] + "..."
		}
	}

	titleText := tview.NewTextView().
		SetText(title).
		SetTextAlign(tview.AlignCenter)
	messageText := tview.NewTextView().
		SetText(pickerArgs.Message).
		SetTextAlign(tview.AlignCenter)
	padding := tview.NewTextView()
	list := tview.NewList()

	flex.AddItem(padding, 1, 0, false)

	if strings.TrimSpace(title) != "" {
		flex.AddItem(titleText, 1, 0, false)
	}
	if strings.TrimSpace(pickerArgs.Message) != "" {
		flex.AddItem(messageText, 2, 0, false)
	}

	flex.AddItem(padding, 1, 0, false)
	flex.AddItem(list, 0, 1, true)

	list.SetDrawFunc(func(_ tcell.Screen, x, y, w, h int) (int, int, int, int) {
		longest := 2
		for _, item := range pickerArgs.Items {
			if len(item.Name) > longest {
				longest = len(item.Name)
			}
		}

		listWidth := longest + 4
		if listWidth < w {
			x += (w - listWidth) / 2
			w = listWidth
		}

		return x, y, w, h
	})

	for i, item := range pickerArgs.Items {
		currentItem := pickerArgs.Items[i]
		list.AddItem(item.Name, "", 0, func() {
			run(currentItem)
		})
	}

	if pickerArgs.Selected < 0 || pickerArgs.Selected >= len(pickerArgs.Items) {
		pickerArgs.Selected = 0
	} else {
		list.SetCurrentItem(pickerArgs.Selected)
	}

	if pickerArgs.EventID == "" || pickerArgs.Dismissible {
		list.AddItem("Cancel", "", 0, dismiss)
	}

	timer, cto := handleTimeout(app, pickerArgs.Timeout)
	watchCompletion(app, fs, pickerArgs.Complete)

	list.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEsc || event.Rune() == 'q' {
			dismiss()
		}
		if timer != nil {
			timer.Stop()
		}
		timer, cto = handleTimeout(app, cto)
		return event
	})

	centeredPages := tui.CenterWidget(75, 15, flex)
	return app.SetRoot(centeredPages, true), nil
}

// PickerUI displays a list picker of ZapScript commands to run.
func PickerUI(cfg *config.Instance, pl platforms.Platform, argsPath string) error {
	log.Info().Str("args", argsPath).Msg("showing picker")

	pidFileCreated := false
	if runningFromZapScript() {
		killed, err := killWidgetIfRunning(pl)
		if err != nil {
			return fmt.Errorf("picker widget: %w", err)
		}
		if killed {
			log.Info().Msg("killed open widget")
		}
		err = createPIDFile(pl)
		if err != nil {
			return fmt.Errorf("picker widget: %w", err)
		}
		pidFileCreated = true
	}

	if pidFileCreated {
		defer func() {
			log.Info().Msg("cleaning up PID file on exit")
			err := removePIDFile(pl)
			if err != nil {
				log.Error().Err(err).Msg("error removing PID file")
			}
		}()

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			sig := <-sigCh
			log.Info().Str("signal", sig.String()).Msg("received signal, cleaning up PID file")
			err := removePIDFile(pl)
			if err != nil {
				log.Error().Err(err).Msg("error removing PID file")
			}
			os.Exit(2)
		}()
	}

	err := tui.BuildAndRetry(nil, func() (*tview.Application, error) {
		return PickerUIBuilder(cfg, pl, argsPath)
	})
	log.Debug().Msg("exiting picker widget")
	if err != nil {
		return fmt.Errorf("failed to build and run picker UI: %w", err)
	}
	return nil
}
