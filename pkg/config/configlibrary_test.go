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

	toml "github.com/pelletier/go-toml/v2"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLibrarySyncDefaultsDisabled(t *testing.T) {
	t.Parallel()
	cfg := &Instance{}
	assert.False(t, cfg.LibrarySyncEnabled())
	assert.Equal(t, DefaultLibraryBaseURL, cfg.LibraryBaseURL())
}

func TestLibrarySyncPersists(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	cfg, err := NewConfigWithFs(t.TempDir(), BaseDefaults, fs)
	require.NoError(t, err)
	cfg.SetLibrarySync(true)
	require.NoError(t, cfg.Save())
	cfg.SetLibrarySync(false)
	require.NoError(t, cfg.Load())
	assert.True(t, cfg.LibrarySyncEnabled())

	data, err := afero.ReadFile(fs, cfg.cfgPath)
	require.NoError(t, err)
	var persisted map[string]any
	require.NoError(t, toml.Unmarshal(data, &persisted))
	library, ok := persisted["library"].(map[string]any)
	require.True(t, ok, "library sync is written under [library]")
	assert.Equal(t, true, library["sync"])
	_, hasBaseURL := library["base_url"]
	assert.False(t, hasBaseURL, "the default endpoint is never written")
}

func TestSetLibraryBaseURL(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	cfg, err := NewConfigWithFs(t.TempDir(), BaseDefaults, fs)
	require.NoError(t, err)

	require.NoError(t, cfg.SetLibraryBaseURL("https://library.example.com/api/"))
	assert.Equal(t, "https://library.example.com/api", cfg.LibraryBaseURL())
	require.Error(t, cfg.SetLibraryBaseURL("http://example.com"))
	assert.Equal(t, "https://library.example.com/api", cfg.LibraryBaseURL())

	require.NoError(t, cfg.Save())
	require.NoError(t, cfg.Load())
	assert.Equal(t, "https://library.example.com/api", cfg.LibraryBaseURL())
}

func TestLibraryBaseURLInvalidInConfigFallsBack(t *testing.T) {
	t.Parallel()

	cfg := &Instance{}
	require.NoError(t, cfg.LoadTOML(`[library]
base_url = "http://example.com"
`))
	assert.Empty(t, cfg.vals.Library.BaseURL)
	assert.Equal(t, DefaultLibraryBaseURL, cfg.LibraryBaseURL())

	require.NoError(t, cfg.LoadTOML(`[library]
base_url = "http://127.0.0.1:8787"
`))
	assert.Equal(t, "http://127.0.0.1:8787", cfg.LibraryBaseURL())
}
