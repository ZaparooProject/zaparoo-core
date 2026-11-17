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

package userdb

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContentRule_EmptyByDefault(t *testing.T) {
	t.Parallel()
	db := NewInMemoryUserDB(t)

	rule, err := db.GetContentRule("")
	require.NoError(t, err)
	assert.Empty(t, rule.Mode)
	assert.NotNil(t, rule.Entries)
	assert.Empty(t, rule.Entries)
}

func TestContentRule_ReplaceKeepsOrderAndIsPerProfile(t *testing.T) {
	t.Parallel()
	db := NewInMemoryUserDB(t)

	kid := &database.ContentRule{Mode: database.ContentModeAllowed, Entries: []database.ContentRuleEntry{
		{Effect: database.ContentEffectAllow, Kind: database.ContentKindSystem, SystemID: "NES"},
		{Effect: database.ContentEffectDeny, Kind: database.ContentKindMedia, SystemID: "NES", Path: "/games/a.nes"},
		{Effect: database.ContentEffectAllow, Kind: database.ContentKindDeck, Value: "kids"},
		{Effect: database.ContentEffectDeny, Kind: database.ContentKindTags, Value: "rating:adult"},
	}}
	require.NoError(t, db.ReplaceContentRule("kid-1", kid))
	require.NoError(t, db.ReplaceContentRule("", &database.ContentRule{Mode: database.ContentModeAll}))

	got, err := db.GetContentRule("kid-1")
	require.NoError(t, err)
	assert.Equal(t, *kid, got)

	shared, err := db.GetContentRule("")
	require.NoError(t, err)
	assert.Equal(t, database.ContentModeAll, shared.Mode)
	assert.Empty(t, shared.Entries)

	// A replace drops what was there instead of adding to it.
	require.NoError(t, db.ReplaceContentRule("kid-1", &database.ContentRule{
		Entries: []database.ContentRuleEntry{
			{Effect: database.ContentEffectAllow, Kind: database.ContentKindSystem, SystemID: "SNES"},
		},
	}))
	got, err = db.GetContentRule("kid-1")
	require.NoError(t, err)
	assert.Empty(t, got.Mode, "an empty mode clears the profile's own mode")
	require.Len(t, got.Entries, 1)
	assert.Equal(t, "SNES", got.Entries[0].SystemID)
}

func TestContentRule_StoresKindsThisBuildDoesNotKnow(t *testing.T) {
	t.Parallel()
	db := NewInMemoryUserDB(t)

	// Kind is free text so that a later kind needs no schema change.
	require.NoError(t, db.ReplaceContentRule("", &database.ContentRule{
		Entries: []database.ContentRuleEntry{{Effect: "allow", Kind: "rating", Value: "everyone"}},
	}))
	got, err := db.GetContentRule("")
	require.NoError(t, err)
	require.Len(t, got.Entries, 1)
	assert.Equal(t, "rating", got.Entries[0].Kind)
}

func TestContentRule_DeletedWithItsProfile(t *testing.T) {
	t.Parallel()
	db := NewInMemoryUserDB(t)
	require.NoError(t, db.CreateProfile(&database.Profile{
		ProfileID: "admin-1", Name: "Parent", Role: "admin", SwitchID: "a-b-c", PINHash: "x",
	}))
	require.NoError(t, db.CreateProfile(&database.Profile{
		ProfileID: "kid-1", Name: "Kid", Role: "member", SwitchID: "d-e-f",
	}))
	rule := &database.ContentRule{Mode: database.ContentModeAllowed, Entries: []database.ContentRuleEntry{
		{Effect: database.ContentEffectAllow, Kind: database.ContentKindSystem, SystemID: "NES"},
	}}
	require.NoError(t, db.ReplaceContentRule("kid-1", rule))
	require.NoError(t, db.ReplaceContentRule("", rule))

	require.NoError(t, db.DeleteProfile("kid-1"))

	gone, err := db.GetContentRule("kid-1")
	require.NoError(t, err)
	assert.Empty(t, gone.Mode)
	assert.Empty(t, gone.Entries)

	// A profile made later with the same ID must not inherit the old rule,
	// and the shared profile's rule is untouched.
	shared, err := db.GetContentRule("")
	require.NoError(t, err)
	assert.Equal(t, database.ContentModeAllowed, shared.Mode)
	assert.Len(t, shared.Entries, 1)
}
