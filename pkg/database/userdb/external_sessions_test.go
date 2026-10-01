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

func externalSessionFixture(launchID string) *database.ExternalSession {
	return &database.ExternalSession{
		LaunchID: launchID, SystemID: "NES", SystemName: "Nintendo",
		MediaPath: "source://test/NES/" + launchID + ".nes", MediaName: "Game",
		LauncherID: "RetroArch", Target: "com.retroarch.aarch64",
		BootID: "boot-1", Status: "pending", Source: "foreground_events",
		RequestedMs: 100000, RequestedElapsedMs: 5000,
		InitialInteractive: true, InitialUnlocked: true,
	}
}

func TestBeginExternalSessionPersistsPendingAndSeedsInitialEvidence(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)
	ctx := t.Context()

	require.NoError(t, db.BeginExternalSession(ctx, externalSessionFixture("launch-1")))

	var status, source string
	require.NoError(t, db.sql.Load().QueryRowContext(ctx,
		`SELECT Status, Source FROM ExternalSessions WHERE LaunchID = 'launch-1'`).Scan(&status, &source))
	require.Equal(t, "pending", status)
	require.Equal(t, "foreground_events", source)

	var evidenceCount int
	require.NoError(t, db.sql.Load().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM ExternalSessionEvidence WHERE LaunchID = 'launch-1'`).Scan(&evidenceCount))
	require.Equal(t, 2, evidenceCount, "initial interactive+unlocked state seeds two evidence rows")
}

func TestBeginExternalSessionRejectsInvalidSession(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)
	ctx := t.Context()

	require.Error(t, db.BeginExternalSession(ctx, nil))

	missingTarget := externalSessionFixture("launch-bad")
	missingTarget.Target = ""
	require.Error(t, db.BeginExternalSession(ctx, missingTarget))

	badSource := externalSessionFixture("launch-bad-2")
	badSource.Source = "usage_events"
	require.Error(t, db.BeginExternalSession(ctx, badSource))

	alreadyDispatched := externalSessionFixture("launch-bad-3")
	dispatched := int64(1)
	alreadyDispatched.DispatchedMs = &dispatched
	require.Error(t, db.BeginExternalSession(ctx, alreadyDispatched))
}

func TestBeginExternalSessionSupersedesOlderUnresolvedOfSameBoot(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)
	ctx := t.Context()

	first := externalSessionFixture("launch-first")
	require.NoError(t, db.BeginExternalSession(ctx, first))

	otherBoot := externalSessionFixture("launch-other-boot")
	otherBoot.BootID = "boot-2"
	require.NoError(t, db.BeginExternalSession(ctx, otherBoot))

	second := externalSessionFixture("launch-second")
	second.RequestedMs = 200000
	require.NoError(t, db.BeginExternalSession(ctx, second))

	var superseded sql.NullInt64
	require.NoError(t, db.sql.Load().QueryRowContext(ctx,
		`SELECT SupersededMs FROM ExternalSessions WHERE LaunchID = 'launch-first'`).Scan(&superseded))
	require.True(t, superseded.Valid)
	require.Equal(t, int64(200000), superseded.Int64)

	require.NoError(t, db.sql.Load().QueryRowContext(ctx,
		`SELECT SupersededMs FROM ExternalSessions WHERE LaunchID = 'launch-other-boot'`).Scan(&superseded))
	require.False(t, superseded.Valid, "a different boot's session must not be superseded")

	require.NoError(t, db.sql.Load().QueryRowContext(ctx,
		`SELECT SupersededMs FROM ExternalSessions WHERE LaunchID = 'launch-second'`).Scan(&superseded))
	require.False(t, superseded.Valid, "the newest session is never superseded by itself")
}

func TestRecordExternalDispatchWritesProvisionalHistoryForForegroundEvents(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)
	ctx := t.Context()

	require.NoError(t, db.BeginExternalSession(ctx, externalSessionFixture("launch-1")))
	recorded, err := db.RecordExternalDispatch(ctx, "launch-1", 100500)
	require.NoError(t, err)
	require.True(t, recorded)

	var historyID sql.NullInt64
	require.NoError(t, db.sql.Load().QueryRowContext(ctx,
		`SELECT MediaHistoryDBID FROM ExternalSessions WHERE LaunchID = 'launch-1'`).Scan(&historyID))
	require.True(t, historyID.Valid)

	var source, confidence string
	var endTime sql.NullInt64
	require.NoError(t, db.sql.Load().QueryRowContext(ctx,
		`SELECT SessionSource, SessionConfidence, EndTime FROM MediaHistory WHERE DBID = ?`,
		historyID.Int64).Scan(&source, &confidence, &endTime))
	require.Equal(t, "foreground_events", source)
	require.Equal(t, "provisional", confidence)
	require.False(t, endTime.Valid, "a provisional row has no end until confirmed or withdrawn")
}

func TestRecordExternalDispatchHostReturnWritesNoHistory(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)
	ctx := t.Context()

	fixture := externalSessionFixture("launch-1")
	fixture.Source = "host_return"
	require.NoError(t, db.BeginExternalSession(ctx, fixture))
	recorded, err := db.RecordExternalDispatch(ctx, "launch-1", 100500)
	require.NoError(t, err)
	require.True(t, recorded)

	var historyID sql.NullInt64
	require.NoError(t, db.sql.Load().QueryRowContext(ctx,
		`SELECT MediaHistoryDBID FROM ExternalSessions WHERE LaunchID = 'launch-1'`).Scan(&historyID))
	require.False(t, historyID.Valid, "an approximate fallback publishes nothing until it closes")

	var count int
	require.NoError(t, db.sql.Load().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM MediaHistory`).Scan(&count))
	require.Zero(t, count)
}

func TestRecordExternalDispatchIsNotAppliedTwice(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)
	ctx := t.Context()

	require.NoError(t, db.BeginExternalSession(ctx, externalSessionFixture("launch-1")))
	first, err := db.RecordExternalDispatch(ctx, "launch-1", 100500)
	require.NoError(t, err)
	require.True(t, first)

	second, err := db.RecordExternalDispatch(ctx, "launch-1", 100600)
	require.NoError(t, err)
	require.False(t, second, "a session already dispatched cannot be dispatched again")

	var count int
	require.NoError(t, db.sql.Load().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM MediaHistory`).Scan(&count))
	require.Equal(t, 1, count, "a replayed receipt must not duplicate the provisional row")
}

func TestAbandonExternalSessionOnlyForUndispatchedPending(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)
	ctx := t.Context()

	require.NoError(t, db.BeginExternalSession(ctx, externalSessionFixture("launch-1")))
	abandoned, err := db.AbandonExternalSession(ctx, "launch-1", 100100)
	require.NoError(t, err)
	require.True(t, abandoned)

	var status string
	require.NoError(t, db.sql.Load().QueryRowContext(ctx,
		`SELECT Status FROM ExternalSessions WHERE LaunchID = 'launch-1'`).Scan(&status))
	require.Equal(t, "abandoned", status)

	require.NoError(t, db.BeginExternalSession(ctx, externalSessionFixture("launch-2")))
	_, err = db.RecordExternalDispatch(ctx, "launch-2", 100500)
	require.NoError(t, err)
	abandoned, err = db.AbandonExternalSession(ctx, "launch-2", 100600)
	require.NoError(t, err)
	require.False(t, abandoned, "an outcome-unknown dispatched launch must stay pending, not abandon")
}

func TestMarkExternalSessionStaleRetractsUnconfirmedProvisionalHistory(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)
	ctx := t.Context()

	require.NoError(t, db.BeginExternalSession(ctx, externalSessionFixture("launch-1")))
	_, err := db.RecordExternalDispatch(ctx, "launch-1", 100500)
	require.NoError(t, err)

	staled, err := db.MarkExternalSessionStale(ctx, "launch-1", 200000)
	require.NoError(t, err)
	require.True(t, staled)

	var isDeleted bool
	require.NoError(t, db.sql.Load().QueryRowContext(ctx,
		`SELECT IsDeleted FROM MediaHistory WHERE SystemID = 'NES'`).Scan(&isDeleted))
	require.True(t, isDeleted, "a never-confirmed provisional row is withdrawn, not left as history")

	staledAgain, err := db.MarkExternalSessionStale(ctx, "launch-1", 300000)
	require.NoError(t, err)
	require.False(t, staledAgain, "a terminal session cannot be staled twice")

	var evidenceCount int
	require.NoError(t, db.sql.Load().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM ExternalSessionEvidence WHERE LaunchID = 'launch-1'`).Scan(&evidenceCount))
	require.Zero(t, evidenceCount, "evidence is pruned once a session is terminal")
}

func TestUnresolvedExternalSessionsOnlyListsUnresolvedOnes(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)
	ctx := t.Context()

	require.NoError(t, db.BeginExternalSession(ctx, externalSessionFixture("launch-pending")))
	closedFixture := externalSessionFixture("launch-closed")
	closedFixture.Source = "host_return"
	require.NoError(t, db.BeginExternalSession(ctx, closedFixture))
	_, err := db.RecordExternalDispatch(ctx, "launch-closed", 100500)
	require.NoError(t, err)
	_, err = db.CloseExternalSessionApproximate(ctx, "launch-closed", 100500, 110500)
	require.NoError(t, err)

	sessions, err := db.UnresolvedExternalSessions(ctx)
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	require.Equal(t, "launch-pending", sessions[0].LaunchID)
}

func TestCloseExternalSessionApproximateWritesApproximateHistory(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)
	ctx := t.Context()

	fixture := externalSessionFixture("launch-1")
	fixture.Source = "host_return"
	require.NoError(t, db.BeginExternalSession(ctx, fixture))
	_, err := db.RecordExternalDispatch(ctx, "launch-1", 100500)
	require.NoError(t, err)

	closed, err := db.CloseExternalSessionApproximate(ctx, "launch-1", 100500, 130500)
	require.NoError(t, err)
	require.True(t, closed)

	var source, confidence string
	var playTime int
	require.NoError(t, db.sql.Load().QueryRowContext(ctx,
		`SELECT SessionSource, SessionConfidence, PlayTime FROM MediaHistory WHERE SystemID = 'NES'`).
		Scan(&source, &confidence, &playTime))
	require.Equal(t, "host_return", source)
	require.Equal(t, "approximate", confidence)
	require.Equal(t, 30, playTime)

	var status string
	require.NoError(t, db.sql.Load().QueryRowContext(ctx,
		`SELECT Status FROM ExternalSessions WHERE LaunchID = 'launch-1'`).Scan(&status))
	require.Equal(t, "closed", status)
}

func TestCloseExternalSessionApproximateSubSecondWritesNoHistory(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)
	ctx := t.Context()

	fixture := externalSessionFixture("launch-1")
	fixture.Source = "host_return"
	require.NoError(t, db.BeginExternalSession(ctx, fixture))
	_, err := db.RecordExternalDispatch(ctx, "launch-1", 100500)
	require.NoError(t, err)

	closed, err := db.CloseExternalSessionApproximate(ctx, "launch-1", 100500, 100600)
	require.NoError(t, err)
	require.True(t, closed)

	var count int
	require.NoError(t, db.sql.Load().QueryRowContext(ctx, `SELECT COUNT(*) FROM MediaHistory`).Scan(&count))
	require.Zero(t, count, "a sub-second estimate is not worth recording as history")
}

func TestCloseExternalSessionApproximateRejectsPrecedingDispatch(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)
	ctx := t.Context()

	fixture := externalSessionFixture("launch-1")
	fixture.Source = "host_return"
	require.NoError(t, db.BeginExternalSession(ctx, fixture))
	_, err := db.RecordExternalDispatch(ctx, "launch-1", 100500)
	require.NoError(t, err)

	_, err = db.CloseExternalSessionApproximate(ctx, "launch-1", 99000, 110000)
	require.Error(t, err)
}

func TestExternalSessionMethodsReportErrNullSQLWhenDisconnected(t *testing.T) {
	t.Parallel()
	db := &UserDB{}
	ctx := t.Context()

	require.ErrorIs(t, db.BeginExternalSession(ctx, externalSessionFixture("launch-1")), ErrNullSQL)
	_, err := db.RecordExternalDispatch(ctx, "launch-1", 1000)
	require.ErrorIs(t, err, ErrNullSQL)
	_, err = db.AbandonExternalSession(ctx, "launch-1", 1000)
	require.ErrorIs(t, err, ErrNullSQL)
	_, err = db.MarkExternalSessionStale(ctx, "launch-1", 1000)
	require.ErrorIs(t, err, ErrNullSQL)
	_, err = db.UnresolvedExternalSessions(ctx)
	require.ErrorIs(t, err, ErrNullSQL)
	_, err = db.CloseExternalSessionApproximate(ctx, "launch-1", 1000, 2000)
	require.ErrorIs(t, err, ErrNullSQL)
	_, err = db.ApplyExternalEvidence(ctx, evidenceBatch("launch-1", 2000))
	require.ErrorIs(t, err, ErrNullSQL)
	_, _, err = db.NextExternalLaunchElapsed(ctx, "boot-1", 1000)
	require.ErrorIs(t, err, ErrNullSQL)
}

func TestNextExternalLaunchElapsedFindsTheEarliestLaterLaunch(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)
	ctx := t.Context()

	earlier := externalSessionFixture("launch-earlier")
	earlier.RequestedElapsedMs = 4000
	require.NoError(t, db.BeginExternalSession(ctx, earlier))

	later := externalSessionFixture("launch-later")
	later.RequestedMs = 200000
	later.RequestedElapsedMs = 9000
	require.NoError(t, db.BeginExternalSession(ctx, later))

	evenLater := externalSessionFixture("launch-even-later")
	evenLater.RequestedMs = 300000
	evenLater.RequestedElapsedMs = 15000
	require.NoError(t, db.BeginExternalSession(ctx, evenLater))

	elapsed, found, err := db.NextExternalLaunchElapsed(ctx, "boot-1", 5000)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, int64(9000), elapsed, "the nearest later launch must win, not the furthest")
}

func TestNextExternalLaunchElapsedExcludesAbandonedSessions(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)
	ctx := t.Context()

	pending := externalSessionFixture("launch-pending")
	pending.RequestedElapsedMs = 4000
	require.NoError(t, db.BeginExternalSession(ctx, pending))

	abandoned := externalSessionFixture("launch-abandoned")
	abandoned.RequestedMs = 200000
	abandoned.RequestedElapsedMs = 9000
	require.NoError(t, db.BeginExternalSession(ctx, abandoned))
	wasAbandoned, err := db.AbandonExternalSession(ctx, "launch-abandoned", 200100)
	require.NoError(t, err)
	require.True(t, wasAbandoned)

	_, found, err := db.NextExternalLaunchElapsed(ctx, "boot-1", 5000)
	require.NoError(t, err)
	require.False(t, found, "an abandoned launch never dispatched, so it cannot cap an earlier one")
}

func TestNextExternalLaunchElapsedIgnoresOtherBootsAndEarlierLaunches(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)
	ctx := t.Context()

	earlier := externalSessionFixture("launch-earlier")
	earlier.RequestedElapsedMs = 3000
	require.NoError(t, db.BeginExternalSession(ctx, earlier))

	otherBoot := externalSessionFixture("launch-other-boot")
	otherBoot.BootID = "boot-2"
	otherBoot.RequestedMs = 200000
	otherBoot.RequestedElapsedMs = 9000
	require.NoError(t, db.BeginExternalSession(ctx, otherBoot))

	_, found, err := db.NextExternalLaunchElapsed(ctx, "boot-1", 5000)
	require.NoError(t, err)
	require.False(t, found, "an earlier launch and a different boot's launch must not satisfy the query")
}

func TestCleanupMediaHistoryDeletesOnlyTerminalExternalSessions(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)
	ctx := t.Context()

	oldClosed := externalSessionFixture("launch-old-closed")
	oldClosed.RequestedMs = 1000
	oldClosed.Source = "host_return"
	require.NoError(t, db.BeginExternalSession(ctx, oldClosed))
	_, err := db.RecordExternalDispatch(ctx, "launch-old-closed", 1500)
	require.NoError(t, err)
	closed, err := db.CloseExternalSessionApproximate(ctx, "launch-old-closed", 1500, 31500)
	require.NoError(t, err)
	require.True(t, closed)

	var segmentCount int
	require.NoError(t, db.sql.Load().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM ExternalSessionSegments WHERE LaunchID = 'launch-old-closed'`).Scan(&segmentCount))
	require.Equal(t, 1, segmentCount, "closing approximately must have left a segment to clean up")

	oldPending := externalSessionFixture("launch-old-pending")
	oldPending.RequestedMs = 1000
	require.NoError(t, db.BeginExternalSession(ctx, oldPending))

	rows, err := db.CleanupMediaHistory(0, false)
	require.NoError(t, err)
	require.GreaterOrEqual(t, rows, int64(0))

	var count int
	require.NoError(t, db.sql.Load().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM ExternalSessions WHERE LaunchID = 'launch-old-closed'`).Scan(&count))
	require.Zero(t, count, "a terminal session past the retention cutoff must be deleted")

	require.NoError(t, db.sql.Load().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM ExternalSessionSegments WHERE LaunchID = 'launch-old-closed'`).Scan(&segmentCount))
	require.Zero(t, segmentCount, "a deleted session's segments must not be left orphaned")

	require.NoError(t, db.sql.Load().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM ExternalSessions WHERE LaunchID = 'launch-old-pending'`).Scan(&count))
	require.Equal(t, 1, count, "a session still in flight must never be deleted regardless of age")
}

func TestCleanupMediaHistoryDeletesApproximateRowsEvenWhenSyncRequired(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)
	ctx := t.Context()

	_, err := db.sql.Load().ExecContext(ctx, `INSERT INTO MediaHistory
		(StartTime, SystemID, SystemName, MediaPath, MediaName, LauncherID, PlayTime,
		SessionSource, SessionConfidence)
		VALUES (1000, 'PC', 'PC', 'source://test/PC/game.steam', 'Game', 'GameNative.Steam', 30,
		'host_return', 'approximate')`)
	require.NoError(t, err)

	rows, err := db.CleanupMediaHistory(0, true)
	require.NoError(t, err)
	require.Equal(t, int64(1), rows, "an approximate row can never be synced, so requiring sync must not protect it")

	var count int
	require.NoError(t, db.sql.Load().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM MediaHistory WHERE SessionConfidence = 'approximate'`).Scan(&count))
	require.Zero(t, count)
}
