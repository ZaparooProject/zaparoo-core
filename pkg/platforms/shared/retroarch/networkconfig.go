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
	"fmt"
	"path/filepath"

	"github.com/spf13/afero"
)

type ConfigProfile string

const (
	ConfigProfileManaged    ConfigProfile = "managed"
	ConfigProfileLowLatency ConfigProfile = "low_latency"

	NetworkCommandConfig = "network_cmd_enable = \"true\"\nnetwork_cmd_port = \"55355\"\n"
	managedConfig        = NetworkCommandConfig + "config_save_on_exit = \"false\"\n"
	lowLatencyConfig     = managedConfig +
		"video_vsync = \"true\"\n" +
		"video_threaded = \"false\"\n" +
		"audio_sync = \"true\"\n" +
		"audio_latency = \"64\"\n" +
		"run_ahead_enabled = \"false\"\n" +
		"rewind_enable = \"false\"\n" +
		"video_shader_enable = \"false\"\n" +
		"input_overlay = \"\"\n" +
		"input_overlay_enable = \"false\"\n" +
		"input_overlay_enable_autopreferred = \"false\"\n" +
		"auto_overrides_enable = \"false\"\n" +
		"menu_driver = \"rgui\"\n"
)

func ConfigForProfile(profile ConfigProfile) (string, error) {
	switch profile {
	case ConfigProfileManaged:
		return managedConfig, nil
	case ConfigProfileLowLatency:
		return lowLatencyConfig, nil
	default:
		return "", fmt.Errorf("unknown RetroArch config profile: %s", profile)
	}
}

// EnsureConfigProfile writes a Zaparoo-owned RetroArch overlay without
// changing the user's primary RetroArch configuration.
func EnsureConfigProfile(fs afero.Fs, path string, profile ConfigProfile) error {
	contents, err := ConfigForProfile(profile)
	if err != nil {
		return err
	}
	return writeConfig(fs, path, contents)
}

// EnsureNetworkCommandConfig writes Core's minimal RetroArch network-command
// overlay without changing the user's primary RetroArch configuration.
func EnsureNetworkCommandConfig(fs afero.Fs, path string) error {
	return writeConfig(fs, path, NetworkCommandConfig)
}

func writeConfig(fs afero.Fs, path, contents string) error {
	if fs == nil {
		fs = afero.NewOsFs()
	}
	if err := fs.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create RetroArch config directory: %w", err)
	}
	if err := afero.WriteFile(fs, path, []byte(contents), 0o600); err != nil {
		return fmt.Errorf("write RetroArch network config: %w", err)
	}
	return nil
}
