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
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func benchMapping(pattern string) *database.Mapping {
	return &database.Mapping{
		Label: pattern, Type: MappingTypeID, Match: MatchTypeExact,
		Pattern: pattern, Override: "@NES/Game", Enabled: true,
	}
}

func TestGetEnabledMappings_CacheFollowsMutations(t *testing.T) {
	t.Parallel()
	db := newBenchUserDB(t)

	list, err := db.GetEnabledMappings()
	require.NoError(t, err)
	assert.Empty(t, list)

	require.NoError(t, db.AddMapping(benchMapping("aa01")))
	list, err = db.GetEnabledMappings()
	require.NoError(t, err)
	require.Len(t, list, 1)
	id := list[0].DBID

	updated := benchMapping("aa02")
	require.NoError(t, db.UpdateMapping(id, updated))
	list, err = db.GetEnabledMappings()
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "aa02", list[0].Pattern)

	disabled := benchMapping("aa02")
	disabled.Enabled = false
	require.NoError(t, db.UpdateMapping(id, disabled))
	list, err = db.GetEnabledMappings()
	require.NoError(t, err)
	assert.Empty(t, list)

	require.NoError(t, db.UpdateMapping(id, benchMapping("aa03")))
	require.NoError(t, db.DeleteMapping(id))
	list, err = db.GetEnabledMappings()
	require.NoError(t, err)
	assert.Empty(t, list)
}

func TestGetEnabledMappings_CachedReadSkipsDatabase(t *testing.T) {
	t.Parallel()
	db := newBenchUserDB(t)
	require.NoError(t, db.AddMapping(benchMapping("bb01")))

	first, err := db.GetEnabledMappings()
	require.NoError(t, err)
	require.Len(t, first, 1)

	// A row written behind the cache's back stays invisible, which proves the
	// second read was served from memory.
	_, err = db.sql.Load().ExecContext(db.ctx, "UPDATE Mappings SET Pattern = 'changed'")
	require.NoError(t, err)
	second, err := db.GetEnabledMappings()
	require.NoError(t, err)
	require.Len(t, second, 1)
	assert.Equal(t, "bb01", second[0].Pattern)

	// Callers append config mappings to the result; that must not leak back.
	second[0].Pattern = "mutated"
	third, err := db.GetEnabledMappings()
	require.NoError(t, err)
	assert.Equal(t, "bb01", third[0].Pattern)
}

func TestGetEnabledMappings_HandleSwapDropsCache(t *testing.T) {
	t.Parallel()
	db := newBenchUserDB(t)
	require.NoError(t, db.AddMapping(benchMapping("cc01")))
	list, err := db.GetEnabledMappings()
	require.NoError(t, err)
	require.Len(t, list, 1)

	// A restore replaces the database files and stores a new handle.
	otherPath := filepath.Join(t.TempDir(), "other.db")
	other, err := sql.Open(sqliteDriverName(), otherPath+sqliteConnParams)
	require.NoError(t, err)
	old := db.sql.Load()
	db.sql.Store(other)
	t.Cleanup(func() { _ = old.Close() })
	require.NoError(t, db.Allocate())

	list, err = db.GetEnabledMappings()
	require.NoError(t, err)
	assert.Empty(t, list)
}

func TestGetEnabledMappings_WriteDuringReadIsNotCached(t *testing.T) {
	t.Parallel()
	var cache enabledMappingsCache
	handle := &sql.DB{}
	generation := cache.generation()
	cache.invalidate()
	cache.put(handle, generation, []database.Mapping{{Pattern: "stale"}})
	_, ok := cache.get(handle)
	assert.False(t, ok)
}

func TestUpdateMapping_PrepareFailureReturnsError(t *testing.T) {
	t.Parallel()
	db := newBenchUserDB(t)
	require.NoError(t, db.sql.Load().Close())
	err := db.UpdateMapping(1, benchMapping("dd01"))
	require.Error(t, err)
}
