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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

			const readerID = "reader-a"
			proc := &scanPreprocessor{}
			if tt.initialPrev != nil {
				proc.prevTokens = map[string]*tokens.Token{readerID: tt.initialPrev}
			}
			action := proc.Process(readerID, tt.scan, tt.readerError)

			assert.Equal(t, tt.wantAction, action, "unexpected action")
			assert.Equal(t, tt.wantPrevPost, proc.PrevToken(readerID),
				"unexpected tracked token after Process")
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

	const readerID = "reader-a"
	proc := &scanPreprocessor{}

	// 1. Initial card scan
	action := proc.Process(readerID, card, false)
	assert.Equal(t, scanNewToken, action, "first scan should be new token")
	assert.Equal(t, card, proc.PrevToken(readerID))

	// 2. Reader error (USB hotplug) — tracked token preserved
	action = proc.Process(readerID, nil, true)
	assert.Equal(t, scanReaderErrorRemoval, action, "reader error should be error removal")
	assert.Equal(t, card, proc.PrevToken(readerID), "tracked token must survive reader error")

	// 3. Reader reconnects, same card — duplicate
	action = proc.Process(readerID, card, false)
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

	const readerID = "reader-a"
	proc := &scanPreprocessor{}

	// 1. Scan card
	action := proc.Process(readerID, card, false)
	assert.Equal(t, scanNewToken, action)

	// 2. Normal removal
	action = proc.Process(readerID, nil, false)
	assert.Equal(t, scanNormalRemoval, action)
	assert.Nil(t, proc.PrevToken(readerID), "tracked token should be cleared")

	// 3. Same card again — should be new token (not duplicate)
	action = proc.Process(readerID, card, false)
	assert.Equal(t, scanNewToken, action)
}

func readerTokenUIDs(st *state.State) []string {
	listed := st.ReaderTokens()
	uids := make([]string, 0, len(listed))
	for i := range listed {
		uids = append(uids, listed[i].UID)
	}
	return uids
}

// A reader error ends state's record of the card but not the preprocessor's,
// so the same card read again is a duplicate that state must still list.
func TestProcessScan_CardReadAgainAfterReaderErrorIsListed(t *testing.T) {
	t.Parallel()
	st, _ := state.NewState(mocks.NewMockPlatform(), "test-boot-uuid")
	t.Cleanup(st.StopService)
	r := mocks.NewMockReader()
	r.On("ReaderID").Return("r1").Maybe()
	r.On("Capabilities").Return([]readers.Capability{readers.CapabilityRemovable}).Maybe()
	r.On("Metadata").Return(readers.DriverMetadata{ID: "mock"}).Maybe()
	r.On("Path").Return("/dev/r1").Maybe()
	st.SetReader(r)

	proc := &scanPreprocessor{}
	card := &tokens.Token{UID: "card", ReaderID: "r1", ScanTime: time.Now()}
	require.Equal(t, scanNewToken, processScan(st, proc, "r1", card, false))
	require.Equal(t, scanReaderErrorRemoval, processScan(st, proc, "r1", nil, true))
	assert.Empty(t, readerTokenUIDs(st))

	again := &tokens.Token{UID: "card", ReaderID: "r1", ScanTime: time.Now().Add(time.Second)}
	require.Equal(t, scanSkipDuplicate, processScan(st, proc, "r1", again, false))
	listed := st.ReaderTokens()
	require.Len(t, listed, 1)
	assert.Equal(t, card.ScanTime, listed[0].ScanTime, "the card was not read as new")

	require.Equal(t, scanSkipDuplicate, processScan(st, proc, "r1", again, false))
	assert.Equal(t, []string{"card"}, readerTokenUIDs(st))
}
