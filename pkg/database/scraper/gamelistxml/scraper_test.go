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
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- cleanField ---

func TestCleanField_TrimSpace(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "Nintendo", cleanField("  Nintendo  "))
}

func TestCleanField_TabsAndNewlines(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "Super Mario Bros", cleanField("Super\tMario\nBros"))
}

func TestCleanField_HTMLEntities(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "Banjo & Kazooie", cleanField("Banjo &amp; Kazooie"))
	assert.Equal(t, "A < B > C", cleanField("A &lt; B &gt; C"))
	assert.Equal(t, `say "hello"`, cleanField("say &quot;hello&quot;"))
}

func TestCleanField_Combined(t *testing.T) {
	t.Parallel()
	// Entity unescaped, surrounding whitespace trimmed, embedded \n replaced with space.
	assert.Equal(t, "Tom & Jerry", cleanField("  Tom\n&amp; Jerry  "))
	// \r\n each become their own space — no collapsing of runs.
	assert.Equal(t, "Tom &  Jerry", cleanField("  Tom\n&amp;\r\nJerry  "))
}

func TestCleanField_Empty(t *testing.T) {
	t.Parallel()
	assert.Empty(t, cleanField(""))
}

// --- resolveESPath ---

func TestResolveESPath_RelativeDotSlash(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	got := resolveESPath("./roms/mario.nes", root)
	assert.Equal(t, filepath.Join(root, "roms", "mario.nes"), got)
}

func TestResolveESPath_RelativeNoDot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	got := resolveESPath("roms/mario.nes", root)
	assert.Equal(t, filepath.Join(root, "roms", "mario.nes"), got)
}

func TestResolveESPath_Absolute(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	absPath := filepath.Join(root, "mario.nes")
	got := resolveESPath(absPath, filepath.Join(root, "other"))
	assert.Equal(t, absPath, got)
}

func TestResolveESPath_Empty(t *testing.T) {
	t.Parallel()
	got := resolveESPath("", t.TempDir())
	assert.Empty(t, got)
}

func TestResolveESPath_PathTraversal(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	got := resolveESPath("../../etc/passwd", root)
	// Relative paths that escape systemRootPath must be rejected.
	assert.Empty(t, got, "path traversal outside root must return empty string")
}

func TestResolveESPath_TraversalToAbsolute(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	got := resolveESPath("../../../etc/passwd", root)
	assert.Empty(t, got, "deep traversal outside root must return empty string")
}

// --- normalizePlayers ---

func TestNormalizePlayers_Single(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "1", normalizePlayers("1"))
}

func TestNormalizePlayers_Range(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "4", normalizePlayers("1-4"))
}

func TestNormalizePlayers_CSV(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "4", normalizePlayers("1, 2, 4"))
}

func TestNormalizePlayers_Mixed(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "4", normalizePlayers("2-4"))
}

func TestNormalizePlayers_Empty(t *testing.T) {
	t.Parallel()
	assert.Empty(t, normalizePlayers(""))
}

func TestNormalizePlayers_NonNumeric(t *testing.T) {
	t.Parallel()
	assert.Empty(t, normalizePlayers("co-op"))
}

// --- normalizeRating ---

func TestNormalizeRating_Standard(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "75", normalizeRating("0.75"))
}

func TestNormalizeRating_Zero(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "0", normalizeRating("0.0"))
}

func TestNormalizeRating_One(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "100", normalizeRating("1.0"))
}

func TestNormalizeRating_Rounding(t *testing.T) {
	t.Parallel()
	// 0.755 rounds to 76, not truncated to 75.
	assert.Equal(t, "76", normalizeRating("0.755"))
}

func TestNormalizeRating_Empty(t *testing.T) {
	t.Parallel()
	assert.Empty(t, normalizeRating(""))
}

func TestNormalizeRating_Invalid(t *testing.T) {
	t.Parallel()
	assert.Empty(t, normalizeRating("great"))
}

// --- extractYear ---

func TestExtractYear_ISODate(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "1985", extractYear("19851001T000000"))
}

func TestExtractYear_DashedDate(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "1985", extractYear("1985-10-01"))
}

func TestExtractYear_YearOnly(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "1985", extractYear("1985"))
}

func TestExtractYear_Short(t *testing.T) {
	t.Parallel()
	assert.Empty(t, extractYear("198"))
}

func TestExtractYear_NonNumericStart(t *testing.T) {
	t.Parallel()
	assert.Empty(t, extractYear("DATE"))
}

func TestExtractYear_Empty(t *testing.T) {
	t.Parallel()
	assert.Empty(t, extractYear(""))
}

// --- splitCSV ---

func TestSplitCSV_Multiple(t *testing.T) {
	t.Parallel()
	got := splitCSV("en, fr, de")
	assert.Equal(t, []string{"en", "fr", "de"}, got)
}

func TestSplitCSV_Single(t *testing.T) {
	t.Parallel()
	got := splitCSV("en")
	assert.Equal(t, []string{"en"}, got)
}

func TestSplitCSV_Empty(t *testing.T) {
	t.Parallel()
	got := splitCSV("")
	assert.Empty(t, got)
}

// --- mimeFromExt ---

func TestMimeFromExt_PNG(t *testing.T) { assert.Equal(t, "image/png", mimeFromExt("art.PNG")) }
func TestMimeFromExt_JPG(t *testing.T) { assert.Equal(t, "image/jpeg", mimeFromExt("art.jpg")) }
func TestMimeFromExt_MP4(t *testing.T) { assert.Equal(t, "video/mp4", mimeFromExt("clip.mp4")) }
func TestMimeFromExt_PDF(t *testing.T) { assert.Equal(t, "application/pdf", mimeFromExt("manual.pdf")) }
func TestMimeFromExt_Unknown(t *testing.T) {
	assert.Equal(t, "application/octet-stream", mimeFromExt("file.xyz"))
}

// --- MapToDB ---

func TestMapToDB_FullGame(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	rec := GamelistRecord{
		SystemRootPath: root,
		Game: esapi.Game{
			Path:        "./roms/mario.nes",
			Name:        "Super Mario Bros",
			Developer:   "Nintendo",
			Publisher:   "Nintendo",
			ReleaseDate: "19851001T000000",
			Rating:      "0.75",
			Genre:       "Platform",
			Players:     "1-4",
			Lang:        "en",
			Region:      "usa",
			Desc:        "A classic platformer.",
			Image:       "./media/images/mario.png",
			Video:       "./media/videos/mario.mp4",
		},
	}

	mediaTags, titleTags, titleProps, mediaProps := (&GamelistXMLScraper{}).MapToDB(rec)

	// Media-level tags
	assert.Contains(t, mediaTags, database.TagInfo{Type: string(tags.TagTypeLang), Tag: "en"})
	assert.Contains(t, mediaTags, database.TagInfo{Type: string(tags.TagTypeRegion), Tag: "usa"})
	// players is title-level (Fix 7): assert it is NOT in mediaTags.
	for _, tag := range mediaTags {
		assert.NotEqual(t, string(tags.TagTypePlayers), tag.Type, "players must not appear in mediaTags")
	}

	// Title-level tags
	assert.Contains(t, titleTags, database.TagInfo{Type: string(tags.TagTypeDeveloper), Tag: "Nintendo"})
	assert.Contains(t, titleTags, database.TagInfo{Type: string(tags.TagTypePublisher), Tag: "Nintendo"})
	assert.Contains(t, titleTags, database.TagInfo{Type: string(tags.TagTypeYear), Tag: "1985"})
	assert.Contains(t, titleTags, database.TagInfo{Type: string(tags.TagTypeRating), Tag: "75"})
	assert.Contains(t, titleTags, database.TagInfo{Type: string(tags.TagTypeGenre), Tag: "Platform"})
	// players must be title-level (Fix 7).
	assert.Contains(t, titleTags, database.TagInfo{Type: string(tags.TagTypePlayers), Tag: "4"})

	// Title-level properties
	descPropKey := string(tags.TagTypeProperty) + ":" + string(tags.TagPropertyDescription)
	imgPropKey := string(tags.TagTypeProperty) + ":" + string(tags.TagPropertyImageBoxart)
	videoPropKey := string(tags.TagTypeProperty) + ":" + string(tags.TagPropertyVideo)
	var foundDesc, foundImg, foundVideo bool
	for _, p := range titleProps {
		switch p.TypeTag {
		case descPropKey:
			foundDesc = true
			assert.Equal(t, "A classic platformer.", p.Text)
		case imgPropKey:
			foundImg = true
			assert.Equal(t, filepath.ToSlash(filepath.Join(root, "media", "images", "mario.png")), p.Text)
		case videoPropKey:
			foundVideo = true
			assert.Equal(t, filepath.ToSlash(filepath.Join(root, "media", "videos", "mario.mp4")), p.Text)
		}
	}
	assert.True(t, foundDesc, "description property missing")
	assert.True(t, foundImg, "boxart property missing")
	assert.True(t, foundVideo, "video property missing")

	assert.Empty(t, mediaProps, "gamelistxml scraper writes no media-level properties")
}

func TestMapToDB_MultiLang(t *testing.T) {
	t.Parallel()
	rec := GamelistRecord{
		Game: esapi.Game{Lang: "en, fr, de"},
	}
	mediaTags, _, _, _ := (&GamelistXMLScraper{}).MapToDB(rec)

	var langs []string
	for _, tag := range mediaTags {
		if tag.Type == string(tags.TagTypeLang) {
			langs = append(langs, tag.Tag)
		}
	}
	assert.ElementsMatch(t, []string{"en", "fr", "de"}, langs)
}

func TestMapToDB_MultiRegion(t *testing.T) {
	t.Parallel()
	rec := GamelistRecord{
		Game: esapi.Game{Region: "usa,eur"},
	}
	mediaTags, _, _, _ := (&GamelistXMLScraper{}).MapToDB(rec)

	var regions []string
	for _, tag := range mediaTags {
		if tag.Type == string(tags.TagTypeRegion) {
			regions = append(regions, tag.Tag)
		}
	}
	assert.ElementsMatch(t, []string{"usa", "eur"}, regions)
}

func TestMapToDB_EmptyGame_NoTags(t *testing.T) {
	t.Parallel()
	mediaTags, titleTags, titleProps, mediaProps := (&GamelistXMLScraper{}).MapToDB(GamelistRecord{})
	assert.Empty(t, mediaTags)
	assert.Empty(t, titleTags)
	assert.Empty(t, titleProps)
	assert.Empty(t, mediaProps)
}

func TestMapToDB_PathProp_SkipsUnresolvablePath(t *testing.T) {
	t.Parallel()
	// An empty image path should not produce a property.
	rec := GamelistRecord{
		SystemRootPath: "/media/nes",
		Game: esapi.Game{
			Image: "", // empty → skip
		},
	}
	_, _, titleProps, _ := (&GamelistXMLScraper{}).MapToDB(rec)
	for _, p := range titleProps {
		assert.NotEqual(t, string(tags.TagTypeProperty)+":"+string(tags.TagPropertyImageBoxart), p.TypeTag,
			"empty image path should not produce a boxart property")
	}
}

func TestMapToDB_ArcadeBoard(t *testing.T) {
	t.Parallel()
	rec := GamelistRecord{
		Game: esapi.Game{ArcadeSystemName: "CPS2"},
	}
	_, titleTags, _, _ := (&GamelistXMLScraper{}).MapToDB(rec)
	require.NotEmpty(t, titleTags)
	assert.Contains(t, titleTags, database.TagInfo{Type: string(tags.TagTypeArcadeBoard), Tag: "CPS2"})
}

// TestPathProp_NormalizesSlashes verifies that pathProp returns forward-slash
// paths regardless of the OS separator. The MediaDB stores paths with
// filepath.ToSlash (see indexing_pipeline.go), so artwork paths must match.
func TestPathProp_NormalizesSlashes(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	p := pathProp("prop:image", "./images/mario.png", root)
	if p == nil {
		t.Fatal("expected non-nil property")
	}
	if strings.Contains(p.Text, "\\") {
		t.Errorf("pathProp returned backslashes in path: %q", p.Text)
	}
}

func TestMapToDB_ContentType_Image(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	rec := GamelistRecord{
		SystemRootPath: root,
		Game:           esapi.Game{Image: "./images/mario.png"},
	}
	_, _, titleProps, _ := (&GamelistXMLScraper{}).MapToDB(rec)
	propKey := string(tags.TagTypeProperty) + ":" + string(tags.TagPropertyImageBoxart)
	for _, p := range titleProps {
		if p.TypeTag == propKey {
			assert.Equal(t, "image/png", p.ContentType)
			return
		}
	}
	t.Error("boxart property not found")
}
