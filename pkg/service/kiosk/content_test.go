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

package kiosk_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/kiosk"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const (
	marioPath = "/media/fat/games/NES/Mario.nes"
	sonicPath = "/media/fat/games/Genesis/Sonic.md"
)

func setRule(t *testing.T, db *database.Database, profileID, mode string, entries ...database.ContentRuleEntry) {
	t.Helper()
	require.NoError(t, db.UserDB.ReplaceContentRule(profileID, &database.ContentRule{Mode: mode, Entries: entries}))
}

func allowSystem(id string) database.ContentRuleEntry {
	return database.ContentRuleEntry{
		Effect: database.ContentEffectAllow, Kind: database.ContentKindSystem, SystemID: id,
	}
}

func denySystem(id string) database.ContentRuleEntry {
	return database.ContentRuleEntry{Effect: database.ContentEffectDeny, Kind: database.ContentKindSystem, SystemID: id}
}

func media(effect, systemID, path string) database.ContentRuleEntry {
	return database.ContentRuleEntry{Effect: effect, Kind: database.ContentKindMedia, SystemID: systemID, Path: path}
}

func check(lock *kiosk.Service, profileID, systemID, path string) error {
	//nolint:wrapcheck // the tests assert on the error exactly as the lock returns it
	return lock.CheckLaunch(context.Background(), profileID, systemID, path, true)
}

func requireRefused(t *testing.T, err error, reason string) {
	t.Helper()
	require.ErrorIs(t, err, kiosk.ErrLaunchRefused)
	var refused *kiosk.RefusedError
	require.ErrorAs(t, err, &refused)
	assert.Equal(t, reason, refused.Reason)
}

func TestCheckLaunchOnlyAppliesWhileLocked(t *testing.T) {
	t.Parallel()
	lock, db, _ := newLock(t)
	setRule(t, db, "", database.ContentModeAllowed)

	require.NoError(t, check(lock, "", "NES", marioPath), "kiosk off")

	require.NoError(t, lock.Enable(preset(t, kiosk.PresetFamily), kiosk.EnableOptions{ConfirmEmpty: true}))
	requireRefused(t, check(lock, "", "NES", marioPath), kiosk.ReasonNotAllowed)

	require.NoError(t, lock.Unlock(time.Minute))
	require.NoError(t, check(lock, "", "NES", marioPath), "temporarily unlocked")
}

func TestAllowedOnlyRule(t *testing.T) {
	t.Parallel()
	lock, db, _ := newLock(t)
	setRule(t, db, kidProfileID, database.ContentModeAllowed,
		allowSystem("NES"),
		media(database.ContentEffectAllow, "Genesis", sonicPath),
	)
	require.NoError(t, lock.Enable(preset(t, kiosk.PresetFamily), kiosk.EnableOptions{ConfirmEmpty: true}))

	require.NoError(t, check(lock, kidProfileID, "NES", marioPath), "whole system allowed")
	require.NoError(t, check(lock, kidProfileID, "nes", marioPath), "system IDs match in any case")
	require.NoError(t, check(lock, kidProfileID, "Genesis", sonicPath), "single game allowed")
	require.NoError(t, check(lock, kidProfileID, "NES", ""), "launching the allowed system itself")

	requireRefused(t, check(lock, kidProfileID, "Genesis", "/media/fat/games/Genesis/Other.md"), kiosk.ReasonNotAllowed)
	requireRefused(t, check(lock, kidProfileID, "SNES", "/media/fat/games/SNES/Zelda.sfc"), kiosk.ReasonNotAllowed)
	requireRefused(t, check(lock, kidProfileID, "Genesis", ""), kiosk.ReasonNotAllowed)
	requireRefused(t, check(lock, kidProfileID, "", "/tmp/unknown.bin"), kiosk.ReasonNotAllowed)

	// The rule is the profile's own: the shared profile has none and falls
	// to the Family default, which allows nothing.
	requireRefused(t, check(lock, "", "NES", marioPath), kiosk.ReasonNotAllowed)
}

func TestEverythingExceptDeniedRule(t *testing.T) {
	t.Parallel()
	lock, db, _ := newLock(t)
	setRule(t, db, "", database.ContentModeAll,
		denySystem("Genesis"),
		media(database.ContentEffectDeny, "NES", marioPath),
	)
	enableOpen(t, lock)

	require.NoError(t, check(lock, "", "SNES", "/media/fat/games/SNES/Zelda.sfc"))
	require.NoError(t, check(lock, "", "NES", "/media/fat/games/NES/Other.nes"))
	require.NoError(t, check(lock, "", "", "/tmp/unknown.bin"), "nothing denies an unmatched path")

	requireRefused(t, check(lock, "", "Genesis", sonicPath), kiosk.ReasonDenied)
	requireRefused(t, check(lock, "", "Genesis", ""), kiosk.ReasonDenied)
	requireRefused(t, check(lock, "", "NES", marioPath), kiosk.ReasonDenied)
}

func TestDenyBeatsAllow(t *testing.T) {
	t.Parallel()
	lock, db, _ := newLock(t)
	setRule(t, db, "", database.ContentModeAllowed,
		allowSystem("NES"),
		media(database.ContentEffectDeny, "NES", marioPath),
	)
	require.NoError(t, lock.Enable(preset(t, kiosk.PresetFamily), kiosk.EnableOptions{ConfirmEmpty: true}))

	requireRefused(t, check(lock, "", "NES", marioPath), kiosk.ReasonDenied)
	require.NoError(t, check(lock, "", "NES", "/media/fat/games/NES/Other.nes"))
}

func TestHiddenMediaIsRefused(t *testing.T) {
	t.Parallel()
	lock, db, _ := newLock(t)
	setRule(t, db, "", database.ContentModeAllowed, allowSystem("NES"))
	require.NoError(t, db.UserDB.UpsertMediaUserData(&database.MediaUserData{
		SystemID: "NES", Path: marioPath, IsHidden: true,
	}))
	require.NoError(t, lock.Enable(preset(t, kiosk.PresetFamily), kiosk.EnableOptions{ConfirmEmpty: true}))

	requireRefused(t, check(lock, "", "NES", marioPath), kiosk.ReasonHidden)
	require.NoError(t, check(lock, "", "NES", "/media/fat/games/NES/Other.nes"))
}

func TestDeckEntry(t *testing.T) {
	t.Parallel()
	lock, db, _ := newLock(t)
	require.NoError(t, db.UserDB.CreateDeck(&database.Deck{
		DeckID: "kids-deck", Name: "Kids",
		Items: []database.DeckItem{{
			ZapScript: "**launch:" + marioPath,
			Anchor:    database.DeckItemAnchor{SystemID: "NES", Path: marioPath},
		}},
	}))
	setRule(t, db, "", database.ContentModeAllowed, database.ContentRuleEntry{
		Effect: database.ContentEffectAllow, Kind: database.ContentKindDeck, Value: "kids-deck",
	})
	require.NoError(t, lock.Enable(preset(t, kiosk.PresetFamily), kiosk.EnableOptions{ConfirmEmpty: true}))

	require.NoError(t, check(lock, "", "NES", marioPath))
	requireRefused(t, check(lock, "", "NES", "/media/fat/games/NES/Other.nes"), kiosk.ReasonNotAllowed)
	requireRefused(t, check(lock, "", "NES", ""), kiosk.ReasonNotAllowed)
}

func tagRule(effect, value string) database.ContentRuleEntry {
	return database.ContentRuleEntry{Effect: effect, Kind: database.ContentKindTags, Value: value}
}

func TestTagEntries(t *testing.T) {
	t.Parallel()
	lock, db, _ := newLock(t)
	mediaDB := helpers.NewMockMediaDBI()
	db.MediaDB = mediaDB
	mediaDB.On("GetZapScriptTagsBySystemAndPath", mock.Anything, "NES", marioPath).
		Return([]database.TagInfo{{Type: "rating", Tag: "everyone"}, {Type: "genre", Tag: "platform"}}, nil)
	mediaDB.On("GetZapScriptTagsBySystemAndPath", mock.Anything, "Genesis", sonicPath).
		Return([]database.TagInfo{{Type: "rating", Tag: "adult"}}, nil)
	mediaDB.On("GetZapScriptTagsBySystemAndPath", mock.Anything, "SNES", mock.Anything).
		Return([]database.TagInfo{}, nil)

	setRule(t, db, "", database.ContentModeAll, tagRule(database.ContentEffectDeny, "rating:adult"))
	setRule(t, db, kidProfileID, database.ContentModeAllowed, tagRule(database.ContentEffectAllow, "rating:everyone"))
	enableOpen(t, lock)

	// Deny by tag under an open rule.
	require.NoError(t, check(lock, "", "NES", marioPath))
	requireRefused(t, check(lock, "", "Genesis", sonicPath), kiosk.ReasonDenied)

	// Allow by tag under an allowed-only rule.
	require.NoError(t, check(lock, kidProfileID, "NES", marioPath))
	requireRefused(t, check(lock, kidProfileID, "Genesis", sonicPath), kiosk.ReasonNotAllowed)

	// Media the index knows nothing about matches no tag entry, so an
	// allowed-only rule refuses it: this is what a reindex looks like.
	requireRefused(t, check(lock, kidProfileID, "SNES", "/media/fat/games/SNES/Zelda.sfc"), kiosk.ReasonNotAllowed)
}

func TestTagExclusionNeverOpensUnindexedMedia(t *testing.T) {
	t.Parallel()
	lock, db, _ := newLock(t)
	mediaDB := helpers.NewMockMediaDBI()
	db.MediaDB = mediaDB
	mediaDB.On("GetZapScriptTagsBySystemAndPath", mock.Anything, mock.Anything, mock.Anything).
		Return([]database.TagInfo{}, nil)

	// "Anything not tagged adult" must not read as "anything" when there
	// are no tags to look at.
	setRule(t, db, "", database.ContentModeAllowed, tagRule(database.ContentEffectAllow, "-rating:adult"))
	require.NoError(t, lock.Enable(preset(t, kiosk.PresetFamily), kiosk.EnableOptions{ConfirmEmpty: true}))

	requireRefused(t, check(lock, "", "NES", marioPath), kiosk.ReasonNotAllowed)
}

func TestTagEntryWithoutMediaDB(t *testing.T) {
	t.Parallel()
	lock, db, _ := newLock(t)
	setRule(t, db, "", database.ContentModeAllowed, tagRule(database.ContentEffectAllow, "rating:everyone"))
	require.NoError(t, lock.Enable(preset(t, kiosk.PresetFamily), kiosk.EnableOptions{ConfirmEmpty: true}))
	requireRefused(t, check(lock, "", "NES", marioPath), kiosk.ReasonNotAllowed)
}

func TestUnknownEntryKindMatchesNothing(t *testing.T) {
	t.Parallel()
	lock, db, _ := newLock(t)
	setRule(t, db, "", database.ContentModeAllowed, database.ContentRuleEntry{
		Effect: database.ContentEffectAllow, Kind: "rating", Value: "everyone",
	})
	require.NoError(t, lock.Enable(preset(t, kiosk.PresetFamily), kiosk.EnableOptions{ConfirmEmpty: true}))
	requireRefused(t, check(lock, "", "NES", marioPath), kiosk.ReasonNotAllowed)
}

func TestProfileModeOverridesDeviceDefault(t *testing.T) {
	t.Parallel()
	lock, db, _ := newLock(t)
	setRule(t, db, kidProfileID, database.ContentModeAll)
	require.NoError(t, lock.Enable(preset(t, kiosk.PresetFamily), kiosk.EnableOptions{ConfirmEmpty: true}))

	require.NoError(t, check(lock, kidProfileID, "NES", marioPath), "profile's own open mode")
	requireRefused(t, check(lock, "", "NES", marioPath), kiosk.ReasonNotAllowed)
}

func TestAllowOnce(t *testing.T) {
	t.Parallel()
	lock, db, clock := newLock(t)
	setRule(t, db, "", database.ContentModeAllowed)
	require.NoError(t, lock.Enable(preset(t, kiosk.PresetFamily), kiosk.EnableOptions{ConfirmEmpty: true}))
	ctx := context.Background()

	lock.AllowOnce("NES", marioPath)
	// The check a confirmation makes first does not spend the approval.
	require.NoError(t, lock.CheckLaunch(ctx, "", "NES", marioPath, false))
	require.NoError(t, lock.CheckLaunch(ctx, "", "nes", marioPath, true))
	requireRefused(t, lock.CheckLaunch(ctx, "", "NES", marioPath, true), kiosk.ReasonNotAllowed)

	// It covers one path, not its system.
	lock.AllowOnce("NES", marioPath)
	requireRefused(t, check(lock, "", "NES", "/media/fat/games/NES/Other.nes"), kiosk.ReasonNotAllowed)

	// And it does not wait forever.
	clock.Advance(6 * time.Minute)
	requireRefused(t, check(lock, "", "NES", marioPath), kiosk.ReasonNotAllowed)
}

func TestSystemScope(t *testing.T) {
	t.Parallel()
	lock, db, _ := newLock(t)
	setRule(t, db, "", database.ContentModeAll, denySystem("Genesis"))
	setRule(t, db, kidProfileID, database.ContentModeAllowed,
		allowSystem("NES"), media(database.ContentEffectAllow, "SNES", "/x.sfc"), denySystem("SNES"))

	unlocked := lock.SystemScope(kidProfileID)
	assert.False(t, unlocked.Limited(), "no scope while kiosk is off")
	assert.True(t, unlocked.Permits("Genesis"))

	enableOpen(t, lock)

	shared := lock.SystemScope("")
	assert.True(t, shared.Limited())
	assert.True(t, shared.Permits("NES"))
	assert.False(t, shared.Permits("genesis"))

	kid := lock.SystemScope(kidProfileID)
	assert.True(t, kid.Permits("NES"))
	assert.False(t, kid.Permits("SNES"), "a denied system stays closed even with a game allowed in it")
	assert.False(t, kid.Permits("Genesis"))
}

func TestSystemScopeCannotNarrowDeckOrTagRules(t *testing.T) {
	t.Parallel()
	lock, db, _ := newLock(t)
	setRule(t, db, "", database.ContentModeAllowed, allowSystem("NES"),
		database.ContentRuleEntry{Effect: database.ContentEffectAllow, Kind: database.ContentKindDeck, Value: "d"})
	enableOpen(t, lock)

	// A deck reaches into any system, so the scope leaves every system
	// open and the launch check does the deciding.
	scope := lock.SystemScope("")
	assert.True(t, scope.Permits("Genesis"))
	requireRefused(t, check(lock, "", "Genesis", sonicPath), kiosk.ReasonNotAllowed)
}

func TestSystemScopeWithNothingAllowedExposesNothing(t *testing.T) {
	t.Parallel()
	lock, _, _ := newLock(t)
	require.NoError(t, lock.Enable(preset(t, kiosk.PresetFamily), kiosk.EnableOptions{ConfirmEmpty: true}))
	scope := lock.SystemScope("")
	assert.True(t, scope.Limited())
	assert.False(t, scope.Permits("NES"))
}

func TestDenyThatCannotBeLookedUpRefuses(t *testing.T) {
	t.Parallel()
	lock, db, _ := newLock(t)
	mediaDB := helpers.NewMockMediaDBI()
	db.MediaDB = mediaDB
	mediaDB.On("GetZapScriptTagsBySystemAndPath", mock.Anything, mock.Anything, mock.Anything).
		Return(nil, errors.New("database is locked"))

	// An open rule with one deny by tag. The index cannot be read, so
	// nothing says the game is not the one being denied.
	setRule(t, db, "", database.ContentModeAll, tagRule(database.ContentEffectDeny, "rating:adult"))
	enableOpen(t, lock)
	requireRefused(t, check(lock, "", "NES", marioPath), kiosk.ReasonDenied)

	// A launch of the system itself has no media to look up and is not
	// what a tag entry is about.
	require.NoError(t, check(lock, "", "NES", ""))
}

func TestDenyByTagWithNoIndexAtAllRefuses(t *testing.T) {
	t.Parallel()
	lock, db, _ := newLock(t)
	setRule(t, db, "", database.ContentModeAll, tagRule(database.ContentEffectDeny, "rating:adult"))
	enableOpen(t, lock)
	requireRefused(t, check(lock, "", "NES", marioPath), kiosk.ReasonDenied)
}

func TestAllowThatCannotBeLookedUpAllowsNothing(t *testing.T) {
	t.Parallel()
	lock, db, _ := newLock(t)
	mediaDB := helpers.NewMockMediaDBI()
	db.MediaDB = mediaDB
	mediaDB.On("GetZapScriptTagsBySystemAndPath", mock.Anything, mock.Anything, mock.Anything).
		Return(nil, errors.New("database is locked"))

	setRule(t, db, "", database.ContentModeAllowed,
		tagRule(database.ContentEffectAllow, "rating:everyone"), allowSystem("SNES"))
	require.NoError(t, lock.Enable(preset(t, kiosk.PresetFamily), kiosk.EnableOptions{ConfirmEmpty: true}))

	requireRefused(t, check(lock, "", "NES", marioPath), kiosk.ReasonNotAllowed)
	// An entry that can be evaluated still works beside one that cannot.
	require.NoError(t, check(lock, "", "SNES", "/media/fat/games/SNES/Zelda.sfc"))
}
