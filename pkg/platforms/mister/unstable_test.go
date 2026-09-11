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

package mister

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/cores"
	platformshared "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func findLauncher(launchers []platforms.Launcher, id string) *platforms.Launcher {
	for i := range launchers {
		if launchers[i].ID == id {
			return &launchers[i]
		}
	}
	return nil
}

func TestUnstableLaunchersExist(t *testing.T) {
	t.Parallel()

	pl := NewPlatform()
	launchers := CreateLaunchers(pl)

	cases := []struct {
		id       string
		systemID string
	}{
		{"UnstableSNES", "SNES"},
		{"UnstableNES", "NES"},
		// The NES nightly also runs FDS, so FDS gets its own launcher.
		{"UnstableFDS", "FDS"},
		{"UnstablePSX", "PSX"},
		{"UnstableSaturn", "Saturn"},
		{"UnstableDualRAMSaturn", "Saturn"},
		{"UnstableGenesis", "Genesis"},
		{"UnstableGBA", "GBA"},
		{"UnstableGameboy", "Gameboy"},
		{"UnstableGameboyColor", "GameboyColor"},
		{"UnstableMegaCD", "MegaCD"},
		{"UnstableNeoGeo", "NeoGeo"},
		{"UnstableAmigaCD32", "AmigaCD32"},
		{"UnstableZXSpectrum", "ZXSpectrum"},
		{"Unstableao486", "ao486"},
	}

	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()

			found := findLauncher(launchers, tc.id)
			require.NotNil(t, found, "%s launcher should exist", tc.id)
			assert.Equal(t, tc.systemID, found.SystemID,
				"%s must inherit slots from %s", tc.id, tc.systemID)
			assert.Contains(t, found.Groups, platformshared.LauncherGroupUnstable)
		})
	}
}

// TestUnstableLaunchersSkipArcadeAndNonSystemCores guards the curation in
// unstableNightlyCores: arcade nightlies launch through MRAs that name their
// own RBF, and cores without a Zaparoo system have nothing to attach to.
func TestUnstableLaunchersSkipArcadeAndNonSystemCores(t *testing.T) {
	t.Parallel()

	pl := NewPlatform()
	launchers := CreateLaunchers(pl)

	for _, id := range []string{
		"UnstableArcade", "UnstableArcadeGalaga", "UnstableArcadeTecmo",
		"UnstableAtariST", "UnstablePC88", "UnstableSTV", "UnstableMenu",
	} {
		assert.Nil(t, findLauncher(launchers, id), "%s should not exist", id)
	}

	// The Minimig nightly serves Amiga too, but the Amiga launcher resolves
	// AmigaVision listing files and virtual MGL paths that a generic alt core
	// launcher would mishandle.
	assert.Nil(t, findLauncher(launchers, "UnstableAmiga"),
		"Amiga must keep its bespoke launcher")
}

func TestUnstableLauncherRegistersDatedPattern(t *testing.T) {
	t.Parallel()

	pl := NewPlatform()
	CreateLaunchers(pl)

	assert.Equal(t,
		[]string{"SNES_unstable_<date>_<hash>"},
		cores.GlobalRBFCache.AltCorePaths("UnstableSNES"),
	)
	assert.Equal(t,
		[]string{"Saturn_DualSDRAM_unstable_<date>_<hash>"},
		cores.GlobalRBFCache.AltCorePaths("UnstableDualRAMSaturn"),
	)
	// The Genesis system's stock core is MegaDrive; Genesis is the legacy name
	// the same core shipped under, kept as a fallback candidate.
	assert.Equal(t,
		[]string{"MegaDrive_unstable_<date>_<hash>", "Genesis_unstable_<date>_<hash>"},
		cores.GlobalRBFCache.AltCorePaths("UnstableGenesis"),
	)
}

func TestUnstableDualRAMSaturnIsInBothGroups(t *testing.T) {
	t.Parallel()

	pl := NewPlatform()
	launchers := CreateLaunchers(pl)

	found := findLauncher(launchers, "UnstableDualRAMSaturn")
	require.NotNil(t, found)
	assert.Equal(t,
		[]string{platformshared.LauncherGroupUnstable, platformshared.LauncherGroupDualRAM},
		found.Groups,
		"the family group must come first so Groups[0] identifies the distribution",
	)
}

func TestUnstableLauncherResolvesNightlyRBF(t *testing.T) {
	withRBFCache(t, []cores.RBFInfo{
		{
			Path:      "/media/fat/_Console/SNES_20260311.rbf",
			Filename:  "SNES_20260311.rbf",
			ShortName: "SNES",
			MglName:   "_Console/SNES",
		},
		{
			Path:      "/media/fat/_Unstable/SNES_unstable_20260824_1919a7.rbf",
			Filename:  "SNES_unstable_20260824_1919a7.rbf",
			ShortName: "SNES_unstable_20260824_1919a7",
			MglName:   "_Unstable/SNES_unstable_20260824_1919a7",
		},
	})

	pl := NewPlatform()
	CreateLaunchers(pl)

	info, ok := cores.GlobalRBFCache.ResolveLauncherStrict(nil, "UnstableSNES", "SNES")
	require.True(t, ok)
	assert.Equal(t, "_Unstable/SNES_unstable_20260824_1919a7", info.MglName)
}

func TestUnstableLauncherPrefersNewestNightly(t *testing.T) {
	withRBFCache(t, []cores.RBFInfo{
		{
			Path:      "/media/fat/_Unstable/SNES_unstable_20260101_aaaaaa.rbf",
			Filename:  "SNES_unstable_20260101_aaaaaa.rbf",
			ShortName: "SNES_unstable_20260101_aaaaaa",
			MglName:   "_Unstable/SNES_unstable_20260101_aaaaaa",
		},
		{
			Path:      "/media/fat/_Unstable/SNES_unstable_20260824_1919a7.rbf",
			Filename:  "SNES_unstable_20260824_1919a7.rbf",
			ShortName: "SNES_unstable_20260824_1919a7",
			MglName:   "_Unstable/SNES_unstable_20260824_1919a7",
		},
	})

	pl := NewPlatform()
	CreateLaunchers(pl)

	info, ok := cores.GlobalRBFCache.ResolveLauncherStrict(nil, "UnstableSNES", "SNES")
	require.True(t, ok)
	assert.Equal(t, "_Unstable/SNES_unstable_20260824_1919a7", info.MglName)
}

// TestUnstableSaturnIgnoresDualSDRAMNightly covers the token matcher's
// part-count rule: the plain Saturn nightly must not resolve to the dual-SDRAM
// build, which needs two SDRAM boards to run at all.
func TestUnstableSaturnIgnoresDualSDRAMNightly(t *testing.T) {
	withRBFCache(t, []cores.RBFInfo{
		{
			Path:      "/media/fat/_Console/Saturn_20260311.rbf",
			Filename:  "Saturn_20260311.rbf",
			ShortName: "Saturn",
			MglName:   "_Console/Saturn",
		},
		{
			Path:      "/media/fat/_Unstable/Saturn_DualSDRAM_unstable_20260817_13ea1a.rbf",
			Filename:  "Saturn_DualSDRAM_unstable_20260817_13ea1a.rbf",
			ShortName: "Saturn_DualSDRAM_unstable_20260817_13ea1a",
			MglName:   "_Unstable/Saturn_DualSDRAM_unstable_20260817_13ea1a",
		},
	})

	pl := NewPlatform()
	CreateLaunchers(pl)

	_, ok := cores.GlobalRBFCache.ResolveLauncherStrict(nil, "UnstableSaturn", "Saturn")
	assert.False(t, ok, "plain Saturn nightly must not match the dual-SDRAM build")

	info, ok := cores.GlobalRBFCache.ResolveLauncherStrict(nil, "UnstableDualRAMSaturn", "Saturn")
	require.True(t, ok)
	assert.Equal(t, "_Unstable/Saturn_DualSDRAM_unstable_20260817_13ea1a", info.MglName)
}

func TestUnstableLauncherUnavailableWithoutNightly(t *testing.T) {
	withRBFCache(t, []cores.RBFInfo{
		{
			Path:      "/media/fat/_Console/SNES_20260311.rbf",
			Filename:  "SNES_20260311.rbf",
			ShortName: "SNES",
			MglName:   "_Console/SNES",
		},
	})

	pl := NewPlatform()
	CreateLaunchers(pl)

	launchers := []platforms.Launcher{{ID: "UnstableSNES", SystemID: "SNES"}}
	setCoreAvailability(launchers)
	require.NotNil(t, launchers[0].Availability)

	err := launchers[0].Availability(nil)
	require.Error(t, err, "an uninstalled nightly must not report available")
	assert.Contains(t, err.Error(), "SNES_unstable_<date>_<hash>")
}
