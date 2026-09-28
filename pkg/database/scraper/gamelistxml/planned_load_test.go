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
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/container"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediascanner"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// fullRecordIndexes builds the record indexes over every row of a system, as
// the scrape did before loads were planned. The planned load must answer every
// lookup the same way.
func fullRecordIndexes(
	allMedia []database.MediaWithFullPath,
	titlesBySlug, allTitlesBySlug map[string]database.MediaTitle,
	scrapedIDs map[int64]struct{},
) loadRecordIndexes {
	indexes := loadRecordIndexes{
		TitlesBySlug:     titlesBySlug,
		AllTitlesBySlug:  allTitlesBySlug,
		MediaByPathFold:  make(map[string]database.Media, len(allMedia)),
		MediaByTitleDBID: make(map[int64][]database.Media, len(allMedia)),
		MediaByFilename:  make(map[string][]database.Media, len(allMedia)),
	}
	containerRows := make([]database.Media, 0, len(allMedia))
	for i := range allMedia {
		m := &allMedia[i]
		if m.IsMissing {
			continue
		}
		media := database.Media{
			DBID: m.DBID, MediaTitleDBID: m.MediaTitleDBID, Path: m.Path, ParentDir: m.ParentDir,
		}
		containerRows = append(containerRows, media)
		if _, scraped := scrapedIDs[m.DBID]; !scraped {
			indexes.MediaByPathFold[pathFoldKey(m.Path)] = media
			indexes.MediaByTitleDBID[m.MediaTitleDBID] = append(indexes.MediaByTitleDBID[m.MediaTitleDBID], media)
			if key := mediaFilenameKey(m.Path); key != "" {
				indexes.MediaByFilename[key] = append(indexes.MediaByFilename[key], media)
			}
		}
	}
	indexes.Containers = container.NewIndex(containerRows)
	for slug, title := range titlesBySlug {
		if len(indexes.MediaByTitleDBID[title.DBID]) == 0 {
			delete(titlesBySlug, slug)
		}
	}
	return indexes
}

// equivalenceT is the part of testing.TB the equivalence helpers need, which
// rapid's T also provides.
type equivalenceT interface {
	require.TestingT
	Helper()
}

// equivalenceLibrary is one system's rows and state, fed identically to a
// full load and to a planned one.
type equivalenceLibrary struct {
	// tb validates the writes the mock receives.
	tb        testing.TB
	scraped   map[int64]struct{}
	media     []database.MediaWithFullPath
	titles    []database.TitleWithSystem
	unscraped []database.MediaTitle
	sources   []database.MediaSource
	force     bool
	arcade    bool
}

// matchOutcome is what a scrape would write, reduced to the parts that
// identify the target and how it was chosen.
type matchOutcome struct {
	records          []string
	companionTargets []string
	companion        [3]int
}

const equivalenceSystemDBID = int64(7)

func (lib *equivalenceLibrary) mock() *batchMockMediaDB {
	m := &batchMockMediaDB{t: lib.tb, MockMediaDBI: helpers.NewMockMediaDBI()}
	m.On("GetMediaBySystemID", "c64").Return(lib.media, nil).Maybe()
	m.On("GetTitlesBySystemID", "c64").Return(lib.titles, nil).Maybe()
	m.On("FindMediaTitlesWithoutSentinel", mock.Anything, equivalenceSystemDBID, mock.Anything).
		Return(lib.unscraped, nil).Maybe()
	m.On("GetScrapedMediaIDs", mock.Anything, "gamelist.xml", equivalenceSystemDBID).
		Return(lib.scraped, nil).Maybe()
	if len(lib.sources) > 0 {
		m.On("GetMediaSourcesForScrape", mock.Anything, "c64", mock.Anything).Return(lib.sources, nil).Maybe()
	}
	return m
}

func (lib *equivalenceLibrary) scraper(db database.MediaDBI) *GamelistXMLScraper {
	return &GamelistXMLScraper{db: db, matchArcadeSets: lib.arcade}
}

func (lib *equivalenceLibrary) opts() scraper.ScrapeOptions {
	return scraper.ScrapeOptions{Pauser: syncutil.NewPauser(), Force: lib.force}
}

func summarizeRun(
	t equivalenceT, g *GamelistXMLScraper, system scraper.ScrapeSystem, mdb *batchMockMediaDB,
	indexes loadRecordIndexes, planned *plannedIndexes, parsed parsedGamelistSystem, opts scraper.ScrapeOptions,
) matchOutcome {
	t.Helper()
	ctx := context.Background()
	var out matchOutcome
	// A planned run drops the companion entries once extracted, as the scrape
	// does; the full run keeps them, so the comparison covers the drop.
	parents, children := companionEntriesFromParsed(ctx, system, parsed)
	if planned != nil {
		parsed.dropCompanionGames()
	}
	companion := g.writeCompanionEntries(ctx, opts, system, mdb, indexes, parents, children, nil, 0, 0)
	out.companion = [3]int{companion.Processed, companion.Matched, companion.Skipped}
	for _, batch := range mdb.batches {
		for _, target := range batch {
			out.companionTargets = append(out.companionTargets,
				fmt.Sprintf("%d/%d", target.MediaDBID, target.MediaTitleDBID))
		}
	}
	// The scrape's own early exit. A full load can keep titles no entry names,
	// so it skips less often, but a skipped system and one matched to nothing
	// write the same.
	if len(indexes.TitlesBySlug) == 0 && len(indexes.MediaByPathFold) == 0 {
		return out
	}
	records, err := g.loadRecordsFromParsed(ctx, system, indexes, parsed, planned)
	require.NoError(t, err)
	for _, r := range records {
		out.records = append(out.records, fmt.Sprintf("%s|%s|%s|%d|%d|%t|%s",
			r.Game.Path, r.Game.Name, r.MatchKind, r.MatchedMediaDBID, r.MatchedTitleDBID,
			r.MediaLevelWriteSafe, r.SourceDirectory))
	}
	return out
}

// runFull mirrors the scrape before planned loads: every row, then parsing.
func (lib *equivalenceLibrary) runFull(t equivalenceT, system scraper.ScrapeSystem) matchOutcome {
	t.Helper()
	ctx := context.Background()
	mdb := lib.mock()
	g := lib.scraper(mdb)
	titlesBySlug := make(map[string]database.MediaTitle)
	allTitlesBySlug := make(map[string]database.MediaTitle)
	for _, title := range lib.titles {
		row := database.MediaTitle{DBID: title.DBID, SystemDBID: title.SystemDBID, Slug: title.Slug, Name: title.Name}
		if lib.force {
			titlesBySlug[title.Slug] = row
		}
		allTitlesBySlug[title.Slug] = row
	}
	if !lib.force {
		for _, title := range lib.unscraped {
			titlesBySlug[title.Slug] = title
		}
	}
	scraped := lib.scraped
	if lib.force {
		scraped = map[int64]struct{}{}
	}
	indexes := fullRecordIndexes(lib.media, titlesBySlug, allTitlesBySlug, scraped)
	parsed, err := g.loadParsedGamelistSystem(ctx, system)
	require.NoError(t, err)
	indexes.ArcadeBySetName, err = g.indexArcadeSets(ctx, lib.media, parsed)
	require.NoError(t, err)
	return summarizeRun(t, g, system, mdb, indexes, nil, parsed, lib.opts())
}

// runPlanned is the scrape's own per-system load.
func (lib *equivalenceLibrary) runPlanned(
	t equivalenceT, system scraper.ScrapeSystem,
) (matchOutcome, plannedLoadStats, bool) {
	t.Helper()
	ctx := context.Background()
	mdb := lib.mock()
	g := lib.scraper(mdb)
	parsed, err := g.loadParsedGamelistSystem(ctx, system)
	require.NoError(t, err)
	plan := buildLookupPlan(g.cfg, system, &parsed)
	if plan.entries == 0 {
		return matchOutcome{}, plannedLoadStats{}, false
	}
	sources, err := mdb.GetMediaSourcesForScrape(ctx, system.ID, nil)
	require.NoError(t, err)
	plan.addSources(sources)
	indexes, planned, err := g.loadPlannedIndexes(ctx, lib.opts(), "gamelist.xml", system, mdb, plan, &parsed)
	require.NoError(t, err)
	planned.Sources = sources
	return summarizeRun(t, g, system, mdb, indexes, planned, parsed, lib.opts()), planned.Stats, true
}

func requireEquivalent(t equivalenceT, lib *equivalenceLibrary, system scraper.ScrapeSystem) plannedLoadStats {
	t.Helper()
	full := lib.runFull(t, system)
	planned, stats, loaded := lib.runPlanned(t, system)
	if !loaded {
		assert.Empty(t, full.records, "a system the plan skips must have had nothing to write")
		assert.Empty(t, full.companionTargets, "a system the plan skips must have had nothing to write")
		return stats
	}
	assert.Equal(t, full, planned)
	return stats
}

func writeGamelist(t equivalenceT, root, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"),
		[]byte("<gameList>\n"+body+"\n</gameList>"), 0o600))
}

func mediaRow(dbid, titleDBID int64, path string) database.MediaWithFullPath {
	return database.MediaWithFullPath{
		DBID: dbid, MediaTitleDBID: titleDBID, Path: path, ParentDir: container.ParentDir(path),
	}
}

func sortedByPath(rows []database.MediaWithFullPath) []database.MediaWithFullPath {
	sort.Slice(rows, func(i, j int) bool { return rows[i].Path < rows[j].Path })
	return rows
}

func titleSlugFor(system scraper.ScrapeSystem, resolved, name string) string {
	return mediascanner.GetPathFragments(&mediascanner.PathFragmentParams{
		Path: resolved, SystemID: system.ID, NoExt: true, ProvidedName: name,
	}).Slug
}

func TestPlannedLoad_MatchesFullLoad(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setup func(t *testing.T, root string, system scraper.ScrapeSystem) *equivalenceLibrary
		name  string
	}{
		{
			name: "path, slug and filename matches",
			setup: func(t *testing.T, root string, system scraper.ScrapeSystem) *equivalenceLibrary {
				writeGamelist(t, root, `
  <game><path>./Alpha.d64</path><name>Alpha</name></game>
  <game><path>./Renamed.d64</path><name>Beta</name></game>
  <game id="9" source="ZaparooCompanion"><name>Gamma</name></game>
  <game parentid="9" source="ZaparooCompanion"><path>./other/Gamma.d64</path></game>`)
				beta := titleSlugFor(system, filepath.Join(root, "Renamed.d64"), "Beta")
				return &equivalenceLibrary{
					media: sortedByPath([]database.MediaWithFullPath{
						mediaRow(1, 1, filepath.Join(root, "Alpha.d64")),
						mediaRow(2, 2, filepath.Join(root, "Beta.d64")),
						mediaRow(3, 3, filepath.Join(root, "sub", "Gamma.d64")),
						mediaRow(4, 4, filepath.Join(root, "Unrelated.d64")),
					}),
					titles: []database.TitleWithSystem{
						{DBID: 1, Slug: "alpha", Name: "Alpha", SystemDBID: equivalenceSystemDBID},
						{DBID: 2, Slug: beta, Name: "Beta", SystemDBID: equivalenceSystemDBID},
						{DBID: 3, Slug: "gamma", Name: "Gamma", SystemDBID: equivalenceSystemDBID},
						{DBID: 4, Slug: "unrelated", Name: "Unrelated", SystemDBID: equivalenceSystemDBID},
					},
					unscraped: []database.MediaTitle{
						{DBID: 2, Slug: beta, Name: "Beta", SystemDBID: equivalenceSystemDBID},
					},
					scraped: map[int64]struct{}{},
				}
			},
		},
		{
			// The gamelist names a directory in another case, so the path
			// fallback scans it; its single row must be there to match.
			name: "case-mismatched directory reaches the path fallback",
			setup: func(t *testing.T, root string, _ scraper.ScrapeSystem) *equivalenceLibrary {
				writeGamelist(t, root, `<game><path>./game dir</path><name>Dir Game</name></game>`)
				return &equivalenceLibrary{
					media: sortedByPath([]database.MediaWithFullPath{
						mediaRow(1, 1, filepath.Join(root, "Game Dir", "disk.d64")),
						mediaRow(2, 2, filepath.Join(root, "Elsewhere.d64")),
					}),
					scraped: map[int64]struct{}{},
				}
			},
		},
		{
			// Two rows below the fallback's prefix make it ambiguous; dropping
			// either would turn the other into a false match.
			name: "ambiguous path fallback stays ambiguous",
			setup: func(t *testing.T, root string, _ scraper.ScrapeSystem) *equivalenceLibrary {
				writeGamelist(t, root, `<game><path>./game dir</path><name>Dir Game</name></game>`)
				return &equivalenceLibrary{
					media: sortedByPath([]database.MediaWithFullPath{
						mediaRow(1, 1, filepath.Join(root, "Game Dir", "disk1.d64")),
						mediaRow(2, 2, filepath.Join(root, "Game Dir", "disk2.d64")),
					}),
					scraped: map[int64]struct{}{},
				}
			},
		},
		{
			name: "disc folder entries resolve through the container target",
			setup: func(t *testing.T, root string, _ scraper.ScrapeSystem) *equivalenceLibrary {
				writeGamelist(t, root, `
  <game><path>./Cool Game</path><name>Cool Game</name></game>
  <folder><path>./Other Game</path><name>Other Game</name></folder>
  <folder><path>./Nested</path><name>Nested</name></folder>`)
				return &equivalenceLibrary{
					media: sortedByPath([]database.MediaWithFullPath{
						mediaRow(1, 1, filepath.Join(root, "Cool Game", "Cool Game.cue")),
						mediaRow(2, 1, filepath.Join(root, "Cool Game", "Cool Game (Track 1).bin")),
						mediaRow(3, 2, filepath.Join(root, "Other Game", "Disc 1.chd")),
						mediaRow(4, 2, filepath.Join(root, "Other Game", "Disc 2.chd")),
						mediaRow(5, 3, filepath.Join(root, "Nested", "a.chd")),
						mediaRow(6, 3, filepath.Join(root, "Nested", "deeper", "b.chd")),
					}),
					scraped: map[int64]struct{}{3: {}},
				}
			},
		},
		{
			name: "cd track entries reach their cue and m3u",
			setup: func(t *testing.T, root string, _ scraper.ScrapeSystem) *equivalenceLibrary {
				gameDir := filepath.Join(root, "Disc Game")
				require.NoError(t, os.MkdirAll(gameDir, 0o750))
				require.NoError(t, os.WriteFile(filepath.Join(gameDir, "Disc Game.cue"),
					[]byte("FILE \"Disc Game (Track 1).bin\" BINARY\n"), 0o600))
				writeGamelist(t, root,
					`<game><path>./Disc Game/Disc Game (Track 1).bin</path><name>Disc Game</name></game>`)
				return &equivalenceLibrary{
					media: sortedByPath([]database.MediaWithFullPath{
						mediaRow(1, 1, filepath.Join(gameDir, "Disc Game.cue")),
						mediaRow(2, 2, filepath.Join(root, "Other.cue")),
						mediaRow(3, 3, filepath.Join(root, "Plain.d64")),
					}),
					scraped: map[int64]struct{}{},
				}
			},
		},
		{
			name: "companion slug child writes every row of its title",
			setup: func(t *testing.T, root string, _ scraper.ScrapeSystem) *equivalenceLibrary {
				writeGamelist(t, root, `
  <game id="1" source="ZaparooCompanion"><name>Delta</name></game>
  <game parentid="1" source="ZaparooCompanion"><path>./delta.slug</path></game>`)
				return &equivalenceLibrary{
					media: sortedByPath([]database.MediaWithFullPath{
						mediaRow(1, 5, filepath.Join(root, "Delta (USA).d64")),
						mediaRow(2, 5, filepath.Join(root, "Delta (Europe).d64")),
						mediaRow(3, 6, filepath.Join(root, "Epsilon.d64")),
					}),
					titles: []database.TitleWithSystem{
						{DBID: 5, Slug: "delta", Name: "Delta", SystemDBID: equivalenceSystemDBID},
						{DBID: 6, Slug: "epsilon", Name: "Epsilon", SystemDBID: equivalenceSystemDBID},
					},
					scraped: map[int64]struct{}{},
				}
			},
		},
		{
			name: "arcade set names select descriptors",
			setup: func(t *testing.T, root string, _ scraper.ScrapeSystem) *equivalenceLibrary {
				for name, set := range map[string]string{"Pac-Man.mra": "puckman", "Other.mra": "other"} {
					require.NoError(t, os.WriteFile(filepath.Join(root, name),
						[]byte("<misterromdescription><setname>"+set+"</setname></misterromdescription>"), 0o600))
				}
				writeGamelist(t, root, `<game><path>./puckman.zip</path><name>Pac-Man</name></game>`)
				return &equivalenceLibrary{
					media: sortedByPath([]database.MediaWithFullPath{
						mediaRow(1, 1, filepath.Join(root, "Pac-Man.mra")),
						mediaRow(2, 2, filepath.Join(root, "Other.mra")),
					}),
					scraped: map[int64]struct{}{},
					arcade:  true,
				}
			},
		},
		{
			name: "indexed sources claim their rows",
			setup: func(t *testing.T, root string, _ scraper.ScrapeSystem) *equivalenceLibrary {
				writeGamelist(t, root, `<game><path>./Adventure</path><name>Adventure</name></game>`)
				target := filepath.Join(root, "Adventure", "adventure.scummvm")
				return &equivalenceLibrary{
					media: sortedByPath([]database.MediaWithFullPath{
						mediaRow(1, 1, target),
						mediaRow(2, 2, filepath.Join(root, "Other.d64")),
					}),
					sources: []database.MediaSource{{
						MediaPath: target, SourcePath: filepath.Join(root, "Adventure"),
						SourceRoot: root, SourceKind: "directory", MediaDBID: 1,
						SystemDBID: equivalenceSystemDBID, Unique: true,
					}},
					scraped: map[int64]struct{}{},
				}
			},
		},
		{
			name: "no entries loads nothing",
			setup: func(t *testing.T, root string, _ scraper.ScrapeSystem) *equivalenceLibrary {
				writeGamelist(t, root, `<game id="1" source="ZaparooCompanion"><name>Parent only</name></game>`)
				return &equivalenceLibrary{
					media:   []database.MediaWithFullPath{mediaRow(1, 1, filepath.Join(root, "A.d64"))},
					scraped: map[int64]struct{}{},
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			system := scraper.ScrapeSystem{ID: "c64", DBID: equivalenceSystemDBID, ROMPaths: []string{root}}
			lib := tt.setup(t, root, system)
			lib.tb = t
			requireEquivalent(t, lib, system)
			lib.force = true
			requireEquivalent(t, lib, system)
		})
	}
}

// The reported C64 library: a gamelist naming a few titles in a system of many
// thousands must not hold the system's rows.
func TestPlannedLoad_RetainsOnlyReachableRows(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	system := scraper.ScrapeSystem{ID: "c64", DBID: equivalenceSystemDBID, ROMPaths: []string{root}}

	const systemSize = 20000
	media := make([]database.MediaWithFullPath, 0, systemSize)
	titles := make([]database.TitleWithSystem, 0, systemSize)
	for i := range systemSize {
		name := fmt.Sprintf("Game %05d", i)
		media = append(media, mediaRow(int64(i+1), int64(i+1), filepath.Join(root, "games", name+".d64")))
		titles = append(titles, database.TitleWithSystem{
			DBID: int64(i + 1), Slug: strings.ToLower(strings.ReplaceAll(name, " ", "")),
			Name: name, SystemDBID: equivalenceSystemDBID,
		})
	}
	var body strings.Builder
	for _, i := range []int{3, 500, 19999} {
		_, _ = fmt.Fprintf(&body, "<game><path>./games/Game %05d.d64</path><name>Game %05d</name></game>\n", i, i)
	}
	_, _ = body.WriteString(`<game id="1" source="ZaparooCompanion"><name>Game 00042</name></game>
<game parentid="1" source="ZaparooCompanion"><path>./game00042.slug</path></game>`)
	writeGamelist(t, root, body.String())

	lib := &equivalenceLibrary{
		tb:    t,
		media: sortedByPath(media), titles: titles,
		unscraped: []database.MediaTitle{}, scraped: map[int64]struct{}{}, force: true,
	}
	stats := requireEquivalent(t, lib, system)
	assert.Equal(t, systemSize, stats.systemMedia)
	assert.LessOrEqual(t, stats.retainedMedia, 4, "only the rows the four entries name may be held")
	assert.Equal(t, 1, stats.passes)
}

// Random libraries and gamelists, including case variants, containers, zip
// members, companion children and scraped rows, match the full load exactly.
func TestPropertyPlannedLoadMatchesFullLoad(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	n := 0
	rapid.Check(t, func(rt *rapid.T) {
		n++
		root := filepath.Join(base, fmt.Sprintf("r%d", n))
		require.NoError(rt, os.MkdirAll(root, 0o750))
		system := scraper.ScrapeSystem{ID: "c64", DBID: equivalenceSystemDBID, ROMPaths: []string{root}}

		dirs := []string{"", "Game A/", "game a/", "Disc Game/", "Disc Game/Extra/", "pack.zip/"}
		files := []string{
			"x.d64", "X.d64", "Game.cue", "Game (Track 1).bin", "Game.m3u",
			"Disc 1.chd", "Disc 2.chd", "readme.txt", "pack.zip",
		}
		names := []string{"Game A", "Disc Game", "X", "Pack"}

		seen := make(map[string]bool)
		var media []database.MediaWithFullPath
		for i := range rapid.IntRange(0, 14).Draw(rt, "rows") {
			rel := rapid.SampledFrom(dirs).Draw(rt, "dir") + rapid.SampledFrom(files).Draw(rt, "file")
			if seen[rel] {
				continue
			}
			seen[rel] = true
			row := mediaRow(int64(i+1), rapid.Int64Range(1, 4).Draw(rt, "title"), filepath.Join(root, rel))
			row.IsMissing = rapid.IntRange(0, 5).Draw(rt, "missing") == 0
			if rapid.IntRange(0, 6).Draw(rt, "parentMismatch") == 0 {
				row.ParentDir = ""
			}
			media = append(media, row)
		}

		const titleCount = 4
		titles := make([]database.TitleWithSystem, 0, titleCount)
		var unscraped []database.MediaTitle
		for id := int64(1); id <= titleCount; id++ {
			name := rapid.SampledFrom(names).Draw(rt, "titleName")
			slug := titleSlugFor(system, filepath.Join(root, name+".d64"), name)
			titles = append(titles, database.TitleWithSystem{
				DBID: id, Slug: slug, Name: name, SystemDBID: equivalenceSystemDBID,
			})
			if rapid.Bool().Draw(rt, "unscrapedTitle") {
				unscraped = append(unscraped, database.MediaTitle{
					DBID: id, Slug: slug, Name: name, SystemDBID: equivalenceSystemDBID,
				})
			}
		}
		scraped := make(map[int64]struct{})
		for _, row := range media {
			if rapid.IntRange(0, 3).Draw(rt, "scraped") == 0 {
				scraped[row.DBID] = struct{}{}
			}
		}

		var body strings.Builder
		entryPaths := make([]string, 0, len(dirs)*len(files)+4)
		for _, d := range dirs {
			entryPaths = append(entryPaths, "./"+strings.TrimSuffix(d, "/"))
			for _, f := range files {
				entryPaths = append(entryPaths, "./"+d+f, "./"+strings.ToLower(d+f))
			}
		}
		entryPaths = append(entryPaths, "./missing.d64", "../outside.d64")
		for range rapid.IntRange(0, 6).Draw(rt, "games") {
			_, _ = fmt.Fprintf(&body, "<game><path>%s</path><name>%s</name></game>\n",
				rapid.SampledFrom(entryPaths).Draw(rt, "gamePath"), rapid.SampledFrom(names).Draw(rt, "gameName"))
		}
		for range rapid.IntRange(0, 2).Draw(rt, "folders") {
			_, _ = fmt.Fprintf(&body, "<folder><path>%s</path><name>Folder</name></folder>\n",
				"./"+strings.TrimSuffix(rapid.SampledFrom(dirs[1:]).Draw(rt, "folderDir"), "/"))
		}
		if rapid.Bool().Draw(rt, "companion") {
			_, _ = body.WriteString(`<game id="1" source="ZaparooCompanion"><name>Parent</name></game>` + "\n")
			for range rapid.IntRange(1, 3).Draw(rt, "children") {
				children := append(slices.Clone(entryPaths), "./gamea.slug", "./x.slug")
				child := rapid.SampledFrom(children).Draw(rt, "child")
				_, _ = fmt.Fprintf(&body,
					`<game parentid="1" source="ZaparooCompanion"><path>%s</path></game>`+"\n", child)
			}
		}
		writeGamelist(rt, root, body.String())

		lib := &equivalenceLibrary{
			tb:    t,
			media: sortedByPath(media), titles: titles, unscraped: unscraped,
			scraped: scraped, force: rapid.Bool().Draw(rt, "force"),
		}
		requireEquivalent(rt, lib, system)
	})
}

// A system without gamelist entries has nothing to match, so the scrape must not
// read its rows. On MiSTer a large system's rows alone ran the device out of
// memory during "Preparing scrape" even with no gamelist.xml present.
func TestScrapeLoop_NoGamelistEntriesSkipsDatabaseLoad(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		gamelist string
	}{
		{name: "no gamelist"},
		{
			name:     "companion parents without children",
			gamelist: `<gameList><game id="1" source="ZaparooCompanion"><name>Parent</name></game></gameList>`,
		},
		{name: "unreadable gamelist", gamelist: `<gameList><game><path>./Broken.d64</path>`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			if tt.gamelist != "" {
				require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(tt.gamelist), 0o600))
			}
			// Expectations make the mock record the calls this test forbids.
			mockDB := newMockMediaDB(t)
			mockDB.On("GetMediaBySystemID", "c64").Return([]database.MediaWithFullPath{}, nil).Maybe()
			mockDB.On("GetTitlesBySystemID", "c64").Return([]database.TitleWithSystem{}, nil).Maybe()
			mockDB.On("FindMediaTitlesWithoutSentinel", mock.Anything, mock.Anything, mock.Anything).
				Return([]database.MediaTitle{}, nil).Maybe()
			mockDB.On("GetScrapedMediaIDs", mock.Anything, mock.Anything, mock.Anything).
				Return(map[int64]struct{}{}, nil).Maybe()
			mockDB.On("GetMediaSourcesForScrape", mock.Anything, mock.Anything, mock.Anything).
				Return([]database.MediaSource{}, nil).Maybe()
			s := &GamelistXMLScraper{db: mockDB}
			ch := make(chan scraper.ScrapeUpdate, 128)
			s.scrapeLoop(context.Background(), scraper.ScrapeOptions{Pauser: syncutil.NewPauser()},
				[]scraper.ScrapeSystem{{ID: "c64", ROMPaths: []string{root}, DBID: 1}}, mockDB, ch)

			updates := drainChannel(ch)
			require.NotEmpty(t, updates)
			last := updates[len(updates)-1]
			assert.True(t, last.Done)
			assert.Equal(t, 1, last.CurrentStep)
			for _, method := range []string{
				"GetMediaBySystemID", "GetTitlesBySystemID", "FindMediaTitlesWithoutSentinel",
				"GetScrapedMediaIDs", "GetScrapeRunMediaIDs", "GetMediaSourcesForScrape",
			} {
				for _, call := range mockDB.Calls {
					assert.NotEqual(t, method, call.Method, "%s must not run for a system with no entries", method)
				}
			}
		})
	}
}

// cancellingStreamDB cancels the scrape while its media rows are streaming.
type cancellingStreamDB struct {
	*helpers.MockMediaDBI
	cancel context.CancelFunc
}

func (db *cancellingStreamDB) ForEachMediaBySystemID(
	ctx context.Context, _ string, fn func(*database.MediaWithFullPath) error,
) error {
	row := database.MediaWithFullPath{DBID: 1, Path: "/roms/c64/a.d64"}
	if err := fn(&row); err != nil {
		return err
	}
	db.cancel()
	return ctx.Err() //nolint:wrapcheck // the real stream reports the context error as is
}

func TestScrapeLoop_CancelDuringMediaStreamEndsSystem(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "gamelist.xml"),
		[]byte(`<gameList><game><path>./a.d64</path><name>A</name></game></gameList>`), 0o600))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mockDB := newMockMediaDB(t)
	mockDB.On("FindMediaTitlesWithoutSentinel", mock.Anything, int64(1), mock.Anything).
		Return([]database.MediaTitle{}, nil)
	mockDB.On("GetScrapedMediaIDs", mock.Anything, "gamelist.xml", int64(1)).Return(map[int64]struct{}{}, nil)
	db := &cancellingStreamDB{MockMediaDBI: mockDB, cancel: cancel}

	s := &GamelistXMLScraper{db: db}
	ch := make(chan scraper.ScrapeUpdate, 128)
	s.scrapeLoop(ctx, scraper.ScrapeOptions{Pauser: syncutil.NewPauser()},
		[]scraper.ScrapeSystem{{ID: "c64", ROMPaths: []string{root}, DBID: 1}}, db, ch)

	updates := drainChannel(ch)
	require.NotEmpty(t, updates)
	last := updates[len(updates)-1]
	assert.True(t, last.Done)
	require.NoError(t, last.FatalErr, "a cancelled load is not a failure")
	mockDB.AssertNotCalled(t, "ApplyScrapeResult", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// cancellingFs cancels a scrape on the first read of any file and counts the
// reads that follow.
type cancellingFs struct {
	afero.Fs
	cancel      context.CancelFunc
	readsAfter  *atomic.Int64
	cancelledAt *atomic.Bool
}

func (f cancellingFs) Open(name string) (afero.File, error) {
	file, err := f.Fs.Open(name)
	if err != nil {
		return nil, err //nolint:wrapcheck // test double passes the filesystem's error through
	}
	return cancellingFile{File: file, fs: f}, nil
}

type cancellingFile struct {
	afero.File
	fs cancellingFs
}

func (f cancellingFile) Read(p []byte) (int, error) {
	if f.fs.cancelledAt.Swap(true) {
		f.fs.readsAfter.Add(1)
	} else {
		f.fs.cancel()
	}
	return f.File.Read(p) //nolint:wrapcheck // test double
}

// A scrape cancelled while a large gamelist decodes stops decoding at once, and
// the gamelist is not reported as unreadable, which would leave an inbox
// message.
func TestLoadParsedGamelistSystem_CancelStopsDecoding(t *testing.T) {
	t.Parallel()
	mem := afero.NewMemMapFs()
	var body strings.Builder
	_, _ = body.WriteString("<gameList>")
	for i := range 5000 {
		_, _ = fmt.Fprintf(&body, "<game><path>./g%d.d64</path><name>Game %d</name><desc>%s</desc></game>",
			i, i, strings.Repeat("x", 200))
	}
	_, _ = body.WriteString("</gameList>")
	require.NoError(t, afero.WriteFile(mem, "/roms/c64/gamelist.xml", []byte(body.String()), 0o600))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var readsAfter atomic.Int64
	var cancelled atomic.Bool
	fs := cancellingFs{Fs: mem, cancel: cancel, readsAfter: &readsAfter, cancelledAt: &cancelled}
	parsed, err := (&GamelistXMLScraper{fs: fs}).loadParsedGamelistSystem(ctx,
		scraper.ScrapeSystem{ID: "c64", ROMPaths: []string{"/roms/c64"}})
	require.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, parsed.SourceErrors)
	assert.Empty(t, parsed.Files)
	assert.Zero(t, readsAfter.Load(), "decoding must stop at the cancellation, not read the rest of the file")
}
