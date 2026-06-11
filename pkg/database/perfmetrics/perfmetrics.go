// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package perfmetrics captures coarse process and SQLite resource counters for
// long media operations. Snapshots are best-effort: missing /proc support or
// PRAGMA failures should never affect indexing or scraping behavior.
package perfmetrics

import (
	"bufio"
	"context"
	"database/sql"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

type ProcessIO struct {
	RChar               int64
	WChar               int64
	Syscr               int64
	Syscw               int64
	ReadBytes           int64
	WriteBytes          int64
	CancelledWriteBytes int64
}

type RuntimeStats struct {
	AllocBytes      uint64
	TotalAllocBytes uint64
	SysBytes        uint64
	HeapAllocBytes  uint64
	HeapSysBytes    uint64
	HeapObjects     uint64
	Mallocs         uint64
	Frees           uint64
	GCCycles        uint32
	GCPauseNs       uint64
	Goroutines      int
	CgoCalls        int64
}

type DBStats struct {
	DBBytes               int64
	WALBytes              int64
	SHMBytes              int64
	PageCount             int64
	FreelistCount         int64
	PageSize              int64
	CacheSize             int64
	WALBusy               int64
	WALFrames             int64
	WALCheckpointedFrames int64
}

type Snapshot struct {
	At      time.Time
	IO      ProcessIO
	Runtime RuntimeStats
	DB      DBStats
	IOOK    bool
	DBOK    bool
}

type Recorder struct {
	dbPath string
	sqlDB  *sql.DB
}

func NewRecorder(dbPath string, sqlDB *sql.DB) Recorder {
	return Recorder{dbPath: dbPath, sqlDB: sqlDB}
}

type DBProvider interface {
	GetDBPath() string
	UnsafeGetSQLDb() *sql.DB
}

func NewRecorderForDB(db DBProvider) (recorder Recorder) {
	defer func() {
		if recover() != nil {
			recorder = Recorder{}
		}
	}()
	if db == nil {
		return Recorder{}
	}
	return NewRecorder(db.GetDBPath(), db.UnsafeGetSQLDb())
}

func (r Recorder) Capture(ctx context.Context, includeDB bool) Snapshot {
	s := Snapshot{At: time.Now()}
	if ioStats, ok := readProcessIO(); ok {
		s.IO = ioStats
		s.IOOK = true
	}
	s.Runtime = readRuntimeStats()
	if includeDB {
		if dbStats, ok := readDBStats(ctx, r.dbPath, r.sqlDB); ok {
			s.DB = dbStats
			s.DBOK = true
		}
	}
	return s
}

func AddDelta(event *zerolog.Event, start Snapshot, end Snapshot) *zerolog.Event {
	event.Dur("wallDuration", end.At.Sub(start.At))

	if start.IOOK && end.IOOK {
		event.
			Int64("procReadBytes", end.IO.ReadBytes-start.IO.ReadBytes).
			Int64("procWriteBytes", end.IO.WriteBytes-start.IO.WriteBytes).
			Int64("procReadSyscalls", end.IO.Syscr-start.IO.Syscr).
			Int64("procWriteSyscalls", end.IO.Syscw-start.IO.Syscw).
			Int64("procReadChars", end.IO.RChar-start.IO.RChar).
			Int64("procWriteChars", end.IO.WChar-start.IO.WChar).
			Int64("procCancelledWriteBytes", end.IO.CancelledWriteBytes-start.IO.CancelledWriteBytes)
	}

	event.
		Uint64("allocBytes", end.Runtime.TotalAllocBytes-start.Runtime.TotalAllocBytes).
		Uint64("mallocs", end.Runtime.Mallocs-start.Runtime.Mallocs).
		Uint64("frees", end.Runtime.Frees-start.Runtime.Frees).
		Uint32("gcCycles", end.Runtime.GCCycles-start.Runtime.GCCycles).
		Uint64("gcPauseNs", end.Runtime.GCPauseNs-start.Runtime.GCPauseNs).
		Int64("cgoCalls", end.Runtime.CgoCalls-start.Runtime.CgoCalls).
		Uint64("heapAllocBytes", end.Runtime.HeapAllocBytes).
		Uint64("heapSysBytes", end.Runtime.HeapSysBytes).
		Uint64("heapObjects", end.Runtime.HeapObjects).
		Int("goroutines", end.Runtime.Goroutines)

	if end.DBOK {
		event.
			Int64("dbBytes", end.DB.DBBytes).
			Int64("dbWalBytes", end.DB.WALBytes).
			Int64("dbShmBytes", end.DB.SHMBytes).
			Int64("dbPageCount", end.DB.PageCount).
			Int64("dbFreelistCount", end.DB.FreelistCount).
			Int64("dbPageSize", end.DB.PageSize).
			Int64("dbCacheSize", end.DB.CacheSize).
			Int64("dbWalBusy", end.DB.WALBusy).
			Int64("dbWalFrames", end.DB.WALFrames).
			Int64("dbWalCheckpointedFrames", end.DB.WALCheckpointedFrames)
	}
	if start.DBOK && end.DBOK {
		event.
			Int64("dbBytesDelta", end.DB.DBBytes-start.DB.DBBytes).
			Int64("dbWalBytesDelta", end.DB.WALBytes-start.DB.WALBytes).
			Int64("dbShmBytesDelta", end.DB.SHMBytes-start.DB.SHMBytes)
	}

	return event
}

func readProcessIO() (ProcessIO, bool) {
	f, err := os.Open("/proc/self/io")
	if err != nil {
		return ProcessIO{}, false
	}
	defer func() { _ = f.Close() }()

	var stats ProcessIO
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		name, raw, ok := strings.Cut(scanner.Text(), ":")
		if !ok {
			continue
		}
		value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err != nil {
			continue
		}
		switch name {
		case "rchar":
			stats.RChar = value
		case "wchar":
			stats.WChar = value
		case "syscr":
			stats.Syscr = value
		case "syscw":
			stats.Syscw = value
		case "read_bytes":
			stats.ReadBytes = value
		case "write_bytes":
			stats.WriteBytes = value
		case "cancelled_write_bytes":
			stats.CancelledWriteBytes = value
		}
	}
	return stats, scanner.Err() == nil
}

func readRuntimeStats() RuntimeStats {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	return RuntimeStats{
		AllocBytes:      mem.Alloc,
		TotalAllocBytes: mem.TotalAlloc,
		SysBytes:        mem.Sys,
		HeapAllocBytes:  mem.HeapAlloc,
		HeapSysBytes:    mem.HeapSys,
		HeapObjects:     mem.HeapObjects,
		Mallocs:         mem.Mallocs,
		Frees:           mem.Frees,
		GCCycles:        mem.NumGC,
		GCPauseNs:       mem.PauseTotalNs,
		Goroutines:      runtime.NumGoroutine(),
		CgoCalls:        runtime.NumCgoCall(),
	}
}

func readDBStats(ctx context.Context, dbPath string, sqlDB *sql.DB) (DBStats, bool) {
	var stats DBStats
	if dbPath != "" {
		stats.DBBytes = fileSize(dbPath)
		stats.WALBytes = fileSize(dbPath + "-wal")
		stats.SHMBytes = fileSize(dbPath + "-shm")
	}
	if sqlDB == nil {
		return stats, dbPath != ""
	}

	if v, ok := querySingleInt(ctx, sqlDB, "PRAGMA page_count;"); ok {
		stats.PageCount = v
	}
	if v, ok := querySingleInt(ctx, sqlDB, "PRAGMA freelist_count;"); ok {
		stats.FreelistCount = v
	}
	if v, ok := querySingleInt(ctx, sqlDB, "PRAGMA page_size;"); ok {
		stats.PageSize = v
	}
	if v, ok := querySingleInt(ctx, sqlDB, "PRAGMA cache_size;"); ok {
		stats.CacheSize = v
	}
	if busy, frames, checkpointed, ok := queryWALStats(ctx, sqlDB); ok {
		stats.WALBusy = busy
		stats.WALFrames = frames
		stats.WALCheckpointedFrames = checkpointed
	}
	return stats, true
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

func querySingleInt(ctx context.Context, db *sql.DB, query string) (int64, bool) {
	var value int64
	if err := db.QueryRowContext(ctx, query).Scan(&value); err != nil {
		return 0, false
	}
	return value, true
}

func queryWALStats(ctx context.Context, db *sql.DB) (int64, int64, int64, bool) {
	var busy, frames, checkpointed int64
	if err := db.QueryRowContext(ctx, "PRAGMA wal_checkpoint(NOOP);").Scan(&busy, &frames, &checkpointed); err != nil {
		return 0, 0, 0, false
	}
	return busy, frames, checkpointed, true
}
