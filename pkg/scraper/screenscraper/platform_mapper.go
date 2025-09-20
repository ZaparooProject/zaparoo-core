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

package screenscraper

import (
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/scraper"
)


// PlatformMapper handles mapping between zaparoo system IDs and ScreenScraper platform IDs
// Based on Batocera EmulationStation's screenscraper_platformid_map
type PlatformMapper struct {
	// ScreenScraper-specific platform ID mappings
	screenScraperMappings map[string]string
}

// NewPlatformMapper creates a new ScreenScraper platform mapper
func NewPlatformMapper() *PlatformMapper {
	// Build ScreenScraper platform map from central definitions
	screenScraperMappings := make(map[string]string)
	for zaparooID, platformIDs := range scraper.PlatformDefinitions {
		if platformIDs.ScreenScraper != "" {
			screenScraperMappings[zaparooID] = platformIDs.ScreenScraper
		}
	}

	return &PlatformMapper{
		screenScraperMappings: screenScraperMappings,
	}
}

// MapToScraperPlatform maps a zaparoo system ID to a ScreenScraper platform ID
func (pm *PlatformMapper) MapToScraperPlatform(systemID string) (string, bool) {
	platformID, exists := pm.screenScraperMappings[systemID]
	return platformID, exists
}

// MapFromScraperPlatform maps a ScreenScraper platform ID back to zaparoo system ID
func (pm *PlatformMapper) MapFromScraperPlatform(scraperPlatformID string) (string, bool) {
	for systemID, platformID := range pm.screenScraperMappings {
		if platformID == scraperPlatformID {
			return systemID, true
		}
	}
	return "", false
}

// GetSupportedSystems returns all zaparoo system IDs supported by ScreenScraper
func (pm *PlatformMapper) GetSupportedSystems() []string {
	systems := make([]string, 0, len(pm.screenScraperMappings))
	for systemID := range pm.screenScraperMappings {
		systems = append(systems, systemID)
	}
	return systems
}

// GetScreenScraperPlatformID returns the numeric platform ID for ScreenScraper
func (pm *PlatformMapper) GetScreenScraperPlatformID(systemID string) (string, bool) {
	platformID, exists := pm.screenScraperMappings[systemID]
	return platformID, exists
}
