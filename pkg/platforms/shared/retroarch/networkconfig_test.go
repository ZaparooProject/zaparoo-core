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

package retroarch

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigForProfile(t *testing.T) {
	t.Parallel()

	managed, err := ConfigForProfile(ConfigProfileManaged)
	require.NoError(t, err)
	assert.Equal(t, NetworkCommandConfig+"config_save_on_exit = \"false\"\n", managed)

	lowLatency, err := ConfigForProfile(ConfigProfileLowLatency)
	require.NoError(t, err)
	for _, setting := range []string{
		"network_cmd_enable = \"true\"",
		"config_save_on_exit = \"false\"",
		"video_vsync = \"true\"",
		"video_threaded = \"false\"",
		"audio_sync = \"true\"",
		"audio_latency = \"64\"",
		"run_ahead_enabled = \"false\"",
		"rewind_enable = \"false\"",
		"video_shader_enable = \"false\"",
		"input_overlay = \"\"",
		"input_overlay_enable = \"false\"",
		"input_overlay_enable_autopreferred = \"false\"",
		"auto_overrides_enable = \"false\"",
		"menu_driver = \"rgui\"",
	} {
		assert.Contains(t, lowLatency, setting)
	}
	assert.NotContains(t, lowLatency, "video_driver")
	assert.NotContains(t, lowLatency, "audio_driver")
	assert.NotContains(t, lowLatency, "input_driver")
	assert.NotContains(t, lowLatency, "input_poll_type_behavior")
	assert.NotContains(t, lowLatency, "video_hard_sync")
	assert.NotContains(t, lowLatency, "video_frame_delay")
	assert.NotContains(t, lowLatency, "video_max_swapchain_images")
}

func TestEnsureConfigProfile(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	path := filepath.Join("config", "retroarch-native.cfg")
	require.NoError(t, EnsureConfigProfile(fs, path, ConfigProfileLowLatency))

	data, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	expected, err := ConfigForProfile(ConfigProfileLowLatency)
	require.NoError(t, err)
	assert.Equal(t, expected, string(data))

	info, err := fs.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, "-rw-------", info.Mode().String())
}

func TestConfigForProfileRejectsUnknownProfile(t *testing.T) {
	t.Parallel()

	_, err := ConfigForProfile(ConfigProfile("unknown"))
	require.Error(t, err)
}
