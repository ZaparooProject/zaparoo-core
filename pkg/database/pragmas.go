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

package database

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// SQLite's PRAGMA synchronous integer values, for comparing against
// EffectivePragmas.Synchronous without a magic number at each call site.
const (
	SynchronousOff    = 0
	SynchronousNormal = 1
	SynchronousFull   = 2
)

// EffectivePragmas is what SQLite actually applied for a connection, read back
// after open rather than trusted from the DSN. A DSN pragma can silently fail
// to take (e.g. a filesystem that does not support WAL falls back to a
// rollback journal), and nothing upstream of this reports that today.
type EffectivePragmas struct {
	JournalMode       string
	Synchronous       int64
	PageSize          int64
	WALAutoCheckpoint int64
	TempStore         int64
	CacheSize         int64
	BusyTimeout       int64
	ForeignKeys       int64
}

// ReadEffectivePragmas queries the pragmas SQLite actually has set on conn.
// Pragmas are per-connection state, so the caller must pass a connection
// pinned via sqlDB.Conn (or the transaction's connection) rather than the
// pool — querying through *sql.DB can silently land on a different physical
// connection than the one being verified.
func ReadEffectivePragmas(ctx context.Context, conn *sql.Conn) (EffectivePragmas, error) {
	var p EffectivePragmas
	if err := conn.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&p.JournalMode); err != nil {
		return p, fmt.Errorf("failed to read journal_mode: %w", err)
	}
	if err := conn.QueryRowContext(ctx, "PRAGMA synchronous").Scan(&p.Synchronous); err != nil {
		return p, fmt.Errorf("failed to read synchronous: %w", err)
	}
	if err := conn.QueryRowContext(ctx, "PRAGMA page_size").Scan(&p.PageSize); err != nil {
		return p, fmt.Errorf("failed to read page_size: %w", err)
	}
	if err := conn.QueryRowContext(ctx, "PRAGMA wal_autocheckpoint").Scan(&p.WALAutoCheckpoint); err != nil {
		return p, fmt.Errorf("failed to read wal_autocheckpoint: %w", err)
	}
	if err := conn.QueryRowContext(ctx, "PRAGMA temp_store").Scan(&p.TempStore); err != nil {
		return p, fmt.Errorf("failed to read temp_store: %w", err)
	}
	if err := conn.QueryRowContext(ctx, "PRAGMA cache_size").Scan(&p.CacheSize); err != nil {
		return p, fmt.Errorf("failed to read cache_size: %w", err)
	}
	if err := conn.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&p.BusyTimeout); err != nil {
		return p, fmt.Errorf("failed to read busy_timeout: %w", err)
	}
	if err := conn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&p.ForeignKeys); err != nil {
		return p, fmt.Errorf("failed to read foreign_keys: %w", err)
	}
	return p, nil
}

// LogEffectivePragmas attaches p's fields to event for a single reportable
// log line proving what SQLite actually applied, as opposed to what the DSN
// asked for.
func LogEffectivePragmas(event *zerolog.Event, p EffectivePragmas) *zerolog.Event {
	return event.
		Str("journalMode", p.JournalMode).
		Int64("synchronous", p.Synchronous).
		Int64("pageSize", p.PageSize).
		Int64("walAutoCheckpoint", p.WALAutoCheckpoint).
		Int64("tempStore", p.TempStore).
		Int64("cacheSize", p.CacheSize).
		Int64("busyTimeout", p.BusyTimeout).
		Int64("foreignKeys", p.ForeignKeys)
}

// LogEffectivePragmasForDB pins a connection from sqlDB, reads back the
// pragmas SQLite actually applied, and logs one line labelled dbLabel. It
// warns instead of the usual info when journal_mode is not "wal" or the
// effective synchronous/page_size do not match what the DSN asked for — a
// DSN pragma can silently fail to take (e.g. a filesystem that falls back to
// a rollback journal instead of WAL), and nothing upstream reported that
// before this. Best-effort: a failure to acquire a connection or read the
// pragmas back is logged and does not fail the open.
func LogEffectivePragmasForDB(
	ctx context.Context, sqlDB *sql.DB, dbLabel string, wantSynchronous, wantPageSize int64,
) {
	if sqlDB == nil {
		log.Warn().Str("db", dbLabel).Msg("no database handle available to verify effective database pragmas")
		return
	}
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		log.Warn().Err(err).Str("db", dbLabel).Msg("failed to acquire connection to verify effective database pragmas")
		return
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Str("db", dbLabel).Msg("failed to release pragma verification connection")
		}
	}()

	pragmas, err := ReadEffectivePragmas(ctx, conn)
	if err != nil {
		log.Warn().Err(err).Str("db", dbLabel).Msg("failed to verify effective database pragmas")
		return
	}

	event := log.Info()
	if pragmas.JournalMode != "wal" || pragmas.Synchronous != wantSynchronous || pragmas.PageSize != wantPageSize {
		event = log.Warn()
	}
	LogEffectivePragmas(event, pragmas).Str("db", dbLabel).Msg("effective database pragmas")
}
