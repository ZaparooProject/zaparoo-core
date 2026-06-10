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

package mediadb

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/require"
)

const browseBenchRows = 1000

func BenchmarkBrowseFiles_TagAttach_100(b *testing.B) {
	benchBrowseFilesTagAttach(b, "no-tags", false)
	benchBrowseFilesTagAttach(b, "with-tags", true)
}

func benchBrowseFilesTagAttach(b *testing.B, name string, withTags bool) {
	b.Helper()
	b.Run(name, func(b *testing.B) {
		b.ReportAllocs()
		ctx := context.Background()
		mediaDB, cleanup := setupBrowseBenchMediaDB(b)
		defer cleanup()
		parentDir := seedBenchBrowseDB(b, mediaDB, browseBenchRows, withTags)

		opts := &database.BrowseFilesOptions{PathPrefix: parentDir, Limit: 100}
		b.ResetTimer()
		for b.Loop() {
			results, err := mediaDB.BrowseFiles(ctx, opts)
			if err != nil {
				b.Fatal(err)
			}
			if len(results) != 100 {
				b.Fatalf("expected 100 results, got %d", len(results))
			}
		}
	})
}

func setupBrowseBenchMediaDB(b *testing.B) (mediaDB *MediaDB, cleanup func()) {
	b.Helper()
	tempDir, err := os.MkdirTemp("", "zaparoo-browse-bench-mediadb-*")
	require.NoError(b, err)

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Settings").Return(platforms.Settings{DataDir: tempDir})

	mediaDB, err = OpenMediaDB(context.Background(), mockPlatform)
	require.NoError(b, err)
	cleanup = func() {
		if mediaDB != nil {
			_ = mediaDB.Close()
		}
		_ = os.RemoveAll(tempDir)
	}
	return mediaDB, cleanup
}

func seedBenchBrowseDB(b *testing.B, mediaDB *MediaDB, rows int, withTags bool) string {
	b.Helper()
	ctx := context.Background()
	parentDir := filepath.ToSlash(filepath.Join(string(filepath.Separator), "roms", "bench")) + "/"
	tx, err := mediaDB.sql.BeginTx(ctx, nil)
	require.NoError(b, err)
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO Systems (DBID, SystemID, Name) VALUES (1, 'Bench', 'Bench');
		INSERT INTO TagTypes (DBID, Type, IsExclusive) VALUES
			(1, 'user', 0),
			(2, 'genre', 0),
			(3, 'year', 1),
			(4, 'developer', 1);
		INSERT INTO Tags (DBID, TypeDBID, Tag) VALUES
			(1, 1, 'favorite'),
			(2, 2, 'action'),
			(3, 2, 'arcade'),
			(4, 3, '1990'),
			(5, 3, '1991'),
			(6, 4, 'bench-dev');
	`)
	require.NoError(b, err)

	titleStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES (?, 1, ?, ?)
	`)
	require.NoError(b, err)
	defer func() { require.NoError(b, titleStmt.Close()) }()

	mediaStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path, ParentDir, SortName) VALUES (?, ?, 1, ?, ?, ?)
	`)
	require.NoError(b, err)
	defer func() { require.NoError(b, mediaStmt.Close()) }()

	mediaTagStmt, err := tx.PrepareContext(ctx, `INSERT INTO MediaTags (MediaDBID, TagDBID) VALUES (?, ?)`)
	require.NoError(b, err)
	defer func() { require.NoError(b, mediaTagStmt.Close()) }()

	titleTagStmt, err := tx.PrepareContext(ctx, `INSERT INTO MediaTitleTags (MediaTitleDBID, TagDBID) VALUES (?, ?)`)
	require.NoError(b, err)
	defer func() { require.NoError(b, titleTagStmt.Close()) }()

	for i := 1; i <= rows; i++ {
		mediaID := int64(i)
		slug := fmt.Sprintf("browse-game-%05d", i)
		name := fmt.Sprintf("Browse Game %05d", i)
		path := filepath.ToSlash(filepath.Join(parentDir, fmt.Sprintf("browse-game-%05d.zip", i)))
		_, err = titleStmt.ExecContext(ctx, mediaID, slug, name)
		require.NoError(b, err)
		_, err = mediaStmt.ExecContext(ctx, mediaID, mediaID, path, parentDir, name)
		require.NoError(b, err)
		if !withTags {
			continue
		}
		if i%10 == 0 {
			_, err = mediaTagStmt.ExecContext(ctx, mediaID, int64(1))
			require.NoError(b, err)
		}
		_, err = mediaTagStmt.ExecContext(ctx, mediaID, int64(2+i%2))
		require.NoError(b, err)
		_, err = titleTagStmt.ExecContext(ctx, mediaID, int64(4+i%2))
		require.NoError(b, err)
		_, err = titleTagStmt.ExecContext(ctx, mediaID, int64(6))
		require.NoError(b, err)
	}

	require.NoError(b, tx.Commit())
	return parentDir
}
