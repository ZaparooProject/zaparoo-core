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

package sqltrace

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	sqlite3 "github.com/mattn/go-sqlite3"
	"github.com/rs/zerolog/log"
)

const (
	logInterval   = 60 * time.Second
	TopStatements = 10
)

// Collector aggregates statement timings reported by SQLite's trace hook for
// every connection it is attached to.
type Collector struct {
	lastLog    time.Time
	statements map[uintptr]string
	stats      map[string]*statementStats
	label      string
	mu         syncutil.Mutex
}

type statementStats struct {
	firstSeen time.Time
	lastSeen  time.Time
	statement string
	operation string
	count     int64
	total     time.Duration
	max       time.Duration
}

// NewCollector returns a collector whose log lines are prefixed with label.
func NewCollector(label string) *Collector {
	return &Collector{
		label:      label,
		statements: make(map[uintptr]string),
		stats:      make(map[string]*statementStats),
	}
}

// Attach installs the collector as the trace callback of conn.
func (c *Collector) Attach(conn *sqlite3.SQLiteConn) error {
	err := conn.SetTrace(&sqlite3.TraceConfig{
		Callback:        c.callback,
		EventMask:       sqlite3.TraceStmt | sqlite3.TraceProfile,
		WantExpandedSQL: false,
	})
	if err != nil {
		return fmt.Errorf("set %s sql trace: %w", c.label, err)
	}
	return nil
}

//nolint:gocritic // signature is fixed by sqlite3.TraceConfig
func (c *Collector) callback(info sqlite3.TraceInfo) int {
	switch info.EventCode {
	case sqlite3.TraceStmt:
		c.recordStatement(info.StmtHandle, info.StmtOrTrigger)
	case sqlite3.TraceProfile:
		c.recordProfile(info.StmtHandle, time.Duration(info.RunTimeNanosec))
	}
	return 0
}

func (c *Collector) recordStatement(handle uintptr, statement string) {
	statement = strings.Join(strings.Fields(statement), " ")
	if statement == "" || strings.HasPrefix(statement, "--") {
		return
	}
	c.mu.Lock()
	c.statements[handle] = statement
	c.mu.Unlock()
}

func (c *Collector) recordProfile(handle uintptr, duration time.Duration) {
	now := time.Now()
	var shouldLog bool

	c.mu.Lock()
	statement := c.statements[handle]
	if statement == "" {
		statement = "<unknown>"
	}
	entry := c.stats[statement]
	if entry == nil {
		entry = &statementStats{
			statement: statement,
			operation: operation(statement),
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
	if c.lastLog.IsZero() || now.Sub(c.lastLog) >= logInterval {
		c.lastLog = now
		shouldLog = true
	}
	c.mu.Unlock()

	if shouldLog {
		c.LogTop(TopStatements)
	}
}

// LogTop logs the limit statements with the highest cumulative run time.
func (c *Collector) LogTop(limit int) {
	c.mu.Lock()
	if len(c.stats) == 0 {
		c.mu.Unlock()
		return
	}
	top := make([]statementStats, 0, len(c.stats))
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
	for i := range limit {
		entry := top[i]
		avg := time.Duration(0)
		if entry.count > 0 {
			avg = entry.total / time.Duration(entry.count)
		}
		log.Debug().
			Int("rank", i+1).
			Str("operation", entry.operation).
			Int64("count", entry.count).
			Float64("total_duration_ms", millis(entry.total)).
			Float64("avg_duration_ms", millis(avg)).
			Float64("max_duration_ms", millis(entry.max)).
			Time("first_seen", entry.firstSeen).
			Time("last_seen", entry.lastSeen).
			Str("sql", entry.statement).
			Msg(c.label + ": sql trace top statement")
	}
}

func millis(duration time.Duration) float64 {
	return float64(duration) / float64(time.Millisecond)
}

func operation(statement string) string {
	fields := strings.Fields(statement)
	if len(fields) == 0 {
		return "unknown"
	}
	return strings.ToUpper(fields[0])
}
