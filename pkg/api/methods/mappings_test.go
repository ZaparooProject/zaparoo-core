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
	"context"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/userdb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func dbMappingFixture() database.Mapping {
	return database.Mapping{
		DBID:     7,
		Label:    "my mapping",
		Type:     userdb.MappingTypeID,
		Match:    userdb.MatchTypeExact,
		Pattern:  "abcdef",
		Override: "**launch:db",
		Added:    1700000000,
		Enabled:  true,
	}
}

func configWithFileMapping(t *testing.T) *config.Instance {
	t.Helper()

	configDir := t.TempDir()
	mappingsDir := filepath.Join(configDir, config.MappingsDir)
	mappingTOML := `
[[mappings.entry]]
match_pattern = "file-pattern"
zapscript = "**launch:file"
`
	memFs := afero.NewMemMapFs()
	require.NoError(t, memFs.MkdirAll(mappingsDir, 0o750))
	require.NoError(t, afero.WriteFile(
		memFs, filepath.Join(mappingsDir, "test.toml"), []byte(mappingTOML), 0o600))

	cfg, err := config.NewConfigWithFs(configDir, config.BaseDefaults, memFs)
	require.NoError(t, err)
	require.NoError(t, cfg.LoadMappings(mappingsDir))
	return cfg
}

func TestHandleMappings_DBOnlyByDefault(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockUserDB.On("GetAllMappings").Return([]database.Mapping{dbMappingFixture()}, nil)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB},
		Config:   configWithFileMapping(t),
		Params:   nil,
	}

	result, err := HandleMappings(env)
	require.NoError(t, err)

	resp, ok := result.(models.AllMappingsResponse)
	require.True(t, ok)
	require.Len(t, resp.Mappings, 1, "file mappings must be excluded without opt-in")

	m := resp.Mappings[0]
	assert.Equal(t, "7", m.ID)
	assert.Equal(t, mappingSourceDatabase, m.Source)
	assert.False(t, m.ReadOnly)
	// legacy v0.1 type name conversion
	assert.Equal(t, userdb.LegacyMappingTypeUID, m.Type)
	assert.NotEmpty(t, m.Added)
}

func TestHandleMappings_IncludeReadOnlyAddsFileMappings(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockUserDB.On("GetAllMappings").Return([]database.Mapping{dbMappingFixture()}, nil)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB},
		Config:   configWithFileMapping(t),
		Params:   []byte(`{"includeReadOnly":true}`),
	}

	result, err := HandleMappings(env)
	require.NoError(t, err)

	resp, ok := result.(models.AllMappingsResponse)
	require.True(t, ok)
	require.Len(t, resp.Mappings, 2)

	db := resp.Mappings[0]
	assert.Equal(t, mappingSourceDatabase, db.Source)
	assert.False(t, db.ReadOnly)

	file := resp.Mappings[1]
	assert.Equal(t, mappingSourceFile, file.Source)
	assert.True(t, file.ReadOnly)
	assert.Empty(t, file.ID, "file mappings have no database ID")
	assert.Empty(t, file.Added, "file mappings have no timestamp")
	assert.Equal(t, "file-pattern", file.Pattern)
	assert.Equal(t, "**launch:file", file.Override)
}

func TestHandleMappings_InvalidParams(t *testing.T) {
	t.Parallel()

	env := requests.RequestEnv{
		Context: context.Background(),
		Params:  []byte(`{"includeReadOnly":"nope"}`),
	}

	_, err := HandleMappings(env)
	require.Error(t, err)
}
