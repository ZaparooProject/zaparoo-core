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
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/rivo/tview"
)

// buildRemoteActivityPage loads recent remote operations ledger entries in
// the background, then shows them as an owner-facing activity log: what a
// linked account's remote commands have actually done.
func buildRemoteActivityPage(svc SettingsService, pages *tview.Pages, app *tview.Application, goBack func()) {
	loadSettingsPage(pages, app, PageSettingsRemoteActivity,
		[]string{"Settings", "Online", "Activity"},
		"Loading remote activity...",
		"Failed to load remote activity",
		tuiContext, goBack,
		func(ctx context.Context) ([]models.RemoteActivityEntry, error) {
			activity, err := svc.GetRemoteActivity(ctx)
			if err != nil {
				return nil, fmt.Errorf("get remote activity: %w", err)
			}
			if activity == nil {
				return nil, nil
			}
			return activity.Entries, nil
		},
		func(entries []models.RemoteActivityEntry) {
			renderRemoteActivityPage(pages, app, entries, goBack)
		},
	)
}

func renderRemoteActivityPage(
	pages *tview.Pages,
	app *tview.Application,
	entries []models.RemoteActivityEntry,
	goBack func(),
) {
	frame := NewPageFrame(app).SetTitle("Settings", "Online", "Activity")
	frame.SetOnEscape(goBack)
	buttonBar := NewButtonBar(app).AddButton("Back", goBack).SetupNavigation(goBack)
	frame.SetButtonBar(buttonBar)

	list := tview.NewList()
	list.SetSecondaryTextColor(CurrentTheme().SecondaryTextColor)
	list.ShowSecondaryText(true)
	list.SetSelectedFocusOnly(true)
	for i := range entries {
		entry := &entries[i]
		mainText := fmt.Sprintf(
			"%s  %s", formatRemoteActivityTime(entry.CreatedAt), safeRemoteActivityText(entry.OperationType))
		secondary := formatRemoteActivitySecondary(entry)
		detail := formatRemoteActivityDetail(entry)
		list.AddItem(mainText, secondary, 0, func() {
			ShowInfoModal(pages, app, "Remote activity", detail, func() {
				app.SetFocus(list)
			})
		})
	}
	if len(entries) == 0 {
		list.AddItem("(no remote activity yet)", "Nothing has run through Remote control yet", 0, nil)
	}

	frame.SetContent(list)
	frame.SetupContentToButtonNavigation()
	pages.AddAndSwitchToPage(PageSettingsRemoteActivity, frame, true)
	frame.FocusContent()
}

// formatRemoteActivityTime renders a stored RFC3339 timestamp as a short
// local-order date and time, or the raw value if it doesn't parse.
func formatRemoteActivityTime(raw string) string {
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return safeRemoteActivityText(raw)
	}
	return parsed.UTC().Format("2 Jan 15:04")
}

func safeRemoteActivityText(value string) string {
	value = strings.ToValidUTF8(value, "\uFFFD")
	var clean strings.Builder
	for _, r := range value {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) {
			_, _ = clean.WriteRune('\uFFFD')
		} else {
			_, _ = clean.WriteRune(r)
		}
	}
	return tview.Escape(clean.String())
}

// formatRemoteActivityOrigin renders who issued the operation: the account
// itself, or a named API key.
func formatRemoteActivityOrigin(entry *models.RemoteActivityEntry) string {
	originKind := safeRemoteActivityText(entry.OriginKind)
	if entry.OriginKeyName != "" {
		return originKind + " (" + safeRemoteActivityText(entry.OriginKeyName) + ")"
	}
	return originKind
}

// formatRemoteActivitySecondary renders the list row's secondary line:
// origin and outcome.
func formatRemoteActivitySecondary(entry *models.RemoteActivityEntry) string {
	outcome := entry.Status
	if outcome == "" {
		outcome = entry.State
	}
	secondary := formatRemoteActivityOrigin(entry) + ", " + safeRemoteActivityText(outcome)
	if entry.ErrorCode != "" {
		secondary += ": " + safeRemoteActivityText(entry.ErrorCode)
	}
	return secondary
}

// formatRemoteActivityDetail renders the full detail modal body for one entry.
func formatRemoteActivityDetail(entry *models.RemoteActivityEntry) string {
	detail := "Operation: " + safeRemoteActivityText(entry.OperationType) +
		"\nFrom: " + formatRemoteActivityOrigin(entry) +
		"\nWhen: " + formatRemoteActivityTime(entry.CreatedAt)
	outcome := entry.Status
	if outcome == "" {
		outcome = entry.State
	}
	detail += "\nOutcome: " + safeRemoteActivityText(outcome)
	if entry.ErrorCode != "" {
		detail += "\nError: " + safeRemoteActivityText(entry.ErrorCode)
	}
	return detail
}
