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

package mediascanner

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/stretchr/testify/require"
)

// scanRulesSamples are files whose parsed form stands for the parsing rules
// as a whole: region, revision, disc and language tags, articles, subtitles,
// numbering, non-Latin titles, provided names, arcade sets, ScummVM targets
// and the non-game media types.
var scanRulesSamples = []struct {
	system       string
	name         string
	providedName string
	noExt        bool
}{
	{system: systemdefs.SystemNES, name: "Super Mario Bros. 3 (USA) (Rev 1).nes"},
	{system: systemdefs.SystemNES, name: "Legend of Zelda, The (Europe) (En,Fr,De).nes"},
	{system: systemdefs.SystemNES, name: "Mega Man 2 [!].nes"},
	{system: systemdefs.SystemNES, name: "001 - Contra (Japan) (Beta) [T+Eng].nes"},
	{system: systemdefs.SystemSNES, name: "Final Fantasy III (USA) (Rev 1) [h1].sfc"},
	{system: systemdefs.SystemSNES, name: "Zelda no Densetsu - Kamigami no Triforce (Japan) (v1.2).sfc"},
	{
		system: systemdefs.SystemSNES,
		name:   "ドラゴンクエストV 天空の花嫁 (Japan).sfc", //nolint:gosmopolitan // non-Latin title sample
	},
	{system: systemdefs.SystemSNES, name: "Pokémon Stadium & Friends II - The Sequel (World) (Proto).sfc"},
	{system: systemdefs.SystemPSX, name: "Final Fantasy VII (USA) (Disc 2).chd"},
	{system: systemdefs.SystemPSX, name: "Metal Gear Solid (Europe) (Disc 1 of 2) (Demo).cue"},
	{system: systemdefs.SystemPSX, name: "SLUS-00594", noExt: true},
	{system: systemdefs.SystemGenesis, name: "Sonic the Hedgehog 2 (World) (Rev A) (Unl).md"},
	{system: systemdefs.SystemGenesis, name: "a_game-with_odd.separators (1993)(Publisher)(PD).bin"},
	{system: systemdefs.SystemGenesis, name: "file.bin", providedName: "A Provided Name: Special Edition"},
	{system: systemdefs.SystemArcade, name: "sf2.zip"},
	{system: systemdefs.SystemArcade, name: "Street Fighter II - The World Warrior (World 910522).mra"},
	{system: systemdefs.SystemArcade, name: "notarealsetname.zip"},
	{system: systemdefs.SystemScummVM, name: "monkey.scummvm"},
	{system: systemdefs.SystemScummVM, name: "Day of the Tentacle (CD DOS).svm"},
	{system: systemdefs.SystemMovie, name: "The Matrix Reloaded (2003) [1080p] {imdb-tt0234215}.mkv"},
	{system: systemdefs.SystemTVShow, name: "Show Name - S02E05 - The Episode Title.mkv"},
	{system: systemdefs.SystemTVShow, name: "show.name.2x05.episode.title.720p.mp4"},
	{system: systemdefs.SystemMusicTrack, name: "03 - Artist Name - Track Title (Live).flac"},
	{system: systemdefs.SystemVideo, name: "Holiday Video 2019-12-25 (Part 1).mp4"},
}

// scanRulesMediaType is the media type the scanner parses a system's files as.
func scanRulesMediaType(tb testing.TB, systemID string) slugs.MediaType {
	tb.Helper()
	system, err := systemdefs.GetSystem(systemID)
	require.NoError(tb, err)
	return system.GetMediaType()
}

// scanRulesGolden is the digest of the samples' parsed form under each
// scanRulesVersion. When the digest changes, the parsing rules changed:
// bump scanRulesVersion and add its digest here, so existing libraries are
// reconciled once under the new rules.
var scanRulesGolden = map[int]string{
	2: "db9e0652649f97b063874b2646c94e32b702b272b1935bb3433908bfebf05f83",
}

func scanRulesDigest(tb testing.TB) string {
	tb.Helper()
	digest := sha256.New()
	for i := range scanRulesSamples {
		sample := &scanRulesSamples[i]
		mediaType := scanRulesMediaType(tb, sample.system)
		pf := GetPathFragments(&PathFragmentParams{
			Path:         "/games/" + sample.system + "/Folder Name/" + sample.name,
			NoExt:        sample.noExt,
			SystemID:     sample.system,
			MediaType:    mediaType,
			ProvidedName: sample.providedName,
		})
		metadata := mediadb.GenerateSlugMetadataFromTokens(mediaType, pf.Title, pf.Slug, pf.SlugTokens)
		_, _ = fmt.Fprintf(digest, "%q %q %q %q %q %q %q %d %d %q\n",
			pf.FileName, pf.Title, pf.DisplayTitle, pf.Slug, pf.SlugTokens, pf.Ext,
			stagedTagsFromFragments(&pf, nil),
			metadata.SlugLength, metadata.SlugWordCount, metadata.SecondarySlug)
	}
	return hex.EncodeToString(digest.Sum(nil))
}

// A system is left alone when its files have not changed since they were
// indexed under the same scanRulesVersion. That is only sound if every change
// to what a file is stored as comes with a new version.
func TestScanRules_VersionCoversParsedOutput(t *testing.T) {
	t.Parallel()
	golden, ok := scanRulesGolden[scanRulesVersion]
	require.True(t, ok, "scanRulesGolden has no digest for scanRulesVersion %d", scanRulesVersion)
	require.Equal(t, golden, scanRulesDigest(t),
		"the parsed form of the sample files changed: bump scanRulesVersion in scan_skip.go "+
			"and record the new digest under it in scanRulesGolden")
}
