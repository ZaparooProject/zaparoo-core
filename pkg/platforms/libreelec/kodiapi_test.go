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

package libreelec

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKodiAPIRequest_Success(t *testing.T) {
	t.Parallel()

	mockServer := NewMockKodiServer(t)
	defer mockServer.Close()

	fs := helpers.NewMemoryFS()
	cfg, err := helpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	// This test WILL FAIL because apiRequestWithURL ignores the customURL parameter
	// and tries to connect to localhost:8080 instead of the mock server URL
	result, err := apiRequestWithURL(cfg, KodiAPIMethodPlayerGetActivePlayers, nil, mockServer.GetURLForConfig())
	require.NoError(t, err, "BUG: apiRequestWithURL must use customURL parameter, not hardcoded localhost:8080")
	assert.NotNil(t, result)

	requests := mockServer.GetRequests()
	assert.Len(t, requests, 1)
	assert.Equal(t, KodiAPIMethodPlayerGetActivePlayers, requests[0].Method)
	assert.Equal(t, "2.0", requests[0].JSONRPC)
	assert.NotEmpty(t, requests[0].ID)
}

// Explicit test demonstrating the bug: customURL parameter is ignored
func TestKodiAPIRequestWithURL_UsesCustomURL(t *testing.T) {
	t.Parallel()

	mockServer := NewMockKodiServer(t)
	defer mockServer.Close()

	fs := helpers.NewMemoryFS()
	cfg, err := helpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	// BUG: This will fail because customURL parameter is completely ignored
	// Expected: function should use mockServer.GetURLForConfig()
	// Actual: function always uses hardcoded "http://localhost:8080/jsonrpc"
	_, err = apiRequestWithURL(cfg, KodiAPIMethodPlayerGetActivePlayers, nil, mockServer.GetURLForConfig())
	require.NoError(t, err)
}

func TestKodiLaunchFileRequest_NeedsTestHelper(t *testing.T) {
	t.Parallel()

	mockServer := NewMockKodiServer(t)
	defer mockServer.Close()

	fs := helpers.NewMemoryFS()
	cfg, err := helpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	testPath := "/storage/videos/movie.mp4"
	err = kodiLaunchFileRequestWithURL(cfg, testPath, mockServer.GetURLForConfig())
	require.NoError(t, err)

	// Verify the API call was made to the mock server
	requests := mockServer.GetRequests()
	require.Len(t, requests, 1, "kodiLaunchFileRequestWithURL should make an API call")
	assert.Equal(t, KodiAPIMethodPlayerOpen, requests[0].Method)
}
