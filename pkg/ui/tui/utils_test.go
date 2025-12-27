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
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCenterWidget(t *testing.T) {
	t.Parallel()

	textView := tview.NewTextView().SetText("Centered content")
	centered := CenterWidget(40, 10, textView)

	require.NotNil(t, centered)

	// Verify it's a Flex
	flex, ok := centered.(*tview.Flex)
	require.True(t, ok, "CenterWidget should return a Flex")
	assert.NotNil(t, flex)
}

func TestCenterWidget_DifferentSizes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		width  int
		height int
	}{
		{"small", 20, 5},
		{"medium", 60, 20},
		{"large", 100, 40},
		{"tall and narrow", 20, 50},
		{"wide and short", 80, 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			box := tview.NewBox()
			centered := CenterWidget(tt.width, tt.height, box)
			require.NotNil(t, centered)
		})
	}
}

func TestPageDefaults(t *testing.T) {
	t.Parallel()

	pages := tview.NewPages()
	textView := tview.NewTextView().SetText("Test content")

	result := pageDefaults("testPage", pages, textView)

	require.NotNil(t, result)

	// Verify page was added
	assert.True(t, pages.HasPage("testPage"))

	// Verify it's the front page
	name, _ := pages.GetFrontPage()
	assert.Equal(t, "testPage", name)
}

func TestPageDefaults_MultiplePAges(t *testing.T) {
	t.Parallel()

	pages := tview.NewPages()

	// Add first page
	tv1 := tview.NewTextView().SetText("Page 1")
	pageDefaults("page1", pages, tv1)

	// Add second page
	tv2 := tview.NewTextView().SetText("Page 2")
	pageDefaults("page2", pages, tv2)

	// Second page should be front
	name, _ := pages.GetFrontPage()
	assert.Equal(t, "page2", name)

	// Both pages should exist
	assert.True(t, pages.HasPage("page1"))
	assert.True(t, pages.HasPage("page2"))
}

func TestCurrentTheme(t *testing.T) {
	t.Parallel()

	theme := CurrentTheme()

	require.NotNil(t, theme)
	assert.NotEmpty(t, theme.Name)
	assert.NotEmpty(t, theme.DisplayName)
	assert.NotEmpty(t, theme.BgColorName)
	assert.NotEmpty(t, theme.AccentColorName)
	assert.NotEmpty(t, theme.TextColorName)
}

func TestSetCurrentTheme(t *testing.T) {
	// Not parallel - modifies global tview.Styles which races with widget creation in other tests

	// Setting a valid theme should return true
	ok := SetCurrentTheme("default")
	assert.True(t, ok)
	assert.Equal(t, "default", CurrentTheme().Name)

	// Setting an invalid theme should return false
	ok = SetCurrentTheme("nonexistent_theme")
	assert.False(t, ok)

	// Theme should still be the last valid one
	assert.Equal(t, "default", CurrentTheme().Name)
}

func TestAvailableThemes(t *testing.T) {
	t.Parallel()

	assert.NotEmpty(t, AvailableThemes)
	assert.NotEmpty(t, ThemeNames)

	// All names in ThemeNames should exist in AvailableThemes
	for _, name := range ThemeNames {
		theme, ok := AvailableThemes[name]
		assert.True(t, ok, "Theme %s should exist in AvailableThemes", name)
		assert.NotNil(t, theme)
		assert.Equal(t, name, theme.Name)
	}
}

func TestThemeDefaultColors(t *testing.T) {
	t.Parallel()

	theme := &ThemeDefault

	assert.Equal(t, tcell.ColorLightYellow, theme.BorderColor)
	assert.Equal(t, tcell.ColorWhite, theme.PrimaryTextColor)
	assert.Equal(t, tcell.ColorDarkBlue, theme.PrimitiveBackgroundColor)
	assert.Equal(t, tcell.ColorBlue, theme.ContrastBackgroundColor)
	assert.Equal(t, "darkblue", theme.BgColorName)
	assert.Equal(t, "yellow", theme.AccentColorName)
}

func TestGenericModal_WithButton(t *testing.T) {
	t.Parallel()

	var callbackCalled bool
	var callbackIndex int
	var callbackLabel string

	modal := genericModal(
		"Test message",
		"Test Title",
		func(buttonIndex int, buttonLabel string) {
			callbackCalled = true
			callbackIndex = buttonIndex
			callbackLabel = buttonLabel
		},
		true,
	)

	require.NotNil(t, modal)
	assert.False(t, callbackCalled, "callback should not be called on creation")
	_ = callbackIndex
	_ = callbackLabel
}

func TestGenericModal_WithoutButton(t *testing.T) {
	t.Parallel()

	modal := genericModal(
		"Message without button",
		"Title",
		nil,
		false,
	)

	require.NotNil(t, modal)
}

func TestGenericModal_EmptyMessage(t *testing.T) {
	t.Parallel()

	modal := genericModal("", "Empty Message", nil, true)
	require.NotNil(t, modal)
}

func TestExitDelayOptions(t *testing.T) {
	t.Parallel()

	// Verify the exit delay options are present
	assert.NotEmpty(t, ExitDelayOptions)

	// Verify first option is 0 seconds
	assert.Equal(t, "0 seconds", ExitDelayOptions[0].Label)
	assert.InDelta(t, 0, ExitDelayOptions[0].Value, 0.001)

	// Verify options contain expected values (label and value)
	expectedOptions := []struct {
		label string
		value float32
	}{
		{"0 seconds", 0},
		{"5 seconds", 5},
		{"10 seconds", 10},
	}
	for _, expected := range expectedOptions {
		found := false
		for _, opt := range ExitDelayOptions {
			if opt.Label == expected.label && opt.Value == expected.value {
				found = true
				break
			}
		}
		assert.True(t, found, "ExitDelayOptions should contain {%s, %f}", expected.label, expected.value)
	}
}
