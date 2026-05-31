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
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLookupSystemDefaultsFuzzyMatching(t *testing.T) {
	t.Parallel()

	// Create instance with system defaults using aliases
	cfg := &Instance{
		vals: Values{
			Systems: Systems{
				Default: []SystemsDefault{
					{
						System:   "megadrive", // Alias for Genesis
						Launcher: "retroarch",
					},
					{
						System:   "N64", // Alias for Nintendo64
						Launcher: "mupen64plus",
					},
					{
						System:     "playstation", // Alias for PSX
						Launcher:   "duckstation",
						BeforeExit: "cleanup.sh",
					},
				},
			},
		},
	}

	tests := []struct {
		name             string
		systemID         string
		expectedLauncher string
		expectFound      bool
	}{
		{
			name:             "canonical ID matches alias in config (Genesis via megadrive)",
			systemID:         "Genesis",
			expectFound:      true,
			expectedLauncher: "retroarch",
		},
		{
			name:             "canonical ID matches alias in config (Nintendo64 via N64)",
			systemID:         "Nintendo64",
			expectFound:      true,
			expectedLauncher: "mupen64plus",
		},
		{
			name:             "canonical ID matches alias in config (PSX via playstation)",
			systemID:         "PSX",
			expectFound:      true,
			expectedLauncher: "duckstation",
		},
		{
			name:        "system not in defaults returns false",
			systemID:    "NES",
			expectFound: false,
		},
		{
			name:        "unknown system ID returns false",
			systemID:    "UnknownSystem",
			expectFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, found := cfg.LookupSystemDefaults(tt.systemID)
			assert.Equal(t, tt.expectFound, found)
			if tt.expectFound {
				assert.Equal(t, tt.expectedLauncher, result.Launcher)
			}
		})
	}
}

func TestLookupSystemDefaultsWithInvalidConfig(t *testing.T) {
	t.Parallel()

	// Create instance with invalid system IDs in the defaults
	cfg := &Instance{
		vals: Values{
			Systems: Systems{
				Default: []SystemsDefault{
					{
						System:   "InvalidSystemID",
						Launcher: "should-not-match",
					},
					{
						System:   "AnotherBadID",
						Launcher: "also-should-not-match",
					},
					{
						System:   "Genesis", // One valid entry
						Launcher: "valid-launcher",
					},
				},
			},
		},
	}

	// Should still match Genesis despite invalid entries
	result, found := cfg.LookupSystemDefaults("Genesis")
	assert.True(t, found)
	assert.Equal(t, "valid-launcher", result.Launcher)

	// Invalid entries should not cause matches
	_, found = cfg.LookupSystemDefaults("InvalidSystemID")
	assert.False(t, found)
}

func TestLookupSystemDefaultsCaseInsensitive(t *testing.T) {
	t.Parallel()

	// Create instance with mixed case system IDs
	cfg := &Instance{
		vals: Values{
			Systems: Systems{
				Default: []SystemsDefault{
					{
						System:   "GENESIS", // Uppercase canonical ID
						Launcher: "uppercase-launcher",
					},
				},
			},
		},
	}

	// Should match regardless of case in the lookup
	result, found := cfg.LookupSystemDefaults("Genesis")
	assert.True(t, found)
	assert.Equal(t, "uppercase-launcher", result.Launcher)

	result, found = cfg.LookupSystemDefaults("genesis")
	assert.True(t, found)
	assert.Equal(t, "uppercase-launcher", result.Launcher)
}

func TestLookupSystemDefaultsReturnsCorrectFields(t *testing.T) {
	t.Parallel()

	cfg := &Instance{
		vals: Values{
			Systems: Systems{
				Default: []SystemsDefault{
					{
						System:     "SMS", // Alias for MasterSystem
						Launcher:   "retroarch",
						BeforeExit: "echo 'goodbye'",
					},
				},
			},
		},
	}

	result, found := cfg.LookupSystemDefaults("MasterSystem")
	assert.True(t, found)
	assert.Equal(t, "SMS", result.System) // Original config value preserved
	assert.Equal(t, "retroarch", result.Launcher)
	assert.Equal(t, "echo 'goodbye'", result.BeforeExit)
}

func TestSetSystemDefaultsReplacesAllEntries(t *testing.T) {
	t.Parallel()

	cfg := &Instance{
		vals: Values{
			Systems: Systems{
				Default: []SystemsDefault{
					{System: "Genesis", Launcher: "retroarch"},
				},
			},
		},
	}

	cfg.SetSystemDefaults([]SystemsDefault{
		{System: "SNES", Launcher: "snes9x", BeforeExit: "echo bye"},
		{System: "N64", Launcher: "mupen64plus"},
	})

	got := cfg.SystemDefaults()
	assert.Len(t, got, 2)
	assert.Equal(t, "SNES", got[0].System)
	assert.Equal(t, "snes9x", got[0].Launcher)
	assert.Equal(t, "echo bye", got[0].BeforeExit)
	assert.Equal(t, "N64", got[1].System)
	assert.Equal(t, "mupen64plus", got[1].Launcher)
}

func TestSetSystemDefaultsClearsList(t *testing.T) {
	t.Parallel()

	cfg := &Instance{
		vals: Values{
			Systems: Systems{
				Default: []SystemsDefault{
					{System: "Genesis", Launcher: "retroarch"},
				},
			},
		},
	}

	cfg.SetSystemDefaults([]SystemsDefault{})
	assert.Empty(t, cfg.SystemDefaults())
}

func TestSystemDefaults_SaveLoadRoundTrip(t *testing.T) {
	t.Parallel()

	memFs := afero.NewMemMapFs()
	cfg, err := NewConfigWithFs("/config", BaseDefaults, memFs)
	require.NoError(t, err)

	cfg.SetSystemDefaults([]SystemsDefault{
		{System: "Genesis", Launcher: "retroarch"},
		{System: "SNES", Launcher: "snes9x", BeforeExit: "echo bye"},
		{System: "N64"},
	})

	err = cfg.Save()
	require.NoError(t, err)

	err = cfg.Load()
	require.NoError(t, err)

	got := cfg.SystemDefaults()
	require.Len(t, got, 3)

	assert.Equal(t, "Genesis", got[0].System)
	assert.Equal(t, "retroarch", got[0].Launcher)
	assert.Empty(t, got[0].BeforeExit)

	assert.Equal(t, "SNES", got[1].System)
	assert.Equal(t, "snes9x", got[1].Launcher)
	assert.Equal(t, "echo bye", got[1].BeforeExit)

	assert.Equal(t, "N64", got[2].System)
	assert.Empty(t, got[2].Launcher)
	assert.Empty(t, got[2].BeforeExit)
}
