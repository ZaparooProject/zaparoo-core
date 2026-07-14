//go:build linux

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

package cores

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRBFPath_StripsOnlyOfficialDateSuffix(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	tests := []struct {
		name          string
		relPath       string
		wantShortName string
		wantMglName   string
	}{
		{
			name:          "official dated core",
			relPath:       filepath.Join("_Console", "GBA_20260528.rbf"),
			wantShortName: "GBA",
			wantMglName:   filepath.Join("_Console", "GBA"),
		},
		{
			name:          "underscore core strips date only",
			relPath:       filepath.Join("_Console", "Saturn_DS_20230410.rbf"),
			wantShortName: "Saturn_DS",
			wantMglName:   filepath.Join("_Console", "Saturn_DS"),
		},
		{
			name:          "llapi dated core strips date only",
			relPath:       filepath.Join("_LLAPI", "GBA_LLAPI_20251205.rbf"),
			wantShortName: "GBA_LLAPI",
			wantMglName:   filepath.Join("_LLAPI", "GBA_LLAPI"),
		},
		{
			name:          "undated alt core",
			relPath:       filepath.Join("_Console", "PSX2XCPU.rbf"),
			wantShortName: "PSX2XCPU",
			wantMglName:   filepath.Join("_Console", "PSX2XCPU"),
		},
		{
			name:          "db9 fork preserves full name",
			relPath:       filepath.Join("_Console", "GBA_20260528_ceb4a49_DB9.rbf"),
			wantShortName: "GBA_20260528_ceb4a49_DB9",
			wantMglName:   filepath.Join("_Console", "GBA_20260528_ceb4a49_DB9"),
		},
		{
			name:          "megadrive db9 fork preserves full name",
			relPath:       filepath.Join("_Console", "MegaDrive_20260528_fef1285_DB9.rbf"),
			wantShortName: "MegaDrive_20260528_fef1285_DB9",
			wantMglName:   filepath.Join("_Console", "MegaDrive_20260528_fef1285_DB9"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := parseRBFPathAt(root, filepath.Join(root, tc.relPath))

			assert.Equal(t, filepath.Base(tc.relPath), got.Filename)
			assert.Equal(t, tc.wantShortName, got.ShortName)
			assert.Equal(t, tc.wantMglName, got.MglName)
		})
	}
}

func TestShallowScanRBF_IncludesRetroAchievementsCores(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	raCoreDir := filepath.Join(root, "_RA_Cores", "Cores")
	require.NoError(t, os.MkdirAll(raCoreDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(raCoreDir, "NES.rbf"), []byte{}, 0o600))

	rbfs, err := shallowScanRBFAt(root)
	require.NoError(t, err)

	expectedMglName := filepath.Join("_RA_Cores", "Cores", "NES")
	var found *RBFInfo
	for i := range rbfs {
		if rbfs[i].MglName == expectedMglName {
			found = &rbfs[i]
			break
		}
	}

	require.NotNil(t, found, "RA core should be included in shallow RBF scan")
	assert.Equal(t, "NES", found.ShortName)
	assert.Equal(t, "NES.rbf", found.Filename)
}

func TestShallowScanRBF_IncludesLightGunSindenCores(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	lightGunDir := filepath.Join(root, "Light Gun")
	require.NoError(t, os.MkdirAll(lightGunDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(lightGunDir, "NES-Sinden.rbf"), []byte{}, 0o600))

	rbfs, err := shallowScanRBFAt(root)
	require.NoError(t, err)

	expectedMglName := filepath.Join("Light Gun", "NES-Sinden")
	var found *RBFInfo
	for i := range rbfs {
		if rbfs[i].MglName == expectedMglName {
			found = &rbfs[i]
			break
		}
	}

	require.NotNil(t, found, "Light Gun Sinden core should be included in shallow RBF scan")
	assert.Equal(t, "NES-Sinden", found.ShortName)
	assert.Equal(t, "NES-Sinden.rbf", found.Filename)
}
