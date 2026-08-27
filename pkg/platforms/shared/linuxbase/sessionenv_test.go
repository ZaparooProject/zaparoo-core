//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package linuxbase

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseDesktopSessionEnv(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{
		"DISPLAY=:1",
		"WAYLAND_DISPLAY=wayland-1",
		"XDG_RUNTIME_DIR=/run/user/1000",
		"DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1000/bus",
		"XDG_CONFIG_HOME=/home/test/.config",
		"XDG_DATA_HOME=/home/test/.local/share",
		"XDG_CACHE_HOME=/home/test/.cache",
		"XDG_STATE_HOME=/home/test/.local/state",
	}, ParseDesktopSessionEnv("PATH=/usr/bin\nDISPLAY=:1\nWAYLAND_DISPLAY=wayland-1\n"+
		"XDG_RUNTIME_DIR=/run/user/1000\nDBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1000/bus\n"+
		"XDG_CONFIG_HOME=/home/test/.config\nXDG_DATA_HOME=/home/test/.local/share\n"+
		"XDG_CACHE_HOME=/home/test/.cache\nXDG_STATE_HOME=/home/test/.local/state\nINVALID\n"))
}

func TestDesktopSessionEnvDefaults(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{
		"XDG_CONFIG_HOME=/home/test/.config",
		"XDG_DATA_HOME=/home/test/.local/share",
		"XDG_CACHE_HOME=/home/test/.cache",
		"XDG_STATE_HOME=/home/test/.local/state",
	}, desktopSessionEnvDefaults("/home/test"))
	assert.Nil(t, desktopSessionEnvDefaults(""))
}
