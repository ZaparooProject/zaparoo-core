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

package fixtures

import (
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/pkg/database/systemdefs"
)

// Database fixture collections for testing

// HistoryEntries provides sample history entries for testing
var HistoryEntries = struct {
	Collection    []database.HistoryEntry
	Successful    database.HistoryEntry
	Failed        database.HistoryEntry
	APIToken      database.HistoryEntry
	HardwareToken database.HistoryEntry
}{
	Successful: database.HistoryEntry{
		Time:       time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC),
		Type:       "ntag213",
		TokenID:    "04:12:34:AB:CD:EF:80",
		TokenValue: "zelda:botw",
		TokenData:  "",
		DBID:       1,
		Success:    true,
	},
	Failed: database.HistoryEntry{
		Time:       time.Date(2025, 1, 15, 12, 5, 0, 0, time.UTC),
		Type:       "ntag213",
		TokenID:    "04:56:78:AB:CD:EF:80",
		TokenValue: "unknown:game",
		TokenData:  "",
		DBID:       2,
		Success:    false,
	},
	APIToken: database.HistoryEntry{
		Time:       time.Date(2025, 1, 15, 12, 10, 0, 0, time.UTC),
		Type:       "api",
		TokenID:    "api-request-123",
		TokenValue: "mario:smw",
		TokenData:  `{"source":"web_ui","user_id":"test"}`,
		DBID:       3,
		Success:    true,
	},
	HardwareToken: database.HistoryEntry{
		Time:       time.Date(2025, 1, 15, 12, 15, 0, 0, time.UTC),
		Type:       "mifare_classic",
		TokenID:    "AB:CD:EF:12:34:56:78",
		TokenValue: "sega:sonic",
		TokenData:  "sector_0_data",
		DBID:       4,
		Success:    true,
	},
	Collection: []database.HistoryEntry{
		{
			Time:       time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC),
			Type:       "ntag213",
			TokenID:    "04:12:34:AB:CD:EF:80",
			TokenValue: "zelda:botw",
			TokenData:  "",
			DBID:       1,
			Success:    true,
		},
		{
			Time:       time.Date(2025, 1, 15, 12, 5, 0, 0, time.UTC),
			Type:       "ntag213",
			TokenID:    "04:56:78:AB:CD:EF:80",
			TokenValue: "unknown:game",
			TokenData:  "",
			DBID:       2,
			Success:    false,
		},
		{
			Time:       time.Date(2025, 1, 15, 12, 10, 0, 0, time.UTC),
			Type:       "api",
			TokenID:    "api-request-123",
			TokenValue: "mario:smw",
			TokenData:  `{"source":"web_ui","user_id":"test"}`,
			DBID:       3,
			Success:    true,
		},
	},
}

// Mappings provides sample mapping data for testing
var Mappings = struct {
	Collection      []database.Mapping
	SimplePattern   database.Mapping
	RegexPattern    database.Mapping
	SystemMapping   database.Mapping
	DisabledMapping database.Mapping
}{
	SimplePattern: database.Mapping{
		Label:    "Zelda Collection",
		Type:     "text",
		Match:    "exact",
		Pattern:  "zelda:*",
		Override: "",
		DBID:     1,
		Added:    time.Now().Unix(),
		Enabled:  true,
	},
	RegexPattern: database.Mapping{
		Label:    "Mario Games",
		Type:     "text",
		Match:    "regex",
		Pattern:  "mario:.*",
		Override: "",
		DBID:     2,
		Added:    time.Now().Unix(),
		Enabled:  true,
	},
	SystemMapping: database.Mapping{
		Label:    "SNES System",
		Type:     "system",
		Match:    "exact",
		Pattern:  "snes",
		Override: "Nintendo - Super Nintendo Entertainment System",
		DBID:     3,
		Added:    time.Now().Unix(),
		Enabled:  true,
	},
	DisabledMapping: database.Mapping{
		Label:    "Disabled Mapping",
		Type:     "text",
		Match:    "exact",
		Pattern:  "disabled:*",
		Override: "",
		DBID:     4,
		Added:    time.Now().Unix(),
		Enabled:  false,
	},
	Collection: []database.Mapping{
		{
			Label:    "Zelda Collection",
			Type:     "text",
			Match:    "exact",
			Pattern:  "zelda:*",
			Override: "",
			DBID:     1,
			Added:    time.Now().Unix(),
			Enabled:  true,
		},
		{
			Label:    "Mario Games",
			Type:     "text",
			Match:    "regex",
			Pattern:  "mario:.*",
			Override: "",
			DBID:     2,
			Added:    time.Now().Unix(),
			Enabled:  true,
		},
		{
			Label:    "SNES System",
			Type:     "system",
			Match:    "exact",
			Pattern:  "snes",
			Override: "Nintendo - Super Nintendo Entertainment System",
			DBID:     3,
			Added:    time.Now().Unix(),
			Enabled:  true,
		},
	},
}

// Systems provides sample system data for MediaDB testing
var Systems = struct {
	Atari2600   database.System
	GameBoy     database.System
	SNES        database.System
	PlayStation database.System
	Collection  []database.System
}{
	Atari2600: database.System{
		SystemID: "atari2600",
		Name:     "Atari - 2600",
		DBID:     1,
	},
	GameBoy: database.System{
		SystemID: "gb",
		Name:     "Nintendo - Game Boy",
		DBID:     2,
	},
	SNES: database.System{
		SystemID: "snes",
		Name:     "Nintendo - Super Nintendo Entertainment System",
		DBID:     3,
	},
	PlayStation: database.System{
		SystemID: "psx",
		Name:     "Sony - PlayStation",
		DBID:     4,
	},
	Collection: []database.System{
		{SystemID: "atari2600", Name: "Atari - 2600", DBID: 1},
		{SystemID: "gb", Name: "Nintendo - Game Boy", DBID: 2},
		{SystemID: "snes", Name: "Nintendo - Super Nintendo Entertainment System", DBID: 3},
		{SystemID: "psx", Name: "Sony - PlayStation", DBID: 4},
	},
}

// MediaTitles provides sample media title data
var MediaTitles = struct {
	Collection   []database.MediaTitle
	Pitfall      database.MediaTitle
	Tetris       database.MediaTitle
	SuperMetroid database.MediaTitle
	FFVII        database.MediaTitle
}{
	Pitfall: database.MediaTitle{
		Slug:       "pitfall",
		Name:       "Pitfall!",
		DBID:       1,
		SystemDBID: 1, // Atari 2600
	},
	Tetris: database.MediaTitle{
		Slug:       "tetris",
		Name:       "Tetris",
		DBID:       2,
		SystemDBID: 2, // Game Boy
	},
	SuperMetroid: database.MediaTitle{
		Slug:       "super-metroid",
		Name:       "Super Metroid",
		DBID:       3,
		SystemDBID: 3, // SNES
	},
	FFVII: database.MediaTitle{
		Slug:       "final-fantasy-vii",
		Name:       "Final Fantasy VII",
		DBID:       4,
		SystemDBID: 4, // PlayStation
	},
	Collection: []database.MediaTitle{
		{Slug: "pitfall", Name: "Pitfall!", DBID: 1, SystemDBID: 1},
		{Slug: "tetris", Name: "Tetris", DBID: 2, SystemDBID: 2},
		{Slug: "super-metroid", Name: "Super Metroid", DBID: 3, SystemDBID: 3},
		{Slug: "final-fantasy-vii", Name: "Final Fantasy VII", DBID: 4, SystemDBID: 4},
	},
}

// Media provides sample media file data
var Media = struct {
	Collection      []database.Media
	PitfallROM      database.Media
	TetrisROM       database.Media
	SuperMetroidROM database.Media
	FFVIIISO        database.Media
}{
	PitfallROM: database.Media{
		Path:           "/media/Atari - 2600/Pitfall! (1982).zip",
		DBID:           1,
		MediaTitleDBID: 1,
	},
	TetrisROM: database.Media{
		Path:           "/media/Nintendo - Game Boy/Tetris (1989).gb",
		DBID:           2,
		MediaTitleDBID: 2,
	},
	SuperMetroidROM: database.Media{
		Path:           "/media/Nintendo - Super Nintendo Entertainment System/Super Metroid (1994).sfc",
		DBID:           3,
		MediaTitleDBID: 3,
	},
	FFVIIISO: database.Media{
		Path:           "/media/Sony - PlayStation/Final Fantasy VII (1997).chd",
		DBID:           4,
		MediaTitleDBID: 4,
	},
	Collection: []database.Media{
		{Path: "/media/Atari - 2600/Pitfall! (1982).zip", DBID: 1, MediaTitleDBID: 1},
		{Path: "/media/Nintendo - Game Boy/Tetris (1989).gb", DBID: 2, MediaTitleDBID: 2},
		{
			Path:           "/media/Nintendo - Super Nintendo Entertainment System/Super Metroid (1994).sfc",
			DBID:           3,
			MediaTitleDBID: 3,
		},
		{Path: "/media/Sony - PlayStation/Final Fantasy VII (1997).chd", DBID: 4, MediaTitleDBID: 4},
	},
}

// TagTypes provides sample tag type data
var TagTypes = struct {
	Genre      database.TagType
	Developer  database.TagType
	Year       database.TagType
	Collection []database.TagType
}{
	Genre: database.TagType{
		Type: "genre",
		DBID: 1,
	},
	Developer: database.TagType{
		Type: "developer",
		DBID: 2,
	},
	Year: database.TagType{
		Type: "year",
		DBID: 3,
	},
	Collection: []database.TagType{
		{Type: "genre", DBID: 1},
		{Type: "developer", DBID: 2},
		{Type: "year", DBID: 3},
	},
}

// Tags provides sample tag data
var Tags = struct {
	Collection  []database.Tag
	ActionGenre database.Tag
	PuzzleGenre database.Tag
	Nintendo    database.Tag
	SquareEnix  database.Tag
	Year1982    database.Tag
	Year1989    database.Tag
}{
	ActionGenre: database.Tag{
		Tag:      "Action",
		DBID:     1,
		TypeDBID: 1, // Genre
	},
	PuzzleGenre: database.Tag{
		Tag:      "Puzzle",
		DBID:     2,
		TypeDBID: 1, // Genre
	},
	Nintendo: database.Tag{
		Tag:      "Nintendo",
		DBID:     3,
		TypeDBID: 2, // Developer
	},
	SquareEnix: database.Tag{
		Tag:      "Square Enix",
		DBID:     4,
		TypeDBID: 2, // Developer
	},
	Year1982: database.Tag{
		Tag:      "1982",
		DBID:     5,
		TypeDBID: 3, // Year
	},
	Year1989: database.Tag{
		Tag:      "1989",
		DBID:     6,
		TypeDBID: 3, // Year
	},
	Collection: []database.Tag{
		{Tag: "Action", DBID: 1, TypeDBID: 1},
		{Tag: "Puzzle", DBID: 2, TypeDBID: 1},
		{Tag: "Nintendo", DBID: 3, TypeDBID: 2},
		{Tag: "Square Enix", DBID: 4, TypeDBID: 2},
		{Tag: "1982", DBID: 5, TypeDBID: 3},
		{Tag: "1989", DBID: 6, TypeDBID: 3},
	},
}

// MediaTags provides sample media-tag associations
var MediaTags = struct {
	Collection     []database.MediaTag
	PitfallAction  database.MediaTag
	TetrisPuzzle   database.MediaTag
	TetrisNintendo database.MediaTag
}{
	PitfallAction: database.MediaTag{
		DBID:      1,
		MediaDBID: 1, // Pitfall
		TagDBID:   1, // Action
	},
	TetrisPuzzle: database.MediaTag{
		DBID:      2,
		MediaDBID: 2, // Tetris
		TagDBID:   2, // Puzzle
	},
	TetrisNintendo: database.MediaTag{
		DBID:      3,
		MediaDBID: 2, // Tetris
		TagDBID:   3, // Nintendo
	},
	Collection: []database.MediaTag{
		{DBID: 1, MediaDBID: 1, TagDBID: 1}, // Pitfall -> Action
		{DBID: 2, MediaDBID: 2, TagDBID: 2}, // Tetris -> Puzzle
		{DBID: 3, MediaDBID: 2, TagDBID: 3}, // Tetris -> Nintendo
	},
}

// SearchResults provides sample search result data
var SearchResults = struct {
	PitfallResult      database.SearchResult
	TetrisResult       database.SearchResult
	SuperMetroidResult database.SearchResult
	Collection         []database.SearchResult
}{
	PitfallResult: database.SearchResult{
		SystemID: "atari2600",
		Name:     "Pitfall!",
		Path:     "/media/Atari - 2600/Pitfall! (1982).zip",
	},
	TetrisResult: database.SearchResult{
		SystemID: "gb",
		Name:     "Tetris",
		Path:     "/media/Nintendo - Game Boy/Tetris (1989).gb",
	},
	SuperMetroidResult: database.SearchResult{
		SystemID: "snes",
		Name:     "Super Metroid",
		Path:     "/media/Nintendo - Super Nintendo Entertainment System/Super Metroid (1994).sfc",
	},
	Collection: []database.SearchResult{
		{SystemID: "atari2600", Name: "Pitfall!", Path: "/media/Atari - 2600/Pitfall! (1982).zip"},
		{SystemID: "gb", Name: "Tetris", Path: "/media/Nintendo - Game Boy/Tetris (1989).gb"},
		{
			SystemID: "snes",
			Name:     "Super Metroid",
			Path:     "/media/Nintendo - Super Nintendo Entertainment System/Super Metroid (1994).sfc",
		},
	},
}

// Helper functions for creating test data

// NewHistoryEntry creates a new history entry with custom values
func NewHistoryEntry(tokenType, tokenID, tokenValue string, success bool) database.HistoryEntry {
	return database.HistoryEntry{
		Time:       time.Now(),
		Type:       tokenType,
		TokenID:    tokenID,
		TokenValue: tokenValue,
		TokenData:  "",
		Success:    success,
	}
}

// NewMapping creates a new mapping with custom values
func NewMapping(label, matchType, pattern string, enabled bool) database.Mapping {
	return database.Mapping{
		Label:   label,
		Type:    "text",
		Match:   matchType,
		Pattern: pattern,
		Added:   time.Now().Unix(),
		Enabled: enabled,
	}
}

// NewSystem creates a new system with custom values
func NewSystem(systemID, name string) database.System {
	return database.System{
		SystemID: systemID,
		Name:     name,
	}
}

// GetSystemDefsByID returns system definitions for testing
func GetSystemDefsByID() map[string]systemdefs.System {
	return map[string]systemdefs.System{
		"atari2600": {ID: "atari2600"},
		"gb":        {ID: "gb"},
		"snes":      {ID: "snes"},
		"psx":       {ID: "psx"},
	}
}

// GetTestSystemDefs returns a collection of system definitions for testing
func GetTestSystemDefs() []systemdefs.System {
	return []systemdefs.System{
		{ID: "atari2600"},
		{ID: "gb"},
		{ID: "snes"},
		{ID: "psx"},
	}
}
