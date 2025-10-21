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

package mediadb

import (
	"context"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createTestMedia is a helper that creates a test media record and returns its DBID
func createTestMedia(t *testing.T, mediaDB *MediaDB, systemID, slug, name, path string) int64 {
	t.Helper()

	err := mediaDB.BeginTransaction(false)
	require.NoError(t, err)

	system, err := systemdefs.GetSystem(systemID)
	require.NoError(t, err)

	systemDB := database.System{
		SystemID: system.ID,
		Name:     system.ID,
	}
	// Use FindOrInsertSystem to avoid UNIQUE constraint violations when creating multiple media for same system
	insertedSystem, err := mediaDB.FindOrInsertSystem(systemDB)
	require.NoError(t, err)

	title := database.MediaTitle{
		SystemDBID: insertedSystem.DBID,
		Slug:       slug,
		Name:       name,
	}
	insertedTitle, err := mediaDB.InsertMediaTitle(&title)
	require.NoError(t, err)

	media := database.Media{
		SystemDBID:     insertedSystem.DBID,
		MediaTitleDBID: insertedTitle.DBID,
		Path:           path,
	}
	insertedMedia, err := mediaDB.InsertMedia(media)
	require.NoError(t, err)

	err = mediaDB.CommitTransaction()
	require.NoError(t, err)

	return insertedMedia.DBID
}

// TestGenerateSlugCacheKey_ConsistentHashing verifies that the same inputs always produce the same hash
func TestGenerateSlugCacheKey_ConsistentHashing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		systemID   string
		slug       string
		tagFilters []database.TagFilter
	}{
		{
			name:       "simple case",
			systemID:   "NES",
			slug:       "supermario",
			tagFilters: nil,
		},
		{
			name:     "with single tag",
			systemID: "SNES",
			slug:     "zelda",
			tagFilters: []database.TagFilter{
				{Type: "region", Value: "usa"},
			},
		},
		{
			name:     "with multiple tags",
			systemID: "Genesis",
			slug:     "sonic",
			tagFilters: []database.TagFilter{
				{Type: "region", Value: "usa"},
				{Type: "genre", Value: "platform"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key1, err1 := generateSlugCacheKey(tt.systemID, tt.slug, tt.tagFilters)
			require.NoError(t, err1)

			key2, err2 := generateSlugCacheKey(tt.systemID, tt.slug, tt.tagFilters)
			require.NoError(t, err2)

			assert.Equal(t, key1, key2, "same inputs should produce same hash")
			assert.NotEmpty(t, key1, "hash should not be empty")
			assert.Len(t, key1, 64, "SHA256 hash should be 64 hex characters")
		})
	}
}

// TestGenerateSlugCacheKey_TagOrderIndependence verifies that tag order doesn't affect the hash
func TestGenerateSlugCacheKey_TagOrderIndependence(t *testing.T) {
	t.Parallel()

	tags1 := []database.TagFilter{
		{Type: "region", Value: "usa"},
		{Type: "genre", Value: "platform"},
		{Type: "lang", Value: "en"},
	}

	tags2 := []database.TagFilter{
		{Type: "lang", Value: "en"},
		{Type: "region", Value: "usa"},
		{Type: "genre", Value: "platform"},
	}

	key1, err1 := generateSlugCacheKey("NES", "mario", tags1)
	require.NoError(t, err1)

	key2, err2 := generateSlugCacheKey("NES", "mario", tags2)
	require.NoError(t, err2)

	assert.Equal(t, key1, key2, "different tag order should produce same hash due to sorting")
}

// TestGenerateSlugCacheKey_CaseNormalization verifies case-insensitive hashing
func TestGenerateSlugCacheKey_CaseNormalization(t *testing.T) {
	t.Parallel()

	key1, err1 := generateSlugCacheKey("NES", "SuperMario", nil)
	require.NoError(t, err1)

	key2, err2 := generateSlugCacheKey("nes", "supermario", nil)
	require.NoError(t, err2)

	assert.Equal(t, key1, key2, "case should be normalized (lowercased)")
}

// TestGenerateSlugCacheKey_WhitespaceNormalization verifies whitespace is trimmed
func TestGenerateSlugCacheKey_WhitespaceNormalization(t *testing.T) {
	t.Parallel()

	key1, err1 := generateSlugCacheKey("  NES  ", "  mario  ", nil)
	require.NoError(t, err1)

	key2, err2 := generateSlugCacheKey("NES", "mario", nil)
	require.NoError(t, err2)

	assert.Equal(t, key1, key2, "whitespace should be trimmed")
}

// TestGenerateSlugCacheKey_EmptyTags verifies handling of nil and empty tag slices
func TestGenerateSlugCacheKey_EmptyTags(t *testing.T) {
	t.Parallel()

	keyNil, errNil := generateSlugCacheKey("NES", "mario", nil)
	require.NoError(t, errNil)

	keyEmpty, errEmpty := generateSlugCacheKey("NES", "mario", []database.TagFilter{})
	require.NoError(t, errEmpty)

	assert.Equal(t, keyNil, keyEmpty, "nil and empty tag slices should produce same hash")
}

// TestSlugCache_SetAndGet_Integration tests the full cache set/get cycle
func TestSlugCache_SetAndGet_Integration(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create actual media record for FK constraint
	err := mediaDB.BeginTransaction(false)
	require.NoError(t, err)

	nesSystem, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)

	system := database.System{
		SystemID: nesSystem.ID,
		Name:     "NES",
	}
	insertedSystem, err := mediaDB.InsertSystem(system)
	require.NoError(t, err)

	title := database.MediaTitle{
		SystemDBID: insertedSystem.DBID,
		Slug:       "supermariobros",
		Name:       "Super Mario Bros",
	}
	insertedTitle, err := mediaDB.InsertMediaTitle(&title)
	require.NoError(t, err)

	media := database.Media{
		SystemDBID:     insertedSystem.DBID,
		MediaTitleDBID: insertedTitle.DBID,
		Path:           "/roms/mario.nes",
	}
	insertedMedia, err := mediaDB.InsertMedia(media)
	require.NoError(t, err)

	err = mediaDB.CommitTransaction()
	require.NoError(t, err)

	// Test basic set and get
	systemID := nesSystem.ID
	slug := "supermariobros"
	mediaDBID := insertedMedia.DBID
	strategy := "exact_match"

	err = mediaDB.SetCachedSlugResolution(ctx, systemID, slug, nil, mediaDBID, strategy)
	require.NoError(t, err)

	// Verify we can retrieve it
	gotMediaDBID, gotStrategy, found := mediaDB.GetCachedSlugResolution(ctx, systemID, slug, nil)
	assert.True(t, found, "cache entry should be found")
	assert.Equal(t, mediaDBID, gotMediaDBID)
	assert.Equal(t, strategy, gotStrategy)
}

// TestSlugCache_CacheMiss verifies cache miss behavior
func TestSlugCache_CacheMiss(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	ctx := context.Background()

	// Try to get non-existent cache entry
	mediaDBID, strategy, found := mediaDB.GetCachedSlugResolution(ctx, "NES", "nonexistent", nil)
	assert.False(t, found, "cache entry should not be found")
	assert.Equal(t, int64(0), mediaDBID)
	assert.Empty(t, strategy)
}

// TestSlugCache_MultipleEntries verifies multiple cache entries can coexist
func TestSlugCache_MultipleEntries_Integration(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create test media records
	nesMarDBID := createTestMedia(t, mediaDB, "NES", "mario", "Super Mario Bros", "/roms/nes/mario.nes")
	nesZeldaDBID := createTestMedia(t, mediaDB, "NES", "zelda", "The Legend of Zelda", "/roms/nes/zelda.nes")
	snesMarDBID := createTestMedia(t, mediaDB, "SNES", "mario", "Super Mario World", "/roms/snes/mario.smc")
	genesisSonicDBID := createTestMedia(t, mediaDB, "Genesis", "sonic", "Sonic the Hedgehog", "/roms/genesis/sonic.bin")

	// Set multiple entries
	entries := []struct {
		systemID  string
		slug      string
		strategy  string
		mediaDBID int64
	}{
		{systemID: "NES", slug: "mario", mediaDBID: nesMarDBID, strategy: "exact"},
		{systemID: "NES", slug: "zelda", mediaDBID: nesZeldaDBID, strategy: "exact"},
		{systemID: "SNES", slug: "mario", mediaDBID: snesMarDBID, strategy: "exact"},
		{systemID: "Genesis", slug: "sonic", mediaDBID: genesisSonicDBID, strategy: "fuzzy"},
	}

	for _, entry := range entries {
		err := mediaDB.SetCachedSlugResolution(ctx, entry.systemID, entry.slug, nil, entry.mediaDBID, entry.strategy)
		require.NoError(t, err)
	}

	// Verify all entries can be retrieved
	for _, entry := range entries {
		mediaDBID, strategy, found := mediaDB.GetCachedSlugResolution(ctx, entry.systemID, entry.slug, nil)
		assert.True(t, found, "cache entry should be found for %s/%s", entry.systemID, entry.slug)
		assert.Equal(t, entry.mediaDBID, mediaDBID)
		assert.Equal(t, entry.strategy, strategy)
	}
}

// TestSlugCache_WithTagFilters verifies caching works with tag filters
func TestSlugCache_WithTagFilters_Integration(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	ctx := context.Background()

	systemID := "NES"
	slug := "mario"

	// Create test media records
	media1DBID := createTestMedia(t, mediaDB, systemID, slug, "Super Mario Bros", "/roms/mario1.nes")
	media2DBID := createTestMedia(
		t, mediaDB, systemID, slug+"_usa", "Super Mario Bros (USA)", "/roms/mario_usa.nes")
	media3DBID := createTestMedia(
		t, mediaDB, systemID, slug+"_multi", "Super Mario Bros (USA) (Platform)", "/roms/mario_multi.nes")

	// Set entry with no tags
	err := mediaDB.SetCachedSlugResolution(ctx, systemID, slug, nil, media1DBID, "no_tags")
	require.NoError(t, err)

	// Set entry with USA region tag
	usaTags := []database.TagFilter{{Type: "region", Value: "usa"}}
	err = mediaDB.SetCachedSlugResolution(ctx, systemID, slug, usaTags, media2DBID, "usa_region")
	require.NoError(t, err)

	// Set entry with multiple tags
	multiTags := []database.TagFilter{
		{Type: "region", Value: "usa"},
		{Type: "genre", Value: "platform"},
	}
	err = mediaDB.SetCachedSlugResolution(ctx, systemID, slug, multiTags, media3DBID, "multi_tag")
	require.NoError(t, err)

	// Verify each entry is separate
	mediaDBID1, strategy1, found1 := mediaDB.GetCachedSlugResolution(ctx, systemID, slug, nil)
	assert.True(t, found1)
	assert.Equal(t, media1DBID, mediaDBID1)
	assert.Equal(t, "no_tags", strategy1)

	mediaDBID2, strategy2, found2 := mediaDB.GetCachedSlugResolution(ctx, systemID, slug, usaTags)
	assert.True(t, found2)
	assert.Equal(t, media2DBID, mediaDBID2)
	assert.Equal(t, "usa_region", strategy2)

	mediaDBID3, strategy3, found3 := mediaDB.GetCachedSlugResolution(ctx, systemID, slug, multiTags)
	assert.True(t, found3)
	assert.Equal(t, media3DBID, mediaDBID3)
	assert.Equal(t, "multi_tag", strategy3)
}

// TestSlugCache_OverwriteExisting verifies INSERT OR REPLACE behavior
func TestSlugCache_OverwriteExisting_Integration(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	ctx := context.Background()

	systemID := "NES"
	slug := "mario"

	// Create test media records
	media1DBID := createTestMedia(
		t, mediaDB, systemID, slug+"_first", "Super Mario Bros (First)", "/roms/mario_first.nes")
	media2DBID := createTestMedia(
		t, mediaDB, systemID, slug+"_second", "Super Mario Bros (Second)", "/roms/mario_second.nes")

	// Set initial entry
	err := mediaDB.SetCachedSlugResolution(ctx, systemID, slug, nil, media1DBID, "first")
	require.NoError(t, err)

	// Overwrite with new values
	err = mediaDB.SetCachedSlugResolution(ctx, systemID, slug, nil, media2DBID, "second")
	require.NoError(t, err)

	// Verify the new value is returned
	mediaDBID, strategy, found := mediaDB.GetCachedSlugResolution(ctx, systemID, slug, nil)
	assert.True(t, found)
	assert.Equal(t, media2DBID, mediaDBID, "should return updated value")
	assert.Equal(t, "second", strategy, "should return updated strategy")
}

// TestSlugCache_InvalidateFull clears the entire cache
func TestSlugCache_InvalidateFull_Integration(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create test media records
	nesMedia := createTestMedia(t, mediaDB, "NES", "mario", "Super Mario Bros", "/roms/nes/mario.nes")
	snesMedia := createTestMedia(t, mediaDB, "SNES", "zelda", "The Legend of Zelda", "/roms/snes/zelda.smc")
	genesisMedia := createTestMedia(t, mediaDB, "Genesis", "sonic", "Sonic the Hedgehog", "/roms/genesis/sonic.bin")

	// Populate cache with multiple entries
	err := mediaDB.SetCachedSlugResolution(ctx, "NES", "mario", nil, nesMedia, "exact")
	require.NoError(t, err)
	err = mediaDB.SetCachedSlugResolution(ctx, "SNES", "zelda", nil, snesMedia, "exact")
	require.NoError(t, err)
	err = mediaDB.SetCachedSlugResolution(ctx, "Genesis", "sonic", nil, genesisMedia, "exact")
	require.NoError(t, err)

	// Invalidate entire cache
	err = mediaDB.InvalidateSlugCache(ctx)
	require.NoError(t, err)

	// Verify all entries are gone
	_, _, found1 := mediaDB.GetCachedSlugResolution(ctx, "NES", "mario", nil)
	assert.False(t, found1, "NES entry should be cleared")

	_, _, found2 := mediaDB.GetCachedSlugResolution(ctx, "SNES", "zelda", nil)
	assert.False(t, found2, "SNES entry should be cleared")

	_, _, found3 := mediaDB.GetCachedSlugResolution(ctx, "Genesis", "sonic", nil)
	assert.False(t, found3, "Genesis entry should be cleared")
}

// TestSlugCache_InvalidateBySystem verifies per-system invalidation
func TestSlugCache_InvalidateBySystem_Integration(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create test media records
	nesMario := createTestMedia(t, mediaDB, "NES", "mario", "Super Mario Bros", "/roms/nes/mario.nes")
	nesZelda := createTestMedia(t, mediaDB, "NES", "zelda", "The Legend of Zelda", "/roms/nes/zelda.nes")
	snesKart := createTestMedia(t, mediaDB, "SNES", "mariokart", "Super Mario Kart", "/roms/snes/kart.smc")
	genesisSonic := createTestMedia(t, mediaDB, "Genesis", "sonic", "Sonic the Hedgehog", "/roms/genesis/sonic.bin")

	// Populate cache for multiple systems
	err := mediaDB.SetCachedSlugResolution(ctx, "NES", "mario", nil, nesMario, "exact")
	require.NoError(t, err)
	err = mediaDB.SetCachedSlugResolution(ctx, "NES", "zelda", nil, nesZelda, "exact")
	require.NoError(t, err)
	err = mediaDB.SetCachedSlugResolution(ctx, "SNES", "mariokart", nil, snesKart, "exact")
	require.NoError(t, err)
	err = mediaDB.SetCachedSlugResolution(ctx, "Genesis", "sonic", nil, genesisSonic, "exact")
	require.NoError(t, err)

	// Invalidate only NES system
	err = mediaDB.InvalidateSlugCacheForSystems(ctx, []string{"NES"})
	require.NoError(t, err)

	// Verify NES entries are gone
	_, _, found1 := mediaDB.GetCachedSlugResolution(ctx, "NES", "mario", nil)
	assert.False(t, found1, "NES mario should be cleared")

	_, _, found2 := mediaDB.GetCachedSlugResolution(ctx, "NES", "zelda", nil)
	assert.False(t, found2, "NES zelda should be cleared")

	// Verify other systems remain
	_, _, found3 := mediaDB.GetCachedSlugResolution(ctx, "SNES", "mariokart", nil)
	assert.True(t, found3, "SNES entry should remain")

	_, _, found4 := mediaDB.GetCachedSlugResolution(ctx, "Genesis", "sonic", nil)
	assert.True(t, found4, "Genesis entry should remain")
}

// TestSlugCache_InvalidateMultipleSystems verifies multiple systems can be invalidated at once
func TestSlugCache_InvalidateMultipleSystems_Integration(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create test media records
	nesMario := createTestMedia(t, mediaDB, "NES", "mario", "Super Mario Bros", "/roms/nes/mario.nes")
	snesZelda := createTestMedia(t, mediaDB, "SNES", "zelda", "The Legend of Zelda", "/roms/snes/zelda.smc")
	genesisSonic := createTestMedia(t, mediaDB, "Genesis", "sonic", "Sonic the Hedgehog", "/roms/genesis/sonic.bin")

	// Populate cache for multiple systems
	err := mediaDB.SetCachedSlugResolution(ctx, "NES", "mario", nil, nesMario, "exact")
	require.NoError(t, err)
	err = mediaDB.SetCachedSlugResolution(ctx, "SNES", "zelda", nil, snesZelda, "exact")
	require.NoError(t, err)
	err = mediaDB.SetCachedSlugResolution(ctx, "Genesis", "sonic", nil, genesisSonic, "exact")
	require.NoError(t, err)

	// Invalidate NES and SNES
	err = mediaDB.InvalidateSlugCacheForSystems(ctx, []string{"NES", "SNES"})
	require.NoError(t, err)

	// Verify NES and SNES entries are gone
	_, _, found1 := mediaDB.GetCachedSlugResolution(ctx, "NES", "mario", nil)
	assert.False(t, found1, "NES entry should be cleared")

	_, _, found2 := mediaDB.GetCachedSlugResolution(ctx, "SNES", "zelda", nil)
	assert.False(t, found2, "SNES entry should be cleared")

	// Verify Genesis remains
	_, _, found3 := mediaDB.GetCachedSlugResolution(ctx, "Genesis", "sonic", nil)
	assert.True(t, found3, "Genesis entry should remain")
}

// TestSlugCache_InvalidateEmptyList verifies empty system list is a no-op
func TestSlugCache_InvalidateEmptyList_Integration(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create test media record
	nesMario := createTestMedia(t, mediaDB, "NES", "mario", "Super Mario Bros", "/roms/nes/mario.nes")

	// Populate cache
	err := mediaDB.SetCachedSlugResolution(ctx, "NES", "mario", nil, nesMario, "exact")
	require.NoError(t, err)

	// Invalidate with empty list - should be no-op
	err = mediaDB.InvalidateSlugCacheForSystems(ctx, []string{})
	require.NoError(t, err)

	// Verify entry still exists
	_, _, found := mediaDB.GetCachedSlugResolution(ctx, "NES", "mario", nil)
	assert.True(t, found, "entry should still exist after empty invalidation")
}

// TestSlugCache_InvalidateNonExistentSystem verifies invalidating non-existent system doesn't error
func TestSlugCache_InvalidateNonExistentSystem_Integration(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	ctx := context.Background()

	// Try to invalidate cache for system that doesn't exist
	err := mediaDB.InvalidateSlugCacheForSystems(ctx, []string{"NonExistentSystem"})
	require.NoError(t, err, "invalidating non-existent system should not error")
}

// TestGetMediaByDBID_Integration verifies media retrieval by DBID
func TestGetMediaByDBID_Integration(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create tag types BEFORE transaction
	regionTagType, err := mediaDB.FindOrInsertTagType(database.TagType{Type: "region"})
	require.NoError(t, err)

	genreTagType, err := mediaDB.FindOrInsertTagType(database.TagType{Type: "genre"})
	require.NoError(t, err)

	// Insert test data
	err = mediaDB.BeginTransaction(false)
	require.NoError(t, err)

	nesSystem, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)

	system := database.System{
		SystemID: nesSystem.ID,
		Name:     "NES",
	}
	insertedSystem, err := mediaDB.InsertSystem(system)
	require.NoError(t, err)

	title := database.MediaTitle{
		SystemDBID: insertedSystem.DBID,
		Slug:       slugs.SlugifyString("Super Mario Bros"),
		Name:       "Super Mario Bros",
	}
	insertedTitle, err := mediaDB.InsertMediaTitle(&title)
	require.NoError(t, err)

	media := database.Media{
		SystemDBID:     insertedSystem.DBID,
		MediaTitleDBID: insertedTitle.DBID,
		Path:           "/roms/nes/mario.nes",
	}
	insertedMedia, err := mediaDB.InsertMedia(media)
	require.NoError(t, err)

	// Add tags
	usaTag := database.Tag{
		TypeDBID: regionTagType.DBID,
		Tag:      "usa",
	}
	insertedUSATag, err := mediaDB.FindOrInsertTag(usaTag)
	require.NoError(t, err)

	platformTag := database.Tag{
		TypeDBID: genreTagType.DBID,
		Tag:      "platform",
	}
	insertedPlatformTag, err := mediaDB.FindOrInsertTag(platformTag)
	require.NoError(t, err)

	_, err = mediaDB.InsertMediaTag(database.MediaTag{
		MediaDBID: insertedMedia.DBID,
		TagDBID:   insertedUSATag.DBID,
	})
	require.NoError(t, err)

	_, err = mediaDB.InsertMediaTag(database.MediaTag{
		MediaDBID: insertedMedia.DBID,
		TagDBID:   insertedPlatformTag.DBID,
	})
	require.NoError(t, err)

	err = mediaDB.CommitTransaction()
	require.NoError(t, err)

	// Test GetMediaByDBID
	result, err := mediaDB.GetMediaByDBID(ctx, insertedMedia.DBID)
	require.NoError(t, err)

	assert.Equal(t, nesSystem.ID, result.SystemID)
	assert.Equal(t, "Super Mario Bros", result.Name)
	assert.Equal(t, "/roms/nes/mario.nes", result.Path)
	assert.Equal(t, insertedMedia.DBID, result.MediaID)
	assert.Len(t, result.Tags, 2)

	// Verify tags are correctly populated
	tagMap := make(map[string]string)
	for _, tag := range result.Tags {
		tagMap[tag.Type] = tag.Tag
	}
	assert.Equal(t, "usa", tagMap["region"])
	assert.Equal(t, "platform", tagMap["genre"])
}

// TestGetMediaByDBID_NoTags verifies media with no tags
func TestGetMediaByDBID_NoTags_Integration(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	ctx := context.Background()

	// Insert test data without tags
	err := mediaDB.BeginTransaction(false)
	require.NoError(t, err)

	nesSystem, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)

	system := database.System{
		SystemID: nesSystem.ID,
		Name:     "NES",
	}
	insertedSystem, err := mediaDB.InsertSystem(system)
	require.NoError(t, err)

	title := database.MediaTitle{
		SystemDBID: insertedSystem.DBID,
		Slug:       slugs.SlugifyString("Test Game"),
		Name:       "Test Game",
	}
	insertedTitle, err := mediaDB.InsertMediaTitle(&title)
	require.NoError(t, err)

	media := database.Media{
		SystemDBID:     insertedSystem.DBID,
		MediaTitleDBID: insertedTitle.DBID,
		Path:           "/roms/test.nes",
	}
	insertedMedia, err := mediaDB.InsertMedia(media)
	require.NoError(t, err)

	err = mediaDB.CommitTransaction()
	require.NoError(t, err)

	// Test GetMediaByDBID
	result, err := mediaDB.GetMediaByDBID(ctx, insertedMedia.DBID)
	require.NoError(t, err)

	assert.Equal(t, nesSystem.ID, result.SystemID)
	assert.Equal(t, "Test Game", result.Name)
	assert.Equal(t, "/roms/test.nes", result.Path)
	assert.Empty(t, result.Tags, "should have no tags")
}

// TestGetMediaByDBID_InvalidID verifies error handling for invalid DBID
func TestGetMediaByDBID_InvalidID_Integration(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	ctx := context.Background()

	// Try to get media with non-existent DBID
	_, err := mediaDB.GetMediaByDBID(ctx, 99999)
	require.Error(t, err, "should error for non-existent DBID")
	assert.Contains(t, err.Error(), "failed to get media by DBID")
}

// TestSlugCache_CascadeDelete verifies cache entries are deleted when media is deleted
func TestSlugCache_CascadeDelete_Integration(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	ctx := context.Background()

	// Insert test data
	err := mediaDB.BeginTransaction(false)
	require.NoError(t, err)

	nesSystem, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)

	system := database.System{
		SystemID: nesSystem.ID,
		Name:     "NES",
	}
	insertedSystem, err := mediaDB.InsertSystem(system)
	require.NoError(t, err)

	title := database.MediaTitle{
		SystemDBID: insertedSystem.DBID,
		Slug:       slugs.SlugifyString("Test Game"),
		Name:       "Test Game",
	}
	insertedTitle, err := mediaDB.InsertMediaTitle(&title)
	require.NoError(t, err)

	media := database.Media{
		SystemDBID:     insertedSystem.DBID,
		MediaTitleDBID: insertedTitle.DBID,
		Path:           "/roms/test.nes",
	}
	insertedMedia, err := mediaDB.InsertMedia(media)
	require.NoError(t, err)

	err = mediaDB.CommitTransaction()
	require.NoError(t, err)

	// Cache the slug resolution
	err = mediaDB.SetCachedSlugResolution(ctx, nesSystem.ID, "testgame", nil, insertedMedia.DBID, "exact")
	require.NoError(t, err)

	// Verify cache entry exists
	_, _, found := mediaDB.GetCachedSlugResolution(ctx, nesSystem.ID, "testgame", nil)
	assert.True(t, found, "cache entry should exist before delete")

	// Delete the media (using Truncate as a simple way to delete)
	err = mediaDB.Truncate()
	require.NoError(t, err)

	// Verify cache entry is gone (due to FK CASCADE)
	_, _, foundAfter := mediaDB.GetCachedSlugResolution(ctx, nesSystem.ID, "testgame", nil)
	assert.False(t, foundAfter, "cache entry should be deleted via CASCADE when media is deleted")
}
