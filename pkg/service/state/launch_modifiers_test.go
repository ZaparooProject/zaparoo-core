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
	"encoding/json"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newModifierTestState(t *testing.T) (st *State, ns <-chan models.Notification) {
	t.Helper()
	st, ns = NewState(mocks.NewMockPlatform(), "test-boot-uuid")
	t.Cleanup(st.StopService)
	return st, ns
}

// heldModifier places a card on readerID and arms a sticky modifier from it.
func heldModifier(t *testing.T, st *State, readerID, uid string, args map[string]string) {
	t.Helper()
	card := tokens.Token{UID: uid, Text: "**launch?" + uid, ReaderID: readerID, ScanTime: time.Now()}
	st.SetReaderToken(readerID, &card, time.Now())
	require.True(t, st.ArmLaunchModifier(&LaunchModifier{
		Source: card, Args: args, Sticky: true, CreatedAt: time.Now(),
	}))
}

func modifierUIDs(mods []LaunchModifier) []string {
	uids := make([]string, 0, len(mods))
	for i := range mods {
		uids = append(uids, mods[i].Source.UID)
	}
	return uids
}

func lastModifiersNotification(t *testing.T, ns <-chan models.Notification) models.LaunchModifiersParams {
	t.Helper()
	var last *models.Notification
	for {
		select {
		case n := <-ns:
			if n.Method == models.NotificationTokensModifiers {
				last = &n
			}
			continue
		default:
		}
		break
	}
	require.NotNil(t, last, "no tokens.modifiers notification")
	var params models.LaunchModifiersParams
	require.NoError(t, json.Unmarshal(last.Params, &params))
	return params
}

func TestLaunchModifier_StickyNotConsumedAndClearedAfterUse(t *testing.T) {
	t.Parallel()
	st, ns := newModifierTestState(t)
	heldModifier(t, st, "r1", "gbc", map[string]string{"set_name": "GBC"})

	for range 3 {
		active, expired := st.TakeLaunchModifiers(time.Now())
		require.Len(t, active, 1)
		assert.Empty(t, expired)
	}
	require.Len(t, st.LaunchModifiers(), 1)

	st.ReleaseReaderToken("r1", time.Now(), false)
	assert.Empty(t, st.LaunchModifiers())
	assert.Empty(t, lastModifiersNotification(t, ns).Modifiers)
}

func TestLaunchModifier_StickyLiftedBeforeUseBecomesOneShot(t *testing.T) {
	t.Parallel()
	st, ns := newModifierTestState(t)
	heldModifier(t, st, "r1", "gbc", map[string]string{"set_name": "GBC"})

	st.ReleaseReaderToken("r1", time.Now(), false)
	mods := st.LaunchModifiers()
	require.Len(t, mods, 1)
	assert.False(t, mods[0].Sticky)
	sent := lastModifiersNotification(t, ns).Modifiers
	require.Len(t, sent, 1)
	assert.False(t, sent[0].Sticky)

	active, _ := st.TakeLaunchModifiers(time.Now())
	require.Len(t, active, 1)
	assert.Empty(t, st.LaunchModifiers())
}

func TestLaunchModifier_DegradedOneShotExpiresFromRemoval(t *testing.T) {
	t.Parallel()
	st, _ := newModifierTestState(t)
	heldModifier(t, st, "r1", "gbc", map[string]string{"set_name": "GBC"})
	lifted := time.Now()
	st.ReleaseReaderToken("r1", lifted, false)

	active, expired := st.TakeLaunchModifiers(lifted.Add(LaunchModifierTTL + time.Second))
	assert.Empty(t, active)
	require.Len(t, expired, 1)
	assert.Empty(t, st.LaunchModifiers())
}

func TestLaunchModifier_ReaderErrorEndsUnusedSticky(t *testing.T) {
	t.Parallel()
	st, _ := newModifierTestState(t)
	heldModifier(t, st, "r1", "gbc", map[string]string{"set_name": "GBC"})

	st.ReleaseReaderToken("r1", time.Now(), true)
	assert.Empty(t, st.LaunchModifiers())
}

func TestLaunchModifier_ReaderDisconnectEndsSticky(t *testing.T) {
	t.Parallel()
	st, _ := newModifierTestState(t)
	heldModifier(t, st, "r1", "gbc", map[string]string{"set_name": "GBC"})

	st.RemoveReader("r1")
	assert.Empty(t, st.LaunchModifiers())
}

func TestLaunchModifier_LiftingOneCardKeepsAnother(t *testing.T) {
	t.Parallel()
	st, _ := newModifierTestState(t)
	heldModifier(t, st, "r1", "gbc", map[string]string{"set_name": "GBC"})
	heldModifier(t, st, "r2", "details", map[string]string{"action": "details"})
	st.TakeLaunchModifiers(time.Now())

	st.ReleaseReaderToken("r1", time.Now(), false)
	assert.Equal(t, []string{"details"}, modifierUIDs(st.LaunchModifiers()))
}

func TestLaunchModifier_SwappedCardWithoutRemovalReleasesOld(t *testing.T) {
	t.Parallel()
	st, _ := newModifierTestState(t)
	heldModifier(t, st, "r1", "gbc", map[string]string{"set_name": "GBC"})
	st.TakeLaunchModifiers(time.Now())

	game := tokens.Token{UID: "game", Text: "**launch:game.gb", ReaderID: "r1", ScanTime: time.Now()}
	st.SetReaderToken("r1", &game, time.Now())
	assert.Empty(t, st.LaunchModifiers())
}

func TestLaunchModifier_SameReaderReplaces(t *testing.T) {
	t.Parallel()
	st, _ := newModifierTestState(t)
	heldModifier(t, st, "r1", "gbc", map[string]string{"set_name": "GBC"})
	heldModifier(t, st, "r1", "sgb", map[string]string{"set_name": "SGB"})

	assert.Equal(t, []string{"sgb"}, modifierUIDs(st.LaunchModifiers()))
}

func TestLaunchModifier_OneShotReplacesOneShotAndAppliesLast(t *testing.T) {
	t.Parallel()
	st, _ := newModifierTestState(t)
	st.ArmLaunchModifier(&LaunchModifier{
		Source: tokens.Token{UID: "first"}, Args: map[string]string{"launcher": "a"}, CreatedAt: time.Now(),
	})
	st.ArmLaunchModifier(&LaunchModifier{
		Source: tokens.Token{UID: "second"}, Args: map[string]string{"launcher": "b"}, CreatedAt: time.Now(),
	})
	heldModifier(t, st, "r1", "held", map[string]string{"launcher": "c"})
	assert.Equal(t, []string{"second", "held"}, modifierUIDs(st.LaunchModifiers()))

	active, _ := st.TakeLaunchModifiers(time.Now())
	assert.Equal(t, []string{"held", "second"}, modifierUIDs(active))
	assert.Equal(t, []string{"held"}, modifierUIDs(st.LaunchModifiers()))
}

func TestLaunchModifier_StickyRequestForLiftedCardArmsOneShot(t *testing.T) {
	t.Parallel()
	st, _ := newModifierTestState(t)
	card := tokens.Token{UID: "gbc", Text: "**launch?set_name=GBC", ReaderID: "r1", ScanTime: time.Now()}

	assert.False(t, st.ArmLaunchModifier(&LaunchModifier{
		Source: card, Args: map[string]string{"set_name": "GBC"}, Sticky: true, CreatedAt: time.Now(),
	}))
}

func TestLaunchModifier_ArmCopiesArgs(t *testing.T) {
	t.Parallel()
	st, _ := newModifierTestState(t)
	args := map[string]string{"launcher": "a"}
	st.ArmLaunchModifier(&LaunchModifier{Source: tokens.Token{UID: "x"}, Args: args, CreatedAt: time.Now()})
	args["launcher"] = "mutated"

	mods := st.LaunchModifiers()
	require.Len(t, mods, 1)
	assert.Equal(t, "a", mods[0].Args["launcher"])
}
