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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/stretchr/testify/assert"
)

func TestScanPreprocessor_Process(t *testing.T) {
	t.Parallel()

	cardA := &tokens.Token{
		UID:      "abc123",
		Text:     "**launch.system:nes",
		ScanTime: time.Now(),
	}
	cardB := &tokens.Token{
		UID:      "def456",
		Text:     "**launch.system:snes",
		ScanTime: time.Now(),
	}

	tests := []struct {
		initialPrev  *tokens.Token
		scan         *tokens.Token
		wantPrevPost *tokens.Token
		name         string
		wantAction   scanAction
		readerError  bool
	}{
		{
			name:         "first scan of a token",
			initialPrev:  nil,
			scan:         cardA,
			readerError:  false,
			wantAction:   scanNewToken,
			wantPrevPost: cardA,
		},
		{
			name:         "duplicate scan",
			initialPrev:  cardA,
			scan:         cardA,
			readerError:  false,
			wantAction:   scanSkipDuplicate,
			wantPrevPost: cardA,
		},
		{
			name:         "different token replaces prevToken",
			initialPrev:  cardA,
			scan:         cardB,
			readerError:  false,
			wantAction:   scanNewToken,
			wantPrevPost: cardB,
		},
		{
			name:         "normal removal clears prevToken",
			initialPrev:  cardA,
			scan:         nil,
			readerError:  false,
			wantAction:   scanNormalRemoval,
			wantPrevPost: nil,
		},
		{
			name:         "reader error preserves prevToken",
			initialPrev:  cardA,
			scan:         nil,
			readerError:  true,
			wantAction:   scanReaderErrorRemoval,
			wantPrevPost: cardA,
		},
		{
			name:         "reader error with nil prevToken is duplicate",
			initialPrev:  nil,
			scan:         nil,
			readerError:  true,
			wantAction:   scanSkipDuplicate,
			wantPrevPost: nil,
		},
		{
			name:         "nil removal when prevToken already nil is duplicate",
			initialPrev:  nil,
			scan:         nil,
			readerError:  false,
			wantAction:   scanSkipDuplicate,
			wantPrevPost: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			proc := &scanPreprocessor{prevToken: tt.initialPrev}
			action := proc.Process(tt.scan, tt.readerError)

			assert.Equal(t, tt.wantAction, action, "unexpected action")
			assert.Equal(t, tt.wantPrevPost, proc.PrevToken(), "unexpected prevToken after Process")
		})
	}
}

// TestScanPreprocessor_ReaderErrorSequence is the #497 regression test
// expressed as a sequence of scanPreprocessor calls:
// card scanned -> reader error -> reader reconnects -> same card detected.
func TestScanPreprocessor_ReaderErrorSequence(t *testing.T) {
	t.Parallel()

	card := &tokens.Token{
		UID:  "nfc-tag-001",
		Text: "**launch.system:snes",
	}

	proc := &scanPreprocessor{}

	// 1. Initial card scan
	action := proc.Process(card, false)
	assert.Equal(t, scanNewToken, action, "first scan should be new token")
	assert.Equal(t, card, proc.PrevToken())

	// 2. Reader error (USB hotplug) — prevToken preserved
	action = proc.Process(nil, true)
	assert.Equal(t, scanReaderErrorRemoval, action, "reader error should be error removal")
	assert.Equal(t, card, proc.PrevToken(), "prevToken must survive reader error")

	// 3. Reader reconnects, same card — duplicate
	action = proc.Process(card, false)
	assert.Equal(t, scanSkipDuplicate, action, "re-scan after error must be duplicate")
}

// TestScanPreprocessor_NormalRemovalSequence verifies that normal removal
// clears prevToken so the same card can be re-scanned.
func TestScanPreprocessor_NormalRemovalSequence(t *testing.T) {
	t.Parallel()

	card := &tokens.Token{
		UID:  "nfc-tag-001",
		Text: "**launch.system:nes",
	}

	proc := &scanPreprocessor{}

	// 1. Scan card
	action := proc.Process(card, false)
	assert.Equal(t, scanNewToken, action)

	// 2. Normal removal
	action = proc.Process(nil, false)
	assert.Equal(t, scanNormalRemoval, action)
	assert.Nil(t, proc.PrevToken(), "prevToken should be cleared")

	// 3. Same card again — should be new token (not duplicate)
	action = proc.Process(card, false)
	assert.Equal(t, scanNewToken, action)
}
