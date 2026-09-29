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
	"database/sql"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/stretchr/testify/require"
)

func beginAndDispatch(t *testing.T, db *UserDB, launchID string) {
	t.Helper()
	ctx := t.Context()
	require.NoError(t, db.BeginExternalSession(ctx, externalSessionFixture(launchID)))
	_, err := db.RecordExternalDispatch(ctx, launchID, 100500)
	require.NoError(t, err)
}

func evidenceBatch(launchID string, toMs int64, events ...database.ForegroundEvent) *database.ForegroundEvidence {
	return &database.ForegroundEvidence{
		LaunchID: launchID, BootID: "boot-1", Permission: "granted", Complete: true,
		QueryFromMs: 100000, QueryToMs: toMs,
		ObservedElapsedMs: 5000 + (toMs - 100000), ObservedWallMs: toMs,
		Version: 1, Events: events,
	}
}

func TestApplyExternalEvidenceConfirmsAndUpdatesProvisionalToExact(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)
	ctx := t.Context()
	beginAndDispatch(t, db, "launch-1")

	batch := evidenceBatch("launch-1", 110000,
		database.ForegroundEvent{Kind: "target_resumed", TimestampMs: 100500, Sequence: 0},
		database.ForegroundEvent{Kind: "other_resumed", TimestampMs: 105500, Sequence: 1},
	)
	changed, err := db.ApplyExternalEvidence(ctx, batch)
	require.NoError(t, err)
	require.True(t, changed)

	var source, confidence string
	var playTime int
	var isDeleted bool
	require.NoError(t, db.sql.Load().QueryRowContext(ctx,
		`SELECT SessionSource, SessionConfidence, PlayTime, IsDeleted FROM MediaHistory WHERE SystemID = 'NES'`).
		Scan(&source, &confidence, &playTime, &isDeleted))
	require.Equal(t, "foreground_events", source)
	require.Equal(t, "exact", confidence)
	require.Equal(t, 5, playTime)
	require.False(t, isDeleted)

	var status string
	require.NoError(t, db.sql.Load().QueryRowContext(ctx,
		`SELECT Status FROM ExternalSessions WHERE LaunchID = 'launch-1'`).Scan(&status))
	require.Equal(t, "closed", status)

	var evidenceCount int
	require.NoError(t, db.sql.Load().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM ExternalSessionEvidence WHERE LaunchID = 'launch-1'`).Scan(&evidenceCount))
	require.Zero(t, evidenceCount, "evidence is pruned once the session reaches a terminal status")
}

func TestApplyExternalEvidenceOverlappingBatchIsIdempotent(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)
	ctx := t.Context()
	beginAndDispatch(t, db, "launch-1")

	first := evidenceBatch("launch-1", 103000,
		database.ForegroundEvent{Kind: "target_resumed", TimestampMs: 100500, Sequence: 0},
	)
	_, err := db.ApplyExternalEvidence(ctx, first)
	require.NoError(t, err)

	// Replays the same event the first batch already ingested, in a window
	// overlapping it by design (a real query overlaps by 5s for exactly
	// this reason).
	second := evidenceBatch("launch-1", 106000,
		database.ForegroundEvent{Kind: "target_resumed", TimestampMs: 100500, Sequence: 0},
		database.ForegroundEvent{Kind: "other_resumed", TimestampMs: 105000, Sequence: 1},
	)
	second.QueryFromMs = 100000
	changed, err := db.ApplyExternalEvidence(ctx, second)
	require.NoError(t, err)
	require.True(t, changed)

	var count int
	require.NoError(t, db.sql.Load().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM MediaHistory`).Scan(&count))
	require.Equal(t, 1, count, "the replayed overlap must not create a second history row")

	var playTime int
	require.NoError(t, db.sql.Load().QueryRowContext(ctx,
		`SELECT PlayTime FROM MediaHistory WHERE SystemID = 'NES'`).Scan(&playTime))
	require.Equal(t, 4, playTime)
}

func TestApplyExternalEvidenceStaleOnBootChange(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)
	ctx := t.Context()
	beginAndDispatch(t, db, "launch-1")

	batch := evidenceBatch("launch-1", 103000)
	batch.BootID = "boot-2"
	_, err := db.ApplyExternalEvidence(ctx, batch)
	require.Error(t, err, "evidence from a different boot than the launch must be refused, not applied")

	var status string
	require.NoError(t, db.sql.Load().QueryRowContext(ctx,
		`SELECT Status FROM ExternalSessions WHERE LaunchID = 'launch-1'`).Scan(&status))
	require.Equal(t, "pending", status, "a refused batch must not silently change session state")
}

func TestApplyExternalEvidenceCannotReviveATerminalSession(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)
	ctx := t.Context()
	beginAndDispatch(t, db, "launch-1")
	staled, err := db.MarkExternalSessionStale(ctx, "launch-1", 200000)
	require.NoError(t, err)
	require.True(t, staled)

	batch := evidenceBatch("launch-1", 103000,
		database.ForegroundEvent{Kind: "target_resumed", TimestampMs: 100500, Sequence: 0})
	_, err = db.ApplyExternalEvidence(ctx, batch)
	require.Error(t, err)
}

func TestApplyExternalEvidenceHardClosesOnSupersede(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)
	ctx := t.Context()
	beginAndDispatch(t, db, "launch-1")

	second := externalSessionFixture("launch-2")
	second.RequestedMs = 108000
	require.NoError(t, db.BeginExternalSession(ctx, second))

	batch := evidenceBatch("launch-1", 200000,
		database.ForegroundEvent{Kind: "target_resumed", TimestampMs: 100500, Sequence: 0})
	changed, err := db.ApplyExternalEvidence(ctx, batch)
	require.NoError(t, err)
	require.True(t, changed)

	var status string
	var endedMs sql.NullInt64
	require.NoError(t, db.sql.Load().QueryRowContext(ctx,
		`SELECT Status, EndedMs FROM ExternalSessions WHERE LaunchID = 'launch-1'`).Scan(&status, &endedMs))
	require.Equal(t, "closed", status)
	require.True(t, endedMs.Valid)
	require.Equal(t, int64(108000), endedMs.Int64, "closed at the newer launch's request time, not the query end")

	var playTime int
	require.NoError(t, db.sql.Load().QueryRowContext(ctx,
		`SELECT PlayTime FROM MediaHistory WHERE MediaPath LIKE '%launch-1%'`).Scan(&playTime))
	require.Equal(t, 7, playTime)
}

func TestApplyExternalEvidenceUnconfirmedTimeoutAbandonsAndRetracts(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)
	ctx := t.Context()
	beginAndDispatch(t, db, "launch-1")

	batch := evidenceBatch("launch-1", 100000+31_000)
	changed, err := db.ApplyExternalEvidence(ctx, batch)
	require.NoError(t, err)
	require.True(t, changed)

	var status string
	require.NoError(t, db.sql.Load().QueryRowContext(ctx,
		`SELECT Status FROM ExternalSessions WHERE LaunchID = 'launch-1'`).Scan(&status))
	require.Equal(t, "abandoned", status)

	var isDeleted bool
	require.NoError(t, db.sql.Load().QueryRowContext(ctx,
		`SELECT IsDeleted FROM MediaHistory WHERE SystemID = 'NES'`).Scan(&isDeleted))
	require.True(t, isDeleted)
}

func TestApplyExternalEvidenceRejectsInvalidBatch(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)
	ctx := t.Context()
	beginAndDispatch(t, db, "launch-1")

	denied := evidenceBatch("launch-1", 103000)
	denied.Permission = "denied"
	_, err := db.ApplyExternalEvidence(ctx, denied)
	require.Error(t, err)

	incomplete := evidenceBatch("launch-1", 103000)
	incomplete.Complete = false
	_, err = db.ApplyExternalEvidence(ctx, incomplete)
	require.Error(t, err)

	_, err = db.ApplyExternalEvidence(ctx, nil)
	require.Error(t, err)
}
