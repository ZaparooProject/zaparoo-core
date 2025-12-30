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
	"github.com/stretchr/testify/assert"
)

func TestNewOnScreenKeyboard(t *testing.T) {
	t.Parallel()

	var submittedText string
	var cancelCalled bool

	osk := NewOnScreenKeyboard(
		"initial",
		func(text string) { submittedText = text },
		func() { cancelCalled = true },
	)

	assert.NotNil(t, osk)
	assert.Equal(t, "initial", osk.GetText())
	assert.False(t, cancelCalled)
	assert.Empty(t, submittedText)
}

func TestOnScreenKeyboard_SetText(t *testing.T) {
	t.Parallel()

	osk := NewOnScreenKeyboard("", nil, nil)
	osk.SetText("new text")
	assert.Equal(t, "new text", osk.GetText())
}

func TestOnScreenKeyboard_LayoutSwitching(t *testing.T) {
	t.Parallel()

	osk := NewOnScreenKeyboard("", nil, nil)

	// Default is lowercase
	layout := osk.currentLayout()
	assert.Equal(t, "q", layout[1][0]) // First letter in QWERTY row

	// Enable shift for uppercase
	osk.shiftOn = true
	layout = osk.currentLayout()
	assert.Equal(t, "Q", layout[1][0])

	// Enable symbols
	osk.shiftOn = false
	osk.symbolsOn = true
	layout = osk.currentLayout()
	assert.Equal(t, "!", layout[0][0]) // First symbol
}

func TestOnScreenKeyboard_Navigation(t *testing.T) {
	t.Parallel()

	osk := NewOnScreenKeyboard("", nil, nil)
	handler := osk.InputHandler()

	// Initial position is 0,0
	assert.Equal(t, 0, osk.cursorRow)
	assert.Equal(t, 0, osk.cursorCol)

	// Move right
	event := tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone)
	handler(event, nil)
	assert.Equal(t, 0, osk.cursorRow)
	assert.Equal(t, 1, osk.cursorCol)

	// Move down
	event = tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)
	handler(event, nil)
	assert.Equal(t, 1, osk.cursorRow)
	assert.Equal(t, 1, osk.cursorCol)

	// Move left
	event = tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModNone)
	handler(event, nil)
	assert.Equal(t, 1, osk.cursorRow)
	assert.Equal(t, 0, osk.cursorCol)

	// Move up
	event = tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)
	handler(event, nil)
	assert.Equal(t, 0, osk.cursorRow)
	assert.Equal(t, 0, osk.cursorCol)
}

func TestOnScreenKeyboard_WrapAround(t *testing.T) {
	t.Parallel()

	osk := NewOnScreenKeyboard("", nil, nil)
	handler := osk.InputHandler()
	layout := osk.currentLayout()

	// Move up from row 0 should wrap to last row
	event := tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)
	handler(event, nil)
	assert.Equal(t, len(layout)-1, osk.cursorRow)

	// Move down from last row should wrap to row 0
	event = tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)
	handler(event, nil)
	assert.Equal(t, 0, osk.cursorRow)

	// Reset to col 0, then test left wrap
	osk.cursorCol = 0
	event = tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModNone)
	handler(event, nil)
	assert.Equal(t, len(layout[0])-1, osk.cursorCol)

	// Test right wrap from last col
	event = tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone)
	handler(event, nil)
	assert.Equal(t, 0, osk.cursorCol)
}

func TestOnScreenKeyboard_Backspace(t *testing.T) {
	t.Parallel()

	osk := NewOnScreenKeyboard("hello", nil, nil)
	handler := osk.InputHandler()

	event := tcell.NewEventKey(tcell.KeyBackspace, 0, tcell.ModNone)
	handler(event, nil)
	assert.Equal(t, "hell", osk.GetText())

	// Backspace on empty string should not panic
	osk.SetText("")
	handler(event, nil)
	assert.Empty(t, osk.GetText())
}

func TestOnScreenKeyboard_DirectInput(t *testing.T) {
	t.Parallel()

	osk := NewOnScreenKeyboard("", nil, nil)
	handler := osk.InputHandler()

	// Direct character input (for physical keyboards)
	event := tcell.NewEventKey(tcell.KeyRune, 'a', tcell.ModNone)
	handler(event, nil)
	assert.Equal(t, "a", osk.GetText())

	event = tcell.NewEventKey(tcell.KeyRune, 'b', tcell.ModNone)
	handler(event, nil)
	assert.Equal(t, "ab", osk.GetText())
}

func TestOnScreenKeyboard_ActivateKey(t *testing.T) {
	t.Parallel()

	t.Run("regular character", func(t *testing.T) {
		t.Parallel()
		osk := NewOnScreenKeyboard("", nil, nil)
		// Position on 'q' (row 1, col 0)
		osk.cursorRow = 1
		osk.cursorCol = 0
		osk.activateKey()
		assert.Equal(t, "q", osk.GetText())
	})

	t.Run("space", func(t *testing.T) {
		t.Parallel()
		osk := NewOnScreenKeyboard("test", nil, nil)
		// Position on SPC (row 4, col 2)
		osk.cursorRow = 4
		osk.cursorCol = 2
		osk.activateKey()
		assert.Equal(t, "test ", osk.GetText())
	})

	t.Run("backspace", func(t *testing.T) {
		t.Parallel()
		osk := NewOnScreenKeyboard("test", nil, nil)
		// Position on DEL (row 4, col 3)
		osk.cursorRow = 4
		osk.cursorCol = 3
		osk.activateKey()
		assert.Equal(t, "tes", osk.GetText())
	})

	t.Run("shift toggle", func(t *testing.T) {
		t.Parallel()
		osk := NewOnScreenKeyboard("", nil, nil)
		assert.False(t, osk.shiftOn)
		// Position on SHFT (row 4, col 0)
		osk.cursorRow = 4
		osk.cursorCol = 0
		osk.activateKey()
		assert.True(t, osk.shiftOn)
		// Toggle off
		osk.activateKey()
		assert.False(t, osk.shiftOn)
	})

	t.Run("symbols toggle", func(t *testing.T) {
		t.Parallel()
		osk := NewOnScreenKeyboard("", nil, nil)
		assert.False(t, osk.symbolsOn)
		// Position on SYM (row 4, col 1)
		osk.cursorRow = 4
		osk.cursorCol = 1
		osk.activateKey()
		assert.True(t, osk.symbolsOn)
	})

	t.Run("submit", func(t *testing.T) {
		t.Parallel()
		var submittedText string
		osk := NewOnScreenKeyboard("hello", func(text string) {
			submittedText = text
		}, nil)
		// Position on OK (row 4, col 4)
		osk.cursorRow = 4
		osk.cursorCol = 4
		osk.activateKey()
		assert.Equal(t, "hello", submittedText)
	})

	t.Run("cancel", func(t *testing.T) {
		t.Parallel()
		cancelCalled := false
		osk := NewOnScreenKeyboard("", nil, func() {
			cancelCalled = true
		})
		// Position on CANC (row 4, col 5)
		osk.cursorRow = 4
		osk.cursorCol = 5
		osk.activateKey()
		assert.True(t, cancelCalled)
	})
}

func TestOnScreenKeyboard_ShiftAutoDisable(t *testing.T) {
	t.Parallel()

	osk := NewOnScreenKeyboard("", nil, nil)
	osk.shiftOn = true

	// Type a letter (position on 'Q' -> row 1, col 0)
	osk.cursorRow = 1
	osk.cursorCol = 0
	osk.activateKey()

	// Shift should auto-disable after typing
	assert.False(t, osk.shiftOn)
	assert.Equal(t, "Q", osk.GetText())
}

func TestIsActionKey(t *testing.T) {
	t.Parallel()

	assert.True(t, isActionKey(keyActionBackspace))
	assert.True(t, isActionKey(keyActionEnter))
	assert.True(t, isActionKey(keyActionShift))
	assert.True(t, isActionKey(keyActionSymbols))
	assert.True(t, isActionKey(keyActionSpace))
	assert.True(t, isActionKey(keyActionCancel))
	assert.True(t, isActionKey("ABC"))

	assert.False(t, isActionKey("a"))
	assert.False(t, isActionKey("1"))
	assert.False(t, isActionKey("!"))
}

func TestOnScreenKeyboard_EscapeCancel(t *testing.T) {
	t.Parallel()

	cancelCalled := false
	osk := NewOnScreenKeyboard("", nil, func() {
		cancelCalled = true
	})
	handler := osk.InputHandler()

	event := tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)
	handler(event, nil)
	assert.True(t, cancelCalled)
}

func TestOnScreenKeyboard_EnterSubmit(t *testing.T) {
	t.Parallel()

	var submittedText string
	osk := NewOnScreenKeyboard("test", func(text string) {
		submittedText = text
	}, nil)
	handler := osk.InputHandler()

	// KeyEnter triggers activateKey which depends on cursor position
	// Position cursor on OK (row 4, col 4)
	osk.cursorRow = 4
	osk.cursorCol = 4

	event := tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone)
	handler(event, nil)
	assert.Equal(t, "test", submittedText)
}
