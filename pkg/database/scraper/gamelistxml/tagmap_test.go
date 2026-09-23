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

package gamelistxml

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper/scrapertest"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esapi"
	"github.com/stretchr/testify/assert"
)

func tagValuesOfType(infos []database.TagInfo, tagType tags.TagType) []string {
	var values []string
	for _, info := range infos {
		if info.Type == string(tagType) {
			values = append(values, info.Tag)
		}
	}
	return values
}

func TestRecalboxGenreIDsAreCanonical(t *testing.T) {
	t.Parallel()
	for id, values := range recalboxGenreIDs {
		for _, v := range values {
			assert.Truef(t, tags.IsCanonicalValue(tags.TagTypeGenre, v), "genreid %s maps to %q", id, v)
		}
	}
	for code, v := range screenScraperRegions {
		assert.Truef(t, tags.IsCanonicalValue(tags.TagTypeRegion, v), "region %s maps to %q", code, v)
	}
}

func TestMapToDB_Genre(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		genre   string
		genreID string
		want    []string
	}{
		{name: "sports", genre: "Sports", want: []string{"sports"}},
		{name: "platform", genre: "Platform", want: []string{"action:platformer", "action"}},
		{
			name:  "hyphen joined",
			genre: "Sports / Football (American)-Sports",
			want:  []string{"sports:football", "sports"},
		},
		{name: "vertical shmup", genre: "Shoot'em up / Vertical", want: []string{"shmup:v", "shmup"}},
		{name: "racing", genre: "Race, Driving", want: []string{"racing"}},
		{name: "sports subgenre", genre: "Sports / Football", want: []string{"sports"}},
		{name: "puzzle", genre: "Puzzle-Game", want: []string{"puzzle"}},
		{name: "rpg", genre: "Role Playing Game", want: []string{"rpg"}},
		{
			name: "adventure", genre: "Adventure / Point and Click",
			want: []string{"adventure:pointandclick", "adventure"},
		},
		{name: "lightgun", genre: "Lightgun Shooter", want: []string{"shooting:gallery", "shooting"}},
		{name: "casino", genre: "Casino", want: []string{"parlor"}},
		{name: "comma list", genre: "Beat'em Up, Fighting", want: []string{"brawler", "fighting"}},
		{name: "genre id only", genreID: "257", want: []string{"action:platformer", "action"}},
		{
			name:    "text and id",
			genre:   "Plateforme",
			genreID: "263",
			want:    []string{"brawler"},
		},
		{name: "id zero means none", genreID: "0"},
		{name: "not a genre", genre: "Compilation"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := GamelistRecord{Game: esapi.Game{Genre: tt.genre, GenreID: tt.genreID}}
			titleTags := mapToDBValid(t, &GamelistXMLScraper{}, &rec).TitleTags
			assert.ElementsMatch(t, tt.want, tagValuesOfType(titleTags, tags.TagTypeGenre))
			for _, tag := range titleTags {
				assert.Empty(t, tag.Label, "closed-type tags carry no label")
			}
		})
	}
}

func TestMapToDB_RegionAndLangWords(t *testing.T) {
	t.Parallel()
	rec := GamelistRecord{Game: esapi.Game{Region: "wor, eur, USA, ss", Lang: "en, Japanese"}}
	g := &GamelistXMLScraper{}
	mediaTags := mapToDBValid(t, g, &rec).MediaTags
	assert.Equal(t, []string{"world", "eu", "us"}, tagValuesOfType(mediaTags, tags.TagTypeRegion))
	assert.Equal(t, []string{"en", "ja"}, tagValuesOfType(mediaTags, tags.TagTypeLang))
	assert.Equal(t, 1, g.unmapped.Count(tags.TagTypeRegion), "the ScreenScraper default region is not a region")
}

func TestMapToDB_PlayersRatingYearFollowRules(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		players []string
		rating  []string
		year    []string
		game    esapi.Game
	}{
		{
			name: "valid", game: esapi.Game{Players: "1-16", Rating: "0.755", ReleaseDate: "19870101T000000"},
			players: []string{"16"}, rating: []string{"76"}, year: []string{"1987"},
		},
		{
			name: "out of vocabulary", game: esapi.Game{Players: "1-11", Rating: "1.5", ReleaseDate: "18990101"},
		},
		{name: "negative rating", game: esapi.Game{Rating: "-0.5"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := GamelistRecord{Game: tt.game}
			titleTags := mapToDBValid(t, &GamelistXMLScraper{}, &rec).TitleTags
			assert.Equal(t, tt.players, tagValuesOfType(titleTags, tags.TagTypePlayers))
			assert.Equal(t, tt.rating, tagValuesOfType(titleTags, tags.TagTypeRating))
			assert.Equal(t, tt.year, tagValuesOfType(titleTags, tags.TagTypeYear))
		})
	}
}

// A value the tables cannot map must be dropped rather than emitted, since
// the write path would refuse it, and noted so the tables can grow.
func TestMapToDB_UnmappedValuesDroppedAndNoted(t *testing.T) {
	t.Parallel()
	g := &GamelistXMLScraper{}
	rec := GamelistRecord{Game: esapi.Game{
		Genre:            "Gardening",
		GenreID:          "9999",
		Region:           "Atlantis",
		Lang:             "Klingon",
		ArcadeSystemName: "Imaginary Board 9000",
		Family:           "No Such Series",
		Players:          "1-32",
		Developer:        "Some Studio",
	}}
	result := mapToDBValid(t, g, &rec)

	assert.Empty(t, result.MediaTags)
	assert.Equal(t, []database.TagInfo{
		{Type: string(tags.TagTypeDeveloper), Tag: "some-studio", Label: "Some Studio"},
	}, result.TitleTags)
	assert.Equal(t, 2, g.unmapped.Count(tags.TagTypeGenre))
	for _, tagType := range []tags.TagType{
		tags.TagTypeRegion, tags.TagTypeLang, tags.TagTypeArcadeBoard, tags.TagTypeSearch, tags.TagTypePlayers,
	} {
		assert.Equalf(t, 1, g.unmapped.Count(tagType), "unmapped %s not noted", tagType)
	}
	scrapertest.RequireValidTags(t, result.TitleTags)
}

// recalboxGenresEnum is every value of Recalbox's GameGenres enum
// (recalbox-emulationstation es-app/src/games/classifications/Genres.h),
// written as decimal, as <genreid> carries it. None (0) is omitted.
var recalboxGenresEnum = []string{
	"256", "257", "258", "259", "260", "261", "262", "263", "264", "265", "266",
	"512", "513", "514", "515", "516", "517", "518",
	"768", "769", "770", "771", "772", "773", "774",
	"1024", "1025", "1026", "1027", "1028", "1029",
	"1280", "1281", "1282", "1283", "1284", "1285", "1286", "1287", "1288",
	"1536", "1537", "1538", "1539", "1540",
	"1792", "2048", "2304", "2560", "2816", "3072", "3328", "3584", "3840", "4096", "4352",
}

// TestRecalboxGenreIDsCoverTheWholeEnum keeps the id table complete: every
// id Recalbox can write either maps or is dropped on purpose, and the table
// names no id the enum lacks.
func TestRecalboxGenreIDsCoverTheWholeEnum(t *testing.T) {
	t.Parallel()
	enum := make(map[string]struct{}, len(recalboxGenresEnum))
	for _, id := range recalboxGenresEnum {
		enum[id] = struct{}{}
		_, ok := recalboxGenreIDs[id]
		assert.True(t, ok, "Recalbox genre id %s has no decision", id)
	}
	for id := range recalboxGenreIDs {
		_, ok := enum[id]
		assert.True(t, ok, "genre id %s is not in Recalbox's enum", id)
	}
}
