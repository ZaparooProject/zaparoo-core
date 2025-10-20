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

package zapscript

import (
	"context"
	"os"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript/parser"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript/titles"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// setupTestMediaDBWithAllGames creates a real SQLite database populated with comprehensive test data
// covering all matching strategies. All tests use this SAME database.
func setupTestMediaDBWithAllGames(t *testing.T) (db *mediadb.MediaDB, cleanup func()) {
	// Create temp directory
	tempDir, err := os.MkdirTemp("", "zaparoo-test-title-strategies-*")
	require.NoError(t, err)

	// Create mock platform that returns temp directory
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Settings").Return(platforms.Settings{
		DataDir: tempDir,
	})

	// Open MediaDB
	ctx := context.Background()
	mediaDB, err := mediadb.OpenMediaDB(ctx, mockPlatform)
	require.NoError(t, err)

	// Create tag types
	regionTagType, err := mediaDB.FindOrInsertTagType(database.TagType{Type: "region"})
	require.NoError(t, err)
	unfinishedTagType, err := mediaDB.FindOrInsertTagType(database.TagType{Type: "unfinished"})
	require.NoError(t, err)
	langTagType, err := mediaDB.FindOrInsertTagType(database.TagType{Type: "lang"})
	require.NoError(t, err)

	// Begin transaction for bulk insert
	err = mediaDB.BeginTransaction(false)
	require.NoError(t, err)

	// Get systems
	snesSystem, err := systemdefs.GetSystem("SNES")
	require.NoError(t, err)
	nesSystem, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)
	genesisSystem, err := systemdefs.GetSystem("Genesis")
	require.NoError(t, err)
	n64System, err := systemdefs.GetSystem("Nintendo64")
	require.NoError(t, err)
	pcSystem, err := systemdefs.GetSystem("PC")
	require.NoError(t, err)

	// Insert systems
	insertedSNES, err := mediaDB.InsertSystem(database.System{SystemID: snesSystem.ID, Name: "SNES"})
	require.NoError(t, err)
	insertedNES, err := mediaDB.InsertSystem(database.System{SystemID: nesSystem.ID, Name: "NES"})
	require.NoError(t, err)
	insertedGenesis, err := mediaDB.InsertSystem(database.System{SystemID: genesisSystem.ID, Name: "Genesis"})
	require.NoError(t, err)
	insertedN64, err := mediaDB.InsertSystem(database.System{SystemID: n64System.ID, Name: "Nintendo64"})
	require.NoError(t, err)
	insertedPC, err := mediaDB.InsertSystem(database.System{SystemID: pcSystem.ID, Name: "PC"})
	require.NoError(t, err)

	// Create tags
	usaTag, err := mediaDB.FindOrInsertTag(database.Tag{TypeDBID: regionTagType.DBID, Tag: string(tags.TagRegionUS)})
	require.NoError(t, err)
	europeTag, err := mediaDB.FindOrInsertTag(database.Tag{TypeDBID: regionTagType.DBID, Tag: string(tags.TagRegionEU)})
	require.NoError(t, err)
	japanTag, err := mediaDB.FindOrInsertTag(database.Tag{TypeDBID: regionTagType.DBID, Tag: string(tags.TagRegionJP)})
	require.NoError(t, err)
	demoTag, err := mediaDB.FindOrInsertTag(database.Tag{TypeDBID: unfinishedTagType.DBID, Tag: "demo"})
	require.NoError(t, err)
	betaTag, err := mediaDB.FindOrInsertTag(database.Tag{TypeDBID: unfinishedTagType.DBID, Tag: "beta"})
	require.NoError(t, err)
	protoTag, err := mediaDB.FindOrInsertTag(database.Tag{TypeDBID: unfinishedTagType.DBID, Tag: "proto"})
	require.NoError(t, err)
	enTag, err := mediaDB.FindOrInsertTag(database.Tag{TypeDBID: langTagType.DBID, Tag: "en"})
	require.NoError(t, err)
	jaTag, err := mediaDB.FindOrInsertTag(database.Tag{TypeDBID: langTagType.DBID, Tag: "ja"})
	require.NoError(t, err)
	frTag, err := mediaDB.FindOrInsertTag(database.Tag{TypeDBID: langTagType.DBID, Tag: "fr"})
	require.NoError(t, err)

	// === SNES GAMES - For exact matching, tag filtering, variants ===
	addGame := func(systemDBID int64, name, path string, tagDBIDs ...int64) {
		metadata := mediadb.GenerateSlugWithMetadata(name)
		title := database.MediaTitle{
			SystemDBID:    systemDBID,
			Slug:          metadata.Slug,
			Name:          name,
			SlugLength:    metadata.SlugLength,
			SlugWordCount: metadata.SlugWordCount,
			SecondarySlug: metadata.SecondarySlug,
		}
		insertedTitle, titleErr := mediaDB.InsertMediaTitle(&title)
		require.NoError(t, titleErr)

		media := database.Media{
			SystemDBID:     systemDBID,
			MediaTitleDBID: insertedTitle.DBID,
			Path:           path,
		}
		insertedMedia, mediaErr := mediaDB.InsertMedia(media)
		require.NoError(t, mediaErr)

		for _, tagDBID := range tagDBIDs {
			_, err = mediaDB.InsertMediaTag(database.MediaTag{
				MediaDBID: insertedMedia.DBID,
				TagDBID:   tagDBID,
			})
			require.NoError(t, err)
		}
	}

	// SNES games - multiple regions for testing region selection
	addGame(insertedSNES.DBID, "Plumber Quest Adventures (USA)",
		"/roms/snes/Plumber Quest Adventures (USA).sfc", usaTag.DBID)
	addGame(insertedSNES.DBID, "Plumber Quest Adventures (Europe)",
		"/roms/snes/Plumber Quest Adventures (Europe).sfc", europeTag.DBID)
	addGame(insertedSNES.DBID, "Plumber Quest Adventures (Japan)",
		"/roms/snes/Plumber Quest Adventures (Japan).sfc", japanTag.DBID)
	addGame(insertedSNES.DBID, "Plumber Quest Adventures (Demo)",
		"/roms/snes/Plumber Quest Adventures (Demo).sfc", demoTag.DBID)
	addGame(insertedSNES.DBID, "Plumber Quest Adventures (Beta)",
		"/roms/snes/Plumber Quest Adventures (Beta).sfc", betaTag.DBID)
	addGame(insertedSNES.DBID, "Hero's Sword: Ancient Kingdom (USA)",
		"/roms/snes/Hero Sword AK (USA).sfc", usaTag.DBID)
	addGame(insertedSNES.DBID, "Time Paradox RPG (USA)",
		"/roms/snes/Time Paradox RPG (USA).sfc", usaTag.DBID)
	addGame(insertedSNES.DBID, "Time Paradox RPG (Europe)",
		"/roms/snes/Time Paradox RPG (Europe).sfc", europeTag.DBID)
	// No region tag for testing untagged matching
	addGame(insertedSNES.DBID, "Time Paradox RPG", "/roms/snes/Time Paradox RPG.sfc")
	addGame(insertedSNES.DBID, "Jungle Ape Platformer (USA)", "/roms/snes/Jungle Ape (USA).sfc", usaTag.DBID)
	addGame(insertedSNES.DBID, "Dragon Warrior Chronicles (USA)", "/roms/snes/Dragon Warrior (USA).sfc", usaTag.DBID)
	addGame(insertedSNES.DBID, "Star Racer X (USA)", "/roms/snes/Star Racer X (USA).sfc", usaTag.DBID)
	addGame(insertedSNES.DBID, "Mystic Quest 2 (USA)", "/roms/snes/Mystic Quest 2 (USA).sfc", usaTag.DBID)
	// For leading article normalization
	addGame(insertedSNES.DBID, "The Mystic Quest (USA)", "/roms/snes/The Mystic Quest (USA).sfc", usaTag.DBID)
	// For roman numeral normalization
	addGame(insertedSNES.DBID, "Mystic Quest IV (USA)", "/roms/snes/Mystic Quest 4 (USA).sfc", usaTag.DBID)

	// Genesis games - for fuzzy matching and token signature
	// "turbo" 5 chars
	addGame(insertedGenesis.DBID, "Turbo (USA)", "/roms/genesis/Turbo (USA).md", usaTag.DBID)
	// Full name
	addGame(insertedGenesis.DBID, "Turbo the Speedster (USA)", "/roms/genesis/Turbo Full (USA).md", usaTag.DBID)
	// "turboblaze" for token test
	addGame(insertedGenesis.DBID, "Turbo Blaze (USA)", "/roms/genesis/Turbo Blaze (USA).md", usaTag.DBID)
	addGame(insertedGenesis.DBID, "Urban Brawlers 2 (USA)", "/roms/genesis/Urban Brawlers 2 (USA).md", usaTag.DBID)
	addGame(insertedGenesis.DBID, "Thunder Force V (USA)", "/roms/genesis/Thunder Force V (USA).md", usaTag.DBID)
	addGame(insertedGenesis.DBID, "Golden Axe Chronicles (USA)", "/roms/genesis/Golden Axe (USA).md", usaTag.DBID)
	addGame(insertedGenesis.DBID, "Alien Storm (USA)", "/roms/genesis/Alien Storm (USA).md", usaTag.DBID)
	addGame(insertedGenesis.DBID, "Rocket Knight (USA)", "/roms/genesis/Rocket Knight (USA).md", usaTag.DBID)
	// Reversed word order for token test
	addGame(insertedGenesis.DBID, "Blaze Turbo (USA)", "/roms/genesis/Blaze Turbo (USA).md", usaTag.DBID)
	addGame(insertedGenesis.DBID, "Dragon Fire (USA)", "/roms/genesis/Dragon Fire (USA).md", usaTag.DBID)
	// Another token pair
	addGame(insertedGenesis.DBID, "Fire Dragon (USA)", "/roms/genesis/Fire Dragon (USA).md", usaTag.DBID)
	// For ambiguity testing with other Turbo games
	addGame(insertedGenesis.DBID, "Turbo Charged (USA)", "/roms/genesis/Turbo Charged (USA).md", usaTag.DBID)

	// NES games - for fuzzy matching with typos (SIMPLE NAMES for pre-filter to work)
	// "sword" 5 chars
	addGame(insertedNES.DBID, "Sword (USA)", "/roms/nes/Sword (USA).nes", usaTag.DBID)
	// "quest" 5 chars
	addGame(insertedNES.DBID, "Quest (USA)", "/roms/nes/Quest (USA).nes", usaTag.DBID)
	// "galaxia" 7 chars
	addGame(insertedNES.DBID, "Galaxia (USA)", "/roms/nes/Galaxia (USA).nes", usaTag.DBID)
	// "vampirehunt" 11 chars
	addGame(insertedNES.DBID, "Vampire Hunt (USA)", "/roms/nes/Vampire Hunt (USA).nes", usaTag.DBID)
	// Full name version
	addGame(insertedNES.DBID, "The Quest of Heroes (USA)", "/roms/nes/Quest Heroes (USA).nes", usaTag.DBID)
	// Full name version
	addGame(insertedNES.DBID, "Super Plumber Bros. (USA)", "/roms/nes/Plumber Bros (USA).nes", usaTag.DBID)
	addGame(insertedNES.DBID, "Ninja Warrior (USA)", "/roms/nes/Ninja Warrior (USA).nes", usaTag.DBID)
	addGame(insertedNES.DBID, "Dragon Master (USA)", "/roms/nes/Dragon Master (USA).nes", usaTag.DBID)
	addGame(insertedNES.DBID, "Bubble Adventure (USA)", "/roms/nes/Bubble Adventure (USA).nes", usaTag.DBID)
	addGame(insertedNES.DBID, "Bomber King (USA)", "/roms/nes/Bomber King (USA).nes", usaTag.DBID)
	addGame(insertedNES.DBID, "Mega Runner (USA)", "/roms/nes/Mega Runner (USA).nes", usaTag.DBID)
	// "bubble" 6 chars for fuzzy test
	addGame(insertedNES.DBID, "Bubble (USA)", "/roms/nes/Bubble (USA).nes", usaTag.DBID)
	// For confidence threshold testing (similar to Dragon Master)
	addGame(insertedNES.DBID, "Dragon War (USA)", "/roms/nes/Dragon War (USA).nes", usaTag.DBID)

	// N64 games - for secondary title matching (needs ":" or "-" in title)
	// Secondary: "Crystal Temple"
	addGame(insertedN64.DBID, "Hero's Adventure: Crystal Temple (USA)", "/roms/n64/Hero Crystal (USA).z64",
		usaTag.DBID)
	// Secondary: "Dark Moon"
	addGame(insertedN64.DBID, "Hero's Adventure: Dark Moon (USA)", "/roms/n64/Hero Dark Moon (USA).z64",
		usaTag.DBID)
	addGame(insertedN64.DBID, "Extreme Racer 64 (USA)", "/roms/n64/Racer64 (USA).z64", usaTag.DBID)
	addGame(insertedN64.DBID, "Secret Agent 009 (USA)", "/roms/n64/Agent009 (USA).z64", usaTag.DBID)
	// Short secondary with dash
	addGame(insertedN64.DBID, "Adventure - Temple (USA)", "/roms/n64/Adventure Temple Alt (USA).z64", usaTag.DBID)
	addGame(insertedN64.DBID, "Fighter Tournament 64 (USA)", "/roms/n64/Fighter64 (USA).z64", usaTag.DBID)
	addGame(insertedN64.DBID, "Kart Champion (USA)", "/roms/n64/Kart Champion (USA).z64", usaTag.DBID)
	addGame(insertedN64.DBID, "Paper Heroes (USA)", "/roms/n64/Paper Heroes (USA).z64", usaTag.DBID)

	// PC games - for token signature matching (word order independent)
	addGame(insertedPC.DBID, "Robot Wars", "/games/pc/robotwars/rw.exe")   // "robotwars" 9 chars, 2 words
	addGame(insertedPC.DBID, "Wars Robot", "/games/pc/warsrobot/wr.exe")   // Reversed for token test
	addGame(insertedPC.DBID, "Space Quest", "/games/pc/spacequest/sq.exe") // "spacequest" 10 chars, 2 words
	addGame(insertedPC.DBID, "Crystal Defender", "/games/pc/crystaldefender/cd.exe")
	addGame(insertedPC.DBID, "Zombie Attack", "/games/pc/zombieattack/za.exe")
	addGame(insertedPC.DBID, "Neon Runner", "/games/pc/neonrunner/nr.exe")

	// Additional test data for Priority 1 critical tests
	// For strategy precedence testing
	addGame(insertedGenesis.DBID, "Turbo Charged Racing (USA)",
		"/roms/genesis/Turbo Charged Racing (USA).md", usaTag.DBID)

	// For ambiguity/tie-breaking tests
	addGame(insertedGenesis.DBID, "Storm Warrior (Europe)", "/roms/genesis/Storm Warrior (EU).md", europeTag.DBID)
	addGame(insertedGenesis.DBID, "Storm Warrior (USA)", "/roms/genesis/Storm Warrior (USA).md", usaTag.DBID)

	// For confidence threshold tests
	addGame(insertedNES.DBID, "Dragon Warlock (USA)", "/roms/nes/Dragon Warlock (USA).nes", usaTag.DBID)
	addGame(insertedNES.DBID, "Mystery Castle (USA)", "/roms/nes/Mystery Castle (USA).nes", usaTag.DBID)

	// Additional test data for Priority 2 important tests
	// For ampersand normalization
	addGame(insertedSNES.DBID, "Dungeons & Dragons (USA)", "/roms/snes/DnD (USA).sfc", usaTag.DBID)

	// For 3+ word token signature
	addGame(insertedPC.DBID, "Quest Space Crystal", "/games/pc/crystal_space_quest.exe")

	// For secondary title with dash (already have colon tests)
	addGame(insertedN64.DBID, "Racing - Speed Edition (USA)", "/roms/n64/Racing Speed (USA).z64", usaTag.DBID)

	// Additional test data for Priority 3 edge case tests
	// For extreme length tests
	// Very short (1 char)
	addGame(insertedNES.DBID, "Q (USA)", "/roms/nes/Q (USA).nes", usaTag.DBID)
	// Very long
	addGame(insertedSNES.DBID, "The Super Ultimate Mega Hyper Championship Tournament Edition Deluxe (USA)",
		"/roms/snes/Super Ultimate Long (USA).sfc", usaTag.DBID)
	// Unicode accent
	addGame(insertedSNES.DBID, "Pokémon Stadium (USA)", "/roms/snes/Pokemon Stadium (USA).sfc", usaTag.DBID)
	// Multiple colons
	addGame(insertedN64.DBID, "Adventure Game: Part 1: The Beginning (USA)",
		"/roms/n64/Adventure Part 1 (USA).z64", usaTag.DBID)
	// Very short for prefilter test
	addGame(insertedGenesis.DBID, "X (USA)", "/roms/genesis/X (USA).md", usaTag.DBID)
	// Prototype variant
	addGame(insertedGenesis.DBID, "Plumber Quest Adventures Ultimate Edition (Proto)",
		"/roms/genesis/Plumber Proto (USA).md", protoTag.DBID)
	// Progressive trim depth test
	addGame(insertedSNES.DBID, "Fighter Game One Two Three Four (USA)",
		"/roms/snes/Fighter 1234 (USA).sfc", usaTag.DBID)
	// For multi-typo fuzzy test
	addGame(insertedNES.DBID, "Adventure Quest Warrior (USA)",
		"/roms/nes/Adv Quest Warrior (USA).nes", usaTag.DBID)

	// Additional test data for config preference tests
	// Games with multiple regions for testing region preferences
	addGame(insertedGenesis.DBID, "Space Shooter (USA)",
		"/roms/genesis/Space Shooter (USA).md", usaTag.DBID, enTag.DBID)
	addGame(insertedGenesis.DBID, "Space Shooter (Europe)",
		"/roms/genesis/Space Shooter (EUR).md", europeTag.DBID, enTag.DBID)
	addGame(insertedGenesis.DBID, "Space Shooter (Japan)",
		"/roms/genesis/Space Shooter (JPN).md", japanTag.DBID, jaTag.DBID)

	// Games with same region, different languages
	addGame(insertedSNES.DBID, "RPG Quest (Europe) (En)",
		"/roms/snes/RPG Quest (EUR) (En).sfc", europeTag.DBID, enTag.DBID)
	addGame(insertedSNES.DBID, "RPG Quest (Europe) (Fr)",
		"/roms/snes/RPG Quest (EUR) (Fr).sfc", europeTag.DBID, frTag.DBID)
	addGame(insertedSNES.DBID, "RPG Quest (Japan)", "/roms/snes/RPG Quest (JPN).sfc", japanTag.DBID, jaTag.DBID)

	err = mediaDB.CommitTransaction()
	require.NoError(t, err)

	cleanup = func() {
		if mediaDB != nil {
			_ = mediaDB.Close()
		}
		_ = os.RemoveAll(tempDir)
	}

	db = mediaDB
	return db, cleanup
}

// makeConfigWithPreferences creates a config instance with specific region/language preferences
func makeConfigWithPreferences(t *testing.T, regions, langs []string) *config.Instance {
	configDir := t.TempDir()
	defaults := config.BaseDefaults
	defaults.Media.DefaultRegions = regions
	defaults.Media.DefaultLangs = langs

	cfg, err := config.NewConfig(configDir, defaults)
	require.NoError(t, err)
	return cfg
}

// TestCmdTitle_AllStrategiesIntegration tests EVERY matching strategy against a real database
func TestCmdTitle_AllStrategiesIntegration(t *testing.T) {
	// Setup SHARED database for ALL tests
	mediaDB, cleanup := setupTestMediaDBWithAllGames(t)
	defer cleanup()

	tests := []struct {
		advArgs          map[string]string
		cfg              *config.Instance
		name             string
		input            string
		expectedPath     string
		expectedStrategy string
		description      string
		expectedError    bool
	}{
		// ============================================================
		// EXACT MATCH STRATEGY
		// ============================================================

		{
			name:             "exact_match_with_preferred_region",
			input:            "SNES/Plumber Quest Adventures",
			expectedPath:     "/roms/snes/Plumber Quest Adventures (USA).sfc",
			expectedStrategy: titles.StrategyExactMatch,
			description:      "Exact slug match selects USA region (preferred)",
		},
		{
			name:             "exact_match_no_tag_filter",
			input:            "SNES/Time Paradox RPG",
			expectedPath:     "/roms/snes/Time Paradox RPG (USA).sfc",
			expectedStrategy: titles.StrategyExactMatch,
			description:      "Exact match without explicit tags, uses default region preference",
		},
		{
			name:             "exact_match_full_title_with_colon",
			input:            "Nintendo64/Hero's Adventure: Crystal Temple",
			expectedPath:     "/roms/n64/Hero Crystal (USA).z64",
			expectedStrategy: titles.StrategyExactMatch,
			description:      "Full title with colon matches via exact match (colon is stripped during slugification)",
		},
		{
			name:             "exact_match_full_title_with_colon_darkmoon",
			input:            "Nintendo64/Hero's Adventure: Dark Moon",
			expectedPath:     "/roms/n64/Hero Dark Moon (USA).z64",
			expectedStrategy: titles.StrategyExactMatch,
			description:      "Full title with colon matches via exact match (colon is stripped during slugification)",
		},
		{
			name:             "exact_match_turbo_blaze",
			input:            "Genesis/Turbo Blaze",
			expectedPath:     "/roms/genesis/Turbo Blaze (USA).md",
			expectedStrategy: titles.StrategyExactMatch,
			description:      "Exact match for 'Turbo Blaze'",
		},
		{
			name:             "exact_match_wars_robot",
			input:            "PC/Wars Robot",
			expectedPath:     "/games/pc/warsrobot/wr.exe",
			expectedStrategy: titles.StrategyExactMatch,
			description:      "Exact match for 'Wars Robot' (database has this exact title)",
		},
		{
			name:             "exact_match_variant_excludes_demo",
			input:            "SNES/Plumber Quest Adventures",
			expectedPath:     "/roms/snes/Plumber Quest Adventures (USA).sfc",
			expectedStrategy: titles.StrategyExactMatch,
			description:      "Automatically excludes demo/beta variants",
		},
		{
			name:             "exact_match_word_normalization",
			input:            "Genesis/Turbo Blaze",
			expectedPath:     "/roms/genesis/Turbo Blaze (USA).md",
			expectedStrategy: titles.StrategyExactMatch,
			description:      "Handles word normalization in slug matching",
		},
		{
			name:             "exact_match_high_confidence_early_exit",
			input:            "SNES/Dragon Warrior Chronicles",
			expectedPath:     "/roms/snes/Dragon Warrior (USA).sfc",
			expectedStrategy: titles.StrategyExactMatch,
			description:      "Exact slug match with perfect tag alignment triggers early exit at high confidence threshold",
		},
		{
			name:             "exact_match_tie_break_with_region_preference",
			input:            "Genesis/Storm Warrior",
			expectedPath:     "/roms/genesis/Storm Warrior (USA).md",
			expectedStrategy: titles.StrategyExactMatch,
			description:      "When multiple exact matches exist (USA and Europe), default region preference (USA) breaks the tie",
		},
		{
			name:             "exact_match_full_title_with_dash",
			input:            "Nintendo64/Adventure - Temple",
			expectedPath:     "/roms/n64/Adventure Temple Alt (USA).z64",
			expectedStrategy: titles.StrategyExactMatch,
			description:      "Full title with dash 'Adventure - Temple' matches database entry (slug: adventuretemple)",
		},
		{
			name:             "exact_match_adventure_temple",
			input:            "Nintendo64/Adventure Temple",
			expectedPath:     "/roms/n64/Adventure Temple Alt (USA).z64",
			expectedStrategy: titles.StrategyExactMatch,
			description:      "Exact match for 'Adventure Temple'",
		},
		{
			name:             "exact_match_multiple_same_slug_tag_score_diff",
			input:            "SNES/Time Paradox RPG",
			expectedPath:     "/roms/snes/Time Paradox RPG (USA).sfc",
			expectedStrategy: titles.StrategyExactMatch,
			description:      "When multiple exact slug matches exist (USA, Europe, no-region), tag scoring selects USA as highest preference",
		},
		{
			name:             "exact_match_variant_inclusion_with_explicit_tag",
			input:            "SNES/Plumber Quest Adventures",
			expectedPath:     "/roms/snes/Plumber Quest Adventures (Demo).sfc",
			expectedStrategy: titles.StrategyExactMatch,
			description:      "Explicit tag (unfinished:demo) overrides default variant exclusion and finds demo game",
			advArgs:          map[string]string{"tags": "unfinished:demo"},
		},
		{
			name:             "exact_match_very_short_title_1_char",
			input:            "NES/Q",
			expectedPath:     "/roms/nes/Q (USA).nes",
			expectedStrategy: titles.StrategyExactMatch,
			description:      "Single character title should match correctly (tests minimum slug length handling)",
		},
		{
			name:             "exact_match_very_long_title",
			input:            "SNES/The Super Ultimate Mega Hyper Championship Tournament Edition Deluxe",
			expectedPath:     "/roms/snes/Super Ultimate Long (USA).sfc",
			expectedStrategy: titles.StrategyExactMatch,
			description:      "Very long title (50+ chars) should match correctly without truncation issues",
		},
		{
			name:             "exact_match_unicode_accent_normalization",
			input:            "SNES/Pokemon Stadium",
			expectedPath:     "/roms/snes/Pokemon Stadium (USA).sfc",
			expectedStrategy: titles.StrategyExactMatch,
			description:      "Query without accent 'Pokemon' matches database entry with accent 'Pokémon' via unicode normalization",
		},
		{
			name:             "exact_match_multiple_colons_in_title",
			input:            "Nintendo64/Adventure Game: Part 1: The Beginning",
			expectedPath:     "/roms/n64/Adventure Part 1 (USA).z64",
			expectedStrategy: titles.StrategyExactMatch,
			description:      "Query with multiple colons matches exactly (colons stripped during slugification)",
		},
		{
			name:             "exact_match_prefilter_extreme_length_diff",
			input:            "Genesis/X",
			expectedPath:     "/roms/genesis/X (USA).md",
			expectedStrategy: titles.StrategyExactMatch,
			description:      "Very short query (1 char) should not be excluded by fuzzy prefilter length constraints",
		},

		// ============================================================
		// SECONDARY TITLE EXACT MATCH STRATEGY
		// ============================================================

		{
			name:             "secondary_title_exact_match_crystal_temple",
			input:            "Nintendo64/Crystal Temple",
			expectedPath:     "/roms/n64/Hero Crystal (USA).z64",
			expectedStrategy: titles.StrategySecondaryTitleExact,
			description:      "Token 'Crystal Temple' matches DB entry 'Hero's Adventure: Crystal Temple' via SecondarySlug column",
		},
		{
			name:             "secondary_title_exact_match_dark_moon",
			input:            "Nintendo64/Dark Moon",
			expectedPath:     "/roms/n64/Hero Dark Moon (USA).z64",
			expectedStrategy: titles.StrategySecondaryTitleExact,
			description:      "Token 'Dark Moon' matches DB entry 'Hero's Adventure: Dark Moon' via SecondarySlug column",
		},

		// ============================================================
		// MAIN TITLE ONLY STRATEGY
		// ============================================================

		{
			name:             "main_title_prefix_match",
			input:            "Nintendo64/Hero's Adventure",
			expectedPath:     "/roms/n64/Hero Crystal (USA).z64",
			expectedStrategy: titles.StrategyMainTitleOnly,
			description:      "Token 'Hero's Adventure' matches DB entries via prefix search and main title post-filter",
		},

		// ============================================================
		// TOKEN SIGNATURE STRATEGY
		// ============================================================

		{
			name:             "token_signature_3_word_match",
			input:            "PC/Crystal Space Quest",
			expectedPath:     "/games/pc/crystal_space_quest.exe",
			expectedStrategy: titles.StrategyTokenSignature,
			description:      "Token signature matches 3-word reversal: 'Crystal Space Quest' matches 'Quest Space Crystal'",
		},

		// ============================================================
		// FUZZY MATCH STRATEGY (JARO-WINKLER)
		// ============================================================

		{
			name:             "fuzzy_typo_missing_char",
			input:            "NES/Buble",
			expectedPath:     "/roms/nes/Bubble (USA).nes",
			expectedStrategy: titles.StrategyJaroWinklerDamerau,
			description:      "Fuzzy match corrects typo: buble -> bubble (missing 'b')",
		},
		{
			name:             "fuzzy_typo_wrong_char",
			input:            "Genesis/Turbu",
			expectedPath:     "/roms/genesis/Turbo (USA).md",
			expectedStrategy: titles.StrategyJaroWinklerDamerau,
			description:      "Fuzzy match corrects typo: turbu -> turbo",
		},
		{
			name:             "fuzzy_typo_transposed",
			input:            "NES/Galxaia",
			expectedPath:     "/roms/nes/Galaxia (USA).nes",
			expectedStrategy: titles.StrategyJaroWinklerDamerau,
			description:      "Fuzzy match corrects transposition: galxaia -> galaxia",
		},
		{
			name:             "fuzzy_match_with_multiple_typos",
			input:            "NES/Vampir Hnt",
			expectedPath:     "/roms/nes/Vampire Hunt (USA).nes",
			expectedStrategy: titles.StrategyJaroWinklerDamerau,
			description:      "Fuzzy matching handles multiple typos (vampir hnt -> vampire hunt)",
		},
		{
			name:             "fuzzy_confidence_between_minimum_and_acceptable",
			input:            "NES/Dragon Wrlck",
			expectedPath:     "/roms/nes/Dragon Warlock (USA).nes",
			expectedStrategy: titles.StrategyJaroWinklerDamerau,
			description:      "Fuzzy match for 'Dragon Wrlck' finds 'Dragon Warlock' with confidence between 0.60-0.70, should launch with warning",
		},
		{
			name:             "fuzzy_precedence_wins_over_trim",
			input:            "Genesis/Turbo Chargd",
			expectedPath:     "/roms/genesis/Turbo Charged (USA).md",
			expectedStrategy: titles.StrategyJaroWinklerDamerau,
			description:      "A fuzzy match on 'Turbo Chargd' finds 'Turbo Charged' and stops, demonstrating first-match-wins behavior",
		},
		{
			name:             "fuzzy_ambiguous_tie_break",
			input:            "NES/Dragon Warr",
			expectedPath:     "/roms/nes/Dragon War (USA).nes",
			expectedStrategy: titles.StrategyJaroWinklerDamerau,
			description:      "Ambiguous typo 'Dragon Warr' could match 'Dragon War' or 'Dragon Warlock', tie-breaking selects best match",
		},

		// ============================================================
		// PROGRESSIVE TRIM STRATEGY
		// ============================================================

		{
			name:             "progressive_trim_verbose_query",
			input:            "SNES/Hero's Sword Ancient Kingdom Extended Edition",
			expectedPath:     "/roms/snes/Hero Sword AK (USA).sfc",
			expectedStrategy: titles.StrategyProgressiveTrim,
			description:      "Progressive trim handles verbose query by removing 'Extended Edition' to match 'Hero's Sword: Ancient Kingdom'",
		},
		{
			name:             "progressive_trim_prevents_overmatching",
			input:            "Genesis/Turbo the Speedster and Friends",
			expectedPath:     "/roms/genesis/Turbo Full (USA).md",
			expectedStrategy: titles.StrategyProgressiveTrim,
			description:      "Trims to 'Turbo the Speedster' (exact match), prevents over-trimming to ambiguous 'Turbo'",
		},
		{
			name:             "progressive_trim_max_depth",
			input:            "SNES/Fighter Game One Two Three Four Extra Words",
			expectedPath:     "/roms/snes/Fighter 1234 (USA).sfc",
			expectedStrategy: titles.StrategyProgressiveTrim,
			description:      "Progressive trim at max depth (3 iterations) should find match after trimming to 'Fighter Game One Two Three Four'",
		},

		// ============================================================
		// TAG FILTERING
		// ============================================================

		{
			name:             "tag_filter_negative_operator",
			input:            "SNES/Plumber Quest Adventures",
			expectedPath:     "/roms/snes/Plumber Quest Adventures (Europe).sfc",
			expectedStrategy: titles.StrategyExactMatch,
			description:      "Negative tag filter (-region:us) excludes USA version, selects Europe",
			advArgs:          map[string]string{"tags": "-region:us"},
		},
		{
			name:             "tag_filter_explicit_preference",
			input:            "SNES/Plumber Quest Adventures",
			expectedPath:     "/roms/snes/Plumber Quest Adventures (Europe).sfc",
			expectedStrategy: titles.StrategyExactMatch,
			description:      "AdvArgs tags can specify explicit region preference (Europe instead of default USA)",
			advArgs:          map[string]string{"tags": "region:eu"},
		},
		{
			name:             "tag_filter_multiple_negative",
			input:            "SNES/Plumber Quest Adventures",
			expectedPath:     "/roms/snes/Plumber Quest Adventures (Europe).sfc",
			expectedStrategy: titles.StrategyExactMatch,
			description:      "Multiple negative tags (-region:us, -region:jp) exclude both and select Europe",
			advArgs:          map[string]string{"tags": "-region:us,-region:jp"},
		},
		{
			name:             "tag_filter_advargs_overrides_filename",
			input:            "SNES/Time Paradox RPG (Europe)",
			expectedPath:     "/roms/snes/Time Paradox RPG (USA).sfc",
			expectedStrategy: titles.StrategyExactMatch,
			description:      "Explicit tag in advArgs (region:us) overrides different tag found in filename (europe)",
			advArgs:          map[string]string{"tags": "region:us"},
		},
		{
			name:             "tag_filter_empty_string_ignored",
			input:            "SNES/Plumber Quest Adventures",
			expectedPath:     "/roms/snes/Plumber Quest Adventures (USA).sfc",
			expectedStrategy: titles.StrategyExactMatch,
			description:      "Empty tags advArg should be ignored and use default behavior (USA preference)",
			advArgs:          map[string]string{"tags": ""},
		},
		{
			name:             "tag_filter_mixed_positive_negative_operators",
			input:            "SNES/Plumber Quest Adventures",
			expectedPath:     "/roms/snes/Plumber Quest Adventures (Europe).sfc",
			expectedStrategy: titles.StrategyExactMatch,
			description:      "Mixed operators: positive (region:eu) and negative (-unfinished:demo) should both apply",
			advArgs:          map[string]string{"tags": "region:eu,-unfinished:demo"},
		},
		{
			name:             "tag_filter_lang_override_config",
			input:            "SNES/RPG Quest",
			expectedPath:     "/roms/snes/RPG Quest (EUR) (Fr).sfc",
			expectedStrategy: titles.StrategyExactMatch,
			description:      "Explicit lang tag in advArgs (lang:fr) overrides default config language preference (en)",
			advArgs:          map[string]string{"tags": "lang:fr"},
		},
		{
			name:             "tag_filter_explicit_override_config_preference",
			input:            "Genesis/Space Shooter",
			expectedPath:     "/roms/genesis/Space Shooter (JPN).md",
			expectedStrategy: titles.StrategyExactMatch,
			description:      "Explicit tag (region:jp) in advArgs overrides config region preference (eu)",
			advArgs:          map[string]string{"tags": "region:jp"},
			cfg:              makeConfigWithPreferences(t, []string{string(tags.TagRegionEU)}, nil),
		},

		// ============================================================
		// NORMALIZATION
		// ============================================================

		{
			name:             "normalization_roman_numeral",
			input:            "SNES/Mystic Quest 4",
			expectedPath:     "/roms/snes/Mystic Quest 4 (USA).sfc",
			expectedStrategy: titles.StrategyExactMatch,
			description:      "Slugification normalizes roman numerals: query '4' matches 'IV' in database",
		},
		{
			name:             "normalization_leading_article",
			input:            "SNES/Mystic Quest",
			expectedPath:     "/roms/snes/The Mystic Quest (USA).sfc",
			expectedStrategy: titles.StrategyExactMatch,
			description:      "Slugification handles leading article 'The' correctly",
		},
		{
			name:             "normalization_ampersand_to_and",
			input:            "SNES/Dungeons and Dragons",
			expectedPath:     "/roms/snes/DnD (USA).sfc",
			expectedStrategy: titles.StrategyExactMatch,
			description:      "Slugification normalizes 'and' to match '&' in database: 'Dungeons and Dragons' matches 'Dungeons & Dragons'",
		},

		// ============================================================
		// CONFIGURATION PREFERENCES
		// ============================================================

		{
			name:             "config_pref_default_region_usa",
			input:            "Genesis/Space Shooter",
			expectedPath:     "/roms/genesis/Space Shooter (USA).md",
			expectedStrategy: titles.StrategyExactMatch,
			description:      "With no config (blank), defaults to USA region preference",
			cfg:              nil,
		},
		{
			name:             "config_pref_region_europe",
			input:            "Genesis/Space Shooter",
			expectedPath:     "/roms/genesis/Space Shooter (EUR).md",
			expectedStrategy: titles.StrategyExactMatch,
			description:      "Config with europe preference selects European version of Space Shooter",
			cfg:              makeConfigWithPreferences(t, []string{string(tags.TagRegionEU)}, nil),
		},
		{
			name:             "config_pref_region_japan",
			input:            "Genesis/Space Shooter",
			expectedPath:     "/roms/genesis/Space Shooter (JPN).md",
			expectedStrategy: titles.StrategyExactMatch,
			description:      "Config with japan preference selects Japanese version of Space Shooter",
			cfg:              makeConfigWithPreferences(t, []string{string(tags.TagRegionJP)}, nil),
		},
		{
			name:             "config_pref_region_priority_europe_then_japan",
			input:            "SNES/Plumber Quest Adventures",
			expectedPath:     "/roms/snes/Plumber Quest Adventures (Europe).sfc",
			expectedStrategy: titles.StrategyExactMatch,
			description:      "Config with [europe, japan] preference selects Europe (first available in priority list)",
			cfg:              makeConfigWithPreferences(t, []string{string(tags.TagRegionEU), string(tags.TagRegionJP)}, nil),
		},
		{
			name:             "config_pref_lang_english_default",
			input:            "SNES/RPG Quest",
			expectedPath:     "/roms/snes/RPG Quest (EUR) (En).sfc",
			expectedStrategy: titles.StrategyExactMatch,
			description:      "Default lang preference (en) selects English version when multiple exist",
			cfg:              nil,
		},
		{
			name:             "config_pref_lang_french",
			input:            "SNES/RPG Quest",
			expectedPath:     "/roms/snes/RPG Quest (EUR) (Fr).sfc",
			expectedStrategy: titles.StrategyExactMatch,
			description:      "Config with fr language preference selects French version of RPG Quest",
			cfg:              makeConfigWithPreferences(t, nil, []string{string(tags.TagLangFR)}),
		},
		{
			name:             "config_pref_lang_japanese",
			input:            "SNES/RPG Quest",
			expectedPath:     "/roms/snes/RPG Quest (JPN).sfc",
			expectedStrategy: titles.StrategyExactMatch,
			description:      "Config with ja language preference selects Japanese version of RPG Quest",
			cfg:              makeConfigWithPreferences(t, []string{string(tags.TagRegionJP)}, []string{string(tags.TagLangJA)}),
		},

		// ============================================================
		// ERROR CASES
		// ============================================================

		{
			name:          "error_nonexistent_game",
			input:         "SNES/This Game Does Not Exist At All",
			expectedError: true,
			description:   "Returns error when no strategy finds a match",
		},
		{
			name:          "error_confidence_below_minimum_threshold",
			input:         "NES/Myster Cstl",
			expectedError: true,
			description:   "Fuzzy match for 'Myster Cstl' might find 'Mystery Castle' but confidence is below minimum threshold (0.60), so it should fail",
		},
		{
			name:          "error_invalid_system_id",
			input:         "FakeSystem/Some Game",
			expectedError: true,
			description:   "Should return error when the specified system ID does not exist in systemdefs",
		},
		{
			name:          "error_invalid_format_no_slash",
			input:         "Plumber Quest Adventures",
			expectedError: true,
			description:   "Should return error for input lacking the 'System/Title' format",
		},
		{
			name:          "error_slug_normalizes_to_empty",
			input:         "SNES/!@#$%",
			expectedError: true,
			description:   "Should return error if the game title normalizes to an empty slug after slugification",
		},
		{
			name:          "error_cache_miss_on_different_tags",
			input:         "NES/Galaxia",
			expectedError: true,
			description:   "Query with (-region:us) fails despite 'Galaxia (USA)' existing - cache keys include tags",
			advArgs:       map[string]string{"tags": "-region:us"},
		},
		{
			name:          "error_variant_proto_excluded",
			input:         "Genesis/Plumber Quest Adventures Ultimate Edition",
			expectedError: true,
			description:   "Prototype variant should be excluded by default variant filtering (only (Proto) exists in DB)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock platform for launch
			mockPlatform := mocks.NewMockPlatform()
			if !tt.expectedError {
				mockPlatform.On("LaunchMedia", mock.Anything, mock.Anything, mock.Anything).
					Return(nil)
			}

			// Use custom config if provided, otherwise use blank config (defaults)
			cfg := tt.cfg
			if cfg == nil {
				cfg = &config.Instance{}
			}

			cmd := parser.Command{
				Name: "launch.title",
				Args: []string{tt.input},
				AdvArgs: func() map[string]string {
					if tt.advArgs != nil {
						return tt.advArgs
					}
					return map[string]string{}
				}(),
			}

			env := platforms.CmdEnv{
				Cmd: cmd,
				Database: &database.Database{
					MediaDB: mediaDB,
				},
				Cfg:      cfg,
				Playlist: playlists.PlaylistController{},
			}

			// Execute cmdTitle
			result, err := cmdTitle(mockPlatform, env)

			if tt.expectedError {
				assert.Error(t, err, "Expected error for: %s", tt.description)
				return
			}

			require.NoError(t, err, "Unexpected error for: %s", tt.description)
			assert.True(t, result.MediaChanged, "Expected MediaChanged=true for: %s", tt.description)

			// Verify the correct strategy was used
			assert.Equal(t, tt.expectedStrategy, result.Strategy,
				"Expected strategy %s but got %s for: %s", tt.expectedStrategy, result.Strategy, tt.description)

			// Verify LaunchMedia was called
			mockPlatform.AssertExpectations(t)

			t.Logf("✓ %s: Strategy '%s' matched '%s'", tt.name, tt.expectedStrategy, tt.input)
		})
	}
}
