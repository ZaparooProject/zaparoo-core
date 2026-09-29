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
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/stretchr/testify/require"
)

func TestExternalSessionsMigrationKeepsLegacyHistoryAndRejectsDuplicateEvidence(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)
	sqlDB := db.sql.Load()
	_, err := sqlDB.ExecContext(t.Context(), `INSERT INTO MediaHistory
		(StartTime, SystemID, SystemName, MediaPath, MediaName, LauncherID, PlayTime)
		VALUES (1000, 'NES', 'Nintendo', 'source://test/NES/Game.nes', 'Game', 'RetroArch', 123)`)
	require.NoError(t, err)

	require.NoError(t, database.MigrateDownTo(sqlDB, migrationFiles, "migrations", 20260929120000-1))
	require.NoError(t, sqlMigrateUp(sqlDB, ""))
	var source, confidence string
	var seconds int
	require.NoError(t, sqlDB.QueryRowContext(t.Context(), `SELECT PlayTime, SessionSource, SessionConfidence
		FROM MediaHistory WHERE SystemID = 'NES'`).Scan(&seconds, &source, &confidence))
	require.Equal(t, 123, seconds)
	require.Equal(t, "active_media", source)
	require.Equal(t, "unspecified", confidence)

	_, err = sqlDB.ExecContext(t.Context(), `INSERT INTO ExternalSessions
		(LaunchID, SystemID, SystemName, MediaPath, MediaName, LauncherID,
		Target, BootID, Status, Source, RequestedMs, UpdatedMs)
		VALUES ('launch-1', 'NES', 'Nintendo', 'source://test/NES/Game.nes', 'Game', 'RetroArch',
		'com.retroarch.aarch64', 'boot-1', 'pending', 'foreground_events', 100000, 100000)`)
	require.NoError(t, err)
	_, err = sqlDB.ExecContext(t.Context(), `INSERT INTO ExternalSessionEvidence (LaunchID, TimestampMs, Kind)
		VALUES ('launch-1', 101000, 'other_resumed')`)
	require.NoError(t, err)
	_, err = sqlDB.ExecContext(t.Context(), `INSERT INTO ExternalSessionEvidence (LaunchID, TimestampMs, Kind)
		VALUES ('launch-1', 101000, 'other_resumed')`)
	require.Error(t, err, "overlapping queries must not duplicate an event")
	_, err = sqlDB.ExecContext(t.Context(), `INSERT INTO ExternalSessionEvidence (LaunchID, TimestampMs, Kind)
		VALUES ('launch-1', 102000, 'unfiltered_package_name')`)
	require.Error(t, err, "raw unrelated-app evidence is outside the contract")
}
