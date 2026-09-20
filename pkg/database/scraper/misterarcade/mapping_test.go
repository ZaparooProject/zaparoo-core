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
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
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
	write := buildWrite(&entry, "")

	assert.Equal(t, scraper.SentinelTagInfo(scraperID), write.Sentinel)
	assert.Equal(t, []string{"1990"}, tagValues(write.TitleTags, tags.TagTypeYear))
	assert.Equal(t, []string{"capcom"}, tagValues(write.TitleTags, tags.TagTypeDeveloper))
	assert.Equal(t, []string{"shooter-flying-vertical", "shooter"},
		tagValues(write.TitleTags, tags.TagTypeGenre), "the family is written beside the full genre")
	assert.Equal(t, []string{"19xx"}, tagValues(write.TitleTags, tags.TagTypeGameFamily))
	assert.Equal(t, []string{"capcom:cps1"}, tagValues(write.TitleTags, tags.TagTypeArcadeBoard))
	assert.Equal(t, []string{"2", "simultaneous"}, tagValues(write.TitleTags, tags.TagTypePlayers))
	assert.Equal(t, []string{"joystick:8", "buttons:2"}, tagValues(write.TitleTags, tags.TagTypeInput))
	assert.Equal(t, []string{"15khz"}, tagValues(write.TitleTags, tags.TagTypeVideo))
	assert.Equal(t, []string{"tate:ccw", "keyword:flip"}, tagValues(write.TitleTags, tags.TagTypeSearch))
	assert.Empty(t, tagValues(write.TitleTags, tags.TagTypeRelease), "this set is not homebrew")

	assert.Equal(t, []string{"world"}, tagValues(write.MediaTags, tags.TagTypeRegion))
	assert.Equal(t, []string{"1990-02-27"}, tagValues(write.MediaTags, tags.TagTypeBuildDate),
		"a six-digit version is the romset build date")
	assert.Equal(t, []string{"alt"}, tagValues(write.MediaTags, tags.TagTypeAlt))
	assert.Empty(t, tagValues(write.MediaTags, tags.TagTypeUnlicensed))
	assert.Equal(t, "1941", propText(write.MediaProps, tags.TagPropertyMAMESetName))
}

func TestBuildWriteWritesRunMarkerOnlyForARun(t *testing.T) {
	t.Parallel()
	entry := cps1Entry()

	write := buildWrite(&entry, "")
	assert.Empty(t, tagValues(write.MediaTags, tags.ScraperRunType(scraperID)))

	write = buildWrite(&entry, "run-7")
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
	write := buildWrite(&entry, "")

	assert.Empty(t, write.TitleTags, "a row of sentinels writes no title metadata")
	assert.Equal(t, []string{"bootleg"}, tagValues(write.MediaTags, tags.TagTypeUnlicensed),
		"the upstream \"ys\" typo still means yes")
	assert.Len(t, write.MediaTags, 1)
	assert.Equal(t, "sparse", propText(write.MediaProps, tags.TagPropertyMAMESetName))
}

func TestBuildWriteUnescapesCatalogText(t *testing.T) {
	t.Parallel()
	entry := Entry{SetName: "arkanoid", Category: "Ball &amp; Paddle - Breakout"}
	write := buildWrite(&entry, "")
	assert.Equal(t, []string{"ball-paddle-breakout", "ball-paddle"},
		tagValues(write.TitleTags, tags.TagTypeGenre))
}

func TestBuildWritePrefersSeriesOverParentTitleForFamily(t *testing.T) {
	t.Parallel()
	// gamefamily holds one value, so the two candidate columns cannot both win.
	withSeries := Entry{SetName: "a", Series: "19XX", ParentTitle: "1941- Counter Attack"}
	assert.Equal(t, []string{"19xx"}, tagValues(buildWrite(&withSeries, "").TitleTags, tags.TagTypeGameFamily))

	withoutSeries := Entry{SetName: "b", ParentTitle: "Dokaben"}
	assert.Equal(t, []string{"dokaben"}, tagValues(buildWrite(&withoutSeries, "").TitleTags, tags.TagTypeGameFamily))

	neither := Entry{SetName: "c"}
	assert.Empty(t, tagValues(buildWrite(&neither, "").TitleTags, tags.TagTypeGameFamily))
}

func TestBuildWriteReadsBootlegFromTheRegionColumn(t *testing.T) {
	t.Parallel()
	// The catalog files some unlicensed sets under region; that states a
	// provenance, not a territory.
	entry := Entry{SetName: "bl", Region: "bootleg"}
	write := buildWrite(&entry, "")
	assert.Empty(t, tagValues(write.MediaTags, tags.TagTypeRegion))
	assert.Equal(t, []string{"bootleg"}, tagValues(write.MediaTags, tags.TagTypeUnlicensed))
}

func TestBuildWriteSplitsMultiRegionAndDropsUnknownTerritories(t *testing.T) {
	t.Parallel()
	entry := Entry{SetName: "multi", Region: "USA - Asia"}
	assert.Equal(t, []string{"us", "asia"}, tagValues(buildWrite(&entry, "").MediaTags, tags.TagTypeRegion))

	unknown := Entry{SetName: "hisp", Region: "Hispanic"}
	assert.Empty(t, tagValues(buildWrite(&unknown, "").MediaTags, tags.TagTypeRegion),
		"a word with no canonical region is dropped, not invented")
}

func TestBuildWriteDeduplicatesBootlegAcrossColumns(t *testing.T) {
	t.Parallel()
	entry := Entry{SetName: "dupe", Region: "bootleg", Bootleg: "yes", Version: "bootleg"}
	assert.Equal(t, []string{"bootleg"}, tagValues(buildWrite(&entry, "").MediaTags, tags.TagTypeUnlicensed))
}

func TestPlayerTags(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		column string
		want   []string
	}{
		{name: "single player", column: "1", want: []string{"1"}},
		{name: "two simultaneous", column: "2 (simultaneous)", want: []string{"2", "simultaneous"}},
		{name: "two alternating", column: "2 (alternating)", want: []string{"2", "alt"}},
		{
			name: "a range supports every count in it", column: "2-4 (simultaneous)",
			want: []string{"2", "3", "4", "simultaneous"},
		},
		{name: "sentinel", column: "n-a", want: nil},
		{name: "unparseable", column: "many", want: nil},
		{name: "beyond the vocabulary", column: "40", want: nil},
		{name: "reversed range", column: "4-2", want: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := make([]string, 0, len(tc.want))
			for _, value := range playerTags(tc.column) {
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
			want: []string{"pedals:1"}, unknown: []string{"stick"},
		},
		{
			name: "a bare hyphen does not", move: "8-way - Pedal",
			want: []string{"joystick:8", "pedals:1"},
		},
		{
			name: "commas separate", move: "8-way,Positional",
			want: []string{"joystick:8"}, unknown: []string{"positional"},
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
		{
			name: "an ambiguous phrase is reported, not guessed", move: "2-way",
			unknown: []string{"2-way"},
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

func TestArcadeBoardValueUsesCanonicalSpellingWhenOneExists(t *testing.T) {
	t.Parallel()
	assert.Equal(t, string(tags.TagArcadeBoardCapcomCPS2), string(arcadeBoardValue("Capcom CPS-2")))
	assert.Equal(t, string(tags.TagArcadeBoardIremM72), string(arcadeBoardValue("Irem M72")))
	assert.Equal(t, string(tags.TagArcadeBoardSegaSystem16), string(arcadeBoardValue("Sega System 16")))
	// The catalog names far more boards than the canonical list does; keeping
	// its own spelling is better than leaving most arcade games with none.
	assert.Equal(t, "namco:pacmanhardware", string(arcadeBoardValue("Namco Pac-Man hardware")))
	assert.Equal(t, "toaplan", string(arcadeBoardValue("Toaplan")))
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
