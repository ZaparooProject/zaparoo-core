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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHostConfigIgnoresEnvironment(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(t.TempDir(), "external.toml")
	t.Setenv(CfgEnv, envPath)
	t.Setenv(AppEnv, "external-desktop-app")
	cfg, err := NewHostConfig(dir, Values{})
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, CfgFile), cfg.cfgPath)
	assert.Empty(t, cfg.appPath)
	assert.FileExists(t, cfg.cfgPath)
	assert.NoFileExists(t, envPath)
}

func TestHostConfigRejectsRelativePath(t *testing.T) {
	t.Parallel()
	_, err := NewHostConfig("relative", Values{})
	require.ErrorContains(t, err, "absolute")
}
