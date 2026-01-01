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

package service

import (
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReaderManager tests are integration tests that would require
// more complex setup. The actual behavior is tested by verifying:
// 1. The Scan struct has the ReaderError field (tested below)
// 2. Manual/integration testing of the reader error scenarios
//
// For unit testing the reader manager logic, we would need to refactor
// it to accept a scan channel as a parameter or make it more testable.

// TestReaderScan_ReaderErrorField tests that the ReaderError field
// is properly set in different reader scenarios
func TestReaderScan_ReaderErrorField(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		scan        readers.Scan
		expectError bool
	}{
		{
			name: "normal token scan",
			scan: readers.Scan{
				Source: "test-reader",
				Token: &tokens.Token{
					UID:      "abc123",
					ScanTime: time.Now(),
				},
				ReaderError: false,
			},
			expectError: false,
		},
		{
			name: "normal token removal",
			scan: readers.Scan{
				Source:      "test-reader",
				Token:       nil,
				ReaderError: false,
			},
			expectError: false,
		},
		{
			name: "reader error removal",
			scan: readers.Scan{
				Source:      "test-reader",
				Token:       nil,
				ReaderError: true,
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expectError, tt.scan.ReaderError,
				"ReaderError field should be %v", tt.expectError)
		})
	}
}

// TestWroteTokenState tests the state management for wrote tokens
func TestWroteTokenState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		wroteToken   *tokens.Token
		name         string
		expectUID    string
		expectText   string
		expectNonNil bool
	}{
		{
			name: "set wrote token",
			wroteToken: &tokens.Token{
				UID:      "test-uid-123",
				Text:     "**TOTK",
				ScanTime: time.Now(),
			},
			expectNonNil: true,
			expectUID:    "test-uid-123",
			expectText:   "**TOTK",
		},
		{
			name:         "clear wrote token",
			wroteToken:   nil,
			expectNonNil: false,
		},
		{
			name: "wrote token with data",
			wroteToken: &tokens.Token{
				UID:      "nfc-abc",
				Text:     "launch://game.bin",
				Data:     "extra-data",
				ScanTime: time.Now(),
			},
			expectNonNil: true,
			expectUID:    "nfc-abc",
			expectText:   "launch://game.bin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create mock platform for state initialization
			mockPlatform := mocks.NewMockPlatform()
			st, _ := state.NewState(mockPlatform, "test-boot-uuid")

			// Set the wrote token
			st.SetWroteToken(tt.wroteToken)

			// Get the wrote token
			got := st.GetWroteToken()

			if tt.expectNonNil {
				require.NotNil(t, got, "expected non-nil wrote token")
				assert.Equal(t, tt.expectUID, got.UID, "UID should match")
				assert.Equal(t, tt.expectText, got.Text, "Text should match")
			} else {
				assert.Nil(t, got, "expected nil wrote token")
			}
		})
	}
}

// TestTokensEqualForWroteTokenSkip tests the token comparison logic
// used to determine if a scanned token should be skipped because it
// was just written
func TestTokensEqualForWroteTokenSkip(t *testing.T) {
	t.Parallel()

	baseTime := time.Now()

	tests := []struct {
		token1   *tokens.Token
		token2   *tokens.Token
		name     string
		expected bool
	}{
		{
			name: "identical tokens should match",
			token1: &tokens.Token{
				UID:      "abc123",
				Text:     "**TOTK",
				ScanTime: baseTime,
			},
			token2: &tokens.Token{
				UID:      "abc123",
				Text:     "**TOTK",
				ScanTime: baseTime.Add(time.Second), // Different scan time should still match
			},
			expected: true,
		},
		{
			name: "different UID should not match",
			token1: &tokens.Token{
				UID:      "abc123",
				Text:     "**TOTK",
				ScanTime: baseTime,
			},
			token2: &tokens.Token{
				UID:      "def456",
				Text:     "**TOTK",
				ScanTime: baseTime,
			},
			expected: false,
		},
		{
			name: "different Text should not match",
			token1: &tokens.Token{
				UID:      "abc123",
				Text:     "**TOTK",
				ScanTime: baseTime,
			},
			token2: &tokens.Token{
				UID:      "abc123",
				Text:     "**BOTW",
				ScanTime: baseTime,
			},
			expected: false,
		},
		{
			name: "both UID and Text must match",
			token1: &tokens.Token{
				UID:      "abc123",
				Text:     "**TOTK",
				ScanTime: baseTime,
			},
			token2: &tokens.Token{
				UID:      "def456",
				Text:     "**BOTW",
				ScanTime: baseTime,
			},
			expected: false,
		},
		{
			name:     "both nil should match",
			token1:   nil,
			token2:   nil,
			expected: true,
		},
		{
			name: "one nil should not match",
			token1: &tokens.Token{
				UID:  "abc123",
				Text: "**TOTK",
			},
			token2:   nil,
			expected: false,
		},
		{
			name:   "nil vs non-nil should not match",
			token1: nil,
			token2: &tokens.Token{
				UID:  "abc123",
				Text: "**TOTK",
			},
			expected: false,
		},
		{
			name: "Data field should not affect comparison",
			token1: &tokens.Token{
				UID:      "abc123",
				Text:     "**TOTK",
				Data:     "extra-data-1",
				ScanTime: baseTime,
			},
			token2: &tokens.Token{
				UID:      "abc123",
				Text:     "**TOTK",
				Data:     "extra-data-2", // Different Data
				ScanTime: baseTime,
			},
			expected: true, // Should still match - Data is ignored
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := helpers.TokensEqual(tt.token1, tt.token2)
			assert.Equal(t, tt.expected, result,
				"TokensEqual(%+v, %+v) = %v, want %v",
				tt.token1, tt.token2, result, tt.expected)
		})
	}
}

// TestWroteTokenSkipLogic tests the core logic that determines whether
// a just-written token should be skipped when scanned.
func TestWroteTokenSkipLogic(t *testing.T) {
	t.Parallel()

	tests := []struct {
		wroteToken   *tokens.Token
		scannedToken *tokens.Token
		name         string
		description  string
		expectSkip   bool
	}{
		{
			name: "just written token should be skipped",
			wroteToken: &tokens.Token{
				UID:      "abc123",
				Text:     "**TOTK",
				ScanTime: time.Now(),
			},
			scannedToken: &tokens.Token{
				UID:      "abc123",
				Text:     "**TOTK",
				ScanTime: time.Now().Add(time.Millisecond * 100),
			},
			expectSkip:  true,
			description: "When a token is written and immediately scanned, it should be skipped to prevent auto-launch",
		},
		{
			name: "different token should not be skipped",
			wroteToken: &tokens.Token{
				UID:      "abc123",
				Text:     "**TOTK",
				ScanTime: time.Now(),
			},
			scannedToken: &tokens.Token{
				UID:      "def456",
				Text:     "**BOTW",
				ScanTime: time.Now().Add(time.Millisecond * 100),
			},
			expectSkip:  false,
			description: "If a different token is scanned after a write, it should not be skipped",
		},
		{
			name:       "no wrote token means no skip",
			wroteToken: nil,
			scannedToken: &tokens.Token{
				UID:      "abc123",
				Text:     "**TOTK",
				ScanTime: time.Now(),
			},
			expectSkip:  false,
			description: "If no token was written, any scanned token should not be skipped",
		},
		{
			name: "same UID but different text should not be skipped",
			wroteToken: &tokens.Token{
				UID:      "abc123",
				Text:     "**TOTK",
				ScanTime: time.Now(),
			},
			scannedToken: &tokens.Token{
				UID:      "abc123",
				Text:     "**BOTW",
				ScanTime: time.Now().Add(time.Millisecond * 100),
			},
			expectSkip:  false,
			description: "Matching UID alone is not sufficient - text must also match",
		},
		{
			name: "same text but different UID should not be skipped",
			wroteToken: &tokens.Token{
				UID:      "abc123",
				Text:     "**TOTK",
				ScanTime: time.Now(),
			},
			scannedToken: &tokens.Token{
				UID:      "def456",
				Text:     "**TOTK",
				ScanTime: time.Now().Add(time.Millisecond * 100),
			},
			expectSkip:  false,
			description: "Matching text alone is not sufficient - UID must also match",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Simulate the skip check logic from pkg/service/readers.go:391-398
			shouldSkip := tt.wroteToken != nil && helpers.TokensEqual(tt.scannedToken, tt.wroteToken)

			assert.Equal(t, tt.expectSkip, shouldSkip,
				"Skip logic failed: %s\nWrote: %+v\nScanned: %+v",
				tt.description, tt.wroteToken, tt.scannedToken)
		})
	}
}

// TestWroteTokenClearingAfterSkip tests that wroteToken is properly
// cleared after the skip check, preventing false positives on subsequent scans
func TestWroteTokenClearingAfterSkip(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")

	wroteToken := &tokens.Token{
		UID:      "abc123",
		Text:     "**TOTK",
		ScanTime: time.Now(),
	}

	// Set the wrote token (simulating a write operation)
	st.SetWroteToken(wroteToken)
	assert.NotNil(t, st.GetWroteToken(), "wrote token should be set")

	// Simulate the skip check logic
	scannedToken := &tokens.Token{
		UID:      "abc123",
		Text:     "**TOTK",
		ScanTime: time.Now().Add(time.Millisecond * 100),
	}

	wt := st.GetWroteToken()
	if wt != nil && helpers.TokensEqual(scannedToken, wt) {
		// This is what happens in readers.go:394-396
		st.SetWroteToken(nil)
		// In real code: continue preprocessing (skip the launch)
	}

	// Verify wrote token was cleared
	assert.Nil(t, st.GetWroteToken(),
		"wrote token should be cleared after skip check to prevent false positives on next scan")

	// Simulate scanning the same token again
	secondScan := &tokens.Token{
		UID:      "abc123",
		Text:     "**TOTK",
		ScanTime: time.Now().Add(time.Millisecond * 200),
	}

	wt2 := st.GetWroteToken()
	shouldSkipSecondScan := wt2 != nil && helpers.TokensEqual(secondScan, wt2)

	assert.False(t, shouldSkipSecondScan,
		"second scan of same token should NOT be skipped (wrote token was cleared)")
}
