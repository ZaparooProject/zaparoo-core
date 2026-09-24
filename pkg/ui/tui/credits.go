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
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/assets/credits"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
)

// buildCreditsPage lists the people who contributed to Zaparoo Core and the
// third-party software it includes. Selecting a component opens its full
// license text.
func buildCreditsPage(pages *tview.Pages, app *tview.Application) {
	goBack := func() { pages.SwitchToPage(PageSettingsMain) }

	bundle, err := credits.Components()
	if err != nil {
		log.Error().Err(err).Msg("error loading third-party software credits")
		ShowErrorModal(pages, app, "Failed to load credits", goBack)
		return
	}
	contributors, err := credits.Contributors()
	if err != nil {
		log.Error().Err(err).Msg("error loading contributor credits")
		ShowErrorModal(pages, app, "Failed to load credits", goBack)
		return
	}

	frame := NewPageFrame(app).SetTitle("Settings", "Credits")
	frame.SetOnEscape(goBack)
	buttonBar := NewButtonBar(app).AddButton("Back", goBack).SetupNavigation(goBack)
	frame.SetButtonBar(buttonBar)

	menu := NewSettingsList(pages, PageSettingsMain)
	menu.SetDynamicHelpMode(true).SetHelpCallback(func(desc string) { frame.SetHelpText(desc) })
	menu.SetOnNavigateOut(frame.FocusButtonBar)

	menu.AddHeader("Contributors")
	for _, contributor := range contributors {
		menu.AddAction(contributor.Name, "github.com/"+contributor.Login, nil)
	}

	menu.AddHeader("Third-party software")
	closeViewer := func() {
		pages.SwitchToPage(PageSettingsCredits)
		pages.RemovePage(PageSettingsCreditsViewer)
	}
	for i := range bundle.Components {
		component := &bundle.Components[i]
		menu.AddNavAction(component.Name, componentSummary(component), func() {
			showTextViewer(pages, app, PageSettingsCreditsViewer,
				[]string{"Settings", "Credits", component.Name},
				bundle.ComponentText(component, reflowParagraphs), closeViewer)
		})
	}

	frame.SetContent(menu.List)
	menu.TriggerInitialHelp()
	frame.SetupContentToButtonNavigation()
	pages.AddAndSwitchToPage(PageSettingsCredits, frame, true)
}

// componentSummary is the help line for a component: its version and
// license.
func componentSummary(component *credits.Component) string {
	if component.Version == "" {
		return "License: " + component.License
	}
	return component.Version + " · License: " + component.License
}

// reflowMinLine is the shortest line treated as hard-wrapped prose. License
// files wrap prose at 70-80 columns; shorter lines are deliberate breaks,
// such as copyright lines, addresses or file lists.
const reflowMinLine = 50

// reflowParagraphs joins the hard-wrapped lines of plain paragraphs so the
// viewer can wrap them to the screen; license files are usually wrapped at
// 76-80 columns, which leaves a stray word on every line in the 75-column
// CRT window. A paragraph is reflowed only when every line but the last is
// long, all lines share the same indentation, and none starts like a list
// item or a copyright line, so lists and short-line blocks keep their
// layout.
func reflowParagraphs(text string) string {
	blocks := strings.Split(text, "\n\n")
	for i, block := range blocks {
		body := strings.TrimRight(block, "\n")
		lines := strings.Split(body, "\n")
		if len(lines) < 2 || !plainParagraph(lines) {
			continue
		}
		for j, line := range lines {
			lines[j] = strings.TrimSpace(line)
		}
		blocks[i] = strings.Join(lines, " ") + block[len(body):]
	}
	return strings.Join(blocks, "\n\n")
}

func plainParagraph(lines []string) bool {
	indent := leadingSpace(lines[0])
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || leadingSpace(line) != indent || looksLikeListItem(trimmed) ||
			strings.HasPrefix(strings.ToLower(trimmed), "copyright") || strings.HasPrefix(trimmed, "©") {
			return false
		}
		if i < len(lines)-1 && len(line) < reflowMinLine {
			return false
		}
	}
	return true
}

func leadingSpace(line string) string {
	return line[:len(line)-len(strings.TrimLeft(line, " \t"))]
}

// looksLikeListItem reports whether a line starts with a bullet, a
// numbered or lettered marker, or a separator line.
func looksLikeListItem(line string) bool {
	if strings.HasPrefix(line, "•") || strings.ContainsRune("-*=#([", rune(line[0])) {
		return true
	}
	end := strings.IndexAny(line, ".)")
	if end <= 0 || end > 3 {
		return false
	}
	marker := line[:end]
	return strings.Trim(marker, "0123456789") == "" ||
		(len(marker) == 1 && marker[0] >= 'a' && marker[0] <= 'z')
}
