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

package config

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadMappings_LoadsFromAferoFS(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	mappingsDir := "/data/mappings"
	require.NoError(t, fs.MkdirAll(mappingsDir, 0o750))

	mappingTOML := `
[[mappings.entry]]
match_pattern = "*.sfc"
zapscript = "**launch:snes/{match}"

[[mappings.entry]]
match_pattern = "*.nes"
zapscript = "**launch:nes/{match}"
`
	require.NoError(t, afero.WriteFile(fs, mappingsDir+"/snes.toml", []byte(mappingTOML), 0o600))

	cfg := &Instance{fs: fs}

	err := cfg.LoadMappings(mappingsDir)
	require.NoError(t, err)

	mappings := cfg.Mappings()
	require.Len(t, mappings, 2)
	assert.Equal(t, "*.sfc", mappings[0].MatchPattern)
	assert.Equal(t, "**launch:snes/{match}", mappings[0].ZapScript)
	assert.Equal(t, "*.nes", mappings[1].MatchPattern)
}

func TestLoadMappings_MultipleFiles(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	mappingsDir := "/data/mappings"
	require.NoError(t, fs.MkdirAll(mappingsDir, 0o750))

	file1 := `
[[mappings.entry]]
match_pattern = "*.sfc"
zapscript = "**launch:snes/{match}"
`
	file2 := `
[[mappings.entry]]
match_pattern = "*.gen"
zapscript = "**launch:genesis/{match}"
`
	require.NoError(t, afero.WriteFile(fs, mappingsDir+"/snes.toml", []byte(file1), 0o600))
	require.NoError(t, afero.WriteFile(fs, mappingsDir+"/genesis.toml", []byte(file2), 0o600))

	cfg := &Instance{fs: fs}

	err := cfg.LoadMappings(mappingsDir)
	require.NoError(t, err)

	mappings := cfg.Mappings()
	require.Len(t, mappings, 2)
}

func TestLoadMappings_IgnoresNonTOMLFiles(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	mappingsDir := "/data/mappings"
	require.NoError(t, fs.MkdirAll(mappingsDir, 0o750))

	validTOML := `
[[mappings.entry]]
match_pattern = "*.sfc"
zapscript = "**launch:snes/{match}"
`
	require.NoError(t, afero.WriteFile(fs, mappingsDir+"/valid.toml", []byte(validTOML), 0o600))
	require.NoError(t, afero.WriteFile(fs, mappingsDir+"/readme.md", []byte("# Mappings"), 0o600))
	require.NoError(t, afero.WriteFile(fs, mappingsDir+"/backup.bak", []byte("old data"), 0o600))

	cfg := &Instance{fs: fs}

	err := cfg.LoadMappings(mappingsDir)
	require.NoError(t, err)

	mappings := cfg.Mappings()
	require.Len(t, mappings, 1)
	assert.Equal(t, "*.sfc", mappings[0].MatchPattern)
}

func TestLoadMappings_EmptyDirectory(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	mappingsDir := "/data/mappings"
	require.NoError(t, fs.MkdirAll(mappingsDir, 0o750))

	cfg := &Instance{fs: fs}

	err := cfg.LoadMappings(mappingsDir)
	require.NoError(t, err)

	mappings := cfg.Mappings()
	assert.Empty(t, mappings)
}

func TestLoadMappings_MissingDirectory(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()

	cfg := &Instance{fs: fs}

	err := cfg.LoadMappings("/nonexistent/mappings")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to stat mappings directory")
}

func TestLoadMappings_SkipsInvalidTOML(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	mappingsDir := "/data/mappings"
	require.NoError(t, fs.MkdirAll(mappingsDir, 0o750))

	validTOML := `
[[mappings.entry]]
match_pattern = "*.sfc"
zapscript = "**launch:snes/{match}"
`
	invalidTOML := `not valid [[[ toml content`

	require.NoError(t, afero.WriteFile(fs, mappingsDir+"/good.toml", []byte(validTOML), 0o600))
	require.NoError(t, afero.WriteFile(fs, mappingsDir+"/bad.toml", []byte(invalidTOML), 0o600))

	cfg := &Instance{fs: fs}

	err := cfg.LoadMappings(mappingsDir)
	require.NoError(t, err)

	mappings := cfg.Mappings()
	require.Len(t, mappings, 1)
	assert.Equal(t, "*.sfc", mappings[0].MatchPattern)
}

func TestLoadMappings_WithTokenKey(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	mappingsDir := "/data/mappings"
	require.NoError(t, fs.MkdirAll(mappingsDir, 0o750))

	mappingTOML := `
[[mappings.entry]]
token_key = "uid"
match_pattern = "04:AB:CD:*"
zapscript = "**launch:snes/mario.sfc"
`
	require.NoError(t, afero.WriteFile(fs, mappingsDir+"/uid_mapping.toml", []byte(mappingTOML), 0o600))

	cfg := &Instance{fs: fs}

	err := cfg.LoadMappings(mappingsDir)
	require.NoError(t, err)

	mappings := cfg.Mappings()
	require.Len(t, mappings, 1)
	assert.Equal(t, "uid", mappings[0].TokenKey)
	assert.Equal(t, "04:AB:CD:*", mappings[0].MatchPattern)
	assert.Equal(t, "**launch:snes/mario.sfc", mappings[0].ZapScript)
}
