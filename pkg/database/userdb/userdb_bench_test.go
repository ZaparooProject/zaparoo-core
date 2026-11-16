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

package userdb

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/google/uuid"
)

// newBenchUserDB opens a UserDB with the production connection settings on
// the test's temp directory, so commit costs match a real install.
func newBenchUserDB(b testing.TB) *UserDB {
	b.Helper()
	return newBenchUserDBWithParams(b, sqliteConnParams)
}

func newBenchUserDBWithParams(b testing.TB, connParams string) *UserDB {
	b.Helper()
	pl := mocks.NewMockPlatform()
	pl.On("ID").Return("bench-platform")
	path := filepath.Join(b.TempDir(), "user.db")
	sqlDB, err := sql.Open(sqliteDriverName(), path+connParams)
	if err != nil {
		b.Fatalf("open bench user db: %v", err)
	}
	db := &UserDB{}
	if err := db.SetSQLForTesting(context.Background(), sqlDB, pl); err != nil {
		_ = sqlDB.Close()
		b.Fatalf("allocate bench user db: %v", err)
	}
	b.Cleanup(func() { _ = db.Close() })
	return db
}

var benchSystems = []string{"NES", "SNES", "Genesis", "PSX", "Arcade", "GBA", "Nintendo64", "TurboGrafx16"}

// seedBenchMediaHistory writes rows closed sessions spread over the last year
// in one transaction, returning their DBIDs.
func seedBenchMediaHistory(b *testing.B, db *UserDB, rows int) []int64 {
	b.Helper()
	sqlDB := db.sql.Load()
	tx, err := sqlDB.BeginTx(db.ctx, nil)
	if err != nil {
		b.Fatalf("begin seed tx: %v", err)
	}
	now := time.Now()
	ids := make([]int64, 0, rows)
	for i := range rows {
		system := benchSystems[i%len(benchSystems)]
		start := now.Add(-time.Duration(rows-i) * 9 * time.Minute)
		end := start.Add(8 * time.Minute)
		res, err := tx.ExecContext(db.ctx, `INSERT INTO MediaHistory(
				ID, StartTime, EndTime, SystemID, SystemName, MediaPath, MediaName, LauncherID,
				PlayTime, BootUUID, MonotonicStart, DurationSec, WallDuration, TimeSkewFlag,
				ClockReliable, ClockSource, CreatedAt, UpdatedAt
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, 1, 'system', ?, ?)`,
			uuid.NewString(), start.Unix(), end.Unix(), system, system,
			fmt.Sprintf("/media/fat/games/%s/Game %05d.bin", system, i%2000),
			fmt.Sprintf("Game %05d", i%2000), system, 480, "bench-boot", int64(i), 480, 480,
			start.Unix(), end.Unix())
		if err != nil {
			_ = tx.Rollback()
			b.Fatalf("seed media history: %v", err)
		}
		id, _ := res.LastInsertId()
		ids = append(ids, id)
	}
	if err := tx.Commit(); err != nil {
		b.Fatalf("commit seed tx: %v", err)
	}
	return ids
}

func benchMediaHistoryEntry(i int) *database.MediaHistoryEntry {
	now := time.Now()
	return &database.MediaHistoryEntry{
		ID:            uuid.NewString(),
		StartTime:     now,
		CreatedAt:     now,
		UpdatedAt:     now,
		SystemID:      "NES",
		SystemName:    "NES",
		MediaPath:     fmt.Sprintf("/media/fat/games/NES/Bench %06d.nes", i),
		MediaName:     fmt.Sprintf("Bench %06d", i),
		LauncherID:    "NES",
		BootUUID:      "bench-boot",
		ClockSource:   "system",
		ClockReliable: true,
	}
}

var benchHistorySizes = []int{1_000, 50_000}

func BenchmarkUserDB_AddHistory(b *testing.B) {
	db := newBenchUserDB(b)
	b.ReportAllocs()
	i := 0
	for b.Loop() {
		i++
		now := time.Now()
		//nolint:gosec // token fields are benchmark fixtures, not credentials
		err := db.AddHistory(&database.HistoryEntry{
			ID: uuid.NewString(), Time: now, CreatedAt: now, Type: "ntag215",
			TokenID: fmt.Sprintf("04a1b2c3%06d", i), TokenValue: "@NES/Super Mario Bros. 3",
			Success: true, ClockReliable: true, BootUUID: "bench-boot",
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUserDB_GetEnabledMappings(b *testing.B) {
	for _, n := range []int{0, 50} {
		b.Run(fmt.Sprintf("mappings_%d", n), func(b *testing.B) {
			db := newBenchUserDB(b)
			for i := range n {
				err := db.AddMapping(&database.Mapping{
					Label: fmt.Sprintf("m%d", i), Type: MappingTypeID, Match: MatchTypeExact,
					Pattern: fmt.Sprintf("04a1b2c3%06d", i), Override: "@NES/Game", Enabled: true,
				})
				if err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			for b.Loop() {
				if _, err := db.GetEnabledMappings(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkUserDB_MediaHistorySession(b *testing.B) {
	db := newBenchUserDB(b)
	seedBenchMediaHistory(b, db, 1_000)
	b.ReportAllocs()
	i := 0
	for b.Loop() {
		i++
		id, err := db.AddMediaHistory(benchMediaHistoryEntry(i))
		if err != nil {
			b.Fatal(err)
		}
		if err := db.UpdateMediaHistoryTime(id, 60); err != nil {
			b.Fatal(err)
		}
		if err := db.CloseMediaHistory(id, time.Now(), 120); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUserDB_MediaHistoryHeartbeat(b *testing.B) {
	db := newBenchUserDB(b)
	seedBenchMediaHistory(b, db, 1_000)
	id, err := db.AddMediaHistory(benchMediaHistoryEntry(0))
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	i := 0
	for b.Loop() {
		i++
		if err := db.UpdateMediaHistoryTime(id, i*60); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUserDB_SumMediaPlayTimeForDay(b *testing.B) {
	for _, n := range benchHistorySizes {
		b.Run(fmt.Sprintf("rows_%d", n), func(b *testing.B) {
			db := newBenchUserDB(b)
			seedBenchMediaHistory(b, db, n)
			dayStart := time.Now().Truncate(24 * time.Hour)
			b.ReportAllocs()
			for b.Loop() {
				if _, err := db.SumMediaPlayTimeForDay(dayStart); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkUserDB_GetMediaHistory(b *testing.B) {
	for _, n := range benchHistorySizes {
		b.Run(fmt.Sprintf("rows_%d", n), func(b *testing.B) {
			db := newBenchUserDB(b)
			seedBenchMediaHistory(b, db, n)
			b.Run("all", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					if _, err := db.GetMediaHistory(nil, 0, 50); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("system", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					if _, err := db.GetMediaHistory([]string{"PSX"}, 0, 50); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("distinct", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					if _, err := db.GetDistinctMediaHistory(context.Background(), nil, 0, 50); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("top", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					if _, err := db.GetMediaHistoryTop(nil, nil, 50); err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}

func BenchmarkUserDB_MarkMediaHistorySynced(b *testing.B) {
	const rows = 2_000
	db := newBenchUserDB(b)
	ids := seedBenchMediaHistory(b, db, rows)
	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		if _, err := db.sql.Load().ExecContext(db.ctx, "UPDATE MediaHistory SET SyncedAt = NULL"); err != nil {
			b.Fatal(err)
		}
		batch, err := db.GetMediaHistorySyncBatch(time.Time{}, 0, rows)
		if err != nil {
			b.Fatal(err)
		}
		refs := make([]database.MediaHistorySyncRef, 0, len(batch))
		for i := range batch {
			refs = append(refs, database.MediaHistorySyncRef{DBID: batch[i].DBID, UpdatedAt: batch[i].UpdatedAt})
		}
		if len(refs) != len(ids) {
			b.Fatalf("sync batch returned %d of %d rows", len(refs), len(ids))
		}
		b.StartTimer()
		if err := db.MarkMediaHistorySynced(refs, time.Now()); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkUserDB_HistoryIndexBuild prices the index builds that the
// media_history_read_indexes migration runs at startup on an existing install.
func BenchmarkUserDB_HistoryIndexBuild(b *testing.B) {
	for _, n := range benchHistorySizes {
		b.Run(fmt.Sprintf("rows_%d", n), func(b *testing.B) {
			db := newBenchUserDB(b)
			seedBenchMediaHistory(b, db, n)
			sqlDB := db.sql.Load()
			for b.Loop() {
				b.StopTimer()
				_, err := sqlDB.ExecContext(db.ctx,
					"DROP INDEX idx_media_history_media; DROP INDEX idx_media_history_end_time")
				if err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
				_, err = sqlDB.ExecContext(db.ctx,
					`CREATE INDEX idx_media_history_media ON MediaHistory (MediaPath, SystemID, DBID);
					CREATE INDEX idx_media_history_end_time ON MediaHistory (EndTime)`)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkUserDB_CommitSynchronous prices what synchronous=FULL adds to one
// small commit on the storage under TMPDIR. It exists to measure, not to
// choose: the user database keeps FULL.
func BenchmarkUserDB_CommitSynchronous(b *testing.B) {
	for _, mode := range []string{"FULL", "NORMAL"} {
		b.Run(mode, func(b *testing.B) {
			params := strings.Replace(sqliteConnParams, "_synchronous=FULL", "_synchronous="+mode, 1)
			db := newBenchUserDBWithParams(b, params)
			id, err := db.AddMediaHistory(benchMediaHistoryEntry(0))
			if err != nil {
				b.Fatal(err)
			}
			i := 0
			for b.Loop() {
				i++
				if err := db.UpdateMediaHistoryTime(id, i*60); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
