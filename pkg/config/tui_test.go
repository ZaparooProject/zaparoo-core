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
	"os"
	"path/filepath"
	"testing"

	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyTUIDefaults_AllNil(t *testing.T) {
	t.Parallel()

	cfg := applyTUIDefaults(tuiConfigRaw{}, "generic")

	assert.Equal(t, defaultTUITheme, cfg.Theme)
	assert.Equal(t, defaultTUIWriteFormat, cfg.WriteFormat)
	assert.True(t, cfg.Mouse)
	assert.False(t, cfg.CRTMode)
	assert.False(t, cfg.OnScreenKeyboard)
	assert.False(t, cfg.ErrorReportingPrompted)
}

func TestApplyTUIDefaults_AllSet(t *testing.T) {
	t.Parallel()

	theme := "dark"
	writeFormat := "uid"
	mouse := false
	crt := true
	osk := true
	prompted := true

	raw := tuiConfigRaw{
		Theme:                  &theme,
		WriteFormat:            &writeFormat,
		Mouse:                  &mouse,
		CRTMode:                &crt,
		OnScreenKeyboard:       &osk,
		ErrorReportingPrompted: &prompted,
	}

	cfg := applyTUIDefaults(raw, "generic")

	assert.Equal(t, "dark", cfg.Theme)
	assert.Equal(t, "uid", cfg.WriteFormat)
	assert.False(t, cfg.Mouse)
	assert.True(t, cfg.CRTMode)
	assert.True(t, cfg.OnScreenKeyboard)
	assert.True(t, cfg.ErrorReportingPrompted)
}

func TestApplyTUIDefaults_MisterPlatformDefaults(t *testing.T) {
	t.Parallel()

	cfg := applyTUIDefaults(tuiConfigRaw{}, platformids.Mister)

	assert.True(t, cfg.CRTMode, "MiSTer should default CRT mode to true")
	assert.True(t, cfg.OnScreenKeyboard, "MiSTer should default OSK to true")
}

func TestApplyTUIDefaults_MistexPlatformDefaults(t *testing.T) {
	t.Parallel()

	cfg := applyTUIDefaults(tuiConfigRaw{}, platformids.Mistex)

	assert.True(t, cfg.CRTMode, "MiSTex should default CRT mode to true")
	assert.True(t, cfg.OnScreenKeyboard, "MiSTex should default OSK to true")
}

func TestApplyTUIDefaults_PlatformDefaultsOverriddenByUser(t *testing.T) {
	t.Parallel()

	crt := false
	osk := false
	raw := tuiConfigRaw{
		CRTMode:          &crt,
		OnScreenKeyboard: &osk,
	}

	cfg := applyTUIDefaults(raw, platformids.Mister)

	assert.False(t, cfg.CRTMode, "User override should take precedence over platform default")
	assert.False(t, cfg.OnScreenKeyboard, "User override should take precedence over platform default")
}

func TestApplyTUIDefaults_ErrorReportingPromptedExplicitFalse(t *testing.T) {
	t.Parallel()

	prompted := false
	raw := tuiConfigRaw{
		ErrorReportingPrompted: &prompted,
	}

	cfg := applyTUIDefaults(raw, "generic")

	assert.False(t, cfg.ErrorReportingPrompted, "Explicit false should be preserved")
}

// TestTUIConfigGlobalState tests functions that mutate the global tuiCfg atomic.
// These cannot run in parallel with each other.
func TestTUIConfigGlobalState(t *testing.T) {
	t.Run("GetSetTUIConfig", func(t *testing.T) {
		cfg := TUIConfig{
			Theme:                  "custom",
			WriteFormat:            "uid",
			Mouse:                  false,
			CRTMode:                true,
			OnScreenKeyboard:       true,
			ErrorReportingPrompted: true,
		}

		SetTUIConfig(cfg)
		got := GetTUIConfig()

		assert.Equal(t, cfg, got)
	})

	t.Run("LoadTUIConfig_CreatesDefaultWhenMissing", func(t *testing.T) {
		dir := t.TempDir()

		err := LoadTUIConfig(dir, "generic")
		require.NoError(t, err)

		tuiPath := filepath.Join(dir, TUIFile)
		_, err = os.Stat(tuiPath)
		require.NoError(t, err, "TUI config file should be created")

		cfg := GetTUIConfig()
		assert.Equal(t, defaultTUITheme, cfg.Theme)
		assert.Equal(t, defaultTUIWriteFormat, cfg.WriteFormat)
		assert.True(t, cfg.Mouse)
		assert.False(t, cfg.ErrorReportingPrompted)
	})

	t.Run("LoadTUIConfig_ReadsExistingFile", func(t *testing.T) {
		dir := t.TempDir()
		tuiPath := filepath.Join(dir, TUIFile)

		content := `theme = "retro"
write_format = "uid"
mouse = false
crt_mode = true
on_screen_keyboard = false
error_reporting_prompted = true
`
		err := os.WriteFile(tuiPath, []byte(content), 0o600)
		require.NoError(t, err)

		err = LoadTUIConfig(dir, "generic")
		require.NoError(t, err)

		cfg := GetTUIConfig()
		assert.Equal(t, "retro", cfg.Theme)
		assert.Equal(t, "uid", cfg.WriteFormat)
		assert.False(t, cfg.Mouse)
		assert.True(t, cfg.CRTMode)
		assert.False(t, cfg.OnScreenKeyboard)
		assert.True(t, cfg.ErrorReportingPrompted)
	})

	t.Run("LoadTUIConfig_FillsMissingWithDefaults", func(t *testing.T) {
		dir := t.TempDir()
		tuiPath := filepath.Join(dir, TUIFile)

		content := `theme = "minimal"
`
		err := os.WriteFile(tuiPath, []byte(content), 0o600)
		require.NoError(t, err)

		err = LoadTUIConfig(dir, "generic")
		require.NoError(t, err)

		cfg := GetTUIConfig()
		assert.Equal(t, "minimal", cfg.Theme)
		assert.Equal(t, defaultTUIWriteFormat, cfg.WriteFormat)
		assert.True(t, cfg.Mouse)
		assert.False(t, cfg.CRTMode)
		assert.False(t, cfg.ErrorReportingPrompted)
	})

	t.Run("SaveTUIConfig_RoundTrip", func(t *testing.T) {
		dir := t.TempDir()

		original := TUIConfig{
			Theme:                  "saved",
			WriteFormat:            "uid",
			Mouse:                  true,
			CRTMode:                false,
			OnScreenKeyboard:       true,
			ErrorReportingPrompted: true,
		}
		SetTUIConfig(original)

		err := SaveTUIConfig(dir)
		require.NoError(t, err)

		err = LoadTUIConfig(dir, "generic")
		require.NoError(t, err)

		loaded := GetTUIConfig()
		assert.Equal(t, original, loaded)
	})
}
