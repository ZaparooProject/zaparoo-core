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

package helpers

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLifecycle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected platforms.LauncherLifecycle
	}{
		{
			name:     "empty string defaults to blocking",
			input:    "",
			expected: platforms.LifecycleBlocking,
		},
		{
			name:     "blocking returns blocking",
			input:    "blocking",
			expected: platforms.LifecycleBlocking,
		},
		{
			name:     "background returns fire and forget",
			input:    "background",
			expected: platforms.LifecycleFireAndForget,
		},
		{
			name:     "Background (uppercase) returns fire and forget",
			input:    "Background",
			expected: platforms.LifecycleFireAndForget,
		},
		{
			name:     "BACKGROUND (all caps) returns fire and forget",
			input:    "BACKGROUND",
			expected: platforms.LifecycleFireAndForget,
		},
		{
			name:     "unknown value defaults to blocking",
			input:    "invalid",
			expected: platforms.LifecycleBlocking,
		},
		{
			name:     "tracked is not supported, defaults to blocking",
			input:    "tracked",
			expected: platforms.LifecycleBlocking,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := parseLifecycle(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseCustomLaunchers_NewFields(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test")

	customLaunchers := []config.LaunchersCustom{
		{
			ID:         "TestLauncher",
			Execute:    "echo [[media_path]]",
			MediaDirs:  []string{"/media/videos"},
			FileExts:   []string{".mp4", ".mkv"},
			Groups:     []string{"Video", "MediaPlayers"},
			Schemes:    []string{"test", "mytest"},
			Restricted: true,
			Lifecycle:  "background",
		},
		{
			ID:        "BlockingLauncher",
			Execute:   "vlc [[media_path]]",
			Lifecycle: "blocking",
		},
		{
			ID:        "DefaultLifecycleLauncher",
			Execute:   "mpv [[media_path]]",
			Lifecycle: "", // empty should default to blocking
		},
	}

	launchers := ParseCustomLaunchers(mockPlatform, customLaunchers)

	assert.Len(t, launchers, 3)

	// Test first launcher with all new fields
	launcher1 := launchers[0]
	assert.Equal(t, "TestLauncher", launcher1.ID)
	assert.Equal(t, []string{"Video", "MediaPlayers"}, launcher1.Groups)
	assert.Equal(t, []string{"test", "mytest"}, launcher1.Schemes)
	assert.True(t, launcher1.AllowListOnly)
	assert.Equal(t, platforms.LifecycleFireAndForget, launcher1.Lifecycle)
	assert.Equal(t, []string{"/media/videos"}, launcher1.Folders)
	assert.Equal(t, []string{".mp4", ".mkv"}, launcher1.Extensions)

	// Test second launcher with blocking lifecycle
	launcher2 := launchers[1]
	assert.Equal(t, "BlockingLauncher", launcher2.ID)
	assert.Equal(t, platforms.LifecycleBlocking, launcher2.Lifecycle)

	// Test third launcher with default (empty) lifecycle
	launcher3 := launchers[2]
	assert.Equal(t, "DefaultLifecycleLauncher", launcher3.ID)
	assert.Equal(t, platforms.LifecycleBlocking, launcher3.Lifecycle)
}

func TestParseCustomLaunchers_EmptyGroups(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test")

	customLaunchers := []config.LaunchersCustom{
		{
			ID:      "NoGroupsLauncher",
			Execute: "echo test",
		},
	}

	launchers := ParseCustomLaunchers(mockPlatform, customLaunchers)

	assert.Len(t, launchers, 1)
	assert.Nil(t, launchers[0].Groups)
	assert.Nil(t, launchers[0].Schemes)
	assert.False(t, launchers[0].AllowListOnly) // maps from Restricted field
}

func TestParseCustomLaunchers_SkipsUnknownSystem(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test")

	customLaunchers := []config.LaunchersCustom{
		{
			ID:      "GoodLauncher",
			System:  "SNES",
			Execute: "echo good",
		},
		{
			ID:      "BadSystemLauncher",
			System:  "NonExistentSystem12345",
			Execute: "echo bad",
		},
		{
			ID:      "NoSystemLauncher",
			Execute: "echo nosystem",
		},
	}

	launchers := ParseCustomLaunchers(mockPlatform, customLaunchers)

	// The launcher with the unknown system should be skipped
	assert.Len(t, launchers, 2)
	assert.Equal(t, "GoodLauncher", launchers[0].ID)
	assert.Equal(t, "SNES", launchers[0].SystemID)
	assert.Equal(t, "NoSystemLauncher", launchers[1].ID)
	assert.Empty(t, launchers[1].SystemID)
}

func TestParseCustomLaunchers_AllUnknownSystems(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test")

	customLaunchers := []config.LaunchersCustom{
		{
			ID:      "BadLauncher1",
			System:  "FakeSystem1",
			Execute: "echo bad1",
		},
		{
			ID:      "BadLauncher2",
			System:  "FakeSystem2",
			Execute: "echo bad2",
		},
	}

	launchers := ParseCustomLaunchers(mockPlatform, customLaunchers)
	assert.Empty(t, launchers)
}

func TestParseCustomLaunchers_AbsolutePathWithSystemID(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test")

	customLaunchers := []config.LaunchersCustom{
		{
			ID:        "ps2-custom",
			System:    "PS2",
			Execute:   "pcsx2 [[media_path]]",
			MediaDirs: []string{"/emulation/roms/ps2", "/mnt/games/ps2"},
			FileExts:  []string{"iso", ".bin", "MDF", " .chd "},
		},
	}

	launchers := ParseCustomLaunchers(mockPlatform, customLaunchers)

	require.Len(t, launchers, 1)
	l := launchers[0]

	assert.Equal(t, "ps2-custom", l.ID)
	assert.Equal(t, "PS2", l.SystemID, "SystemID should be the canonical ID from LookupSystem")
	assert.Equal(t, []string{"/emulation/roms/ps2", "/mnt/games/ps2"}, l.Folders,
		"MediaDirs should map directly to Folders")
	assert.Equal(t, []string{".iso", ".bin", ".mdf", ".chd"}, l.Extensions,
		"FileExts should be dot-prefixed and lowercased")
	assert.NotNil(t, l.Launch, "Launch function should be set")
}

func TestFormatExtensions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "empty list",
			input:    []string{},
			expected: []string{},
		},
		{
			name:     "extensions with dots",
			input:    []string{".mp4", ".mkv"},
			expected: []string{".mp4", ".mkv"},
		},
		{
			name:     "extensions without dots",
			input:    []string{"mp4", "mkv"},
			expected: []string{".mp4", ".mkv"},
		},
		{
			name:     "mixed extensions",
			input:    []string{".mp4", "mkv", ".AVI"},
			expected: []string{".mp4", ".mkv", ".avi"},
		},
		{
			name:     "empty strings are filtered",
			input:    []string{".mp4", "", ".mkv"},
			expected: []string{".mp4", ".mkv"},
		},
		{
			name:     "whitespace is trimmed",
			input:    []string{" .mp4 ", " mkv "},
			expected: []string{".mp4", ".mkv"},
		},
		{
			name:     "uppercase converted to lowercase",
			input:    []string{".MP4", ".MKV", ".AVI"},
			expected: []string{".mp4", ".mkv", ".avi"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := formatExtensions(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
