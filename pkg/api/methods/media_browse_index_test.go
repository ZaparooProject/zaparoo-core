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
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func browseIndexOpts(pathPrefix, sort string) any {
	return mock.MatchedBy(func(opts database.BrowseIndexOptions) bool {
		return opts.PathPrefix == pathPrefix && opts.Sort == sort && len(opts.Systems) == 0
	})
}

func newBrowseIndexPlatform(t *testing.T, rootDirs []string) *mocks.MockPlatform {
	t.Helper()
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("SupportedReaders", mock.Anything).Return(nil)
	mockPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).Return(rootDirs)
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).
		Return([]platforms.Launcher{
			{ID: "Steam", SystemID: "pc", Schemes: []string{"steam"}},
		})
	return mockPlatform
}

func TestHandleMediaBrowseIndex_FilesystemPath(t *testing.T) {
	t.Parallel()

	romsRoot := browseTestAbsPath("roms")
	snesPath := filepath.Join(romsRoot, "SNES")
	prefix := filepath.ToSlash(snesPath) + "/"

	mockPlatform := newBrowseIndexPlatform(t, []string{romsRoot})
	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("BrowseIndex", mock.Anything, browseIndexOpts(prefix, "name-desc")).
		Return(database.BrowseIndexResult{
			Scheme:     "latin",
			SortMode:   "name-desc",
			TotalFiles: 3,
			Buckets: []database.BrowseIndexBucket{
				{Key: "A", AtStart: true},
				{Key: "B", SortValue: "Apex", LastID: 7, Count: 2},
			},
		}, nil)

	sortOrder := "name-desc"
	env := newBrowseEnv(t, mockMediaDB, mockPlatform, models.BrowseParams{Path: &snesPath, Sort: &sortOrder})
	result, err := HandleMediaBrowseIndex(env)
	require.NoError(t, err)

	res, ok := result.(models.BrowseIndexResults)
	require.True(t, ok)
	assert.Equal(t, "latin", res.Scheme)
	assert.Equal(t, 3, res.TotalFiles)
	require.Len(t, res.Groups, 2)

	// The leading bucket has no preceding row, so its cursor is empty (page 1).
	assert.Equal(t, "A", res.Groups[0].Key)
	assert.Equal(t, "A", res.Groups[0].Label)
	assert.Empty(t, res.Groups[0].Cursor)

	// The second bucket carries a real seek cursor that decodes to its keyset.
	assert.Equal(t, "B", res.Groups[1].Key)
	require.NotEmpty(t, res.Groups[1].Cursor)
	cursor, err := decodeBrowseCursor(res.Groups[1].Cursor)
	require.NoError(t, err)
	require.NotNil(t, cursor)
	assert.Equal(t, "Apex", cursor.SortValue)
	assert.Equal(t, int64(7), cursor.LastID)
	assert.Equal(t, "name-desc", cursor.SortMode)
	assert.Equal(t, 3, cursor.TotalFiles)

	mockMediaDB.AssertExpectations(t)
}

func TestHandleMediaBrowseIndex_VirtualPath(t *testing.T) {
	t.Parallel()

	mockPlatform := newBrowseIndexPlatform(t, []string{browseTestAbsPath("roms")})
	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("BrowseIndex", mock.Anything, browseIndexOpts("steam://", "")).
		Return(database.BrowseIndexResult{
			Scheme:     "latin",
			SortMode:   "name-asc",
			TotalFiles: 1,
			Buckets:    []database.BrowseIndexBucket{{Key: "A", AtStart: true, Count: 1}},
		}, nil)

	steamPath := "steam://"
	env := newBrowseEnv(t, mockMediaDB, mockPlatform, models.BrowseParams{Path: &steamPath})
	result, err := HandleMediaBrowseIndex(env)
	require.NoError(t, err)

	res, ok := result.(models.BrowseIndexResults)
	require.True(t, ok)
	require.Len(t, res.Groups, 1)
	assert.Equal(t, "A", res.Groups[0].Key)
	mockMediaDB.AssertExpectations(t)
}

func TestHandleMediaBrowseIndex_RootReturnsNoRail(t *testing.T) {
	t.Parallel()

	mockPlatform := newBrowseIndexPlatform(t, []string{browseTestAbsPath("roms")})
	mockMediaDB := helpers.NewMockMediaDBI()

	env := newBrowseEnv(t, mockMediaDB, mockPlatform, nil)
	result, err := HandleMediaBrowseIndex(env)
	require.NoError(t, err)

	res, ok := result.(models.BrowseIndexResults)
	require.True(t, ok)
	assert.Equal(t, "none", res.Scheme)
	assert.Empty(t, res.Groups)
	// No DB call for a root listing.
	mockMediaDB.AssertNotCalled(t, "BrowseIndex", mock.Anything, mock.Anything)
}

func TestHandleMediaBrowseIndex_UnknownVirtualScheme(t *testing.T) {
	t.Parallel()

	mockPlatform := newBrowseIndexPlatform(t, []string{browseTestAbsPath("roms")})
	mockMediaDB := helpers.NewMockMediaDBI()

	bogus := "bogus://"
	env := newBrowseEnv(t, mockMediaDB, mockPlatform, models.BrowseParams{Path: &bogus})
	_, err := HandleMediaBrowseIndex(env)
	require.Error(t, err)
	mockMediaDB.AssertNotCalled(t, "BrowseIndex", mock.Anything, mock.Anything)
}

func TestHandleMediaBrowseIndex_OutsideRootRejected(t *testing.T) {
	t.Parallel()

	mockPlatform := newBrowseIndexPlatform(t, []string{browseTestAbsPath("roms")})
	mockMediaDB := helpers.NewMockMediaDBI()

	outside := browseTestAbsPath("etc", "secrets")
	env := newBrowseEnv(t, mockMediaDB, mockPlatform, models.BrowseParams{Path: &outside})
	_, err := HandleMediaBrowseIndex(env)
	require.Error(t, err)
	mockMediaDB.AssertNotCalled(t, "BrowseIndex", mock.Anything, mock.Anything)
}
