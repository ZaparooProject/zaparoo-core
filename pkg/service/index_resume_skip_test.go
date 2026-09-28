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

package service

import (
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/methods"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	inboxservice "github.com/ZaparooProject/zaparoo-core/v2/pkg/service/inbox"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type stuckIndex struct {
	db       *database.Database
	st       *state.State
	userDB   *testhelpers.MockUserDBI
	platform *mocks.MockPlatform
}

// stuckIndexFixture is an interrupted index that has resumed without moving
// its checkpoint, so the next resume reaches `attempts` no-progress resumes.
func stuckIndexFixture(t *testing.T, attempts int, current string, skipped []string) stuckIndex {
	t.Helper()
	methods.ClearIndexingStatus()
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform")
	mockPlatform.On("Settings").Return(platforms.Settings{})
	mockPlatform.On("Launchers", mock.Anything).Return([]platforms.Launcher{})
	mockPlatform.On("RootDirs", mock.Anything).Return([]string{"/test/roms"})

	db, cleanup := testhelpers.NewTestDatabase(t)
	t.Cleanup(cleanup)
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")
	mockUserDB := testhelpers.NewMockUserDBI()
	mockUserDB.On("AddInboxMessage", mock.Anything).Return(&database.InboxMessage{}, nil)
	st.SetInbox(inboxservice.NewService(mockUserDB, st.Notifications))

	require.NoError(t, db.MediaDB.SetIndexingStatus(mediadb.IndexingStatusRunning))
	require.NoError(t, db.MediaDB.SetIndexingSystems([]string{"NES", "SNES"}))
	require.NoError(t, db.MediaDB.SetLastIndexedSystem("NES"))
	require.NoError(t, db.MediaDB.SetIndexResumeCheckpoint(indexResumeCheckpointPrefix+"NES"))
	for range attempts - 1 {
		_, err := db.MediaDB.IncrementIndexResumeAttempts()
		require.NoError(t, err)
	}
	require.NoError(t, db.MediaDB.SetIndexingCurrentSystem(current))
	require.NoError(t, db.MediaDB.SetIndexingSkippedSystems(skipped))
	return stuckIndex{db: db, st: st, userDB: mockUserDB, platform: mockPlatform}
}

func inboxMessages(userDB *testhelpers.MockUserDBI, category string) []*database.InboxMessage {
	var out []*database.InboxMessage
	for i := range userDB.Calls {
		if userDB.Calls[i].Method != "AddInboxMessage" {
			continue
		}
		msg, ok := userDB.Calls[i].Arguments.Get(0).(*database.InboxMessage)
		if ok && msg.Category == category {
			out = append(out, msg)
		}
	}
	return out
}

func waitForIndexingToStop(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for methods.IsIndexing() && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	require.False(t, methods.IsIndexing(), "resumed indexing did not finish")
}

// Issue #1572: a system whose index cannot finish within one power-on made
// every boot resume into it again until indexing paused altogether, holding
// back every system after it. After a couple of resumes without progress the
// system is skipped, the user is told which, and the rest resumes.
func TestCheckAndResumeIndexing_SkipsSystemIndexingKeepsStoppingIn(t *testing.T) {
	// Not parallel: GenerateMediaDB uses the global indexing status.
	f := stuckIndexFixture(t, indexResumeSkipAfterAttempts, "SNES", nil)
	db, st, userDB, pl := f.db, f.st, f.userDB, f.platform
	fs := testhelpers.NewMemoryFS()
	cfg, err := testhelpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	started := checkAndResumeIndexing(pl, cfg, db, st, nil)
	require.True(t, started, "the rest of the library must still be indexed")
	attempts, err := db.MediaDB.GetIndexResumeAttempts()
	require.NoError(t, err)
	assert.Zero(t, attempts, "skipping a system restarts the resume budget")

	msgs := inboxMessages(userDB, inboxservice.CategoryMediaIndexSystemSkipped)
	require.Len(t, msgs, 1)
	assert.Contains(t, msgs[0].Body, systemDisplayName("SNES"))
	assert.Empty(t, inboxMessages(userDB, inboxservice.CategoryMediaIndexResumeLimit))

	waitForIndexingToStop(t)
	status, err := db.MediaDB.GetIndexingStatus()
	require.NoError(t, err)
	assert.Equal(t, mediadb.IndexingStatusCompleted, status)
	skipped, err := db.MediaDB.GetIndexingSkippedSystems()
	require.NoError(t, err)
	assert.Empty(t, skipped, "a completed run clears the skip list")
}

// One resume without progress is not yet a stuck system: an ordinary power-off
// mid-system must simply resume.
func TestCheckAndResumeIndexing_DoesNotSkipAfterOneStall(t *testing.T) {
	f := stuckIndexFixture(t, indexResumeSkipAfterAttempts-1, "SNES", nil)
	db, st, userDB, pl := f.db, f.st, f.userDB, f.platform
	fs := testhelpers.NewMemoryFS()
	cfg, err := testhelpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	require.True(t, checkAndResumeIndexing(pl, cfg, db, st, nil))
	assert.Empty(t, inboxMessages(userDB, inboxservice.CategoryMediaIndexSystemSkipped))
	waitForIndexingToStop(t)
}

// A system already skipped cannot be blamed twice. If the run still does not
// move, the stall limit pauses indexing as before.
func TestCheckAndResumeIndexing_StallLimitStillAppliesAfterSkipping(t *testing.T) {
	f := stuckIndexFixture(t, maxIndexNoProgressResumeAttempts, "SNES", []string{"SNES"})
	db, st, userDB, pl := f.db, f.st, f.userDB, f.platform
	fs := testhelpers.NewMemoryFS()
	cfg, err := testhelpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	assert.False(t, checkAndResumeIndexing(pl, cfg, db, st, nil))
	status, err := db.MediaDB.GetIndexingStatus()
	require.NoError(t, err)
	assert.Equal(t, mediadb.IndexingStatusCancelled, status)
	assert.Empty(t, inboxMessages(userDB, inboxservice.CategoryMediaIndexSystemSkipped))
	assert.Len(t, inboxMessages(userDB, inboxservice.CategoryMediaIndexResumeLimit), 1)
}

// Every skipped system is named, since the message replaces the previous one.
func TestSkipStuckIndexSystem_NamesEverySkippedSystem(t *testing.T) {
	f := stuckIndexFixture(t, indexResumeSkipAfterAttempts, "SNES", []string{"Genesis"})
	db, st, userDB := f.db, f.st, f.userDB

	require.True(t, skipStuckIndexSystem(db.MediaDB, st))
	skipped, err := db.MediaDB.GetIndexingSkippedSystems()
	require.NoError(t, err)
	assert.Equal(t, []string{"Genesis", "SNES"}, skipped)
	msgs := inboxMessages(userDB, inboxservice.CategoryMediaIndexSystemSkipped)
	require.Len(t, msgs, 1)
	for _, id := range skipped {
		assert.Contains(t, msgs[0].Body, systemDisplayName(id), id)
	}
}
