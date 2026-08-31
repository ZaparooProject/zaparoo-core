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
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMappingsFromConfig(t *testing.T) {
	t.Parallel()

	configDir := t.TempDir()
	mappingsDir := filepath.Join(configDir, config.MappingsDir)

	mappingTOML := `
[[mappings.entry]]
match_pattern = "exact-value"
zapscript = "**launch:exact"

[[mappings.entry]]
match_pattern = "prefix*"
zapscript = "**launch:partial"

[[mappings.entry]]
match_pattern = "/^abc[0-9]+$/"
zapscript = "**launch:regex"

[[mappings.entry]]
token_key = "value"
match_pattern = "hello"
zapscript = "**launch:value"

[[mappings.entry]]
token_key = "data"
match_pattern = "0401"
zapscript = "**launch:data"
`

	memFs := afero.NewMemMapFs()
	require.NoError(t, memFs.MkdirAll(mappingsDir, 0o750))
	require.NoError(t, afero.WriteFile(
		memFs, filepath.Join(mappingsDir, "test.toml"), []byte(mappingTOML), 0o600))

	cfg, err := config.NewConfigWithFs(configDir, config.BaseDefaults, memFs)
	require.NoError(t, err)
	require.NoError(t, cfg.LoadMappings(mappingsDir))

	mappings := MappingsFromConfig(cfg)
	require.Len(t, mappings, 5)

	for _, m := range mappings {
		assert.True(t, m.Enabled, "file mappings should be enabled")
		assert.Zero(t, m.DBID, "file mappings carry no database ID")
	}

	// exact: no wildcards, no regex delimiters
	assert.Equal(t, MappingTypeID, mappings[0].Type)
	assert.Equal(t, MatchTypeExact, mappings[0].Match)
	assert.Equal(t, "exact-value", mappings[0].Pattern)
	assert.Equal(t, "**launch:exact", mappings[0].Override)

	// glob "*" maps to partial with the asterisk stripped
	assert.Equal(t, MatchTypePartial, mappings[1].Match)
	assert.Equal(t, "prefix", mappings[1].Pattern)

	// "/.../" maps to regex with the delimiters stripped
	assert.Equal(t, MatchTypeRegex, mappings[2].Match)
	assert.Equal(t, "^abc[0-9]+$", mappings[2].Pattern)

	// token_key drives the mapping type
	assert.Equal(t, MappingTypeValue, mappings[3].Type)
	assert.Equal(t, MappingTypeData, mappings[4].Type)
}
