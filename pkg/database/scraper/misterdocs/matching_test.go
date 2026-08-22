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
	assert.Contains(t, write.MediaTags, database.TagInfo{Type: "scraper-run.mister-docs", Tag: "run-1"})
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

func TestMatchManualTitle_RejectsUnknown(t *testing.T) {
	t.Parallel()

	assert.Nil(t, matchManualTitle(testSystemIndex(), "System Manual.pdf"))
}

func TestNormalizedYear(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "1994", normalizedYear("1994-01-01"))
	assert.Empty(t, normalizedYear("unknown"))
}
