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
	"github.com/google/uuid"
)

var _ database.ExternalSessionStore = (*UserDB)(nil)

// BeginExternalSession durably records a selected title before its host
// intent is dispatched. This does not create a MediaHistory row or confirm
// playtime. Any older unresolved session of the same boot is superseded: a
// later recorded launch is proof its target can no longer be current,
// whether or not the older one ever got evidence to say so.
func (db *UserDB) BeginExternalSession(ctx context.Context, session *database.ExternalSession) error {
	if session == nil {
		return errors.New("missing pending external session")
	}
	if session.LaunchID == "" || session.RequestedMs <= 0 || session.SystemID == "" ||
		session.SystemName == "" || session.MediaPath == "" || session.MediaName == "" ||
		session.LauncherID == "" || session.Target == "" || session.BootID == "" ||
		session.Status != "pending" || session.DispatchedMs != nil || session.CursorMs != nil ||
		(session.Source != "foreground_events" && session.Source != "host_return") {
		return errors.New("invalid pending external session")
	}
	if session.RequestedElapsedMs < 0 {
		return errors.New("invalid launch monotonic time")
	}
	tx, err := db.sql.Load().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin pending external session: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO ExternalSessions (
			LaunchID, SystemID, SystemName, MediaPath, MediaName, MediaIdentity, LauncherID,
			ProfileID, Target, BootID,
			Status, Source, RequestedMs, RequestedElapsedMs,
			InitialInteractive, InitialUnlocked, UpdatedMs
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?, ?, ?, ?, ?)`,
		session.LaunchID, session.SystemID, session.SystemName, session.MediaPath,
		session.MediaName, database.EncodeMediaIdentity(session.MediaIdentity), session.LauncherID,
		session.ProfileID, session.Target, session.BootID, session.Source,
		session.RequestedMs, session.RequestedElapsedMs,
		session.InitialInteractive, session.InitialUnlocked, session.RequestedMs)
	if err != nil {
		return fmt.Errorf("persist pending external session: %w", err)
	}
	if session.Source == "foreground_events" {
		for _, event := range []struct {
			kind   string
			active bool
		}{
			{"screen_interactive", session.InitialInteractive},
			{"keyguard_hidden", session.InitialUnlocked},
		} {
			if !event.active {
				continue
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO ExternalSessionEvidence
				(LaunchID, TimestampMs, Kind) VALUES (?, ?, ?)`,
				session.LaunchID, session.RequestedMs, event.kind); err != nil {
				return fmt.Errorf("persist initial external foreground state: %w", err)
			}
		}
	}
	supersedeErr := supersedeOlderExternalSessions(ctx, tx, session.BootID, session.LaunchID, session.RequestedMs)
	if supersedeErr != nil {
		return supersedeErr
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit pending external session: %w", err)
	}
	return nil
}

// supersedeOlderExternalSessions marks every other unresolved session of the
// same boot as superseded at atMs: a newly recorded launch proves the
// previous one's target is no longer current, even if it never confirmed or
// its host evidence never says so.
func supersedeOlderExternalSessions(ctx context.Context, tx *sql.Tx, bootID, launchID string, atMs int64) error {
	if _, err := tx.ExecContext(ctx, `UPDATE ExternalSessions SET SupersededMs = ?, UpdatedMs = ?
		WHERE BootID = ? AND LaunchID != ? AND SupersededMs IS NULL
		  AND Status IN ('pending', 'confirmed', 'active', 'suspended')`,
		atMs, atMs, bootID, launchID); err != nil {
		return fmt.Errorf("supersede prior external sessions: %w", err)
	}
	return nil
}

// RecordExternalDispatch records the host receipt. A foreground_events launch
// also gets a provisional history row with no end, so connected clients and a
// linked Online account see it as in progress; reconciliation later makes it
// exact or retracts it. A receipt never records playtime, and a timed-out
// dispatch stays pending; Core must reconcile it before retrying.
func (db *UserDB) RecordExternalDispatch(ctx context.Context, launchID string, dispatchedMs int64) (bool, error) {
	if launchID == "" || dispatchedMs <= 0 {
		return false, errors.New("invalid external dispatch receipt")
	}
	tx, err := db.sql.Load().BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin external dispatch receipt: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `
		UPDATE ExternalSessions SET DispatchedMs = ?, UpdatedMs = ?
		WHERE LaunchID = ? AND Status = 'pending' AND DispatchedMs IS NULL AND RequestedMs <= ?`,
		dispatchedMs, dispatchedMs, launchID, dispatchedMs)
	if err != nil {
		return false, fmt.Errorf("record external dispatch: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read external dispatch result: %w", err)
	}
	if changed != 1 {
		return false, nil
	}
	var source string
	if err = tx.QueryRowContext(ctx, `SELECT Source FROM ExternalSessions WHERE LaunchID = ?`,
		launchID).Scan(&source); err != nil {
		return false, fmt.Errorf("load dispatched external session: %w", err)
	}
	if source == "foreground_events" {
		historyID, insertErr := insertExternalHistory(ctx, tx, launchID, dispatchedMs, nil, 0,
			"foreground_events", "provisional")
		if insertErr != nil {
			return false, insertErr
		}
		if _, err = tx.ExecContext(ctx, `UPDATE ExternalSessions SET MediaHistoryDBID = ?
			WHERE LaunchID = ?`, historyID, launchID); err != nil {
			return false, fmt.Errorf("link provisional external history: %w", err)
		}
	}
	if err = tx.Commit(); err != nil {
		return false, fmt.Errorf("commit external dispatch receipt: %w", err)
	}
	return true, nil
}

// insertExternalHistory writes the MediaHistory summary of one externally-
// timed launch from its persisted snapshot. endMs is nil for a session still
// in progress.
func insertExternalHistory(
	ctx context.Context, tx *sql.Tx, launchID string, startMs int64, endMs *int64, seconds int64,
	source, confidence string,
) (int64, error) {
	var systemID, systemName, mediaPath, mediaName, identity, launcherID, bootID string
	var profile sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT SystemID, SystemName, MediaPath, MediaName,
		MediaIdentity, LauncherID, ProfileID, BootID FROM ExternalSessions WHERE LaunchID = ?`,
		launchID).Scan(&systemID, &systemName, &mediaPath, &mediaName, &identity, &launcherID,
		&profile, &bootID); err != nil {
		return 0, fmt.Errorf("load external history snapshot: %w", err)
	}
	decoded := database.DecodeMediaIdentity(identity)
	var tags []string
	if decoded != nil {
		tags = decoded.LegacyTags()
	}
	var profileID *string
	if profile.Valid {
		profileID = &profile.String
	}
	var end *time.Time
	if endMs != nil {
		endTime := time.UnixMilli(*endMs)
		end = &endTime
	}
	clockSource := "host_usage_events"
	if source == "host_return" {
		clockSource = "host_launcher_return"
	}
	now := time.Now()
	entry := &database.MediaHistoryEntry{
		ID: uuid.NewString(), StartTime: time.UnixMilli(startMs), EndTime: end,
		SystemID: systemID, SystemName: systemName, MediaPath: mediaPath, MediaName: mediaName,
		LauncherID: launcherID, PlayTime: int(seconds), DurationSec: int(seconds),
		BootUUID: bootID, ClockSource: clockSource, ClockReliable: true,
		CreatedAt: now, UpdatedAt: now, ProfileID: profileID, MediaIdentity: decoded, Tags: tags,
		SessionSource: source, SessionConfidence: confidence,
	}
	id, err := sqlAddMediaHistory(ctx, tx, entry)
	if err != nil {
		return 0, fmt.Errorf("materialize external history: %w", err)
	}
	return id, nil
}

// retractProvisionalExternalHistory withdraws an in-progress row whose launch
// was never confirmed. It is kept as deleted, not removed, so a sync that
// already reported the start also reports its withdrawal.
func retractProvisionalExternalHistory(ctx context.Context, tx *sql.Tx, historyID int64) error {
	if _, err := tx.ExecContext(ctx, `UPDATE MediaHistory SET IsDeleted = 1,
		UpdatedAt = MAX(UpdatedAt + 1, ?), SyncedAt = NULL
		WHERE DBID = ? AND SessionConfidence = 'provisional'`,
		time.Now().Unix(), historyID); err != nil {
		return fmt.Errorf("retract provisional external history: %w", err)
	}
	return nil
}

// deleteExternalEvidence prunes raw evidence once a session is terminal: the
// derived segments and history row are what remain durable, and there is no
// further replay to keep it for.
func deleteExternalEvidence(ctx context.Context, tx *sql.Tx, launchID string) error {
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM ExternalSessionEvidence WHERE LaunchID = ?`, launchID); err != nil {
		return fmt.Errorf("prune external session evidence: %w", err)
	}
	return nil
}

// AbandonExternalSession is only for a known failed preflight or dispatch. An
// unknown outcome must remain pending for evidence reconciliation.
func (db *UserDB) AbandonExternalSession(ctx context.Context, launchID string, failedMs int64) (bool, error) {
	if launchID == "" || failedMs <= 0 {
		return false, errors.New("invalid external dispatch failure")
	}
	tx, err := db.sql.Load().BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin external session abandonment: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `
		UPDATE ExternalSessions SET Status = 'abandoned', EndedMs = ?, UpdatedMs = ?
		WHERE LaunchID = ? AND Status = 'pending' AND DispatchedMs IS NULL AND RequestedMs <= ?`,
		failedMs, failedMs, launchID, failedMs)
	if err != nil {
		return false, fmt.Errorf("abandon external session: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read external abandonment result: %w", err)
	}
	if changed != 1 {
		return false, nil
	}
	if pruneErr := deleteExternalEvidence(ctx, tx, launchID); pruneErr != nil {
		return false, pruneErr
	}
	if err = tx.Commit(); err != nil {
		return false, fmt.Errorf("commit external session abandonment: %w", err)
	}
	return true, nil
}

// MarkExternalSessionStale closes a session after reboot, permission loss or
// a retention gap without inventing any foreground interval. Exact history
// retains only its last proven closed segment; a never-confirmed launch
// creates no history.
func (db *UserDB) MarkExternalSessionStale(ctx context.Context, launchID string, atMs int64) (bool, error) {
	if launchID == "" || atMs <= 0 {
		return false, errors.New("invalid stale external session")
	}
	tx, err := db.sql.Load().BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin stale external session: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var historyID, closed sql.NullInt64
	var status string
	if err = tx.QueryRowContext(ctx, `SELECT Status, MediaHistoryDBID,
		(SELECT MAX(EndedMs) FROM ExternalSessionSegments WHERE LaunchID = ExternalSessions.LaunchID)
		FROM ExternalSessions WHERE LaunchID = ?`, launchID).Scan(&status, &historyID, &closed); err != nil {
		return false, fmt.Errorf("load stale external session: %w", err)
	}
	if status == "stale" || status == "abandoned" || status == "closed" {
		return false, nil
	}
	result, err := tx.ExecContext(ctx, `UPDATE ExternalSessions SET Status = 'stale', EndedMs = ?,
		UpdatedMs = ? WHERE LaunchID = ? AND Status IN ('pending', 'confirmed', 'active', 'suspended')`,
		closed, atMs, launchID)
	if err != nil {
		return false, fmt.Errorf("mark external session stale: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read stale external session result: %w", err)
	}
	if changed == 1 && historyID.Valid && !closed.Valid {
		if retractErr := retractProvisionalExternalHistory(ctx, tx, historyID.Int64); retractErr != nil {
			return false, retractErr
		}
	}
	if changed == 1 && historyID.Valid && closed.Valid {
		if _, err = tx.ExecContext(ctx, `UPDATE MediaHistory SET EndTime = ?,
			UpdatedAt = MAX(UpdatedAt + 1, ?), SyncedAt = NULL
			WHERE DBID = ? AND SessionSource = 'foreground_events' AND SessionConfidence = 'exact'`,
			closed.Int64/1000, atMs/1000, historyID.Int64); err != nil {
			return false, fmt.Errorf("close stale external exact history: %w", err)
		}
	}
	if changed == 1 {
		if pruneErr := deleteExternalEvidence(ctx, tx, launchID); pruneErr != nil {
			return false, pruneErr
		}
	}
	if err = tx.Commit(); err != nil {
		return false, fmt.Errorf("commit stale external session: %w", err)
	}
	return changed == 1, nil
}

// UnresolvedExternalSessions survives both Core and host restarts. The caller
// must recheck host permission and boot identity before asking for evidence.
func (db *UserDB) UnresolvedExternalSessions(ctx context.Context) ([]database.ExternalSession, error) {
	rows, err := db.sql.Load().QueryContext(ctx, `
		SELECT LaunchID, SystemID, SystemName, MediaPath, MediaName, MediaIdentity, LauncherID,
			ProfileID, Target, BootID,
			Status, Source, RequestedMs, DispatchedMs, CursorMs,
			RequestedElapsedMs, InitialInteractive, InitialUnlocked
		FROM ExternalSessions
		WHERE Status IN ('pending', 'confirmed', 'active', 'suspended')
		ORDER BY RequestedMs, LaunchID`)
	if err != nil {
		return nil, fmt.Errorf("query unresolved external sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var sessions []database.ExternalSession
	for rows.Next() {
		var session database.ExternalSession
		var profile sql.NullString
		var dispatched, cursor sql.NullInt64
		var identity string
		if err := rows.Scan(&session.LaunchID, &session.SystemID, &session.SystemName,
			&session.MediaPath, &session.MediaName, &identity, &session.LauncherID, &profile,
			&session.Target, &session.BootID,
			&session.Status, &session.Source, &session.RequestedMs, &dispatched, &cursor,
			&session.RequestedElapsedMs, &session.InitialInteractive, &session.InitialUnlocked); err != nil {
			return nil, fmt.Errorf("scan unresolved external session: %w", err)
		}
		session.MediaIdentity = database.DecodeMediaIdentity(identity)
		if profile.Valid {
			session.ProfileID = &profile.String
		}
		if dispatched.Valid {
			session.DispatchedMs = &dispatched.Int64
		}
		if cursor.Valid {
			session.CursorMs = &cursor.Int64
		}
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate unresolved external sessions: %w", err)
	}
	return sessions, nil
}

// CloseExternalSessionApproximate closes a dispatched launch that had no host
// foreground evidence with the host's estimate of time away from the
// launcher. The row is host_return/approximate: it never proves the target
// was played and is never uploaded. Only a pending, dispatched session can
// close, so a replayed return cannot create a second row.
func (db *UserDB) CloseExternalSessionApproximate(
	ctx context.Context, launchID string, startMs, endMs int64,
) (bool, error) {
	if launchID == "" || startMs <= 0 || endMs < startMs {
		return false, errors.New("invalid approximate external interval")
	}
	tx, err := db.sql.Load().BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin approximate external session: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var status, source string
	var dispatched sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT Source, Status, DispatchedMs FROM ExternalSessions
		WHERE LaunchID = ?`, launchID).Scan(&source, &status, &dispatched)
	if err != nil {
		return false, fmt.Errorf("load approximate external session: %w", err)
	}
	if status != "pending" || source != "host_return" {
		return false, nil
	}
	if !dispatched.Valid || startMs < dispatched.Int64 {
		return false, errors.New("approximate external interval precedes its dispatch")
	}
	var historyID sql.NullInt64
	// Sub-second estimates carry no playtime worth recording.
	if seconds := (endMs - startMs) / 1000; seconds > 0 {
		if _, err = tx.ExecContext(ctx, `INSERT INTO ExternalSessionSegments
			(LaunchID, Ordinal, StartedMs, EndedMs, PolicyVersion) VALUES (?, 0, ?, ?, ?)`,
			launchID, startMs, endMs, sessionevidence.PolicyVersion); err != nil {
			return false, fmt.Errorf("persist approximate external segment: %w", err)
		}
		historyID.Int64, err = insertExternalHistory(ctx, tx, launchID, startMs, &endMs, seconds,
			"host_return", "approximate")
		if err != nil {
			return false, err
		}
		historyID.Valid = true
	}
	if _, err = tx.ExecContext(ctx, `UPDATE ExternalSessions SET Status = 'closed', EndedMs = ?,
		MediaHistoryDBID = ?, UpdatedMs = ? WHERE LaunchID = ? AND Status = 'pending'`,
		endMs, historyID, endMs, launchID); err != nil {
		return false, fmt.Errorf("close approximate external session: %w", err)
	}
	if pruneErr := deleteExternalEvidence(ctx, tx, launchID); pruneErr != nil {
		return false, pruneErr
	}
	if err = tx.Commit(); err != nil {
		return false, fmt.Errorf("commit approximate external session: %w", err)
	}
	return true, nil
}
