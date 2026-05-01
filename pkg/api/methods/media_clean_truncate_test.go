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
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleMediaCleanTruncate_Success(t *testing.T) {
	t.Parallel()

	mockMediaDB := testhelpers.NewMockMediaDBI()
	mockMediaDB.On("GetIndexingStatus").Return("", nil)
	mockMediaDB.On("Truncate").Return(nil)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{MediaDB: mockMediaDB},
	}

	result, err := HandleMediaCleanTruncate(env)
	require.NoError(t, err)
	assert.Equal(t, NoContent{}, result)
	mockMediaDB.AssertExpectations(t)
}

func TestHandleMediaCleanTruncate_Error(t *testing.T) {
	t.Parallel()

	mockMediaDB := testhelpers.NewMockMediaDBI()
	mockMediaDB.On("GetIndexingStatus").Return("", nil)
	mockMediaDB.On("Truncate").Return(errors.New("disk full"))

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{MediaDB: mockMediaDB},
	}

	_, err := HandleMediaCleanTruncate(env)
	require.Error(t, err)
	mockMediaDB.AssertExpectations(t)
}

func TestHandleMediaCleanTruncate_GetIndexingStatusError(t *testing.T) {
	t.Parallel()

	mockMediaDB := testhelpers.NewMockMediaDBI()
	mockMediaDB.On("GetIndexingStatus").Return("", errors.New("db unavailable"))

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{MediaDB: mockMediaDB},
	}

	_, err := HandleMediaCleanTruncate(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to check indexing status")
	mockMediaDB.AssertExpectations(t)
}

func TestHandleMediaCleanTruncate_IndexingInProgress(t *testing.T) {
	t.Parallel()

	for _, status := range []string{"running", "pending"} {
		t.Run(status, func(t *testing.T) {
			t.Parallel()

			mockMediaDB := testhelpers.NewMockMediaDBI()
			mockMediaDB.On("GetIndexingStatus").Return(status, nil)

			env := requests.RequestEnv{
				Context:  context.Background(),
				Database: &database.Database{MediaDB: mockMediaDB},
			}

			_, err := HandleMediaCleanTruncate(env)
			require.Error(t, err)
			var clientErr *models.ClientError
			require.ErrorAs(t, err, &clientErr, "expected ClientError when indexing is active")
			assert.Contains(t, err.Error(), "cannot truncate while media indexing is in progress")
			mockMediaDB.AssertExpectations(t)
		})
	}
}
