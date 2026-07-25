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

package database_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestLookupMediaIdentity_ExcludesUserOwnedTags(t *testing.T) {
	t.Parallel()

	path := filepath.Join("games", "snes", "Game.sfc")
	mediaDB := testhelpers.NewMockMediaDBI()
	mediaDB.On("SearchMediaPathExact", mock.Anything, mock.Anything, path).
		Return([]database.SearchResult{{
			SystemID: systemdefs.SystemSNES,
			Name:     "Game",
			Path:     path,
			MediaID:  42,
		}}, nil).Once()
	mediaDB.On("GetMediaTagsByMediaDBID", mock.Anything, int64(42)).
		Return([]database.TagInfo{
			{Type: "extension", Tag: "sfc"},
			{Type: "region", Tag: "us"},
			{Type: "user", Tag: "favorite"},
		}, nil).Once()

	identity, found := database.LookupMediaIdentity(
		context.Background(), mediaDB, systemdefs.SystemSNES, path,
	)

	assert.True(t, found)
	assert.Equal(t, "Game", identity.Name)
	assert.Equal(t, []string{"extension:sfc", "region:us"}, identity.Tags)
	mediaDB.AssertExpectations(t)
}

func TestLookupMediaIdentity_ReportsSuccessfulEmptyTags(t *testing.T) {
	t.Parallel()

	path := filepath.Join("games", "snes", "Untagged.sfc")
	mediaDB := testhelpers.NewMockMediaDBI()
	mediaDB.On("SearchMediaPathExact", mock.Anything, mock.Anything, path).
		Return([]database.SearchResult{{
			SystemID: systemdefs.SystemSNES,
			Name:     "Untagged",
			Path:     path,
			MediaID:  43,
		}}, nil).Once()
	mediaDB.On("GetMediaTagsByMediaDBID", mock.Anything, int64(43)).
		Return([]database.TagInfo{}, nil).Once()

	identity, found := database.LookupMediaIdentity(
		context.Background(), mediaDB, systemdefs.SystemSNES, path,
	)

	assert.True(t, found)
	assert.Equal(t, "Untagged", identity.Name)
	assert.Empty(t, identity.Tags)
	mediaDB.AssertExpectations(t)
}
