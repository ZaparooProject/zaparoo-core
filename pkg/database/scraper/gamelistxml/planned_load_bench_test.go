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
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

// seedBenchSystem indexes rows C64 media rows, each its own title, and writes
// a gamelist naming the first entries of them.
func seedBenchSystem(b *testing.B, rows, entries int) (database.MediaDBI, scraper.ScrapeSystem) {
	b.Helper()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(b)
	b.Cleanup(cleanup)
	root := b.TempDir()

	require.NoError(b, mediaDB.BeginTransaction(false))
	system, err := mediaDB.InsertSystem(database.System{SystemID: "C64", Name: "C64"})
	require.NoError(b, err)
	for i := range rows {
		name := fmt.Sprintf("Game %06d", i)
		title, err := mediaDB.InsertMediaTitle(&database.MediaTitle{
			SystemDBID: system.DBID, Slug: strings.ToLower(strings.ReplaceAll(name, " ", "")), Name: name,
		})
		require.NoError(b, err)
		_, err = mediaDB.InsertMedia(database.Media{
			SystemDBID: system.DBID, MediaTitleDBID: title.DBID,
			Path:      filepath.Join(root, "games", name+".d64"),
			ParentDir: filepath.ToSlash(filepath.Join(root, "games")) + "/",
		})
		require.NoError(b, err)
	}
	require.NoError(b, mediaDB.CommitTransaction())

	if entries > 0 {
		var body strings.Builder
		_, _ = body.WriteString("<gameList>\n")
		for i := range entries {
			_, _ = fmt.Fprintf(&body, "<game><path>./games/Game %06d.d64</path><name>Game %06d</name></game>\n", i, i)
		}
		_, _ = body.WriteString("</gameList>\n")
		require.NoError(b, os.WriteFile(filepath.Join(root, "gamelist.xml"), []byte(body.String()), 0o600))
	}
	return mediaDB, scraper.ScrapeSystem{ID: "C64", DBID: system.DBID, ROMPaths: []string{root}}
}

// BenchmarkScrapeSystemLoad compares one system's load and match, everything
// before the writes: the whole system held in indexes (full), against the rows
// the gamelist can reach (planned). small is a gamelist naming every row, where planning buys nothing
// and must cost nothing; large names a few rows of a big system; none has no
// gamelist at all.
func BenchmarkScrapeSystemLoad(b *testing.B) {
	// The scrape logs per system; interleaved with results it breaks benchstat.
	level := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.Disabled)
	b.Cleanup(func() { zerolog.SetGlobalLevel(level) })
	cases := []struct {
		name          string
		rows, entries int
	}{
		{name: "small_500x500", rows: 500, entries: 500},
		{name: "large_100k_100entries", rows: 100_000, entries: 100},
		{name: "none_100k", rows: 100_000},
	}
	opts := scraper.ScrapeOptions{Pauser: syncutil.NewPauser()}
	sentinel := scraper.SentinelTagInfo("gamelist.xml")
	for _, tc := range cases {
		mdb, system := seedBenchSystem(b, tc.rows, tc.entries)
		g := &GamelistXMLScraper{}
		ctx := context.Background()

		b.Run(tc.name+"/full", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				unscraped, err := mdb.FindMediaTitlesWithoutSentinel(ctx, system.DBID, sentinel.Type+":"+sentinel.Tag)
				require.NoError(b, err)
				titlesBySlug := make(map[string]database.MediaTitle, len(unscraped))
				for _, t := range unscraped {
					titlesBySlug[t.Slug] = t
				}
				all, err := mdb.GetTitlesBySystemID(system.ID)
				require.NoError(b, err)
				allTitlesBySlug := make(map[string]database.MediaTitle, len(all))
				for _, t := range all {
					allTitlesBySlug[t.Slug] = database.MediaTitle{
						DBID: t.DBID, SystemDBID: t.SystemDBID, Slug: t.Slug, Name: t.Name,
					}
				}
				media, err := mdb.GetMediaBySystemID(system.ID)
				require.NoError(b, err)
				scraped, err := mdb.GetScrapedMediaIDs(ctx, "gamelist.xml", system.DBID)
				require.NoError(b, err)
				indexes := fullRecordIndexes(media, titlesBySlug, allTitlesBySlug, scraped)
				parsed, err := g.loadParsedGamelistSystem(ctx, system)
				require.NoError(b, err)
				indexes.ArcadeBySetName = map[string][]database.Media{}
				_, err = g.loadRecordsFromParsed(ctx, system, indexes, parsed, nil)
				require.NoError(b, err)
			}
		})
		b.Run(tc.name+"/planned", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				parsed, err := g.loadParsedGamelistSystem(ctx, system)
				require.NoError(b, err)
				plan := buildLookupPlan(nil, system, &parsed)
				if plan.entries == 0 {
					continue
				}
				indexes, planned, err := g.loadPlannedIndexes(ctx, opts, "gamelist.xml", system, mdb, plan, &parsed)
				require.NoError(b, err)
				_, err = g.loadRecordsFromParsed(ctx, system, indexes, parsed, planned)
				require.NoError(b, err)
			}
		})
	}
}
