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

package mediadb

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/require"
)

const scraperBenchRows = 1000

// These benchmarks are first-pass evidence only. A CTE delete variant was faster
// locally but slower on target storage, so scraper SQL changes also need target
// trace logs and EXPLAIN QUERY PLAN evidence before acceptance.
func BenchmarkApplyScrapeResults_CompanionBatch_10(b *testing.B) {
	benchApplyScrapeResultsCompanionBatch(b, "full", func(target *database.ScrapeWriteTarget) {
		target.Write.TitleTags = benchTitleTags(target.MediaTitleDBID)
		target.Write.TitleProps = benchTitleProps(target.MediaTitleDBID)
	})
	benchApplyScrapeResultsCompanionBatch(b, "title-tags-only", func(target *database.ScrapeWriteTarget) {
		target.Write.TitleTags = benchTitleTags(target.MediaTitleDBID)
	})
	benchApplyScrapeResultsCompanionBatch(b, "title-props-only", func(target *database.ScrapeWriteTarget) {
		target.Write.TitleProps = benchTitleProps(target.MediaTitleDBID)
	})
	benchApplyScrapeResultsCompanionBatch(b, "sentinel-only", func(_ *database.ScrapeWriteTarget) {})
}

func benchApplyScrapeResultsCompanionBatch(
	b *testing.B,
	name string,
	customize func(target *database.ScrapeWriteTarget),
) {
	b.Helper()
	b.Run(name, func(b *testing.B) {
		b.ReportAllocs()
		ctx := context.Background()
		mediaDB, cleanup := setupBenchMediaDB(b, scraperBenchRows)
		defer cleanup()

		targets := make([]database.ScrapeWriteTarget, 10)
		for i := range targets {
			mediaDBID := int64(i + 1)
			targets[i] = database.ScrapeWriteTarget{
				MediaDBID:      mediaDBID,
				MediaTitleDBID: mediaDBID,
				Write: &database.ScrapeWrite{
					Sentinel: database.TagInfo{Type: "scraper.bench", Tag: "scraped"},
				},
			}
			customize(&targets[i])
		}

		b.ResetTimer()
		for b.Loop() {
			if err := mediaDB.ApplyScrapeResults(ctx, targets); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func setupBenchMediaDB(b *testing.B, rows int) (mediaDB *MediaDB, cleanup func()) {
	b.Helper()
	tempDir, err := os.MkdirTemp("", "zaparoo-bench-mediadb-*")
	require.NoError(b, err)

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Settings").Return(platforms.Settings{DataDir: tempDir})

	mediaDB, err = OpenMediaDB(context.Background(), mockPlatform)
	require.NoError(b, err)
	cleanup = func() {
		if mediaDB != nil {
			_ = mediaDB.Close()
		}
		_ = os.RemoveAll(tempDir)
	}

	seedBenchScraperDB(b, mediaDB, rows)
	return mediaDB, cleanup
}

func seedBenchScraperDB(b *testing.B, mediaDB *MediaDB, rows int) {
	b.Helper()
	ctx := context.Background()
	tx, err := mediaDB.sql.BeginTx(ctx, nil)
	require.NoError(b, err)
	defer func() {
		_ = tx.Rollback()
	}()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO TagTypes (Type, IsExclusive) VALUES
			('scraper.bench', 0),
			('developer', 1),
			('publisher', 1),
			('year', 1),
			('rating', 1),
			('genre', 0),
			('property', 0);
		INSERT INTO Tags (TypeDBID, Tag)
		SELECT DBID, 'description' FROM TagTypes WHERE Type = 'property';
		INSERT INTO Tags (TypeDBID, Tag)
		SELECT DBID, 'xml-game-id' FROM TagTypes WHERE Type = 'property';
		INSERT INTO Tags (TypeDBID, Tag)
		SELECT DBID, 'image-boxart' FROM TagTypes WHERE Type = 'property';
		INSERT INTO Systems (DBID, SystemID, Name) VALUES (1, 'Bench', 'Bench');
	`)
	require.NoError(b, err)

	titleStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES (?, 1, ?, ?)
	`)
	require.NoError(b, err)
	defer func() {
		require.NoError(b, titleStmt.Close())
	}()

	mediaStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path) VALUES (?, ?, 1, ?)
	`)
	require.NoError(b, err)
	defer func() {
		require.NoError(b, mediaStmt.Close())
	}()

	for i := 1; i <= rows; i++ {
		slug := fmt.Sprintf("bench-game-%05d", i)
		name := fmt.Sprintf("Bench Game %05d", i)
		path := filepath.ToSlash(filepath.Join("roms", fmt.Sprintf("bench-game-%05d.zip", i)))
		_, err = titleStmt.ExecContext(ctx, i, slug, name)
		require.NoError(b, err)
		_, err = mediaStmt.ExecContext(ctx, i, i, path)
		require.NoError(b, err)
	}

	require.NoError(b, tx.Commit())
}

func benchTitleTags(mediaTitleDBID int64) []database.TagInfo {
	return []database.TagInfo{
		{Type: "developer", Tag: fmt.Sprintf("developer-%02d", mediaTitleDBID%10)},
		{Type: "publisher", Tag: fmt.Sprintf("publisher-%02d", mediaTitleDBID%10)},
		{Type: "year", Tag: fmt.Sprintf("%04d", 1980+mediaTitleDBID%40)},
		{Type: "rating", Tag: "0.8"},
		{Type: "genre", Tag: "action"},
		{Type: "genre", Tag: "arcade"},
	}
}

func benchTitleProps(mediaTitleDBID int64) []database.MediaProperty {
	return []database.MediaProperty{
		{TypeTag: "property:description", Text: fmt.Sprintf("Description for bench game %d", mediaTitleDBID)},
		{TypeTag: "property:xml-game-id", Text: strconv.FormatInt(100000+mediaTitleDBID, 10)},
		{
			TypeTag: "property:image-boxart",
			Text:    filepath.ToSlash(filepath.Join("media", "boxart", fmt.Sprintf("%05d.png", mediaTitleDBID))),
		},
	}
}
