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

//go:build sqlite_trace || trace

package mediadb

import (
	"database/sql"
	"sort"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	sqlite3 "github.com/mattn/go-sqlite3"
	"github.com/rs/zerolog/log"
)

const (
	sqliteTraceRuntimeDriver = "sqlite3_zaparoo_trace"
	sqlTraceLogInterval      = 60 * time.Second
	sqlTraceTopStatements    = 10
)

type runtimeSQLTraceCollector struct {
	mu         syncutil.Mutex
	statements map[uintptr]string
	stats      map[string]*runtimeSQLTraceStatement
	lastLog    time.Time
}

type runtimeSQLTraceStatement struct {
	statement string
	operation string
	count     int64
	total     time.Duration
	max       time.Duration
	firstSeen time.Time
	lastSeen  time.Time
}

var runtimeSQLTrace = newRuntimeSQLTraceCollector()

func init() {
	sql.Register(sqliteTraceRuntimeDriver, &sqlite3.SQLiteDriver{
		ConnectHook: func(conn *sqlite3.SQLiteConn) error {
			if !config.IsDevelopmentVersion() {
				return nil
			}
			return conn.SetTrace(&sqlite3.TraceConfig{
				Callback:        runtimeSQLTrace.callback,
				EventMask:       sqlite3.TraceStmt | sqlite3.TraceProfile,
				WantExpandedSQL: false,
			})
		},
	})
}

func sqliteDriverName() string {
	if config.IsDevelopmentVersion() {
		return sqliteTraceRuntimeDriver
	}
	return "sqlite3"
}

func newRuntimeSQLTraceCollector() *runtimeSQLTraceCollector {
	return &runtimeSQLTraceCollector{
		statements: make(map[uintptr]string),
		stats:      make(map[string]*runtimeSQLTraceStatement),
	}
}

func (c *runtimeSQLTraceCollector) callback(info sqlite3.TraceInfo) int {
	switch info.EventCode {
	case sqlite3.TraceStmt:
		c.recordStatement(info.StmtHandle, info.StmtOrTrigger)
	case sqlite3.TraceProfile:
		c.recordProfile(info.StmtHandle, time.Duration(info.RunTimeNanosec))
	}
	return 0
}

func (c *runtimeSQLTraceCollector) recordStatement(handle uintptr, statement string) {
	statement = normalizeRuntimeSQLStatement(statement)
	if statement == "" || strings.HasPrefix(statement, "--") {
		return
	}
	c.mu.Lock()
	c.statements[handle] = statement
	c.mu.Unlock()
}

func (c *runtimeSQLTraceCollector) recordProfile(handle uintptr, duration time.Duration) {
	now := time.Now()
	var shouldLog bool

	c.mu.Lock()
	statement := c.statements[handle]
	if statement == "" {
		statement = "<unknown>"
	}
	entry := c.stats[statement]
	if entry == nil {
		entry = &runtimeSQLTraceStatement{
			statement: statement,
			operation: sqlOperation(statement),
			firstSeen: now,
		}
		c.stats[statement] = entry
	}
	entry.count++
	entry.total += duration
	if duration > entry.max {
		entry.max = duration
	}
	entry.lastSeen = now
	if c.lastLog.IsZero() || now.Sub(c.lastLog) >= sqlTraceLogInterval {
		c.lastLog = now
		shouldLog = true
	}
	c.mu.Unlock()

	if shouldLog {
		logSQLTraceSummary()
	}
}

func logSQLTraceSummary() {
	runtimeSQLTrace.logTop(sqlTraceTopStatements)
}

func (c *runtimeSQLTraceCollector) logTop(limit int) {
	c.mu.Lock()
	if len(c.stats) == 0 {
		c.mu.Unlock()
		return
	}
	top := make([]runtimeSQLTraceStatement, 0, len(c.stats))
	for _, entry := range c.stats {
		top = append(top, *entry)
	}
	c.mu.Unlock()

	sort.Slice(top, func(i, j int) bool {
		if top[i].total == top[j].total {
			return top[i].count > top[j].count
		}
		return top[i].total > top[j].total
	})
	if limit > len(top) {
		limit = len(top)
	}
	for i := 0; i < limit; i++ {
		entry := top[i]
		avg := time.Duration(0)
		if entry.count > 0 {
			avg = entry.total / time.Duration(entry.count)
		}
		log.Debug().
			Int("rank", i+1).
			Str("operation", entry.operation).
			Int64("count", entry.count).
			Float64("total_duration_ms", durationMillis(entry.total)).
			Float64("avg_duration_ms", durationMillis(avg)).
			Float64("max_duration_ms", durationMillis(entry.max)).
			Time("first_seen", entry.firstSeen).
			Time("last_seen", entry.lastSeen).
			Str("sql", entry.statement).
			Msg("mediadb: sql trace top statement")
	}
}

func durationMillis(duration time.Duration) float64 {
	return float64(duration) / float64(time.Millisecond)
}

func normalizeRuntimeSQLStatement(statement string) string {
	return strings.Join(strings.Fields(statement), " ")
}

func sqlOperation(statement string) string {
	fields := strings.Fields(statement)
	if len(fields) == 0 {
		return "unknown"
	}
	return strings.ToUpper(fields[0])
}
