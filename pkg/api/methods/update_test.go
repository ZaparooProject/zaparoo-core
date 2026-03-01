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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/updater"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleUpdateCheck_DevelopmentVersion(t *testing.T) {
	devVersions := []string{"DEVELOPMENT", "abc1234-dev"}

	for _, v := range devVersions {
		t.Run(v, func(t *testing.T) {
			original := config.AppVersion
			config.AppVersion = v
			t.Cleanup(func() { config.AppVersion = original })

			mockPlatform := mocks.NewMockPlatform()
			mockPlatform.SetupBasicMock()

			env := requests.RequestEnv{
				Context:  t.Context(),
				Platform: mockPlatform,
			}

			result, err := HandleUpdateCheck(env, updater.Check)
			require.NoError(t, err)

			resp, ok := result.(models.UpdateCheckResponse)
			require.True(t, ok)
			assert.Equal(t, v, resp.CurrentVersion)
			assert.False(t, resp.UpdateAvailable)
			assert.Empty(t, resp.LatestVersion)
		})
	}
}

func TestHandleUpdateCheck_UpdateAvailable(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.SetupBasicMock()

	env := requests.RequestEnv{
		Context:  t.Context(),
		Platform: mockPlatform,
	}

	checkFn := func(_ context.Context, _ string) (*updater.Result, error) {
		return &updater.Result{
			CurrentVersion:  "2.9.0",
			LatestVersion:   "2.10.0",
			UpdateAvailable: true,
			ReleaseNotes:    "New features",
		}, nil
	}

	result, err := HandleUpdateCheck(env, checkFn)
	require.NoError(t, err)

	resp, ok := result.(models.UpdateCheckResponse)
	require.True(t, ok)
	assert.Equal(t, "2.9.0", resp.CurrentVersion)
	assert.Equal(t, "2.10.0", resp.LatestVersion)
	assert.True(t, resp.UpdateAvailable)
	assert.Equal(t, "New features", resp.ReleaseNotes)
}

func TestHandleUpdateCheck_NoUpdateAvailable(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.SetupBasicMock()

	env := requests.RequestEnv{
		Context:  t.Context(),
		Platform: mockPlatform,
	}

	checkFn := func(_ context.Context, _ string) (*updater.Result, error) {
		return &updater.Result{
			CurrentVersion:  "2.10.0",
			LatestVersion:   "2.10.0",
			UpdateAvailable: false,
		}, nil
	}

	result, err := HandleUpdateCheck(env, checkFn)
	require.NoError(t, err)

	resp, ok := result.(models.UpdateCheckResponse)
	require.True(t, ok)
	assert.Equal(t, "2.10.0", resp.CurrentVersion)
	assert.False(t, resp.UpdateAvailable)
}

func TestHandleUpdateCheck_Error(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.SetupBasicMock()

	env := requests.RequestEnv{
		Context:  t.Context(),
		Platform: mockPlatform,
	}

	checkFn := func(_ context.Context, _ string) (*updater.Result, error) {
		return nil, errors.New("network timeout")
	}

	result, err := HandleUpdateCheck(env, checkFn)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "update check failed")
	assert.Contains(t, err.Error(), "network timeout")
	assert.Nil(t, result)
}

func TestHandleUpdateApply_DevelopmentVersion(t *testing.T) {
	devVersions := []string{"DEVELOPMENT", "abc1234-dev"}

	for _, v := range devVersions {
		t.Run(v, func(t *testing.T) {
			original := config.AppVersion
			config.AppVersion = v
			t.Cleanup(func() { config.AppVersion = original })

			mockPlatform := mocks.NewMockPlatform()
			mockPlatform.SetupBasicMock()

			env := requests.RequestEnv{
				Context:  t.Context(),
				Platform: mockPlatform,
			}

			result, err := HandleUpdateApply(env, updater.Apply, func(_ int) {})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "development builds")
			assert.Nil(t, result)
		})
	}
}

func TestHandleUpdateApply_Error(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.SetupBasicMock()

	env := requests.RequestEnv{
		Context:  t.Context(),
		Platform: mockPlatform,
	}

	applyFn := func(_ context.Context, _ string) (string, error) {
		return "", errors.New("download failed")
	}

	result, err := HandleUpdateApply(env, applyFn, func(_ int) {})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "update apply failed")
	assert.Contains(t, err.Error(), "download failed")
	assert.Nil(t, result)
}

func TestHandleUpdateApply_IndexingInProgress(t *testing.T) {
	t.Parallel()

	statuses := []string{
		mediadb.IndexingStatusRunning,
		mediadb.IndexingStatusPending,
	}

	for _, status := range statuses {
		t.Run(status, func(t *testing.T) {
			t.Parallel()

			mockPlatform := mocks.NewMockPlatform()
			mockPlatform.SetupBasicMock()

			mockMediaDB := helpers.NewMockMediaDBI()
			mockMediaDB.On("GetIndexingStatus").Return(status, nil)

			env := requests.RequestEnv{
				Context:  t.Context(),
				Platform: mockPlatform,
				Database: &database.Database{MediaDB: mockMediaDB},
			}

			applyFn := func(_ context.Context, _ string) (string, error) {
				t.Fatal("applyFn should not be called during indexing")
				return "", nil
			}

			result, err := HandleUpdateApply(env, applyFn, func(_ int) {})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "media indexing is in progress")
			assert.Nil(t, result)

			mockMediaDB.AssertExpectations(t)
		})
	}
}

func TestHandleUpdateApply_IndexingCompleted(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.SetupBasicMock()

	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("GetIndexingStatus").Return(mediadb.IndexingStatusCompleted, nil)

	env := requests.RequestEnv{
		Context:  t.Context(),
		Platform: mockPlatform,
		Database: &database.Database{MediaDB: mockMediaDB},
	}

	applyFn := func(_ context.Context, _ string) (string, error) {
		return "2.10.0", nil
	}

	result, err := HandleUpdateApply(env, applyFn, func(_ int) {})
	require.NoError(t, err)

	resp, ok := result.(models.UpdateApplyResponse)
	require.True(t, ok)
	assert.Equal(t, "2.10.0", resp.NewVersion)

	mockMediaDB.AssertExpectations(t)
}
