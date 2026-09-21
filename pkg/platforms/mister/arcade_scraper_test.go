//go:build linux

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

package mister

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/arcadedb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestArcadeSystemIDsCoversEveryClassifiedSystem(t *testing.T) {
	t.Parallel()
	ids := ArcadeSystemIDs()

	require.NotEmpty(t, ids)
	assert.Equal(t, systemdefs.SystemArcade, ids[0], "the walked system comes first")
	assert.Len(t, ids, len(misterArcadeSystemSpecs)+1)
	for _, spec := range misterArcadeSystemSpecs {
		assert.Contains(t, ids, spec.systemID)
	}
}

func TestArcadeCatalogEntriesCarryEveryReadColumn(t *testing.T) {
	t.Parallel()
	entries := arcadeCatalogEntries([]arcadedb.ArcadeDbEntry{{
		Setname: "1941", Name: "1941- Counter Attack (W)", Region: "World", Version: "900227",
		Alternative: "yes", ParentTitle: "1941- Counter Attack", Platform: "Capcom CPS-1",
		Series: "19XX", Homebrew: "no", Bootleg: "no", Year: "1990", Manufacturer: "Capcom",
		Category: "Shooter - Flying Vertical", Resolution: "15kHz", Rotation: "vertical (ccw)",
		Flip: "yes", Players: "2 (simultaneous)", MoveInputs: "8-way", SpecialControls: "",
		NumButtons: "2",
	}})

	require.Len(t, entries, 1)
	assert.Equal(t, "1941", entries[0].SetName)
	assert.Equal(t, "World", entries[0].Region)
	assert.Equal(t, "900227", entries[0].Version)
	assert.Equal(t, "yes", entries[0].Alternative)
	assert.Equal(t, "1941- Counter Attack", entries[0].ParentTitle)
	assert.Equal(t, "Capcom CPS-1", entries[0].Platform)
	assert.Equal(t, "19XX", entries[0].Series)
	assert.Equal(t, "no", entries[0].Homebrew)
	assert.Equal(t, "no", entries[0].Bootleg)
	assert.Equal(t, "1990", entries[0].Year)
	assert.Equal(t, "Capcom", entries[0].Manufacturer)
	assert.Equal(t, "Shooter - Flying Vertical", entries[0].Category)
	assert.Equal(t, "15kHz", entries[0].Resolution)
	assert.Equal(t, "vertical (ccw)", entries[0].Rotation)
	assert.Equal(t, "yes", entries[0].Flip)
	assert.Equal(t, "2 (simultaneous)", entries[0].Players)
	assert.Equal(t, "8-way", entries[0].MoveInputs)
	assert.Equal(t, "2", entries[0].NumButtons)
}

func TestCachedSetNameOnlyAnswersForAnUnchangedFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "game.mra")
	require.NoError(t, os.WriteFile(path, []byte("<misterromdescription/>"), 0o600))
	info, err := os.Stat(path)
	require.NoError(t, err)

	entry := arcadeClassCacheEntry{SetName: "cps1game", Size: info.Size(), MtimeNs: info.ModTime().UnixNano()}
	cached := map[string]arcadeClassCacheEntry{path: entry}

	setName, ok := cachedSetName(cached, path)
	assert.True(t, ok)
	assert.Equal(t, "cps1game", setName)

	// A descriptor the cache has never seen, and one the classification read
	// but found no set name in, are different answers: the second is cached.
	_, ok = cachedSetName(cached, filepath.Join(dir, "other.mra"))
	assert.False(t, ok)
	unreadable := map[string]arcadeClassCacheEntry{
		path: {SetName: "", Size: info.Size(), MtimeNs: info.ModTime().UnixNano()},
	}
	setName, ok = cachedSetName(unreadable, path)
	assert.True(t, ok, "a cached empty set name spares a re-read")
	assert.Empty(t, setName)

	// Rewriting the descriptor invalidates the entry so the scraper reads it.
	require.NoError(t, os.WriteFile(path, []byte("<misterromdescription><setname>new</setname>"+
		"</misterromdescription>"), 0o600))
	require.NoError(t, os.Chtimes(path, time.Now(), info.ModTime().Add(time.Second)))
	_, ok = cachedSetName(cached, path)
	assert.False(t, ok)
}
