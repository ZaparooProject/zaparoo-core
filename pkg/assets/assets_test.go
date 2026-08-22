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

package assets

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resetSystemMetadataCache(t *testing.T) {
	t.Helper()
	systemMetadataCache = sync.Map{}
	t.Cleanup(func() {
		systemMetadataCache = sync.Map{}
	})
}

func TestGetSystemMetadata_CanonicalAndAlias(t *testing.T) {
	resetSystemMetadataCache(t)

	canonical, err := GetSystemMetadata("Genesis")
	require.NoError(t, err)
	assert.Equal(t, SystemMetadata{
		ID:           "Genesis",
		Name:         "Genesis",
		Category:     "Console",
		ReleaseDate:  "1988-10-29",
		Manufacturer: "Sega",
	}, canonical)

	alias, err := GetSystemMetadata("MegaDrive")
	require.NoError(t, err)
	assert.Equal(t, canonical, alias)
	_, cached := systemMetadataCache.Load("Genesis")
	assert.True(t, cached)
}

func TestGetSystemMetadata_UsesCanonicalCacheEntry(t *testing.T) {
	resetSystemMetadataCache(t)

	cached := SystemMetadata{ID: "cached", Name: "cached metadata"}
	systemMetadataCache.Store("Genesis", cached)

	got, err := GetSystemMetadata("MegaDrive")
	require.NoError(t, err)
	assert.Equal(t, cached, got)
}

func TestGetSystemMetadata_ConcurrentCalls(t *testing.T) {
	resetSystemMetadataCache(t)

	const callers = 32
	results := make(chan SystemMetadata, callers)
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			metadata, err := GetSystemMetadata("MegaDrive")
			results <- metadata
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}
	for metadata := range results {
		assert.Equal(t, "Genesis", metadata.ID)
	}
}
