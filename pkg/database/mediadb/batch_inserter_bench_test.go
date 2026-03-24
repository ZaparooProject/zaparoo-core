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
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	_ "github.com/mattn/go-sqlite3"
)

// newBenchSQLite creates a temp-file SQLite database with the production schema
// (via SetSQLForTesting which runs real migrations) and returns the raw *sql.DB
// for direct batch inserter testing.
func newBenchSQLite(b *testing.B) (sqlDB *sql.DB, cleanup func()) {
	b.Helper()
	dbPath := filepath.Join(b.TempDir(), "bench.db")
	sqlDB, err := sql.Open("sqlite3", dbPath+"?_foreign_keys=ON")
	if err != nil {
		b.Fatal(err)
	}

	// Use SetSQLForTesting to run real migrations and get the production schema
	db := &MediaDB{}
	ctx := context.Background()
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform")
	if err := db.SetSQLForTesting(ctx, sqlDB, mockPlatform); err != nil {
		b.Fatal(err)
	}
	// Close the MediaDB wrapper but keep the raw *sql.DB open for direct use
	db.sql = nil // Prevent Close() from closing the underlying connection

	return sqlDB, func() { _ = sqlDB.Close() }
}

// seedBenchData inserts a system and tag type needed by batch inserter benchmarks.
func seedBenchData(ctx context.Context, b *testing.B, tx *sql.Tx) {
	b.Helper()
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO Systems (DBID, SystemID, Name) VALUES (1, 'nes', 'NES')"); err != nil {
		b.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO TagTypes (DBID, Type) VALUES (1, 'extension')"); err != nil {
		b.Fatal(err)
	}
}

var titleCols = []string{
	"DBID", "SystemDBID", "Slug", "Name",
	"SlugLength", "SlugWordCount", "SecondarySlug",
}

func BenchmarkBatchInserter_Insert(b *testing.B) {
	sizes := []struct {
		name string
		n    int
	}{
		{name: "10k", n: 10_000},
		{name: "50k", n: 50_000},
	}

	for _, sz := range sizes {
		b.Run(sz.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				db, cleanup := newBenchSQLite(b)
				ctx := context.Background()
				tx, err := db.BeginTx(ctx, nil)
				if err != nil {
					b.Fatal(err)
				}

				// Seed a system and tag type
				seedBenchData(ctx, b, tx)

				// Create batch inserters with production dependency graph
				biTitles, err := NewBatchInserterWithOptions(
					ctx, tx, "MediaTitles", titleCols, 5000, false)
				if err != nil {
					b.Fatal(err)
				}
				biMedia, err := NewBatchInserterWithOptions(ctx, tx, "Media",
					[]string{"DBID", "MediaTitleDBID", "SystemDBID", "Path"}, 5000, false)
				if err != nil {
					b.Fatal(err)
				}
				biTags, err := NewBatchInserterWithOptions(ctx, tx, "Tags",
					[]string{"DBID", "TypeDBID", "Tag"}, 5000, false)
				if err != nil {
					b.Fatal(err)
				}
				biMediaTags, err := NewBatchInserterWithOptions(ctx, tx, "MediaTags",
					[]string{"MediaDBID", "TagDBID"}, 5000, true)
				if err != nil {
					b.Fatal(err)
				}

				biMedia.SetDependencies(biTitles)
				biMediaTags.SetDependencies(biMedia, biTags)

				// Insert titles, media, tags, and links
				for i := range sz.n {
					id := int64(i + 1)
					slug := fmt.Sprintf("game-%d", i)

					_ = biTitles.Add(id, int64(1), slug, slug, len(slug), 1, nil)
					_ = biMedia.Add(id, id, int64(1), fmt.Sprintf("/roms/nes/%s.nes", slug))

					if i < 100 { // First 100 unique tags
						_ = biTags.Add(id, int64(1), fmt.Sprintf("ext-%d", i))
					}
					_ = biMediaTags.Add(id, int64((i%100)+1))
				}

				_ = biTitles.Close()
				_ = biMedia.Close()
				_ = biTags.Close()
				_ = biMediaTags.Close()
				_ = tx.Commit()
				cleanup()
			}
		})
	}
}

func BenchmarkBatchInserter_FlushCost(b *testing.B) {
	sizes := []struct {
		name string
		n    int
	}{
		{name: "100rows", n: 100},
		{name: "1000rows", n: 1000},
		{name: "5000rows", n: 5000},
	}

	for _, sz := range sizes {
		b.Run(sz.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				db, cleanup := newBenchSQLite(b)
				ctx := context.Background()
				tx, err := db.BeginTx(ctx, nil)
				if err != nil {
					b.Fatal(err)
				}

				_, err = tx.ExecContext(ctx,
					"INSERT INTO Systems (DBID, SystemID, Name) VALUES (1, 'nes', 'NES')")
				if err != nil {
					b.Fatal(err)
				}

				// batch size > n so no auto-flush
				bi, err := NewBatchInserterWithOptions(
					ctx, tx, "MediaTitles", titleCols, sz.n+1, false)
				if err != nil {
					b.Fatal(err)
				}

				for i := range sz.n {
					slug := fmt.Sprintf("game-%d", i)
					_ = bi.Add(int64(i+1), int64(1), slug, slug, len(slug), 1, nil)
				}

				// Measure: the actual flush
				_ = bi.Flush()
				_ = tx.Commit()
				cleanup()
			}
		})
	}
}

func BenchmarkTransactionCycle_CommitCost(b *testing.B) {
	sizes := []struct {
		name string
		n    int
	}{
		{name: "1k", n: 1_000},
		{name: "5k", n: 5_000},
		{name: "10k", n: 10_000},
	}

	for _, sz := range sizes {
		b.Run(sz.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				db, cleanup := newBenchSQLite(b)
				ctx := context.Background()
				tx, err := db.BeginTx(ctx, nil)
				if err != nil {
					b.Fatal(err)
				}

				_, err = tx.ExecContext(ctx,
					"INSERT INTO Systems (DBID, SystemID, Name) VALUES (1, 'nes', 'NES')")
				if err != nil {
					b.Fatal(err)
				}

				bi, err := NewBatchInserterWithOptions(
					ctx, tx, "MediaTitles", titleCols, 5000, false)
				if err != nil {
					b.Fatal(err)
				}

				for i := range sz.n {
					slug := fmt.Sprintf("game-%d", i)
					_ = bi.Add(int64(i+1), int64(1), slug, slug, len(slug), 1, nil)
				}

				// Measure: flush + commit
				_ = bi.Close()
				_ = tx.Commit()
				cleanup()
			}
		})
	}
}
