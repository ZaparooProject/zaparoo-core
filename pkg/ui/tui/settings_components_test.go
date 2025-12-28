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

	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatToggle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		label         string
		shouldHave    []string
		shouldNotHave []string
		value         bool
		selected      bool
	}{
		{
			name:       "enabled and selected",
			value:      true,
			label:      "Audio feedback",
			selected:   true,
			shouldHave: []string{"[*]", "Audio feedback", "yellow", "black:yellow"},
		},
		{
			name:          "enabled but not selected",
			value:         true,
			label:         "Auto-detect",
			selected:      false,
			shouldHave:    []string{"[*]", "Auto-detect", "white"},
			shouldNotHave: []string{"black:yellow"},
		},
		{
			name:       "disabled and selected",
			value:      false,
			label:      "Debug mode",
			selected:   true,
			shouldHave: []string{"[ ]", "Debug mode", "black:yellow"},
		},
		{
			name:          "disabled and not selected",
			value:         false,
			label:         "Test option",
			selected:      false,
			shouldHave:    []string{"[ ]", "Test option", "white"},
			shouldNotHave: []string{"black:yellow"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := formatToggle(tt.value, tt.label, tt.selected)

			for _, substr := range tt.shouldHave {
				assert.Contains(t, result, substr,
					"expected %q to contain %q", result, substr)
			}
			for _, substr := range tt.shouldNotHave {
				assert.NotContains(t, result, substr,
					"expected %q to not contain %q", result, substr)
			}
		})
	}
}

func TestFormatCycle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		label         string
		currentValue  string
		shouldHave    []string
		shouldNotHave []string
		selected      bool
	}{
		{
			name:         "selected cycle option",
			label:        "Scan mode",
			currentValue: "Tap",
			selected:     true,
			shouldHave:   []string{"Scan mode", "< Tap >", "black:yellow"},
		},
		{
			name:          "unselected cycle option",
			label:         "Exit delay",
			currentValue:  "5 seconds",
			selected:      false,
			shouldHave:    []string{"Exit delay", "< 5 seconds >", "white"},
			shouldNotHave: []string{"black:yellow"},
		},
		{
			name:         "empty value",
			label:        "Option",
			currentValue: "",
			selected:     false,
			shouldHave:   []string{"Option", "<  >"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := formatCycle(tt.label, tt.currentValue, tt.selected)

			for _, substr := range tt.shouldHave {
				assert.Contains(t, result, substr,
					"expected %q to contain %q", result, substr)
			}
			for _, substr := range tt.shouldNotHave {
				assert.NotContains(t, result, substr,
					"expected %q to not contain %q", result, substr)
			}
		})
	}
}

func TestFormatAction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		label         string
		shouldHave    []string
		shouldNotHave []string
		selected      bool
	}{
		{
			name:       "selected action",
			label:      "Go back",
			selected:   true,
			shouldHave: []string{"Go back", "black:yellow"},
		},
		{
			name:          "unselected action",
			label:         "Settings",
			selected:      false,
			shouldHave:    []string{"Settings", "white"},
			shouldNotHave: []string{"black:yellow"},
		},
		{
			name:       "action with special characters",
			label:      "Manage readers (5)",
			selected:   true,
			shouldHave: []string{"Manage readers (5)", "black:yellow"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := formatAction(tt.label, tt.selected)

			for _, substr := range tt.shouldHave {
				assert.Contains(t, result, substr,
					"expected %q to contain %q", result, substr)
			}
			for _, substr := range tt.shouldNotHave {
				assert.NotContains(t, result, substr,
					"expected %q to not contain %q", result, substr)
			}
		})
	}
}

func TestFormatDesc(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		desc     string
		expected string
	}{
		{
			name:     "simple description",
			desc:     "Some description",
			expected: "  Some description",
		},
		{
			name:     "empty description",
			desc:     "",
			expected: "  ",
		},
		{
			name:     "description with leading space",
			desc:     " already indented",
			expected: "   already indented",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := formatDesc(tt.desc)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSettingsList_Creation(t *testing.T) {
	t.Parallel()

	pages := tview.NewPages()
	sl := NewSettingsList(pages, "main")

	require.NotNil(t, sl)
	require.NotNil(t, sl.List)
	assert.Equal(t, 0, sl.GetItemCount())
	assert.Equal(t, "main", sl.previousPage)
}

func TestSettingsList_AddToggle(t *testing.T) {
	t.Parallel()

	pages := tview.NewPages()
	sl := NewSettingsList(pages, "main")

	toggleValue := false
	toggleCalled := false

	sl.AddToggle("Test Toggle", "A test toggle", &toggleValue, func(_ bool) {
		toggleCalled = true
	})

	assert.Equal(t, 1, sl.GetItemCount())
	assert.Len(t, sl.items, 1)
	assert.Equal(t, "toggle", sl.items[0].itemType)
	assert.Equal(t, "Test Toggle", sl.items[0].label)
	assert.Equal(t, "A test toggle", sl.items[0].description)
	assert.False(t, toggleCalled, "callback should not be called on add")
}

func TestSettingsList_AddCycle(t *testing.T) {
	t.Parallel()

	pages := tview.NewPages()
	sl := NewSettingsList(pages, "main")

	cycleIndex := 0
	options := []string{"Option A", "Option B", "Option C"}

	sl.AddCycle("Test Cycle", "A test cycle", options, &cycleIndex, func(_ string, _ int) {})

	assert.Equal(t, 1, sl.GetItemCount())
	assert.Len(t, sl.items, 1)
	assert.Equal(t, "cycle", sl.items[0].itemType)
	assert.Equal(t, "Test Cycle", sl.items[0].label)
	assert.Equal(t, options, sl.items[0].cycleOptions)
}

func TestSettingsList_AddAction(t *testing.T) {
	t.Parallel()

	pages := tview.NewPages()
	sl := NewSettingsList(pages, "main")

	actionCalled := false
	sl.AddAction("Test Action", "A test action", func() {
		actionCalled = true
	})

	assert.Equal(t, 1, sl.GetItemCount())
	assert.Len(t, sl.items, 1)
	assert.Equal(t, "action", sl.items[0].itemType)
	assert.Equal(t, "Test Action", sl.items[0].label)
	assert.False(t, actionCalled, "action should not be called on add")
}

func TestSettingsList_AddBack(t *testing.T) {
	t.Parallel()

	pages := tview.NewPages()
	sl := NewSettingsList(pages, "main")

	sl.AddBack()

	assert.Equal(t, 1, sl.GetItemCount())
	assert.Len(t, sl.items, 1)
	assert.Equal(t, "action", sl.items[0].itemType)
	assert.Equal(t, "Back", sl.items[0].label)
	assert.Equal(t, "Return to previous menu", sl.items[0].description)
}

func TestSettingsList_AddBackWithDesc(t *testing.T) {
	t.Parallel()

	pages := tview.NewPages()
	sl := NewSettingsList(pages, "main")

	sl.AddBackWithDesc("Custom description")

	assert.Equal(t, 1, sl.GetItemCount())
	assert.Equal(t, "Custom description", sl.items[0].description)
}

func TestSettingsList_ChainedMethods(t *testing.T) {
	t.Parallel()

	pages := tview.NewPages()
	sl := NewSettingsList(pages, "main")

	toggleVal := false
	cycleIdx := 0

	// Verify chaining works
	sl.AddToggle("Toggle", "desc", &toggleVal, func(bool) {}).
		AddCycle("Cycle", "desc", []string{"A", "B"}, &cycleIdx, func(string, int) {}).
		AddAction("Action", "desc", func() {}).
		AddBack()

	assert.Equal(t, 4, sl.GetItemCount())
}

func TestButtonBar_Creation(t *testing.T) {
	t.Parallel()

	app := tview.NewApplication()
	bb := NewButtonBar(app)

	require.NotNil(t, bb)
	require.NotNil(t, bb.Box)
	assert.Empty(t, bb.buttons)
}

func TestButtonBar_AddButton(t *testing.T) {
	t.Parallel()

	app := tview.NewApplication()
	bb := NewButtonBar(app)

	buttonPressed := false
	bb.AddButton("Test", func() {
		buttonPressed = true
	})

	assert.Len(t, bb.buttons, 1)
	assert.False(t, buttonPressed, "button should not be pressed on add")
}

func TestButtonBar_GetFirstButton(t *testing.T) {
	t.Parallel()

	app := tview.NewApplication()
	bb := NewButtonBar(app)

	// Empty bar returns nil
	assert.Nil(t, bb.GetFirstButton())

	bb.AddButton("First", func() {})
	bb.AddButton("Second", func() {})

	firstBtn := bb.GetFirstButton()
	require.NotNil(t, firstBtn)
	assert.Equal(t, "First", firstBtn.GetLabel())
}

func TestButtonBar_ChainedMethods(t *testing.T) {
	t.Parallel()

	app := tview.NewApplication()
	bb := NewButtonBar(app)

	bb.AddButton("One", func() {}).
		AddButton("Two", func() {}).
		AddButton("Three", func() {})

	assert.Len(t, bb.buttons, 3)
}
