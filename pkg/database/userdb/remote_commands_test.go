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
	"encoding/json"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRemoteCommandLedgerLifecycleAndReplay(t *testing.T) {
	userDB, cleanup := setupTempUserDB(t)
	defer cleanup()

	deadline := time.Now().UTC().Add(time.Hour)
	command := &database.RemoteCommand{
		CommandID: "cmd_test", OperationID: "op_test", OperationType: "echo",
		ProtocolVersion: 1, ParamsDigest: "abc", Origin: json.RawMessage(`{"kind":"first_party"}`),
		DeadlineAt: deadline, State: "recorded",
	}
	stored, fresh, err := userDB.ClaimRemoteCommand(command)
	require.NoError(t, err)
	assert.True(t, fresh)
	assert.Equal(t, "recorded", stored.State)

	replayed, fresh, err := userDB.ClaimRemoteCommand(command)
	require.NoError(t, err)
	assert.False(t, fresh)
	assert.Equal(t, stored.CommandID, replayed.CommandID)

	expires := time.Now().UTC().Add(time.Minute)
	changed, err := userDB.TransitionRemoteCommand(command.CommandID, "recorded", "accepted", &expires)
	require.NoError(t, err)
	assert.True(t, changed)
	changed, err = userDB.TransitionRemoteCommand(command.CommandID, "recorded", "executing", nil)
	require.NoError(t, err)
	assert.False(t, changed)
	changed, err = userDB.TransitionRemoteCommand(command.CommandID, "accepted", "executing", nil)
	require.NoError(t, err)
	assert.True(t, changed)

	result := json.RawMessage(`{"message":"ok"}`)
	changed, err = userDB.StoreRemoteCommandResult(command.CommandID, "executing", "succeeded", result, "")
	require.NoError(t, err)
	assert.True(t, changed)
	pending, err := userDB.ListUnreportedRemoteCommands(20)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	assert.JSONEq(t, string(result), string(pending[0].Result))

	require.NoError(t, userDB.MarkRemoteCommandResultReported(command.CommandID))
	pending, err = userDB.ListUnreportedRemoteCommands(20)
	require.NoError(t, err)
	assert.Empty(t, pending)
}

// TestListRecentRemoteCommandsOrdersNewestFirstAndRespectsLimit pins the
// query backing the owner-facing remote activity view: newest first,
// regardless of state, bounded by limit.
func TestListRecentRemoteCommandsOrdersNewestFirstAndRespectsLimit(t *testing.T) {
	userDB, cleanup := setupTempUserDB(t)
	defer cleanup()

	base := time.Now().UTC().Add(-time.Hour)
	for i, id := range []string{"cmd_a", "cmd_b", "cmd_c"} {
		command := &database.RemoteCommand{
			CommandID: id, OperationID: "op_" + id, OperationType: "echo",
			ProtocolVersion: 1, ParamsDigest: "abc", Origin: json.RawMessage(`{"kind":"first_party"}`),
			DeadlineAt: base.Add(time.Hour), State: "recorded",
			CreatedAt: base.Add(time.Duration(i) * time.Minute),
		}
		_, _, err := userDB.ClaimRemoteCommand(command)
		require.NoError(t, err)
	}

	all, err := userDB.ListRecentRemoteCommands(10)
	require.NoError(t, err)
	require.Len(t, all, 3)
	assert.Equal(t, []string{"cmd_c", "cmd_b", "cmd_a"}, []string{
		all[0].CommandID, all[1].CommandID, all[2].CommandID,
	})

	limited, err := userDB.ListRecentRemoteCommands(2)
	require.NoError(t, err)
	require.Len(t, limited, 2)
	assert.Equal(t, "cmd_c", limited[0].CommandID)
	assert.Equal(t, "cmd_b", limited[1].CommandID)
}

// TestListUnreportedRemoteCommandsOrdersOldestFirstAndRespectsLimit pins the
// query backing remote.replayStoredResults: oldest first (so replay drains
// the longest-waiting results first), bounded by limit so a large backlog
// can be posted a batch at a time across several replay cycles instead of
// all at once.
func TestListUnreportedRemoteCommandsOrdersOldestFirstAndRespectsLimit(t *testing.T) {
	userDB, cleanup := setupTempUserDB(t)
	defer cleanup()

	base := time.Now().UTC().Add(-time.Hour)
	for i, id := range []string{"cmd_a", "cmd_b", "cmd_c"} {
		command := &database.RemoteCommand{
			CommandID: id, OperationID: "op_" + id, OperationType: "echo",
			ProtocolVersion: 1, ParamsDigest: "abc", Origin: json.RawMessage(`{"kind":"first_party"}`),
			DeadlineAt: base.Add(time.Hour), State: "recorded",
			CreatedAt: base.Add(time.Duration(i) * time.Minute),
		}
		_, _, err := userDB.ClaimRemoteCommand(command)
		require.NoError(t, err)
		expires := base.Add(time.Hour)
		_, err = userDB.TransitionRemoteCommand(id, "recorded", "accepted", &expires)
		require.NoError(t, err)
		_, err = userDB.TransitionRemoteCommand(id, "accepted", "executing", nil)
		require.NoError(t, err)
		_, err = userDB.StoreRemoteCommandResult(id, "executing", "succeeded", json.RawMessage(`{}`), "")
		require.NoError(t, err)
	}

	all, err := userDB.ListUnreportedRemoteCommands(10)
	require.NoError(t, err)
	require.Len(t, all, 3)
	assert.Equal(t, []string{"cmd_a", "cmd_b", "cmd_c"}, []string{
		all[0].CommandID, all[1].CommandID, all[2].CommandID,
	})

	limited, err := userDB.ListUnreportedRemoteCommands(2)
	require.NoError(t, err)
	require.Len(t, limited, 2)
	assert.Equal(t, "cmd_a", limited[0].CommandID)
	assert.Equal(t, "cmd_b", limited[1].CommandID)
}

// TestMarkRemoteCommandResultReported_NotTerminalReturnsError pins that a
// command still short of "terminal" (no result stored yet) cannot be marked
// reported: doing so would let a result be treated as delivered before it
// was ever posted.
func TestMarkRemoteCommandResultReported_NotTerminalReturnsError(t *testing.T) {
	userDB, cleanup := setupTempUserDB(t)
	defer cleanup()

	command := &database.RemoteCommand{
		CommandID: "cmd_pending", OperationID: "op_pending", OperationType: "echo",
		ProtocolVersion: 1, ParamsDigest: "abc", Origin: json.RawMessage(`{"kind":"first_party"}`),
		DeadlineAt: time.Now().UTC().Add(time.Hour), State: "recorded",
	}
	_, _, err := userDB.ClaimRemoteCommand(command)
	require.NoError(t, err)

	err = userDB.MarkRemoteCommandResultReported("cmd_pending")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not terminal")
}

// TestStoreRemoteCommandResult_StaleFromStateIsNoOp pins the concurrency
// contract remote.storeRemoteCommandResultWithRetry relies on: a result
// write against a fromState the row has already moved past reports
// changed=false with no error, rather than an error or a forced overwrite,
// so a superseding concurrent transition is never clobbered.
func TestStoreRemoteCommandResult_StaleFromStateIsNoOp(t *testing.T) {
	userDB, cleanup := setupTempUserDB(t)
	defer cleanup()

	command := &database.RemoteCommand{
		CommandID: "cmd_stale", OperationID: "op_stale", OperationType: "echo",
		ProtocolVersion: 1, ParamsDigest: "abc", Origin: json.RawMessage(`{"kind":"first_party"}`),
		DeadlineAt: time.Now().UTC().Add(time.Hour), State: "recorded",
	}
	_, _, err := userDB.ClaimRemoteCommand(command)
	require.NoError(t, err)

	// The row is still "recorded", so a result write against "executing"
	// (as if a concurrent transition had already moved it past this state)
	// must not match any row.
	changed, err := userDB.StoreRemoteCommandResult(
		"cmd_stale", "executing", "succeeded", json.RawMessage(`{}`), "")
	require.NoError(t, err)
	assert.False(t, changed)
}

// TestStoreRemoteCommandResult_PersistsErrorCode pins that a failed
// operation's error code round-trips through storage, not just its status.
func TestStoreRemoteCommandResult_PersistsErrorCode(t *testing.T) {
	userDB, cleanup := setupTempUserDB(t)
	defer cleanup()

	command := &database.RemoteCommand{
		CommandID: "cmd_failed", OperationID: "op_failed", OperationType: "launch",
		ProtocolVersion: 1, ParamsDigest: "abc", Origin: json.RawMessage(`{"kind":"first_party"}`),
		DeadlineAt: time.Now().UTC().Add(time.Hour), State: "recorded",
	}
	_, _, err := userDB.ClaimRemoteCommand(command)
	require.NoError(t, err)

	changed, err := userDB.StoreRemoteCommandResult(
		"cmd_failed", "recorded", "failed", nil, "media_not_found")
	require.NoError(t, err)
	assert.True(t, changed)

	recent, err := userDB.ListRecentRemoteCommands(1)
	require.NoError(t, err)
	require.Len(t, recent, 1)
	assert.Equal(t, "failed", recent[0].ResultStatus)
	assert.Equal(t, "media_not_found", recent[0].ErrorCode)
}

func TestRemoteCommandLedgerPrunesAfterRetentionCutoff(t *testing.T) {
	userDB, cleanup := setupTempUserDB(t)
	defer cleanup()

	oldDeadline := time.Now().UTC().Add(-25 * time.Hour)
	_, _, err := userDB.ClaimRemoteCommand(&database.RemoteCommand{
		CommandID: "cmd_old", OperationID: "op_old", OperationType: "echo",
		ProtocolVersion: 1, ParamsDigest: "abc", Origin: json.RawMessage(`{"kind":"first_party"}`),
		DeadlineAt: oldDeadline, State: "expired",
	})
	require.NoError(t, err)
	removed, err := userDB.PruneRemoteCommands(time.Now().UTC().Add(-24 * time.Hour))
	require.NoError(t, err)
	assert.EqualValues(t, 1, removed)
}
