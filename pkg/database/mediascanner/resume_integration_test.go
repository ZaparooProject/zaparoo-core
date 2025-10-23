// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

package mediascanner

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResumeScenarios tests basic resume functionality setup
// This is a placeholder for when the actual resume functions are implemented
func TestResumeScenarios(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	mockMediaDB := &helpers.MockMediaDBI{}

	// Mock GetMax*ID calls for resume functionality
	mockMediaDB.On("GetMaxSystemID").Return(int64(5), nil)
	mockMediaDB.On("GetMaxTitleID").Return(int64(10), nil)
	mockMediaDB.On("GetMaxMediaID").Return(int64(15), nil)
	mockMediaDB.On("GetMaxTagTypeID").Return(int64(3), nil)
	mockMediaDB.On("GetMaxTagID").Return(int64(8), nil)
	mockMediaDB.On("GetMaxMediaTagID").Return(int64(20), nil)

	// Test that the new methods exist on the interface
	systemID, err := mockMediaDB.GetMaxSystemID()
	require.NoError(t, err)
	assert.Equal(t, int64(5), systemID)

	titleID, err := mockMediaDB.GetMaxTitleID()
	require.NoError(t, err)
	assert.Equal(t, int64(10), titleID)

	mediaID, err := mockMediaDB.GetMaxMediaID()
	require.NoError(t, err)
	assert.Equal(t, int64(15), mediaID)

	tagTypeID, err := mockMediaDB.GetMaxTagTypeID()
	require.NoError(t, err)
	assert.Equal(t, int64(3), tagTypeID)

	tagID, err := mockMediaDB.GetMaxTagID()
	require.NoError(t, err)
	assert.Equal(t, int64(8), tagID)

	mediaTagID, err := mockMediaDB.GetMaxMediaTagID()
	require.NoError(t, err)
	assert.Equal(t, int64(20), mediaTagID)

	mockMediaDB.AssertExpectations(t)
}

// TestIndexingStatusMethods tests the new indexing status tracking methods
func TestIndexingStatusMethods(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	mockMediaDB := &helpers.MockMediaDBI{}

	// Mock indexing status methods
	mockMediaDB.On("SetIndexingStatus", "running").Return(nil)
	mockMediaDB.On("GetIndexingStatus").Return("running", nil)
	mockMediaDB.On("SetLastIndexedSystem", "nes").Return(nil)
	mockMediaDB.On("GetLastIndexedSystem").Return("nes", nil)

	// Test status setting and getting
	err := mockMediaDB.SetIndexingStatus("running")
	require.NoError(t, err)

	status, err := mockMediaDB.GetIndexingStatus()
	require.NoError(t, err)
	assert.Equal(t, "running", status)

	// Test system tracking
	err = mockMediaDB.SetLastIndexedSystem("nes")
	require.NoError(t, err)

	lastSystem, err := mockMediaDB.GetLastIndexedSystem()
	require.NoError(t, err)
	assert.Equal(t, "nes", lastSystem)

	mockMediaDB.AssertExpectations(t)
}
