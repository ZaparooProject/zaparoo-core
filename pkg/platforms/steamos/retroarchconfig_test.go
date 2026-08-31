//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package steamos

import (
	"path/filepath"
	"testing"

	sharedretroarch "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/retroarch"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnsureNativeRetroArchSystemConfigsDisablesBezels(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	zaparooDir := filepath.Join("home", "config", "zaparoo")
	cores := []sharedretroarch.CoreLaunch{
		{SystemID: "SNES", Folders: []string{"snes"}},
		{SystemID: "SNES", Folders: []string{"sfc"}},
		{SystemID: "NES", Folders: []string{"nes"}},
	}

	require.NoError(t, ensureNativeRetroArchSystemConfigs(fs, zaparooDir, cores))

	for _, systemID := range []string{"SNES", "NES"} {
		data, err := afero.ReadFile(fs, nativeRetroArchSystemConfigPath(zaparooDir, systemID))
		require.NoError(t, err)
		config := string(data)
		assert.Contains(t, config, "input_overlay = \"\"")
		assert.Contains(t, config, "input_overlay_enable = \"false\"")
		assert.Contains(t, config, "input_overlay_enable_autopreferred = \"false\"")
		assert.Contains(t, config, "auto_overrides_enable = \"false\"")
		assert.Contains(t, config, "video_shader_enable = \"false\"")
		assert.Contains(t, config, "menu_driver = \"rgui\"")
	}
}
