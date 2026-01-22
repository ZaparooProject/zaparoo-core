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
	"testing"

	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPageFrame(t *testing.T) {
	t.Parallel()

	app := tview.NewApplication()
	frame := NewPageFrame(app)

	require.NotNil(t, frame)
	assert.NotNil(t, frame.Box)
	assert.NotNil(t, frame.helpText)
}

func TestPageFrame_SetTitle(t *testing.T) {
	t.Parallel()

	app := tview.NewApplication()
	frame := NewPageFrame(app)

	result := frame.SetTitle("Test Title")

	// Should return self for chaining
	assert.Same(t, frame, result)
}

func TestPageFrame_SetHelpText(t *testing.T) {
	t.Parallel()

	app := tview.NewApplication()
	frame := NewPageFrame(app)

	result := frame.SetHelpText("Press Enter to continue")

	// Should return self for chaining
	assert.Same(t, frame, result)
}

func TestPageFrame_SetContent(t *testing.T) {
	t.Parallel()

	app := tview.NewApplication()
	frame := NewPageFrame(app)
	content := tview.NewTextView().SetText("Hello")

	result := frame.SetContent(content)

	// Should return self for chaining
	assert.Same(t, frame, result)
	// Content should be set
	assert.Equal(t, content, frame.content)
}

func TestPageFrame_SetButtonBar(t *testing.T) {
	t.Parallel()

	app := tview.NewApplication()
	frame := NewPageFrame(app)
	buttonBar := NewButtonBar(app)

	result := frame.SetButtonBar(buttonBar)

	// Should return self for chaining
	assert.Same(t, frame, result)
	assert.Equal(t, buttonBar, frame.buttonBar)
}

func TestPageFrame_SetOnEscape(t *testing.T) {
	t.Parallel()

	app := tview.NewApplication()
	frame := NewPageFrame(app)
	escapeCalled := false

	result := frame.SetOnEscape(func() {
		escapeCalled = true
	})

	// Should return self for chaining
	assert.Same(t, frame, result)

	// Trigger escape callback
	if frame.onEscape != nil {
		frame.onEscape()
	}
	assert.True(t, escapeCalled)
}

func TestPageFrame_FocusButtonBar(t *testing.T) {
	t.Parallel()

	app := tview.NewApplication()
	frame := NewPageFrame(app)
	buttonBar := NewButtonBar(app)
	buttonBar.AddButton("Test", func() {})

	frame.SetButtonBar(buttonBar)

	// Just verify it doesn't panic - actual focus testing requires a running app
	frame.FocusButtonBar()
}

func TestPageFrame_Chaining(t *testing.T) {
	t.Parallel()

	app := tview.NewApplication()
	frame := NewPageFrame(app).
		SetTitle("Title").
		SetHelpText("Help").
		SetContent(tview.NewBox())

	require.NotNil(t, frame)
}
