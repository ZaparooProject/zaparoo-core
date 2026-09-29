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
	"errors"
	"fmt"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/sessionevidence"
)

// ApplyExternalEvidence commits the normalized observations, all derived
// segments, the compatible history summary and the high-water mark together.
// No incomplete, revoked, gapped or cross-boot batch is allowed to advance
// it. A session already superseded by a later recorded launch is closed at
// that bound rather than left open past it, no matter what the evidence says.
func (db *UserDB) ApplyExternalEvidence(ctx context.Context, batch *database.ForegroundEvidence) (bool, error) {
	if db.sql.Load() == nil {
		return false, ErrNullSQL
	}
	if batch == nil || batch.Version != 1 || batch.LaunchID == "" || batch.BootID == "" ||
		batch.Permission != "granted" || !batch.Complete || batch.QueryFromMs < 0 ||
		batch.QueryToMs < batch.QueryFromMs || batch.ObservedElapsedMs <= 0 ||
		batch.ObservedWallMs < batch.QueryToMs || len(batch.Events) > 4096 {
		return false, errors.New("invalid or incomplete external evidence query")
	}
	tx, err := db.sql.Load().BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin external evidence transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var session database.ExternalSession
	var profile sql.NullString
	var historyID, cursor, cursorElapsed, superseded sql.NullInt64
	var identity string
	err = tx.QueryRowContext(ctx, `SELECT SystemID, SystemName, MediaPath, MediaName, MediaIdentity, LauncherID,
		ProfileID, Target, BootID, Source, Status,
		RequestedMs, RequestedElapsedMs, MediaHistoryDBID, CursorMs, CursorElapsedMs, SupersededMs
		FROM ExternalSessions WHERE LaunchID = ?`, batch.LaunchID).Scan(
		&session.SystemID, &session.SystemName, &session.MediaPath, &session.MediaName,
		&identity, &session.LauncherID, &profile, &session.Target,
		&session.BootID, &session.Source, &session.Status, &session.RequestedMs,
		&session.RequestedElapsedMs, &historyID, &cursor, &cursorElapsed, &superseded,
	)
	if err != nil {
		return false, fmt.Errorf("load external launch for evidence: %w", err)
	}
	session.MediaIdentity = database.DecodeMediaIdentity(identity)
	if session.Source != "foreground_events" || batch.BootID != session.BootID ||
		batch.QueryFromMs > session.RequestedMs && !cursor.Valid ||
		cursor.Valid && batch.QueryFromMs > cursor.Int64 ||
		batch.QueryToMs < session.RequestedMs {
		return false, errors.New("external evidence has a boot or query gap")
	}
	if session.Status == "stale" || session.Status == "abandoned" || session.Status == "closed" {
		return false, errors.New("external evidence cannot revive a terminal session")
	}
	// A regressed or diverged clock is not a transient query problem that a
	// later batch could resolve: it is proof this session's remaining
	// evidence can never be trusted, so it is retired now rather than
	// retried identically, and uselessly, on every later reconciliation pass.
	if cursorElapsed.Valid && batch.ObservedElapsedMs < cursorElapsed.Int64 {
		_ = tx.Rollback()
		staled, staleErr := db.MarkExternalSessionStale(ctx, batch.LaunchID, batch.ObservedWallMs)
		if staleErr != nil {
			return false, fmt.Errorf("retire external session with a regressed elapsed clock: %w", staleErr)
		}
		return staled, nil
	}
	if session.RequestedElapsedMs > 0 {
		wallDuration := batch.ObservedWallMs - session.RequestedMs
		elapsedDuration := batch.ObservedElapsedMs - session.RequestedElapsedMs
		if elapsedDuration < 0 || wallDuration-elapsedDuration > 3000 ||
			elapsedDuration-wallDuration > 3000 {
			_ = tx.Rollback()
			staled, staleErr := db.MarkExternalSessionStale(ctx, batch.LaunchID, batch.ObservedWallMs)
			if staleErr != nil {
				return false, fmt.Errorf("retire external session with diverged clocks: %w", staleErr)
			}
			return staled, nil
		}
	}
	if cursor.Valid && batch.QueryToMs <= cursor.Int64 {
		return false, nil
	}
	lastTime := session.RequestedMs
	lastSequence := -1
	for _, event := range batch.Events {
		if event.TimestampMs < session.RequestedMs || event.TimestampMs < lastTime ||
			event.TimestampMs > batch.QueryToMs || event.Sequence <= lastSequence ||
			event.Sequence > 4095 || event.Kind == "" || len(event.Activity) > 512 {
			return false, errors.New("invalid or out-of-order external evidence")
		}
		lastTime, lastSequence = event.TimestampMs, event.Sequence
		switch sessionevidence.ForegroundKind(event.Kind) {
		case sessionevidence.TargetResumed, sessionevidence.TargetPaused, sessionevidence.TargetStopped,
			sessionevidence.OtherResumed, sessionevidence.ScreenInteractive,
			sessionevidence.ScreenNonInteractive, sessionevidence.KeyguardShown,
			sessionevidence.KeyguardHidden:
		default:
			return false, errors.New("unknown external foreground event")
		}
		if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO ExternalSessionEvidence
			(LaunchID, TimestampMs, Kind, TargetActivity) VALUES (?, ?, ?, ?)`,
			batch.LaunchID, event.TimestampMs, event.Kind, event.Activity); err != nil {
			return false, fmt.Errorf("persist external foreground evidence: %w", err)
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT TimestampMs, Kind
		FROM ExternalSessionEvidence WHERE LaunchID = ? ORDER BY TimestampMs, EvidenceID`, batch.LaunchID)
	if err != nil {
		return false, fmt.Errorf("replay external foreground evidence: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var events []sessionevidence.ForegroundEvent
	for rows.Next() {
		var event sessionevidence.ForegroundEvent
		if err = rows.Scan(&event.TimestampMs, &event.Kind); err != nil {
			return false, fmt.Errorf("scan external foreground evidence: %w", err)
		}
		events = append(events, event)
	}
	if err = rows.Err(); err != nil {
		return false, fmt.Errorf("iterate external foreground evidence: %w", err)
	}
	if err = rows.Close(); err != nil {
		return false, fmt.Errorf("close external foreground evidence cursor: %w", err)
	}
	var hardCloseAtMs int64
	queryEndMs := batch.QueryToMs
	if superseded.Valid {
		hardCloseAtMs = superseded.Int64
		if queryEndMs > hardCloseAtMs {
			queryEndMs = hardCloseAtMs
		}
		// The host cannot know a later event belongs to a title it dispatched
		// after this one: a supersede is proof this session's story ends at
		// hardCloseAtMs, so anything retained past it is simply not this
		// session's evidence, not an out-of-order batch to reject.
		kept := events[:0]
		for _, event := range events {
			if event.TimestampMs <= queryEndMs {
				kept = append(kept, event)
			}
		}
		events = kept
	}
	result, err := sessionevidence.ReconcileForeground(session.RequestedMs, queryEndMs, hardCloseAtMs, events)
	if err != nil {
		return false, fmt.Errorf("reconcile external foreground evidence: %w", err)
	}
	if _, err = tx.ExecContext(ctx,
		`DELETE FROM ExternalSessionSegments WHERE LaunchID = ?`, batch.LaunchID); err != nil {
		return false, fmt.Errorf("replace external segments: %w", err)
	}
	for i, segment := range result.Segments {
		if _, err = tx.ExecContext(ctx, `INSERT INTO ExternalSessionSegments
			(LaunchID, Ordinal, StartedMs, EndedMs, PolicyVersion) VALUES (?, ?, ?, ?, ?)`,
			batch.LaunchID, i, segment.StartMs, segment.EndMs, sessionevidence.PolicyVersion); err != nil {
			return false, fmt.Errorf("persist external segment: %w", err)
		}
	}
	if result.Confirmed && len(result.Segments) > 0 {
		seconds := result.ClosedDurationMs() / 1000
		var endMs *int64
		if result.Status == "closed" {
			endMs = &result.ClosedMs
		}
		if !historyID.Valid {
			historyID.Int64, err = insertExternalHistory(ctx, tx, batch.LaunchID, result.ConfirmedMs,
				endMs, seconds, "foreground_events", "exact")
			if err != nil {
				return false, err
			}
			historyID.Valid = true
		} else {
			var end any
			if endMs != nil {
				end = *endMs / 1000
			}
			now := time.Now().Unix()
			// A provisional start becomes exact here: the confirmed resume
			// replaces the dispatch time as its start.
			_, err = tx.ExecContext(ctx, `UPDATE MediaHistory SET PlayTime = ?, DurationSec = ?, EndTime = ?,
				StartTime = ?, SessionConfidence = 'exact', IsDeleted = 0,
				UpdatedAt = MAX(UpdatedAt + 1, ?), SyncedAt = NULL
				WHERE DBID = ? AND SessionSource = 'foreground_events'
				  AND SessionConfidence IN ('exact', 'provisional')`,
				seconds, seconds, end, result.ConfirmedMs/1000, now, historyID.Int64)
			if err != nil {
				return false, fmt.Errorf("update external history: %w", err)
			}
		}
	}
	status := result.Status
	if !result.Confirmed && status != "abandoned" && batch.QueryToMs-session.RequestedMs > 30_000 {
		status = "abandoned"
	}
	// A launch that ends without a closed, confirmed segment withdraws the
	// in-progress row its dispatch published.
	if historyID.Valid && len(result.Segments) == 0 &&
		(status == "closed" || status == "abandoned" || status == "stale") {
		if retractErr := retractProvisionalExternalHistory(ctx, tx, historyID.Int64); retractErr != nil {
			return false, retractErr
		}
	}
	var confirmed, ended any
	if result.Confirmed {
		confirmed = result.ConfirmedMs
	}
	if result.ClosedMs > 0 {
		ended = result.ClosedMs
	}
	if _, err = tx.ExecContext(ctx, `UPDATE ExternalSessions SET Status = ?, ConfirmedMs = ?,
		EndedMs = ?, CursorMs = ?, CursorElapsedMs = ?, MediaHistoryDBID = ?, UpdatedMs = ?
		WHERE LaunchID = ?`,
		status, confirmed, ended, batch.QueryToMs, batch.ObservedElapsedMs,
		historyID, batch.QueryToMs, batch.LaunchID); err != nil {
		return false, fmt.Errorf("advance external evidence cursor: %w", err)
	}
	if status == "closed" || status == "abandoned" || status == "stale" {
		if pruneErr := deleteExternalEvidence(ctx, tx, batch.LaunchID); pruneErr != nil {
			return false, pruneErr
		}
	}
	if err = tx.Commit(); err != nil {
		return false, fmt.Errorf("commit external evidence: %w", err)
	}
	return true, nil
}
