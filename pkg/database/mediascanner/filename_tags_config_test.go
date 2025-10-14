// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

package mediascanner

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/require"
)

func TestGetPathFragments_FilenameTagsConfig(t *testing.T) {
	// Test with filename tags enabled (default behavior)
	fs := helpers.NewMemoryFS()
	configDir := t.TempDir()

	cfgEnabled, err := helpers.NewTestConfig(fs, configDir)
	require.NoError(t, err)

	// Test with filename tags disabled
	cfgDisabled, err := helpers.NewTestConfig(fs, configDir)
	require.NoError(t, err)
	// Manually set the disabled value since we can't pass custom defaults easily
	cfgDisabled.SetFilenameTags(false)

	testPath := "/games/nes/Super Mario Bros (USA, Europe) (Rev 1).nes"

	// Test with filename tags enabled
	fragmentsEnabled := GetPathFragments(cfgEnabled, testPath, false, false)
	require.NotEmpty(t, fragmentsEnabled.Tags, "filename tags should be extracted when enabled")
	require.Contains(t, fragmentsEnabled.Tags, "rev:1", "revision tag should be extracted")
	// Multi-region tags are now properly parsed into individual regions
	require.Contains(t, fragmentsEnabled.Tags, "region:us", "USA region tag should be extracted")
	require.Contains(t, fragmentsEnabled.Tags, "region:eu", "Europe region tag should be extracted")
	require.Contains(t, fragmentsEnabled.Tags, "lang:en", "English language tag should be extracted")

	// Test with filename tags disabled
	fragmentsDisabled := GetPathFragments(cfgDisabled, testPath, false, false)
	require.Empty(t, fragmentsDisabled.Tags, "filename tags should not be extracted when disabled")

	// Test with nil config (should behave as enabled for backward compatibility)
	fragmentsNil := GetPathFragments(nil, testPath, false, false)
	t.Logf("Tags with nil config: %v", fragmentsNil.Tags)
	require.NotEmpty(t, fragmentsNil.Tags, "filename tags should be extracted when config is nil")
	require.Contains(t, fragmentsNil.Tags, "rev:1", "revision tag should be extracted when config is nil")
}

func TestGetPathFragments_CacheKeyWithConfig(t *testing.T) {
	fs := helpers.NewMemoryFS()
	configDir := t.TempDir()

	cfgEnabled, err := helpers.NewTestConfig(fs, configDir)
	require.NoError(t, err)

	cfgDisabled, err := helpers.NewTestConfig(fs, configDir)
	require.NoError(t, err)
	cfgDisabled.SetFilenameTags(false)

	testPath := "/games/nes/Super Mario Bros (USA).nes"

	// Get fragments with enabled config
	fragments1 := GetPathFragments(cfgEnabled, testPath, false, false)
	require.NotEmpty(t, fragments1.Tags, "should have tags when enabled")

	// Get fragments with disabled config - should return different result
	fragments2 := GetPathFragments(cfgDisabled, testPath, false, false)
	require.Empty(t, fragments2.Tags, "should have no tags when disabled")

	// Verify cache works correctly by calling again with same configs
	fragments1Again := GetPathFragments(cfgEnabled, testPath, false, false)
	fragments2Again := GetPathFragments(cfgDisabled, testPath, false, false)

	require.Len(t, fragments1Again.Tags, len(fragments1.Tags), "cached result should be same for enabled config")
	require.Empty(t, fragments2Again.Tags, "cached result should be same for disabled config")
}
