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
	"path/filepath"
	"strconv"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScraperGamelistXMLCustomPath_Default(t *testing.T) {
	t.Parallel()

	assert.Empty(t, (*Instance)(nil).ScraperGamelistXMLCustomPath())
	assert.Empty(t, (&Instance{}).ScraperGamelistXMLCustomPath())
}

func TestScraperGamelistXMLCustomPath_Load(t *testing.T) {
	t.Parallel()

	customPath := filepath.Join(t.TempDir(), "gamelists")
	cfg := &Instance{}
	err := cfg.LoadTOML("[scraper.gamelist_xml]\ncustom_path = " + strconv.Quote(customPath) + "\n")
	require.NoError(t, err)

	assert.Equal(t, customPath, cfg.ScraperGamelistXMLCustomPath())
}

func TestScraperGamelistXMLCustomPath_SaveLoadRoundTrip(t *testing.T) {
	t.Parallel()

	configDir := t.TempDir()
	customPath := filepath.Join(t.TempDir(), "gamelists")
	cfg, err := NewConfig(configDir, BaseDefaults)
	require.NoError(t, err)
	require.NoError(t, cfg.LoadTOML(
		"[scraper.gamelist_xml]\ncustom_path = "+strconv.Quote(customPath)+"\n",
	))
	require.NoError(t, cfg.Save())

	contents, err := afero.ReadFile(cfg.getFs(), cfg.cfgPath)
	require.NoError(t, err)
	assert.Contains(t, string(contents), "[scraper.gamelist_xml]")
	assert.Contains(t, string(contents), "custom_path = ")
	assert.Contains(t, string(contents), customPath)

	reloaded, err := NewConfig(configDir, BaseDefaults)
	require.NoError(t, err)
	assert.Equal(t, customPath, reloaded.ScraperGamelistXMLCustomPath())
}
