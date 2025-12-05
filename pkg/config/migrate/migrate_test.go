// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

package migrate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIniToToml_APIPort(t *testing.T) {
	t.Parallel()

	t.Run("custom port is migrated", func(t *testing.T) {
		t.Parallel()

		iniContent := `[api]
port = 8080
`
		iniPath := filepath.Join(t.TempDir(), "tapto.ini")
		err := os.WriteFile(iniPath, []byte(iniContent), 0o600)
		require.NoError(t, err)

		vals, err := IniToToml(iniPath)

		require.NoError(t, err)
		require.NotNil(t, vals.Service.APIPort, "APIPort should be set for custom port")
		assert.Equal(t, 8080, *vals.Service.APIPort)
	})

	t.Run("default port is not migrated", func(t *testing.T) {
		t.Parallel()

		iniContent := `[api]
port = 7497
`
		iniPath := filepath.Join(t.TempDir(), "tapto.ini")
		err := os.WriteFile(iniPath, []byte(iniContent), 0o600)
		require.NoError(t, err)

		vals, err := IniToToml(iniPath)

		require.NoError(t, err)
		assert.Nil(t, vals.Service.APIPort, "APIPort should be nil for default port")
	})

	t.Run("invalid port is ignored", func(t *testing.T) {
		t.Parallel()

		iniContent := `[api]
port = invalid
`
		iniPath := filepath.Join(t.TempDir(), "tapto.ini")
		err := os.WriteFile(iniPath, []byte(iniContent), 0o600)
		require.NoError(t, err)

		vals, err := IniToToml(iniPath)

		require.NoError(t, err)
		assert.Nil(t, vals.Service.APIPort, "APIPort should be nil for invalid port")
	})

	t.Run("empty port is ignored", func(t *testing.T) {
		t.Parallel()

		iniContent := `[tapto]
debug = true
`
		iniPath := filepath.Join(t.TempDir(), "tapto.ini")
		err := os.WriteFile(iniPath, []byte(iniContent), 0o600)
		require.NoError(t, err)

		vals, err := IniToToml(iniPath)

		require.NoError(t, err)
		assert.Nil(t, vals.Service.APIPort, "APIPort should be nil when not specified")
		assert.True(t, vals.DebugLogging)
	})
}

func TestIniToToml_OtherSettings(t *testing.T) {
	t.Parallel()

	t.Run("reader settings are migrated", func(t *testing.T) {
		t.Parallel()

		iniContent := `[tapto]
reader = pn532_uart:/dev/ttyUSB0
probe_device = true
disable_sounds = false
exit_game = true
exit_game_delay = 2
`
		iniPath := filepath.Join(t.TempDir(), "tapto.ini")
		err := os.WriteFile(iniPath, []byte(iniContent), 0o600)
		require.NoError(t, err)

		vals, err := IniToToml(iniPath)

		require.NoError(t, err)
		require.Len(t, vals.Readers.Connect, 1)
		assert.Equal(t, "pn532_uart", vals.Readers.Connect[0].Driver)
		assert.Equal(t, "/dev/ttyUSB0", vals.Readers.Connect[0].Path)
		assert.True(t, vals.Readers.AutoDetect)
		assert.True(t, vals.Audio.ScanFeedback) // disable_sounds=false means feedback=true
		assert.Equal(t, config.ScanModeHold, vals.Readers.Scan.Mode)
		assert.InDelta(t, 2, vals.Readers.Scan.ExitDelay, 0.001)
	})
}

func TestRequired(t *testing.T) {
	t.Parallel()

	t.Run("returns true when ini exists and toml does not", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		iniPath := filepath.Join(tmpDir, "tapto.ini")
		tomlPath := filepath.Join(tmpDir, "config.toml")

		err := os.WriteFile(iniPath, []byte("[tapto]"), 0o600)
		require.NoError(t, err)

		assert.True(t, Required(iniPath, tomlPath))
	})

	t.Run("returns false when both exist", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		iniPath := filepath.Join(tmpDir, "tapto.ini")
		tomlPath := filepath.Join(tmpDir, "config.toml")

		err := os.WriteFile(iniPath, []byte("[tapto]"), 0o600)
		require.NoError(t, err)
		err = os.WriteFile(tomlPath, []byte("config_schema = 1"), 0o600)
		require.NoError(t, err)

		assert.False(t, Required(iniPath, tomlPath))
	})

	t.Run("returns false when ini does not exist", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		iniPath := filepath.Join(tmpDir, "tapto.ini")
		tomlPath := filepath.Join(tmpDir, "config.toml")

		assert.False(t, Required(iniPath, tomlPath))
	})
}
