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

package updater

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/inbox"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestReportLastUpdate_ReportsUnusableMarkerOnce(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	dir := stateDirFor(dataDir)
	require.NoError(t, os.MkdirAll(dir, stateDirPerm))
	require.NoError(t, os.WriteFile(markerPath(dir), []byte("{"), stateFilePerm))
	require.NoError(t, RunStartupWatchdog(t.Context(), dataDir, testTargetVersion))
	assert.FileExists(t, markerPath(dir)+markerBadSuffix)

	mockUserDB := helpers.NewMockUserDBI()
	mockUserDB.On("AddInboxMessage", mock.MatchedBy(func(msg *database.InboxMessage) bool {
		return msg.Title == "Update needs manual recovery" &&
			msg.Severity == inbox.SeverityError &&
			msg.Category == inbox.CategoryUpdateResult
	})).Return(&database.InboxMessage{
		DBID:     1,
		Title:    "Update needs manual recovery",
		Category: inbox.CategoryUpdateResult,
	}, nil).Once()

	ns := make(chan models.Notification, 2)
	inboxSvc := inbox.NewService(mockUserDB, ns)
	ReportLastUpdate(dataDir, inboxSvc)
	assert.Nil(t, peekUpdateResult(dir))

	// The quarantined marker remains install-blocking, but its stable result
	// identity prevents the same warning being posted on every boot.
	require.NoError(t, RunStartupWatchdog(t.Context(), dataDir, testTargetVersion))
	ReportLastUpdate(dataDir, inboxSvc)
	mockUserDB.AssertExpectations(t)
}

func TestReportLastUpdate_DescribesRollbackDirection(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	dir := stateDirFor(dataDir)
	require.NoError(t, recordUpdateResult(dir, &updateResult{
		At:          time.Date(2026, 8, 18, 5, 0, 0, 0, time.UTC),
		Outcome:     outcomeRolledBack,
		FromVersion: testPrevVersion,
		ToVersion:   testTargetVersion,
	}))

	mockUserDB := helpers.NewMockUserDBI()
	mockUserDB.On("AddInboxMessage", mock.MatchedBy(func(msg *database.InboxMessage) bool {
		return msg.Title == "Update rolled back" &&
			msg.Body == "Version 2.2.0 did not start, so version 2.1.0 was restored. "+
				"Your data was restored from the snapshot taken before the update." &&
			msg.Category == inbox.CategoryUpdateResult
	})).Return(&database.InboxMessage{
		DBID:     1,
		Title:    "Update rolled back",
		Category: inbox.CategoryUpdateResult,
	}, nil).Once()

	ns := make(chan models.Notification, 1)
	ReportLastUpdate(dataDir, inbox.NewService(mockUserDB, ns))

	assert.Nil(t, peekUpdateResult(dir))
	mockUserDB.AssertExpectations(t)
}

func TestReportLastUpdate_RetriesAfterInboxFailure(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	dir := stateDirFor(dataDir)
	require.NoError(t, recordUpdateResult(dir, &updateResult{
		At:          time.Date(2026, 8, 18, 5, 0, 0, 0, time.UTC),
		Outcome:     outcomeRollbackBlocked,
		FromVersion: testPrevVersion,
		ToVersion:   testTargetVersion,
		Detail:      "snapshot missing",
	}))

	mockUserDB := helpers.NewMockUserDBI()
	mockUserDB.On("AddInboxMessage", mock.Anything).
		Return((*database.InboxMessage)(nil), errors.New("database busy")).Once()
	mockUserDB.On("AddInboxMessage", mock.Anything).
		Return(&database.InboxMessage{
			DBID:     1,
			Title:    "Update could not be rolled back",
			Category: inbox.CategoryUpdateResult,
		}, nil).Once()

	ns := make(chan models.Notification, 2)
	inboxSvc := inbox.NewService(mockUserDB, ns)
	ReportLastUpdate(dataDir, inboxSvc)
	assert.NotNil(t, peekUpdateResult(dir), "failed inbox write must remain pending")

	ReportLastUpdate(dataDir, inboxSvc)
	assert.Nil(t, peekUpdateResult(dir), "successful retry must acknowledge the result")
	mockUserDB.AssertExpectations(t)
}

func TestReportLastUpdate_MessagePerOutcome(t *testing.T) {
	t.Parallel()

	tests := []struct {
		outcome  updateOutcome
		title    string
		body     string
		severity int
	}{
		{
			outcome:  outcomeSucceeded,
			severity: inbox.SeverityInfo,
			title:    "Update installed",
			body:     "Updated from version 2.1.0 to version 2.2.0.",
		},
		{
			outcome:  outcomeRollbackBlocked,
			severity: inbox.SeverityError,
			title:    "Update could not be rolled back",
			body: "Version 2.2.0 did not start and version 2.1.0 could not be restored " +
				"automatically. The snapshot taken before the update is still in your " +
				"backups and can be restored by hand.",
		},
	}

	for _, tt := range tests {
		t.Run(string(tt.outcome), func(t *testing.T) {
			t.Parallel()

			dataDir := t.TempDir()
			dir := stateDirFor(dataDir)
			require.NoError(t, recordUpdateResult(dir, &updateResult{
				At:          time.Date(2026, 8, 18, 5, 0, 0, 0, time.UTC),
				Outcome:     tt.outcome,
				FromVersion: testPrevVersion,
				ToVersion:   testTargetVersion,
				Detail:      "disk was full",
			}))

			mockUserDB := helpers.NewMockUserDBI()
			mockUserDB.On("AddInboxMessage", mock.MatchedBy(func(msg *database.InboxMessage) bool {
				return msg.Title == tt.title &&
					msg.Severity == tt.severity &&
					// A detail from the failure is appended so support has the
					// reason, not just the outcome.
					msg.Body == tt.body+"\n\ndisk was full" &&
					msg.Category == inbox.CategoryUpdateResult
			})).Return(&database.InboxMessage{
				DBID:     1,
				Title:    tt.title,
				Category: inbox.CategoryUpdateResult,
			}, nil).Once()

			ns := make(chan models.Notification, 1)
			ReportLastUpdate(dataDir, inbox.NewService(mockUserDB, ns))

			assert.Nil(t, peekUpdateResult(dir), "a delivered result must not be shown again")
			mockUserDB.AssertExpectations(t)
		})
	}
}

// Nothing to report and nowhere to report to are both ordinary: most boots have
// no update result, and some platforms start without an inbox.
func TestReportLastUpdate_QuietWithNothingToSay(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	ReportLastUpdate(dataDir, nil)

	mockUserDB := helpers.NewMockUserDBI()
	ns := make(chan models.Notification, 1)
	ReportLastUpdate(dataDir, inbox.NewService(mockUserDB, ns))
	mockUserDB.AssertNotCalled(t, "AddInboxMessage", mock.Anything)
}
