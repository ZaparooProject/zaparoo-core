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
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseToken(t *testing.T) {
	t.Parallel()
	tok, ok := parseToken("#input:joystick>8:buttons>2")
	require.True(t, ok)
	assert.Equal(t, token{tagType: "input", subs: []subtag{
		{name: "joystick", children: []string{"8"}}, {name: "buttons", children: []string{"2"}},
	}}, tok)

	tok, ok = parseToken("#genre:action>platformer>runandgun")
	require.True(t, ok)
	assert.Equal(t, []string{"platformer", "runandgun"}, tok.subs[0].children)

	tok, ok = parseToken("#mameparent")
	require.True(t, ok)
	assert.Equal(t, token{tagType: "mameparent"}, tok)

	tok, ok = parseToken("#mamerom:mk_parent>mk_r1")
	require.True(t, ok, "MAME set names carry underscores")
	assert.Equal(t, "mk_parent", tok.subs[0].name)

	for _, bad := range []string{
		"", "#", "genre:action", "@genre:action", "#Genre:action", "#genre:", "#genre:action>",
		"#genre:action>>maze", "#genre::action", "#players:2;coop", "#lang:fr-ca", "#genre:action platform",
		"#" + strings.Repeat("a", maxTokenBytes),
		"#t" + strings.Repeat(":s", maxSubtags+1),
		"#t:s" + strings.Repeat(">c", maxChildren+1),
	} {
		_, ok := parseToken(bad)
		assert.False(t, ok, "%.40q", bad)
	}
}

func TestSplitTokensRecoversKnownMistakes(t *testing.T) {
	t.Parallel()
	tokens, unreadable := splitTokens("  #players:1   lang:en qsound #genre:puzzle  notatag ")
	require.Len(t, tokens, 3)
	assert.Equal(t, "players", tokens[0].tagType)
	assert.Equal(t, "lang", tokens[1].tagType, "a listed tag missing its marker is read")
	assert.Equal(t, "genre", tokens[2].tagType)
	assert.Equal(t, []string{"notatag"}, unreadable, "a listed note is dropped quietly, an unlisted one is reported")

	tokens, unreadable = splitTokens(strings.Repeat("#players:1 ", maxTokens+10))
	assert.Len(t, tokens, maxTokens)
	assert.Empty(t, unreadable)
}

func TestReadTags(t *testing.T) {
	t.Parallel()
	parsed := readTags("#genre:sports>wrestling #players:2:vs #input:joystick>8:buttons>2 #lang:ja #based")
	assert.Equal(t, []typedValue{
		{tags.TagTypeGenre, "sports"},
		{tags.TagTypeGenre, "sports:wrestling"},
		{tags.TagTypePlayers, "2"},
		{tags.TagTypePlayers, "vs"},
		{tags.TagTypeInput, "joystick:8"},
		{tags.TagTypeInput, "buttons:2"},
		{tags.TagTypeLang, "ja"},
		{tags.TagTypeBased, tags.TagBasedBased},
	}, parsed.values)
	assert.Empty(t, parsed.unknown)
	assert.False(t, parsed.variant)

	parsed = readTags("#genre:sim>driving #arcadeboard:nintendo>vssystem #players:2:coop>vs #save:backp")
	assert.Equal(t, []typedValue{
		{tags.TagTypeGenre, "sim"},
		{tags.TagTypeGenre, tags.TagGameGenreRacingDriving},
		{tags.TagTypeArcadeBoard, tags.TagArcadeBoardNintendoVS},
		{tags.TagTypePlayers, "2"},
		{tags.TagTypePlayers, tags.TagPlayersCoop},
		{tags.TagTypePlayers, tags.TagPlayersVS},
		{tags.TagTypeSave, tags.TagSaveBackup},
	}, parsed.values, "renames and repairs land on Core's values")

	parsed = readTags("#genre:nosuchgenre #newtype:value #lang notatag")
	assert.Empty(t, parsed.values)
	assert.Equal(t, []typedValue{
		{tags.TagTypeUnknown, "notatag"},
		{tags.TagTypeGenre, "nosuchgenre"},
		{"newtype", "value"},
	}, parsed.unknown, "a bare tag that says nothing is skipped; the rest is reported")

	for _, cell := range []string{
		"#unfinished:beta", "#unlicensed", "#alt:1", "#reboxed", "#rerelease:virtualconsole",
	} {
		assert.True(t, readTags(cell).variant, cell)
	}
}

func TestReadTagsSets(t *testing.T) {
	t.Parallel()
	clone := readTags("#mamerom:10yard>10yardj")
	assert.Equal(t, "10yardj", clone.setName)
	assert.Equal(t, "10yard", clone.setParent)

	parent := readTags("#mameparent #mamerom:10yard")
	assert.Equal(t, "10yard", parent.setName)
	assert.Empty(t, parent.setParent)
	assert.Empty(t, parent.unknown)

	for _, bad := range []string{"#mamerom", "#mamerom:a:b", "#mamerom:a>b>c", "#mamerom:" + strings.Repeat("x", 40)} {
		parsed := readTags(bad)
		assert.Empty(t, parsed.setName, bad)
		assert.NotEmpty(t, parsed.unknown, bad)
	}
}

func TestReadTagsCompilationsAndDiscs(t *testing.T) {
	t.Parallel()
	parsed := readTags("#compilation:starforce:starsoldier>hector87")
	assert.Equal(t, []string{"starforce", "starsoldier", "hector87"}, parsed.contains,
		"both source separators preserve raw member IDs")
	assert.Equal(t, []typedValue{{tags.TagTypeMultigame, tags.TagMultigameCompilation}}, parsed.values)

	bare := readTags("#compilation")
	assert.Empty(t, bare.contains)
	assert.Empty(t, bare.unknown)
	assert.Equal(t, []typedValue{{tags.TagTypeMultigame, tags.TagMultigameCompilation}}, bare.values)

	disc := readTags("#disc:2>1")
	assert.Equal(t, []typedValue{{tags.TagTypeDiscTotal, "2"}, {tags.TagTypeDisc, "1"}}, disc.values)
	assert.Empty(t, disc.unknown)
}

func FuzzReadTags(f *testing.F) {
	for _, seed := range []string{
		"#players:1 #genre:parlor>pinball #embedded:backup>battery #save:backup #search:3d>stereo #lang:en",
		"#input:joystick>8:buttons>2 #players:2:alt #genre:sports>football #arcadeboard:irem>m58 #mamerom:10yard",
		"#compilation:starforce:starsoldier:hector87 #port:arcade:nintendo>famicom",
		"joystick>8:buttons>2 #players:2;coop lang:en [b] (1993-03-17) @reboxed:banprestobest",
		"#genre:action>platformer:adventure:shooting>>rail #disc:2>1 #mameparent #based",
		"", "#", "#:", "#>", "#a:>", "# # #", "#a:b>c>d>e:f>g",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, cell string) {
		parsed := readTags(cell)
		if len(parsed.values) > maxTokens*(maxSubtags*(maxChildren+1)+1) {
			t.Fatalf("%d values from one cell", len(parsed.values))
		}
		for _, value := range parsed.values {
			if err := tags.ValidateTagValue(value.Type, string(value.Value)); err != nil {
				t.Fatalf("accepted a value the vocabulary refuses: %v", err)
			}
		}
		if parsed.setName != "" && !tags.IsValidTagValue(tags.TagTypeMameParent, parsed.setName) {
			t.Fatalf("accepted set name %q", parsed.setName)
		}
		for _, id := range parsed.contains {
			if !validDatasetID(id) {
				t.Fatalf("accepted compilation ID %q", id)
			}
		}
	})
}
