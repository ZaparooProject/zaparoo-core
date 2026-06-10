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
	"strconv"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
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

func TestCompanionChildTags_NormalizesCSV(t *testing.T) {
	t.Parallel()

	got := companionChildTags(companionChild{Region: "USA, EUR", Lang: "EN, JA"})

	assert.Equal(t, []database.TagInfo{
		{Type: string(tags.TagTypeRegion), Tag: "usa"},
		{Type: string(tags.TagTypeRegion), Tag: "eur"},
		{Type: string(tags.TagTypeLang), Tag: "en"},
		{Type: string(tags.TagTypeLang), Tag: "ja"},
	}, got)
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

func assertCompanionCounts(t *testing.T, stats *companionStats, processed, matched, skipped int) {
	t.Helper()
	assert.Equal(t, processed, stats.Processed)
	assert.Equal(t, matched, stats.Matched)
	assert.Equal(t, skipped, stats.Skipped)
}

type batchMockMediaDB struct {
	*helpers.MockMediaDBI
	batchErr error
	batches  [][]database.ScrapeWriteTarget
}

func (m *batchMockMediaDB) ApplyScrapeResults(
	_ context.Context, targets []database.ScrapeWriteTarget,
) error {
	batch := append([]database.ScrapeWriteTarget(nil), targets...)
	m.batches = append(m.batches, batch)
	return m.batchErr
}

func mediaByPath(rows ...database.Media) loadRecordIndexes {
	indexes := loadRecordIndexes{
		TitlesBySlug:     map[string]database.MediaTitle{},
		AllTitlesBySlug:  map[string]database.MediaTitle{},
		MediaByPathFold:  make(map[string]database.Media, len(rows)),
		MediaByTitleDBID: make(map[int64][]database.Media, len(rows)),
		MediaByFilename:  make(map[string][]database.Media, len(rows)),
	}
	for _, row := range rows {
		indexes.MediaByPathFold[pathFoldKey(row.Path)] = row
		indexes.MediaByTitleDBID[row.MediaTitleDBID] = append(indexes.MediaByTitleDBID[row.MediaTitleDBID], row)
		filenameKey := mediaFilenameKey(row.Path)
		if filenameKey != "" {
			indexes.MediaByFilename[filenameKey] = append(indexes.MediaByFilename[filenameKey], row)
		}
	}
	return indexes
}

func mediaBySlugAndPath(slug string, title *database.MediaTitle, rows ...database.Media) loadRecordIndexes {
	indexes := mediaByPath(rows...)
	indexes.TitlesBySlug[slug] = *title
	return indexes
}

func TestLoadRecords_PathMatch(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "media", "image"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`
<gameList>
  <game><path>./mario.nes</path><name>Mario</name></game>
  <game><path>./zelda.nes</path><name>Zelda</name></game>
</gameList>`), 0o600))

	records, err := (&GamelistXMLScraper{}).LoadRecords(
		context.Background(),
		scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}},
		mediaByPath(database.Media{DBID: 11, MediaTitleDBID: 22, Path: filepath.Join(root, "mario.nes")}),
	)
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, root, records[0].SystemRootPath)
	assert.Equal(t, "./mario.nes", records[0].Game.Path)
	assert.Equal(t, "Mario", records[0].Game.Name)
	assert.Equal(t, int64(11), records[0].MatchedMediaDBID)
	assert.Equal(t, int64(22), records[0].MatchedTitleDBID)
	assert.Equal(t, filepath.Join(root, "media", "image"), records[0].AvailableMediaDirs["image"])
}

func TestLoadRecords_SubfolderPathMatch(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "Japan"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`
<gameList>
  <game><path>./Japan/Game.nes</path><name>Game</name></game>
</gameList>`), 0o600))

	records, err := (&GamelistXMLScraper{}).LoadRecords(
		context.Background(),
		scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}},
		mediaByPath(database.Media{DBID: 12, MediaTitleDBID: 23, Path: filepath.Join(root, "Japan", "Game.nes")}),
	)
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, "./Japan/Game.nes", records[0].Game.Path)
	assert.Equal(t, int64(12), records[0].MatchedMediaDBID)
	assert.Equal(t, int64(23), records[0].MatchedTitleDBID)
}

func TestLoadRecords_SkipsCompanionChildren(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`
<gameList>
  <game id="42" source="ZaparooCompanion">
    <name>Parent</name>
  </game>
  <game parentid="42" source="ZaparooCompanion">
    <path>./child.nes</path>
  </game>
</gameList>`), 0o600))

	records, err := (&GamelistXMLScraper{}).LoadRecords(
		context.Background(),
		scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}},
		mediaByPath(database.Media{DBID: 12, MediaTitleDBID: 23, Path: filepath.Join(root, "child.nes")}),
	)
	require.NoError(t, err)
	assert.Empty(t, records, "companion children must be handled only by companion processing")
}

func TestParsedGamelistFeedsCompanionAndRegularRecords(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`
<gameList>
  <game id="42" source="ZaparooCompanion">
    <name>Parent</name>
    <developer>Dev</developer>
  </game>
  <game parentid="42" source="ZaparooCompanion">
    <path>./child.nes</path>
  </game>
  <game><path>./regular.nes</path><name>Regular</name></game>
</gameList>`), 0o600))

	s := &GamelistXMLScraper{}
	system := scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}}
	parsed, err := s.loadParsedGamelistSystem(context.Background(), system)
	require.NoError(t, err)
	require.Len(t, parsed.Files, 1)

	parents, children := companionEntriesFromParsed(context.Background(), system, parsed)
	require.Len(t, parents, 1)
	require.Len(t, children, 1)
	assert.Equal(t, filepath.Join(root, "child.nes"), children[0].ResolvedPath)

	records, err := s.loadRecordsFromParsed(
		context.Background(),
		system,
		mediaByPath(database.Media{DBID: 12, MediaTitleDBID: 23, Path: filepath.Join(root, "regular.nes")}),
		parsed,
	)
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, "./regular.nes", records[0].Game.Path)
	assert.Equal(t, int64(12), records[0].MatchedMediaDBID)
}

func TestLoadRecords_DoesNotReadNestedGameList(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "Japan"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(root, "Japan", "gamelist.xml"), []byte(`
<gameList>
  <game><path>./Game.nes</path><name>Game</name></game>
</gameList>`), 0o600))

	records, err := (&GamelistXMLScraper{}).LoadRecords(
		context.Background(),
		scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}},
		mediaByPath(database.Media{DBID: 13, MediaTitleDBID: 24, Path: filepath.Join(root, "Japan", "Game.nes")}),
	)
	require.NoError(t, err)
	assert.Empty(t, records)
}

func TestLoadRecords_TitleNameDoesNotNeedSlugMatch(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`
<gameList>
  <game><path>./mslug.zip</path><name>Metal Slug</name></game>
</gameList>`), 0o600))

	records, err := (&GamelistXMLScraper{}).LoadRecords(
		context.Background(),
		scraper.ScrapeSystem{ID: "NeoGeo", ROMPaths: []string{root}},
		mediaBySlugAndPath("metal-slug", &database.MediaTitle{DBID: 42, Slug: "metal-slug"},
			database.Media{DBID: 7, MediaTitleDBID: 42, Path: filepath.Join(root, "mslug.zip")}),
	)
	require.NoError(t, err)
	require.Len(t, records, 1, "slug match should work even when file basename differs from display name")
	assert.Equal(t, "Metal Slug", records[0].Game.Name)
	assert.Equal(t, int64(42), records[0].MatchedTitleDBID)
	assert.Equal(t, int64(7), records[0].MatchedMediaDBID)
}

func TestLoadRecords_PokemonAccentMatchesNonAccentedSlug(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`
<gameList>
  <game><path>./pkruby.gba</path><name>Pokémon Ruby Version</name></game>
</gameList>`), 0o600))

	records, err := (&GamelistXMLScraper{}).LoadRecords(
		context.Background(),
		scraper.ScrapeSystem{ID: "gba", ROMPaths: []string{root}},
		mediaBySlugAndPath("pokemonruby", &database.MediaTitle{DBID: 856, Slug: "pokemonruby"},
			database.Media{DBID: 857, MediaTitleDBID: 856, Path: filepath.Join(root, "indexed", "Pokemon Ruby.gba")}),
	)
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, "Pokémon Ruby Version", records[0].Game.Name)
	assert.Equal(t, int64(856), records[0].MatchedTitleDBID)
	assert.Equal(t, int64(857), records[0].MatchedMediaDBID)
	assert.Equal(t, gamelistMatchSlugOnly, records[0].MatchKind)
	assert.False(t, records[0].MediaLevelWriteSafe)
}

func TestLoadRecords_SlugMatchSelectsExactMediaPath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	usaPath := filepath.Join(root, "USA", "Game.nes")
	japanPath := filepath.Join(root, "Japan", "Game.nes")
	require.NoError(t, os.MkdirAll(filepath.Dir(usaPath), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Dir(japanPath), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`
<gameList>
  <game><path>./Japan/Game.nes</path><name>Game</name></game>
</gameList>`), 0o600))

	records, err := (&GamelistXMLScraper{}).LoadRecords(
		context.Background(),
		scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}},
		mediaBySlugAndPath("game", &database.MediaTitle{DBID: 50, Slug: "game"},
			database.Media{DBID: 60, MediaTitleDBID: 50, Path: usaPath},
			database.Media{DBID: 61, MediaTitleDBID: 50, Path: japanPath}),
	)
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, int64(50), records[0].MatchedTitleDBID)
	assert.Equal(t, int64(61), records[0].MatchedMediaDBID)
	assert.Equal(t, gamelistMatchSlugPath, records[0].MatchKind)
	assert.True(t, records[0].MediaLevelWriteSafe)
}

func TestLoadRecords_DuplicateNameExactPathsAllMediaSafe(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	disc1Path := filepath.Join(root, "Game (Disc 1).cue")
	disc2Path := filepath.Join(root, "Game (Disc 2).cue")
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`
<gameList>
  <game><path>./Game (Disc 1).cue</path><name>Game</name></game>
  <game><path>./Game (Disc 2).cue</path><name>Game</name></game>
</gameList>`), 0o600))

	records, err := (&GamelistXMLScraper{}).LoadRecords(
		context.Background(),
		scraper.ScrapeSystem{ID: "psx", ROMPaths: []string{root}},
		mediaBySlugAndPath("game", &database.MediaTitle{DBID: 50, Slug: "game"},
			database.Media{DBID: 60, MediaTitleDBID: 50, Path: disc1Path},
			database.Media{DBID: 61, MediaTitleDBID: 50, Path: disc2Path}),
	)
	require.NoError(t, err)
	require.Len(t, records, 2)
	assert.Equal(t, int64(60), records[0].MatchedMediaDBID)
	assert.Equal(t, int64(61), records[1].MatchedMediaDBID)
	for _, record := range records {
		assert.Equal(t, int64(50), record.MatchedTitleDBID)
		assert.Equal(t, gamelistMatchSlugPath, record.MatchKind)
		assert.True(t, record.MediaLevelWriteSafe)
	}
}

func TestLoadRecords_TrackPathResolvesToCueMedia(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	gameDir := filepath.Join(root, "Game")
	cuePath := filepath.Join(gameDir, "Game.cue")
	require.NoError(t, os.MkdirAll(gameDir, 0o750))
	require.NoError(t, os.WriteFile(cuePath, []byte("FILE \"track01.bin\" BINARY\n  TRACK 01 MODE2/2352\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`
<gameList>
  <game>
    <path>./Game/track01.bin</path>
    <name>Game</name>
    <image>./media/images/Game.png</image>
  </game>
</gameList>`), 0o600))

	records, err := (&GamelistXMLScraper{}).LoadRecords(
		context.Background(),
		scraper.ScrapeSystem{ID: "psx", ROMPaths: []string{root}},
		mediaBySlugAndPath("game", &database.MediaTitle{DBID: 50, Slug: "game"},
			database.Media{DBID: 60, MediaTitleDBID: 50, Path: cuePath}),
	)
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, int64(60), records[0].MatchedMediaDBID)
	assert.Equal(t, gamelistMatchSlugPath, records[0].MatchKind)
	assert.True(t, records[0].MediaLevelWriteSafe)

	result := (&GamelistXMLScraper{}).MapToDB(records[0])
	propKey := string(tags.TagTypeProperty) + ":" + string(tags.TagPropertyImageImage)
	var found bool
	for _, p := range result.MediaProps {
		if p.TypeTag == propKey {
			found = true
			assert.Equal(t, filepath.ToSlash(filepath.Join(root, "media", "images", "Game.png")), p.Text)
		}
	}
	assert.True(t, found, "canonical cue match should keep media-level explicit image property")
}

func TestLoadRecords_TrackPathResolvesToM3UMedia(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	gameDir := filepath.Join(root, "Game")
	cuePath := filepath.Join(gameDir, "Game (Disc 1).cue")
	m3uPath := filepath.Join(root, "Game.m3u")
	require.NoError(t, os.MkdirAll(gameDir, 0o750))
	require.NoError(t, os.WriteFile(cuePath, []byte("FILE \"track01.bin\" BINARY\n"), 0o600))
	require.NoError(t, os.WriteFile(m3uPath, []byte(filepath.Join("Game", "Game (Disc 1).cue")+"\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`
<gameList>
  <game><path>./Game/track01.bin</path><name>Game</name></game>
</gameList>`), 0o600))

	records, err := (&GamelistXMLScraper{}).LoadRecords(
		context.Background(),
		scraper.ScrapeSystem{ID: "psx", ROMPaths: []string{root}},
		mediaBySlugAndPath("game", &database.MediaTitle{DBID: 50, Slug: "game"},
			database.Media{DBID: 60, MediaTitleDBID: 50, Path: cuePath},
			database.Media{DBID: 61, MediaTitleDBID: 50, Path: m3uPath}),
	)
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, int64(61), records[0].MatchedMediaDBID)
	assert.Equal(t, gamelistMatchSlugPath, records[0].MatchKind)
	assert.True(t, records[0].MediaLevelWriteSafe)
}

func TestLoadRecords_TrackPathResolvesToM3UCueCaseInsensitive(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	gameDir := filepath.Join(root, "Game")
	cuePath := filepath.Join(gameDir, "Game (Disc 1).cue")
	m3uPath := filepath.Join(root, "Game.m3u")
	require.NoError(t, os.MkdirAll(gameDir, 0o750))
	require.NoError(t, os.WriteFile(cuePath, []byte("FILE \"track01.bin\" BINARY\n"), 0o600))
	require.NoError(t, os.WriteFile(m3uPath, []byte(filepath.Join("game", "game (disc 1).CUE")+"\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`
<gameList>
  <game><path>./Game/track01.bin</path><name>Game</name></game>
</gameList>`), 0o600))

	records, err := (&GamelistXMLScraper{}).LoadRecords(
		context.Background(),
		scraper.ScrapeSystem{ID: "psx", ROMPaths: []string{root}},
		mediaBySlugAndPath("game", &database.MediaTitle{DBID: 50, Slug: "game"},
			database.Media{DBID: 61, MediaTitleDBID: 50, Path: m3uPath}),
	)
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, int64(61), records[0].MatchedMediaDBID)
	assert.Equal(t, gamelistMatchSlugPath, records[0].MatchKind)
	assert.True(t, records[0].MediaLevelWriteSafe)
}

func TestLoadRecords_TrackPathAmbiguousCueMatchesUnsafe(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	cue1Path := filepath.Join(root, "Game A.cue")
	cue2Path := filepath.Join(root, "Game B.cue")
	require.NoError(t, os.WriteFile(cue1Path, []byte("FILE track01.bin BINARY\n"), 0o600))
	require.NoError(t, os.WriteFile(cue2Path, []byte("FILE track01.bin BINARY\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`
<gameList>
  <game><path>./track01.bin</path><name>Game</name></game>
</gameList>`), 0o600))

	records, err := (&GamelistXMLScraper{}).LoadRecords(
		context.Background(),
		scraper.ScrapeSystem{ID: "psx", ROMPaths: []string{root}},
		mediaBySlugAndPath("game", &database.MediaTitle{DBID: 50, Slug: "game"},
			database.Media{DBID: 60, MediaTitleDBID: 50, Path: cue1Path},
			database.Media{DBID: 61, MediaTitleDBID: 50, Path: cue2Path}),
	)
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, gamelistMatchSlugOnly, records[0].MatchKind)
	assert.False(t, records[0].MediaLevelWriteSafe)
}

func TestLoadRecords_MultiDiscTracksResolveDistinctCues(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	disc1Dir := filepath.Join(root, "Disc 1")
	disc2Dir := filepath.Join(root, "Disc 2")
	disc1Cue := filepath.Join(disc1Dir, "Game (Disc 1).cue")
	disc2Cue := filepath.Join(disc2Dir, "Game (Disc 2).cue")
	require.NoError(t, os.MkdirAll(disc1Dir, 0o750))
	require.NoError(t, os.MkdirAll(disc2Dir, 0o750))
	require.NoError(t, os.WriteFile(disc1Cue, []byte("FILE track01.bin BINARY\n"), 0o600))
	require.NoError(t, os.WriteFile(disc2Cue, []byte("FILE track01.bin BINARY\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`
<gameList>
  <game><path>./Disc 1/track01.bin</path><name>Game</name></game>
  <game><path>./Disc 2/track01.bin</path><name>Game</name></game>
</gameList>`), 0o600))

	records, err := (&GamelistXMLScraper{}).LoadRecords(
		context.Background(),
		scraper.ScrapeSystem{ID: "psx", ROMPaths: []string{root}},
		mediaBySlugAndPath("game", &database.MediaTitle{DBID: 50, Slug: "game"},
			database.Media{DBID: 60, MediaTitleDBID: 50, Path: disc1Cue},
			database.Media{DBID: 61, MediaTitleDBID: 50, Path: disc2Cue}),
	)
	require.NoError(t, err)
	require.Len(t, records, 2)
	assert.Equal(t, int64(60), records[0].MatchedMediaDBID)
	assert.Equal(t, int64(61), records[1].MatchedMediaDBID)
	for _, record := range records {
		assert.Equal(t, gamelistMatchSlugPath, record.MatchKind)
		assert.True(t, record.MediaLevelWriteSafe)
	}
}

func TestLoadRecords_KnownSlugDoesNotBlockExactPathFallback(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	gamePath := filepath.Join(root, "conflict.nes")
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`
<gameList>
  <game><path>./conflict.nes</path><name>Known Title</name></game>
</gameList>`), 0o600))

	indexes := mediaByPath(database.Media{DBID: 70, MediaTitleDBID: 80, Path: gamePath})
	indexes.AllTitlesBySlug = map[string]database.MediaTitle{
		"knowntitle": {DBID: 90, Slug: "knowntitle", Name: "Known Title"},
	}

	records, err := (&GamelistXMLScraper{}).LoadRecords(
		context.Background(),
		scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}},
		indexes,
	)
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, int64(80), records[0].MatchedTitleDBID)
	assert.Equal(t, int64(70), records[0].MatchedMediaDBID)
	assert.Equal(t, gamelistMatchPathOnly, records[0].MatchKind)
	assert.True(t, records[0].MediaLevelWriteSafe)
}

func TestLoadRecords_SkipsPathOnlyFallbackWhenSlugKnownWithoutExactPath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	indexedPath := filepath.Join(root, "indexed.nes")
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`
<gameList>
  <game><path>./conflict.nes</path><name>Known Title</name></game>
</gameList>`), 0o600))

	indexes := mediaByPath(database.Media{DBID: 70, MediaTitleDBID: 80, Path: indexedPath})
	indexes.AllTitlesBySlug = map[string]database.MediaTitle{
		"knowntitle": {DBID: 90, Slug: "knowntitle", Name: "Known Title"},
	}

	records, err := (&GamelistXMLScraper{}).LoadRecords(
		context.Background(),
		scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}},
		indexes,
	)
	require.NoError(t, err)
	assert.Empty(t, records)
}

func TestLoadRecords_PathOnlyFallbackWhenSlugMissing(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	gamePath := filepath.Join(root, "odd-name.nes")
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`
<gameList>
  <game><path>./odd-name.nes</path><name>Unindexed Display Name</name></game>
</gameList>`), 0o600))

	records, err := (&GamelistXMLScraper{}).LoadRecords(
		context.Background(),
		scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}},
		mediaByPath(database.Media{DBID: 70, MediaTitleDBID: 80, Path: gamePath}),
	)
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, int64(80), records[0].MatchedTitleDBID)
	assert.Equal(t, int64(70), records[0].MatchedMediaDBID)
	assert.Equal(t, gamelistMatchPathOnly, records[0].MatchKind)
	assert.True(t, records[0].MediaLevelWriteSafe)
}

func TestLoadRecords_ZipAsDirChildMatch(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	zipPath := filepath.Join(root, "Japan", "10-Yard Fight (Japan) (Rev 1).zip")
	innerPath := filepath.Join(zipPath, "10-Yard Fight (Japan) (Rev 1).nes")
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`
<gameList>
  <game><path>./Japan/10-Yard Fight (Japan) (Rev 1).zip</path><name>10-Yard Fight</name></game>
</gameList>`), 0o600))

	records, err := (&GamelistXMLScraper{}).LoadRecords(
		context.Background(),
		scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}},
		mediaByPath(database.Media{DBID: 70, MediaTitleDBID: 80, Path: innerPath}),
	)
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, "10-Yard Fight", records[0].Game.Name)
	assert.Equal(t, int64(70), records[0].MatchedMediaDBID)
	assert.Equal(t, int64(80), records[0].MatchedTitleDBID)
}

func TestLoadRecords_DarksoftFolderChildMatch(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	setDir := filepath.Join(root, "2020bb")
	mediaPath := filepath.Join(setDir, "2020bb.xml")
	require.NoError(t, os.MkdirAll(setDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`
<gameList>
  <game>
    <path>./2020bb</path>
    <name>2020 Super Baseball</name>
    <genre>Sports</genre>
  </game>
</gameList>`), 0o600))

	records, err := (&GamelistXMLScraper{}).LoadRecords(
		context.Background(),
		scraper.ScrapeSystem{ID: "NeoGeo", ROMPaths: []string{root}},
		mediaByPath(database.Media{DBID: 70, MediaTitleDBID: 80, Path: mediaPath}),
	)
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, "2020 Super Baseball", records[0].Game.Name)
	assert.Equal(t, int64(70), records[0].MatchedMediaDBID)
	assert.Equal(t, int64(80), records[0].MatchedTitleDBID)
	assert.Equal(t, gamelistMatchPathOnly, records[0].MatchKind)
	assert.True(t, records[0].MediaLevelWriteSafe)
}

func TestLoadRecords_DarksoftFolderAmbiguousChildrenSkipped(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	setDir := filepath.Join(root, "2020bb")
	require.NoError(t, os.MkdirAll(setDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`
<gameList>
  <game><path>./2020bb</path><name>2020 Super Baseball</name></game>
</gameList>`), 0o600))

	records, err := (&GamelistXMLScraper{}).LoadRecords(
		context.Background(),
		scraper.ScrapeSystem{ID: "NeoGeo", ROMPaths: []string{root}},
		mediaByPath(
			database.Media{DBID: 71, MediaTitleDBID: 81, Path: filepath.Join(setDir, "2020bb.xml")},
			database.Media{DBID: 72, MediaTitleDBID: 82, Path: filepath.Join(setDir, "2020bb.mra")},
		),
	)
	require.NoError(t, err)
	assert.Empty(t, records)
}

func TestLoadRecords_ZipAsDirAmbiguousChildrenSkipped(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	zipPath := filepath.Join(root, "Japan", "Multi Game.zip")
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`
<gameList>
  <game><path>./Japan/Multi Game.zip</path><name>Multi Game</name></game>
</gameList>`), 0o600))

	records, err := (&GamelistXMLScraper{}).LoadRecords(
		context.Background(),
		scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}},
		mediaByPath(
			database.Media{DBID: 71, MediaTitleDBID: 81, Path: filepath.Join(zipPath, "one.nes")},
			database.Media{DBID: 72, MediaTitleDBID: 82, Path: filepath.Join(zipPath, "two.nes")},
		),
	)
	require.NoError(t, err)
	assert.Empty(t, records)
}

func TestLoadRecords_ZipAsDirExactMatchWins(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	zipPath := filepath.Join(root, "Japan", "Game.zip")
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`
<gameList>
  <game><path>./Japan/Game.zip</path><name>Game</name></game>
</gameList>`), 0o600))

	records, err := (&GamelistXMLScraper{}).LoadRecords(
		context.Background(),
		scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}},
		mediaByPath(
			database.Media{DBID: 73, MediaTitleDBID: 83, Path: zipPath},
			database.Media{DBID: 74, MediaTitleDBID: 84, Path: filepath.Join(zipPath, "inner.nes")},
		),
	)
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, int64(73), records[0].MatchedMediaDBID)
	assert.Equal(t, int64(83), records[0].MatchedTitleDBID)
}

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

	records, err := (&GamelistXMLScraper{}).LoadRecords(
		context.Background(),
		scraper.ScrapeSystem{
			ID:       "nes",
			ROMPaths: []string{missingRoot, malformedRoot, validRoot},
		},
		mediaByPath(database.Media{DBID: 3, MediaTitleDBID: 5, Path: filepath.Join(validRoot, "mario.nes")}),
	)
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, int64(3), records[0].MatchedMediaDBID)
	assert.Equal(t, int64(5), records[0].MatchedTitleDBID)
}

func TestLoadRecords_FirstPathWins(t *testing.T) {
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

	records, err := (&GamelistXMLScraper{}).LoadRecords(
		context.Background(),
		scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root1, root2}},
		mediaByPath(database.Media{DBID: 10, MediaTitleDBID: 1, Path: filepath.Join(root1, "game.nes")}),
	)
	require.NoError(t, err)
	require.Len(t, records, 1, "second root's duplicate path must be skipped after first match")
	assert.Equal(t, root1, records[0].SystemRootPath, "first root wins")
}

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
		mediaByPath(),
	)
	require.ErrorIs(t, err, context.Canceled)
}

func TestLoadRecords_PathTraversalSkipped(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`
<gameList>
  <game><path>../../etc/passwd</path><name>Traversal</name></game>
  <game><path>./mario.nes</path><name>Mario</name></game>
</gameList>`), 0o600))

	records, err := (&GamelistXMLScraper{}).LoadRecords(
		context.Background(),
		scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}},
		mediaByPath(database.Media{DBID: 9, MediaTitleDBID: 1, Path: filepath.Join(root, "mario.nes")}),
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

	assert.Contains(t, result.MediaTags, database.TagInfo{Type: string(tags.TagTypeLang), Tag: "en"})
	assert.Contains(t, result.MediaTags, database.TagInfo{Type: string(tags.TagTypeRegion), Tag: "usa"})

	// Title-level tags
	assert.Contains(t, result.TitleTags, database.TagInfo{
		Type: string(tags.TagTypeDeveloper), Tag: "nintendo", Label: "Nintendo",
	})
	assert.Contains(t, result.TitleTags, database.TagInfo{
		Type: string(tags.TagTypePublisher), Tag: "nintendo", Label: "Nintendo",
	})
	assert.Contains(t, result.TitleTags, database.TagInfo{Type: string(tags.TagTypeYear), Tag: "1985"})
	assert.Contains(t, result.TitleTags, database.TagInfo{Type: string(tags.TagTypeRating), Tag: "75"})
	assert.Contains(t, result.TitleTags, database.TagInfo{
		Type: string(tags.TagTypeGenre), Tag: "platform", Label: "Platform",
	})
	assert.Contains(t, result.TitleTags, database.TagInfo{Type: string(tags.TagTypePlayers), Tag: "4"})

	// Title-level properties
	descPropKey := string(tags.TagTypeProperty) + ":" + string(tags.TagPropertyDescription)
	var foundDesc bool
	for _, p := range result.TitleProps {
		if p.TypeTag == descPropKey {
			foundDesc = true
			assert.Equal(t, "A classic platformer.", p.Text)
		}
	}
	assert.True(t, foundDesc, "description property missing")

	// Media-level properties
	imgPropKey := string(tags.TagTypeProperty) + ":" + string(tags.TagPropertyImageImage)
	videoPropKey := string(tags.TagTypeProperty) + ":" + string(tags.TagPropertyVideo)
	var foundImg, foundVideo bool
	for _, p := range result.MediaProps {
		switch p.TypeTag {
		case imgPropKey:
			foundImg = true
			assert.Equal(t, filepath.ToSlash(filepath.Join(root, "media", "images", "mario.png")), p.Text)
		case videoPropKey:
			foundVideo = true
			assert.Equal(t, filepath.ToSlash(filepath.Join(root, "media", "videos", "mario.mp4")), p.Text)
		}
	}
	assert.True(t, foundImg, "image property missing")
	assert.True(t, foundVideo, "video property missing")
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
	assert.Contains(t, titleTags, database.TagInfo{
		Type: string(tags.TagTypeArcadeBoard), Tag: "cps2", Label: "CPS2",
	})
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
	p := pathProp("prop:image", "./images/mario.png", root, nil)
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
	mediaProps := (&GamelistXMLScraper{}).MapToDB(&rec).MediaProps
	propKey := string(tags.TagTypeProperty) + ":" + string(tags.TagPropertyImageImage)
	for _, p := range mediaProps {
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

func TestMapToDB_NestedExplicitImagePath(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	rec := GamelistRecord{
		SystemRootPath: root,
		Game: esapi.Game{
			Path:  "./Japan/Game.nes",
			Image: "./media/images/Japan/Game.png",
		},
	}

	result := (&GamelistXMLScraper{}).MapToDB(&rec)

	propKey := string(tags.TagTypeProperty) + ":" + string(tags.TagPropertyImageImage)
	var found bool
	for _, p := range result.MediaProps {
		if p.TypeTag == propKey {
			found = true
			assert.Equal(t, filepath.ToSlash(filepath.Join(root, "media", "images", "Japan", "Game.png")), p.Text)
		}
	}
	assert.True(t, found, "nested explicit image property missing")
}

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
	for _, p := range result.MediaProps {
		if p.TypeTag == propKey {
			found = true
			assert.Equal(t, filepath.ToSlash(filepath.Join(imgDir, "mario.png")), p.Text)
			assert.Equal(t, "image/png", p.ContentType)
		}
	}
	assert.True(t, found, "filesystem fallback image property missing")
}

func TestMapToDB_FilesystemFallback_NestedGamePath(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	imgDir := filepath.Join(root, "media", "images")
	imgPath := filepath.Join(imgDir, "Japan", "Game.png")
	require.NoError(t, os.MkdirAll(filepath.Dir(imgPath), 0o750))
	require.NoError(t, os.WriteFile(imgPath, []byte{}, 0o600))

	rec := GamelistRecord{
		SystemRootPath:     root,
		AvailableMediaDirs: map[string]string{"images": imgDir},
		Game: esapi.Game{
			Path: "./Japan/Game.nes",
		},
	}

	result := (&GamelistXMLScraper{}).MapToDB(&rec)

	propKey := string(tags.TagTypeProperty) + ":" + string(tags.TagPropertyImageImage)
	var found bool
	for _, p := range result.MediaProps {
		if p.TypeTag == propKey {
			found = true
			assert.Equal(t, filepath.ToSlash(imgPath), p.Text)
		}
	}
	assert.True(t, found, "nested filesystem fallback image property missing")
}

func TestMapToDB_FilesystemFallback_NestedWinsBeforeFlat(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	imgDir := filepath.Join(root, "media", "images")
	nestedPath := filepath.Join(imgDir, "Japan", "Game.png")
	flatPath := filepath.Join(imgDir, "Game.png")
	require.NoError(t, os.MkdirAll(filepath.Dir(nestedPath), 0o750))
	require.NoError(t, os.WriteFile(nestedPath, []byte{}, 0o600))
	require.NoError(t, os.WriteFile(flatPath, []byte{}, 0o600))

	rec := GamelistRecord{
		SystemRootPath:     root,
		AvailableMediaDirs: map[string]string{"images": imgDir},
		Game:               esapi.Game{Path: "./Japan/Game.nes"},
	}

	result := (&GamelistXMLScraper{}).MapToDB(&rec)

	propKey := string(tags.TagTypeProperty) + ":" + string(tags.TagPropertyImageImage)
	var found bool
	for _, p := range result.MediaProps {
		if p.TypeTag == propKey {
			found = true
			assert.Equal(t, filepath.ToSlash(nestedPath), p.Text)
		}
	}
	assert.True(t, found, "nested filesystem fallback image property missing")
}

func TestMapToDB_FilesystemFallback_ThumbnailBox2DFrontAlias(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	thumbDir := filepath.Join(root, "media", "box2dfront")
	thumbPath := filepath.Join(thumbDir, "Game.png")
	require.NoError(t, os.MkdirAll(thumbDir, 0o750))
	require.NoError(t, os.WriteFile(thumbPath, []byte{}, 0o600))

	rec := GamelistRecord{
		SystemRootPath:     root,
		AvailableMediaDirs: map[string]string{"box2dfront": thumbDir},
		Game:               esapi.Game{Path: "./Game.nes"},
	}

	result := (&GamelistXMLScraper{}).MapToDB(&rec)

	propKey := string(tags.TagTypeProperty) + ":" + string(tags.TagPropertyImageThumbnail)
	var found bool
	for _, p := range result.MediaProps {
		if p.TypeTag == propKey {
			found = true
			assert.Equal(t, filepath.ToSlash(thumbPath), p.Text)
		}
	}
	assert.True(t, found, "box2dfront thumbnail fallback property missing")
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
	for _, p := range result.MediaProps {
		if p.TypeTag == propKey {
			found = true
			assert.Equal(t, filepath.ToSlash(filepath.Join(boxartDir, "sonic.png")), p.Text)
		}
	}
	assert.True(t, found, "filesystem fallback boxart property missing")
}

func TestMapToDB_FallbackFindsBox2DLogoTitleScreenDirs(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	box2DDir := filepath.Join(root, "media", "box2d")
	logoDir := filepath.Join(root, "media", "logo")
	titleScreenDir := filepath.Join(root, "media", "titlescreen")
	for _, dir := range []string{box2DDir, logoDir, titleScreenDir} {
		require.NoError(t, os.MkdirAll(dir, 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "Game.png"), []byte{}, 0o600))
	}

	rec := GamelistRecord{
		SystemRootPath: root,
		AvailableMediaDirs: map[string]string{
			"box2d":       box2DDir,
			"logo":        logoDir,
			"titlescreen": titleScreenDir,
		},
		Game: esapi.Game{Path: "./Game.nes"},
	}

	result := (&GamelistXMLScraper{}).MapToDB(&rec)
	props := map[string]string{}
	for _, p := range result.MediaProps {
		props[p.TypeTag] = p.Text
	}

	propType := string(tags.TagTypeProperty) + ":"
	assert.Equal(t,
		filepath.ToSlash(filepath.Join(box2DDir, "Game.png")),
		props[propType+string(tags.TagPropertyImageBoxart)],
	)
	assert.Equal(t,
		filepath.ToSlash(filepath.Join(logoDir, "Game.png")),
		props[propType+string(tags.TagPropertyImageWheel)],
	)
	assert.Equal(t,
		filepath.ToSlash(filepath.Join(titleScreenDir, "Game.png")),
		props[propType+string(tags.TagPropertyImageTitleshot)],
	)
}

func TestMapToDB_FallbackFindsJPGMediaFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	imgDir := filepath.Join(root, "media", "images")
	imgPath := filepath.Join(imgDir, "Game.jpg")
	require.NoError(t, os.MkdirAll(imgDir, 0o750))
	require.NoError(t, os.WriteFile(imgPath, []byte{}, 0o600))

	rec := GamelistRecord{
		SystemRootPath:     root,
		AvailableMediaDirs: map[string]string{"images": imgDir},
		Game:               esapi.Game{Path: "./Game.nes"},
	}

	result := (&GamelistXMLScraper{}).MapToDB(&rec)

	propKey := string(tags.TagTypeProperty) + ":" + string(tags.TagPropertyImageImage)
	var found bool
	for _, p := range result.MediaProps {
		if p.TypeTag == propKey {
			found = true
			assert.Equal(t, filepath.ToSlash(imgPath), p.Text)
			assert.Equal(t, "image/jpeg", p.ContentType)
		}
	}
	assert.True(t, found, "jpg filesystem fallback image property missing")
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
	for _, p := range result.MediaProps {
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
	for _, p := range result.MediaProps {
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
	for _, p := range result.MediaProps {
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
	for _, p := range result.MediaProps {
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
	for _, p := range result.MediaProps {
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
	for _, p := range result.MediaProps {
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
	got := resolveESAssetPath("./images/art.png", root, nil)
	assert.Equal(t, filepath.Join(root, "images", "art.png"), got)
}

func TestResolveESAssetPath_OutsideRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	got := resolveESAssetPath("../../etc/passwd", root, nil)
	assert.Empty(t, got)
}

func TestResolveESAssetPath_EmptyPath(t *testing.T) {
	t.Parallel()
	got := resolveESAssetPath("", t.TempDir(), nil)
	assert.Empty(t, got)
}

func TestResolveESAssetPath_ExternalRootAccepted(t *testing.T) {
	t.Parallel()
	romRoot := t.TempDir()
	assetRoot := t.TempDir()
	assetPath := filepath.Join(assetRoot, "art", "game.png")

	got := resolveESAssetPath(assetPath, romRoot, []string{assetRoot})

	assert.Equal(t, assetPath, got)
}

func TestResolveESAssetPath_ExternalRootRejectedWhenUnconfigured(t *testing.T) {
	t.Parallel()
	romRoot := t.TempDir()
	assetRoot := t.TempDir()
	assetPath := filepath.Join(assetRoot, "art", "game.png")

	got := resolveESAssetPath(assetPath, romRoot, nil)

	assert.Empty(t, got)
}

func TestResolveESAssetPath_ExternalRootEscapeRejected(t *testing.T) {
	t.Parallel()
	romRoot := t.TempDir()
	assetRoot := t.TempDir()
	outsideRoot := t.TempDir()
	escaped := filepath.Join(assetRoot, "..", filepath.Base(outsideRoot), "art.png")

	got := resolveESAssetPath(escaped, romRoot, []string{assetRoot})

	assert.Empty(t, got)
}

func TestMapToDB_ExternalAssetRootImage(t *testing.T) {
	t.Parallel()
	romRoot := t.TempDir()
	assetRoot := t.TempDir()
	assetPath := filepath.Join(assetRoot, "art", "game.png")

	rec := GamelistRecord{
		SystemRootPath: romRoot,
		Game:           esapi.Game{Image: assetPath},
	}
	mediaProps := (&GamelistXMLScraper{externalAssetRoots: []string{assetRoot}}).MapToDB(&rec).MediaProps
	propKey := string(tags.TagTypeProperty) + ":" + string(tags.TagPropertyImageImage)
	var found bool
	for _, p := range mediaProps {
		if p.TypeTag == propKey {
			found = true
			assert.Equal(t, filepath.ToSlash(assetPath), p.Text)
		}
	}
	assert.True(t, found, "external asset root image property missing")
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
	assert.Contains(t, titleTags, database.TagInfo{
		Type: string(tags.TagTypeGameFamily), Tag: "mario", Label: "Mario",
	})
}

func TestMapToDB_Manual(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	rec := GamelistRecord{
		SystemRootPath: root,
		Game:           esapi.Game{Manual: "./manuals/game.pdf"},
	}
	mediaProps := (&GamelistXMLScraper{}).MapToDB(&rec).MediaProps
	propKey := string(tags.TagTypeProperty) + ":" + string(tags.TagPropertyManual)
	var found bool
	for _, p := range mediaProps {
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
	for _, p := range (&GamelistXMLScraper{}).MapToDB(&rec).MediaProps {
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
	for _, p := range (&GamelistXMLScraper{}).MapToDB(&rec).MediaProps {
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
	for _, p := range (&GamelistXMLScraper{}).MapToDB(&rec).MediaProps {
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
	for _, p := range (&GamelistXMLScraper{}).MapToDB(&rec).MediaProps {
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
	for _, p := range (&GamelistXMLScraper{}).MapToDB(&rec).MediaProps {
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
	for _, p := range (&GamelistXMLScraper{}).MapToDB(&rec).MediaProps {
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
	for _, p := range (&GamelistXMLScraper{}).MapToDB(&rec).MediaProps {
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
	for _, p := range (&GamelistXMLScraper{}).MapToDB(&rec).MediaProps {
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

func companionXMLGameIDProps(id string) []database.MediaProperty {
	return []database.MediaProperty{
		{
			TypeTag:     string(tags.TagTypeProperty) + ":" + string(tags.TagPropertyXMLGameID),
			Text:        id,
			ContentType: "text/plain",
		},
	}
}

func companionWriteMatcher(
	mediaTags []database.TagInfo,
	titleTags []database.TagInfo,
	titleProps []database.MediaProperty,
) any {
	return mock.MatchedBy(func(w *database.ScrapeWrite) bool {
		return w != nil &&
			w.Sentinel == scraper.SentinelTagInfo("gamelist.xml") &&
			assert.ObjectsAreEqual(mediaTags, w.MediaTags) &&
			assert.ObjectsAreEqual(titleTags, w.TitleTags) &&
			assert.ObjectsAreEqual(titleProps, w.TitleProps)
	})
}

func companionArtworkWriteMatcher(t *testing.T, expected map[string]string) any {
	t.Helper()
	return mock.MatchedBy(func(w *database.ScrapeWrite) bool {
		if w == nil || w.Sentinel != scraper.SentinelTagInfo("gamelist.xml") {
			return false
		}
		got := make(map[string]string, len(w.TitleProps))
		for _, p := range w.TitleProps {
			got[p.TypeTag] = p.Text
		}
		for key, path := range expected {
			if !assert.Equal(t, filepath.ToSlash(path), got[key], "companion artwork %s", key) {
				return false
			}
		}
		return true
	})
}

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
	stats := s.processCompanionEntries(
		context.Background(), scraper.ScrapeOptions{}, system, mockDB, loadRecordIndexes{}, nil,
	)
	assert.Equal(t, companionStats{}, stats)
	mockDB.AssertExpectations(t)
}

func TestProcessCompanionEntries_ChildByExactPath(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(companionXML), 0o600))

	resolvedPath := filepath.ToSlash(filepath.Join(root, "child.rom"))
	childTags := []database.TagInfo{
		{Type: string(tags.TagTypeRegion), Tag: "usa"},
		{Type: string(tags.TagTypeLang), Tag: "en"},
	}
	titleTags := []database.TagInfo{{Type: string(tags.TagTypeDeveloper), Tag: "dev-corp", Label: "Dev Corp"}}
	mockDB := helpers.NewMockMediaDBI()
	mockDB.On("ApplyScrapeResult", mock.Anything, int64(10), int64(20),
		companionWriteMatcher(childTags, titleTags, companionXMLGameIDProps("42"))).Return(nil)

	s := &GamelistXMLScraper{db: mockDB}
	system := scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}, DBID: 1}
	indexes := mediaByPath(database.Media{DBID: 10, MediaTitleDBID: 20, Path: resolvedPath})
	stats := s.processCompanionEntries(context.Background(), scraper.ScrapeOptions{}, system, mockDB, indexes, nil)
	assertCompanionCounts(t, &stats, 1, 1, 0)
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

	mockDB := helpers.NewMockMediaDBI()
	mockDB.On("ApplyScrapeResult", mock.Anything, int64(40), int64(30),
		companionWriteMatcher(
			nil,
			[]database.TagInfo{{Type: string(tags.TagTypeDeveloper), Tag: "dev", Label: "Dev"}},
			companionXMLGameIDProps("99"),
		)).Return(nil)

	s := &GamelistXMLScraper{db: mockDB}
	system := scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}, DBID: 5}
	indexes := mediaByPath(database.Media{DBID: 40, MediaTitleDBID: 30, Path: filepath.Join(root, "myslug.nes")})
	indexes.AllTitlesBySlug["myslug"] = database.MediaTitle{DBID: 30, SystemDBID: 5, Slug: "myslug"}
	stats := s.processCompanionEntries(context.Background(), scraper.ScrapeOptions{}, system, mockDB, indexes, nil)
	assertCompanionCounts(t, &stats, 1, 1, 0)
	mockDB.AssertExpectations(t)
}

func TestProcessCompanionEntries_ChildBySlugFileWritesAllTitleMedia(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`<gameList>
  <game id="99" source="ZaparooCompanion">
    <name>Slug Game</name>
    <developer>Dev</developer>
  </game>
  <game parentid="99" source="ZaparooCompanion">
    <path>./myslug.slug</path>
    <region>usa</region>
    <lang>en</lang>
  </game>
</gameList>`), 0o600))

	mockDB := &batchMockMediaDB{MockMediaDBI: helpers.NewMockMediaDBI()}
	s := &GamelistXMLScraper{db: mockDB}
	system := scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}, DBID: 5}
	indexes := mediaByPath(
		database.Media{DBID: 40, MediaTitleDBID: 30, Path: filepath.Join(root, "myslug-1.nes")},
		database.Media{DBID: 41, MediaTitleDBID: 30, Path: filepath.Join(root, "myslug-2.nes")},
		database.Media{DBID: 42, MediaTitleDBID: 30, Path: filepath.Join(root, "myslug-3.nes")},
	)
	indexes.AllTitlesBySlug["myslug"] = database.MediaTitle{DBID: 30, SystemDBID: 5, Slug: "myslug"}

	stats := s.processCompanionEntries(context.Background(), scraper.ScrapeOptions{}, system, mockDB, indexes, nil)

	assertCompanionCounts(t, &stats, 1, 3, 0)
	require.Len(t, mockDB.batches, 1)
	require.Len(t, mockDB.batches[0], 3)
	assert.ElementsMatch(t, []int64{40, 41, 42}, []int64{
		mockDB.batches[0][0].MediaDBID,
		mockDB.batches[0][1].MediaDBID,
		mockDB.batches[0][2].MediaDBID,
	})
	for i, target := range mockDB.batches[0] {
		require.NotNil(t, target.Write)
		assert.Equal(t, scraper.SentinelTagInfo("gamelist.xml"), target.Write.Sentinel)
		assert.Empty(t, target.Write.MediaTags, "slug child should not write file-level tags for target %d", i)
	}
	assert.Equal(t,
		[]database.TagInfo{{Type: string(tags.TagTypeDeveloper), Tag: "dev", Label: "Dev"}},
		mockDB.batches[0][0].Write.TitleTags,
	)
	assert.Equal(t, companionXMLGameIDProps("99"), mockDB.batches[0][0].Write.TitleProps)
	assert.Empty(t, mockDB.batches[0][1].Write.TitleTags)
	assert.Empty(t, mockDB.batches[0][1].Write.TitleProps)
	assert.Empty(t, mockDB.batches[0][2].Write.TitleTags)
	assert.Empty(t, mockDB.batches[0][2].Write.TitleProps)
}

func TestProcessCompanionEntries_DuplicateSlugChildConsumesTitleMedia(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`<gameList>
  <game id="99" source="ZaparooCompanion">
    <name>Slug Game</name>
    <developer>Dev</developer>
  </game>
  <game parentid="99" source="ZaparooCompanion"><path>./myslug.slug</path></game>
  <game parentid="99" source="ZaparooCompanion"><path>./myslug.slug</path></game>
</gameList>`), 0o600))

	mockDB := &batchMockMediaDB{MockMediaDBI: helpers.NewMockMediaDBI()}
	s := &GamelistXMLScraper{db: mockDB}
	system := scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}, DBID: 5}
	indexes := mediaByPath(
		database.Media{DBID: 40, MediaTitleDBID: 30, Path: filepath.Join(root, "myslug-1.nes")},
		database.Media{DBID: 41, MediaTitleDBID: 30, Path: filepath.Join(root, "myslug-2.nes")},
	)
	indexes.AllTitlesBySlug["myslug"] = database.MediaTitle{DBID: 30, SystemDBID: 5, Slug: "myslug"}

	stats := s.processCompanionEntries(context.Background(), scraper.ScrapeOptions{}, system, mockDB, indexes, nil)

	assertCompanionCounts(t, &stats, 2, 2, 1)
	assert.Equal(t, 1, stats.MissingTitleMedia)
	require.Len(t, mockDB.batches, 1)
	require.Len(t, mockDB.batches[0], 2)
	assert.ElementsMatch(t, []int64{40, 41}, []int64{
		mockDB.batches[0][0].MediaDBID,
		mockDB.batches[0][1].MediaDBID,
	})
}

func TestProcessCompanionEntries_RewritesAlreadyScraped(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(companionXML), 0o600))

	resolvedPath := filepath.ToSlash(filepath.Join(root, "child.rom"))
	mockDB := helpers.NewMockMediaDBI()
	mockDB.On("ApplyScrapeResult", mock.Anything, int64(10), int64(20), mock.Anything).Return(nil)

	s := &GamelistXMLScraper{db: mockDB}
	system := scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}, DBID: 1}
	indexes := mediaByPath(database.Media{DBID: 10, MediaTitleDBID: 20, Path: resolvedPath})
	stats := s.processCompanionEntries(context.Background(), scraper.ScrapeOptions{}, system, mockDB, indexes, nil)
	assertCompanionCounts(t, &stats, 1, 1, 0)
	mockDB.AssertExpectations(t)
}

func TestProcessCompanionEntries_ForceRewritesAlreadyScraped(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(companionXML), 0o600))

	const runID = "companion-run"
	resolvedPath := filepath.ToSlash(filepath.Join(root, "child.rom"))
	runMarkerMatcher := mock.MatchedBy(func(w *database.ScrapeWrite) bool {
		return w != nil && assert.Contains(t, w.MediaTags, database.TagInfo{
			Type: string(tags.ScraperRunType("gamelist.xml")),
			Tag:  runID,
		})
	})
	mockDB := helpers.NewMockMediaDBI()
	mockDB.On("ApplyScrapeResult", mock.Anything, int64(10), int64(20), runMarkerMatcher).Return(nil)

	s := &GamelistXMLScraper{db: mockDB}
	system := scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}, DBID: 1}
	indexes := mediaByPath(database.Media{DBID: 10, MediaTitleDBID: 20, Path: resolvedPath})
	stats := s.processCompanionEntries(
		context.Background(), scraper.ScrapeOptions{RunID: runID, Force: true}, system, mockDB, indexes, nil,
	)
	assertCompanionCounts(t, &stats, 1, 1, 0)
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

	s := &GamelistXMLScraper{db: mockDB}
	system := scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}, DBID: 5}
	stats := s.processCompanionEntries(
		context.Background(), scraper.ScrapeOptions{}, system, mockDB, mediaByPath(), nil,
	)
	assert.Equal(t, 1, stats.Processed)
	assert.Equal(t, 1, stats.Skipped)
	assert.Equal(t, 1, stats.MissingTitleSlugs)
	mockDB.AssertExpectations(t)
}

func TestProcessCompanionEntries_ParentNotFoundForChild(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`<gameList>
  <game parentid="99" source="ZaparooCompanion">
    <path>./child.rom</path>
  </game>
</gameList>`), 0o600))

	mockDB := helpers.NewMockMediaDBI()
	s := &GamelistXMLScraper{db: mockDB}
	system := scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}, DBID: 1}
	stats := s.processCompanionEntries(
		context.Background(), scraper.ScrapeOptions{}, system, mockDB, mediaByPath(), nil,
	)
	assertCompanionCounts(t, &stats, 1, 0, 1)
	mockDB.AssertExpectations(t)
}

func TestProcessCompanionEntries_MapsIssue161CompanionArtwork(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`<gameList>
  <game id="161" source="ZaparooCompanion">
    <name>Doom</name>
    <screenshot>./media/screenshot/Doom.png</screenshot>
    <titlescreen>./media/titlescreen/Doom.png</titlescreen>
    <boxart2d>./media/box2d/Doom.png</boxart2d>
    <boxart3d>./media/box3d/Doom.png</boxart3d>
    <logo>./media/logo/Doom.png</logo>
  </game>
  <game parentid="161" source="ZaparooCompanion">
    <path>./Doom.rom</path>
  </game>
</gameList>`), 0o600))

	childPath := filepath.ToSlash(filepath.Join(root, "Doom.rom"))
	propPrefix := string(tags.TagTypeProperty) + ":"
	expectedArtwork := map[string]string{
		propPrefix + string(tags.TagPropertyImageScreenshot): filepath.Join(root, "media", "screenshot", "Doom.png"),
		propPrefix + string(tags.TagPropertyImageTitleshot):  filepath.Join(root, "media", "titlescreen", "Doom.png"),
		propPrefix + string(tags.TagPropertyImageBoxart):     filepath.Join(root, "media", "box2d", "Doom.png"),
		propPrefix + string(tags.TagPropertyImageBoxart3D):   filepath.Join(root, "media", "box3d", "Doom.png"),
		propPrefix + string(tags.TagPropertyImageWheel):      filepath.Join(root, "media", "logo", "Doom.png"),
	}
	mockDB := helpers.NewMockMediaDBI()
	mockDB.On(
		"ApplyScrapeResult", mock.Anything, int64(10), int64(20), companionArtworkWriteMatcher(t, expectedArtwork),
	).Return(nil)

	s := &GamelistXMLScraper{db: mockDB}
	system := scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}, DBID: 1}
	indexes := mediaByPath(database.Media{DBID: 10, MediaTitleDBID: 20, Path: childPath})
	stats := s.processCompanionEntries(context.Background(), scraper.ScrapeOptions{}, system, mockDB, indexes, nil)
	assertCompanionCounts(t, &stats, 1, 1, 0)
	mockDB.AssertExpectations(t)
}

func TestProcessCompanionEntries_ExternalCompanionArtwork(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	assetRoot := t.TempDir()
	assetPath := filepath.Join(assetRoot, "covers", "Doom.png")
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`<gameList>
  <game id="854" source="ZaparooCompanion">
    <name>Doom</name>
    <image>`+assetPath+`</image>
  </game>
  <game parentid="854" source="ZaparooCompanion">
    <path>./Doom.rom</path>
  </game>
</gameList>`), 0o600))

	childPath := filepath.ToSlash(filepath.Join(root, "Doom.rom"))
	propKey := string(tags.TagTypeProperty) + ":" + string(tags.TagPropertyImageImage)
	mockDB := helpers.NewMockMediaDBI()
	mockDB.On(
		"ApplyScrapeResult", mock.Anything, int64(10), int64(20),
		companionArtworkWriteMatcher(t, map[string]string{propKey: assetPath}),
	).Return(nil)

	s := &GamelistXMLScraper{db: mockDB, externalAssetRoots: []string{assetRoot}}
	system := scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}, DBID: 1}
	indexes := mediaByPath(database.Media{DBID: 10, MediaTitleDBID: 20, Path: childPath})
	stats := s.processCompanionEntries(context.Background(), scraper.ScrapeOptions{}, system, mockDB, indexes, nil)
	assertCompanionCounts(t, &stats, 1, 1, 0)
	mockDB.AssertExpectations(t)
}

func TestProcessCompanionEntries_StatsAggregateWithNormalProgress(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`<gameList>
  <game id="42" source="ZaparooCompanion">
    <name>Companion Game</name>
    <developer>Dev Corp</developer>
  </game>
  <game parentid="42" source="ZaparooCompanion">
    <path>./companion.rom</path>
    <region>usa</region>
    <lang>en</lang>
  </game>
  <game><path>./normal.rom</path><name>Normal Game</name></game>
</gameList>`), 0o600))

	const (
		companionTitleDBID = int64(20)
		companionMediaDBID = int64(10)
		normalTitleDBID    = int64(21)
		normalMediaDBID    = int64(11)
		systemDBID         = int64(1)
	)
	companionPath := filepath.ToSlash(filepath.Join(root, "companion.rom"))
	mockDB := helpers.NewMockMediaDBI()
	mockDB.On("FindMediaTitlesWithoutSentinel", mock.Anything, systemDBID, "scraper.gamelist.xml:scraped").
		Return([]database.MediaTitle{{
			DBID: normalTitleDBID, SystemDBID: systemDBID, Slug: "normal-game", Name: "Normal Game",
		}}, nil)
	mockDB.On("GetTitlesBySystemID", "nes").Return([]database.TitleWithSystem{{
		DBID: normalTitleDBID, SystemDBID: systemDBID, Slug: "normal-game", Name: "Normal Game",
	}}, nil)
	mockDB.On("GetMediaBySystemID", "nes").Return([]database.MediaWithFullPath{
		{DBID: companionMediaDBID, MediaTitleDBID: companionTitleDBID, Path: companionPath},
		{DBID: normalMediaDBID, MediaTitleDBID: normalTitleDBID, Path: filepath.Join(root, "normal.rom")},
	}, nil)
	mockDB.On("ApplyScrapeResult", mock.Anything, companionMediaDBID, companionTitleDBID, mock.Anything).Return(nil)
	mockDB.On("GetScrapedMediaIDs", mock.Anything, "gamelist.xml", systemDBID).
		Return(map[int64]struct{}{}, nil)
	mockDB.On("ApplyScrapeResult", mock.Anything, normalMediaDBID, normalTitleDBID, mock.Anything).Return(nil)

	s := &GamelistXMLScraper{db: mockDB}
	system := scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}, DBID: systemDBID}
	ch := make(chan scraper.ScrapeUpdate, 128)

	s.scrapeLoop(context.Background(), scraper.ScrapeOptions{Pauser: syncutil.NewPauser()},
		[]scraper.ScrapeSystem{system}, mockDB, ch)

	updates := drainChannel(ch)
	var progress, done scraper.ScrapeUpdate
	for _, u := range updates {
		if u.Done {
			done = u
			continue
		}
		if u.Processed == 2 && u.Matched == 2 {
			progress = u
		}
	}
	require.Equal(t, 2, progress.Processed, "normal progress should include companion counts")
	assert.Equal(t, 2, progress.Total)
	assert.Equal(t, 2, progress.Matched)
	assert.Equal(t, 2, done.Processed)
	assert.Equal(t, 2, done.Matched)
	mockDB.AssertExpectations(t)
}

func TestProcessCompanionEntries_EachChildUsesAtomicScrapeWrite(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
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

	child1Path := filepath.ToSlash(filepath.Join(root, "child1.rom"))
	child2Path := filepath.ToSlash(filepath.Join(root, "child2.rom"))
	mockDB := helpers.NewMockMediaDBI()
	mockDB.On("ApplyScrapeResult", mock.Anything, int64(10), int64(20), mock.Anything).Return(nil)
	mockDB.On("ApplyScrapeResult", mock.Anything, int64(11), int64(20), mock.Anything).Return(nil)

	s := &GamelistXMLScraper{db: mockDB}
	system := scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}, DBID: 1}
	indexes := mediaByPath(
		database.Media{DBID: 10, MediaTitleDBID: 20, Path: child1Path},
		database.Media{DBID: 11, MediaTitleDBID: 20, Path: child2Path},
	)
	stats := s.processCompanionEntries(context.Background(), scraper.ScrapeOptions{}, system, mockDB, indexes, nil)
	assertCompanionCounts(t, &stats, 2, 2, 0)
	mockDB.AssertExpectations(t)
}

func TestProcessCompanionEntries_UsesBatchWritesWhenAvailable(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`<gameList>
  <game id="42" source="ZaparooCompanion"><name>Game</name><developer>Dev</developer></game>
  <game parentid="42" source="ZaparooCompanion"><path>./child1.rom</path></game>
  <game parentid="42" source="ZaparooCompanion"><path>./child2.rom</path></game>
</gameList>`), 0o600))

	mockDB := &batchMockMediaDB{MockMediaDBI: helpers.NewMockMediaDBI()}
	s := &GamelistXMLScraper{db: mockDB}
	system := scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}, DBID: 1}
	indexes := mediaByPath(
		database.Media{DBID: 10, MediaTitleDBID: 20, Path: filepath.Join(root, "child1.rom")},
		database.Media{DBID: 11, MediaTitleDBID: 21, Path: filepath.Join(root, "child2.rom")},
	)

	stats := s.processCompanionEntries(context.Background(), scraper.ScrapeOptions{}, system, mockDB, indexes, nil)

	assertCompanionCounts(t, &stats, 2, 2, 0)
	require.Len(t, mockDB.batches, 1)
	assert.Len(t, mockDB.batches[0], 2)
	assert.Equal(t, 1, stats.WriteStats.Batches)
	mockDB.AssertNotCalled(t, "ApplyScrapeResult", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestProcessCompanionEntries_DeduplicatesIdenticalTitleWrites(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`<gameList>
  <game id="42" source="ZaparooCompanion"><name>Game</name><developer>Dev</developer></game>
  <game parentid="42" source="ZaparooCompanion"><path>./child1.rom</path></game>
  <game parentid="42" source="ZaparooCompanion"><path>./child2.rom</path></game>
</gameList>`), 0o600))

	mockDB := &batchMockMediaDB{MockMediaDBI: helpers.NewMockMediaDBI()}
	s := &GamelistXMLScraper{db: mockDB}
	system := scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}, DBID: 1}
	indexes := mediaByPath(
		database.Media{DBID: 10, MediaTitleDBID: 20, Path: filepath.Join(root, "child1.rom")},
		database.Media{DBID: 11, MediaTitleDBID: 20, Path: filepath.Join(root, "child2.rom")},
	)

	stats := s.processCompanionEntries(context.Background(), scraper.ScrapeOptions{}, system, mockDB, indexes, nil)

	assertCompanionCounts(t, &stats, 2, 2, 0)
	assert.Equal(t, 1, stats.UniqueTitleWrites)
	assert.Equal(t, 1, stats.DuplicateTitles)
	assert.Equal(t, 0, stats.ConflictingTitleWrites)
	require.Len(t, mockDB.batches, 1)
	require.Len(t, mockDB.batches[0], 2)
	assert.NotEmpty(t, mockDB.batches[0][0].Write.TitleTags)
	assert.Empty(t, mockDB.batches[0][1].Write.TitleTags)
	assert.Empty(t, mockDB.batches[0][1].Write.TitleProps)
}

func TestProcessCompanionEntries_PreservesConflictingTitleWrites(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`<gameList>
  <game id="1" source="ZaparooCompanion"><name>Game One</name><developer>Dev One</developer></game>
  <game id="2" source="ZaparooCompanion"><name>Game Two</name><developer>Dev Two</developer></game>
  <game parentid="1" source="ZaparooCompanion"><path>./child1.rom</path></game>
  <game parentid="2" source="ZaparooCompanion"><path>./child2.rom</path></game>
</gameList>`), 0o600))

	mockDB := &batchMockMediaDB{MockMediaDBI: helpers.NewMockMediaDBI()}
	s := &GamelistXMLScraper{db: mockDB}
	system := scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}, DBID: 1}
	indexes := mediaByPath(
		database.Media{DBID: 10, MediaTitleDBID: 20, Path: filepath.Join(root, "child1.rom")},
		database.Media{DBID: 11, MediaTitleDBID: 20, Path: filepath.Join(root, "child2.rom")},
	)

	stats := s.processCompanionEntries(context.Background(), scraper.ScrapeOptions{}, system, mockDB, indexes, nil)

	assertCompanionCounts(t, &stats, 2, 2, 0)
	assert.Equal(t, 1, stats.UniqueTitleWrites)
	assert.Equal(t, 0, stats.DuplicateTitles)
	assert.Equal(t, 1, stats.ConflictingTitleWrites)
	require.Len(t, mockDB.batches, 1)
	require.Len(t, mockDB.batches[0], 2)
	assert.NotEmpty(t, mockDB.batches[0][0].Write.TitleTags)
	assert.NotEmpty(t, mockDB.batches[0][1].Write.TitleTags)
}

func TestProcessCompanionEntries_ThrottlesBatchProgress(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	var xml strings.Builder
	_, _ = xml.WriteString(`<gameList><game id="42" source="ZaparooCompanion"><name>Game</name></game>`)
	mediaRows := make([]database.Media, 0, companionWriteBatchSize+1)
	for i := range companionWriteBatchSize + 1 {
		name := "child" + strconv.Itoa(i) + ".rom"
		_, _ = xml.WriteString(`<game parentid="42" source="ZaparooCompanion"><path>./` + name + `</path></game>`)
		mediaRows = append(mediaRows, database.Media{
			DBID: int64(i + 1), MediaTitleDBID: int64(i + 100), Path: filepath.Join(root, name),
		})
	}
	_, _ = xml.WriteString(`</gameList>`)
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(xml.String()), 0o600))

	mockDB := &batchMockMediaDB{MockMediaDBI: helpers.NewMockMediaDBI()}
	s := &GamelistXMLScraper{db: mockDB}
	system := scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}, DBID: 1}
	ch := make(chan scraper.ScrapeUpdate, 128)

	stats := s.processCompanionEntries(
		context.Background(), scraper.ScrapeOptions{}, system, mockDB, mediaByPath(mediaRows...), ch,
	)

	assertCompanionCounts(t, &stats, companionWriteBatchSize+1, companionWriteBatchSize+1, 0)
	updates := drainBufferedUpdates(ch)
	assert.NotContains(t, updates, scraper.ScrapeUpdate{
		SystemID: "nes", Total: companionWriteBatchSize + 1, Processed: companionWriteBatchSize,
	})
	assert.Contains(t, updates, scraper.ScrapeUpdate{
		SystemID: "nes", Total: companionWriteBatchSize + 1,
		Processed: companionWriteBatchSize + 1, Matched: companionWriteBatchSize + 1,
	})
}

func TestProcessCompanionEntries_BatchFailureFallsBackToPerTargetWrites(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`<gameList>
  <game id="42" source="ZaparooCompanion"><name>Game</name><developer>Dev</developer></game>
  <game parentid="42" source="ZaparooCompanion"><path>./child1.rom</path></game>
  <game parentid="42" source="ZaparooCompanion"><path>./child2.rom</path></game>
</gameList>`), 0o600))

	mockDB := &batchMockMediaDB{MockMediaDBI: helpers.NewMockMediaDBI(), batchErr: assert.AnError}
	mockDB.On("ApplyScrapeResult", mock.Anything, int64(10), int64(20), mock.Anything).Return(nil)
	mockDB.On("ApplyScrapeResult", mock.Anything, int64(11), int64(21), mock.Anything).Return(assert.AnError)

	s := &GamelistXMLScraper{db: mockDB}
	system := scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}, DBID: 1}
	indexes := mediaByPath(
		database.Media{DBID: 10, MediaTitleDBID: 20, Path: filepath.Join(root, "child1.rom")},
		database.Media{DBID: 11, MediaTitleDBID: 21, Path: filepath.Join(root, "child2.rom")},
	)

	stats := s.processCompanionEntries(context.Background(), scraper.ScrapeOptions{}, system, mockDB, indexes, nil)

	assertCompanionCounts(t, &stats, 2, 1, 1)
	require.Len(t, mockDB.batches, 1)
	assert.Equal(t, 1, stats.WriteStats.BatchFallbacks)
	mockDB.AssertExpectations(t)
}

func TestProcessCompanionEntries_NoRegionLangStillWritesSentinel(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`<gameList>
  <game id="1" source="ZaparooCompanion">
    <name>Game</name>
    <developer>Dev</developer>
  </game>
  <game parentid="1" source="ZaparooCompanion">
    <path>./game.rom</path>
  </game>
</gameList>`), 0o600))

	gamePath := filepath.ToSlash(filepath.Join(root, "game.rom"))
	mockDB := helpers.NewMockMediaDBI()
	mockDB.On("ApplyScrapeResult", mock.Anything, int64(5), int64(6),
		companionWriteMatcher(
			nil,
			[]database.TagInfo{{Type: string(tags.TagTypeDeveloper), Tag: "dev", Label: "Dev"}},
			companionXMLGameIDProps("1"),
		)).Return(nil)

	s := &GamelistXMLScraper{db: mockDB}
	system := scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}, DBID: 1}
	indexes := mediaByPath(database.Media{DBID: 5, MediaTitleDBID: 6, Path: gamePath})
	stats := s.processCompanionEntries(context.Background(), scraper.ScrapeOptions{}, system, mockDB, indexes, nil)
	assertCompanionCounts(t, &stats, 1, 1, 0)
	mockDB.AssertExpectations(t)
}

func TestProcessCompanionEntries_AmbiguousSuffixMatchSkipped(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(companionXML), 0o600))

	resolvedPath := filepath.ToSlash(filepath.Join(root, "child.rom"))
	mockDB := helpers.NewMockMediaDBI()

	s := &GamelistXMLScraper{db: mockDB}
	system := scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}, DBID: 1}
	indexes := mediaByPath(
		database.Media{DBID: 10, MediaTitleDBID: 20, Path: filepath.Join(root, "one", "child.rom")},
		database.Media{DBID: 11, MediaTitleDBID: 21, Path: filepath.Join(root, "two", "child.rom")},
	)
	delete(indexes.MediaByPathFold, pathFoldKey(resolvedPath))
	stats := s.processCompanionEntries(context.Background(), scraper.ScrapeOptions{}, system, mockDB, indexes, nil)
	assert.Equal(t, 1, stats.Processed)
	assert.Equal(t, 1, stats.Skipped)
	assert.Equal(t, 1, stats.AmbiguousFilenames)
	mockDB.AssertExpectations(t)
}

func TestProcessCompanionEntries_FilenameNotIndexed(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(companionXML), 0o600))

	mockDB := helpers.NewMockMediaDBI()

	s := &GamelistXMLScraper{db: mockDB}
	system := scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}, DBID: 1}
	stats := s.processCompanionEntries(
		context.Background(), scraper.ScrapeOptions{}, system, mockDB, mediaByPath(), nil,
	)
	assert.Equal(t, 1, stats.Processed)
	assert.Equal(t, 1, stats.Skipped)
	assert.Equal(t, 1, stats.UnmatchedFilenames)
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

func drainBufferedUpdates(ch chan scraper.ScrapeUpdate) []scraper.ScrapeUpdate {
	var updates []scraper.ScrapeUpdate
	for {
		select {
		case update, ok := <-ch:
			if !ok {
				return updates
			}
			updates = append(updates, update)
		default:
			return updates
		}
	}
}

func TestScrapeLoop_ProgressIsPerSystem(t *testing.T) {
	t.Parallel()

	root1 := t.TempDir()
	root2 := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root1, "gamelist.xml"), []byte(`
<gameList>
  <game><path>./first.nes</path><name>First</name></game>
</gameList>`), 0o600))
	games := []string{"one", "two", "three"}
	var builder strings.Builder
	_, _ = builder.WriteString("<gameList>\n")
	for _, name := range games {
		_, _ = builder.WriteString(
			"  <game><path>./" + name + ".sfc</path><name>" + name + "</name></game>\n",
		)
	}
	_, _ = builder.WriteString("</gameList>")
	require.NoError(t, os.WriteFile(filepath.Join(root2, "gamelist.xml"), []byte(builder.String()), 0o600))

	mockDB := helpers.NewMockMediaDBI()
	mockDB.On("GetTitlesBySystemID", "nes").Return([]database.TitleWithSystem{{
		DBID: 1, SystemDBID: 10, Slug: "first", Name: "First",
	}}, nil)
	mockDB.On("GetMediaBySystemID", "nes").Return([]database.MediaWithFullPath{{
		DBID: 11, MediaTitleDBID: 1, Path: filepath.Join(root1, "first.nes"),
	}}, nil)
	mockDB.On("GetTitlesBySystemID", "snes").Return([]database.TitleWithSystem{
		{DBID: 2, SystemDBID: 20, Slug: "one", Name: "one"},
		{DBID: 3, SystemDBID: 20, Slug: "two", Name: "two"},
		{DBID: 4, SystemDBID: 20, Slug: "three", Name: "three"},
	}, nil)
	mockDB.On("GetMediaBySystemID", "snes").Return([]database.MediaWithFullPath{
		{DBID: 12, MediaTitleDBID: 2, Path: filepath.Join(root2, "one.sfc")},
		{DBID: 13, MediaTitleDBID: 3, Path: filepath.Join(root2, "two.sfc")},
		{DBID: 14, MediaTitleDBID: 4, Path: filepath.Join(root2, "three.sfc")},
	}, nil)
	mockDB.On(
		"ApplyScrapeResult", mock.Anything, mock.AnythingOfType("int64"), mock.AnythingOfType("int64"), mock.Anything,
	).Return(nil)

	s := &GamelistXMLScraper{db: mockDB}
	ch := make(chan scraper.ScrapeUpdate, 128)
	s.scrapeLoop(context.Background(), scraper.ScrapeOptions{
		Pauser: syncutil.NewPauser(),
		Force:  true,
	}, []scraper.ScrapeSystem{
		{ID: "nes", ROMPaths: []string{root1}, DBID: 10},
		{ID: "snes", ROMPaths: []string{root2}, DBID: 20},
	}, mockDB, ch)

	updates := drainChannel(ch)
	var lastSNES scraper.ScrapeUpdate
	var done scraper.ScrapeUpdate
	for _, update := range updates {
		if update.SystemID == "snes" {
			lastSNES = update
		}
		if update.Done {
			done = update
		}
	}

	assert.Equal(t, 3, lastSNES.Total)
	assert.Equal(t, 3, lastSNES.Processed)
	assert.Equal(t, 3, lastSNES.Matched)
	assert.Equal(t, 0, lastSNES.Skipped)
	require.True(t, done.Done)
	assert.Equal(t, 4, done.Processed)
	assert.Equal(t, 4, done.Matched)
	mockDB.AssertExpectations(t)
}

func TestScrapeLoop_PauseCancelBeforeNextSystemPreservesProgress(t *testing.T) {
	t.Parallel()

	root1 := t.TempDir()
	root2 := t.TempDir()
	recordPath := filepath.Join(root1, "first.nes")
	require.NoError(t, os.WriteFile(filepath.Join(root1, "gamelist.xml"), []byte(`
<gameList>
  <game><path>./first.nes</path><name>First</name></game>
</gameList>`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root2, "gamelist.xml"), []byte(`
<gameList>
  <game><path>./second.sfc</path><name>Second</name></game>
</gameList>`), 0o600))

	pauser := syncutil.NewPauser()
	paused := make(chan struct{})
	mockDB := helpers.NewMockMediaDBI()
	mockDB.On("GetTitlesBySystemID", "nes").Return([]database.TitleWithSystem{{
		DBID: 1, SystemDBID: 10, Slug: "first", Name: "First",
	}}, nil)
	mockDB.On("GetMediaBySystemID", "nes").Return([]database.MediaWithFullPath{{
		DBID: 11, MediaTitleDBID: 1, Path: recordPath,
	}}, nil)
	mockDB.On("ApplyScrapeResult", mock.Anything, int64(11), int64(1), mock.Anything).
		Run(func(_ mock.Arguments) {
			pauser.Pause()
			close(paused)
		}).
		Return(nil)

	s := &GamelistXMLScraper{db: mockDB}
	ch := make(chan scraper.ScrapeUpdate, 128)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.scrapeLoop(ctx, scraper.ScrapeOptions{Pauser: pauser, Force: true}, []scraper.ScrapeSystem{
			{ID: "nes", ROMPaths: []string{root1}, DBID: 10},
			{ID: "snes", ROMPaths: []string{root2}, DBID: 20},
		}, mockDB, ch)
	}()

	<-paused
	cancel()
	<-done

	updates := drainChannel(ch)
	var doneUpdate scraper.ScrapeUpdate
	for _, update := range updates {
		if update.Done {
			doneUpdate = update
		}
	}
	require.True(t, doneUpdate.Done)
	assert.Equal(t, "snes", doneUpdate.SystemID)
	assert.Equal(t, 1, doneUpdate.Processed)
	assert.Equal(t, 1, doneUpdate.Matched)
	assert.Equal(t, 0, doneUpdate.Skipped)
	mockDB.AssertExpectations(t)
}

func TestScrapeLoop_ProgressIncludesCompanionBaseline(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	companionPath := filepath.Join(root, "companion.nes")
	normalPath := filepath.Join(root, "normal.nes")
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`
<gameList>
  <game id="100" source="ZaparooCompanion">
    <name>Companion Parent</name>
    <desc>Parent metadata</desc>
  </game>
  <game parentid="100" source="ZaparooCompanion">
    <path>./companion.nes</path>
  </game>
  <game><path>./normal.nes</path><name>Normal</name></game>
</gameList>`), 0o600))

	mockDB := helpers.NewMockMediaDBI()
	mockDB.On("GetMediaBySystemID", "nes").Return([]database.MediaWithFullPath{
		{DBID: 10, MediaTitleDBID: 1000, Path: companionPath},
		{DBID: 11, MediaTitleDBID: 1001, Path: normalPath},
	}, nil)
	mockDB.On("GetTitlesBySystemID", "nes").Return([]database.TitleWithSystem{{
		DBID: 1001, SystemDBID: 100, Slug: "normal", Name: "Normal",
	}}, nil)
	mockDB.On(
		"ApplyScrapeResult", mock.Anything, mock.AnythingOfType("int64"), mock.AnythingOfType("int64"), mock.Anything,
	).Return(nil)

	s := &GamelistXMLScraper{db: mockDB}
	ch := make(chan scraper.ScrapeUpdate, 128)
	s.scrapeLoop(context.Background(), scraper.ScrapeOptions{
		Pauser: syncutil.NewPauser(),
		Force:  true,
	}, []scraper.ScrapeSystem{{ID: "nes", ROMPaths: []string{root}, DBID: 100}}, mockDB, ch)

	updates := drainChannel(ch)
	var lastNES scraper.ScrapeUpdate
	for _, update := range updates {
		if update.SystemID == "nes" {
			lastNES = update
		}
	}

	assert.Equal(t, 2, lastNES.Total)
	assert.Equal(t, 2, lastNES.Processed)
	assert.Equal(t, 2, lastNES.Matched)
	assert.Equal(t, 0, lastNES.Skipped)
	mockDB.AssertExpectations(t)
}

func TestScrapeLoop_NormalMode_Success(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`
<gameList>
  <game><path>./mario.nes</path><name>Mario</name><region>usa, eur</region><lang>en</lang></game>
</gameList>`), 0o600))

	const (
		titleDBID  = int64(1)
		mediaDBID  = int64(10)
		systemDBID = int64(100)
	)

	mockDB := helpers.NewMockMediaDBI()
	mockDB.On("FindMediaTitlesWithoutSentinel", mock.Anything, systemDBID, "scraper.gamelist.xml:scraped").
		Return([]database.MediaTitle{{DBID: titleDBID, SystemDBID: systemDBID, Slug: "mario", Name: "Mario"}}, nil)
	mockDB.On("GetMediaBySystemID", "nes").
		Return([]database.MediaWithFullPath{{
			DBID: mediaDBID, MediaTitleDBID: titleDBID, Path: filepath.Join(root, "mario.nes"),
		}}, nil)
	mockDB.On("GetScrapedMediaIDs", mock.Anything, "gamelist.xml", systemDBID).
		Return(map[int64]struct{}{}, nil)
	mediaTagMatcher := mock.MatchedBy(func(w *database.ScrapeWrite) bool {
		return w != nil && assert.ElementsMatch(t, []database.TagInfo{
			{Type: string(tags.TagTypeRegion), Tag: "usa"},
			{Type: string(tags.TagTypeRegion), Tag: "eur"},
			{Type: string(tags.TagTypeLang), Tag: "en"},
		}, w.MediaTags)
	})
	mockDB.On("ApplyScrapeResult", mock.Anything, mediaDBID, titleDBID, mediaTagMatcher).Return(nil)

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

func TestScrapeLoop_ForceResumeSkipsCompletedRunMedia(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	firstPath := filepath.Join(root, "first.nes")
	secondPath := filepath.Join(root, "second.nes")
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`
<gameList>
  <game><path>./first.nes</path><name>First</name></game>
  <game><path>./second.nes</path><name>Second</name></game>
</gameList>`), 0o600))

	const (
		firstTitleDBID  = int64(1)
		firstMediaDBID  = int64(10)
		secondTitleDBID = int64(2)
		secondMediaDBID = int64(20)
		systemDBID      = int64(100)
		runID           = "resume-run"
	)

	writeMatcher := mock.MatchedBy(func(w *database.ScrapeWrite) bool {
		return w != nil && assert.Contains(t, w.MediaTags, database.TagInfo{
			Type: string(tags.ScraperRunType("gamelist.xml")),
			Tag:  runID,
		})
	})
	mockDB := helpers.NewMockMediaDBI()
	mockDB.On("GetTitlesBySystemID", "nes").Return([]database.TitleWithSystem{
		{DBID: firstTitleDBID, SystemDBID: systemDBID, Slug: "first", Name: "First"},
		{DBID: secondTitleDBID, SystemDBID: systemDBID, Slug: "second", Name: "Second"},
	}, nil)
	mockDB.On("GetMediaBySystemID", "nes").Return([]database.MediaWithFullPath{
		{DBID: firstMediaDBID, MediaTitleDBID: firstTitleDBID, Path: firstPath},
		{DBID: secondMediaDBID, MediaTitleDBID: secondTitleDBID, Path: secondPath},
	}, nil)
	mockDB.On("GetScrapeRunMediaIDs", mock.Anything, "gamelist.xml", runID, systemDBID).
		Return(map[int64]struct{}{firstMediaDBID: {}}, nil)
	mockDB.On("ApplyScrapeResult", mock.Anything, secondMediaDBID, secondTitleDBID, writeMatcher).Return(nil)

	s := &GamelistXMLScraper{db: mockDB}
	system := scraper.ScrapeSystem{ID: "nes", ROMPaths: []string{root}, DBID: systemDBID}
	ch := make(chan scraper.ScrapeUpdate, 128)

	s.scrapeLoop(context.Background(), scraper.ScrapeOptions{
		Pauser: syncutil.NewPauser(),
		RunID:  runID,
		Force:  true,
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

func TestScrapeLoop_Issue794ZipAsDirMedia(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`
<gameList>
  <game id="1658" source="ScreenScraper.fr">
    <path>./Japan/10-Yard Fight (Japan) (Rev 1).zip</path>
    <name>10-Yard Fight</name>
    <desc>Football game.</desc>
    <rating>0.4</rating>
    <releasedate>19850830T000000</releasedate>
    <developer>Irem</developer>
    <publisher>Nintendo</publisher>
    <genre>Sports / Football (American)-Sports</genre>
    <players>1-2</players>
    <image>./media/images/Japan/10-Yard Fight (Japan) (Rev 1).png</image>
    <thumbnail>./media/box2dfront/Japan/10-Yard Fight (Japan) (Rev 1).png</thumbnail>
  </game>
</gameList>`), 0o600))

	const (
		mediaDBID  = int64(7940)
		titleDBID  = int64(7941)
		systemDBID = int64(7942)
	)
	zipPath := filepath.Join(root, "Japan", "10-Yard Fight (Japan) (Rev 1).zip")
	innerPath := filepath.Join(zipPath, "10-Yard Fight (Japan) (Rev 1).nes")
	propPrefix := string(tags.TagTypeProperty) + ":"
	imageProp := propPrefix + string(tags.TagPropertyImageImage)
	thumbnailProp := propPrefix + string(tags.TagPropertyImageThumbnail)
	writeMatcher := mock.MatchedBy(func(w *database.ScrapeWrite) bool {
		if w == nil || w.Sentinel != scraper.SentinelTagInfo("gamelist.xml") {
			return false
		}
		got := make(map[string]string, len(w.MediaProps))
		for _, p := range w.MediaProps {
			got[p.TypeTag] = p.Text
		}
		return assert.Equal(t, filepath.ToSlash(filepath.Join(
			root, "media", "images", "Japan", "10-Yard Fight (Japan) (Rev 1).png",
		)), got[imageProp]) && assert.Equal(t, filepath.ToSlash(filepath.Join(
			root, "media", "box2dfront", "Japan", "10-Yard Fight (Japan) (Rev 1).png",
		)), got[thumbnailProp])
	})

	mockDB := helpers.NewMockMediaDBI()
	mockDB.On("FindMediaTitlesWithoutSentinel", mock.Anything, systemDBID, "scraper.gamelist.xml:scraped").
		Return([]database.MediaTitle{}, nil)
	mockDB.On("GetMediaBySystemID", "nes").
		Return([]database.MediaWithFullPath{{DBID: mediaDBID, MediaTitleDBID: titleDBID, Path: innerPath}}, nil)
	mockDB.On("GetScrapedMediaIDs", mock.Anything, "gamelist.xml", systemDBID).
		Return(map[int64]struct{}{}, nil)
	mockDB.On("ApplyScrapeResult", mock.Anything, mediaDBID, titleDBID, writeMatcher).Return(nil)

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

func TestScrapeLoop_SlugOnlyMatchWritesTitleMetadataOnly(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`
<gameList>
  <game>
    <path>./xml-path.nes</path>
    <name>Mario</name>
    <desc>Title metadata</desc>
    <region>usa</region>
    <image>./media/images/mario.png</image>
  </game>
</gameList>`), 0o600))

	const (
		titleDBID  = int64(2)
		mediaDBID  = int64(20)
		systemDBID = int64(200)
	)

	mockDB := helpers.NewMockMediaDBI()
	mockDB.On("FindMediaTitlesWithoutSentinel", mock.Anything, systemDBID, "scraper.gamelist.xml:scraped").
		Return([]database.MediaTitle{{DBID: titleDBID, SystemDBID: systemDBID, Slug: "mario", Name: "Mario"}}, nil)
	mockDB.On("GetMediaBySystemID", "nes").
		Return([]database.MediaWithFullPath{{
			DBID: mediaDBID, MediaTitleDBID: titleDBID, Path: filepath.Join(root, "indexed-path.nes"),
		}}, nil)
	mockDB.On("GetScrapedMediaIDs", mock.Anything, "gamelist.xml", systemDBID).
		Return(map[int64]struct{}{}, nil)
	writeMatcher := mock.MatchedBy(func(w *database.ScrapeWrite) bool {
		if w == nil {
			return false
		}
		descTypeTag := string(tags.TagTypeProperty) + ":" + string(tags.TagPropertyDescription)
		return assert.Empty(t, w.MediaTags) &&
			assert.Empty(t, w.MediaProps) &&
			assert.Contains(t, w.TitleProps, database.MediaProperty{
				TypeTag:     descTypeTag,
				Text:        "Title metadata",
				ContentType: "text/plain",
			})
	})
	mockDB.On("ApplyScrapeResult", mock.Anything, mediaDBID, titleDBID, writeMatcher).Return(nil)

	s := &GamelistXMLScraper{db: mockDB}
	ch := make(chan scraper.ScrapeUpdate, 128)
	s.scrapeLoop(context.Background(), scraper.ScrapeOptions{
		Pauser: syncutil.NewPauser(),
	}, []scraper.ScrapeSystem{{ID: "nes", ROMPaths: []string{root}, DBID: systemDBID}}, mockDB, ch)

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

func TestScrapeLoop_ForceMode_Success(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`
<gameList>
  <game><path>./sonic.md</path><name>Sonic</name></game>
</gameList>`), 0o600))

	const (
		titleDBID  = int64(2)
		mediaDBID  = int64(20)
		systemDBID = int64(200)
	)

	mockDB := helpers.NewMockMediaDBI()
	mockDB.On("GetMediaBySystemID", "genesis").
		Return([]database.MediaWithFullPath{{
			DBID: mediaDBID, MediaTitleDBID: titleDBID, Path: filepath.Join(root, "sonic.md"),
		}}, nil)
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

	const (
		titleDBID  = int64(3)
		mediaDBID  = int64(30)
		systemDBID = int64(300)
	)

	mockDB := helpers.NewMockMediaDBI()
	mockDB.On("FindMediaTitlesWithoutSentinel", mock.Anything, systemDBID, "scraper.gamelist.xml:scraped").
		Return([]database.MediaTitle{{DBID: titleDBID, SystemDBID: systemDBID, Slug: "mario", Name: "Mario"}}, nil)
	mockDB.On("GetMediaBySystemID", "nes").
		Return([]database.MediaWithFullPath{{
			DBID: mediaDBID, MediaTitleDBID: titleDBID, Path: filepath.Join(root, "mario.nes"),
		}}, nil)
	mockDB.On("GetScrapedMediaIDs", mock.Anything, "gamelist.xml", systemDBID).
		Return(map[int64]struct{}{}, nil)
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

func TestScrapeLoop_AllMediaScraped_SkipsSystem(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`
<gameList>
  <game><path>./mario.nes</path><name>Mario</name></game>
</gameList>`), 0o600))

	const (
		mediaDBID  = int64(40)
		titleDBID  = int64(4)
		systemDBID = int64(400)
	)

	mockDB := helpers.NewMockMediaDBI()
	mockDB.On("FindMediaTitlesWithoutSentinel", mock.Anything, systemDBID, "scraper.gamelist.xml:scraped").
		Return([]database.MediaTitle{}, nil)
	mockDB.On("GetMediaBySystemID", "nes").
		Return([]database.MediaWithFullPath{{
			DBID: mediaDBID, MediaTitleDBID: titleDBID, Path: filepath.Join(root, "mario.nes"),
		}}, nil)
	mockDB.On("GetScrapedMediaIDs", mock.Anything, "gamelist.xml", systemDBID).
		Return(map[int64]struct{}{mediaDBID: {}}, nil)

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

func TestScrapeLoop_CompanionSkipsAlreadyScrapedMedia(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(`
<gameList>
  <game id="42" source="ZaparooCompanion">
    <name>Companion Game</name>
  </game>
  <game parentid="42" source="ZaparooCompanion">
    <path>./child.rom</path>
  </game>
</gameList>`), 0o600))

	const (
		mediaDBID  = int64(41)
		titleDBID  = int64(42)
		systemDBID = int64(401)
	)

	mockDB := helpers.NewMockMediaDBI()
	mockDB.On("FindMediaTitlesWithoutSentinel", mock.Anything, systemDBID, "scraper.gamelist.xml:scraped").
		Return([]database.MediaTitle{}, nil)
	mockDB.On("GetMediaBySystemID", "nes").
		Return([]database.MediaWithFullPath{{
			DBID: mediaDBID, MediaTitleDBID: titleDBID, Path: filepath.Join(root, "child.rom"),
		}}, nil)
	mockDB.On("GetScrapedMediaIDs", mock.Anything, "gamelist.xml", systemDBID).
		Return(map[int64]struct{}{mediaDBID: {}}, nil)

	s := &GamelistXMLScraper{db: mockDB}
	ch := make(chan scraper.ScrapeUpdate, 128)
	s.scrapeLoop(context.Background(), scraper.ScrapeOptions{Pauser: syncutil.NewPauser()},
		[]scraper.ScrapeSystem{{ID: "nes", ROMPaths: []string{root}, DBID: systemDBID}}, mockDB, ch)

	updates := drainChannel(ch)
	var done scraper.ScrapeUpdate
	for _, u := range updates {
		if u.Done {
			done = u
		}
	}
	require.True(t, done.Done)
	assert.Equal(t, 1, done.Processed)
	assert.Equal(t, 0, done.Matched)
	assert.Equal(t, 1, done.Skipped)
	mockDB.AssertNotCalled(t, "ApplyScrapeResult", mock.Anything, mediaDBID, titleDBID, mock.Anything)
	mockDB.AssertExpectations(t)
}
