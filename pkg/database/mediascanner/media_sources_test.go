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

package mediascanner

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/require"
)

func TestNormalizeScanSource(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "games")
	valid := &platforms.MediaSource{Path: filepath.Join(root, "Game"), Root: root, Kind: platforms.MediaSourceDirectory}
	got, err := normalizeScanSource("test://one/One", valid)
	require.NoError(t, err)
	require.Equal(t, filepath.Clean(valid.Path), got.Path)
	require.NotEmpty(t, got.Key)

	for _, source := range []*platforms.MediaSource{
		{Path: valid.Path, Root: root, Kind: "unknown"},
		{Path: "relative", Root: root, Kind: platforms.MediaSourceDirectory},
		{Path: filepath.Join(root, "..", "escape"), Root: root, Kind: platforms.MediaSourceDirectory},
		{Path: valid.Path + "\x00", Root: root, Kind: platforms.MediaSourceDirectory},
		{
			Path: valid.Path, Root: filepath.VolumeName(root) + string(filepath.Separator),
			Kind: platforms.MediaSourceDirectory,
		},
	} {
		_, normalizeErr := normalizeScanSource("test://one/One", source)
		require.Error(t, normalizeErr)
	}
	_, err = normalizeScanSource(filepath.Join(root, "game.rom"), valid)
	require.Error(t, err)
}

func TestMediaSourceReconcileLifecycle(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	defer cleanup()
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "games")
	path := "test://one/One"
	source := &platforms.MediaSource{Path: filepath.Join(root, "One"), Root: root, Kind: platforms.MediaSourceDirectory}

	reconcile := func(src *platforms.MediaSource, incomplete bool) {
		require.NoError(t, SeedCanonicalTags(ctx, mediaDB))
		require.NoError(t, mediaDB.BeginTransaction(true))
		require.NoError(t, mediaDB.ClearScanStage())
		require.NoError(t, StageMediaPath(&StageMediaPathParams{
			DB: mediaDB, SystemID: "NES", Path: path, NoExt: true, Source: src,
		}))
		_, err := mediaDB.ReconcileStagedSystem(ctx, "NES", database.ScanReconcileOpts{IncompleteScan: incomplete})
		require.NoError(t, err)
		require.NoError(t, mediaDB.CommitTransaction())
	}

	reconcile(source, false)
	rows, err := mediaDB.GetMediaSourcesForScrape(ctx, "NES", nil)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, filepath.Clean(source.Path), rows[0].SourcePath)

	reconcile(nil, true)
	rows, err = mediaDB.GetMediaSourcesForScrape(ctx, "NES", nil)
	require.NoError(t, err)
	require.Len(t, rows, 1, "incomplete scan must preserve absent source provenance")

	reconcile(nil, false)
	rows, err = mediaDB.GetMediaSourcesForScrape(ctx, "NES", nil)
	require.NoError(t, err)
	require.Empty(t, rows)
}

func TestCoalesceScanResultsRejectsConflictingSources(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "games")
	path := "test://one/One"
	files := []platforms.ScanResult{
		{
			Path: path,
			Source: &platforms.MediaSource{
				Path: filepath.Join(root, "A"), Root: root, Kind: platforms.MediaSourceDirectory,
			},
		},
		{
			Path: path,
			Source: &platforms.MediaSource{
				Path: filepath.Join(root, "B"), Root: root, Kind: platforms.MediaSourceDirectory,
			},
		},
		{
			Path: path,
			Source: &platforms.MediaSource{
				Path: filepath.Join(root, "A"), Root: root, Kind: platforms.MediaSourceDirectory,
			},
		},
	}
	got := coalesceScanResults("NES", files)
	require.Len(t, got, 1)
	require.Nil(t, got[0].Source)
}
