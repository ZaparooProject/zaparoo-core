//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package arkos

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseNetworkCommandConfig(t *testing.T) {
	t.Parallel()

	enabled, port := parseNetworkCommandConfig("video_driver = \"gl\"\n")
	assert.False(t, enabled)
	assert.Equal(t, defaultRetroArchPort, port)

	enabled, port = parseNetworkCommandConfig("network_cmd_enable = \"true\"\nnetwork_cmd_port = \"55400\"\n")
	assert.True(t, enabled)
	assert.Equal(t, 55400, port)

	enabled, port = parseNetworkCommandConfig("network_cmd_enable=false\nnetwork_cmd_port = \"99999\"\n")
	assert.False(t, enabled)
	assert.Equal(t, defaultRetroArchPort, port)
}

func TestRetroArchConfigPath(t *testing.T) {
	t.Parallel()

	ra64 := filepath.Join(frontendHome, ".config", "retroarch", "retroarch.cfg")
	ra32 := filepath.Join(frontendHome, ".config", "retroarch32", "retroarch.cfg")
	tests := []struct {
		launch esLaunch
		want   string
	}{
		{launch: esLaunch{emulator: "retroarch"}, want: ra64},
		{launch: esLaunch{emulator: "retroarch32"}, want: ra32},
		{launch: esLaunch{emulator: "x", command: "nice /opt/retroarch/bin/retroarch32 -L c %ROM%"}, want: ra32},
		{launch: esLaunch{emulator: "x", command: "nice /usr/local/bin/retroarch -L c %ROM%"}, want: ra64},
		{launch: esLaunch{emulator: "ppsspp", command: "/opt/ppsspp/PPSSPPSDL %ROM%"}},
	}
	for _, tt := range tests {
		got, ok := retroArchConfigPath(&tt.launch)
		assert.Equal(t, tt.want != "", ok)
		assert.Equal(t, tt.want, got)
	}
}

func TestRetroArchControls_OnlyWhenNetworkCommandsEnabled(t *testing.T) {
	t.Parallel()

	fs := newTestFs(t)
	p := newTestPlatform(t, fs, newMockCmd())
	launch := esLaunch{emulator: "retroarch"}

	assert.Nil(t, p.retroArchControls(&launch), "no config")

	cfgPath := filepath.Join(frontendHome, ".config", "retroarch", "retroarch.cfg")
	writeFile(t, fs, cfgPath, "network_cmd_enable = \"false\"\n")
	assert.Nil(t, p.retroArchControls(&launch))

	writeFile(t, fs, cfgPath, "network_cmd_enable = \"true\"\n")
	controls := p.retroArchControls(&launch)
	require.NotEmpty(t, controls)

	data, err := readLimited(fs, cfgPath)
	require.NoError(t, err)
	assert.Equal(t, "network_cmd_enable = \"true\"\n", string(data), "config is not modified")
}
