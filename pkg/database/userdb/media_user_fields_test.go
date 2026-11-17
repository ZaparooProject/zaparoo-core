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

package userdb_test

import (
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fieldValues(fields []database.MediaUserField) map[string]string {
	values := make(map[string]string, len(fields))
	for i := range fields {
		values[filepath.Base(fields[i].Path)+"|"+fields[i].Field] = fields[i].Value
	}
	return values
}

func TestMediaUserFieldsSetGetDelete(t *testing.T) {
	t.Parallel()
	db, cleanup := testhelpers.NewInMemoryUserDB(t)
	t.Cleanup(cleanup)
	usa := filepath.Join("roms", "NES", "Alpha (USA).nes")
	europe := filepath.Join("roms", "NES", "Alpha (Europe).nes")
	boxart := database.MediaUserFieldImagePrefix + "boxart"

	set := func(path, field, value string, titlePaths ...string) {
		t.Helper()
		require.NoError(t, db.SetMediaUserField(&database.MediaUserField{
			SystemID: "NES", Path: path, Field: field, Value: value, Slug: "alpha",
		}, titlePaths))
	}
	set(usa, database.MediaUserFieldDescription, "first", usa, europe)
	set(usa, boxart, "usa.png", usa, europe)
	set(europe, boxart, "europe.png", usa, europe)
	// The same title-level field set from the other version replaces it.
	set(europe, database.MediaUserFieldDescription, "second", usa, europe)

	all, err := db.ListMediaUserFields()
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"Alpha (Europe).nes|description":  "second",
		"Alpha (USA).nes|image:boxart":    "usa.png",
		"Alpha (Europe).nes|image:boxart": "europe.png",
	}, fieldValues(all), "a title has one description; each file keeps its own image")
	for i := range all {
		assert.Equal(t, "alpha", all[i].Slug)
		assert.NotZero(t, all[i].UpdatedAt)
	}

	forTitle, err := db.GetMediaUserFieldsForTitle("NES", "alpha", []string{usa})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"Alpha (Europe).nes|description": "second",
		"Alpha (USA).nes|image:boxart":   "usa.png",
	}, fieldValues(forTitle), "a slug match brings title-level fields only")

	other, err := db.GetMediaUserFieldsForTitle("SNES", "alpha", []string{usa})
	require.NoError(t, err)
	assert.Empty(t, other, "fields belong to their system")

	require.NoError(t, db.DeleteMediaUserField("NES", "alpha", []string{usa}, boxart))
	require.NoError(t, db.DeleteMediaUserField("NES", "alpha", []string{usa}, database.MediaUserFieldDescription))
	all, err = db.ListMediaUserFields()
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"Alpha (Europe).nes|image:boxart": "europe.png"}, fieldValues(all),
		"an image is removed from its own file only; a description from the whole title")

	require.NoError(t, db.DeleteMediaUserField("NES", "alpha", []string{usa}, boxart),
		"nothing to delete is not an error")
}

func TestMediaUserFieldsUpdateKeepsCreatedAt(t *testing.T) {
	t.Parallel()
	db, cleanup := testhelpers.NewInMemoryUserDB(t)
	t.Cleanup(cleanup)
	path := filepath.Join("roms", "NES", "Alpha.nes")
	field := &database.MediaUserField{SystemID: "NES", Path: path, Field: database.MediaUserFieldName, Value: "A"}

	require.NoError(t, db.SetMediaUserField(field, nil))
	field.Value = "B"
	require.NoError(t, db.SetMediaUserField(field, nil))

	all, err := db.ListMediaUserFields()
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.Equal(t, "B", all[0].Value)
	assert.NotZero(t, all[0].CreatedAt)
}
