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
	"testing"

	toml "github.com/pelletier/go-toml/v2"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncryptionEnabled_DefaultsFalse(t *testing.T) {
	t.Parallel()

	cfg := &Instance{}
	assert.False(t, cfg.EncryptionEnabled(), "missing field should default to false")
}

func TestSetEncryptionEnabled(t *testing.T) {
	t.Parallel()

	cfg := &Instance{}

	cfg.SetEncryptionEnabled(true)
	assert.True(t, cfg.EncryptionEnabled())

	cfg.SetEncryptionEnabled(false)
	assert.False(t, cfg.EncryptionEnabled())
}

func TestEncryptionExplicitFalseOverridesTrueDefault(t *testing.T) {
	t.Setenv(CfgEnv, "")

	fs := afero.NewMemMapFs()
	configDir := "/config"
	require.NoError(t, fs.MkdirAll(configDir, 0o750))
	configPath := filepath.Join(configDir, CfgFile)
	require.NoError(t, afero.WriteFile(fs, configPath, []byte("[service]\nencryption = false\n"), 0o600))

	enabled := true
	defaults := BaseDefaults
	defaults.Service.Encryption = &enabled
	cfg, err := NewConfigWithFs(configDir, defaults, fs)
	require.NoError(t, err)
	assert.False(t, cfg.EncryptionEnabled())

	require.NoError(t, cfg.Save())
	data, err := afero.ReadFile(fs, configPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "encryption = false")

	// Removing the explicit override restores the unmutated platform default.
	require.NoError(t, afero.WriteFile(fs, configPath, []byte("[service]\n"), 0o600))
	require.NoError(t, cfg.Load())
	assert.True(t, cfg.EncryptionEnabled())
}

func TestServiceEncryption_ExplicitValues(t *testing.T) {
	t.Parallel()

	data, err := toml.Marshal(Service{})
	require.NoError(t, err)
	assert.NotContains(t, string(data), "encryption")

	enabled := true
	data, err = toml.Marshal(Service{Encryption: &enabled})
	require.NoError(t, err)
	assert.Contains(t, string(data), "encryption = true")

	var got Service
	require.NoError(t, toml.Unmarshal(data, &got))
	require.NotNil(t, got.Encryption)
	assert.True(t, *got.Encryption)

	disabled := false
	data, err = toml.Marshal(Service{Encryption: &disabled})
	require.NoError(t, err)
	assert.Contains(t, string(data), "encryption = false")

	got = Service{}
	require.NoError(t, toml.Unmarshal(data, &got))
	require.NotNil(t, got.Encryption)
	assert.False(t, *got.Encryption)
}
