//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package batocera

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseESSystemsConfig_BasicParsing(t *testing.T) {
	t.Parallel()

	// Create temp directory with test config
	tmpDir := t.TempDir()

	mainConfig := `<?xml version="1.0" encoding="UTF-8"?>
<systemList>
  <system>
    <name>nes</name>
    <path>/userdata/roms/nes</path>
  </system>
  <system>
    <name>snes</name>
    <path>/userdata/roms/snes</path>
  </system>
</systemList>`

	err := os.WriteFile(filepath.Join(tmpDir, ESSystemsConfigFile), []byte(mainConfig), 0o600)
	require.NoError(t, err)

	config, err := ParseESSystemsConfig(tmpDir)
	require.NoError(t, err)
	require.NotNil(t, config)

	assert.Len(t, config.Systems, 2)
	assert.Equal(t, "/userdata/roms/nes", config.Systems["nes"].Path)
	assert.Equal(t, "/userdata/roms/snes", config.Systems["snes"].Path)
}

func TestParseESSystemsConfig_OverlayMerge(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Main config with NES
	mainConfig := `<?xml version="1.0" encoding="UTF-8"?>
<systemList>
  <system>
    <name>nes</name>
    <path>/userdata/roms/nes</path>
  </system>
</systemList>`

	// Overlay adds genesis and overrides NES path
	overlayConfig := `<?xml version="1.0" encoding="UTF-8"?>
<systemList>
  <system>
    <name>genesis</name>
    <path>/media/SHARE/roms/genesis</path>
  </system>
  <system>
    <name>nes</name>
    <path>/media/SHARE/roms/nes</path>
  </system>
</systemList>`

	err := os.WriteFile(filepath.Join(tmpDir, ESSystemsConfigFile), []byte(mainConfig), 0o600)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(tmpDir, "es_systems_custom.cfg"), []byte(overlayConfig), 0o600)
	require.NoError(t, err)

	config, err := ParseESSystemsConfig(tmpDir)
	require.NoError(t, err)
	require.NotNil(t, config)

	assert.Len(t, config.Systems, 2)
	// Overlay should override NES path
	assert.Equal(t, "/media/SHARE/roms/nes", config.Systems["nes"].Path)
	// Overlay should add genesis
	assert.Equal(t, "/media/SHARE/roms/genesis", config.Systems["genesis"].Path)
}

func TestParseESSystemsConfig_MissingFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	// No config files created

	config, err := ParseESSystemsConfig(tmpDir)
	require.NoError(t, err)
	require.NotNil(t, config)
	assert.Empty(t, config.Systems)
}

func TestParseESSystemsConfig_MalformedXML(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	malformedConfig := `<?xml version="1.0" encoding="UTF-8"?>
<systemList>
  <system>
    <name>nes</name>
    <path>/userdata/roms/nes
  </system>
</systemList>`

	err := os.WriteFile(filepath.Join(tmpDir, ESSystemsConfigFile), []byte(malformedConfig), 0o600)
	require.NoError(t, err)

	config, err := ParseESSystemsConfig(tmpDir)
	// Should not error - just skip malformed file
	require.NoError(t, err)
	require.NotNil(t, config)
	assert.Empty(t, config.Systems)
}

func TestParseESSystemsConfig_EmptyNameOrPath(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	config := `<?xml version="1.0" encoding="UTF-8"?>
<systemList>
  <system>
    <name></name>
    <path>/userdata/roms/empty</path>
  </system>
  <system>
    <name>nopath</name>
    <path></path>
  </system>
  <system>
    <name>valid</name>
    <path>/userdata/roms/valid</path>
  </system>
</systemList>`

	err := os.WriteFile(filepath.Join(tmpDir, ESSystemsConfigFile), []byte(config), 0o600)
	require.NoError(t, err)

	parsed, err := ParseESSystemsConfig(tmpDir)
	require.NoError(t, err)
	require.NotNil(t, parsed)

	// Only valid entry should be parsed
	assert.Len(t, parsed.Systems, 1)
	assert.Equal(t, "/userdata/roms/valid", parsed.Systems["valid"].Path)
}

func TestGetROMPaths_Deduplication(t *testing.T) {
	t.Parallel()

	config := &ESSystemConfig{
		Systems: map[string]ESSystem{
			"nes":     {Name: "nes", Path: "/userdata/roms/nes"},
			"snes":    {Name: "snes", Path: "/userdata/roms/snes"},
			"genesis": {Name: "genesis", Path: "/userdata/roms/genesis"},
			"sms":     {Name: "sms", Path: "/media/SHARE/roms/sms"},
		},
	}

	paths := config.GetROMPaths()

	// Should deduplicate to 2 unique ROM roots
	assert.Len(t, paths, 2)
	assert.Contains(t, paths, "/userdata/roms")
	assert.Contains(t, paths, "/media/SHARE/roms")
}

func TestGetROMPaths_NilConfig(t *testing.T) {
	t.Parallel()

	var config *ESSystemConfig
	paths := config.GetROMPaths()
	assert.Nil(t, paths)
}

func TestGetROMPaths_EmptySystems(t *testing.T) {
	t.Parallel()

	config := &ESSystemConfig{
		Systems: make(map[string]ESSystem),
	}

	paths := config.GetROMPaths()
	assert.Empty(t, paths)
}

func TestExpandPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "absolute path unchanged",
			input:    "/userdata/roms/nes",
			expected: "/userdata/roms/nes",
		},
		{
			name:     "relative path made absolute",
			input:    "roms/nes",
			expected: "/userdata/roms/roms/nes",
		},
		{
			name:     "empty path returns empty",
			input:    "",
			expected: "",
		},
		{
			name:     "path with double slashes cleaned",
			input:    "/userdata//roms//nes",
			expected: "/userdata/roms/nes",
		},
		{
			name:     "path with dots cleaned",
			input:    "/userdata/roms/../roms/nes",
			expected: "/userdata/roms/nes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := expandPath(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractROMRoot(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "system path returns roms root",
			input:    "/userdata/roms/nes",
			expected: "/userdata/roms",
		},
		{
			name:     "external drive path",
			input:    "/media/SHARE/roms/genesis",
			expected: "/media/SHARE/roms",
		},
		{
			name:     "direct roms path",
			input:    "/userdata/roms",
			expected: "/userdata/roms",
		},
		{
			name:     "no roms in path returns parent",
			input:    "/media/SHARE/games/nes",
			expected: "/media/SHARE/games",
		},
		{
			name:     "empty path returns empty",
			input:    "",
			expected: "",
		},
		{
			name:     "case insensitive roms detection",
			input:    "/media/SHARE/ROMS/nes",
			expected: "/media/SHARE/ROMS",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := extractROMRoot(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseESSystemsConfig_MultipleOverlays(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Main config
	mainConfig := `<?xml version="1.0" encoding="UTF-8"?>
<systemList>
  <system>
    <name>nes</name>
    <path>/userdata/roms/nes</path>
  </system>
</systemList>`

	// First overlay
	overlay1 := `<?xml version="1.0" encoding="UTF-8"?>
<systemList>
  <system>
    <name>snes</name>
    <path>/media/USB1/roms/snes</path>
  </system>
</systemList>`

	// Second overlay
	overlay2 := `<?xml version="1.0" encoding="UTF-8"?>
<systemList>
  <system>
    <name>genesis</name>
    <path>/media/USB2/roms/genesis</path>
  </system>
</systemList>`

	err := os.WriteFile(filepath.Join(tmpDir, ESSystemsConfigFile), []byte(mainConfig), 0o600)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(tmpDir, "es_systems_usb1.cfg"), []byte(overlay1), 0o600)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(tmpDir, "es_systems_usb2.cfg"), []byte(overlay2), 0o600)
	require.NoError(t, err)

	config, err := ParseESSystemsConfig(tmpDir)
	require.NoError(t, err)
	require.NotNil(t, config)

	assert.Len(t, config.Systems, 3)
	assert.Equal(t, "/userdata/roms/nes", config.Systems["nes"].Path)
	assert.Equal(t, "/media/USB1/roms/snes", config.Systems["snes"].Path)
	assert.Equal(t, "/media/USB2/roms/genesis", config.Systems["genesis"].Path)

	// Verify GetROMPaths returns unique roots
	paths := config.GetROMPaths()
	assert.Len(t, paths, 3)
	assert.Contains(t, paths, "/userdata/roms")
	assert.Contains(t, paths, "/media/USB1/roms")
	assert.Contains(t, paths, "/media/USB2/roms")
}
