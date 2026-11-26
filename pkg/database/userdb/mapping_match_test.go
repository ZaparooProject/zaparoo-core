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
	"errors"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeEnabledMappings struct {
	err      error
	mappings []database.Mapping
}

func (f fakeEnabledMappings) GetEnabledMappings() ([]database.Mapping, error) {
	return f.mappings, f.err
}

func mappingConfig(t *testing.T, toml string) *config.Instance {
	t.Helper()
	configDir := t.TempDir()
	mappingsDir := filepath.Join(configDir, config.MappingsDir)
	memFs := afero.NewMemMapFs()
	require.NoError(t, memFs.MkdirAll(mappingsDir, 0o750))
	require.NoError(t, afero.WriteFile(memFs, filepath.Join(mappingsDir, "test.toml"), []byte(toml), 0o600))
	cfg, err := config.NewConfigWithFs(configDir, config.BaseDefaults, memFs)
	require.NoError(t, err)
	require.NoError(t, cfg.LoadMappings(mappingsDir))
	return cfg
}

func TestFindMapping(t *testing.T) {
	t.Parallel()

	cfg := mappingConfig(t, `
[[mappings.entry]]
match_pattern = "04a1"
zapscript = "**launch:file"

[[mappings.entry]]
token_key = "value"
match_pattern = "hello"
zapscript = "**launch:value"
`)
	udb := fakeEnabledMappings{mappings: []database.Mapping{{
		DBID: 3, Label: "db", Type: MappingTypeID, Match: MatchTypeExact,
		Pattern: "04a1", Override: "**launch:db", Enabled: true,
	}}}

	match, ok := FindMapping(udb, cfg, &tokens.Token{UID: "04:A1"})
	require.True(t, ok)
	assert.Equal(t, MappingSourceDatabase, match.Source, "the database comes before the mappings folder")
	assert.Equal(t, "**launch:db", match.Mapping.Override)
	assert.Equal(t, int64(3), match.Mapping.DBID)

	match, ok = FindMapping(nil, cfg, &tokens.Token{UID: "04a1"})
	require.True(t, ok)
	assert.Equal(t, MappingSourceFile, match.Source)
	assert.Equal(t, "**launch:file", match.Mapping.Override)

	match, ok = FindMapping(udb, cfg, &tokens.Token{UID: "ffff", Text: "hello"})
	require.True(t, ok)
	assert.Equal(t, "**launch:value", match.Mapping.Override)

	_, ok = FindMapping(udb, cfg, &tokens.Token{UID: "ffff"})
	assert.False(t, ok)

	broken := fakeEnabledMappings{err: errors.New("db closed")}
	match, ok = FindMapping(broken, cfg, &tokens.Token{UID: "04a1"})
	require.True(t, ok, "a database error still checks the mappings folder")
	assert.Equal(t, MappingSourceFile, match.Source)

	_, ok = FindMapping(nil, nil, &tokens.Token{UID: "04a1"})
	assert.False(t, ok)
}
