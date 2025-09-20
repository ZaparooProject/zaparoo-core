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

package igdb

import (
	"strconv"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/scraper"
)


// PlatformMapper maps zaparoo system IDs to IGDB platform IDs
// Based on Batocera EmulationStation's IGDB platform mappings
type PlatformMapper struct {
	igdbPlatformMap map[string]int
}

// NewPlatformMapper creates a new platform mapper with IGDB mappings
func NewPlatformMapper() *PlatformMapper {
	// Build IGDB platform map from central definitions
	igdbPlatformMap := make(map[string]int)
	for zaparooID, platformIDs := range scraper.PlatformDefinitions {
		if platformIDs.IGDB != 0 {
			igdbPlatformMap[zaparooID] = platformIDs.IGDB
		}
	}


	return &PlatformMapper{
		igdbPlatformMap: igdbPlatformMap,
	}
}

// MapToScraperPlatform maps a zaparoo system ID to IGDB platform ID
func (pm *PlatformMapper) MapToScraperPlatform(systemID string) (string, bool) {
	if platformID, exists := pm.igdbPlatformMap[systemID]; exists {
		return strconv.Itoa(platformID), true
	}
	return "", false
}

// GetSupportedSystems returns a list of all supported system IDs
func (pm *PlatformMapper) GetSupportedSystems() []string {
	systems := make([]string, 0, len(pm.igdbPlatformMap))
	for systemID := range pm.igdbPlatformMap {
		systems = append(systems, systemID)
	}
	return systems
}

// GetIGDBPlatformID returns the specific IGDB platform ID for a system
func (pm *PlatformMapper) GetIGDBPlatformID(systemID string) (int, bool) {
	platformID, exists := pm.igdbPlatformMap[systemID]
	return platformID, exists
}
