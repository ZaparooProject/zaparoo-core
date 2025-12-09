//go:build linux

/*
Zaparoo Core
Copyright (C) 2024, 2025 Callan Barrett

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

package linuxbase

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/adrg/xdg"
	"github.com/stretchr/testify/assert"
)

func TestSettings(t *testing.T) {
	t.Parallel()

	settings := Settings()

	// Verify all paths contain the app name
	assert.Contains(t, settings.DataDir, config.AppName)
	assert.Contains(t, settings.ConfigDir, config.AppName)
	assert.Contains(t, settings.TempDir, config.AppName)
	assert.Contains(t, settings.LogDir, config.AppName)

	// Verify paths use XDG directories
	assert.True(t, strings.HasPrefix(settings.DataDir, xdg.DataHome),
		"DataDir should be under XDG_DATA_HOME")
	assert.True(t, strings.HasPrefix(settings.ConfigDir, xdg.ConfigHome),
		"ConfigDir should be under XDG_CONFIG_HOME")
	assert.True(t, strings.HasPrefix(settings.TempDir, os.TempDir()),
		"TempDir should be under system temp directory")

	// LogDir should be under DataDir
	assert.True(t, strings.HasPrefix(settings.LogDir, settings.DataDir),
		"LogDir should be under DataDir")

	// Verify ZipsAsDirs is false for Linux platforms
	assert.False(t, settings.ZipsAsDirs)
}

func TestSettingsPathStructure(t *testing.T) {
	t.Parallel()

	settings := Settings()

	// Verify DataDir structure
	expectedDataDir := filepath.Join(xdg.DataHome, config.AppName)
	assert.Equal(t, expectedDataDir, settings.DataDir)

	// Verify ConfigDir structure
	expectedConfigDir := filepath.Join(xdg.ConfigHome, config.AppName)
	assert.Equal(t, expectedConfigDir, settings.ConfigDir)

	// Verify TempDir structure
	expectedTempDir := filepath.Join(os.TempDir(), config.AppName)
	assert.Equal(t, expectedTempDir, settings.TempDir)

	// Verify LogDir structure (under DataDir with logs subdirectory)
	expectedLogDir := filepath.Join(xdg.DataHome, config.AppName, config.LogsDir)
	assert.Equal(t, expectedLogDir, settings.LogDir)
}
