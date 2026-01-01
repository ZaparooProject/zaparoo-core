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

package testdata

import (
	"encoding/xml"
	"fmt"
	"os"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
)

// DATFile represents a parsed No-Intro or TOSEC DAT file
type DATFile struct {
	XMLName xml.Name   `xml:"datafile"`
	Header  DATHeader  `xml:"header"`
	Games   []DATGame  `xml:"game"`
}

// DATHeader contains metadata about the DAT file
type DATHeader struct {
	Name        string `xml:"name"`
	Description string `xml:"description"`
	Version     string `xml:"version"`
	Author      string `xml:"author"`
	Homepage    string `xml:"homepage"`
	URL         string `xml:"url"`
}

// DATGame represents a single game entry in a DAT file
type DATGame struct {
	Name        string   `xml:"name,attr"`
	ID          string   `xml:"id,attr"`
	Description string   `xml:"description"`
	ROMs        []DATROM `xml:"rom"`
}

// DATROM represents a ROM file within a game entry
type DATROM struct {
	Name   string `xml:"name,attr"`
	Size   int64  `xml:"size,attr"`
	CRC    string `xml:"crc,attr"`
	MD5    string `xml:"md5,attr"`
	SHA1   string `xml:"sha1,attr"`
	SHA256 string `xml:"sha256,attr"`
}

// ParseDATFile parses a No-Intro or TOSEC DAT file and returns a DATFile struct
func ParseDATFile(filePath string) (*DATFile, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read DAT file: %w", err)
	}

	var dat DATFile
	if err := xml.Unmarshal(data, &dat); err != nil {
		return nil, fmt.Errorf("failed to parse XML: %w", err)
	}

	if len(dat.Games) == 0 {
		return nil, fmt.Errorf("no games found in DAT file")
	}

	return &dat, nil
}

// ExtractGameNames extracts all game names from a DAT file with metadata stripped
func ExtractGameNames(dat *DATFile) []string {
	names := make([]string, 0, len(dat.Games))

	for _, game := range dat.Games {
		name := game.Name
		if name == "" {
			name = game.Description
		}

		// Strip metadata brackets like (USA), [!], etc.
		name = slugs.StripMetadataBrackets(name)
		name = strings.TrimSpace(name)

		if name != "" {
			names = append(names, name)
		}
	}

	return names
}

// MatchSystemID attempts to match a DAT file name to a Zaparoo system ID
// Returns the matched system ID or an error if no match is found
func MatchSystemID(datName string) (string, error) {
	datName = strings.TrimSpace(datName)
	if datName == "" {
		return "", fmt.Errorf("empty DAT name")
	}

	// Strategy 1: Try direct lookup (handles cases like "Gameboy", "GBA", etc.)
	if system, err := systemdefs.LookupSystem(datName); err == nil {
		return system.ID, nil
	}

	// Strategy 2: Extract keywords and try matching
	// Handle common DAT name patterns like "Nintendo - Game Boy" or "Sega - Genesis"
	keywords := extractKeywords(datName)

	// Try matching combined keywords (e.g., "Game Boy" → "Gameboy")
	for i := 0; i < len(keywords); i++ {
		for j := i + 1; j <= len(keywords); j++ {
			combined := strings.Join(keywords[i:j], "")
			if system, err := systemdefs.LookupSystem(combined); err == nil {
				return system.ID, nil
			}
		}
	}

	// Strategy 3: Try individual keywords
	for _, keyword := range keywords {
		if system, err := systemdefs.LookupSystem(keyword); err == nil {
			return system.ID, nil
		}
	}

	// Strategy 4: Try special case mappings for common DAT naming patterns
	systemID := matchSpecialCases(datName)
	if systemID != "" {
		if system, err := systemdefs.LookupSystem(systemID); err == nil {
			return system.ID, nil
		}
	}

	return "", fmt.Errorf("no system match found for DAT name: %s", datName)
}

// extractKeywords extracts searchable keywords from a DAT name
// Removes common prefixes like manufacturer names and splits on separators
func extractKeywords(datName string) []string {
	// Remove common manufacturer/organization prefixes
	prefixes := []string{
		"Nintendo - ", "Sega - ", "Sony - ", "Microsoft - ",
		"Atari - ", "NEC - ", "SNK - ", "Bandai - ",
		"Acorn - ", "Apple - ", "Commodore - ", "Amstrad - ",
		"APF - ", "8-Bit Productions ", "TOSEC - ",
		"No-Intro - ",
	}

	cleaned := datName
	for _, prefix := range prefixes {
		cleaned = strings.TrimPrefix(cleaned, prefix)
	}

	// Split on common separators and clean
	separators := []string{" - ", "/", " & ", " + "}
	parts := []string{cleaned}

	for _, sep := range separators {
		var newParts []string
		for _, part := range parts {
			newParts = append(newParts, strings.Split(part, sep)...)
		}
		parts = newParts
	}

	// Clean and filter keywords
	keywords := make([]string, 0, len(parts))
	for _, part := range parts {
		// Remove parenthetical info and extra metadata
		part = slugs.StripMetadataBrackets(part)
		part = strings.TrimSpace(part)

		if part != "" {
			keywords = append(keywords, part)
		}
	}

	return keywords
}

// matchSpecialCases handles DAT naming patterns that need special mapping
func matchSpecialCases(datName string) string {
	lower := strings.ToLower(datName)

	// Map common DAT name patterns to system IDs
	specialCases := map[string]string{
		"game boy color":        "GameboyColor",
		"game boy advance":      "GBA",
		"game boy":              "Gameboy",
		"game gear":             "GameGear",
		"master system":         "MasterSystem",
		"mega drive":            "Genesis",
		"mega cd":               "MegaCD",
		"sega cd":               "MegaCD",
		"super nintendo":        "SNES",
		"family computer":       "NES",
		"famicom":               "NES",
		"disk system":           "FDS",
		"pc engine":             "TurboGrafx16",
		"turbografx":            "TurboGrafx16",
		"neo geo pocket color":  "NeoGeoPocketColor",
		"neo geo pocket":        "NeoGeoPocket",
		"neo geo cd":            "NeoGeoCD",
		"neo geo":               "NeoGeo",
		"wonderswan color":      "WonderSwanColor",
		"wonderswan":            "WonderSwan",
		"playstation":           "PSX",
		"commander x16":         "CommanderX16",
		"zx spectrum":           "ZXSpectrum",
		"amstrad cpc":           "Amstrad",
		"commodore 64":          "C64",
		"atari 2600":            "Atari2600",
		"atari 7800":            "Atari7800",
		"atari lynx":            "AtariLynx",
		"atari st":              "AtariST",
		"intellivision":         "Intellivision",
		"colecovision":          "ColecoVision",
		"mp-1000":               "APF",
		"archimedes":            "Archimedes",
	}

	for pattern, systemID := range specialCases {
		if strings.Contains(lower, pattern) {
			return systemID
		}
	}

	return ""
}
