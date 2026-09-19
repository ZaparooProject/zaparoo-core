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

package state

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLibrarySyncSignals(t *testing.T) {
	t.Parallel()

	var nilState *State
	nilState.RequestLibrarySync()
	nilState.NotifyLibraryStateChanged()

	st, notifications := NewState(nil, "")
	t.Cleanup(func() {
		for len(notifications) > 0 {
			<-notifications
		}
	})
	st.RequestLibrarySync()
	st.NotifyLibraryStateChanged()

	st.NotifyLibraryDecksChanged()
	st.NotifyLibraryDecksAccessed()
	st.RefreshLibraryDeck(context.Background(), "0123456789ab")
	st.NotifyLibraryHint([]string{"state"}, 7)
	st.SetLibraryPipeState(true)

	settings, edits, deckEdits, accesses := 0, 0, 0, 0
	refreshed := ""
	var hintKinds []string
	var hintRevision int64
	pipeHeld := false
	st.SetLibrarySyncSignals(LibrarySyncSignals{
		SettingChanged: func() { settings++ },
		StateChanged:   func() { edits++ },
		DecksChanged:   func() { deckEdits++ },
		DecksAccessed:  func() { accesses++ },
		RefreshDeck:    func(_ context.Context, deckID string) { refreshed = deckID },
		Hint:           func(kinds []string, revision int64) { hintKinds, hintRevision = kinds, revision },
		PipeState:      func(connected bool) { pipeHeld = connected },
	})
	st.RequestLibrarySync()
	st.NotifyLibraryStateChanged()
	st.NotifyLibraryStateChanged()
	st.NotifyLibraryDecksChanged()
	st.NotifyLibraryDecksAccessed()
	st.RefreshLibraryDeck(context.Background(), "0123456789ab")
	assert.Equal(t, 1, settings)
	assert.Equal(t, 2, edits)
	assert.Equal(t, 1, deckEdits)
	assert.Equal(t, 1, accesses)
	assert.Equal(t, "0123456789ab", refreshed)

	st.NotifyLibraryHint([]string{"decks", "state"}, 42)
	st.SetLibraryPipeState(true)
	assert.Equal(t, []string{"decks", "state"}, hintKinds)
	assert.Equal(t, int64(42), hintRevision)
	assert.True(t, pipeHeld)
	st.SetLibraryPipeState(false)
	assert.False(t, pipeHeld)
}
