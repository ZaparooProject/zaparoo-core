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

package gamedatabase

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	gdb "github.com/ZaparooProject/zaparoo-core/v2/pkg/database/gamedatabase"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper/scrapertest"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func buildTitle(row *gdb.Row, unmapped *scraper.UnmappedValues) *database.ScrapeWrite {
	parsed := readTags(row.Tags)
	b := newWriteBuilder("", unmapped)
	b.addTitle(row, &parsed)
	b.addSet(&parsed)
	return b.write
}

func TestAddTitleWritesGameFactsAndLeavesReleaseFacts(t *testing.T) {
	t.Parallel()
	write := buildTitle(&gdb.Row{
		Title: "Example", ID: "example", Region: "Japan", Date: "1995-07-21",
		Developer: "Nintendo/Tose/Nintendo", Publisher: "Nintendo",
		Tags: "#players:2:vs #genre:sports>tennis #lang:ja #rev:1 #unfinished:beta #disc:2>1 " +
			"#reboxed:players #arcadeboard:konami>gx:sega>system16",
	}, nil)
	scrapertest.RequireValidWrite(t, write)

	title := write.TitleTags
	assert.Equal(t, []string{"example"}, propertyValues(t, write.TitleProps, tags.TagPropertyGameDatabaseID))
	assert.Equal(t, []string{"1995"}, values(title, tags.TagTypeYear))
	assert.Equal(t, []string{"nintendo"}, values(title, tags.TagTypeDeveloper))
	assert.Equal(t, []string{"tose"}, values(title, tags.TagTypeCredit), "the developer is not also its own credit")
	assert.Equal(t, []string{"nintendo"}, values(title, tags.TagTypePublisher))
	assert.Equal(t, []string{"sports", "sports:tennis"}, values(title, tags.TagTypeGenre))
	assert.Equal(t, []string{"konami:gx"}, values(title, tags.TagTypeArcadeBoard), "a title holds one board")
	for _, releaseOnly := range []tags.TagType{
		tags.TagTypeLang, tags.TagTypeRev, tags.TagTypeUnfinished, tags.TagTypeDisc, tags.TagTypeDiscTotal,
		tags.TagTypeReboxed, tags.TagTypeRegion,
	} {
		assert.Empty(t, values(title, releaseOnly), "%s describes one release", releaseOnly)
	}
	assert.Empty(t, write.MediaTags)
	assert.Empty(t, write.MediaProps)

	for i := range title {
		if title[i].Type == string(tags.TagTypeDeveloper) {
			assert.Equal(t, "Nintendo", title[i].Label, "a company keeps upstream's spelling as its label")
		} else if title[i].Type == string(tags.TagTypeGenre) {
			assert.Empty(t, title[i].Label)
		}
	}
}

func TestAddTitleDropsWhatItCannotStore(t *testing.T) {
	t.Parallel()
	unmapped := &scraper.UnmappedValues{}
	write := buildTitle(&gdb.Row{
		Title: "Example", ID: "Not An ID", Date: "sometime", Developer: " / ", Publisher: "",
		Tags: "#genre:nosuchgenre #players:1",
	}, unmapped)
	scrapertest.RequireValidWrite(t, write)
	assert.Equal(t, []database.TagInfo{{Type: "players", Tag: "1"}}, write.TitleTags)
	assert.Empty(t, write.TitleProps, "invalid catalog IDs are not stored")
	assert.Equal(t, 1, unmapped.Count(tags.TagTypeGenre))
	assert.Equal(t, 1, unmapped.Count(tags.TagTypeYear), "an unlisted unreadable date is reported")

	quiet := &scraper.UnmappedValues{}
	buildTitle(&gdb.Row{Title: "Example", Date: "NAMCO", Tags: "#players:1"}, quiet)
	assert.Zero(t, quiet.Count(tags.TagTypeYear), "a listed one is dropped quietly")
}

func TestAddSet(t *testing.T) {
	t.Parallel()
	clone := buildTitle(&gdb.Row{Title: "10-Yard Fight", Tags: "#mamerom:10yard>10yardj"}, nil)
	assert.Equal(t, []database.MediaProperty{{
		TypeTag: tags.PropertyTypeTag(tags.TagPropertyMAMESetName), Text: "10yardj",
	}}, clone.MediaProps)
	assert.Equal(t, []database.TagInfo{{Type: "mameparent", Tag: "10yard"}}, clone.MediaTags)

	parent := buildTitle(&gdb.Row{Title: "10-Yard Fight", Tags: "#mameparent #mamerom:10yard"}, nil)
	assert.Len(t, parent.MediaProps, 1)
	assert.Empty(t, parent.MediaTags)

	b := newWriteBuilder("run-1", nil)
	assert.Equal(t, []database.TagInfo{scraper.RunTagInfo(scraperID, "run-1")}, b.write.MediaTags)
	assert.Equal(t, scraper.SentinelTagInfo(scraperID), b.write.Sentinel)
}

func TestVocabularyHelpers(t *testing.T) {
	t.Parallel()
	var (
		id string
		ok bool
	)
	for date, want := range map[string]string{
		"1995-07-21": "1995", "1995-07": "1995", "1995": "1995",
		"1989-1-31": "1989", "1990-06-": "1990", "1996-02-02[": "1996",
		"": "", "199": "", "19950": "", "995-06-01": "", "1949": "", "2100-01-01": "", "NAMCO": "",
	} {
		got, found := releaseYear(date)
		assert.Equal(t, want, got, date)
		assert.Equal(t, want != "", found, date)
	}
	padded, ok := releaseYear(" 1995 ")
	assert.True(t, ok)
	assert.Equal(t, "1995", padded)
	for date, want := range map[string]string{
		"1995-07-21": "1995-07-21", "1995-07": "1995-07-99", "1995": "1995-99-99",
		"1989-1-31": "1989-99-99", "1990-06-": "1990-06-99", "": "9999-99-99", "NAMCO": "9999-99-99",
	} {
		assert.Equal(t, want, sortDate(date), date)
	}

	id, ok = datasetID("supermarioworld")
	assert.True(t, ok)
	assert.Equal(t, "supermarioworld", id)
	id, ok = datasetID("Porter")
	assert.True(t, ok)
	assert.Equal(t, "porter", id)
	_, ok = datasetID("two ids/here")
	assert.False(t, ok)

	regions, unknown := regionValues("USA/Europe / South Korea/Unknown/")
	assert.Equal(t, []tags.TagValue{tags.TagRegionUS, tags.TagRegionEU, tags.TagRegionKR}, regions)
	assert.Empty(t, unknown)
	_, unknown = regionValues("Atlantis")
	assert.Equal(t, []string{"Atlantis"}, unknown)
}

func FuzzBuildWrite(f *testing.F) {
	f.Add("Mario's Tennis", "mariostennis", "Japan/USA", "1995-07-21", "Nintendo/Tose", "Nintendo",
		"#players:1 #genre:sports>tennis #search:3d>stereo:feature>mario #lang:en")
	f.Add("10-Yard Fight", "10yardfight", "World", "1983", "Irem", "Irem",
		"#input:joystick>8:buttons>2 #arcadeboard:irem>m58 #mameparent #mamerom:10yard")
	f.Add("", "", "", "", "/", "//", "#compilation:a:b>c #arcadeboard:konami:sega:taito")
	f.Fuzz(func(t *testing.T, title, id, region, date, developer, publisher, cell string) {
		row := &gdb.Row{
			Title: title, ID: id, Region: region, Date: date, Developer: developer, Publisher: publisher, Tags: cell,
		}
		write := buildTitle(row, &scraper.UnmappedValues{})
		scrapertest.RequireValidWrite(t, write)
		perType := map[string]int{}
		for _, info := range write.TitleTags {
			perType[info.Type]++
		}
		for tagType, count := range perType {
			if tags.IsExclusiveType(tags.TagType(tagType)) && count > 1 {
				t.Fatalf("%d %s values for one title", count, tagType)
			}
		}
		members := propertyValues(t, write.TitleProps, tags.TagPropertyGameDatabaseContains)
		seen := make(map[string]struct{}, len(members))
		for _, member := range members {
			require.True(t, validDatasetID(member))
			_, duplicate := seen[member]
			require.False(t, duplicate, "compilation property contains duplicate ID")
			seen[member] = struct{}{}
		}
		for _, info := range append(write.TitleTags, write.MediaTags...) {
			require.NotEqual(t, "compilation", info.Type)
			require.NotEqual(t, "gamedatabase", info.Type)
		}
		regionValues(region)
		sortDate(date)
	})
}

func propertyValues(t *testing.T, props []database.MediaProperty, property tags.TagValue) []string {
	t.Helper()
	for _, prop := range props {
		if prop.TypeTag != tags.PropertyTypeTag(property) {
			continue
		}
		if property == tags.TagPropertyGameDatabaseContains {
			var ids []string
			require.NoError(t, json.Unmarshal([]byte(prop.Text), &ids))
			return ids
		}
		return []string{prop.Text}
	}
	return nil
}

func TestCatalogIDsArePropertiesNotTags(t *testing.T) {
	t.Parallel()
	write := buildTitle(&gdb.Row{ID: "example", Tags: "#compilation:starforce:starsoldier>hector87:starforce"}, nil)
	scrapertest.RequireValidWrite(t, write)
	assert.Equal(t, []string{"example"}, propertyValues(t, write.TitleProps, tags.TagPropertyGameDatabaseID))
	require.Len(t, write.TitleProps, 2)
	assert.Equal(t, []string{"starforce", "starsoldier", "hector87"},
		propertyValues(t, write.TitleProps, tags.TagPropertyGameDatabaseContains))
	assert.Equal(t, `["starforce","starsoldier","hector87"]`, write.TitleProps[1].Text)
	assert.Equal(t, []string{"compilation"}, values(write.TitleTags, tags.TagTypeMultigame))
	for _, tag := range append(write.TitleTags, write.MediaTags...) {
		assert.NotEqual(t, "gamedatabase", tag.Type)
		assert.NotEqual(t, "compilation", tag.Type)
	}
}

func TestCompilationPropertiesPreserveRawIDsAndRejectMalformedIDs(t *testing.T) {
	t.Parallel()
	unmapped := &scraper.UnmappedValues{}
	write := buildTitle(&gdb.Row{
		ID: "collection", Tags: "#compilation:culdcept:culcept:futuregame:culdcept:bad_id",
	}, unmapped)
	scrapertest.RequireValidWrite(t, write)
	assert.Equal(t, []string{"culdcept", "culcept", "futuregame"},
		propertyValues(t, write.TitleProps, tags.TagPropertyGameDatabaseContains),
		"preserve aliases and previously unseen valid IDs; deduplicate only identical strings")
	assert.Equal(t, 1, unmapped.Count(tags.TagTypeProperty))
	assert.Equal(t, []string{"compilation"}, values(write.TitleTags, tags.TagTypeMultigame))
	assert.Equal(t, []string{"collection"}, propertyValues(t, write.TitleProps, tags.TagPropertyGameDatabaseID))
	require.Len(t, write.TitleProps, 2)
	assert.Empty(t, write.MediaProps, "membership belongs to title, not ROM")
}

func TestCompilationWithoutValidMembersOmitsProperty(t *testing.T) {
	t.Parallel()
	for _, cell := range []string{"", "#compilation", "#compilation:bad_id"} {
		write := buildTitle(&gdb.Row{Tags: cell}, nil)
		scrapertest.RequireValidWrite(t, write)
		assert.Empty(t, write.TitleProps, cell)
	}
}

func TestDatasetIDFormat(t *testing.T) {
	t.Parallel()
	for _, id := range []string{"supermarioworld", "starsoldier", strings.Repeat("a", 96)} {
		assert.True(t, validDatasetID(id), id)
	}
	for _, id := range []string{"", "super-mario-world", "Star Soldier", "two/ids", strings.Repeat("a", 97)} {
		assert.False(t, validDatasetID(id), id)
	}
}
