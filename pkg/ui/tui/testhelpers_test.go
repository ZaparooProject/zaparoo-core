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
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/stretchr/testify/require"
)

// TestScreen wraps a SimulationScreen with helper methods for testing.
type TestScreen struct {
	tcell.SimulationScreen
	t         *testing.T
	width     int
	height    int
	finalized bool
}

// NewTestScreen creates and initializes a simulation screen for testing.
func NewTestScreen(t *testing.T, width, height int) *TestScreen {
	t.Helper()
	sim := tcell.NewSimulationScreen("UTF-8")
	require.NotNil(t, sim, "failed to create simulation screen")

	err := sim.Init()
	require.NoError(t, err, "failed to initialize simulation screen")

	sim.SetSize(width, height)

	return &TestScreen{
		SimulationScreen: sim,
		t:                t,
		width:            width,
		height:           height,
	}
}

// InjectKeyPress simulates a key press with optional modifiers.
func (s *TestScreen) InjectKeyPress(key tcell.Key, r rune, mod tcell.ModMask) {
	s.InjectKey(key, r, mod)
}

// InjectEnter simulates pressing the Enter key.
func (s *TestScreen) InjectEnter() {
	s.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
}

// InjectEscape simulates pressing the Escape key.
func (s *TestScreen) InjectEscape() {
	s.InjectKey(tcell.KeyEscape, 0, tcell.ModNone)
}

// InjectTab simulates pressing the Tab key.
func (s *TestScreen) InjectTab() {
	s.InjectKey(tcell.KeyTab, 0, tcell.ModNone)
}

// InjectBacktab simulates pressing Shift+Tab.
func (s *TestScreen) InjectBacktab() {
	s.InjectKey(tcell.KeyBacktab, 0, tcell.ModNone)
}

// InjectArrowDown simulates pressing the Down arrow key.
func (s *TestScreen) InjectArrowDown() {
	s.InjectKey(tcell.KeyDown, 0, tcell.ModNone)
}

// InjectArrowUp simulates pressing the Up arrow key.
func (s *TestScreen) InjectArrowUp() {
	s.InjectKey(tcell.KeyUp, 0, tcell.ModNone)
}

// InjectArrowLeft simulates pressing the Left arrow key.
func (s *TestScreen) InjectArrowLeft() {
	s.InjectKey(tcell.KeyLeft, 0, tcell.ModNone)
}

// InjectArrowRight simulates pressing the Right arrow key.
func (s *TestScreen) InjectArrowRight() {
	s.InjectKey(tcell.KeyRight, 0, tcell.ModNone)
}

// InjectRune simulates typing a character.
func (s *TestScreen) InjectRune(r rune) {
	s.InjectKey(tcell.KeyRune, r, tcell.ModNone)
}

// InjectString simulates typing a string of characters.
func (s *TestScreen) InjectString(str string) {
	for _, r := range str {
		s.InjectRune(r)
	}
}

// GetCellContent returns the rune at a specific position.
func (s *TestScreen) GetCellContent(x, y int) rune {
	cells, width, _ := s.GetContents()
	idx := y*width + x
	if idx < len(cells) && len(cells[idx].Runes) > 0 {
		return cells[idx].Runes[0]
	}
	return ' '
}

// GetCellStyle returns the style at a specific position.
func (s *TestScreen) GetCellStyle(x, y int) tcell.Style {
	cells, width, _ := s.GetContents()
	idx := y*width + x
	if idx < len(cells) {
		return cells[idx].Style
	}
	return tcell.StyleDefault
}

// GetLineContent returns the text content of a specific line.
func (s *TestScreen) GetLineContent(y int) string {
	cells, width, height := s.GetContents()
	if y < 0 || y >= height {
		return ""
	}

	var sb strings.Builder
	for x := range width {
		cell := cells[y*width+x]
		if len(cell.Runes) > 0 {
			sb.WriteRune(cell.Runes[0])
		} else {
			sb.WriteRune(' ')
		}
	}
	return strings.TrimRight(sb.String(), " ")
}

// GetScreenText returns all screen content as a single string.
func (s *TestScreen) GetScreenText() string {
	cells, width, height := s.GetContents()
	var sb strings.Builder
	for y := range height {
		for x := range width {
			cell := cells[y*width+x]
			if len(cell.Runes) > 0 {
				sb.WriteRune(cell.Runes[0])
			} else {
				sb.WriteRune(' ')
			}
		}
		if y < height-1 {
			sb.WriteRune('\n')
		}
	}
	return sb.String()
}

// ContainsText checks if the screen contains the specified text anywhere.
func (s *TestScreen) ContainsText(text string) bool {
	screenText := s.GetScreenText()
	return strings.Contains(screenText, text)
}

// ContainsTextOnLine checks if a specific line contains the text.
func (s *TestScreen) ContainsTextOnLine(y int, text string) bool {
	lineContent := s.GetLineContent(y)
	return strings.Contains(lineContent, text)
}

// DumpScreen returns a formatted string representation of the screen for debugging.
func (s *TestScreen) DumpScreen() string {
	cells, width, height := s.GetContents()
	var sb strings.Builder
	sb.WriteString("Screen dump:\n")
	sb.WriteString(strings.Repeat("-", width) + "\n")
	for y := range height {
		for x := range width {
			cell := cells[y*width+x]
			if len(cell.Runes) > 0 {
				sb.WriteRune(cell.Runes[0])
			} else {
				sb.WriteRune(' ')
			}
		}
		sb.WriteRune('\n')
	}
	sb.WriteString(strings.Repeat("-", width) + "\n")
	return sb.String()
}

// Cleanup should be called when done with the screen.
func (s *TestScreen) Cleanup() {
	if !s.finalized {
		s.finalized = true
		s.Fini()
	}
}
