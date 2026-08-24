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

package methods

import (
	"errors"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestMediaOperationGuardOptimizationConflictsAreClientErrors(t *testing.T) {
	// Shared API status instances make these tests intentionally non-parallel.
	ClearIndexingStatus()
	ClearScrapingStatus()
	t.Cleanup(ClearIndexingStatus)
	t.Cleanup(ClearScrapingStatus)

	tests := []struct {
		start       func(database.MediaDBI) (*database.MediaWriteLease, error)
		name        string
		expectedMsg string
	}{
		{
			name:        "indexing",
			expectedMsg: "database optimization in progress",
			start:       startIndexing,
		},
		{
			name:        "scraping",
			expectedMsg: "database optimization in progress",
			start: func(db database.MediaDBI) (*database.MediaWriteLease, error) {
				return startScraping(db, "test-scraper", false)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ClearIndexingStatus()
			ClearScrapingStatus()
			mockDB := testhelpers.NewMockMediaDBI()
			optimizationLease, err := mockDB.AcquireMediaWrite(database.MediaWriteOperationOptimization)
			require.NoError(t, err)
			defer optimizationLease.Release()

			lease, err := tt.start(mockDB)
			assert.Nil(t, lease)
			require.Error(t, err)
			var clientErr *models.ClientError
			require.ErrorAs(t, err, &clientErr)
			require.ErrorIs(t, err, database.ErrMediaWriteConflict)
			assert.Equal(t, tt.expectedMsg, err.Error())
		})
	}
}

func TestPostIndexOptimizationHandoffBlocksScraping(t *testing.T) {
	mockDB := testhelpers.NewMockMediaDBI()
	indexLease, err := mockDB.AcquireMediaWrite(database.MediaWriteOperationIndexing)
	require.NoError(t, err)

	unblockOptimization := make(chan struct{})
	optimizationDone := make(chan struct{})
	mockDB.On("TrackBackgroundOperation").Return().Once()
	mockDB.On("RunBackgroundOptimizationWithLease", mock.Anything, mock.Anything, indexLease).
		Run(func(mock.Arguments) { <-unblockOptimization }).Return(nil).Once()
	mockDB.On("BackgroundOperationDone").Run(func(mock.Arguments) { close(optimizationDone) }).Return().Once()

	require.NoError(t, startPostIndexOptimization(mockDB, indexLease, nil, nil))
	assert.Equal(t, database.MediaWriteOperationOptimization, mockDB.ActiveMediaWriteOperation())

	scrapeLease, err := mockDB.AcquireMediaWrite(database.MediaWriteOperationScraping)
	assert.Nil(t, scrapeLease)
	require.ErrorIs(t, err, database.ErrMediaWriteConflict)

	close(unblockOptimization)
	select {
	case <-optimizationDone:
	case <-time.After(2 * time.Second):
		t.Fatal("post-index optimization did not release ownership")
	}
	assert.Equal(t, database.MediaWriteOperationNone, mockDB.ActiveMediaWriteOperation())
	mockDB.AssertExpectations(t)
}

func TestPostIndexOptimizationFailedStartReleasesLease(t *testing.T) {
	mockDB := testhelpers.NewMockMediaDBI()
	indexLease, err := mockDB.AcquireMediaWrite(database.MediaWriteOperationIndexing)
	require.NoError(t, err)

	optimizationDone := make(chan struct{})
	mockDB.On("TrackBackgroundOperation").Return().Once()
	mockDB.On("RunBackgroundOptimizationWithLease", mock.Anything, mock.Anything, indexLease).
		Return(errors.New("injected optimization startup failure")).Once()
	mockDB.On("BackgroundOperationDone").Run(func(mock.Arguments) { close(optimizationDone) }).Return().Once()

	require.NoError(t, startPostIndexOptimization(mockDB, indexLease, nil, nil))
	select {
	case <-optimizationDone:
	case <-time.After(2 * time.Second):
		t.Fatal("failed post-index optimization did not finish")
	}
	assert.Equal(t, database.MediaWriteOperationNone, mockDB.ActiveMediaWriteOperation())
	mockDB.AssertExpectations(t)
}

func TestMediaOperationGuardReleasesLeaseWhenStatusClaimFails(t *testing.T) {
	ClearIndexingStatus()
	ClearScrapingStatus()
	t.Cleanup(ClearIndexingStatus)
	t.Cleanup(ClearScrapingStatus)

	mockDB := testhelpers.NewMockMediaDBI()
	statusInstance.setRunning(true)
	lease, err := startIndexing(mockDB)
	assert.Nil(t, lease)
	require.Error(t, err)
	assert.Equal(t, database.MediaWriteOperationNone, mockDB.ActiveMediaWriteOperation())

	statusInstance.clear()
	scrapingStatusInstance.startIfNotRunning("test-scraper", false)
	lease, err = startScraping(mockDB, "test-scraper", false)
	assert.Nil(t, lease)
	require.Error(t, err)
	assert.Equal(t, database.MediaWriteOperationNone, mockDB.ActiveMediaWriteOperation())
	assert.NotErrorIs(t, err, database.ErrMediaWriteConflict)
}
