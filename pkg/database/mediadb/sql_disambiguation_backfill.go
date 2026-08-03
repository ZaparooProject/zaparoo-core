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
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/rs/zerolog/log"
)

func sqlDisambiguationVersionCurrent(ctx context.Context, db sqlQueryable) (bool, error) {
	var version string
	err := db.QueryRowContext(ctx,
		"SELECT Value FROM DBConfig WHERE Name = ?",
		DBConfigDisambiguationVersion,
	).Scan(&version)
	if err == nil {
		return version == disambiguationAlgoVersion, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("failed to read disambiguation version: %w", err)
	}
	return false, nil
}

func sqlMarkDisambiguationVersionCurrent(ctx context.Context, db sqlQueryable) error {
	if _, err := db.ExecContext(ctx,
		"INSERT OR REPLACE INTO DBConfig (Name, Value) VALUES (?, ?)",
		DBConfigDisambiguationVersion,
		disambiguationAlgoVersion,
	); err != nil {
		return fmt.Errorf("failed to mark disambiguation version current: %w", err)
	}
	return nil
}

func sqlMediaTitlesExist(ctx context.Context, db sqlQueryable) (bool, error) {
	var exists int
	err := db.QueryRowContext(ctx, `SELECT 1 FROM MediaTitles LIMIT 1`).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to check media titles existence: %w", err)
	}
	return true, nil
}

func sqlAllSystemDBIDs(ctx context.Context, db sqlQueryable) ([]int64, error) {
	rows, err := db.QueryContext(ctx, `SELECT DBID FROM Systems ORDER BY DBID`)
	if err != nil {
		return nil, fmt.Errorf("failed to query system DBIDs: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close system DBID rows")
		}
	}()

	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if scanErr := rows.Scan(&id); scanErr != nil {
			return nil, fmt.Errorf("failed to scan system DBID: %w", scanErr)
		}
		ids = append(ids, id)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("failed to iterate system DBIDs: %w", rowsErr)
	}
	return ids, nil
}

// disambiguationBackfillAfterSystem is a test seam invoked after each system's
// recompute and checkpoint have committed, so tests can interrupt the walk at
// an exact system boundary. Production leaves it nil.
var disambiguationBackfillAfterSystem func(systemDBID int64)

// The backfill checkpoint records the last system whose recompute committed,
// qualified by algorithm version so a cursor left by an older algorithm's
// interrupted walk never truncates a newer one. Each system's recompute can
// take minutes on slow storage, and short power sessions would otherwise
// restart the walk from the first system on every boot.

func sqlGetDisambiguationBackfillCursor(ctx context.Context, db sqlQueryable) (int64, error) {
	var raw string
	err := db.QueryRowContext(ctx,
		"SELECT Value FROM DBConfig WHERE Name = ?",
		DBConfigDisambiguationBackfillCursor,
	).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("failed to read disambiguation backfill cursor: %w", err)
	}
	version, dbidRaw, found := strings.Cut(raw, ":")
	if !found || version != disambiguationAlgoVersion {
		return 0, nil
	}
	dbid, parseErr := strconv.ParseInt(dbidRaw, 10, 64)
	if parseErr != nil {
		// A malformed cursor only costs re-walking already-committed systems.
		log.Warn().Str("cursor", raw).Msg("ignoring malformed disambiguation backfill cursor")
		dbid = 0
	}
	return dbid, nil
}

func sqlSetDisambiguationBackfillCursor(ctx context.Context, db sqlQueryable, systemDBID int64) error {
	if _, err := db.ExecContext(ctx,
		"INSERT OR REPLACE INTO DBConfig (Name, Value) VALUES (?, ?)",
		DBConfigDisambiguationBackfillCursor,
		disambiguationAlgoVersion+":"+strconv.FormatInt(systemDBID, 10),
	); err != nil {
		return fmt.Errorf("failed to set disambiguation backfill cursor: %w", err)
	}
	return nil
}

func sqlClearDisambiguationBackfillCursor(ctx context.Context, db sqlQueryable) error {
	if _, err := db.ExecContext(ctx,
		"DELETE FROM DBConfig WHERE Name = ?",
		DBConfigDisambiguationBackfillCursor,
	); err != nil {
		return fmt.Errorf("failed to clear disambiguation backfill cursor: %w", err)
	}
	return nil
}

// disambiguationBackfillPending reports whether stored DisambiguationTypes were
// computed by an older algorithm and need a one-time recompute. An empty database
// has nothing to backfill, so it is stamped current immediately — the first index
// computes disambiguation with the current algorithm as it inserts titles.
func (db *MediaDB) disambiguationBackfillPending(ctx context.Context) (bool, error) {
	current, err := sqlDisambiguationVersionCurrent(ctx, db.sql.Load())
	if err != nil {
		return false, err
	}
	if current {
		return false, nil
	}
	hasTitles, err := sqlMediaTitlesExist(ctx, db.sql.Load())
	if err != nil {
		return false, err
	}
	if hasTitles {
		return true, nil
	}
	if markErr := sqlMarkDisambiguationVersionCurrent(ctx, db.sql.Load()); markErr != nil {
		return false, markErr
	}
	return false, nil
}

// runDisambiguationBackfill recomputes DisambiguationTypes for every title when
// the stored values predate the current algorithm, then stamps the version. It
// walks systems one at a time — the same granularity indexing used when it
// recomputed per system on every run — so each write transaction stays short and
// the pauser can interleave while a game is running. Progress is checkpointed
// per system, so an interrupted walk resumes where it stopped instead of
// restarting from the first system.
func (db *MediaDB) runDisambiguationBackfill(ctx context.Context, pauser *syncutil.Pauser) error {
	pending, err := db.disambiguationBackfillPending(ctx)
	if err != nil {
		return err
	}
	if !pending {
		log.Debug().Msg("disambiguation backfill skipped: already at current algorithm version")
		return nil
	}

	systemDBIDs, err := sqlAllSystemDBIDs(ctx, db.sql.Load())
	if err != nil {
		return err
	}

	cursor, err := sqlGetDisambiguationBackfillCursor(ctx, db.sql.Load())
	if err != nil {
		log.Warn().Err(err).Msg("failed to read disambiguation backfill checkpoint; starting from the first system")
		cursor = 0
	}

	started := time.Now()
	remaining := 0
	for _, systemDBID := range systemDBIDs {
		if systemDBID > cursor {
			remaining++
		}
	}
	log.Info().Int("systems", len(systemDBIDs)).Int("remaining", remaining).Int64("cursor", cursor).
		Msg("disambiguation backfill started")

	for _, systemDBID := range systemDBIDs {
		// sqlAllSystemDBIDs orders by DBID, so everything at or below the
		// checkpoint already committed in a previous walk.
		if systemDBID <= cursor {
			continue
		}
		if waitErr := pauser.Wait(ctx); waitErr != nil {
			return fmt.Errorf("disambiguation backfill paused: %w", waitErr)
		}
		if recomputeErr := sqlRecomputeDisambiguationForSystems(
			ctx, db.sql.Load(), []int64{systemDBID},
		); recomputeErr != nil {
			return fmt.Errorf("disambiguation backfill for system %d: %w", systemDBID, recomputeErr)
		}
		if checkpointErr := sqlSetDisambiguationBackfillCursor(ctx, db.sql.Load(), systemDBID); checkpointErr != nil {
			// Progress durability only; the walk itself can continue.
			log.Warn().Err(checkpointErr).Int64("systemDBID", systemDBID).
				Msg("failed to checkpoint disambiguation backfill progress")
		}
		if disambiguationBackfillAfterSystem != nil {
			disambiguationBackfillAfterSystem(systemDBID)
		}
	}

	if err := sqlMarkDisambiguationVersionCurrent(ctx, db.sql.Load()); err != nil {
		return err
	}
	if err := sqlClearDisambiguationBackfillCursor(ctx, db.sql.Load()); err != nil {
		log.Warn().Err(err).Msg("failed to clear disambiguation backfill cursor after completion")
	}

	log.Info().
		Int("systems", len(systemDBIDs)).
		Int("processed", remaining).
		Dur("elapsed", time.Since(started)).
		Msg("disambiguation backfill completed")
	return nil
}
