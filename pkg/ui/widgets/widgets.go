// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

	"github.com/ZaparooProject/zaparoo-core/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/ui/tui"
	widgetmodels "github.com/ZaparooProject/zaparoo-core/pkg/ui/widgets/models"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
)

const (
	DefaultTimeout = 30 // seconds
	PIDFilename    = "widget.pid"
)

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

// killWidgetIfRunning checks if a widget is running via the PID file and
// tries to kill it with an interrupt. Returns true if it was killed.
func killWidgetIfRunning(pl platforms.Platform) (bool, error) {
	path := pidPath(pl)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to stat PID file: %w", err)
	}

	pid := 0
	//nolint:gosec // Safe: reads PID files for process management
	pidBytes, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("failed to read PID file: %w", err)
	}
	pid, err = strconv.Atoi(string(pidBytes))
	if err != nil {
		return false, fmt.Errorf("failed to parse PID: %w", err)
	}

	if !isProcessRunning(pid) {
		// clean up stale file
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

// handleTimeout adds a background timer which quits the app once ended. It's
// used to make sure there aren't hanging processes running in the background
// if a core gets loaded while it's open.
func handleTimeout(_ *tview.Application, timeout int) (timer *time.Timer, actualTimeout int) {
	var to int
	switch {
	case timeout == 0:
		to = DefaultTimeout
	case timeout < 0:
		// no timeout
		return nil, -1
	default:
		to = timeout
	}

	timer = time.AfterFunc(time.Duration(to)*time.Second, func() {
		os.Exit(0)
	})

	return timer, to
}

func NoticeUIBuilder(_ platforms.Platform, argsPath string, loader bool) (*tview.Application, error) {
	var noticeArgs widgetmodels.NoticeArgs

	//nolint:gosec // Safe: reads widget argument files from controlled directories
	args, err := os.ReadFile(argsPath)
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
	tui.SetTheme(&tview.Styles)

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
				if _, err := os.Stat(noticeArgs.Complete); err != nil {
					continue
				}
				log.Debug().Msg("notice complete file exists, stopping")
				err := os.Remove(noticeArgs.Complete)
				if err != nil {
					log.Error().Err(err).Msg("error removing complete file")
				}
				app.QueueUpdateDraw(func() {
					app.Stop()
				})
				os.Exit(0)
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

	centeredPages := tui.CenterWidget(75, 15, view)
	return app.SetRoot(centeredPages, true), nil
}

// NoticeUI is a simple TUI screen that displays a message on screen. It can
// also optionally include a loading indicator spinner next to the message.
func NoticeUI(pl platforms.Platform, argsPath string, loader bool) error {
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

	err := tui.BuildAndRetry(func() (*tview.Application, error) {
		return NoticeUIBuilder(pl, argsPath, loader)
	})
	log.Debug().Msg("exiting notice widget")
	if err != nil {
		return fmt.Errorf("failed to build and retry notice widget: %w", err)
	}
	return nil
}

func PickerUIBuilder(cfg *config.Instance, _ platforms.Platform, argsPath string) (*tview.Application, error) {
	//nolint:gosec // Safe: reads widget argument files from controlled directories
	args, err := os.ReadFile(argsPath)
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
	tui.SetTheme(&tview.Styles)

	run := func(item widgetmodels.PickerItem) {
		log.Info().Msgf("running picker selection: %v", item)

		zsrp := models.RunParams{
			Text:   &item.ZapScript,
			Unsafe: pickerArgs.Unsafe,
		}

		ps, err := json.Marshal(zsrp)
		if err != nil {
			log.Error().Err(err).Msg("error creating run params")
		}

		_, err = client.LocalClient(context.Background(), cfg, models.MethodRun, string(ps))
		if err != nil {
			log.Error().Err(err).Msg("error running local client")
		}

		app.Stop()
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
	padding := tview.NewTextView()
	list := tview.NewList()

	flex.AddItem(padding, 1, 0, false)

	if strings.TrimSpace(title) != "" {
		flex.AddItem(titleText, 1, 0, false)
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

	for _, item := range pickerArgs.Items {
		list.AddItem(item.Name, "", 0, func() {
			run(item)
		})
	}

	if pickerArgs.Selected < 0 || pickerArgs.Selected >= len(pickerArgs.Items) {
		pickerArgs.Selected = 0
	} else {
		list.SetCurrentItem(pickerArgs.Selected)
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

	centeredPages := tui.CenterWidget(75, 15, flex)
	return app.SetRoot(centeredPages, true), nil
}

// PickerUI displays a list picker of Zap Link Cmds to run via the API.
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

	err := tui.BuildAndRetry(func() (*tview.Application, error) {
		return PickerUIBuilder(cfg, pl, argsPath)
	})
	log.Debug().Msg("exiting picker widget")
	if err != nil {
		return fmt.Errorf("failed to build and run picker UI: %w", err)
	}
	return nil
}
