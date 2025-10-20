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
//
// STRATEGY EXECUTION ORDER (from first to last):
//  1. ExactMatch - Exact slug match with tag scoring
//  2. SecondaryTitleExact - Matches secondary title after colon/dash
//  3. MainTitleOnly - Matches main title before colon/dash
//  4. TokenSignature - Word-order-independent matching (2-3 word tokens)
//  5. JaroWinklerDamerau - Fuzzy matching with typo correction
//  6. ProgressiveTrim - Iteratively removes words from end of query
//
// COVERAGE:
//   - ~100 games across 5 systems (SNES, NES, Genesis, N64, PC)
//   - 3 tag types (region, unfinished, lang) with multiple tag values
//   - Intentional "noise" games to ensure matching finds the RIGHT game
//   - Multiple regions/variants/languages for preference testing
//   - Special characters, unicode, ordinals for normalization testing
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

	// ============================================================
	// GAME DATA INSERTION
	// ============================================================
	// Games are organized by system and testing purpose.
	// Many games serve as "noise" to prevent false positive matches.
	//
	// SNES: Exact matching, region/variant filtering, normalization
	// NES: Fuzzy matching, typo correction, confidence boundaries
	// Genesis: Token signature, ambiguity, mixed strategies
	// N64: Secondary titles (colon/dash), main title only
	// PC: Token signature, word-order independence
	// ============================================================

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
	// Noise: Other PC games to ensure token signature doesn't overmatch
	addGame(insertedPC.DBID, "Crystal Defender", "/games/pc/crystaldefender/cd.exe")
	addGame(insertedPC.DBID, "Zombie Attack", "/games/pc/zombieattack/za.exe")
	addGame(insertedPC.DBID, "Neon Runner", "/games/pc/neonrunner/nr.exe")
	// For 2-word token reversal tests (both variants exist for precedence tests)
	addGame(insertedPC.DBID, "Robot Wars", "/games/pc/robotwars/rw.exe") // "robotwars" 9 chars, 2 words
	addGame(insertedPC.DBID, "Wars Robot", "/games/pc/warsrobot/wr.exe") // Reversed for token test
	// For actual token signature matching (only one variant exists)
	addGame(insertedPC.DBID, "Battle Mech", "/games/pc/battlemech/bm.exe")     // Only this order exists
	addGame(insertedPC.DBID, "Shadow Knight", "/games/pc/shadowknight/sk.exe") // Only this order exists
	// For partial token and basic token tests
	addGame(insertedPC.DBID, "Space Quest", "/games/pc/spacequest/sq.exe") // "spacequest" 10 chars, 2 words
	// For token ambiguity tests (multiple games with "Space")
	addGame(insertedPC.DBID, "Space Marines", "/games/pc/spacemarines/sm.exe")
	addGame(insertedPC.DBID, "Space Invaders", "/games/pc/spaceinvaders/si.exe")
	// For 4+ word token signature (should use first 3 words only)
	addGame(insertedPC.DBID, "Epic Battle Arena Championship", "/games/pc/epic_battle_arena.exe")
	// For reversed 4-word test
	addGame(insertedPC.DBID, "Championship Arena Battle", "/games/pc/champ_arena_battle.exe")

	// Additional test data for Priority 1 critical tests
	// For strategy precedence testing
	addGame(insertedGenesis.DBID, "Turbo Charged Racing (USA)",
		"/roms/genesis/Turbo Charged Racing (USA).md", usaTag.DBID)

	// For ambiguity/tie-breaking tests
	addGame(insertedGenesis.DBID, "Storm Warrior (Europe)", "/roms/genesis/Storm Warrior (EU).md", europeTag.DBID)
	addGame(insertedGenesis.DBID, "Storm Warrior (USA)", "/roms/genesis/Storm Warrior (USA).md", usaTag.DBID)

	// For token signature with region preference (multiple regions, same token pattern)
	addGame(insertedGenesis.DBID, "Shadow Knight (USA)", "/roms/genesis/Shadow Knight (USA).md", usaTag.DBID)
	addGame(insertedGenesis.DBID, "Shadow Knight (Europe)", "/roms/genesis/Shadow Knight (EUR).md", europeTag.DBID)
	addGame(insertedGenesis.DBID, "Shadow Knight (Japan)", "/roms/genesis/Shadow Knight (JPN).md", japanTag.DBID)
	// Reversed token order version (for exact match precedence test)
	addGame(insertedGenesis.DBID, "Knight Shadow (USA)", "/roms/genesis/Knight Shadow (USA).md", usaTag.DBID)
	// For actual token signature with region preference (only one word order exists, multiple regions)
	addGame(insertedGenesis.DBID, "Thunder Storm (USA)", "/roms/genesis/Thunder Storm (USA).md", usaTag.DBID)
	addGame(insertedGenesis.DBID, "Thunder Storm (Europe)", "/roms/genesis/Thunder Storm (EUR).md", europeTag.DBID)
	addGame(insertedGenesis.DBID, "Thunder Storm (Japan)", "/roms/genesis/Thunder Storm (JPN).md", japanTag.DBID)

	// For strategy precedence tests
	// Main title vs token signature: "Steel Warriors: Ultimate Battle" where query could match main or use tokens
	addGame(insertedN64.DBID, "Steel Warriors: Ultimate Battle (USA)", "/roms/n64/Steel Warriors UB (USA).z64",
		usaTag.DBID)
	// Token signature alternative with reversed words
	addGame(insertedN64.DBID, "Battle Steel (USA)", "/roms/n64/Battle Steel (USA).z64", usaTag.DBID)
	// For token signature vs fuzzy precedence
	addGame(insertedGenesis.DBID, "Cyber Strike (USA)", "/roms/genesis/Cyber Strike (USA).md", usaTag.DBID)
	// Fuzzy-close alternative
	addGame(insertedGenesis.DBID, "Cybre Strike Force (USA)", "/roms/genesis/Cybre Strike Force (USA).md", usaTag.DBID)

	// For main title only strategy tests
	// Very short main title
	addGame(insertedN64.DBID, "Pro: Championship Edition (USA)", "/roms/n64/Pro Champ (USA).z64", usaTag.DBID)
	addGame(insertedN64.DBID, "Pro: Racing League (USA)", "/roms/n64/Pro Racing (USA).z64", usaTag.DBID)
	// Same main title, multiple regions for ambiguity
	addGame(insertedN64.DBID, "Racing: Grand Prix (USA)", "/roms/n64/Racing GP USA (USA).z64", usaTag.DBID)
	addGame(insertedN64.DBID, "Racing: Grand Prix (Europe)", "/roms/n64/Racing GP EUR (EUR).z64", europeTag.DBID)
	addGame(insertedN64.DBID, "Racing: World Tour (USA)", "/roms/n64/Racing World (USA).z64", usaTag.DBID)

	// For secondary title strategy tests
	// Very short secondary title
	addGame(insertedN64.DBID, "Legends: VR (USA)", "/roms/n64/Legends VR (USA).z64", usaTag.DBID)
	// Secondary title with multiple regions
	addGame(insertedN64.DBID, "Warriors: Shadow Realm (USA)", "/roms/n64/Warriors Shadow USA (USA).z64", usaTag.DBID)
	addGame(insertedN64.DBID, "Warriors: Shadow Realm (Europe)", "/roms/n64/Warriors Shadow EUR (EUR).z64",
		europeTag.DBID)
	addGame(insertedN64.DBID, "Warriors: Shadow Realm (Japan)", "/roms/n64/Warriors Shadow JPN (JPN).z64",
		japanTag.DBID)
	// Ambiguous secondary title (multiple games with same secondary)
	addGame(insertedN64.DBID, "Battle: Arena Masters (USA)", "/roms/n64/Battle Arena 1 (USA).z64", usaTag.DBID)
	addGame(insertedN64.DBID, "Combat: Arena Masters (USA)", "/roms/n64/Combat Arena 1 (USA).z64", usaTag.DBID)

	// For confidence threshold tests
	addGame(insertedNES.DBID, "Dragon Warlock (USA)", "/roms/nes/Dragon Warlock (USA).nes", usaTag.DBID)
	addGame(insertedNES.DBID, "Mystery Castle (USA)", "/roms/nes/Mystery Castle (USA).nes", usaTag.DBID)
	// For precise confidence boundary testing
	// "Precision" for high confidence test (exact match baseline)
	addGame(insertedNES.DBID, "Precision (USA)", "/roms/nes/Precision (USA).nes", usaTag.DBID)
	// "Thunder" for 0.90+ confidence test (1 char typo)
	addGame(insertedNES.DBID, "Thunder (USA)", "/roms/nes/Thunder (USA).nes", usaTag.DBID)
	// "Striker" for 0.70-0.90 confidence test (moderate typos)
	addGame(insertedNES.DBID, "Striker (USA)", "/roms/nes/Striker (USA).nes", usaTag.DBID)
	// "Phoenix" for boundary testing
	addGame(insertedNES.DBID, "Phoenix (USA)", "/roms/nes/Phoenix (USA).nes", usaTag.DBID)

	// For comprehensive variant exclusion tests
	// Game with ALL variant types (demo, beta, proto) and a release version
	addGame(insertedGenesis.DBID, "Cyber Warrior (USA)", "/roms/genesis/Cyber Warrior (USA).md", usaTag.DBID)
	addGame(insertedGenesis.DBID, "Cyber Warrior (Demo)", "/roms/genesis/Cyber Warrior (Demo).md", demoTag.DBID)
	addGame(insertedGenesis.DBID, "Cyber Warrior (Beta)", "/roms/genesis/Cyber Warrior (Beta).md", betaTag.DBID)
	addGame(insertedGenesis.DBID, "Cyber Warrior (Proto)", "/roms/genesis/Cyber Warrior (Proto).md", protoTag.DBID)
	// Game with ONLY beta variant (no release version)
	addGame(insertedGenesis.DBID, "Lost Project (Beta)", "/roms/genesis/Lost Project (Beta).md", betaTag.DBID)
	// Game with multiple variants but no release
	addGame(insertedNES.DBID, "Ancient Ruins (Demo)", "/roms/nes/Ancient Ruins (Demo).nes", demoTag.DBID)
	addGame(insertedNES.DBID, "Ancient Ruins (Beta)", "/roms/nes/Ancient Ruins (Beta).nes", betaTag.DBID)

	// For progressive trim tests
	// Trim-induced ambiguity: "Legend" matches multiple games after trimming
	addGame(insertedSNES.DBID, "Legend Quest (USA)", "/roms/snes/Legend Quest (USA).sfc", usaTag.DBID)
	addGame(insertedSNES.DBID, "Legend Warriors (USA)", "/roms/snes/Legend Warriors (USA).sfc", usaTag.DBID)
	// Very long query for max depth testing (8+ words)
	addGame(insertedSNES.DBID, "Epic Adventure Quest Chronicles Part One (USA)",
		"/roms/snes/Epic Adventure 1 (USA).sfc", usaTag.DBID)
	// For minimum word count test (very short base name)
	addGame(insertedGenesis.DBID, "Pro (USA)", "/roms/genesis/Pro Game (USA).md", usaTag.DBID)

	// For special characters normalization tests
	// Different apostrophe types
	addGame(insertedSNES.DBID, "Hero's Quest (USA)", "/roms/snes/Hero Quest (USA).sfc", usaTag.DBID)
	// Ordinal numbers
	addGame(insertedGenesis.DBID, "Street Fighter 2nd Edition (USA)", "/roms/genesis/SF 2nd (USA).md", usaTag.DBID)
	// Multiple consecutive special chars
	addGame(insertedNES.DBID, "Mega Man!!! (USA)", "/roms/nes/Mega Man (USA).nes", usaTag.DBID)
	// Mixed unicode and special chars
	addGame(insertedSNES.DBID, "Pokémon™ Edition (USA)", "/roms/snes/Pokemon Ed (USA).sfc", usaTag.DBID)

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
//
// TEST ORGANIZATION:
//   - Exact Match Strategy
//   - Strategy Precedence
//   - Secondary Title Exact Match
//   - Main Title Only
//   - Token Signature
//   - Fuzzy Match (Jaro-Winkler)
//   - Confidence Threshold Boundaries
//   - Progressive Trim
//   - Variant Exclusion
//   - Tag Filtering
//   - Normalization
//   - Configuration Preferences
//   - Error Cases
//   - Negative Tests Per Strategy
//
// All tests share the SAME database to verify realistic matching behavior.
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
		// Tests slug-based exact matching with:
		// - Region/language preference selection
		// - Variant filtering (demo/beta/proto exclusion)
		// - Tag scoring and tie-breaking
		// - Normalization (unicode, articles, roman numerals)
		// - Edge cases (very short/long titles, special chars)
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
			name:             "variant_exclusion_beta_excluded",
			input:            "Genesis/Cyber Warrior",
			expectedPath:     "/roms/genesis/Cyber Warrior (USA).md",
			expectedStrategy: titles.StrategyExactMatch,
			description:      "Beta variant is automatically excluded, selects release version",
		},
		{
			name:             "variant_exclusion_all_types_excluded",
			input:            "Genesis/Cyber Warrior",
			expectedPath:     "/roms/genesis/Cyber Warrior (USA).md",
			expectedStrategy: titles.StrategyExactMatch,
			description: "When release, demo, beta, and proto exist, all variants excluded " +
				"and release version selected",
		},
		{
			name:             "variant_exclusion_explicit_demo_inclusion",
			input:            "Genesis/Cyber Warrior",
			expectedPath:     "/roms/genesis/Cyber Warrior (Demo).md",
			expectedStrategy: titles.StrategyExactMatch,
			description:      "Explicit demo tag overrides default exclusion and selects demo variant",
			advArgs:          map[string]string{"tags": "unfinished:demo"},
		},
		{
			name:             "variant_exclusion_explicit_beta_inclusion",
			input:            "Genesis/Cyber Warrior",
			expectedPath:     "/roms/genesis/Cyber Warrior (Beta).md",
			expectedStrategy: titles.StrategyExactMatch,
			description:      "Explicit beta tag overrides default exclusion and selects beta variant",
			advArgs:          map[string]string{"tags": "unfinished:beta"},
		},
		{
			name:             "variant_exclusion_explicit_proto_inclusion",
			input:            "Genesis/Cyber Warrior",
			expectedPath:     "/roms/genesis/Cyber Warrior (Proto).md",
			expectedStrategy: titles.StrategyExactMatch,
			description:      "Explicit proto tag overrides default exclusion and selects proto variant",
			advArgs:          map[string]string{"tags": "unfinished:proto"},
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
			description: "Exact slug match with perfect tag alignment triggers early exit at " +
				"high confidence threshold",
		},
		{
			name:             "exact_match_tie_break_with_region_preference",
			input:            "Genesis/Storm Warrior",
			expectedPath:     "/roms/genesis/Storm Warrior (USA).md",
			expectedStrategy: titles.StrategyExactMatch,
			description: "When multiple exact matches exist (USA and Europe), default region preference " +
				"(USA) breaks the tie",
		},
		{
			name:             "exact_match_full_title_with_dash",
			input:            "Nintendo64/Adventure - Temple",
			expectedPath:     "/roms/n64/Adventure Temple Alt (USA).z64",
			expectedStrategy: titles.StrategyExactMatch,
			description: "Full title with dash 'Adventure - Temple' matches database entry " +
				"(slug: adventuretemple)",
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
			description: "When multiple exact slug matches exist (USA, Europe, no-region), tag scoring " +
				"selects USA as highest preference",
		},
		{
			name:             "exact_match_variant_inclusion_with_explicit_tag",
			input:            "SNES/Plumber Quest Adventures",
			expectedPath:     "/roms/snes/Plumber Quest Adventures (Demo).sfc",
			expectedStrategy: titles.StrategyExactMatch,
			description: "Explicit tag (unfinished:demo) overrides default variant exclusion and " +
				"finds demo game",
			advArgs: map[string]string{"tags": "unfinished:demo"},
		},
		{
			name:             "exact_match_very_short_title_1_char",
			input:            "NES/Q",
			expectedPath:     "/roms/nes/Q (USA).nes",
			expectedStrategy: titles.StrategyExactMatch,
			description: "Single character title should match correctly " +
				"(tests minimum slug length handling)",
		},
		{
			name:             "exact_match_very_long_title",
			input:            "SNES/The Super Ultimate Mega Hyper Championship Tournament Edition Deluxe",
			expectedPath:     "/roms/snes/Super Ultimate Long (USA).sfc",
			expectedStrategy: titles.StrategyExactMatch,
			description: "Very long title (50+ chars) should match correctly without " +
				"truncation issues",
		},
		{
			name:             "exact_match_unicode_accent_normalization",
			input:            "SNES/Pokemon Stadium",
			expectedPath:     "/roms/snes/Pokemon Stadium (USA).sfc",
			expectedStrategy: titles.StrategyExactMatch,
			description: "Query without accent 'Pokemon' matches database entry with accent 'Pokémon' " +
				"via unicode normalization",
		},
		{
			name:             "exact_match_multiple_colons_in_title",
			input:            "Nintendo64/Adventure Game: Part 1: The Beginning",
			expectedPath:     "/roms/n64/Adventure Part 1 (USA).z64",
			expectedStrategy: titles.StrategyExactMatch,
			description: "Query with multiple colons matches exactly " +
				"(colons stripped during slugification)",
		},
		{
			name:             "exact_match_prefilter_extreme_length_diff",
			input:            "Genesis/X",
			expectedPath:     "/roms/genesis/X (USA).md",
			expectedStrategy: titles.StrategyExactMatch,
			description: "Very short query (1 char) should not be excluded by fuzzy prefilter " +
				"length constraints",
		},

		// ============================================================
		// STRATEGY PRECEDENCE & EXECUTION ORDER
		// Verifies strategies run in correct order and first-match wins:
		// 1. Exact > Fuzzy
		// 2. Secondary Title > Main Title Only
		// 3. Main Title Only > Token Signature
		// 4. Token Signature > Fuzzy
		// 5. High confidence (0.90+) triggers early exit
		// ============================================================

		{
			name:             "precedence_exact_match_wins_over_fuzzy",
			input:            "Genesis/Turbo",
			expectedPath:     "/roms/genesis/Turbo (USA).md",
			expectedStrategy: titles.StrategyExactMatch,
			description: "Exact match 'Turbo' wins even though fuzzy matches exist " +
				"(Turbu → Turbo the Speedster, etc.)",
		},
		{
			name:             "precedence_secondary_title_wins_over_main_title",
			input:            "Nintendo64/Crystal Temple",
			expectedPath:     "/roms/n64/Hero Crystal (USA).z64",
			expectedStrategy: titles.StrategySecondaryTitleExact,
			description: "Secondary title exact match wins over main title prefix match " +
				"(strategy execution order)",
		},
		{
			name:             "precedence_main_title_wins_over_token_signature",
			input:            "Nintendo64/Steel Warriors",
			expectedPath:     "/roms/n64/Steel Warriors UB (USA).z64",
			expectedStrategy: titles.StrategyMainTitleOnly,
			description: "Main title only match 'Steel Warriors' wins over " +
				"token signature alternatives",
		},
		{
			name:             "precedence_token_signature_wins_over_fuzzy",
			input:            "Genesis/Strike Cyber",
			expectedPath:     "/roms/genesis/Cyber Strike (USA).md",
			expectedStrategy: titles.StrategyTokenSignature,
			description: "Token signature match (reversed 'Strike Cyber' → 'Cyber Strike') wins " +
				"before fuzzy matching attempts",
		},
		{
			name:             "precedence_early_exit_on_high_confidence",
			input:            "SNES/Dragon Warrior Chronicles",
			expectedPath:     "/roms/snes/Dragon Warrior (USA).sfc",
			expectedStrategy: titles.StrategyExactMatch,
			description: "High confidence exact match (0.90+) triggers early exit, prevents running " +
				"subsequent strategies",
		},
		{
			name:             "precedence_first_match_wins_within_strategy",
			input:            "Genesis/Storm Warrior",
			expectedPath:     "/roms/genesis/Storm Warrior (USA).md",
			expectedStrategy: titles.StrategyExactMatch,
			description: "When multiple matches exist with same confidence in exact match, first by " +
				"tag preference (USA) wins",
		},

		// ============================================================
		// SECONDARY TITLE EXACT MATCH STRATEGY
		// Matches text after colon/dash separator in game titles
		// - Colon-separated: "Main: Secondary" matches "Secondary"
		// - Dash-separated: "Main - Secondary" matches "Secondary"
		// - Multiple colons: uses last segment as secondary
		// - Region preference and ambiguity handling
		// ============================================================

		{
			name:             "secondary_title_exact_match_crystal_temple",
			input:            "Nintendo64/Crystal Temple",
			expectedPath:     "/roms/n64/Hero Crystal (USA).z64",
			expectedStrategy: titles.StrategySecondaryTitleExact,
			description: "Token 'Crystal Temple' matches DB entry 'Hero's Adventure: Crystal Temple' " +
				"via SecondarySlug column",
		},
		{
			name:             "secondary_title_exact_match_dark_moon",
			input:            "Nintendo64/Dark Moon",
			expectedPath:     "/roms/n64/Hero Dark Moon (USA).z64",
			expectedStrategy: titles.StrategySecondaryTitleExact,
			description: "Token 'Dark Moon' matches DB entry 'Hero's Adventure: Dark Moon' " +
				"via SecondarySlug column",
		},
		{
			name:             "secondary_title_dash_separated",
			input:            "Nintendo64/Temple",
			expectedPath:     "/roms/n64/Adventure Temple Alt (USA).z64",
			expectedStrategy: titles.StrategySecondaryTitleExact,
			description: "Secondary title from dash-separated 'Adventure - Temple' " +
				"matches query 'Temple'",
		},
		{
			name:             "secondary_title_very_short",
			input:            "Nintendo64/VR",
			expectedPath:     "/roms/n64/Legends VR (USA).z64",
			expectedStrategy: titles.StrategySecondaryTitleExact,
			description:      "Very short secondary title 'VR' (2 chars) matches correctly",
		},
		{
			name:             "secondary_title_with_region_preference",
			input:            "Nintendo64/Shadow Realm",
			expectedPath:     "/roms/n64/Warriors Shadow USA (USA).z64",
			expectedStrategy: titles.StrategySecondaryTitleExact,
			description: "Secondary title 'Shadow Realm' with multiple regions selects USA " +
				"(default preference)",
		},
		{
			name:             "secondary_title_ambiguous_multiple_games",
			input:            "Nintendo64/Arena Masters",
			expectedPath:     "/roms/n64/Battle Arena 1 (USA).z64",
			expectedStrategy: titles.StrategySecondaryTitleExact,
			description: "Ambiguous secondary title 'Arena Masters' matches multiple games, selects " +
				"first by natural order",
		},
		// ============================================================
		// MAIN TITLE ONLY STRATEGY
		// Matches text before colon/dash in titles with separators
		// - Prefix matching on main title portion
		// - Very short main titles (< 5 chars)
		// - Multiple games with same main title, different secondary
		// - Region preference tie-breaking
		// ============================================================

		{
			name:             "main_title_prefix_match",
			input:            "Nintendo64/Hero's Adventure",
			expectedPath:     "/roms/n64/Hero Crystal (USA).z64",
			expectedStrategy: titles.StrategyMainTitleOnly,
			description: "Token 'Hero's Adventure' matches DB entries via prefix search and " +
				"main title post-filter",
		},
		{
			name:             "main_title_multiple_matches_with_different_secondary",
			input:            "Nintendo64/Hero's Adventure",
			expectedPath:     "/roms/n64/Hero Crystal (USA).z64",
			expectedStrategy: titles.StrategyMainTitleOnly,
			description: "Main title 'Hero's Adventure' matches multiple games (Crystal Temple, Dark Moon), " +
				"selects first by natural order",
		},
		{
			name:             "main_title_very_short_title",
			input:            "Nintendo64/Pro",
			expectedPath:     "/roms/n64/Pro Champ (USA).z64",
			expectedStrategy: titles.StrategyMainTitleOnly,
			description:      "Very short main title 'Pro' (3 chars) matches games with colon separator",
		},
		{
			name:             "main_title_ambiguity_with_region_preference",
			input:            "Nintendo64/Racing",
			expectedPath:     "/roms/n64/Racing GP USA (USA).z64",
			expectedStrategy: titles.StrategyMainTitleOnly,
			description: "Main title 'Racing' matches multiple games, region preference (USA) " +
				"selects correct one",
		},
		{
			name:             "main_title_with_different_secondary_titles",
			input:            "Nintendo64/Racing",
			expectedPath:     "/roms/n64/Racing GP USA (USA).z64",
			expectedStrategy: titles.StrategyMainTitleOnly,
			description: "Main title matches 'Racing: Grand Prix' and 'Racing: World Tour', selects " +
				"by tag preference",
		},

		// ============================================================
		// TOKEN SIGNATURE STRATEGY
		// Word-order-independent matching using 2-3 word tokens
		// - 2-word reversals: "Mech Battle" → "Battle Mech" (order-independent)
		// - 3-word combinations: any permutation matches
		// - 4+ word queries use first 3 words only
		// - Partial token matching (single word to multi-word)
		// - Region preference for multiple matches
		// ============================================================

		{
			name:             "token_signature_2_word_reversal",
			input:            "PC/Mech Battle",
			expectedPath:     "/games/pc/battlemech/bm.exe",
			expectedStrategy: titles.StrategyTokenSignature,
			description: "Token signature matches 'Mech Battle' to 'Battle Mech' " +
				"(word-order independent, 2 tokens)",
		},
		{
			name:             "token_signature_2_word_reversal_query_reversed",
			input:            "PC/Knight Shadow",
			expectedPath:     "/games/pc/shadowknight/sk.exe",
			expectedStrategy: titles.StrategyTokenSignature,
			description: "Token signature matches 'Knight Shadow' to 'Shadow Knight' " +
				"(word-order independent, 2 tokens)",
		},
		{
			name:             "token_signature_3_word_match",
			input:            "PC/Crystal Space Quest",
			expectedPath:     "/games/pc/crystal_space_quest.exe",
			expectedStrategy: titles.StrategyTokenSignature,
			description: "Token signature matches 3-word reversal: 'Crystal Space Quest' matches " +
				"'Quest Space Crystal'",
		},
		{
			name:             "token_signature_4_word_uses_first_3",
			input:            "PC/Battle Epic Arena Championship",
			expectedPath:     "/games/pc/epic_battle_arena.exe",
			expectedStrategy: titles.StrategyTokenSignature,
			description: "Token signature with 4+ words uses first 3 tokens only: 'Battle Epic Arena' " +
				"matches 'Epic Battle Arena'",
		},
		{
			name:             "token_signature_4_word_reversed",
			input:            "PC/Arena Battle Championship",
			expectedPath:     "/games/pc/champ_arena_battle.exe",
			expectedStrategy: titles.StrategyTokenSignature,
			description: "Token signature with 4+ words matches via first 3 tokens: 'Arena Battle Championship' " +
				"matches 'Championship Arena Battle'",
		},
		{
			name:             "token_signature_with_region_preference",
			input:            "Genesis/Storm Thunder",
			expectedPath:     "/roms/genesis/Thunder Storm (USA).md",
			expectedStrategy: titles.StrategyTokenSignature,
			description: "Token signature 'Storm Thunder' matches 'Thunder Storm' with multiple regions " +
				"(USA/Europe/Japan), selects USA (default preference)",
		},
		{
			name:             "token_signature_ambiguity_prefers_exact_over_partial",
			input:            "PC/Space Quest",
			expectedPath:     "/games/pc/spacequest/sq.exe",
			expectedStrategy: titles.StrategyExactMatch,
			description: "When both exact match and token signature possible, exact match wins " +
				"(strategy precedence)",
		},

		// ============================================================
		// FUZZY MATCH STRATEGY - JARO-WINKLER
		// Typo-tolerant matching with confidence scoring
		// - Missing characters: "buble" → "bubble"
		// - Wrong characters: "turbu" → "turbo"
		// - Transpositions: "galxaia" → "galaxia"
		// - Multiple typos in single query
		// - Ambiguity tie-breaking
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
			description: "Fuzzy matching handles multiple typos " +
				"(vampir hnt -> vampire hunt)",
		},
		{
			name:             "fuzzy_confidence_between_minimum_and_acceptable",
			input:            "NES/Dragon Wrlck",
			expectedPath:     "/roms/nes/Dragon Warlock (USA).nes",
			expectedStrategy: titles.StrategyJaroWinklerDamerau,
			description: "Fuzzy match for 'Dragon Wrlck' finds 'Dragon Warlock' with confidence between " +
				"0.60-0.70, should launch with warning",
		},
		{
			name:             "fuzzy_precedence_wins_over_trim",
			input:            "Genesis/Turbo Chargd",
			expectedPath:     "/roms/genesis/Turbo Charged (USA).md",
			expectedStrategy: titles.StrategyJaroWinklerDamerau,
			description: "A fuzzy match on 'Turbo Chargd' finds 'Turbo Charged' and stops, demonstrating " +
				"first-match-wins behavior",
		},
		{
			name:             "fuzzy_ambiguous_tie_break",
			input:            "NES/Dragon Warr",
			expectedPath:     "/roms/nes/Dragon War (USA).nes",
			expectedStrategy: titles.StrategyJaroWinklerDamerau,
			description: "Ambiguous typo 'Dragon Warr' could match 'Dragon War' or 'Dragon Warlock', " +
				"tie-breaking selects best match",
		},

		// ============================================================
		// CONFIDENCE THRESHOLD BOUNDARIES
		// Tests fuzzy matching at specific confidence levels:
		// - 0.90+: Very high confidence, silent launch
		// - 0.70-0.90: Acceptable range, launch with warning
		// - Near 0.70: Close to minimum, strong warning
		// - < 0.70: Below minimum threshold, match fails
		// ============================================================

		{
			name:             "confidence_very_high_single_char_typo",
			input:            "NES/Thundar",
			expectedPath:     "/roms/nes/Thunder (USA).nes",
			expectedStrategy: titles.StrategyJaroWinklerDamerau,
			description: "Single character typo 'Thundar' → 'Thunder' produces very high confidence (0.90+), " +
				"silent launch",
		},
		{
			name:             "confidence_acceptable_range_moderate_typo",
			input:            "NES/Stricker",
			expectedPath:     "/roms/nes/Striker (USA).nes",
			expectedStrategy: titles.StrategyJaroWinklerDamerau,
			description: "Moderate typo 'Stricker' → 'Striker' produces acceptable confidence (0.70-0.90), " +
				"should launch with warning",
		},
		{
			name:             "confidence_near_minimum_multiple_typos",
			input:            "NES/Phnix",
			expectedPath:     "/roms/nes/Phoenix (USA).nes",
			expectedStrategy: titles.StrategyJaroWinklerDamerau,
			description: "Multiple typos 'Phnix' → 'Phoenix' produces near-minimum confidence (close to 0.70), " +
				"should launch with strong warning",
		},
		{
			name:             "confidence_high_transposition",
			input:            "NES/Preicsion",
			expectedPath:     "/roms/nes/Precision (USA).nes",
			expectedStrategy: titles.StrategyJaroWinklerDamerau,
			description: "Transposition typo 'Preicsion' → 'Precision' produces high confidence " +
				"(Jaro-Winkler handles transpositions well)",
		},

		// ============================================================
		// PROGRESSIVE TRIM STRATEGY
		// Iteratively removes words from end of query until match found
		// - Verbose queries with extra words
		// - Prevents over-trimming (stops at first match)
		// - Max depth limit (3 iterations)
		// - Very long queries (8+ words)
		// - Trim-induced ambiguity handling
		// - Minimum word count enforcement
		// - Fallback to fuzzy after trimming
		// ============================================================

		{
			name:             "progressive_trim_verbose_query",
			input:            "SNES/Hero's Sword Ancient Kingdom Extended Edition",
			expectedPath:     "/roms/snes/Hero Sword AK (USA).sfc",
			expectedStrategy: titles.StrategyProgressiveTrim,
			description: "Progressive trim handles verbose query by removing 'Extended Edition' to match " +
				"'Hero's Sword: Ancient Kingdom'",
		},
		{
			name:             "progressive_trim_prevents_overmatching",
			input:            "Genesis/Turbo the Speedster and Friends",
			expectedPath:     "/roms/genesis/Turbo Full (USA).md",
			expectedStrategy: titles.StrategyProgressiveTrim,
			description: "Trims to 'Turbo the Speedster' (exact match), prevents over-trimming to " +
				"ambiguous 'Turbo'",
		},
		{
			name:             "progressive_trim_max_depth",
			input:            "SNES/Fighter Game One Two Three Four Extra Words",
			expectedPath:     "/roms/snes/Fighter 1234 (USA).sfc",
			expectedStrategy: titles.StrategyProgressiveTrim,
			description: "Progressive trim at max depth (3 iterations) should find match after trimming " +
				"to 'Fighter Game One Two Three Four'",
		},
		{
			name:             "progressive_trim_very_long_query",
			input:            "SNES/Epic Adventure Quest Chronicles Part One The Beginning Extended Edition",
			expectedPath:     "/roms/snes/Epic Adventure 1 (USA).sfc",
			expectedStrategy: titles.StrategyProgressiveTrim,
			description: "Very long query (8+ words) progressively trims until " +
				"finding match",
		},
		{
			name:             "progressive_trim_induced_ambiguity",
			input:            "SNES/Legend Quest Warriors United",
			expectedPath:     "/roms/snes/Legend Quest (USA).sfc",
			expectedStrategy: titles.StrategyProgressiveTrim,
			description: "Progressive trim creates ambiguity: 'Legend Quest Warriors' → 'Legend Quest' " +
				"(matches) vs 'Legend Warriors', selects first match",
		},
		// ============================================================
		// TAG FILTERING
		// Tests advanced tag filtering with positive/negative operators
		// - Negative filters: -region:us excludes USA versions
		// - Explicit preferences: region:eu selects Europe
		// - Multiple negative tags: -region:us,-region:jp
		// - AdvArgs override filename tags
		// - Mixed positive/negative operators
		// - Language tag filtering (lang:fr, lang:en)
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
			description: "Explicit lang tag in advArgs (lang:fr) overrides default config " +
				"language preference (en)",
			advArgs: map[string]string{"tags": "lang:fr"},
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
		// Tests slug normalization for special characters and formatting
		// - Roman numerals: "IV" ↔ "4"
		// - Leading articles: "The Game" → "game"
		// - Ampersand: "Dungeons & Dragons" ↔ "Dungeons and Dragons"
		// - Apostrophes: ' vs ' vs ' (all normalize the same)
		// - Ordinal numbers: "2nd", "3rd"
		// - Multiple consecutive special chars: "Game!!!" → "game"
		// - Unicode: Pokémon → Pokemon
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
			description: "Slugification normalizes 'and' to match '&' in database: 'Dungeons and Dragons' " +
				"matches 'Dungeons & Dragons'",
		},
		{
			name:             "normalization_apostrophe_straight",
			input:            "SNES/Hero's Quest",
			expectedPath:     "/roms/snes/Hero Quest (USA).sfc",
			expectedStrategy: titles.StrategyExactMatch,
			description: "Straight apostrophe (') in query matches game with " +
				"smart apostrophe (')",
		},
		{
			name:             "normalization_apostrophe_smart",
			input:            "SNES/Hero's Quest",
			expectedPath:     "/roms/snes/Hero Quest (USA).sfc",
			expectedStrategy: titles.StrategyExactMatch,
			description: "Smart apostrophe (') in query normalizes and matches " +
				"(apostrophes removed during slugification)",
		},
		{
			name:             "normalization_ordinal_number",
			input:            "Genesis/Street Fighter 2nd Edition",
			expectedPath:     "/roms/genesis/SF 2nd (USA).md",
			expectedStrategy: titles.StrategyExactMatch,
			description: "Ordinal number '2nd' normalizes correctly during " +
				"slugification",
		},
		{
			name:             "normalization_multiple_consecutive_special_chars",
			input:            "NES/Mega Man",
			expectedPath:     "/roms/nes/Mega Man (USA).nes",
			expectedStrategy: titles.StrategyExactMatch,
			description: "Query 'Mega Man' matches 'Mega Man!!!' " +
				"(consecutive special chars removed)",
		},
		{
			name:             "normalization_mixed_unicode_and_special_chars",
			input:            "SNES/Pokemon Edition",
			expectedPath:     "/roms/snes/Pokemon Ed (USA).sfc",
			expectedStrategy: titles.StrategyExactMatch,
			description: "Query with ASCII matches database with unicode (Pokémon) and trademark (™) - " +
				"both normalized",
		},

		// ============================================================
		// CONFIGURATION PREFERENCES
		// Tests user configuration preference handling
		// - Default region: USA (when no config specified)
		// - Custom region preferences: EU, JP
		// - Region priority lists: [EU, JP] selects first available
		// - Language preferences: en, fr, ja
		// - AdvArgs explicit tags override config preferences
		// ============================================================

		{
			name:             "config_pref_default_region_usa",
			input:            "Genesis/Space Shooter",
			expectedPath:     "/roms/genesis/Space Shooter (USA).md",
			expectedStrategy: titles.StrategyExactMatch,
			description: "With no config (blank), defaults to USA region " +
				"preference",
			cfg: nil,
		},
		{
			name:             "config_pref_region_europe",
			input:            "Genesis/Space Shooter",
			expectedPath:     "/roms/genesis/Space Shooter (EUR).md",
			expectedStrategy: titles.StrategyExactMatch,
			description: "Config with europe preference selects European version of " +
				"Space Shooter",
			cfg: makeConfigWithPreferences(t, []string{string(tags.TagRegionEU)}, nil),
		},
		{
			name:             "config_pref_region_japan",
			input:            "Genesis/Space Shooter",
			expectedPath:     "/roms/genesis/Space Shooter (JPN).md",
			expectedStrategy: titles.StrategyExactMatch,
			description: "Config with japan preference selects Japanese version of " +
				"Space Shooter",
			cfg: makeConfigWithPreferences(t, []string{string(tags.TagRegionJP)}, nil),
		},
		{
			name:             "config_pref_region_priority_europe_then_japan",
			input:            "SNES/Plumber Quest Adventures",
			expectedPath:     "/roms/snes/Plumber Quest Adventures (Europe).sfc",
			expectedStrategy: titles.StrategyExactMatch,
			description: "Config with [europe, japan] preference selects Europe " +
				"(first available in priority list)",
			cfg: makeConfigWithPreferences(t, []string{string(tags.TagRegionEU), string(tags.TagRegionJP)}, nil),
		},
		{
			name:             "config_pref_lang_english_default",
			input:            "SNES/RPG Quest",
			expectedPath:     "/roms/snes/RPG Quest (EUR) (En).sfc",
			expectedStrategy: titles.StrategyExactMatch,
			description: "Default lang preference (en) selects English version when " +
				"multiple exist",
			cfg: nil,
		},
		{
			name:             "config_pref_lang_french",
			input:            "SNES/RPG Quest",
			expectedPath:     "/roms/snes/RPG Quest (EUR) (Fr).sfc",
			expectedStrategy: titles.StrategyExactMatch,
			description: "Config with fr language preference selects French version of " +
				"RPG Quest",
			cfg: makeConfigWithPreferences(t, nil, []string{string(tags.TagLangFR)}),
		},
		{
			name:             "config_pref_lang_japanese",
			input:            "SNES/RPG Quest",
			expectedPath:     "/roms/snes/RPG Quest (JPN).sfc",
			expectedStrategy: titles.StrategyExactMatch,
			description: "Config with ja language preference selects Japanese version of " +
				"RPG Quest",
			cfg: makeConfigWithPreferences(t, []string{string(tags.TagRegionJP)}, []string{string(tags.TagLangJA)}),
		},

		// ============================================================
		// ERROR CASES
		// Verifies proper error handling for invalid inputs
		// - Nonexistent games (no strategy finds match)
		// - Confidence below minimum threshold
		// - Invalid system ID
		// - Invalid input format (missing System/Title format)
		// - Empty slug after normalization
		// - Cache misses with tag filters
		// - Variant exclusion when only variants exist
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
			description: "Fuzzy match for 'Myster Cstl' might find 'Mystery Castle' but confidence is below " +
				"minimum threshold (0.60), so it should fail",
		},
		{
			name:          "error_invalid_system_id",
			input:         "FakeSystem/Some Game",
			expectedError: true,
			description: "Should return error when the specified system ID does not exist " +
				"in systemdefs",
		},
		{
			name:          "error_invalid_format_no_slash",
			input:         "Plumber Quest Adventures",
			expectedError: true,
			description: "Should return error for input lacking the 'System/Title' " +
				"format",
		},
		{
			name:          "error_slug_normalizes_to_empty",
			input:         "SNES/!@#$%",
			expectedError: true,
			description: "Should return error if the game title normalizes to an empty slug " +
				"after slugification",
		},
		{
			name:          "error_cache_miss_on_different_tags",
			input:         "NES/Galaxia",
			expectedError: true,
			description: "Query with (-region:us) fails despite 'Galaxia (USA)' existing - " +
				"cache keys include tags",
			advArgs: map[string]string{"tags": "-region:us"},
		},
		{
			name:          "error_variant_proto_excluded",
			input:         "Genesis/Plumber Quest Adventures Ultimate Edition",
			expectedError: true,
			description: "Prototype variant should be excluded by default variant filtering " +
				"(only (Proto) exists in DB)",
		},
		{
			name:          "error_only_beta_variant_exists",
			input:         "Genesis/Lost Project",
			expectedError: true,
			description: "Game with ONLY beta variant and no release version should fail " +
				"(variants excluded by default)",
		},
		{
			name:          "error_only_demo_and_beta_variants_exist",
			input:         "NES/Ancient Ruins",
			expectedError: true,
			description: "Game with only demo and beta variants (no release) should fail when " +
				"variants are excluded",
		},

		// ============================================================
		// NEGATIVE TESTS PER STRATEGY
		// Verifies each strategy correctly skips when inappropriate
		// - Exact match: no slug match → proceeds to next strategy
		// - Secondary title: no separator → falls through to exact
		// - Token signature: no token alignment → proceeds
		// - Fuzzy: confidence too low → fails
		// - Progressive trim: exhausts attempts → fails
		// - All strategies exhausted → error returned
		// ============================================================

		{
			name:          "negative_exact_match_no_slug_match",
			input:         "Genesis/Nonexistent Exact Game",
			expectedError: true,
			description: "Exact match strategy finds no result, proceeds to next strategy " +
				"(eventually fails if no strategy matches)",
		},
		{
			name:             "negative_secondary_title_no_separator",
			input:            "PC/Robot Wars",
			expectedPath:     "/games/pc/robotwars/rw.exe",
			expectedStrategy: titles.StrategyExactMatch,
			description: "Secondary title strategy skips 'Robot Wars' (no colon/dash), " +
				"falls through to exact match",
		},
		{
			name:          "negative_secondary_title_multiple_colons",
			input:         "Nintendo64/The Beginning",
			expectedError: true,
			description: "Multiple colons in 'Adventure Game: Part 1: The Beginning' - only first colon " +
				"is delimiter, 'The Beginning' alone doesn't match secondary slug 'part1thebeginning'",
		},
		{
			name:             "negative_token_signature_exact_match_precedence_wars_robot",
			input:            "PC/Wars Robot",
			expectedPath:     "/games/pc/warsrobot/wr.exe",
			expectedStrategy: titles.StrategyExactMatch,
			description: "Exact match for 'Wars Robot' takes precedence over token signature " +
				"(which would also match 'Robot Wars')",
		},
		{
			name:             "negative_token_signature_exact_match_precedence_robot_wars",
			input:            "PC/Robot Wars",
			expectedPath:     "/games/pc/robotwars/rw.exe",
			expectedStrategy: titles.StrategyExactMatch,
			description: "Exact match for 'Robot Wars' takes precedence over token signature " +
				"(which would also match 'Wars Robot')",
		},
		{
			name:             "negative_token_signature_exact_match_precedence_knight_shadow",
			input:            "Genesis/Knight Shadow",
			expectedPath:     "/roms/genesis/Knight Shadow (USA).md",
			expectedStrategy: titles.StrategyExactMatch,
			description: "Exact match for 'Knight Shadow' takes precedence over token signature " +
				"(which would also match 'Shadow Knight' variants)",
		},
		{
			name:          "negative_token_signature_partial_token_not_supported",
			input:         "PC/Quest",
			expectedError: true,
			description: "Single-word 'Quest' doesn't match 'Space Quest' - token signature requires " +
				"exact token set match, not partial/subset (1 token ≠ 2 tokens)",
		},
		{
			name:          "negative_token_signature_no_token_alignment",
			input:         "Genesis/Alpha Beta Gamma",
			expectedError: true,
			description: "Token signature finds no games with matching token sets, proceeds to next strategy " +
				"(eventually fails)",
		},
		{
			name:          "negative_fuzzy_confidence_too_low",
			input:         "NES/Xyz Abc",
			expectedError: true,
			description: "Fuzzy match confidence below minimum threshold (0.70), " +
				"strategy returns no result",
		},
		{
			name:          "negative_progressive_trim_minimum_slug_length",
			input:         "Genesis/Pro Championship Edition Extra",
			expectedError: true,
			description: "Progressive trim stops at minimum slug length (6 chars) - 'Pro' (3 chars) would " +
				"prefix-match too many false positives (Professional, Project, etc.)",
		},
		{
			name:          "negative_progressive_trim_no_fuzzy_fallback",
			input:         "SNES/Legend Qest Extra Words",
			expectedError: true,
			description: "Progressive trim only does exact matching - 'Legend Qest' (after trim) doesn't " +
				"fuzzy-match 'Legend Quest' (typo correction not supported in progressive trim)",
		},
		{
			name:          "negative_progressive_trim_all_trimmed_no_match",
			input:         "SNES/Nonexistent Word Word Word Word",
			expectedError: true,
			description: "Progressive trim exhausts all trim attempts without finding match, " +
				"returns no result",
		},
		{
			name:          "negative_all_strategies_exhausted",
			input:         "Genesis/Completely Made Up Game Title",
			expectedError: true,
			description: "All strategies exhausted without finding match, " +
				"final error returned to user",
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
