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

package misterarcade

import (
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper/scrapertest"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cps1Entry is a real catalog row: the World release of 1941, which exercises
// every column the scraper reads.
func cps1Entry() Entry {
	return Entry{
		SetName: "1941", Region: "World", Version: "900227", Alternative: "yes",
		ParentTitle: "1941- Counter Attack", Platform: "Capcom CPS-1", Series: "19XX",
		Homebrew: "no", Bootleg: "no", Year: "1990", Manufacturer: "Capcom",
		Category: "Shooter - Flying Vertical", Resolution: "15kHz", Rotation: "vertical (ccw)",
		Players: "2 (simultaneous)", MoveInputs: "8-way", SpecialControls: "", NumButtons: "2",
		Flip: "yes",
	}
}

// validWrite builds a write and fails the test for any tag in it the
// vocabulary would refuse.
func validWrite(t *testing.T, entry *Entry, runID string, unmapped *scraper.UnmappedValues) *database.ScrapeWrite {
	t.Helper()
	write := buildWrite(entry, runID, unmapped)
	scrapertest.RequireValidWrite(t, write)
	return write
}

func tagValues(list []database.TagInfo, tagType tags.TagType) []string {
	values := make([]string, 0, 2)
	for _, tag := range list {
		if tag.Type == string(tagType) {
			values = append(values, tag.Tag)
		}
	}
	return values
}

func propText(props []database.MediaProperty, value tags.TagValue) string {
	for _, prop := range props {
		if prop.TypeTag == tags.PropertyTypeTag(value) {
			return prop.Text
		}
	}
	return ""
}

func TestBuildWriteMapsEveryReadColumn(t *testing.T) {
	t.Parallel()
	entry := cps1Entry()
	unmapped := &scraper.UnmappedValues{}
	write := validWrite(t, &entry, "", unmapped)

	assert.Equal(t, scraper.SentinelTagInfo(scraperID), write.Sentinel)
	assert.Equal(t, []string{"1990"}, tagValues(write.TitleTags, tags.TagTypeYear))
	assert.Equal(t, []string{"capcom"}, tagValues(write.TitleTags, tags.TagTypeDeveloper))
	assert.Equal(t, []string{"shmup:v", "shmup"},
		tagValues(write.TitleTags, tags.TagTypeGenre), "the canonical genre is written with its parent")
	assert.Equal(t, []string{"capcom:cps"}, tagValues(write.TitleTags, tags.TagTypeArcadeBoard),
		"the catalog's CPS-1 is the canonical capcom:cps")
	assert.Equal(t, []string{"2", "simultaneous"}, tagValues(write.TitleTags, tags.TagTypePlayers))
	assert.Equal(t, []string{"joystick:8", "buttons:2"}, tagValues(write.TitleTags, tags.TagTypeInput))
	assert.Equal(t, []string{"15khz"}, tagValues(write.TitleTags, tags.TagTypeVideo))
	assert.Equal(t, []string{"franchise:19xx", "tate:ccw", "keyword:flip"},
		tagValues(write.TitleTags, tags.TagTypeSearch), "the series is written as a franchise")
	assert.Empty(t, tagValues(write.TitleTags, tags.TagTypeRelease), "this set is not homebrew")

	assert.Equal(t, []string{"world"}, tagValues(write.MediaTags, tags.TagTypeRegion))
	assert.Equal(t, []string{"1990-02-27"}, tagValues(write.MediaTags, tags.TagTypeBuildDate),
		"a six-digit version is the romset build date")
	assert.Equal(t, []string{"alt"}, tagValues(write.MediaTags, tags.TagTypeAlt))
	assert.Empty(t, tagValues(write.MediaTags, tags.TagTypeUnlicensed))
	assert.Equal(t, "1941", propText(write.MediaProps, tags.TagPropertyMAMESetName))

	for _, tagType := range []tags.TagType{
		tags.TagTypeGenre, tags.TagTypeSearch, tags.TagTypeArcadeBoard, tags.TagTypePlayers,
		tags.TagTypeInput, tags.TagTypeRegion, tags.TagTypeYear, tags.TagTypeVideo,
	} {
		assert.Zero(t, unmapped.Count(tagType), "a fully catalogued row drops nothing from %s", tagType)
	}
}

func TestBuildWriteLabelsOnlyCompanyNames(t *testing.T) {
	t.Parallel()
	entry := cps1Entry()
	write := validWrite(t, &entry, "", nil)
	for _, tag := range append(append([]database.TagInfo{}, write.TitleTags...), write.MediaTags...) {
		if tag.Type == string(tags.TagTypeDeveloper) {
			assert.Equal(t, "Capcom", tag.Label, "a company name keeps the catalog's spelling")
			continue
		}
		assert.Empty(t, tag.Label, "%s:%s is a closed or format tag and carries no label", tag.Type, tag.Tag)
	}
}

func TestBuildWriteWritesRunMarkerOnlyForARun(t *testing.T) {
	t.Parallel()
	entry := cps1Entry()

	write := validWrite(t, &entry, "", nil)
	assert.Empty(t, tagValues(write.MediaTags, tags.ScraperRunType(scraperID)))

	write = validWrite(t, &entry, "run-7", nil)
	assert.Equal(t, []string{"run-7"}, tagValues(write.MediaTags, tags.ScraperRunType(scraperID)))
}

func TestBuildWriteSkipsSentinelAndDirtyValues(t *testing.T) {
	t.Parallel()
	// Every column here says "nothing" in one of the spellings the catalog uses,
	// except the misspelled bootleg flag upstream actually ships.
	entry := Entry{
		SetName: "sparse", Region: "", Version: "n-a", Alternative: "no",
		Platform: "", Series: "", Homebrew: "n-a", Bootleg: "ys", Year: "19xx",
		Manufacturer: "n-a", Category: "", Resolution: "n-a", Rotation: "horizontal",
		Players: "n-a", MoveInputs: "n-a", SpecialControls: "n-a", NumButtons: "0",
		Flip: "n-a",
	}
	write := validWrite(t, &entry, "", nil)

	assert.Empty(t, write.TitleTags, "a row of sentinels writes no title metadata")
	assert.Equal(t, []string{"bootleg"}, tagValues(write.MediaTags, tags.TagTypeUnlicensed),
		"the upstream \"ys\" typo still means yes")
	assert.Len(t, write.MediaTags, 1)
	assert.Equal(t, "sparse", propText(write.MediaProps, tags.TagPropertyMAMESetName))
}

func TestBuildWriteUnescapesCatalogText(t *testing.T) {
	t.Parallel()
	entry := Entry{SetName: "arkanoid", Category: "Ball &amp; Paddle - Breakout"}
	write := validWrite(t, &entry, "", nil)
	assert.Equal(t, []string{"action:blockbreaker", "action"},
		tagValues(write.TitleTags, tags.TagTypeGenre))
}

func TestBuildWriteWritesTheSeriesAsAFranchise(t *testing.T) {
	t.Parallel()
	franchises := func(entry *Entry, unmapped *scraper.UnmappedValues) []string {
		write := validWrite(t, entry, "", unmapped)
		values := make([]string, 0, 1)
		for _, value := range tagValues(write.TitleTags, tags.TagTypeSearch) {
			if strings.HasPrefix(value, "franchise:") {
				values = append(values, value)
			}
		}
		return values
	}

	withSeries := Entry{SetName: "a", Series: "19XX", ParentTitle: "1941- Counter Attack"}
	assert.Equal(t, []string{"franchise:19xx"}, franchises(&withSeries, nil))

	spelled := Entry{SetName: "b", Series: "Ghosts 'n"}
	assert.Equal(t, []string{"franchise:ghostsngoblins"}, franchises(&spelled, nil),
		"the catalog's truncated spelling reaches the franchise")

	// A parent title is not a series; it is used only when the catalog names
	// no series and the title is itself a listed franchise.
	parentIsFranchise := Entry{SetName: "c", ParentTitle: "Galaga"}
	assert.Equal(t, []string{"franchise:galaga"}, franchises(&parentIsFranchise, nil))
	parentIsATitle := Entry{SetName: "d", ParentTitle: "1941- Counter Attack"}
	unmapped := &scraper.UnmappedValues{}
	assert.Empty(t, franchises(&parentIsATitle, unmapped))
	assert.Zero(t, unmapped.Count(tags.TagTypeSearch), "a parent title that is not a franchise is not a drop")

	notAFranchise := Entry{SetName: "e", Series: "Marvel - Capcom"}
	unmapped = &scraper.UnmappedValues{}
	assert.Empty(t, franchises(&notAFranchise, unmapped))
	assert.Zero(t, unmapped.Count(tags.TagTypeSearch), "a curated non-franchise is dropped silently")

	unknown := Entry{SetName: "f", Series: "Some New Saga", ParentTitle: "Galaga"}
	unmapped = &scraper.UnmappedValues{}
	assert.Empty(t, franchises(&unknown, unmapped), "an unknown series is not replaced by the parent title")
	assert.Equal(t, 1, unmapped.Count(tags.TagTypeSearch))
}

func TestBuildWriteMapsTheBoardOntoTheCanonicalList(t *testing.T) {
	t.Parallel()
	board := func(platform string, unmapped *scraper.UnmappedValues) []string {
		entry := Entry{SetName: "x", Platform: platform}
		write := validWrite(t, &entry, "", unmapped)
		return tagValues(write.TitleTags, tags.TagTypeArcadeBoard)
	}
	assert.Equal(t, []string{string(tags.TagArcadeBoardCapcomCPS)}, board("Capcom CPS-1", nil))
	assert.Equal(t, []string{string(tags.TagArcadeBoardCapcomCPS2)}, board("Capcom CPS-2", nil))
	assert.Equal(t, []string{string(tags.TagArcadeBoardNamcoPacMan)}, board("Namco Pac-Man hardware", nil))
	assert.Equal(t, []string{string(tags.TagArcadeBoardToaplanVersion1)}, board("Toaplan 1", nil))

	unmapped := &scraper.UnmappedValues{}
	assert.Empty(t, board("Konami Unique", unmapped), "a catch-all names no board")
	assert.Zero(t, unmapped.Count(tags.TagTypeArcadeBoard), "a curated skip is dropped silently")

	assert.Empty(t, board("Mystery Board 9000", unmapped))
	assert.Equal(t, 1, unmapped.Count(tags.TagTypeArcadeBoard), "an unknown board is reported")
}

func TestBuildWriteNotesEveryDroppedValue(t *testing.T) {
	t.Parallel()
	entry := Entry{
		SetName: "odd", Category: "Unheard Of - Genre", Platform: "Mystery Board",
		Series: "Some New Saga", Region: "Hispanic", Players: "many", NumButtons: "9",
		MoveInputs: "hovercraft yoke", Year: "19xx", Resolution: "25kHz",
	}
	unmapped := &scraper.UnmappedValues{}
	write := validWrite(t, &entry, "", unmapped)

	for _, tagType := range []tags.TagType{
		tags.TagTypeGenre, tags.TagTypeArcadeBoard, tags.TagTypeSearch, tags.TagTypeRegion,
		tags.TagTypePlayers, tags.TagTypeYear, tags.TagTypeVideo,
	} {
		assert.Equal(t, 1, unmapped.Count(tagType), "%s", tagType)
	}
	assert.Equal(t, 2, unmapped.Count(tags.TagTypeInput), "the unlisted button count and the control phrase")
	assert.Empty(t, write.TitleTags)
}

func TestGenreTags(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		category string
		want     []string
		unknown  bool
	}{
		{category: "Shooter - Flying Vertical", want: []string{"shmup:v", "shmup"}},
		{category: "Shooter - Flying Horizontal", want: []string{"shmup:h", "shmup"}},
		{category: "Shooter - Flying Diagonal", want: []string{"shmup:i", "shmup"}},
		{category: "Shooter - Gallery", want: []string{"shooting:gallery", "shooting"}},
		{category: "Fighter - Versus", want: []string{"fighting"}},
		{category: "Fighter - 2.5D", want: []string{"brawler"}},
		{category: "Platform - Shooter Scrolling", want: []string{"action:runandgun", "action"}},
		{category: "Sports - Soccer", want: []string{"sports:soccer", "sports"}},
		{category: "Tabletop - Mahjong", want: []string{"board:mahjong", "board"}},
		{category: "Driving - Race", want: []string{"racing"}},
		{category: "Quiz - Questions in Japanese", want: []string{"quiz"}},
		{category: "Platform - Run Jump [Mature]", want: []string{"action:platformer", "action"}},
		{category: "SHOOTER - FLYING VERTICAL", want: []string{"shmup:v", "shmup"}},
		{category: "System - BIOS"},
		{category: "n-a"},
		{category: "Unheard Of - Genre", unknown: true},
	} {
		values, known := genreTags(tc.category)
		got := make([]string, 0, len(values))
		for _, value := range values {
			got = append(got, string(value))
			require.NoError(t, tags.ValidateTagValue(tags.TagTypeGenre, string(value)))
		}
		assert.Equal(t, !tc.unknown, known, "%q", tc.category)
		if tc.want == nil {
			assert.Empty(t, got, "%q", tc.category)
			continue
		}
		assert.Equal(t, tc.want, got, "%q", tc.category)
	}
}

func TestBuildWriteReadsBootlegFromTheRegionColumn(t *testing.T) {
	t.Parallel()
	// The catalog files some unlicensed sets under region; that states a
	// provenance, not a territory.
	entry := Entry{SetName: "bl", Region: "bootleg"}
	write := validWrite(t, &entry, "", nil)
	assert.Empty(t, tagValues(write.MediaTags, tags.TagTypeRegion))
	assert.Equal(t, []string{"bootleg"}, tagValues(write.MediaTags, tags.TagTypeUnlicensed))
}

func TestBuildWriteSplitsMultiRegionAndDropsUnknownTerritories(t *testing.T) {
	t.Parallel()
	entry := Entry{SetName: "multi", Region: "USA - Asia"}
	assert.Equal(t, []string{"us", "asia"}, tagValues(validWrite(t, &entry, "", nil).MediaTags, tags.TagTypeRegion))

	unknown := Entry{SetName: "hisp", Region: "Hispanic"}
	assert.Empty(t, tagValues(validWrite(t, &unknown, "", nil).MediaTags, tags.TagTypeRegion),
		"a word with no canonical region is dropped, not invented")
}

func TestBuildWriteDeduplicatesBootlegAcrossColumns(t *testing.T) {
	t.Parallel()
	entry := Entry{SetName: "dupe", Region: "bootleg", Bootleg: "yes", Version: "bootleg"}
	assert.Equal(t, []string{"bootleg"}, tagValues(validWrite(t, &entry, "", nil).MediaTags, tags.TagTypeUnlicensed))
}

func TestPlayerTags(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		column  string
		want    []string
		dropped bool
	}{
		{name: "single player", column: "1", want: []string{"1"}},
		{name: "two simultaneous", column: "2 (simultaneous)", want: []string{"2", "simultaneous"}},
		{name: "two alternating", column: "2 (alternating)", want: []string{"2", "alt"}},
		{
			name: "a range supports every count in it", column: "2-4 (simultaneous)",
			want: []string{"2", "3", "4", "simultaneous"},
		},
		{name: "sentinel", column: "n-a", want: nil},
		{name: "unparseable", column: "many", want: nil, dropped: true},
		{name: "beyond the vocabulary", column: "40", want: nil, dropped: true},
		{name: "reversed range", column: "4-2", want: nil, dropped: true},
		{
			name: "an unlisted count in a range is dropped", column: "10-12",
			want: []string{"10", "12"}, dropped: true,
		},
		{name: "an unknown mode is dropped", column: "2 (linked)", want: []string{"2"}, dropped: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			values, ok := playerTags(tc.column)
			assert.Equal(t, !tc.dropped, ok)
			got := make([]string, 0, len(tc.want))
			for _, value := range values {
				got = append(got, string(value))
			}
			if tc.want == nil {
				assert.Empty(t, got)
				return
			}
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestControlTagsMapsEveryPhraseTheCatalogUses(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		move    string
		special string
		want    []string
		unknown []string
	}{
		{name: "eight way", move: "8-way", want: []string{"joystick:8"}},
		{name: "case is ignored", move: "Trackball", want: []string{"trackball"}},
		{
			name: "a spaced hyphen separates phrases", move: "Stick - Pedal",
			want: []string{"pedals:1"},
		},
		{
			name: "a bare hyphen does not", move: "8-way - Pedal",
			want: []string{"joystick:8", "pedals:1"},
		},
		{
			name: "commas separate", move: "8-way,Positional",
			want: []string{"joystick:8"},
		},
		{
			name: "both columns contribute", move: "8-way", special: "twin stick",
			want: []string{"joystick:8", "stick:twin"},
		},
		{
			name: "a repeated control is written once", move: "Trackball", special: "trackball, twin stick",
			want: []string{"trackball", "stick:twin"},
		},
		{name: "double joystick spellings agree", special: "Double Joysticks", want: []string{"joystick:double"}},
		{name: "eight way double states both", move: "8-way double", want: []string{"joystick:8", "joystick:double"}},
		{name: "sentinels say nothing", move: "n-a", special: ""},
		{name: "an ambiguous phrase is dropped, not guessed", move: "2-way"},
		{name: "a bare count is dropped", move: "2", special: "Buttons Only"},
		{
			name: "an unlisted phrase is reported", move: "8-way", special: "hovercraft yoke",
			want: []string{"joystick:8"}, unknown: []string{"hovercraft yoke"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			values, unknown := controlTags(tc.move, tc.special)
			got := make([]string, 0, len(values))
			for _, value := range values {
				got = append(got, string(value))
			}
			if tc.want == nil {
				assert.Empty(t, got)
			} else {
				assert.Equal(t, tc.want, got)
			}
			if tc.unknown == nil {
				assert.Empty(t, unknown)
			} else {
				assert.Equal(t, tc.unknown, unknown)
			}
		})
	}
}

func TestButtonTag(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		column string
		want   string
	}{
		{column: "2", want: "buttons:2"},
		{column: "20", want: "buttons:20"},
		{column: "12", want: "buttons:12"},
		// Counts the vocabulary does not list are not written.
		{column: "9", want: ""},
		{column: "32", want: ""},
		{column: "0", want: ""},
		{column: "", want: ""},
		{column: "many", want: ""},
		{column: "999", want: ""},
	} {
		t.Run("buttons "+tc.column, func(t *testing.T) {
			t.Parallel()
			value, ok := buttonTag(tc.column)
			assert.Equal(t, tc.want != "", ok)
			assert.Equal(t, tc.want, string(value))
		})
	}
}

func TestVersionTagRoutesOnlyModelledForms(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		column  string
		tagType tags.TagType
		value   string
	}{
		{column: "900227", tagType: tags.TagTypeBuildDate, value: "1990-02-27"},
		{column: "Rev A", tagType: tags.TagTypeRev, value: "a"},
		{column: "Rev. 2", tagType: tags.TagTypeRev, value: "2"},
		{column: "Set 3", tagType: tags.TagTypeSet, value: "3"},
		{column: "Prototype", tagType: tags.TagTypeUnfinished, value: "proto"},
		{column: "bootleg", tagType: tags.TagTypeUnlicensed, value: "bootleg"},
		{column: "FD1089B 317-xxxx", tagType: tags.TagTypeProtection, value: "fd1089"},
		{column: "No Protection", tagType: tags.TagTypeProtection, value: "no-protection"},
		// Free-form notes about one dump are left alone rather than forced into
		// a type that does not describe them.
		{column: "Phoenix Edition"},
		{column: "Fri Feb 13 1998"},
		{column: "Old Ver"},
		{column: "n-a"},
	} {
		t.Run("version "+tc.column, func(t *testing.T) {
			t.Parallel()
			tagType, value, ok := versionTag(tc.column)
			if tc.value == "" {
				assert.False(t, ok, "expected no route for %q", tc.column)
				return
			}
			require.True(t, ok, "expected a route for %q", tc.column)
			assert.Equal(t, tc.tagType, tagType)
			assert.Equal(t, tc.value, string(value))
		})
	}
}

func TestScanRateAndRotation(t *testing.T) {
	t.Parallel()
	assert.Equal(t, string(tags.TagVideo15KHz), string(scanRateValue("15kHz")))
	assert.Equal(t, string(tags.TagVideo15KHz), string(scanRateValue("15 kHz")), "upstream has one spaced row")
	assert.Equal(t, string(tags.TagVideo31KHz), string(scanRateValue("31kHz")))
	assert.Empty(t, string(scanRateValue("n-a")))

	assert.Equal(t, string(tags.TagSearchTateCW), string(tateValue("vertical (cw)")))
	assert.Equal(t, string(tags.TagSearchTateCCW), string(tateValue("vertical (ccw)")))
	assert.Empty(t, string(tateValue("horizontal")), "a horizontal monitor is the default")
	assert.Empty(t, string(tateValue("horizontal (180)")))
}

func TestIndexKeepsTheFirstRowForARepeatedSetName(t *testing.T) {
	t.Parallel()
	byName := index([]Entry{
		{SetName: "Pacman", Year: "1980"},
		{SetName: "pacman", Year: "1981"},
		{SetName: "", Year: "1982"},
	})
	require.Len(t, byName, 1)
	require.Contains(t, byName, "pacman")
	assert.Equal(t, "1980", byName["pacman"].Year)
}
