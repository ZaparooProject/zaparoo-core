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
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esde"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCoreDefinitionsResolve(t *testing.T) {
	t.Parallel()

	defs := CoreDefinitions()
	assert.GreaterOrEqual(t, len(defs), 60)

	seenFolders := make(map[string]struct{}, len(defs))
	for _, def := range defs {
		info, ok := esde.LookupByFolderName(def.ESFolder)
		require.True(t, ok, "missing ES-DE folder: %s", def.ESFolder)
		assert.Equal(t, info.SystemID, def.SystemID, "system mismatch for %s", def.ESFolder)
		_, err := systemdefs.LookupSystem(def.SystemID)
		require.NoError(t, err, "unknown system: %s", def.SystemID)
		_, duplicate := seenFolders[strings.ToLower(def.ESFolder)]
		assert.False(t, duplicate, "duplicate ES-DE folder: %s", def.ESFolder)
		seenFolders[strings.ToLower(def.ESFolder)] = struct{}{}

		for _, profile := range []Profile{ProfileApplianceARM, ProfileDesktop} {
			core, enabled := selectedCore(&def, profile)
			if !enabled {
				continue
			}
			_, normalizeErr := normalizeCoreFilename(core)
			require.NoError(t, normalizeErr, "%s core for %s", profile, def.ESFolder)
		}
	}
}

func TestCoreLaunchesProfiles(t *testing.T) {
	t.Parallel()

	desktop := CoreLaunches(ProfileDesktop)
	appliance := CoreLaunches(ProfileApplianceARM)
	assert.Greater(t, len(desktop), len(appliance))

	excluded := []string{"n64", "nds", "saturn", "dreamcast", "psp"}
	for _, folder := range excluded {
		_, ok := CoreLaunchForFolder(ProfileApplianceARM, folder)
		assert.False(t, ok, "appliance profile should exclude %s", folder)
		_, ok = CoreLaunchForFolder(ProfileDesktop, folder)
		assert.True(t, ok, "desktop profile should include %s", folder)
	}

	psx, ok := CoreLaunchForFolder(ProfileApplianceARM, "psx")
	require.True(t, ok)
	assert.Equal(t, "pcsx_rearmed_libretro.so", psx.Core)

	desktopPSX, ok := CoreLaunchForFolder(ProfileDesktop, "psx")
	require.True(t, ok)
	assert.Equal(t, "mednafen_psx_hw_libretro.so", desktopPSX.Core)
}

func TestCorePoliciesMatchSelectedNonCommercialCores(t *testing.T) {
	t.Parallel()

	nonCommercial := map[string]struct{}{
		"fbneo": {}, "genesis_plus_gx": {}, "opera": {}, "picodrive": {}, "snes9x": {},
	}
	for _, def := range CoreDefinitions() {
		for _, profile := range []Profile{ProfileApplianceARM, ProfileDesktop} {
			core, enabled := selectedCore(&def, profile)
			if !enabled {
				continue
			}
			policy, ok := CorePolicyForFolder(profile, def.ESFolder)
			require.True(t, ok)
			_, expectedNonCommercial := nonCommercial[core]
			if expectedNonCommercial {
				assert.Equal(t, PolicyNonCommercial, policy, "%s/%s", profile, def.ESFolder)
			}
		}
	}
}

func TestCoreLaunchesUseMiSTerStyleDefaultAndAlternateLaunchers(t *testing.T) {
	t.Parallel()

	var snesLaunchers []CoreLaunch
	for _, launch := range CoreLaunches(ProfileDesktop) {
		if launch.SystemID == systemdefs.SystemSNES {
			snesLaunchers = append(snesLaunchers, launch)
		}
	}

	require.Len(t, snesLaunchers, 2)
	assert.Equal(t, "RetroArchSNES9x", snesLaunchers[0].ID)
	assert.True(t, snesLaunchers[0].Scan)
	assert.Equal(t, "RetroArchBSNES", snesLaunchers[1].ID)
	assert.True(t, snesLaunchers[1].Scan)
	assert.Equal(t, snesLaunchers[0].Folders, snesLaunchers[1].Folders)
	assert.Equal(t, snesLaunchers[0].Extensions, snesLaunchers[1].Extensions)
}

func TestCoreLaunchIDsUniquePerProfile(t *testing.T) {
	t.Parallel()

	for _, profile := range []Profile{ProfileApplianceARM, ProfileDesktop} {
		seen := make(map[string]struct{})
		for _, launch := range CoreLaunches(profile) {
			_, exists := seen[launch.ID]
			assert.False(t, exists, "duplicate launcher ID %s in %s", launch.ID, profile)
			seen[launch.ID] = struct{}{}
		}
	}
}

func TestCoreDefinitionsReturnsCopy(t *testing.T) {
	t.Parallel()

	defs := CoreDefinitions()
	require.NotEmpty(t, defs)
	original := coreDefinitions[0].DefaultCore
	defs[0].DefaultCore = "changed"
	if defs[0].PerProfileCore != nil {
		defs[0].PerProfileCore[ProfileDesktop] = "changed"
	}
	assert.Equal(t, original, coreDefinitions[0].DefaultCore)
}
