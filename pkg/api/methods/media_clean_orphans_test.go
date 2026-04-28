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
	"context"
	"errors"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestHandleMediaCleanOrphans_Success(t *testing.T) {
	t.Parallel()

	mockMediaDB := testhelpers.NewMockMediaDBI()
	mockMediaDB.On("CleanMediaOrphans", mock.Anything).Return(int64(5), nil)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{MediaDB: mockMediaDB},
	}

	result, err := HandleMediaCleanOrphans(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaCleanOrphansResponse)
	require.True(t, ok)
	assert.Equal(t, int64(5), resp.Deleted)
	mockMediaDB.AssertExpectations(t)
}

func TestHandleMediaCleanOrphans_NoneDeleted(t *testing.T) {
	t.Parallel()

	mockMediaDB := testhelpers.NewMockMediaDBI()
	mockMediaDB.On("CleanMediaOrphans", mock.Anything).Return(int64(0), nil)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{MediaDB: mockMediaDB},
	}

	result, err := HandleMediaCleanOrphans(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaCleanOrphansResponse)
	require.True(t, ok)
	assert.Equal(t, int64(0), resp.Deleted)
}

func TestHandleMediaCleanOrphans_IndexingInProgress(t *testing.T) {
	t.Parallel()

	mockMediaDB := testhelpers.NewMockMediaDBI()
	mockMediaDB.On("CleanMediaOrphans", mock.Anything).Return(int64(0), mediadb.ErrIndexingInProgress)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{MediaDB: mockMediaDB},
	}

	_, err := HandleMediaCleanOrphans(env)
	require.Error(t, err)

	var clientErr *models.ClientError
	assert.ErrorAs(t, err, &clientErr, "expected ClientError for indexing-in-progress guard")
}

func TestHandleMediaCleanOrphans_OptimizationInProgress(t *testing.T) {
	t.Parallel()

	mockMediaDB := testhelpers.NewMockMediaDBI()
	mockMediaDB.On("CleanMediaOrphans", mock.Anything).Return(int64(0), mediadb.ErrOptimizationInProgress)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{MediaDB: mockMediaDB},
	}

	_, err := HandleMediaCleanOrphans(env)
	require.Error(t, err)

	var clientErr *models.ClientError
	assert.ErrorAs(t, err, &clientErr, "expected ClientError for optimization-in-progress guard")
}

func TestHandleMediaCleanOrphans_TransactionActive(t *testing.T) {
	t.Parallel()

	mockMediaDB := testhelpers.NewMockMediaDBI()
	mockMediaDB.On("CleanMediaOrphans", mock.Anything).Return(int64(0), mediadb.ErrTransactionActive)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{MediaDB: mockMediaDB},
	}

	_, err := HandleMediaCleanOrphans(env)
	require.Error(t, err)

	var clientErr *models.ClientError
	assert.ErrorAs(t, err, &clientErr, "expected ClientError for transaction-active guard")
}

func TestHandleMediaCleanOrphans_UnexpectedError(t *testing.T) {
	t.Parallel()

	mockMediaDB := testhelpers.NewMockMediaDBI()
	mockMediaDB.On("CleanMediaOrphans", mock.Anything).Return(int64(0), errors.New("disk full"))

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{MediaDB: mockMediaDB},
	}

	_, err := HandleMediaCleanOrphans(env)
	require.Error(t, err)

	var clientErr *models.ClientError
	assert.NotErrorAs(t, err, &clientErr, "unexpected errors should not be ClientError")
}
