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
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestEquivalentMediaIDsNilAndDisabledGuards(t *testing.T) {
	t.Parallel()

	row := &database.MediaFullRow{Media: database.Media{DBID: 10}}

	ids, err := equivalentMediaIDs(nil, nil)
	require.NoError(t, err)
	assert.Nil(t, ids)

	ids, err = equivalentMediaIDs(nil, row)
	require.NoError(t, err)
	assert.Equal(t, []int64{10}, ids)

	ids, err = equivalentMediaIDs(&requests.RequestEnv{}, row)
	require.NoError(t, err)
	assert.Equal(t, []int64{10}, ids)

	ids, err = equivalentMediaIDs(&requests.RequestEnv{Database: &database.Database{}}, row)
	require.NoError(t, err)
	assert.Equal(t, []int64{10}, ids)
}

func TestEquivalentMediaIDsParentChildAliases(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	platform := mocks.NewMockPlatform()
	platform.On("Settings").Return(platforms.Settings{ZipsAsDirs: true})

	row := &database.MediaFullRow{
		Media: database.Media{
			DBID:      20,
			Path:      "roms/Game.zip/Game.nes",
			ParentDir: "roms/Game.zip/",
		},
		System: database.System{DBID: 1, SystemID: "NES"},
	}
	parent := &database.Media{DBID: 10, Path: "roms/Game.zip"}

	mockDB.On("FindSingleDescendantMedia", mock.Anything, int64(1), "roms/Game.zip").
		Return(&database.Media{DBID: 20, Path: row.Path}, nil)
	mockDB.On("FindMediaBySystemAndPath", mock.Anything, int64(1), "roms/Game.zip").Return(parent, nil)

	env := &requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{MediaDB: mockDB},
		Platform: platform,
	}
	ids, err := equivalentMediaIDs(env, row)
	require.NoError(t, err)
	assert.Equal(t, []int64{20, 10}, ids)
	mockDB.AssertExpectations(t)
	platform.AssertExpectations(t)
}

func TestEquivalentMediaIDsSkipsEmptyOrSelfParent(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	platform := mocks.NewMockPlatform()
	platform.On("Settings").Return(platforms.Settings{ZipsAsDirs: true}).Twice()
	env := &requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{MediaDB: mockDB},
		Platform: platform,
	}

	plain := &database.MediaFullRow{
		Media:  database.Media{DBID: 20, Path: "roms/Game.nes"},
		System: database.System{DBID: 1},
	}
	ids, err := equivalentMediaIDs(env, plain)
	require.NoError(t, err)
	assert.Equal(t, []int64{plain.DBID}, ids)
	mockDB.AssertNumberOfCalls(t, "FindSingleDescendantMedia", 0)

	zipSelf := &database.MediaFullRow{
		Media:  database.Media{DBID: 21, Path: "roms/Game.zip", ParentDir: "roms/Game.zip/"},
		System: database.System{DBID: 1},
	}
	mockDB.On("FindSingleDescendantMedia", mock.Anything, zipSelf.System.DBID, zipSelf.Path).
		Return((*database.Media)(nil), nil).Once()
	ids, err = equivalentMediaIDs(env, zipSelf)
	require.NoError(t, err)
	assert.Equal(t, []int64{zipSelf.DBID}, ids)

	mockDB.AssertExpectations(t)
	platform.AssertExpectations(t)
}

func TestMergeMediaTagsDedupesAndPreservesPrimary(t *testing.T) {
	t.Parallel()

	primary := []database.TagInfo{
		{Type: "region", Tag: "us"},
		{Type: "lang", Tag: "en"},
	}
	alias := []database.TagInfo{
		{Type: "region", Tag: "us"},
		{Type: "region", Tag: "jp"},
	}

	assert.Equal(t, []database.TagInfo{
		{Type: "region", Tag: "us"},
		{Type: "lang", Tag: "en"},
		{Type: "region", Tag: "jp"},
	}, mergeMediaTags(primary, alias))
}

func TestMergeMediaPropertiesDedupesAndPreservesPrimary(t *testing.T) {
	t.Parallel()

	primary := []database.MediaProperty{
		{TypeTag: "property:image-boxart", Text: "primary.png"},
		{TypeTag: "property:description", Text: "primary desc"},
	}
	alias := []database.MediaProperty{
		{TypeTag: "property:image-boxart", Text: "alias.png"},
		{TypeTag: "property:image-screenshot", Text: "shot.png"},
	}

	assert.Equal(t, []database.MediaProperty{
		{TypeTag: "property:image-boxart", Text: "primary.png"},
		{TypeTag: "property:description", Text: "primary desc"},
		{TypeTag: "property:image-screenshot", Text: "shot.png"},
	}, mergeMediaProperties(primary, alias))
}
