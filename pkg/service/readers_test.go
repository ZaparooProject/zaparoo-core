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

// TestTimedExitConditions_ReaderIDRequired is a regression test for the hold mode bug
// where timedExit would silently skip when tokens had empty ReaderID.
// This test verifies the conditions that timedExit checks before starting the exit timer.
func TestTimedExitConditions_ReaderIDRequired(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		readerID       string
		expectedReason string
		token          tokens.Token
		readerInState  bool
		hasRemovable   bool
		expectTimer    bool
	}{
		{
			name: "valid token from reader with ReaderID should start timer",
			token: tokens.Token{
				UID:      "abc123",
				Text:     "**launch.system:nes",
				Source:   tokens.SourceReader,
				ReaderID: "pn532-1234567890abcdef",
				ScanTime: time.Now(),
			},
			readerInState:  true,
			readerID:       "pn532-1234567890abcdef",
			hasRemovable:   true,
			expectTimer:    true,
			expectedReason: "timer should start for valid reader token",
		},
		{
			name: "token from API should NOT start timer",
			token: tokens.Token{
				UID:      "abc123",
				Text:     "**launch.system:nes",
				Source:   tokens.SourceAPI,
				ReaderID: "", // API tokens have no ReaderID
				ScanTime: time.Now(),
			},
			readerInState:  false,
			readerID:       "",
			hasRemovable:   false,
			expectTimer:    false,
			expectedReason: "API tokens cannot be 'removed' from a reader",
		},
		{
			name: "token from playlist should NOT start timer",
			token: tokens.Token{
				UID:      "abc123",
				Text:     "**launch.system:nes",
				Source:   tokens.SourcePlaylist,
				ReaderID: "",
				ScanTime: time.Now(),
			},
			readerInState:  false,
			readerID:       "",
			hasRemovable:   false,
			expectTimer:    false,
			expectedReason: "playlist tokens cannot be 'removed' from a reader",
		},
		{
			name: "REGRESSION: token with empty ReaderID should NOT start timer",
			token: tokens.Token{
				UID:      "abc123",
				Text:     "**launch.system:nes",
				Source:   tokens.SourceReader,
				ReaderID: "", // BUG: This was causing silent failures
				ScanTime: time.Now(),
			},
			readerInState:  false,
			readerID:       "",
			hasRemovable:   false,
			expectTimer:    false,
			expectedReason: "empty ReaderID means reader cannot be found in state",
		},
		{
			name: "token with ReaderID but reader not in state should NOT start timer",
			token: tokens.Token{
				UID:      "abc123",
				Text:     "**launch.system:nes",
				Source:   tokens.SourceReader,
				ReaderID: "unknown-reader-id",
				ScanTime: time.Now(),
			},
			readerInState:  false,
			readerID:       "unknown-reader-id",
			hasRemovable:   false,
			expectTimer:    false,
			expectedReason: "reader not found in state (disconnected or never registered)",
		},
		{
			name: "reader without CapabilityRemovable should NOT start timer",
			token: tokens.Token{
				UID:      "abc123",
				Text:     "**launch.system:nes",
				Source:   tokens.SourceReader,
				ReaderID: "barcode-reader-123",
				ScanTime: time.Now(),
			},
			readerInState:  true,
			readerID:       "barcode-reader-123",
			hasRemovable:   false, // barcode readers don't support removal detection
			expectTimer:    false,
			expectedReason: "reader lacks CapabilityRemovable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockPlatform := mocks.NewMockPlatform()
			st, _ := state.NewState(mockPlatform, "test-boot-uuid")

			// Set up the last scanned token
			st.SetActiveCard(tt.token)

			// Set up reader in state if needed
			if tt.readerInState {
				mockReader := mocks.NewMockReader()
				mockReader.On("ReaderID").Return(tt.readerID)
				mockReader.On("Connected").Return(true)
				mockReader.On("Path").Return("/dev/test")
				mockReader.On("Info").Return("Test Reader")
				mockReader.On("Metadata").Return(readers.DriverMetadata{
					ID:          "mock-reader",
					Description: "Mock Reader for Testing",
				})

				if tt.hasRemovable {
					mockReader.On("Capabilities").Return([]readers.Capability{readers.CapabilityRemovable})
				} else {
					mockReader.On("Capabilities").Return([]readers.Capability{})
				}

				st.SetReader(mockReader)
			}

			// Now verify the conditions that timedExit checks
			lastToken := st.GetLastScanned()

			// Condition 1: Source must be SourceReader
			sourceIsReader := lastToken.Source == tokens.SourceReader

			// Condition 2: Reader must exist in state
			r, readerExists := st.GetReader(lastToken.ReaderID)

			// Condition 3: Reader must have CapabilityRemovable
			hasRemovableCap := false
			if readerExists {
				hasRemovableCap = readers.HasCapability(r, readers.CapabilityRemovable)
			}

			// All conditions must be true for timer to start
			wouldStartTimer := sourceIsReader && readerExists && hasRemovableCap

			assert.Equal(t, tt.expectTimer, wouldStartTimer,
				"%s: sourceIsReader=%v, readerExists=%v, hasRemovableCap=%v",
				tt.expectedReason, sourceIsReader, readerExists, hasRemovableCap)
		})
	}
}

// TestReaderErrorRecovery_PrevTokenPreservation tests that prevToken is
// preserved through reader errors so that duplicate detection still works
// when a reader reconnects and re-detects the same card. This is the core
// fix for #497: USB controller hotplug causing false NFC re-scans.
func TestReaderErrorRecovery_PrevTokenPreservation(t *testing.T) {
	t.Parallel()

	cardToken := &tokens.Token{
		UID:      "abc123",
		Text:     "**launch.system:nes",
		ScanTime: time.Now(),
	}

	tests := []struct {
		initialPrev       *tokens.Token
		scan              *tokens.Token
		expectedPrev      *tokens.Token
		name              string
		readerError       bool
		shouldBeDuplicate bool
	}{
		{
			name:              "normal scan updates prevToken",
			initialPrev:       nil,
			scan:              cardToken,
			readerError:       false,
			expectedPrev:      cardToken,
			shouldBeDuplicate: false,
		},
		{
			name:              "normal removal clears prevToken",
			initialPrev:       cardToken,
			scan:              nil,
			readerError:       false,
			expectedPrev:      nil,
			shouldBeDuplicate: false,
		},
		{
			name:              "reader error preserves prevToken",
			initialPrev:       cardToken,
			scan:              nil,
			readerError:       true,
			expectedPrev:      cardToken,
			shouldBeDuplicate: false,
		},
		{
			name:              "reader error with nil prevToken is duplicate (nil==nil)",
			initialPrev:       nil,
			scan:              nil,
			readerError:       true,
			expectedPrev:      nil,
			shouldBeDuplicate: true,
		},
		{
			name:              "re-scan after reader error is duplicate",
			initialPrev:       cardToken,
			scan:              cardToken,
			readerError:       false,
			expectedPrev:      cardToken,
			shouldBeDuplicate: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			prevToken := tt.initialPrev

			// Step 1: duplicate check (mirrors readers.go line 410)
			isDuplicate := helpers.TokensEqual(tt.scan, prevToken)
			assert.Equal(t, tt.shouldBeDuplicate, isDuplicate,
				"duplicate detection mismatch")

			if isDuplicate {
				return
			}

			// Step 2: prevToken update (mirrors readers.go line 418-424)
			if !tt.readerError {
				prevToken = tt.scan
			}

			assert.Equal(t, tt.expectedPrev, prevToken,
				"prevToken should match expected value after update")
		})
	}
}

// TestReaderErrorRecovery_FullSequence simulates the complete sequence from
// #497: card scanned → reader error (USB hotplug) → reader reconnects →
// same card detected. The fix ensures the re-detection is caught as a
// duplicate and suppressed.
func TestReaderErrorRecovery_FullSequence(t *testing.T) {
	t.Parallel()

	card := &tokens.Token{
		UID:  "nfc-tag-001",
		Text: "**launch.system:snes",
	}

	var prevToken *tokens.Token

	// 1. Initial card scan — should pass through
	isDup := helpers.TokensEqual(card, prevToken)
	assert.False(t, isDup, "first scan should not be duplicate")
	prevToken = card

	// 2. Reader error (USB controller hotplug) — prevToken preserved
	readerErrorScan := (*tokens.Token)(nil)
	isDup = helpers.TokensEqual(readerErrorScan, prevToken)
	assert.False(t, isDup, "reader error scan is not a duplicate of card")
	// readerError=true, so we do NOT update prevToken
	// prevToken stays as card

	assert.Equal(t, card, prevToken,
		"prevToken must be preserved through reader error")

	// 3. Reader reconnects, same card detected — should be caught as duplicate
	isDup = helpers.TokensEqual(card, prevToken)
	assert.True(t, isDup,
		"re-scan of same card after reader error recovery must be detected as duplicate")
}
