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

package gamedatabase

import (
	"slices"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/arcadenames"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
)

// arcadeBundle holds every arcade manufacturer's rows. Upstream files them by
// manufacturer, but a library files a set by where the user keeps it, so one
// bundle serves every arcade system.
const arcadeBundle = "arcade"

// source names the bundles that describe one system's games. secondary
// bundles are read only for titles the primary ones did not match: a system
// that also runs another system's games (Game Boy Color running Game Boy
// cartridges) has both in its folder.
type source struct {
	primary   []string
	secondary []string
}

// consoleSources maps a console system to its bundles. Several systems share
// a bundle where Core indexes one platform's games under more than one system
// (a 2-player or MSU variant of the same core), and one upstream file covers
// several where upstream groups hardware Core separates.
var consoleSources = map[string]source{
	systemdefs.SystemNES:    {primary: []string{"nes"}},
	systemdefs.SystemFDS:    {primary: []string{"fds"}},
	systemdefs.SystemSufami: {primary: []string{"sufami"}},

	systemdefs.SystemSNES:     {primary: []string{"snes", "satellaview"}},
	systemdefs.SystemSNESMSU1: {primary: []string{"snes", "satellaview"}},

	systemdefs.SystemGameboy:      {primary: []string{"gameboy"}, secondary: []string{"gameboycolor"}},
	systemdefs.SystemGameboy2P:    {primary: []string{"gameboy"}, secondary: []string{"gameboycolor"}},
	systemdefs.SystemSuperGameboy: {primary: []string{"gameboy"}, secondary: []string{"gameboycolor"}},
	systemdefs.SystemSGBMSU1:      {primary: []string{"gameboy"}, secondary: []string{"gameboycolor"}},
	systemdefs.SystemGameboyColor: {primary: []string{"gameboycolor"}, secondary: []string{"gameboy"}},

	systemdefs.SystemGBA:   {primary: []string{"gba"}},
	systemdefs.SystemGBA2P: {primary: []string{"gba"}},

	systemdefs.SystemNintendo64: {primary: []string{"n64", "n64dd"}},
	systemdefs.SystemVirtualBoy: {primary: []string{"virtualboy"}},

	systemdefs.SystemTurboGrafx16:   {primary: []string{"pcengine"}},
	systemdefs.SystemSuperGrafx:     {primary: []string{"pcengine"}},
	systemdefs.SystemTurboGrafx16CD: {primary: []string{"pcenginecd"}},
	systemdefs.SystemPCFX:           {primary: []string{"pcfx"}},

	systemdefs.SystemSG1000:       {primary: []string{"sg1000"}},
	systemdefs.SystemMultivision:  {primary: []string{"sg1000"}},
	systemdefs.SystemMasterSystem: {primary: []string{"mastersystem"}},
	systemdefs.SystemGenesis:      {primary: []string{"megadrive"}},
	systemdefs.SystemGenesisMSU:   {primary: []string{"megadrive"}},
	systemdefs.SystemGenesisPlus:  {primary: []string{"megadrive"}},
	systemdefs.SystemMegaCD:       {primary: []string{"megacd"}},
	systemdefs.SystemSega32X:      {primary: []string{"sega32x"}},
	systemdefs.SystemGameGear:     {primary: []string{"gamegear"}},
	systemdefs.SystemGameGear2P:   {primary: []string{"gamegear"}},
	systemdefs.SystemSaturn:       {primary: []string{"saturn"}},

	systemdefs.SystemNeoGeoPocket:      {primary: []string{"neogeopocket"}},
	systemdefs.SystemNeoGeoPocketColor: {primary: []string{"neogeopocket"}},

	systemdefs.SystemLaserActive: {primary: []string{"laseractive"}},
}

// sourceFor returns the bundles for a system. ok is false for a system the
// data does not cover.
func sourceFor(systemID string) (src source, ok bool) {
	if src, ok = consoleSources[systemID]; ok {
		return src, true
	}
	if arcadenames.IsArcadeSystem(systemID) {
		return source{primary: []string{arcadeBundle}}, true
	}
	return source{}, false
}

// SupportedSystems lists every system the data covers, sorted.
func SupportedSystems() []string {
	ids := make([]string, 0, len(consoleSources)+32)
	for id := range consoleSources {
		ids = append(ids, id)
	}
	for _, system := range systemdefs.AllSystems() {
		if _, console := consoleSources[system.ID]; !console && arcadenames.IsArcadeSystem(system.ID) {
			ids = append(ids, system.ID)
		}
	}
	slices.Sort(ids)
	return ids
}
