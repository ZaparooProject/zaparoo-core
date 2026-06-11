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

package mediadb

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/browseprefix"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeSQLQueryable struct {
	id int
}

type fakeSQLResult struct{}

func (fakeSQLResult) LastInsertId() (int64, error) {
	return 0, nil
}

func (fakeSQLResult) RowsAffected() (int64, error) {
	return 0, nil
}

func (*fakeSQLQueryable) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return fakeSQLResult{}, nil
}

func (*fakeSQLQueryable) PrepareContext(context.Context, string) (*sql.Stmt, error) {
	return &sql.Stmt{}, nil
}

func (*fakeSQLQueryable) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	return &sql.Rows{}, nil
}

func (*fakeSQLQueryable) QueryRowContext(context.Context, string, ...any) *sql.Row {
	return nil
}

func TestPrefixPolicyCacheKeyNormalizesSystems(t *testing.T) {
	pathPrefix := filepath.ToSlash(filepath.Join("roms", "nes"))
	left := prefixPolicyCacheKey(pathPrefix, []systemdefs.System{{ID: "SNES"}, {ID: "NES"}})
	right := prefixPolicyCacheKey(pathPrefix, []systemdefs.System{{ID: "NES"}, {ID: "SNES"}})

	assert.Equal(t, left, right)
	assert.Equal(t, pathPrefix, prefixPolicyCacheKey(pathPrefix, nil))
	assert.Equal(t, pathPrefix, prefixPolicyCacheKey(pathPrefix, []systemdefs.System{}))
}

func TestPrefixPolicyCacheRoundTrip(t *testing.T) {
	clearPrefixPolicyCache()
	t.Cleanup(clearPrefixPolicyCache)

	db := &fakeSQLQueryable{id: 1}
	otherDB := &fakeSQLQueryable{id: 2}
	key := prefixPolicyCacheKey(filepath.ToSlash(filepath.Join("roms", "nes")), []systemdefs.System{{ID: "NES"}})
	otherKey := prefixPolicyCacheKey(filepath.ToSlash(filepath.Join("roms", "snes")), []systemdefs.System{{ID: "SNES"}})
	policy := browseprefix.Policy{Kind: browseprefix.KindRank, Enabled: true}

	_, ok := cachedPrefixPolicy(db, key)
	assert.False(t, ok)

	storePrefixPolicy(db, key, policy)
	cached, ok := cachedPrefixPolicy(db, key)
	require.True(t, ok)
	assert.Equal(t, policy, cached)

	_, ok = cachedPrefixPolicy(otherDB, key)
	assert.False(t, ok)
	_, ok = cachedPrefixPolicy(db, otherKey)
	assert.False(t, ok)
}

func TestPrefixPolicyCacheInvalidation(t *testing.T) {
	clearPrefixPolicyCache()
	t.Cleanup(clearPrefixPolicyCache)

	db := &fakeSQLQueryable{id: 1}
	otherDB := &fakeSQLQueryable{id: 2}
	key := prefixPolicyCacheKey(filepath.ToSlash(filepath.Join("roms", "nes")), []systemdefs.System{{ID: "NES"}})
	otherKey := prefixPolicyCacheKey(filepath.ToSlash(filepath.Join("roms", "snes")), []systemdefs.System{{ID: "SNES"}})

	storePrefixPolicy(db, key, browseprefix.Policy{Kind: browseprefix.KindRank, Enabled: true})
	storePrefixPolicy(otherDB, otherKey, browseprefix.Policy{Kind: browseprefix.KindDate, Enabled: true})

	clearPrefixPolicyCacheFor(nil)
	_, ok := cachedPrefixPolicy(db, key)
	assert.True(t, ok)
	_, ok = cachedPrefixPolicy(otherDB, otherKey)
	assert.True(t, ok)

	clearPrefixPolicyCacheFor(db)
	_, ok = cachedPrefixPolicy(db, key)
	assert.False(t, ok)
	_, ok = cachedPrefixPolicy(otherDB, otherKey)
	assert.True(t, ok)

	clearPrefixPolicyCache()
	_, ok = cachedPrefixPolicy(otherDB, otherKey)
	assert.False(t, ok)
}
