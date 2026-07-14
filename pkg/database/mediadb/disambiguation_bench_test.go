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
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/require"
)

// BenchmarkRecomputeDisambiguation_System measures the system-scoped disambiguation
// recompute — the single-pass set-based aggregation run once per system at index time —
// over titles shaped like the arcade "Jackal" case: three siblings sharing one tag, with
// two of them each carrying a distinct extra tag so presence/absence disambiguation fires.
func BenchmarkRecomputeDisambiguation_System(b *testing.B) {
	for _, titles := range []int{1000, 10000, 50000} {
		b.Run(fmt.Sprintf("titles_%d", titles), func(b *testing.B) {
			b.ReportAllocs()
			mediaDB, cleanup := setupBenchDisambDB(b, titles)
			defer cleanup()
			ctx := context.Background()
			for b.Loop() {
				if err := mediaDB.RecomputeSystemDisambiguation(ctx, []int64{1}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// setupBenchDisambDB seeds a MediaDB with `titles` multi-media titles in one system. Each
// title has three media: all share region:world, the second adds input:joystick:rotary,
// the third adds unlicensed:bootleg — so every title disambiguates via presence/absence.
func setupBenchDisambDB(b *testing.B, titles int) (mediaDB *MediaDB, cleanup func()) {
	b.Helper()
	tempDir, err := os.MkdirTemp("", "zaparoo-bench-disamb-*")
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

	ctx := context.Background()
	conn := mediaDB.sql.Load()

	_, err = conn.ExecContext(ctx, `
		INSERT INTO Systems (DBID, SystemID, Name) VALUES (1, 'Bench', 'Bench');
		INSERT INTO TagTypes (DBID, Type, IsExclusive) VALUES
			(1, 'region', 0), (2, 'input', 0), (3, 'unlicensed', 0);
		INSERT INTO Tags (DBID, TypeDBID, Tag) VALUES
			(1, 1, 'world'), (2, 2, 'joystick:rotary'), (3, 3, 'bootleg');
	`)
	require.NoError(b, err)

	_, err = conn.ExecContext(ctx, fmt.Sprintf(`
		WITH RECURSIVE seq(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM seq WHERE i < %d)
		INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name)
			SELECT i, 1, 'game-' || i, 'Game ' || i FROM seq;
		WITH RECURSIVE seq(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM seq WHERE i < %d)
		INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path, IsMissing)
			SELECT (i-1)*3 + j, i, 1, 'game-' || i || '-' || j, 0
			FROM seq, (SELECT 1 AS j UNION SELECT 2 UNION SELECT 3);
		INSERT INTO MediaTags (MediaDBID, TagDBID) SELECT DBID, 1 FROM Media;
		INSERT INTO MediaTags (MediaDBID, TagDBID) SELECT DBID, 2 FROM Media WHERE (DBID %% 3) = 2;
		INSERT INTO MediaTags (MediaDBID, TagDBID) SELECT DBID, 3 FROM Media WHERE (DBID %% 3) = 0;
	`, titles, titles))
	require.NoError(b, err)

	return mediaDB, cleanup
}
