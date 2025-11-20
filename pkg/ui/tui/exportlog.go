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

package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
)

func BuildExportLogModal(
	pl platforms.Platform,
	app *tview.Application,
	pages *tview.Pages,
	logDestPath string,
	logDestName string,
) tview.Primitive {
	exportPages := tview.NewPages()

	// Create log viewer (top section)
	logViewer := tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetWrap(true).
		SetWordWrap(true)

	// Load log content
	loadLogContent := func() {
		logPath := path.Join(pl.Settings().LogDir, config.LogFile)
		content, err := readLastLines(logPath, 50)
		if err != nil {
			logViewer.SetText(fmt.Sprintf("Error reading log file: %v", err))
		} else {
			formatted := formatLogContent(content)
			logViewer.SetText(formatted)
			logViewer.ScrollToBeginning()
		}
	}
	loadLogContent()

	// Create horizontal button bar (bottom section)
	refreshBtn := tview.NewButton("Refresh").SetSelectedFunc(func() {
		loadLogContent()
	})
	uploadBtn := tview.NewButton("Upload").SetSelectedFunc(func() {
		outcome := uploadLog(pl, exportPages, app)
		resultModal := genericModal(outcome, "Upload Log File",
			func(_ int, _ string) {
				exportPages.RemovePage("upload")
			}, true)
		exportPages.AddPage("upload", resultModal, true, true)
		app.SetFocus(resultModal)
	})
	copyBtn := tview.NewButton("Copy").SetSelectedFunc(func() {
		outcome := copyLogToSd(pl, logDestPath, logDestName)
		resultModal := genericModal(outcome, "Copy Log File",
			func(_ int, _ string) {
				exportPages.RemovePage("copy")
			}, true)
		exportPages.AddPage("copy", resultModal, true, true)
		app.SetFocus(resultModal)
	})
	backBtn := tview.NewButton("Go back").SetSelectedFunc(func() {
		pages.SwitchToPage(PageMain)
	})

	// Create help text view
	helpText := tview.NewTextView().
		SetTextAlign(tview.AlignCenter)

	// Set up help text updates when buttons receive focus
	refreshBtn.SetFocusFunc(func() {
		helpText.SetText("Reload log contents from disk.")
	})
	uploadBtn.SetFocusFunc(func() {
		helpText.SetText("Upload log file to termbin.com and display URL.")
	})
	copyBtn.SetFocusFunc(func() {
		helpText.SetText("Copy log file to " + logDestName + ".")
	})
	backBtn.SetFocusFunc(func() {
		helpText.SetText("Return to main screen.")
	})

	// Build button list based on whether copy destination exists
	buttons := []*tview.Button{refreshBtn, uploadBtn}
	if logDestPath != "" {
		buttons = append(buttons, copyBtn)
	}
	buttons = append(buttons, backBtn)

	// Create horizontal button bar with spacers
	buttonBar := tview.NewFlex().
		AddItem(tview.NewTextView(), 0, 1, false) // Left spacer
	for i, btn := range buttons {
		buttonBar.AddItem(btn, 15, 1, i == len(buttons)-1) // Last button (Back) gets focus
		if i < len(buttons)-1 {
			buttonBar.AddItem(tview.NewTextView(), 1, 0, false) // Small gap between buttons
		}
	}
	buttonBar.AddItem(tview.NewTextView(), 0, 1, false) // Right spacer

	// Set up left/right navigation between buttons with wrap-around
	for i, btn := range buttons {
		idx := i // Capture for closure
		btn.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
			switch event.Key() {
			case tcell.KeyLeft:
				prevIdx := (idx - 1 + len(buttons)) % len(buttons)
				app.SetFocus(buttons[prevIdx])
				return nil
			case tcell.KeyRight:
				nextIdx := (idx + 1) % len(buttons)
				app.SetFocus(buttons[nextIdx])
				return nil
			case tcell.KeyEscape:
				pages.SwitchToPage(PageMain)
				return nil
			default:
				return event
			}
		})
	}

	// Create layout with log viewer on top, buttons, then help text on bottom
	layout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(logViewer, 0, 1, false).
		AddItem(buttonBar, 1, 1, true).
		AddItem(helpText, 1, 1, false)

	exportPages.AddAndSwitchToPage("export", layout, true)

	exportPages.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			pages.SwitchToPage(PageMain)
			return nil
		}
		return event
	})

	pageDefaults(PageExportLog, pages, exportPages)
	return exportPages
}

func copyLogToSd(pl platforms.Platform, logDestPath, logDestName string) string {
	logPath := path.Join(pl.Settings().LogDir, config.LogFile)
	newPath := logDestPath
	err := helpers.CopyFile(logPath, newPath)
	outcome := ""
	if err != nil {
		outcome = fmt.Sprintf("Unable to copy log file to %s.", logDestName)
		log.Error().Err(err).Msgf("error copying log file")
	} else {
		outcome = fmt.Sprintf("Copied %s to %s.", config.LogFile, logDestName)
	}
	return outcome
}

func uploadLog(pl platforms.Platform, pages *tview.Pages, app *tview.Application) string {
	logPath := path.Join(pl.Settings().LogDir, config.LogFile)
	modal := genericModal("Uploading log file...", "Log upload", func(_ int, _ string) {}, false)
	pages.AddPage("temp_upload", modal, true, true)
	app.SetFocus(modal)
	app.ForceDraw()

	// Create a pipe to safely pass file content to nc without shell injection
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Read the log file
	//nolint:gosec // Safe: logPath is from internal platform settings, not user input
	logContent, err := os.ReadFile(logPath)
	if err != nil {
		log.Error().Err(err).Msg("failed to read log file")
		return "Error reading log file."
	}

	// Execute nc command with stdin pipe
	cmd := exec.CommandContext(ctx, "nc", "termbin.com", "9999")
	cmd.Stdin = bytes.NewReader(logContent)
	out, err := cmd.Output()
	pages.RemovePage("temp_upload")
	if err != nil {
		log.Error().Err(err).Msgf("error uploading log file to termbin")
		return "Error uploading log file."
	}
	return "Log file URL:\n\n" + strings.TrimSpace(string(out))
}

// readLastLines reads the last n lines from a file
func readLastLines(filePath string, n int) (string, error) {
	//nolint:gosec // Safe: filePath is from internal platform settings
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read log file: %w", err)
	}

	lines := strings.Split(string(content), "\n")

	// Remove empty last line if present
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	// Get last n lines
	start := 0
	if len(lines) > n {
		start = len(lines) - n
	}

	return strings.Join(lines[start:], "\n"), nil
}

// LogEntry represents a parsed JSON log line
type LogEntry struct {
	Level   string `json:"level"`
	Time    string `json:"time"`
	Message string `json:"message"`
}

// formatLogEntry formats a log entry with colors and shortened timestamp
func formatLogEntry(line string) string {
	var entry LogEntry
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		// Not valid JSON, return raw line
		return line
	}

	// Map log levels to colors
	levelColors := map[string]string{
		"error": "red",
		"warn":  "yellow",
		"info":  "green",
		"debug": "gray",
	}

	color, exists := levelColors[entry.Level]
	if !exists {
		color = "white"
	}

	// Shorten timestamp (from "2025-11-20T13:04:23Z" to "13:04:23")
	timestamp := entry.Time
	if t, err := time.Parse(time.RFC3339, entry.Time); err == nil {
		timestamp = t.Format("15:04:05")
	}

	// Format: [color]LEVEL[-] timestamp message
	levelUpper := strings.ToUpper(entry.Level)
	return fmt.Sprintf("[%s::b]%5s[-:-:-] %s %s",
		color, levelUpper, timestamp, entry.Message)
}

// formatLogContent parses and formats log content, newest first
func formatLogContent(content string) string {
	lines := strings.Split(content, "\n")

	// Remove empty lines
	var nonEmpty []string
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			nonEmpty = append(nonEmpty, line)
		}
	}

	// Reverse to show newest first
	for i, j := 0, len(nonEmpty)-1; i < j; i, j = i+1, j-1 {
		nonEmpty[i], nonEmpty[j] = nonEmpty[j], nonEmpty[i]
	}

	// Format each line
	formatted := make([]string, 0, len(nonEmpty))
	for _, line := range nonEmpty {
		formatted = append(formatted, formatLogEntry(line))
	}

	return strings.Join(formatted, "\n")
}
