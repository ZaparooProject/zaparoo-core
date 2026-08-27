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

package misterdocs

import (
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testSystemIndex() systemIndex {
	return newSystemIndex(
		[]database.TitleWithSystem{
			{DBID: 10, Slug: "game", Name: "The Game", SystemID: "SNES"},
			{DBID: 20, Slug: "other", Name: "Other", SystemID: "SNES"},
		},
		[]database.MediaWithFullPath{
			{DBID: 100, MediaTitleDBID: 10, Path: "/games/SNES/Game (USA).sfc"},
			{DBID: 200, MediaTitleDBID: 20, Path: "/games/SNES/Other.sfc"},
		},
	)
}

func TestBuildPendingWrites_MapsExactArtworkMetadataAndManual(t *testing.T) {
	t.Parallel()

	artPath := filepath.Join("docs", "SNES", "Artwork", "Game.jpg")
	manualPath := filepath.Join("docs", "SNES", "Manuals", "Game, The.pdf")
	records := []sourceRecords{{
		Artwork: []artworkRecord{{Name: "Game (USA)", Key: "Game", ImagePath: artPath}},
		GameInfo: map[string]gameInfoRecord{
			"Game": {Year: "1994", Genre: "Platform", Developer: "Studio", Players: "1-4"},
		},
		Synopsis: map[string]string{"Game": "A &amp; B\n adventure"},
		Manuals:  []string{manualPath},
	}}

	targets, stats, found := buildPendingWrites(testSystemIndex(), records, "run-1")
	require.Len(t, targets, 1)
	assert.Equal(t, matchStats{Processed: 2, Matched: 2}, stats)
	assert.Contains(t, found, filepath.Clean(artPath))
	assert.Contains(t, found, filepath.Clean(manualPath))

	write := targets[0].Write
	require.Len(t, write.MediaProps, 1)
	assert.Equal(t, tags.PropertyTypeTag(tags.TagPropertyImageBoxart), write.MediaProps[0].TypeTag)
	assert.Equal(t, filepath.ToSlash(artPath), write.MediaProps[0].Text)

	props := make(map[string]database.MediaProperty)
	for _, prop := range write.TitleProps {
		props[prop.TypeTag] = prop
	}
	assert.Equal(t, filepath.ToSlash(manualPath), props[tags.PropertyTypeTag(tags.TagPropertyManual)].Text)
	assert.Equal(t, "A & B adventure", props[tags.PropertyTypeTag(tags.TagPropertyDescription)].Text)

	tagValues := make(map[string]string)
	for _, tag := range write.TitleTags {
		tagValues[tag.Type] = tag.Tag
	}
	assert.Equal(t, "1994", tagValues[string(tags.TagTypeYear)])
	assert.Equal(t, "4", tagValues[string(tags.TagTypePlayers)])
	assert.Equal(t, "platform", tagValues[string(tags.TagTypeGenre)])
	assert.Equal(t, "studio", tagValues[string(tags.TagTypeDeveloper)])
	assert.Contains(t, write.MediaTags, scraper.RunTagInfo(scraperID, "run-1"))
}

func TestBuildPendingWrites_SkipsDuplicateArtworkForMedia(t *testing.T) {
	t.Parallel()

	firstPath := filepath.Join("docs", "SNES", "Artwork", "Game.jpg")
	secondPath := filepath.Join("docs", "SNES", "Artwork", "Game.png")
	records := []sourceRecords{{Artwork: []artworkRecord{
		{Name: "Game (USA)", Key: "Game", ImagePath: firstPath},
		{Name: "Game (USA)", Key: "Game", ImagePath: secondPath},
	}}}

	targets, stats, _ := buildPendingWrites(testSystemIndex(), records, "")
	require.Len(t, targets, 1)
	assert.Equal(t, matchStats{Processed: 2, Matched: 1, Skipped: 1}, stats)
	require.Len(t, targets[0].Write.MediaProps, 1)
	assert.Equal(t, filepath.ToSlash(firstPath), targets[0].Write.MediaProps[0].Text)
}

func TestMatchArtwork_UsesUniqueTitleFallbackAtTitleScope(t *testing.T) {
	t.Parallel()

	record := artworkRecord{Name: "The Game", Key: "Game", ImagePath: "Game.jpg"}
	media, title, exact := matchArtwork(testSystemIndex(), record)
	require.NotNil(t, media)
	require.NotNil(t, title)
	assert.False(t, exact)
	assert.Equal(t, int64(10), title.DBID)
}

func TestMatchArtwork_DisambiguatesSharedMediaBaseByTitleSlug(t *testing.T) {
	t.Parallel()

	idx := newSystemIndex(
		[]database.TitleWithSystem{
			{DBID: 10, Slug: "game", Name: "Game"},
			{DBID: 20, Slug: "other", Name: "Other"},
		},
		[]database.MediaWithFullPath{
			{DBID: 100, MediaTitleDBID: 10, Path: filepath.Join("games", "Game.sfc")},
			{DBID: 200, MediaTitleDBID: 20, Path: filepath.Join("games", "Game.zip")},
		},
	)

	media, title, exact := matchArtwork(idx, artworkRecord{Name: "Game"})
	require.NotNil(t, media)
	require.NotNil(t, title)
	assert.Equal(t, int64(100), media.DBID)
	assert.Equal(t, int64(10), title.DBID)
	assert.True(t, exact)
}

func TestMatchArtwork_RejectsAmbiguousSharedMediaBase(t *testing.T) {
	t.Parallel()

	idx := newSystemIndex(
		[]database.TitleWithSystem{
			{DBID: 10, Slug: "game", Name: "Game"},
			{DBID: 20, Slug: "game", Name: "Game"},
		},
		[]database.MediaWithFullPath{
			{DBID: 100, MediaTitleDBID: 10, Path: filepath.Join("games", "Game.sfc")},
			{DBID: 200, MediaTitleDBID: 20, Path: filepath.Join("games", "Game.zip")},
		},
	)

	media, title, exact := matchArtwork(idx, artworkRecord{Name: "Game"})
	assert.Nil(t, media)
	assert.Nil(t, title)
	assert.False(t, exact)
}

func TestMatchArtwork_TitleFallbackPrefersPresentMedia(t *testing.T) {
	t.Parallel()

	idx := newSystemIndex(
		[]database.TitleWithSystem{{DBID: 10, Slug: "game", Name: "Game"}},
		[]database.MediaWithFullPath{
			{DBID: 100, MediaTitleDBID: 10, Path: "Missing.sfc", IsMissing: true},
			{DBID: 200, MediaTitleDBID: 10, Path: "Present.sfc"},
		},
	)

	media, title, exact := matchArtwork(idx, artworkRecord{Name: "Game"})
	require.NotNil(t, media)
	require.NotNil(t, title)
	assert.Equal(t, int64(200), media.DBID)
	assert.False(t, exact)
}

func TestMatchManualTitle_RejectsUnknown(t *testing.T) {
	t.Parallel()

	assert.Nil(t, matchManualTitle(testSystemIndex(), "System Manual.pdf"))
}

func TestMatchManualTitle_SharedSlugUsesUniqueNormalizedName(t *testing.T) {
	t.Parallel()

	unique := newSystemIndex([]database.TitleWithSystem{
		{DBID: 10, Slug: "game", Name: "Game"},
		{DBID: 20, Slug: "game", Name: "Game II"},
	}, nil)
	matched := matchManualTitle(unique, "Game.pdf")
	require.NotNil(t, matched)
	assert.Equal(t, int64(10), matched.DBID)

	ambiguous := newSystemIndex([]database.TitleWithSystem{
		{DBID: 10, Slug: "game", Name: "Game"},
		{DBID: 20, Slug: "game", Name: "Game"},
	}, nil)
	assert.Nil(t, matchManualTitle(ambiguous, "Game.pdf"))
}

func TestNormalizedYear(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "1994", normalizedYear("1994-01-01"))
	assert.Empty(t, normalizedYear("unknown"))
}
