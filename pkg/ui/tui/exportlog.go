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

package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
)

// BuildExportLogModal creates the log export page with PageFrame.
func BuildExportLogModal(
	pages *tview.Pages,
	app *tview.Application,
	pl platforms.Platform,
	logDestPath string,
	logDestName string,
) {
	frame := NewPageFrame(app).
		SetTitle("Export Logs").
		SetHelpText("View, upload, or copy log files")

	goBack := func() {
		pages.SwitchToPage(PageSettingsMain)
	}
	frame.SetOnEscape(goBack)

	exportPages := tview.NewPages()

	logViewer := tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetWrap(true).
		SetWordWrap(true)

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

	helpTexts := []string{
		"Reload log contents from disk",
		"Upload log file to termbin.com and display URL",
	}
	if logDestPath != "" {
		helpTexts = append(helpTexts, "Copy log file to "+logDestName)
	}
	helpTexts = append(helpTexts, "Return to main screen")

	buttonBar := NewButtonBar(app)

	buttonBar.AddButtonWithHelp("Refresh", helpTexts[0], func() {
		loadLogContent()
		frame.SetHelpText("Log refreshed")
	})

	buttonBar.AddButtonWithHelp("Upload", helpTexts[1], func() {
		outcome := uploadLog(pl, exportPages, app)
		ShowInfoModal(exportPages, app, "Upload Log File", outcome)
	})

	helpIdx := 2
	if logDestPath != "" {
		buttonBar.AddButtonWithHelp("Copy", helpTexts[helpIdx], func() {
			outcome := copyLogToSd(pl, logDestPath, logDestName)
			ShowInfoModal(exportPages, app, "Copy Log File", outcome)
		})
		helpIdx++
	}

	buttonBar.AddButtonWithHelp("Back", helpTexts[helpIdx], goBack)
	buttonBar.SetupNavigation(goBack)
	buttonBar.SetHelpCallback(func(help string) {
		frame.SetHelpText(help)
	})

	frame.SetButtonBar(buttonBar)

	// Log viewer is scroll-only, not focusable
	buttonBar.SetOnUp(nil)
	buttonBar.SetOnDown(nil)

	contentFlex := tview.NewFlex().SetDirection(tview.FlexRow)
	contentFlex.AddItem(logViewer, 0, 1, false)

	exportPages.AddPage("main", contentFlex, true, true)
	frame.SetContent(exportPages)

	pages.AddAndSwitchToPage(PageExportLog, frame, true)
	frame.FocusButtonBar()
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

	loadingModal := tview.NewModal().SetText("Uploading log file...")
	SetBoxTitle(loadingModal, "Log upload")
	loadingModal.SetBorder(true)
	pages.AddPage("temp_upload", loadingModal, true, true)
	app.SetFocus(loadingModal)
	app.ForceDraw()

	//nolint:gosec // logPath is from internal platform settings, not user input
	logContent, err := os.ReadFile(logPath)
	if err != nil {
		pages.RemovePage("temp_upload")
		log.Error().Err(err).Msg("failed to read log file")
		return "Error reading log file."
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dialer := &net.Dialer{Timeout: 30 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", "termbin.com:9999")
	if err != nil {
		pages.RemovePage("temp_upload")
		log.Error().Err(err).Msg("failed to connect to termbin.com")
		return "Error connecting to upload service."
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			log.Debug().Err(closeErr).Msg("failed to close connection")
		}
	}()

	deadlineErr := conn.SetDeadline(time.Now().Add(30 * time.Second))
	if deadlineErr != nil {
		pages.RemovePage("temp_upload")
		log.Error().Err(deadlineErr).Msg("failed to set connection deadline")
		return "Error uploading log file."
	}

	_, err = conn.Write(logContent)
	if err != nil {
		pages.RemovePage("temp_upload")
		log.Error().Err(err).Msg("failed to send log content")
		return "Error uploading log file."
	}

	response, err := io.ReadAll(conn)
	pages.RemovePage("temp_upload")
	if err != nil {
		log.Error().Err(err).Msg("failed to read upload response")
		return "Error reading upload response."
	}

	return "Log file URL:\n\n" + strings.TrimSpace(string(response))
}

// readLastLines reads the last n lines from a file
func readLastLines(filePath string, n int) (string, error) {
	//nolint:gosec // Safe: filePath is from internal platform settings
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read log file: %w", err)
	}

	lines := strings.Split(string(content), "\n")

	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

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

	t := CurrentTheme()
	levelColors := map[string]string{
		"error": t.ErrorColorName,
		"warn":  t.WarningColorName,
		"info":  t.SuccessColorName,
		"debug": t.SecondaryColor,
	}

	color, exists := levelColors[entry.Level]
	if !exists {
		color = t.TextColorName
	}

	timestamp := entry.Time
	if t, err := time.Parse(time.RFC3339, entry.Time); err == nil {
		timestamp = t.Format("15:04:05")
	}

	levelUpper := strings.ToUpper(entry.Level)
	return fmt.Sprintf("[%s::b]%5s[-:-:-] %s %s",
		color, levelUpper, timestamp, entry.Message)
}

// formatLogContent parses and formats log content, newest first
func formatLogContent(content string) string {
	lines := strings.Split(content, "\n")

	var nonEmpty []string
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			nonEmpty = append(nonEmpty, line)
		}
	}

	for i, j := 0, len(nonEmpty)-1; i < j; i, j = i+1, j-1 {
		nonEmpty[i], nonEmpty[j] = nonEmpty[j], nonEmpty[i]
	}

	formatted := make([]string, 0, len(nonEmpty))
	for _, line := range nonEmpty {
		formatted = append(formatted, formatLogEntry(line))
	}

	return strings.Join(formatted, "\n")
}
