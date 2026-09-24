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
	"fmt"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/assets/credits"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildCreditsPage_Integration(t *testing.T) {
	t.Parallel()

	bundle, err := credits.Components()
	require.NoError(t, err)
	contributors, err := credits.Contributors()
	require.NoError(t, err)
	last := bundle.Components[len(bundle.Components)-1]

	for _, size := range [][2]int{{80, 25}, {75, 15}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			t.Parallel()
			runner := NewTestAppRunner(t, size[0], size[1])
			defer runner.Stop()
			pages := tview.NewPages()
			pages.AddPage(PageSettingsMain, tview.NewTextView().SetText("Settings Main"), true, false)
			runner.Start(pages)
			runner.QueueUpdateDraw(func() { buildCreditsPage(pages, runner.App()) })

			require.True(t, runner.WaitForText("Contributors", uiSettleTimeout))
			assert.True(t, runner.ContainsText(contributors[0].Name))
			assert.True(t, runner.ContainsText("github.com/"+contributors[0].Login),
				"the selected contributor's GitHub account shows in the help line")

			// The last row is a third-party component; Enter opens its license.
			runner.SimulateKey(tcell.KeyEnd, 0, tcell.ModNone)
			require.True(t, runner.WaitForText("License: "+last.License, uiSettleTimeout))
			runner.SimulateEnter()
			require.True(t, runner.WaitForText("Up/Down and PgUp/PgDn to scroll", uiSettleTimeout))
			assert.True(t, runner.ContainsText("License: "+last.License))
			assert.True(t, runner.ContainsText("--- "+last.Files[0].Name+" ---"))

			// Esc returns to the list, which keeps its place.
			runner.SimulateEscape()
			require.True(t, runner.WaitForCondition(func() bool {
				return !runner.ContainsText("Up/Down and PgUp/PgDn to scroll")
			}, uiSettleTimeout))
			assert.True(t, runner.ContainsText("→ "+last.Name))
			assert.True(t, runner.ContainsText("License: "+last.License))
			assert.False(t, pages.HasPage(PageSettingsCreditsViewer), "the viewer page is removed")
		})
	}
}

func TestShowTextViewer_Integration(t *testing.T) {
	t.Parallel()

	runner := NewTestAppRunner(t, 75, 15)
	defer runner.Stop()
	pages := tview.NewPages()
	lines := make([]string, 60)
	for i := range lines {
		lines[i] = fmt.Sprintf("L%03d [not a color tag]", i+1)
	}
	closed := make(chan struct{}, 1)
	runner.Start(pages)
	var frame *PageFrame
	runner.QueueUpdateDraw(func() {
		showTextViewer(pages, runner.App(), "viewer", []string{"Viewer"}, strings.Join(lines, "\n"),
			func() { closed <- struct{}{} })
		name, primitive := pages.GetFrontPage()
		require.Equal(t, "viewer", name)
		pageFrame, ok := primitive.(*PageFrame)
		require.True(t, ok)
		frame = pageFrame
	})
	require.NotNil(t, frame)
	require.True(t, runner.WaitForText("L001 [not a color tag]", uiSettleTimeout),
		"text is shown literally, brackets included")

	runner.SimulateArrowDown()
	assert.False(t, runner.ContainsText("L001"), "Down scrolls a line")
	runner.SimulateKey(tcell.KeyPgDn, 0, tcell.ModNone)
	assert.True(t, runner.ContainsText("L015"), "PgDn scrolls a page")
	assert.False(t, runner.ContainsText("L002"))

	buttonBarFocused := func() bool {
		var focused bool
		runner.QueueUpdateDraw(func() { focused = frame.GetButtonBar().HasFocus() })
		return focused
	}
	runner.SimulateTab()
	assert.True(t, buttonBarFocused(), "Tab moves to the Back button")
	runner.SimulateArrowUp()
	assert.False(t, buttonBarFocused(), "Up from the button returns to the text")

	// Down once the end is on screen moves to the Back button.
	runner.SimulateKey(tcell.KeyEnd, 0, tcell.ModNone)
	assert.True(t, runner.ContainsText("L060"))
	runner.SimulateArrowDown()
	assert.True(t, buttonBarFocused())

	runner.SimulateArrowUp()
	runner.SimulateEscape()
	require.True(t, runner.WaitForSignal(closed, uiSettleTimeout), "Esc goes back")
}

func TestReflowParagraphs(t *testing.T) {
	t.Parallel()

	prose := "Permission is hereby granted, free of charge, to any person obtaining a copy\n" +
		"of this software and associated documentation files (the \"Software\"), to deal\n" +
		"in the Software."
	assert.Equal(t,
		"Permission is hereby granted, free of charge, to any person obtaining a copy "+
			"of this software and associated documentation files (the \"Software\"), to deal "+
			"in the Software.",
		reflowParagraphs(prose))

	kept := []string{
		"Copyright (c) 2006-2010 Kirill Simonov\nCopyright (c) 2006-2011 Kirill Simonov",
		"short line\nanother short line",
		"1. Definitions. This clause is long enough to count as wrapped prose text.\n" +
			"2. Grant of license. Also long enough to count as wrapped prose text here.",
		"   * Redistributions of source code must retain the above copyright notice,\n" +
			"     this list of conditions and the following disclaimer in the docs.",
		"a) First lettered item that is long enough to be wrapped prose in a file.\n" +
			"b) Second lettered item that is long enough to be wrapped prose in a file.",
		"single line",
	}
	for _, block := range kept {
		assert.Equal(t, block, reflowParagraphs(block))
	}

	two := prose + "\n\n" + kept[0] + "\n"
	assert.Equal(t, reflowParagraphs(prose)+"\n\n"+kept[0]+"\n", reflowParagraphs(two),
		"paragraphs are handled separately and trailing newlines survive")
}
