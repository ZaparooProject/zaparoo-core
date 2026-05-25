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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esapi"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// slugFor computes the MediaTitle slug that would be stored for a ROM path
// using the same parameters as the scraper's LoadRecords call.
func slugFor(systemID, path string) string {
	return mediascanner.GetPathFragments(&mediascanner.PathFragmentParams{
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

// TestLoadRecords_NameDerivedSlugMatch verifies the scraper matches titles
// whose stored slug derives from a scanner-provided display name rather than
// the filename. This is a regression test for the fix where the indexer uses
// ScanResult.Name (e.g. NeoGeo AltName, gamelist.xml <name>) to build the
// MediaTitle slug, requiring the scraper to also derive its lookup slug from
// gamelist.xml's <name> rather than the filename.
func TestLoadRecords_NameDerivedSlugMatch(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`
<gameList>
  <game><path>./mslug.zip</path><name>Metal Slug</name></game>
</gameList>`), 0o600))

	// Indexer would have stored the slug derived from ProvidedName="Metal Slug",
	// not from the filename "mslug". Build titlesBySlug accordingly.
	nameSlug := mediascanner.GetPathFragments(&mediascanner.PathFragmentParams{
		SystemID:     "NeoGeo",
		Path:         filepath.Join(root, "mslug.zip"),
		NoExt:        true,
		ProvidedName: "Metal Slug",
	}).Slug

	// Confirm the test setup actually exercises the regression: the
	// name-derived slug must differ from a filename-derived one.
	filenameSlug := slugFor("NeoGeo", filepath.Join(root, "mslug.zip"))
	require.NotEqual(t, filenameSlug, nameSlug,
		"test precondition: name-derived slug must differ from filename-derived")

	titlesBySlug := map[string]database.MediaTitle{
		nameSlug: {DBID: 42, Slug: nameSlug, Name: "Metal Slug"},
	}
	mediaByTitleDBID := map[int64]int64{42: 7}

	records, err := (&GamelistXMLScraper{}).LoadRecords(
		context.Background(),
		scraper.ScrapeSystem{ID: "NeoGeo", ROMPaths: []string{root}},
		titlesBySlug,
		mediaByTitleDBID,
	)
	require.NoError(t, err)
	require.Len(t, records, 1, "scraper must match the title via name-derived slug")
	assert.Equal(t, "Metal Slug", records[0].Game.Name)
	assert.Equal(t, int64(42), records[0].MatchedTitleDBID)
	assert.Equal(t, int64(7), records[0].MatchedMediaDBID)
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

	writeGamelist := func(dir string) {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "gamelist.xml"), []byte(`
<gameList>
  <game><path>./game.nes</path><name>Game</name></game>
</gameList>`), 0o600))
	}
	writeGamelist(root1)
	writeGamelist(root2)

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
	assert.Equal(t, root1, records[0].SystemRootPath, "first root wins")
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

func TestMapToDB_Boxart3D_XMLPath(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	img := filepath.Join(root, "media", "boxart3d", "sonic.png")
	require.NoError(t, os.MkdirAll(filepath.Dir(img), 0o750))
	require.NoError(t, os.WriteFile(img, []byte{}, 0o600))

	rec := GamelistRecord{
		SystemRootPath: root,
		Game: esapi.Game{
			Path:     "./roms/sonic.md",
			Boxart3D: "./media/boxart3d/sonic.png",
		},
	}

	result := (&GamelistXMLScraper{}).MapToDB(&rec)

	propKey := string(tags.TagTypeProperty) + ":" + string(tags.TagPropertyImageBoxart3D)
	var found bool
	for _, p := range result.TitleProps {
		if p.TypeTag == propKey {
			found = true
			assert.Equal(t, filepath.ToSlash(img), p.Text)
		}
	}
	assert.True(t, found, "boxart3d XML path property missing")
}

func TestMapToDB_Boxart2D_And_Boxart3D_AreIndependent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	img2d := filepath.Join(root, "media", "boxart2d", "sonic.png")
	img3d := filepath.Join(root, "media", "boxart3d", "sonic.png")
	require.NoError(t, os.MkdirAll(filepath.Dir(img2d), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Dir(img3d), 0o750))
	require.NoError(t, os.WriteFile(img2d, []byte{}, 0o600))
	require.NoError(t, os.WriteFile(img3d, []byte{}, 0o600))

	rec := GamelistRecord{
		SystemRootPath: root,
		Game: esapi.Game{
			Path:     "./roms/sonic.md",
			Boxart2D: "./media/boxart2d/sonic.png",
			Boxart3D: "./media/boxart3d/sonic.png",
		},
	}

	result := (&GamelistXMLScraper{}).MapToDB(&rec)

	key2d := string(tags.TagTypeProperty) + ":" + string(tags.TagPropertyImageBoxart)
	key3d := string(tags.TagTypeProperty) + ":" + string(tags.TagPropertyImageBoxart3D)
	found2d, found3d := false, false
	for _, p := range result.TitleProps {
		switch p.TypeTag {
		case key2d:
			found2d = true
			assert.Equal(t, filepath.ToSlash(img2d), p.Text)
		case key3d:
			found3d = true
			assert.Equal(t, filepath.ToSlash(img3d), p.Text)
		}
	}
	assert.True(t, found2d, "boxart (2D) property missing")
	assert.True(t, found3d, "boxart3d property missing")
}

func TestMapToDB_FilesystemFallback_Boxart3D(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	dir := filepath.Join(root, "media", "boxart3d")
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sonic.png"), []byte{}, 0o600))

	rec := GamelistRecord{
		SystemRootPath:     root,
		AvailableMediaDirs: map[string]string{"boxart3d": dir},
		Game:               esapi.Game{Path: "./roms/sonic.md"},
	}

	result := (&GamelistXMLScraper{}).MapToDB(&rec)

	propKey := string(tags.TagTypeProperty) + ":" + string(tags.TagPropertyImageBoxart3D)
	var found bool
	for _, p := range result.TitleProps {
		if p.TypeTag == propKey {
			found = true
			assert.Equal(t, filepath.ToSlash(filepath.Join(dir, "sonic.png")), p.Text)
		}
	}
	assert.True(t, found, "filesystem fallback boxart3d property missing")
}

func TestMapToDB_FilesystemFallback_BoxartSide(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	dir := filepath.Join(root, "media", "boxart2dside")
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sonic.png"), []byte{}, 0o600))

	rec := GamelistRecord{
		SystemRootPath:     root,
		AvailableMediaDirs: map[string]string{"boxart2dside": dir},
		Game:               esapi.Game{Path: "./roms/sonic.md"},
	}

	result := (&GamelistXMLScraper{}).MapToDB(&rec)

	propKey := string(tags.TagTypeProperty) + ":" + string(tags.TagPropertyImageBoxartSide)
	var found bool
	for _, p := range result.TitleProps {
		if p.TypeTag == propKey {
			found = true
			assert.Equal(t, filepath.ToSlash(filepath.Join(dir, "sonic.png")), p.Text)
		}
	}
	assert.True(t, found, "filesystem fallback boxartside property missing")
}

func TestMapToDB_FilesystemFallback_BoxartBack(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	dir := filepath.Join(root, "media", "boxart2dback")
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sonic.png"), []byte{}, 0o600))

	rec := GamelistRecord{
		SystemRootPath:     root,
		AvailableMediaDirs: map[string]string{"boxart2dback": dir},
		Game:               esapi.Game{Path: "./roms/sonic.md"},
	}

	result := (&GamelistXMLScraper{}).MapToDB(&rec)

	propKey := string(tags.TagTypeProperty) + ":" + string(tags.TagPropertyImageBoxartBack)
	var found bool
	for _, p := range result.TitleProps {
		if p.TypeTag == propKey {
			found = true
			assert.Equal(t, filepath.ToSlash(filepath.Join(dir, "sonic.png")), p.Text)
		}
	}
	assert.True(t, found, "filesystem fallback boxartback property missing")
}

// --- resolveESPath additional ---

func TestResolveESPath_HomeRelativeEscapesRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	// ~/... resolves to home dir, which is outside t.TempDir().
	got := resolveESPath("~/games/mario.nes", root)
	assert.Empty(t, got, "home-relative path escaping system root must be rejected")
}

func TestResolveESPath_HomeRelativeInsideRoot(t *testing.T) {
	t.Parallel()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir")
	}
	// Use home dir itself as the system root so ~/relative stays inside.
	got := resolveESPath("~/games/mario.nes", home)
	assert.Equal(t, filepath.Join(home, "games", "mario.nes"), got)
}

// --- resolveESAssetPath ---

func TestResolveESAssetPath_InsideRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	got := resolveESAssetPath("./images/art.png", root)
	assert.Equal(t, filepath.Join(root, "images", "art.png"), got)
}

func TestResolveESAssetPath_OutsideRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	got := resolveESAssetPath("../../etc/passwd", root)
	assert.Empty(t, got)
}

func TestResolveESAssetPath_EmptyPath(t *testing.T) {
	t.Parallel()
	got := resolveESAssetPath("", t.TempDir())
	assert.Empty(t, got)
}

// --- mimeFromExt additional ---

func TestMimeFromExt_JPEG(t *testing.T) { assert.Equal(t, "image/jpeg", mimeFromExt("photo.jpeg")) }
func TestMimeFromExt_GIF(t *testing.T)  { assert.Equal(t, "image/gif", mimeFromExt("anim.gif")) }
func TestMimeFromExt_WEBP(t *testing.T) { assert.Equal(t, "image/webp", mimeFromExt("img.webp")) }
func TestMimeFromExt_MKV(t *testing.T)  { assert.Equal(t, "video/x-matroska", mimeFromExt("vid.mkv")) }
func TestMimeFromExt_AVI(t *testing.T)  { assert.Equal(t, "video/avi", mimeFromExt("vid.avi")) }
func TestMimeFromExt_MP3(t *testing.T)  { assert.Equal(t, "audio/mpeg", mimeFromExt("track.mp3")) }
func TestMimeFromExt_M4A(t *testing.T)  { assert.Equal(t, "audio/mp4", mimeFromExt("track.m4a")) }
func TestMimeFromExt_M4B(t *testing.T)  { assert.Equal(t, "audio/mp4", mimeFromExt("book.m4b")) }
func TestMimeFromExt_MPG(t *testing.T)  { assert.Equal(t, "video/mpeg", mimeFromExt("vid.mpg")) }
func TestMimeFromExt_MPEG(t *testing.T) { assert.Equal(t, "video/mpeg", mimeFromExt("vid.mpeg")) }
func TestMimeFromExt_M4V(t *testing.T)  { assert.Equal(t, "video/mp4", mimeFromExt("vid.m4v")) }

// --- MapToDB additional ---

func TestMapToDB_GameFamily(t *testing.T) {
	t.Parallel()
	rec := GamelistRecord{Game: esapi.Game{Family: "Mario"}}
	titleTags := (&GamelistXMLScraper{}).MapToDB(&rec).TitleTags
	assert.Contains(t, titleTags, database.TagInfo{Type: string(tags.TagTypeGameFamily), Tag: "Mario"})
}

func TestMapToDB_Manual(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	rec := GamelistRecord{
		SystemRootPath: root,
		Game:           esapi.Game{Manual: "./manuals/game.pdf"},
	}
	titleProps := (&GamelistXMLScraper{}).MapToDB(&rec).TitleProps
	propKey := string(tags.TagTypeProperty) + ":" + string(tags.TagPropertyManual)
	var found bool
	for _, p := range titleProps {
		if p.TypeTag == propKey {
			found = true
			assert.Equal(t, "application/pdf", p.ContentType)
		}
	}
	assert.True(t, found, "manual property missing")
}

func TestMapToDB_WheelXMLLogoTakesPriority(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	rec := GamelistRecord{
		SystemRootPath: root,
		Game: esapi.Game{
			Path:  "./roms/game.rom",
			Logo:  "./media/logo_source/game.png",
			Wheel: "./media/wheel_source/game.png",
		},
	}
	propKey := string(tags.TagTypeProperty) + ":" + string(tags.TagPropertyImageWheel)
	var found bool
	for _, p := range (&GamelistXMLScraper{}).MapToDB(&rec).TitleProps {
		if p.TypeTag == propKey {
			found = true
			assert.Contains(t, p.Text, "logo_source", "Logo field should take priority over Wheel")
			assert.NotContains(t, p.Text, "wheel_source")
		}
	}
	assert.True(t, found, "wheel property missing")
}

func TestMapToDB_WheelXMLFromWheelWhenNoLogo(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	rec := GamelistRecord{
		SystemRootPath: root,
		Game: esapi.Game{
			Path:  "./roms/game.rom",
			Wheel: "./media/wheel_source/game.png",
		},
	}
	propKey := string(tags.TagTypeProperty) + ":" + string(tags.TagPropertyImageWheel)
	var found bool
	for _, p := range (&GamelistXMLScraper{}).MapToDB(&rec).TitleProps {
		if p.TypeTag == propKey {
			found = true
			assert.Contains(t, p.Text, "wheel_source")
		}
	}
	assert.True(t, found, "wheel property from Wheel field missing when Logo is empty")
}

func TestMapToDB_TitleShotXMLFromTitleScreen(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	rec := GamelistRecord{
		SystemRootPath: root,
		Game: esapi.Game{
			Path:        "./roms/game.rom",
			TitleScreen: "./media/titlescreen_source/game.png",
			TitleShot:   "./media/titleshot_source/game.png",
		},
	}
	propKey := string(tags.TagTypeProperty) + ":" + string(tags.TagPropertyImageTitleshot)
	var found bool
	for _, p := range (&GamelistXMLScraper{}).MapToDB(&rec).TitleProps {
		if p.TypeTag == propKey {
			found = true
			assert.Contains(t, p.Text, "titlescreen_source", "TitleScreen should take priority over TitleShot")
			assert.NotContains(t, p.Text, "titleshot_source")
		}
	}
	assert.True(t, found, "titleshot property missing")
}

func TestMapToDB_TitleShotXMLFromTitleShotWhenNoTitleScreen(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	rec := GamelistRecord{
		SystemRootPath: root,
		Game: esapi.Game{
			Path:      "./roms/game.rom",
			TitleShot: "./media/titleshot_source/game.png",
		},
	}
	propKey := string(tags.TagTypeProperty) + ":" + string(tags.TagPropertyImageTitleshot)
	var found bool
	for _, p := range (&GamelistXMLScraper{}).MapToDB(&rec).TitleProps {
		if p.TypeTag == propKey {
			found = true
			assert.Contains(t, p.Text, "titleshot_source")
		}
	}
	assert.True(t, found, "titleshot from TitleShot missing when TitleScreen is empty")
}

func TestMapToDB_MarqueeXMLPath(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	rec := GamelistRecord{
		SystemRootPath: root,
		Game:           esapi.Game{Path: "./roms/game.rom", Marquee: "./media/marquee/game.png"},
	}
	propKey := string(tags.TagTypeProperty) + ":" + string(tags.TagPropertyImageMarquee)
	var found bool
	for _, p := range (&GamelistXMLScraper{}).MapToDB(&rec).TitleProps {
		if p.TypeTag == propKey {
			found = true
			assert.Equal(t, "image/png", p.ContentType)
		}
	}
	assert.True(t, found, "marquee property missing")
}

func TestMapToDB_FanArtXMLPath(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	rec := GamelistRecord{
		SystemRootPath: root,
		Game:           esapi.Game{Path: "./roms/game.rom", FanArt: "./media/fanart/game.png"},
	}
	propKey := string(tags.TagTypeProperty) + ":" + string(tags.TagPropertyImageFanart)
	var found bool
	for _, p := range (&GamelistXMLScraper{}).MapToDB(&rec).TitleProps {
		if p.TypeTag == propKey {
			found = true
			assert.Equal(t, "image/png", p.ContentType)
		}
	}
	assert.True(t, found, "fanart property missing")
}

func TestMapToDB_MapXMLPath(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	rec := GamelistRecord{
		SystemRootPath: root,
		Game:           esapi.Game{Path: "./roms/game.rom", Map: "./media/map/game.png"},
	}
	propKey := string(tags.TagTypeProperty) + ":" + string(tags.TagPropertyImageMap)
	var found bool
	for _, p := range (&GamelistXMLScraper{}).MapToDB(&rec).TitleProps {
		if p.TypeTag == propKey {
			found = true
		}
	}
	assert.True(t, found, "map image property missing")
}

func TestMapToDB_ScreenshotXMLPath(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	rec := GamelistRecord{
		SystemRootPath: root,
		Game:           esapi.Game{Path: "./roms/game.rom", Screenshot: "./media/screenshot/game.png"},
	}
	propKey := string(tags.TagTypeProperty) + ":" + string(tags.TagPropertyImageScreenshot)
	var found bool
	for _, p := range (&GamelistXMLScraper{}).MapToDB(&rec).TitleProps {
		if p.TypeTag == propKey {
			found = true
			assert.Equal(t, "image/png", p.ContentType)
		}
	}
	assert.True(t, found, "screenshot property missing")
}

// --- loadCompanionEntries ---

// companionXML is a gamelist.xml with one parent and one child companion entry.
const companionXML = `<gameList>
  <game id="42" source="ZaparooCompanion">
    <name>Test Game</name>
    <developer>Dev Corp</developer>
  </game>
  <game parentid="42" source="ZaparooCompanion">
    <path>./child.rom</path>
    <region>usa</region>
    <lang>en</lang>
  </game>
</gameList>`

func TestLoadCompanionEntries_NoEntries(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`
<gameList>
  <game><path>./mario.nes</path><name>Mario</name></game>
</gameList>`), 0o600))

	s := &GamelistXMLScraper{}
	parents, children := s.loadCompanionEntries(
		context.Background(),
		scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}},
	)
	assert.Empty(t, parents)
	assert.Empty(t, children)
}

func TestLoadCompanionEntries_ParentAndChild(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(companionXML), 0o600))

	s := &GamelistXMLScraper{}
	parents, children := s.loadCompanionEntries(
		context.Background(),
		scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}},
	)

	require.Len(t, parents, 1)
	assert.Equal(t, "42", parents[0].GameID)
	assert.Equal(t, "Dev Corp", parents[0].Game.Developer)
	assert.Equal(t, root, parents[0].SystemRootPath)

	require.Len(t, children, 1)
	assert.Equal(t, "42", children[0].ParentGameID)
	assert.Equal(t, "usa", children[0].Region)
	assert.Equal(t, "en", children[0].Lang)
	assert.Equal(t, filepath.Join(root, "child.rom"), children[0].ResolvedPath)
}

func TestLoadCompanionEntries_SourceAsElement(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	// source as an XML child element, not as an attribute.
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`<gameList>
  <game id="7">
    <source>ZaparooCompanion</source>
    <name>ElementSource</name>
    <developer>Test</developer>
  </game>
</gameList>`), 0o600))

	s := &GamelistXMLScraper{}
	parents, children := s.loadCompanionEntries(
		context.Background(),
		scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}},
	)
	require.Len(t, parents, 1, "source element should be recognized as companion")
	assert.Equal(t, "7", parents[0].GameID)
	assert.Empty(t, children)
}

func TestLoadCompanionEntries_ContextCancelled(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(companionXML), 0o600))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s := &GamelistXMLScraper{}
	parents, children := s.loadCompanionEntries(
		ctx,
		scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}},
	)
	// Pre-cancelled context: outer loop select fires ctx.Done before reading the file.
	assert.Empty(t, parents)
	assert.Empty(t, children)
}

func TestLoadCompanionEntries_MissingGamelist(t *testing.T) {
	t.Parallel()
	root := t.TempDir() // no gamelist.xml created

	s := &GamelistXMLScraper{}
	parents, children := s.loadCompanionEntries(
		context.Background(),
		scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}},
	)
	assert.Empty(t, parents)
	assert.Empty(t, children)
}

func TestLoadCompanionEntries_MalformedGamelist(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"),
		[]byte(`<gameList><game id="1"`), 0o600))

	s := &GamelistXMLScraper{}
	parents, children := s.loadCompanionEntries(
		context.Background(),
		scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}},
	)
	assert.Empty(t, parents)
	assert.Empty(t, children)
}

func TestLoadCompanionEntries_EntrySkippedNoIdNoPath(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	// parentid set but no path → can't be a child; falls through to default (skipped).
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`<gameList>
  <game parentid="2" source="ZaparooCompanion">
    <name>No Path</name>
  </game>
</gameList>`), 0o600))

	s := &GamelistXMLScraper{}
	parents, children := s.loadCompanionEntries(
		context.Background(),
		scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}},
	)
	assert.Empty(t, parents)
	assert.Empty(t, children)
}

func TestLoadCompanionEntries_ChildPathTraversalRejected(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	// Child path escapes root → resolveESPath returns "" → child skipped.
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`<gameList>
  <game id="1" source="ZaparooCompanion">
    <name>Parent</name>
    <developer>Dev</developer>
  </game>
  <game parentid="1" source="ZaparooCompanion">
    <path>../../etc/passwd</path>
  </game>
</gameList>`), 0o600))

	s := &GamelistXMLScraper{}
	parents, children := s.loadCompanionEntries(
		context.Background(),
		scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}},
	)
	require.Len(t, parents, 1)
	assert.Empty(t, children, "child with traversal path must be rejected")
}

// --- processCompanionEntries ---

func TestProcessCompanionEntries_NoEntries(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`
<gameList>
  <game><path>./mario.nes</path><name>Mario</name></game>
</gameList>`), 0o600))

	mockDB := helpers.NewMockMediaDBI()
	s := &GamelistXMLScraper{db: mockDB}
	system := scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}, DBID: 1}
	s.processCompanionEntries(context.Background(), system, mockDB)
	// No companion entries → no DB calls.
	mockDB.AssertExpectations(t)
}

func TestProcessCompanionEntries_ChildByFilename(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(companionXML), 0o600))

	mockDB := helpers.NewMockMediaDBI()
	mockDB.On("FindMediaBySystemAndPathSuffix", mock.Anything, int64(1), "child.rom").
		Return([]database.Media{{DBID: 10, MediaTitleDBID: 20}}, nil)
	mockDB.On("UpsertMediaTitleTags", mock.Anything, int64(20), mock.Anything).Return(nil)
	mockDB.On("UpsertMediaTitleProperties", mock.Anything, int64(20), mock.Anything).Return(nil)
	mockDB.On("UpsertMediaTags", mock.Anything, int64(10), mock.Anything).Return(nil)

	s := &GamelistXMLScraper{db: mockDB}
	system := scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}, DBID: 1}
	s.processCompanionEntries(context.Background(), system, mockDB)
	mockDB.AssertExpectations(t)
}

func TestProcessCompanionEntries_ChildBySlugFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`<gameList>
  <game id="99" source="ZaparooCompanion">
    <name>Slug Game</name>
    <developer>Dev</developer>
  </game>
  <game parentid="99" source="ZaparooCompanion">
    <path>./myslug.slug</path>
  </game>
</gameList>`), 0o600))

	title := &database.MediaTitle{DBID: 30}
	mockDB := helpers.NewMockMediaDBI()
	mockDB.On("FindMediaTitleBySystemAndSlug", mock.Anything, int64(5), "myslug").Return(title, nil)
	mockDB.On("UpsertMediaTitleTags", mock.Anything, int64(30), mock.Anything).Return(nil)
	mockDB.On("UpsertMediaTitleProperties", mock.Anything, int64(30), mock.Anything).Return(nil)
	// No region/lang on child → UpsertMediaTags not called.

	s := &GamelistXMLScraper{db: mockDB}
	system := scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}, DBID: 5}
	s.processCompanionEntries(context.Background(), system, mockDB)
	mockDB.AssertExpectations(t)
}

func TestProcessCompanionEntries_SlugNotIndexed(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`<gameList>
  <game id="99" source="ZaparooCompanion">
    <name>Slug Game</name>
    <developer>Dev</developer>
  </game>
  <game parentid="99" source="ZaparooCompanion">
    <path>./missing.slug</path>
  </game>
</gameList>`), 0o600))

	mockDB := helpers.NewMockMediaDBI()
	// FindMediaTitleBySystemAndSlug returns nil → child skipped.
	mockDB.On("FindMediaTitleBySystemAndSlug", mock.Anything, int64(5), "missing").
		Return((*database.MediaTitle)(nil), nil)

	s := &GamelistXMLScraper{db: mockDB}
	system := scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}, DBID: 5}
	s.processCompanionEntries(context.Background(), system, mockDB)
	mockDB.AssertExpectations(t)
}

func TestProcessCompanionEntries_ParentNotFoundForChild(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	// Child references parentid "99" but no parent entry exists.
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`<gameList>
  <game parentid="99" source="ZaparooCompanion">
    <path>./child.rom</path>
  </game>
</gameList>`), 0o600))

	mockDB := helpers.NewMockMediaDBI()
	// Parent not found → child skipped → no DB calls.
	s := &GamelistXMLScraper{db: mockDB}
	system := scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}, DBID: 1}
	s.processCompanionEntries(context.Background(), system, mockDB)
	mockDB.AssertExpectations(t)
}

func TestProcessCompanionEntries_SeenTitleDedup(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	// Two children from same parent; both map to MediaTitleDBID=20.
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`<gameList>
  <game id="42" source="ZaparooCompanion">
    <name>Test Game</name>
    <developer>Dev Corp</developer>
  </game>
  <game parentid="42" source="ZaparooCompanion">
    <path>./child1.rom</path>
    <region>usa</region>
    <lang>en</lang>
  </game>
  <game parentid="42" source="ZaparooCompanion">
    <path>./child2.rom</path>
    <region>jpn</region>
    <lang>ja</lang>
  </game>
</gameList>`), 0o600))

	mockDB := helpers.NewMockMediaDBI()
	mockDB.On("FindMediaBySystemAndPathSuffix", mock.Anything, int64(1), "child1.rom").
		Return([]database.Media{{DBID: 10, MediaTitleDBID: 20}}, nil)
	mockDB.On("FindMediaBySystemAndPathSuffix", mock.Anything, int64(1), "child2.rom").
		Return([]database.Media{{DBID: 11, MediaTitleDBID: 20}}, nil)
	// Title-level tags and props written only once (seenTitles dedup).
	mockDB.On("UpsertMediaTitleTags", mock.Anything, int64(20), mock.Anything).Return(nil).Once()
	mockDB.On("UpsertMediaTitleProperties", mock.Anything, int64(20), mock.Anything).Return(nil).Once()
	// Media-level (region/lang) tags written for each child Media row.
	mockDB.On("UpsertMediaTags", mock.Anything, int64(10), mock.Anything).Return(nil)
	mockDB.On("UpsertMediaTags", mock.Anything, int64(11), mock.Anything).Return(nil)

	s := &GamelistXMLScraper{db: mockDB}
	system := scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}, DBID: 1}
	s.processCompanionEntries(context.Background(), system, mockDB)
	mockDB.AssertExpectations(t)
}

func TestProcessCompanionEntries_NoRegionLangNoChildTags(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	// Child has no region or lang → childTags empty → UpsertMediaTags NOT called.
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`<gameList>
  <game id="1" source="ZaparooCompanion">
    <name>Game</name>
    <developer>Dev</developer>
  </game>
  <game parentid="1" source="ZaparooCompanion">
    <path>./game.rom</path>
  </game>
</gameList>`), 0o600))

	mockDB := helpers.NewMockMediaDBI()
	mockDB.On("FindMediaBySystemAndPathSuffix", mock.Anything, int64(1), "game.rom").
		Return([]database.Media{{DBID: 5, MediaTitleDBID: 6}}, nil)
	mockDB.On("UpsertMediaTitleTags", mock.Anything, int64(6), mock.Anything).Return(nil)
	mockDB.On("UpsertMediaTitleProperties", mock.Anything, int64(6), mock.Anything).Return(nil)
	// UpsertMediaTags must NOT be called (no region or lang).

	s := &GamelistXMLScraper{db: mockDB}
	system := scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}, DBID: 1}
	s.processCompanionEntries(context.Background(), system, mockDB)
	mockDB.AssertExpectations(t)
}

func TestProcessCompanionEntries_FilenameNotIndexed(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(companionXML), 0o600))

	mockDB := helpers.NewMockMediaDBI()
	// FindMediaBySystemAndPathSuffix returns empty → child silently skipped.
	mockDB.On("FindMediaBySystemAndPathSuffix", mock.Anything, int64(1), "child.rom").
		Return([]database.Media{}, nil)

	s := &GamelistXMLScraper{db: mockDB}
	system := scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}, DBID: 1}
	s.processCompanionEntries(context.Background(), system, mockDB)
	mockDB.AssertExpectations(t)
}

// --- scrapeLoop ---

// drainChannel collects all ScrapeUpdates from ch until the channel closes.
func drainChannel(ch chan scraper.ScrapeUpdate) []scraper.ScrapeUpdate {
	var updates []scraper.ScrapeUpdate
	for u := range ch {
		updates = append(updates, u)
	}
	return updates
}

func TestScrapeLoop_NormalMode_Success(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`
<gameList>
  <game><path>./mario.nes</path><name>Mario</name></game>
</gameList>`), 0o600))

	slug := slugFor("nes", filepath.Join(root, "mario.nes"))
	const (
		titleDBID  = int64(1)
		mediaDBID  = int64(10)
		systemDBID = int64(100)
	)
	sentinel := scraper.SentinelTagInfo("gamelist.xml")
	sentinelTag := sentinel.Type + ":" + sentinel.Tag

	mockDB := helpers.NewMockMediaDBI()
	mockDB.On("FindMediaTitlesWithoutSentinel", mock.Anything, systemDBID, sentinelTag).
		Return([]database.MediaTitle{{DBID: titleDBID, SystemDBID: systemDBID, Slug: slug}}, nil)
	mockDB.On("GetMediaBySystemID", "nes").
		Return([]database.MediaWithFullPath{{DBID: mediaDBID, MediaTitleDBID: titleDBID}}, nil)
	mockDB.On("ApplyScrapeResult", mock.Anything, mediaDBID, titleDBID, mock.Anything).Return(nil)

	s := &GamelistXMLScraper{db: mockDB}
	system := scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}, DBID: systemDBID}
	ch := make(chan scraper.ScrapeUpdate, 128)

	s.scrapeLoop(context.Background(), scraper.ScrapeOptions{
		Pauser: syncutil.NewPauser(),
	}, []scraper.ScrapeSystem{system}, mockDB, ch)

	updates := drainChannel(ch)
	var done scraper.ScrapeUpdate
	for _, u := range updates {
		if u.Done {
			done = u
		}
	}
	require.True(t, done.Done)
	assert.Equal(t, 1, done.Processed)
	assert.Equal(t, 1, done.Matched)
	assert.Equal(t, 0, done.Skipped)
	mockDB.AssertExpectations(t)
}

func TestScrapeLoop_ForceMode_Success(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`
<gameList>
  <game><path>./sonic.md</path><name>Sonic</name></game>
</gameList>`), 0o600))

	slug := slugFor("genesis", filepath.Join(root, "sonic.md"))
	const (
		titleDBID  = int64(2)
		mediaDBID  = int64(20)
		systemDBID = int64(200)
	)

	mockDB := helpers.NewMockMediaDBI()
	mockDB.On("GetTitlesBySystemID", "genesis").
		Return([]database.TitleWithSystem{{DBID: titleDBID, SystemDBID: systemDBID, Slug: slug}}, nil)
	mockDB.On("GetMediaBySystemID", "genesis").
		Return([]database.MediaWithFullPath{{DBID: mediaDBID, MediaTitleDBID: titleDBID}}, nil)
	mockDB.On("ApplyScrapeResult", mock.Anything, mediaDBID, titleDBID, mock.Anything).Return(nil)

	s := &GamelistXMLScraper{db: mockDB}
	system := scraper.ScrapeSystem{ID: "genesis", ROMPaths: []string{root}, DBID: systemDBID}
	ch := make(chan scraper.ScrapeUpdate, 128)

	s.scrapeLoop(context.Background(), scraper.ScrapeOptions{
		Pauser: syncutil.NewPauser(),
		Force:  true,
	}, []scraper.ScrapeSystem{system}, mockDB, ch)

	updates := drainChannel(ch)
	var done scraper.ScrapeUpdate
	for _, u := range updates {
		if u.Done {
			done = u
		}
	}
	assert.Equal(t, 1, done.Processed)
	assert.Equal(t, 1, done.Matched)
	mockDB.AssertExpectations(t)
}

func TestScrapeLoop_WriteError_RecordSkipped(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`
<gameList>
  <game><path>./mario.nes</path><name>Mario</name></game>
</gameList>`), 0o600))

	slug := slugFor("nes", filepath.Join(root, "mario.nes"))
	const (
		titleDBID  = int64(3)
		mediaDBID  = int64(30)
		systemDBID = int64(300)
	)
	sentinel := scraper.SentinelTagInfo("gamelist.xml")
	sentinelTag := sentinel.Type + ":" + sentinel.Tag

	mockDB := helpers.NewMockMediaDBI()
	mockDB.On("FindMediaTitlesWithoutSentinel", mock.Anything, systemDBID, sentinelTag).
		Return([]database.MediaTitle{{DBID: titleDBID, SystemDBID: systemDBID, Slug: slug}}, nil)
	mockDB.On("GetMediaBySystemID", "nes").
		Return([]database.MediaWithFullPath{{DBID: mediaDBID, MediaTitleDBID: titleDBID}}, nil)
	mockDB.On("ApplyScrapeResult", mock.Anything, mediaDBID, titleDBID, mock.Anything).
		Return(assert.AnError)

	s := &GamelistXMLScraper{db: mockDB}
	system := scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}, DBID: systemDBID}
	ch := make(chan scraper.ScrapeUpdate, 128)

	s.scrapeLoop(context.Background(), scraper.ScrapeOptions{
		Pauser: syncutil.NewPauser(),
	}, []scraper.ScrapeSystem{system}, mockDB, ch)

	updates := drainChannel(ch)
	var done scraper.ScrapeUpdate
	for _, u := range updates {
		if u.Done {
			done = u
		}
	}
	assert.Equal(t, 1, done.Processed)
	assert.Equal(t, 0, done.Matched)
	assert.Equal(t, 1, done.Skipped)
	mockDB.AssertExpectations(t)
}

func TestScrapeLoop_EmptyTitles_SkipsSystem(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`
<gameList>
  <game><path>./mario.nes</path><name>Mario</name></game>
</gameList>`), 0o600))

	const systemDBID = int64(400)
	sentinel := scraper.SentinelTagInfo("gamelist.xml")
	sentinelTag := sentinel.Type + ":" + sentinel.Tag

	mockDB := helpers.NewMockMediaDBI()
	// All titles already scraped → empty result → GetMediaBySystemID and ApplyScrapeResult never called.
	mockDB.On("FindMediaTitlesWithoutSentinel", mock.Anything, systemDBID, sentinelTag).
		Return([]database.MediaTitle{}, nil)

	s := &GamelistXMLScraper{db: mockDB}
	system := scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}, DBID: systemDBID}
	ch := make(chan scraper.ScrapeUpdate, 128)

	s.scrapeLoop(context.Background(), scraper.ScrapeOptions{
		Pauser: syncutil.NewPauser(),
	}, []scraper.ScrapeSystem{system}, mockDB, ch)

	updates := drainChannel(ch)
	var done scraper.ScrapeUpdate
	for _, u := range updates {
		if u.Done {
			done = u
		}
	}
	require.True(t, done.Done)
	assert.Equal(t, 0, done.Processed)
	mockDB.AssertExpectations(t)
}
