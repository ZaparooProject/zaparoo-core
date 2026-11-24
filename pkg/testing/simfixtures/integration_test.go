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

package simfixtures

import (
	"context"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediascanner"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/scantest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// The media scanner must index exactly the files the report says it will,
// and none of the edge cases meant to be skipped. Not parallel: it swaps the
// global launcher cache.
//
//nolint:paralleltest // Swaps helpers.GlobalLauncherCache.
func TestGenerateMatchesScanner(t *testing.T) {
	r := genIn(t, allOptions("snes", "psx", "arcade", "megadrive"))

	launchers := make([]platforms.Launcher, 0, len(r.Systems))
	for _, sys := range r.Systems {
		launchers = append(launchers, platforms.Launcher{
			ID: "Test" + sys.SystemID, SystemID: sys.SystemID, Folders: []string{sys.Folder},
			Extensions: esdeExtensions(sys.Folder),
		})
	}
	cfg, err := testhelpers.NewTestConfig(testhelpers.NewMemoryFS(), t.TempDir())
	require.NoError(t, err)
	platform := mocks.NewMockPlatform()
	platform.On("ID").Return("test-platform")
	platform.On("Settings").Return(platforms.Settings{})
	platform.On("RootDirs", mock.AnythingOfType("*config.Instance")).Return([]string{r.Root})
	platform.On("Launchers", mock.AnythingOfType("*config.Instance")).Return(launchers)

	original := helpers.GlobalLauncherCache
	cache := &helpers.LauncherCache{}
	cache.Initialize(platform, cfg)
	helpers.GlobalLauncherCache = cache
	t.Cleanup(func() { helpers.GlobalLauncherCache = original })

	for _, sys := range r.Systems {
		got, err := mediascanner.GetFiles(context.Background(), cfg, platform, sys.SystemID,
			filepath.Join(r.Root, sys.Folder), nil)
		require.NoError(t, err, sys.Folder)
		slices.Sort(got)
		assert.Equal(t, sys.MediaPaths, got, sys.Folder)
	}
}

func TestBigDB(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	dbPath := filepath.Join(root, "data", "media.db")
	opts := BigDBOptions{Entries: 51, Seed: 3, Systems: []string{"SNES", "arcade", "psx"}}

	report, err := BigDB(ctx, dbPath, opts)
	require.NoError(t, err)
	assert.Equal(t, dbPath, report.Path)
	assert.Equal(t, 51, report.Entries)
	require.Len(t, report.Systems, 3)
	total := 0
	for _, sys := range report.Systems {
		assert.Equal(t, 17, sys.Entries, sys.SystemID)
		assert.EqualValues(t, 17, sys.MediaUpserted, sys.SystemID)
		assert.Positive(t, sys.TitlesInserted, sys.SystemID)
		total += sys.Entries
	}
	assert.Equal(t, 51, total)
	assert.Equal(t, 51, mediaCount(t, root))

	// A second run stages the same paths, so nothing changes.
	again, err := BigDB(ctx, dbPath, opts)
	require.NoError(t, err)
	require.Len(t, again.Systems, 3)
	for _, sys := range again.Systems {
		assert.Equal(t, 17, sys.Entries, sys.SystemID)
		assert.Zero(t, sys.MediaUpserted, sys.SystemID)
		assert.Zero(t, sys.TitlesInserted, sys.SystemID)
	}
	assert.Equal(t, 51, mediaCount(t, root))
}

// Past scantest.IndexBatchSize, Index commits in batches; nothing is lost.
func TestBigDBCrossesIndexBatch(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("stages over 10,000 media")
	}

	root := t.TempDir()
	entries := scantest.IndexBatchSize + 50
	report, err := BigDB(context.Background(), filepath.Join(root, "data", "media.db"),
		BigDBOptions{Entries: entries, Systems: []string{"nes"}})
	require.NoError(t, err)
	require.Len(t, report.Systems, 1)
	assert.EqualValues(t, entries, report.Systems[0].MediaUpserted)
	assert.Equal(t, entries, mediaCount(t, root))
}

func TestBigDBValidation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()
	tests := []struct {
		name    string
		path    string
		wantErr string
		opts    BigDBOptions
	}{
		{name: "no entries", path: filepath.Join(dir, "media.db"), wantErr: "Entries"},
		{name: "wrong file", path: filepath.Join(dir, "other.db"), opts: BigDBOptions{Entries: 1}, wantErr: "media.db"},
		{
			name: "unknown system", path: filepath.Join(dir, "media.db"),
			opts: BigDBOptions{Entries: 1, Systems: []string{"nope"}}, wantErr: "nope",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := BigDB(ctx, tt.path, tt.opts)
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func mediaCount(t *testing.T, root string) int {
	t.Helper()
	db, err := mediadb.OpenMediaDB(context.Background(), dataDirPlatform{dir: filepath.Join(root, "data")})
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()
	n, err := db.GetTotalMediaCount()
	require.NoError(t, err)
	return n
}
