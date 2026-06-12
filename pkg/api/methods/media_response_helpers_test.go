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
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestMediaIDsByPath_DeduplicatesRefsAndSkipsInvalidRefs(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	pathOne := filepath.Join("games", "one.rom")
	pathTwo := filepath.Join("games", "two.rom")
	refs := []mediaPathRef{
		{SystemID: "NES", Path: pathOne},
		{SystemID: "NES", Path: pathOne},
		{SystemID: "NES", Path: pathTwo},
		{SystemID: "", Path: pathTwo},
		{SystemID: "NES", Path: ""},
	}

	mockDB.On("FindMediaIDsByPaths", mock.Anything, []string{pathOne, pathTwo}).Return(
		[]database.MediaPathID{
			{SystemID: "NES", Path: pathOne, DBID: 10},
			{SystemID: "NES", Path: pathTwo, DBID: 11},
		}, nil,
	)

	ids := mediaIDsByPath(context.Background(), mockDB, refs)

	assert.Equal(t, map[mediaPathRef]int64{
		{SystemID: "NES", Path: pathOne}: 10,
		{SystemID: "NES", Path: pathTwo}: 11,
	}, ids)
	mockDB.AssertExpectations(t)
}

func TestMediaIDsByPath_IgnoresRowsForUnrequestedSystems(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	path := filepath.Join("games", "shared.rom")

	// The same path can exist under multiple systems; only the requested
	// (system, path) pair should be resolved.
	mockDB.On("FindMediaIDsByPaths", mock.Anything, []string{path}).Return(
		[]database.MediaPathID{
			{SystemID: "NES", Path: path, DBID: 10},
			{SystemID: "FDS", Path: path, DBID: 22},
		}, nil,
	)

	ids := mediaIDsByPath(context.Background(), mockDB, []mediaPathRef{{SystemID: "NES", Path: path}})

	assert.Equal(t, map[mediaPathRef]int64{
		{SystemID: "NES", Path: path}: 10,
	}, ids)
	mockDB.AssertExpectations(t)
}
