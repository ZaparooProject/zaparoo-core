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

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Key action constants for special keys.
const (
	keyActionBackspace = "DEL"
	keyActionEnter     = "OK"
	keyActionShift     = "SHFT"
	keyActionSymbols   = "SYM"
	keyActionSpace     = "SPC"
	keyActionCancel    = "CANC"
)

// Keyboard layouts for each mode.
var (
	keysLower = [][]string{
		{"1", "2", "3", "4", "5", "6", "7", "8", "9", "0"},
		{"q", "w", "e", "r", "t", "y", "u", "i", "o", "p"},
		{"a", "s", "d", "f", "g", "h", "j", "k", "l"},
		{"z", "x", "c", "v", "b", "n", "m", ",", "."},
		{keyActionShift, keyActionSymbols, keyActionSpace, keyActionBackspace, keyActionEnter, keyActionCancel},
	}
	keysUpper = [][]string{
		{"1", "2", "3", "4", "5", "6", "7", "8", "9", "0"},
		{"Q", "W", "E", "R", "T", "Y", "U", "I", "O", "P"},
		{"A", "S", "D", "F", "G", "H", "J", "K", "L"},
		{"Z", "X", "C", "V", "B", "N", "M", ",", "."},
		{keyActionShift, keyActionSymbols, keyActionSpace, keyActionBackspace, keyActionEnter, keyActionCancel},
	}
	keysSymbols = [][]string{
		{"!", "@", "#", "$", "%", "^", "&", "*", "(", ")"},
		{"-", "_", "=", "+", "[", "]", "{", "}", "\\", "|"},
		{";", ":", "'", "\"", "`", "~", "/", "?", "<", ">"},
		{}, // Empty row to align bottom row with other modes
		{"ABC", keyActionSpace, keyActionBackspace, keyActionEnter, keyActionCancel},
	}
)

// OnScreenKeyboard provides a virtual keyboard for controller input.
type OnScreenKeyboard struct {
	*tview.Box
	onSubmit  func(string)
	onCancel  func()
	text      string
	cursorRow int
	cursorCol int
	shiftOn   bool
	symbolsOn bool
}

// NewOnScreenKeyboard creates a new on-screen keyboard widget.
func NewOnScreenKeyboard(initialText string, onSubmit func(string), onCancel func()) *OnScreenKeyboard {
	osk := &OnScreenKeyboard{
		Box:      tview.NewBox(),
		text:     initialText,
		onSubmit: onSubmit,
		onCancel: onCancel,
	}
	osk.SetBorder(true)
	SetBoxTitle(osk, "Keyboard")
	return osk
}

// GetText returns the current input text.
func (o *OnScreenKeyboard) GetText() string {
	return o.text
}

// SetText sets the current input text.
func (o *OnScreenKeyboard) SetText(text string) *OnScreenKeyboard {
	o.text = text
	return o
}

// currentLayout returns the active keyboard layout based on mode.
func (o *OnScreenKeyboard) currentLayout() [][]string {
	if o.symbolsOn {
		return keysSymbols
	}
	if o.shiftOn {
		return keysUpper
	}
	return keysLower
}

// Draw renders the keyboard to the screen.
func (o *OnScreenKeyboard) Draw(screen tcell.Screen) {
	o.DrawForSubclass(screen, o)
	x, y, width, height := o.GetInnerRect()
	theme := CurrentTheme()

	// Draw input field at the top
	inputY := y
	inputStyle := tcell.StyleDefault.
		Foreground(theme.PrimaryTextColor).
		Background(theme.FieldFocusedBg)

	// Draw input background
	for i := x; i < x+width; i++ {
		screen.SetContent(i, inputY, ' ', nil, inputStyle)
	}

	// Draw input text (truncate if too long)
	displayText := o.text
	maxLen := width - 2
	if len(displayText) > maxLen {
		displayText = displayText[len(displayText)-maxLen:]
	}
	for i, r := range displayText {
		screen.SetContent(x+1+i, inputY, r, nil, inputStyle)
	}

	// Draw cursor
	cursorPos := x + 1 + len(displayText)
	if cursorPos < x+width-1 {
		screen.SetContent(cursorPos, inputY, '_', nil, inputStyle.Blink(true))
	}

	// Draw keyboard directly below input
	keyboardY := inputY + 1
	layout := o.currentLayout()

	for rowIdx, row := range layout {
		if keyboardY+rowIdx >= y+height {
			break
		}
		o.drawKeyRow(screen, x, keyboardY+rowIdx, row, rowIdx, theme)
	}
}

// oskKeyboardWidth is the total width of the keyboard grid.
const oskKeyboardWidth = 39

// drawKeyRow renders a single row of keys.
func (o *OnScreenKeyboard) drawKeyRow(
	screen tcell.Screen,
	x, y int,
	row []string,
	rowIdx int,
	theme *Theme,
) {
	// Skip empty rows
	if len(row) == 0 {
		return
	}

	layout := o.currentLayout()
	isBottomRow := rowIdx == len(layout)-1

	if isBottomRow {
		o.drawBottomRow(screen, x, y, row, theme)
		return
	}

	// Regular rows: left-aligned grid
	keyWidth := 4
	for colIdx, key := range row {
		isSelected := rowIdx == o.cursorRow && colIdx == o.cursorCol
		style := oskKeyStyle(key, isSelected, theme)
		keyX := x + colIdx*keyWidth
		oskDrawKey(screen, keyX, y, key, keyWidth-1, style)
	}
}

// drawBottomRow renders the bottom action row with custom spacing.
// Layout: [SHFT SYM] ... [SPC] ... [DEL OK CANC]
func (o *OnScreenKeyboard) drawBottomRow(
	screen tcell.Screen,
	x, y int,
	row []string,
	theme *Theme,
) {
	const (
		btnWidth   = 5 // Width for regular buttons
		spaceWidth = 7 // Wider space bar
	)

	// Find indices based on row length
	// Lower/Upper: SHFT(0) SYM(1) SPC(2) DEL(3) OK(4) CANC(5)
	// Symbols:     ABC(0) SPC(1) DEL(2) OK(3) CANC(4)
	var leftEnd, spaceIdx, rightStart int
	if len(row) == 6 {
		leftEnd = 2    // SHFT, SYM
		spaceIdx = 2   // SPC
		rightStart = 3 // DEL, OK, CANC
	} else {
		leftEnd = 1    // ABC
		spaceIdx = 1   // SPC
		rightStart = 2 // DEL, OK, CANC
	}

	// Calculate positions
	spaceX := x + (oskKeyboardWidth-spaceWidth)/2
	rightX := x + oskKeyboardWidth - (len(row)-rightStart)*btnWidth

	for colIdx, key := range row {
		isSelected := o.cursorRow == len(o.currentLayout())-1 && colIdx == o.cursorCol
		style := oskKeyStyle(key, isSelected, theme)

		var keyX, width int
		switch {
		case colIdx < leftEnd:
			// Left group
			keyX = x + colIdx*btnWidth
			width = btnWidth - 1
		case colIdx == spaceIdx:
			// Center space bar
			keyX = spaceX
			width = spaceWidth - 1
		default:
			// Right group
			keyX = rightX + (colIdx-rightStart)*btnWidth
			width = btnWidth - 1
		}

		oskDrawKey(screen, keyX, y, key, width, style)
	}
}

// oskKeyStyle returns the appropriate style for a key.
func oskKeyStyle(key string, isSelected bool, theme *Theme) tcell.Style {
	switch {
	case isSelected:
		return tcell.StyleDefault.
			Foreground(tcell.GetColor(theme.HighlightFgName)).
			Background(tcell.GetColor(theme.HighlightBgName))
	case isActionKey(key):
		return tcell.StyleDefault.
			Foreground(theme.LabelColor).
			Background(theme.PrimitiveBackgroundColor)
	default:
		return tcell.StyleDefault.
			Foreground(theme.PrimaryTextColor).
			Background(theme.PrimitiveBackgroundColor)
	}
}

// oskDrawKey draws a single key with centered label.
func oskDrawKey(screen tcell.Screen, x, y int, label string, width int, style tcell.Style) {
	padding := (width - len(label)) / 2
	for i := range width {
		ch := ' '
		labelIdx := i - padding
		if labelIdx >= 0 && labelIdx < len(label) {
			ch = rune(label[labelIdx])
		}
		screen.SetContent(x+i, y, ch, nil, style)
	}
}

// isActionKey returns true if the key is a special action key.
func isActionKey(key string) bool {
	switch key {
	case keyActionBackspace, keyActionEnter, keyActionShift,
		keyActionSymbols, keyActionSpace, keyActionCancel, "ABC":
		return true
	}
	return false
}

// InputHandler handles keyboard input for navigation and selection.
func (o *OnScreenKeyboard) InputHandler() func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
	return o.WrapInputHandler(func(event *tcell.EventKey, _ func(p tview.Primitive)) {
		layout := o.currentLayout()

		switch event.Key() {
		case tcell.KeyUp:
			fromRow := o.cursorRow
			o.cursorRow--
			if o.cursorRow < 0 {
				o.cursorRow = len(layout) - 1
			}
			// Skip empty rows
			for len(layout[o.cursorRow]) == 0 {
				o.cursorRow--
				if o.cursorRow < 0 {
					o.cursorRow = len(layout) - 1
				}
			}
			o.cursorCol = o.mapColumnForVerticalNav(fromRow, o.cursorRow, o.cursorCol)

		case tcell.KeyDown:
			fromRow := o.cursorRow
			o.cursorRow++
			if o.cursorRow >= len(layout) {
				o.cursorRow = 0
			}
			// Skip empty rows
			for len(layout[o.cursorRow]) == 0 {
				o.cursorRow++
				if o.cursorRow >= len(layout) {
					o.cursorRow = 0
				}
			}
			o.cursorCol = o.mapColumnForVerticalNav(fromRow, o.cursorRow, o.cursorCol)

		case tcell.KeyLeft:
			o.cursorCol--
			if o.cursorCol < 0 {
				o.cursorCol = len(layout[o.cursorRow]) - 1
			}

		case tcell.KeyRight:
			o.cursorCol++
			if o.cursorCol >= len(layout[o.cursorRow]) {
				o.cursorCol = 0
			}

		case tcell.KeyEnter:
			o.activateKey()

		case tcell.KeyEscape:
			if o.onCancel != nil {
				o.onCancel()
			}

		case tcell.KeyBackspace, tcell.KeyBackspace2:
			if o.text != "" {
				o.text = o.text[:len(o.text)-1]
			}

		case tcell.KeyRune:
			// Direct character input (for physical keyboards)
			o.text += string(event.Rune())

		default:
			// Ignore other keys
		}
	})
}

// mapColumnForVerticalNav maps column when navigating between bottom row and letter rows.
// Uses exact visual alignment mapping:
// z↔SHFT, x/c↔SYM, v/b/n↔SPC, m↔DEL, ,↔OK, .↔CANC
func (o *OnScreenKeyboard) mapColumnForVerticalNav(fromRow, toRow, col int) int {
	layout := o.currentLayout()
	isFromBottom := fromRow == len(layout)-1
	isToBottom := toRow == len(layout)-1
	toRowLen := len(layout[toRow])

	if toRowLen == 0 {
		return 0
	}

	bottomRowLen := len(layout[len(layout)-1])

	if isFromBottom && !isToBottom {
		// Moving UP from bottom row to letter row
		if bottomRowLen == 6 {
			// Lower/upper: SHFT(0) SYM(1) SPC(2) DEL(3) OK(4) CANC(5)
			switch col {
			case 0: // SHFT → z
				return 0
			case 1: // SYM → x
				return min(1, toRowLen-1)
			case 2: // SPC → b (middle of v,b,n)
				return min(4, toRowLen-1)
			case 3: // DEL → m
				return min(6, toRowLen-1)
			case 4: // OK → ,
				return min(7, toRowLen-1)
			default: // CANC → .
				return toRowLen - 1
			}
		}
		// Symbols: ABC(0) SPC(1) DEL(2) OK(3) CANC(4)
		switch col {
		case 0: // ABC → leftmost
			return 0
		case 1: // SPC → center
			return min(toRowLen/2, toRowLen-1)
		case 2: // DEL
			return min(6, toRowLen-1)
		case 3: // OK
			return min(7, toRowLen-1)
		default: // CANC
			return toRowLen - 1
		}
	} else if !isFromBottom && isToBottom {
		// Moving DOWN from letter row to bottom row
		if bottomRowLen == 6 {
			// Lower/upper: map to SHFT(0) SYM(1) SPC(2) DEL(3) OK(4) CANC(5)
			switch col {
			case 0: // z → SHFT
				return 0
			case 1, 2: // x, c → SYM
				return 1
			case 3, 4, 5: // v, b, n → SPC
				return 2
			case 6: // m → DEL
				return 3
			case 7: // , → OK
				return 4
			default: // . → CANC
				return 5
			}
		}
		// Symbols: map to ABC(0) SPC(1) DEL(2) OK(3) CANC(4)
		switch {
		case col <= 1: // leftmost 2 → ABC
			return 0
		case col <= 5: // middle → SPC
			return 1
		case col == 6: // → DEL
			return 2
		case col == 7: // → OK
			return 3
		default: // → CANC
			return 4
		}
	}

	// Default: just clamp
	if col >= toRowLen {
		return toRowLen - 1
	}
	return col
}

// activateKey performs the action for the currently selected key.
func (o *OnScreenKeyboard) activateKey() {
	layout := o.currentLayout()
	if o.cursorRow >= len(layout) || o.cursorCol >= len(layout[o.cursorRow]) {
		return
	}

	key := layout[o.cursorRow][o.cursorCol]

	switch key {
	case keyActionBackspace:
		if o.text != "" {
			o.text = o.text[:len(o.text)-1]
		}

	case keyActionEnter:
		if o.onSubmit != nil {
			o.onSubmit(o.text)
		}

	case keyActionShift:
		o.shiftOn = !o.shiftOn
		o.symbolsOn = false

	case keyActionSymbols:
		o.symbolsOn = true
		o.shiftOn = false
		o.cursorCol = 0 // Focus ABC in symbols mode

	case "ABC":
		o.symbolsOn = false
		o.cursorCol = 0 // Focus SHFT in normal mode

	case keyActionSpace:
		o.text += " "

	case keyActionCancel:
		if o.onCancel != nil {
			o.onCancel()
		}

	default:
		// Regular character key
		if strings.TrimSpace(key) != "" {
			o.text += key
			// Auto-disable shift after typing a letter
			if o.shiftOn && !o.symbolsOn {
				o.shiftOn = false
			}
		}
	}
}

// Focus is called when the keyboard receives focus.
func (o *OnScreenKeyboard) Focus(delegate func(p tview.Primitive)) {
	o.Box.Focus(delegate)
}

// HasFocus returns whether the keyboard has focus.
func (o *OnScreenKeyboard) HasFocus() bool {
	return o.Box.HasFocus()
}
