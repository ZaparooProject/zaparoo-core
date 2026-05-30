//go:build sqlite_trace || trace

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package mediadb

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/stretchr/testify/require"

	sqlite3 "github.com/mattn/go-sqlite3"
)

const sqliteTraceBenchDriver = "sqlite3_trace_bench"

var registerTraceBenchDriverOnce sync.Once

type sqlTraceAggregate struct {
	Total time.Duration
	Max   time.Duration
	Count int
}

type sqlTraceCollector struct {
	mu           syncutil.Mutex
	byStmt       map[string]*sqlTraceAggregate
	stmtByHandle map[uintptr]string
}

func BenchmarkApplyScrapeResults_CompanionBatchTrace_10(b *testing.B) {
	b.ReportAllocs()
	collector := &sqlTraceCollector{
		byStmt:       make(map[string]*sqlTraceAggregate),
		stmtByHandle: make(map[uintptr]string),
	}
	mediaDB, cleanup := setupTraceBenchMediaDB(b, scraperBenchRows, collector)
	defer cleanup()

	targets := make([]database.ScrapeWriteTarget, 10)
	for i := range targets {
		mediaDBID := int64(i + 1)
		targets[i] = database.ScrapeWriteTarget{
			MediaDBID:      mediaDBID,
			MediaTitleDBID: mediaDBID,
			Write: &database.ScrapeWrite{
				Sentinel:   database.TagInfo{Type: "scraper.bench", Tag: "scraped"},
				TitleTags:  benchTitleTags(mediaDBID),
				TitleProps: benchTitleProps(mediaDBID),
			},
		}
	}

	ctx := context.Background()
	collector.reset()
	b.ResetTimer()
	for b.Loop() {
		if err := mediaDB.ApplyScrapeResults(ctx, targets); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	collector.logTop(b, 20)
}

func setupTraceBenchMediaDB(b *testing.B, rows int, collector *sqlTraceCollector) (*MediaDB, func()) {
	b.Helper()
	registerTraceBenchDriverOnce.Do(func() {
		sql.Register(sqliteTraceBenchDriver, &sqlite3.SQLiteDriver{
			ConnectHook: func(conn *sqlite3.SQLiteConn) error {
				return conn.SetTrace(&sqlite3.TraceConfig{
					Callback:  collector.callback,
					EventMask: sqlite3.TraceStmt | sqlite3.TraceProfile,
				})
			},
		})
	})

	tempDir, err := os.MkdirTemp("", "zaparoo-trace-bench-mediadb-*")
	require.NoError(b, err)
	dbPath := filepath.Join(tempDir, config.MediaDbFile)
	sqlDB, err := sql.Open(sqliteTraceBenchDriver, dbPath+getSqliteConnParams())
	require.NoError(b, err)
	sqlDB.SetMaxOpenConns(1)

	mediaDB := &MediaDB{sql: sqlDB, dbPath: dbPath, ctx: context.Background()}
	require.NoError(b, mediaDB.Allocate())
	seedBenchScraperDB(b, mediaDB, rows)

	cleanup := func() {
		_ = mediaDB.Close()
		_ = os.RemoveAll(tempDir)
	}
	return mediaDB, cleanup
}

func (c *sqlTraceCollector) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.byStmt = make(map[string]*sqlTraceAggregate)
	c.stmtByHandle = make(map[uintptr]string)
}

func (c *sqlTraceCollector) callback(info sqlite3.TraceInfo) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if info.EventCode == sqlite3.TraceStmt {
		stmt := normalizeSQLTraceStatement(info.StmtOrTrigger)
		if stmt != "" {
			c.stmtByHandle[info.StmtHandle] = stmt
		}
		return 0
	}
	if info.EventCode != sqlite3.TraceProfile {
		return 0
	}
	stmt := c.stmtByHandle[info.StmtHandle]
	if stmt == "" {
		stmt = normalizeSQLTraceStatement(info.StmtOrTrigger)
	}
	if stmt == "" {
		return 0
	}
	duration := time.Duration(info.RunTimeNanosec)
	agg, ok := c.byStmt[stmt]
	if !ok {
		agg = &sqlTraceAggregate{}
		c.byStmt[stmt] = agg
	}
	agg.Count++
	agg.Total += duration
	if duration > agg.Max {
		agg.Max = duration
	}
	return 0
}

func (c *sqlTraceCollector) logTop(b *testing.B, limit int) {
	b.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	type row struct {
		stmt string
		agg  sqlTraceAggregate
	}
	rows := make([]row, 0, len(c.byStmt))
	for stmt, agg := range c.byStmt {
		rows = append(rows, row{stmt: stmt, agg: *agg})
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].agg.Total > rows[j].agg.Total
	})
	if len(rows) < limit {
		limit = len(rows)
	}
	for i := 0; i < limit; i++ {
		agg := rows[i].agg
		avg := time.Duration(0)
		if agg.Count > 0 {
			avg = agg.Total / time.Duration(agg.Count)
		}
		b.Logf("sql-trace rank=%d count=%d total=%s avg=%s max=%s sql=%s",
			i+1, agg.Count, agg.Total, avg, agg.Max, rows[i].stmt)
	}
}

func normalizeSQLTraceStatement(stmt string) string {
	return strings.Join(strings.Fields(stmt), " ")
}
