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

package methods

import (
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleTokens_ReportsLaunchModifiers(t *testing.T) {
	t.Parallel()
	st, _ := state.NewState(mocks.NewMockPlatform(), "test-boot-uuid")
	t.Cleanup(st.StopService)

	resp, err := HandleTokens(requests.RequestEnv{State: st})
	require.NoError(t, err)
	tokensResp, ok := resp.(models.TokensResponse)
	require.True(t, ok)
	assert.NotNil(t, tokensResp.Modifiers)
	assert.Empty(t, tokensResp.Modifiers)

	card := tokens.Token{UID: "04aa", Text: "**launch?set_name=GBC", ReaderID: "r1", ScanTime: time.Now()}
	st.SetReaderToken("r1", &card, time.Now())
	st.ArmLaunchModifier(&state.LaunchModifier{
		Source:    card,
		Args:      map[string]string{"set_name": "GBC", "set_name_same_dir": "yes"},
		Sticky:    true,
		CreatedAt: time.Now(),
	})

	resp, err = HandleTokens(requests.RequestEnv{State: st})
	require.NoError(t, err)
	tokensResp, ok = resp.(models.TokensResponse)
	require.True(t, ok)
	require.Len(t, tokensResp.Modifiers, 1)
	got := tokensResp.Modifiers[0]
	assert.Equal(t, "r1", got.ReaderID)
	assert.Equal(t, "04aa", got.UID)
	assert.True(t, got.Sticky)
	assert.Equal(t, map[string]string{"set_name": "GBC", "set_name_same_dir": "yes"}, got.Args)
}

func tokensTestReader(id string, caps ...readers.Capability) *mocks.MockReader {
	r := mocks.NewMockReader()
	r.On("ReaderID").Return(id).Maybe()
	r.On("Capabilities").Return(caps).Maybe()
	r.On("Close").Return(nil).Maybe()
	r.On("Metadata").Return(readers.DriverMetadata{ID: "mock"}).Maybe()
	r.On("Path").Return("/dev/" + id).Maybe()
	return r
}

func activeUIDs(t *testing.T, st *state.State) []string {
	t.Helper()
	resp, err := HandleTokens(requests.RequestEnv{State: st})
	require.NoError(t, err)
	tokensResp, ok := resp.(models.TokensResponse)
	require.True(t, ok)
	uids := make([]string, 0, len(tokensResp.Active))
	for _, a := range tokensResp.Active {
		uids = append(uids, a.UID)
	}
	return uids
}

// Two readers each hold a card. Scanning the second card must not hide the
// first one, which is still on its reader.
func TestHandleTokens_ListsEveryCardOnAReader(t *testing.T) {
	t.Parallel()
	st, _ := state.NewState(mocks.NewMockPlatform(), "test-boot-uuid")
	t.Cleanup(st.StopService)
	st.SetReader(tokensTestReader("r1", readers.CapabilityRemovable))
	st.SetReader(tokensTestReader("r2", readers.CapabilityRemovable))

	now := time.Now()
	game := tokens.Token{UID: "game", Text: "SNES/game.sfc", ReaderID: "r2", ScanTime: now.Add(-time.Minute)}
	st.SetReaderToken("r2", &game, game.ScanTime)
	st.SetActiveCard(game)

	modifier := tokens.Token{UID: "modifier", Text: "**launch?launcher=X", ReaderID: "r1", ScanTime: now}
	st.SetReaderToken("r1", &modifier, now)
	st.SetActiveCard(modifier)

	assert.Equal(t, []string{"game", "modifier"}, activeUIDs(t, st))
	resp, err := HandleTokens(requests.RequestEnv{State: st})
	require.NoError(t, err)
	tokensResp, ok := resp.(models.TokensResponse)
	require.True(t, ok)
	require.Len(t, tokensResp.Active, 2)
	assert.Equal(t, "r2", tokensResp.Active[0].ReaderID)
	assert.Equal(t, "r1", tokensResp.Active[1].ReaderID)

	// Lifting the newer card clears the active card, but the older card is
	// still on its reader.
	st.ReleaseReaderToken("r1", time.Now(), false)
	st.SetActiveCard(tokens.Token{})
	assert.Equal(t, []string{"game"}, activeUIDs(t, st))

	st.ReleaseReaderToken("r2", time.Now(), false)
	assert.Empty(t, activeUIDs(t, st))
}

// A reader that cannot report removal never says its token left, so an old
// scan from it is not listed as resting on it.
func TestHandleTokens_SkipsReadersThatCannotReportRemoval(t *testing.T) {
	t.Parallel()
	st, _ := state.NewState(mocks.NewMockPlatform(), "test-boot-uuid")
	t.Cleanup(st.StopService)
	st.SetReader(tokensTestReader("barcode"))
	st.SetReader(tokensTestReader("r1", readers.CapabilityRemovable))

	old := tokens.Token{UID: "old-barcode", ReaderID: "barcode", ScanTime: time.Now().Add(-time.Hour)}
	st.SetReaderToken("barcode", &old, old.ScanTime)

	card := tokens.Token{UID: "card", ReaderID: "r1", ScanTime: time.Now()}
	st.SetReaderToken("r1", &card, card.ScanTime)
	st.SetActiveCard(card)

	assert.Equal(t, []string{"card"}, activeUIDs(t, st))
}

// A token run from the API rests on no reader. It is listed with the cards
// on readers, in scan order.
func TestHandleTokens_ListsLastScanFromNoReader(t *testing.T) {
	t.Parallel()
	st, _ := state.NewState(mocks.NewMockPlatform(), "test-boot-uuid")
	t.Cleanup(st.StopService)
	st.SetReader(tokensTestReader("r1", readers.CapabilityRemovable))

	card := tokens.Token{UID: "card", ReaderID: "r1", ScanTime: time.Now().Add(-time.Minute)}
	st.SetReaderToken("r1", &card, card.ScanTime)
	st.SetActiveCard(card)
	st.SetActiveCard(tokens.Token{Text: "SNES/game.sfc", ScanTime: time.Now()})

	assert.Equal(t, []string{"card", ""}, activeUIDs(t, st))
}
