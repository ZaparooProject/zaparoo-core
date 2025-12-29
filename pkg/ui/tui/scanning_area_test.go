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

func TestWrapText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		text     string
		expected []string
		maxWidth int
	}{
		{
			name:     "short text fits in one line",
			text:     "hello",
			maxWidth: 10,
			expected: []string{"hello"},
		},
		{
			name:     "exact width",
			text:     "hello",
			maxWidth: 5,
			expected: []string{"hello"},
		},
		{
			name:     "wrap at space",
			text:     "hello world",
			maxWidth: 6,
			expected: []string{"hello", "world"},
		},
		{
			name:     "wrap long text",
			text:     "this is a longer text",
			maxWidth: 10,
			expected: []string{"this is a", "longer", "text"},
		},
		{
			name:     "no spaces - hard break",
			text:     "abcdefghij",
			maxWidth: 5,
			expected: []string{"abcde", "fghij"},
		},
		{
			name:     "empty text",
			text:     "",
			maxWidth: 10,
			expected: []string{""},
		},
		{
			name:     "zero width returns nil",
			text:     "hello",
			maxWidth: 0,
			expected: nil,
		},
		{
			name:     "negative width returns nil",
			text:     "hello",
			maxWidth: -5,
			expected: nil,
		},
		{
			name:     "multiple spaces",
			text:     "hello  world",
			maxWidth: 10,
			expected: []string{"hello ", "world"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := wrapText(tt.text, tt.maxWidth)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestScanningArea_NewScanningArea(t *testing.T) {
	t.Parallel()

	app := tview.NewApplication()
	sa := NewScanningArea(app)

	require.NotNil(t, sa)
	assert.NotNil(t, sa.Box)
	assert.Equal(t, ScanStateNoReader, sa.state)
	assert.Nil(t, sa.tokenInfo)
	assert.Equal(t, 0, sa.readerInfo.Count)
}

func TestScanningArea_SetReaderInfo(t *testing.T) {
	t.Parallel()

	app := tview.NewApplication()
	sa := NewScanningArea(app)

	// Initially no reader
	assert.Equal(t, ScanStateNoReader, sa.state)

	// Set one reader - should transition to waiting
	sa.SetReaderInfo(1, "libnfc")
	assert.Equal(t, ScanStateWaiting, sa.state)
	assert.Equal(t, 1, sa.readerInfo.Count)
	assert.Equal(t, "libnfc", sa.readerInfo.Driver)

	// Set two readers - should stay in waiting
	sa.SetReaderInfo(2, "acr122")
	assert.Equal(t, ScanStateWaiting, sa.state)
	assert.Equal(t, 2, sa.readerInfo.Count)

	// Set zero readers - should transition to no reader
	sa.SetReaderInfo(0, "")
	assert.Equal(t, ScanStateNoReader, sa.state)

	// Clean up animation if any
	sa.Stop()
}

func TestScanningArea_SetTokenInfo(t *testing.T) {
	t.Parallel()

	app := tview.NewApplication()
	sa := NewScanningArea(app)

	// Start with reader connected
	sa.SetReaderInfo(1, "libnfc")
	assert.Equal(t, ScanStateWaiting, sa.state)

	// Set token - should transition to scanned
	sa.SetTokenInfo("2025-01-01 12:00:00", "ABCD1234", "Super Mario Bros")
	assert.Equal(t, ScanStateScanned, sa.state)
	require.NotNil(t, sa.tokenInfo)
	assert.Equal(t, "2025-01-01 12:00:00", sa.tokenInfo.Time)
	assert.Equal(t, "ABCD1234", sa.tokenInfo.UID)
	assert.Equal(t, "Super Mario Bros", sa.tokenInfo.Value)

	// Clean up animation if any
	sa.Stop()
}

func TestScanningArea_ClearToken(t *testing.T) {
	t.Parallel()

	app := tview.NewApplication()
	sa := NewScanningArea(app)

	// Setup: reader connected, token scanned
	sa.SetReaderInfo(1, "libnfc")
	sa.SetTokenInfo("2025-01-01 12:00:00", "ABCD1234", "Test Game")
	assert.Equal(t, ScanStateScanned, sa.state)

	// Clear token - should go back to waiting (reader still connected)
	sa.ClearToken()
	assert.Equal(t, ScanStateWaiting, sa.state)
	assert.Nil(t, sa.tokenInfo)

	// Now disconnect reader
	sa.SetReaderInfo(0, "")
	assert.Equal(t, ScanStateNoReader, sa.state)

	// Clear token with no reader - should stay in no reader state
	sa.SetTokenInfo("2025-01-01 12:00:00", "ABCD1234", "Test")
	sa.SetReaderInfo(0, "")
	sa.ClearToken()
	assert.Equal(t, ScanStateNoReader, sa.state)

	// Clean up
	sa.Stop()
}

func TestScanningArea_StateTransitions(t *testing.T) {
	t.Parallel()

	app := tview.NewApplication()
	sa := NewScanningArea(app)

	// Initial state
	assert.Equal(t, ScanStateNoReader, sa.state)

	// NoReader -> Waiting (when reader connects)
	sa.SetState(ScanStateWaiting)
	assert.Equal(t, ScanStateWaiting, sa.state)

	// Waiting -> Scanned
	sa.SetState(ScanStateScanned)
	assert.Equal(t, ScanStateScanned, sa.state)

	// Scanned -> Waiting
	sa.SetState(ScanStateWaiting)
	assert.Equal(t, ScanStateWaiting, sa.state)

	// Waiting -> NoReader
	sa.SetState(ScanStateNoReader)
	assert.Equal(t, ScanStateNoReader, sa.state)

	// Clean up
	sa.Stop()
}
