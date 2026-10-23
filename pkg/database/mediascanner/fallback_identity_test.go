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

package mediascanner

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A file played before it was indexed must report the same observation the
// index gives it later, or the server would hold two entities for one game.
func TestFallbackMediaIdentity_MatchesIndexedIdentity(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	root := filepath.Join(string(filepath.Separator), "tmp", "operator")
	paths := []string{
		filepath.Join(root, "SECRET OF MANA.sfc"),
		filepath.Join(root, "MEGAMAN X.sfc"),
		filepath.Join(root, "Super Metroid (Japan, USA) (En,Ja) (Rev 1).sfc"),
		filepath.Join(root, "The Legend of Zelda - A Link to the Past (USA).sfc"),
	}
	indexMediaPaths(t, mediaDB, "SNES", paths...)

	for _, path := range paths {
		indexed, found, err := database.LookupMediaIdentity(context.Background(), mediaDB, "SNES", path)
		require.NoError(t, err)
		require.True(t, found, path)

		fallback, ok := FallbackMediaIdentity(nil, "SNES", path)
		require.True(t, ok, path)
		assert.Equal(t, indexed, fallback, path)
	}
}

func TestFallbackMediaIdentity_UsesCanonicalSystemID(t *testing.T) {
	t.Parallel()

	path := filepath.Join(string(filepath.Separator), "tmp", "operator", "SECRET OF MANA.sfc")
	identity, ok := FallbackMediaIdentity(nil, "snes", path)
	require.True(t, ok)
	assert.Equal(t, "SNES", identity.CanonicalSystemID)
	assert.Equal(t, "SECRET OF MANA", identity.DisplayName)
	assert.NotEmpty(t, identity.CoreSlug)
	assert.NotEmpty(t, identity.ObservationFingerprint)
}

func TestFallbackMediaIdentity_RejectsUnbuildableObservations(t *testing.T) {
	t.Parallel()

	abs := filepath.Join(string(filepath.Separator), "tmp", "operator", "SECRET OF MANA.sfc")
	tests := []struct {
		name     string
		systemID string
		path     string
	}{
		{name: "empty path", systemID: "SNES", path: ""},
		{name: "relative path", systemID: "SNES", path: filepath.Join("operator", "SECRET OF MANA.sfc")},
		{name: "arcade set name", systemID: "Arcade", path: "dkong"},
		{name: "virtual path", systemID: "PC", path: "steam://rungameid/123"},
		{name: "unknown system", systemID: "NotASystem", path: abs},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, ok := FallbackMediaIdentity(nil, tt.systemID, tt.path)
			assert.False(t, ok)
		})
	}
}
