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
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediascanner"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esapi"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// slugFor computes the MediaTitle slug that would be stored for a ROM path
// using the same parameters as the scraper's LoadRecords call.
func slugFor(systemID, path string) string {
	return mediascanner.GetPathFragments(mediascanner.PathFragmentParams{
		SystemID: systemID,
		Path:     path,
		NoExt:    true,
	}).Slug
}

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

func TestResolveESPath_AbsoluteInsideRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	absPath := filepath.Join(root, "roms", "mario.nes")
	got := resolveESPath(absPath, root)
	assert.Equal(t, absPath, got)
}

func TestResolveESPath_AbsoluteOutsideRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	absPath := filepath.Join(root, "mario.nes")
	got := resolveESPath(absPath, filepath.Join(root, "other"))
	assert.Empty(t, got)
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

// --- LoadRecords ---

// TestLoadRecords_SlugMatch verifies that a gamelist entry whose slug matches a
// key in titlesBySlug produces a GamelistRecord with the correct DB IDs.
func TestLoadRecords_SlugMatch(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "media", "image"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`
<gameList>
  <game><path>./mario.nes</path><name>Mario</name></game>
  <game><path>./zelda.nes</path><name>Zelda</name></game>
</gameList>`), 0o600))

	marioSlug := slugFor("nes", filepath.Join(root, "mario.nes"))

	titlesBySlug := map[string]database.MediaTitle{
		marioSlug: {DBID: 22, Slug: marioSlug},
	}
	mediaByTitleDBID := map[int64]int64{22: 11}

	records, err := (&GamelistXMLScraper{}).LoadRecords(
		context.Background(),
		scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}},
		titlesBySlug,
		mediaByTitleDBID,
	)
	require.NoError(t, err)
	// Only mario has a matching MediaTitle slug; zelda is silently skipped.
	require.Len(t, records, 1)
	assert.Equal(t, root, records[0].SystemRootPath)
	assert.Equal(t, "./mario.nes", records[0].Game.Path)
	assert.Equal(t, "Mario", records[0].Game.Name)
	assert.Equal(t, int64(11), records[0].MatchedMediaDBID)
	assert.Equal(t, int64(22), records[0].MatchedTitleDBID)
	assert.Equal(t, filepath.Join(root, "media", "image"), records[0].AvailableMediaDirs["image"])
}

// TestLoadRecords_SkipsMissingAndMalformedGameLists verifies that ROM roots
// without a gamelist.xml and roots with a malformed file are silently skipped.
func TestLoadRecords_SkipsMissingAndMalformedGameLists(t *testing.T) {
	t.Parallel()

	missingRoot := filepath.Join(t.TempDir(), "missing")
	require.NoError(t, os.MkdirAll(missingRoot, 0o750))

	malformedRoot := filepath.Join(t.TempDir(), "malformed")
	require.NoError(t, os.MkdirAll(malformedRoot, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(malformedRoot, "gamelist.xml"), []byte(`<gameList><game>`), 0o600))

	validRoot := filepath.Join(t.TempDir(), "valid")
	require.NoError(t, os.MkdirAll(validRoot, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(validRoot, "gamelist.xml"), []byte(`
<gameList>
  <game><path>./mario.nes</path><name>Mario</name></game>
</gameList>`), 0o600))

	marioSlug := slugFor("nes", filepath.Join(validRoot, "mario.nes"))
	titlesBySlug := map[string]database.MediaTitle{
		marioSlug: {DBID: 5, Slug: marioSlug},
	}
	mediaByTitleDBID := map[int64]int64{5: 3}

	records, err := (&GamelistXMLScraper{}).LoadRecords(
		context.Background(),
		scraper.ScrapeSystem{
			ID:       "nes",
			ROMPaths: []string{missingRoot, malformedRoot, validRoot},
		},
		titlesBySlug,
		mediaByTitleDBID,
	)
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, int64(3), records[0].MatchedMediaDBID)
	assert.Equal(t, int64(5), records[0].MatchedTitleDBID)
}

// TestLoadRecords_FirstWins verifies that when two gamelist roots contain an
// entry with the same slug, only the first is recorded.
func TestLoadRecords_FirstWins(t *testing.T) {
	t.Parallel()

	root1 := t.TempDir()
	root2 := t.TempDir()

	writeGamelist := func(dir, name string) {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "gamelist.xml"), []byte(`
<gameList>
  <game><path>./game.nes</path><name>`+name+`</name></game>
</gameList>`), 0o600))
	}
	writeGamelist(root1, "Game A")
	writeGamelist(root2, "Game B")

	slug := slugFor("nes", filepath.Join(root1, "game.nes"))
	titlesBySlug := map[string]database.MediaTitle{
		slug: {DBID: 1, Slug: slug},
	}
	mediaByTitleDBID := map[int64]int64{1: 10}

	records, err := (&GamelistXMLScraper{}).LoadRecords(
		context.Background(),
		scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root1, root2}},
		titlesBySlug,
		mediaByTitleDBID,
	)
	require.NoError(t, err)
	require.Len(t, records, 1, "second root's duplicate slug must be skipped")
	assert.Equal(t, "Game A", records[0].Game.Name, "first root wins")
}

// TestLoadRecords_NoMediaForTitle verifies that a slug match with no
// corresponding Media row yields MatchedMediaDBID = 0. The scrape loop will
// skip such records via its zero-ID guard.
func TestLoadRecords_NoMediaForTitle(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`
<gameList>
  <game><path>./mario.nes</path><name>Mario</name></game>
</gameList>`), 0o600))

	slug := slugFor("nes", filepath.Join(root, "mario.nes"))
	titlesBySlug := map[string]database.MediaTitle{
		slug: {DBID: 7, Slug: slug},
	}
	// Intentionally omit title DBID 7 from mediaByTitleDBID.
	mediaByTitleDBID := map[int64]int64{}

	records, err := (&GamelistXMLScraper{}).LoadRecords(
		context.Background(),
		scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}},
		titlesBySlug,
		mediaByTitleDBID,
	)
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, int64(7), records[0].MatchedTitleDBID)
	assert.Equal(t, int64(0), records[0].MatchedMediaDBID)
}

// TestLoadRecords_ContextCancellation verifies that a cancelled context causes
// LoadRecords to return context.Canceled immediately.
func TestLoadRecords_ContextCancellation(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`
<gameList>
  <game><path>./mario.nes</path><name>Mario</name></game>
</gameList>`), 0o600))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := (&GamelistXMLScraper{}).LoadRecords(
		ctx,
		scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}},
		map[string]database.MediaTitle{},
		map[int64]int64{},
	)
	require.ErrorIs(t, err, context.Canceled)
}

// TestLoadRecords_PathTraversalSkipped verifies that gamelist entries whose
// paths escape the system root are silently skipped.
func TestLoadRecords_PathTraversalSkipped(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`
<gameList>
  <game><path>../../etc/passwd</path><name>Traversal</name></game>
  <game><path>./mario.nes</path><name>Mario</name></game>
</gameList>`), 0o600))

	slug := slugFor("nes", filepath.Join(root, "mario.nes"))
	titlesBySlug := map[string]database.MediaTitle{
		slug: {DBID: 1, Slug: slug},
	}

	records, err := (&GamelistXMLScraper{}).LoadRecords(
		context.Background(),
		scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}},
		titlesBySlug,
		map[int64]int64{1: 9},
	)
	require.NoError(t, err)
	require.Len(t, records, 1, "traversal entry must be dropped; mario must still match")
	assert.Equal(t, "Mario", records[0].Game.Name)
}

func TestScrape_DBError(t *testing.T) {
	t.Parallel()

	dbErr := assert.AnError
	mockDB := helpers.NewMockMediaDBI()
	mockDB.On("IndexedSystems").Return([]string(nil), dbErr)

	ps := NewPlatformScraper()
	ch := make(chan scraper.ScrapeUpdate, 32)
	err := ps.Scrape(
		context.Background(),
		nil, // cfg — not reached before early return
		nil, // platform — not reached before early return
		afero.NewMemMapFs(),
		&database.Database{MediaDB: mockDB},
		scraper.ScrapeOptions{Systems: []string{"nes"}},
		nil,
		ch,
	)
	require.Error(t, err)
	require.ErrorIs(t, err, dbErr)
	mockDB.AssertExpectations(t)
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

	result := (&GamelistXMLScraper{}).MapToDB(&rec)

	// Media-level tags: not written by this scraper.
	assert.Empty(t, result.MediaTags, "gamelistxml scraper writes no media-level tags")

	// Title-level tags
	assert.Contains(t, result.TitleTags, database.TagInfo{Type: string(tags.TagTypeDeveloper), Tag: "Nintendo"})
	assert.Contains(t, result.TitleTags, database.TagInfo{Type: string(tags.TagTypePublisher), Tag: "Nintendo"})
	assert.Contains(t, result.TitleTags, database.TagInfo{Type: string(tags.TagTypeYear), Tag: "1985"})
	assert.Contains(t, result.TitleTags, database.TagInfo{Type: string(tags.TagTypeRating), Tag: "75"})
	assert.Contains(t, result.TitleTags, database.TagInfo{Type: string(tags.TagTypeGenre), Tag: "Platform"})
	assert.Contains(t, result.TitleTags, database.TagInfo{Type: string(tags.TagTypePlayers), Tag: "4"})

	// Title-level properties
	descPropKey := string(tags.TagTypeProperty) + ":" + string(tags.TagPropertyDescription)
	imgPropKey := string(tags.TagTypeProperty) + ":" + string(tags.TagPropertyImageImage)
	videoPropKey := string(tags.TagTypeProperty) + ":" + string(tags.TagPropertyVideo)
	var foundDesc, foundImg, foundVideo bool
	for _, p := range result.TitleProps {
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
	assert.True(t, foundImg, "image property missing")
	assert.True(t, foundVideo, "video property missing")

	assert.Empty(t, result.MediaProps, "gamelistxml scraper writes no media-level properties")
}

func TestMapToDB_EmptyGame_NoTags(t *testing.T) {
	t.Parallel()
	result := (&GamelistXMLScraper{}).MapToDB(&GamelistRecord{})
	assert.Empty(t, result.MediaTags)
	assert.Empty(t, result.TitleTags)
	assert.Empty(t, result.TitleProps)
	assert.Empty(t, result.MediaProps)
}

func TestMapToDB_PathProp_SkipsUnresolvablePath(t *testing.T) {
	t.Parallel()
	// An empty image path should not produce a property regardless of the root.
	rec := GamelistRecord{
		SystemRootPath: t.TempDir(),
		Game: esapi.Game{
			Image: "", // empty → skip
		},
	}
	titleProps := (&GamelistXMLScraper{}).MapToDB(&rec).TitleProps
	for _, p := range titleProps {
		assert.NotEqual(t, string(tags.TagTypeProperty)+":"+string(tags.TagPropertyImageImage), p.TypeTag,
			"empty image path should not produce an image property")
	}
}

func TestMapToDB_ArcadeBoard(t *testing.T) {
	t.Parallel()
	rec := GamelistRecord{
		Game: esapi.Game{ArcadeSystemName: "CPS2"},
	}
	titleTags := (&GamelistXMLScraper{}).MapToDB(&rec).TitleTags
	require.NotEmpty(t, titleTags)
	assert.Contains(t, titleTags, database.TagInfo{Type: string(tags.TagTypeArcadeBoard), Tag: "CPS2"})
}

// --- MapToDB ScreenScraper ID ---

func TestMapToDB_ScreenScraperIDAttr(t *testing.T) {
	t.Parallel()
	rec := GamelistRecord{
		Game: esapi.Game{ScreenScraperIDAttr: "12345"},
	}
	titleProps := (&GamelistXMLScraper{}).MapToDB(&rec).TitleProps
	propKey := string(tags.TagTypeProperty) + ":" + string(tags.TagPropertyXMLGameID)
	var found bool
	for _, p := range titleProps {
		if p.TypeTag == propKey {
			found = true
			assert.Equal(t, "12345", p.Text)
			assert.Equal(t, "text/plain", p.ContentType)
		}
	}
	assert.True(t, found, "xml-game-id property missing when ScreenScraperIDAttr is set")
}

func TestMapToDB_ScreenScraperIDAttr_ZeroSkipsToElement(t *testing.T) {
	t.Parallel()
	// Attr "0" is treated as absent; element form should be used instead.
	rec := GamelistRecord{
		Game: esapi.Game{ScreenScraperIDAttr: "0", ScreenScraperID: 99},
	}
	titleProps := (&GamelistXMLScraper{}).MapToDB(&rec).TitleProps
	propKey := string(tags.TagTypeProperty) + ":" + string(tags.TagPropertyXMLGameID)
	var found bool
	for _, p := range titleProps {
		if p.TypeTag == propKey {
			found = true
			assert.Equal(t, "99", p.Text)
		}
	}
	assert.True(t, found, "xml-game-id property missing when ScreenScraperIDAttr is \"0\" and element form is set")
}

func TestMapToDB_ScreenScraperIDElement(t *testing.T) {
	t.Parallel()
	// No attr form; element form should be used.
	rec := GamelistRecord{
		Game: esapi.Game{ScreenScraperID: 42},
	}
	titleProps := (&GamelistXMLScraper{}).MapToDB(&rec).TitleProps
	propKey := string(tags.TagTypeProperty) + ":" + string(tags.TagPropertyXMLGameID)
	var found bool
	for _, p := range titleProps {
		if p.TypeTag == propKey {
			found = true
			assert.Equal(t, "42", p.Text)
		}
	}
	assert.True(t, found, "xml-game-id property missing when only element form is set")
}

func TestMapToDB_ScreenScraperID_NeitherSet(t *testing.T) {
	t.Parallel()
	// Both attr and element are zero/empty — no prop should be emitted.
	rec := GamelistRecord{
		Game: esapi.Game{ScreenScraperIDAttr: "", ScreenScraperID: 0},
	}
	titleProps := (&GamelistXMLScraper{}).MapToDB(&rec).TitleProps
	propKey := string(tags.TagTypeProperty) + ":" + string(tags.TagPropertyXMLGameID)
	for _, p := range titleProps {
		assert.NotEqual(t, propKey, p.TypeTag, "xml-game-id should not be emitted when both ID fields are absent")
	}
}

// TestPathProp_NormalizesSlashes verifies that pathProp returns forward-slash
// paths regardless of the OS separator. The MediaDB stores paths with
// filepath.ToSlash (see indexing_pipeline.go), so artwork paths must match.
func TestPathProp_NormalizesSlashes(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	p := pathProp("prop:image", "./images/mario.png", root)
	require.NotNil(t, p, "expected non-nil property")
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
	titleProps := (&GamelistXMLScraper{}).MapToDB(&rec).TitleProps
	propKey := string(tags.TagTypeProperty) + ":" + string(tags.TagPropertyImageImage)
	for _, p := range titleProps {
		if p.TypeTag == propKey {
			assert.Equal(t, "image/png", p.ContentType)
			return
		}
	}
	t.Error("image property not found")
}

// --- statMediaDirs ---

func TestStatMediaDirs_ReturnsAvailableDirs(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(root, "media", "image"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "media", "boxart"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(root, "media", "notadir.txt"), []byte{}, 0o600))

	got := statMediaDirs(root)

	assert.Contains(t, got, "image")
	assert.Contains(t, got, "boxart")
	assert.NotContains(t, got, "notadir.txt")
	assert.Equal(t, filepath.Join(root, "media", "image"), got["image"])
}

func TestStatMediaDirs_NoMediaDir(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	got := statMediaDirs(root)
	assert.Empty(t, got)
}

// --- findMediaFileProp ---

func TestFindMediaFileProp_FoundInFirstCandidate(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	imgDir := filepath.Join(root, "media", "image")
	require.NoError(t, os.MkdirAll(imgDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(imgDir, "mario.png"), []byte{}, 0o600))

	availableDirs := map[string]string{"image": imgDir}
	p := findMediaFileProp("prop:image-image", "mario", []string{"image", "images"}, availableDirs)

	require.NotNil(t, p)
	assert.Equal(t, filepath.ToSlash(filepath.Join(imgDir, "mario.png")), p.Text)
	assert.Equal(t, "image/png", p.ContentType)
}

func TestFindMediaFileProp_FoundInSecondCandidate(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	imagesDir := filepath.Join(root, "media", "images")
	require.NoError(t, os.MkdirAll(imagesDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(imagesDir, "mario.png"), []byte{}, 0o600))

	// "image" is not present; "images" contains the file.
	availableDirs := map[string]string{"images": imagesDir}
	p := findMediaFileProp("prop:image-image", "mario", []string{"image", "images"}, availableDirs)

	require.NotNil(t, p)
	assert.Equal(t, filepath.ToSlash(filepath.Join(imagesDir, "mario.png")), p.Text)
}

func TestFindMediaFileProp_NotFound(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	imgDir := filepath.Join(root, "media", "image")
	require.NoError(t, os.MkdirAll(imgDir, 0o750))
	// No mario.png created.

	availableDirs := map[string]string{"image": imgDir}
	p := findMediaFileProp("prop:image-image", "mario", []string{"image", "images"}, availableDirs)
	assert.Nil(t, p)
}

func TestFindMediaFileProp_NilAvailableDirs(t *testing.T) {
	t.Parallel()
	p := findMediaFileProp("prop:image-image", "mario", []string{"image", "images"}, nil)
	assert.Nil(t, p)
}

func TestFindMediaFileProp_EmptyStem(t *testing.T) {
	t.Parallel()
	p := findMediaFileProp("prop:image-image", "", []string{"image"}, map[string]string{"image": t.TempDir()})
	assert.Nil(t, p)
}

// --- MapToDB filesystem fallback ---

func TestMapToDB_FilesystemFallback_Image(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	imgDir := filepath.Join(root, "media", "image")
	require.NoError(t, os.MkdirAll(imgDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(imgDir, "mario.png"), []byte{}, 0o600))

	rec := GamelistRecord{
		SystemRootPath:     root,
		AvailableMediaDirs: map[string]string{"image": imgDir},
		Game: esapi.Game{
			Path:  "./roms/mario.nes",
			Image: "", // no XML path → filesystem fallback
		},
	}

	result := (&GamelistXMLScraper{}).MapToDB(&rec)

	propKey := string(tags.TagTypeProperty) + ":" + string(tags.TagPropertyImageImage)
	var found bool
	for _, p := range result.TitleProps {
		if p.TypeTag == propKey {
			found = true
			assert.Equal(t, filepath.ToSlash(filepath.Join(imgDir, "mario.png")), p.Text)
			assert.Equal(t, "image/png", p.ContentType)
		}
	}
	assert.True(t, found, "filesystem fallback image property missing")
}

func TestMapToDB_FilesystemFallback_Boxart(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	boxartDir := filepath.Join(root, "media", "boxart")
	require.NoError(t, os.MkdirAll(boxartDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(boxartDir, "sonic.png"), []byte{}, 0o600))

	rec := GamelistRecord{
		SystemRootPath:     root,
		AvailableMediaDirs: map[string]string{"boxart": boxartDir},
		Game:               esapi.Game{Path: "./roms/sonic.md"},
	}

	result := (&GamelistXMLScraper{}).MapToDB(&rec)

	propKey := string(tags.TagTypeProperty) + ":" + string(tags.TagPropertyImageBoxart)
	var found bool
	for _, p := range result.TitleProps {
		if p.TypeTag == propKey {
			found = true
			assert.Equal(t, filepath.ToSlash(filepath.Join(boxartDir, "sonic.png")), p.Text)
		}
	}
	assert.True(t, found, "filesystem fallback boxart property missing")
}

func TestMapToDB_FilesystemFallback_XMLWins(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	// XML-referenced file.
	xmlImg := filepath.Join(root, "media", "images", "mario.png")
	require.NoError(t, os.MkdirAll(filepath.Dir(xmlImg), 0o750))
	require.NoError(t, os.WriteFile(xmlImg, []byte{}, 0o600))

	// Filesystem fallback file in a different dir.
	fsImg := filepath.Join(root, "media", "image", "mario.png")
	require.NoError(t, os.MkdirAll(filepath.Dir(fsImg), 0o750))
	require.NoError(t, os.WriteFile(fsImg, []byte{}, 0o600))

	rec := GamelistRecord{
		SystemRootPath:     root,
		AvailableMediaDirs: map[string]string{"image": filepath.Join(root, "media", "image")},
		Game: esapi.Game{
			Path:  "./roms/mario.nes",
			Image: "./media/images/mario.png", // XML path present → must win
		},
	}

	result := (&GamelistXMLScraper{}).MapToDB(&rec)

	propKey := string(tags.TagTypeProperty) + ":" + string(tags.TagPropertyImageImage)
	var count int
	for _, p := range result.TitleProps {
		if p.TypeTag == propKey {
			count++
			assert.Equal(t, filepath.ToSlash(xmlImg), p.Text, "XML path should take priority over filesystem fallback")
		}
	}
	assert.Equal(t, 1, count, "exactly one image property should be present")
}

func TestMapToDB_FilesystemFallback_NoMediaDir(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	// No media/ directory, no AvailableMediaDirs — should produce no image props.
	rec := GamelistRecord{
		SystemRootPath: root,
		Game:           esapi.Game{Path: "./roms/mario.nes"},
	}
	result := (&GamelistXMLScraper{}).MapToDB(&rec)
	for _, p := range result.TitleProps {
		assert.NotContains(t, p.TypeTag, "image-", "no image props expected when no media dir and no XML paths")
	}
}
