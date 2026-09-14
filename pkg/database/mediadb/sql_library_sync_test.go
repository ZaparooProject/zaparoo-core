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
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testLibraryFingerprint(fill byte) string {
	return "sha256:" + strings.Repeat(string("0123456789abcdef"[fill%16]), 64)
}

func TestLibraryMediaPage(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	t.Cleanup(cleanup)
	ctx := context.Background()

	_, _, nes := setupDisambTitle(t, mediaDB, "NES", "Metroid", []disambTitleMedia{
		{path: "roms/nes/Metroid (USA).nes", tags: map[string]string{"region": "us"}},
		{path: "roms/nes/Metroid (Europe).nes", tags: map[string]string{"region": "eu"}},
	})
	_, _, snes := setupDisambTitle(t, mediaDB, "SNES", "Super Metroid", []disambTitleMedia{
		{path: "roms/snes/Super Metroid.sfc"},
	})
	_, err := mediaDB.sql.Load().ExecContext(ctx, `UPDATE Media SET IsMissing = 1 WHERE DBID = ?`, nes[1])
	require.NoError(t, err)

	first, err := mediaDB.LibraryMediaPage(ctx, 0, 1)
	require.NoError(t, err)
	require.Len(t, first, 1)
	assert.Equal(t, database.LibraryMediaRow{
		MediaDBID: nes[0], SystemID: "NES", Name: "Metroid", Slug: "metroid",
	}, first[0])

	rest, err := mediaDB.LibraryMediaPage(ctx, first[0].MediaDBID, 10)
	require.NoError(t, err)
	require.Len(t, rest, 1, "a missing file is not in the library")
	assert.Equal(t, snes[0], rest[0].MediaDBID)
	assert.Equal(t, "SNES", rest[0].SystemID)

	done, err := mediaDB.LibraryMediaPage(ctx, rest[0].MediaDBID, 10)
	require.NoError(t, err)
	assert.Empty(t, done)
}

func TestLibraryOrdinalCache(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	t.Cleanup(cleanup)
	ctx := context.Background()
	resolvedAt := time.Unix(1_700_000_000, 0).UTC()

	resolved := database.LibraryOrdinal{
		Fingerprint: testLibraryFingerprint(1), Ordinal: 42, ResolvedAt: resolvedAt, SeenGeneration: 3,
	}
	rejected := database.LibraryOrdinal{
		Fingerprint: testLibraryFingerprint(2), Code: "incompatible_identity",
		ResolvedAt: resolvedAt, SeenGeneration: 3,
	}
	require.NoError(t, mediaDB.PutLibraryOrdinals(ctx, []database.LibraryOrdinal{resolved, rejected}))

	found, err := mediaDB.GetLibraryOrdinals(ctx, []string{
		testLibraryFingerprint(1), testLibraryFingerprint(2), testLibraryFingerprint(3),
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]database.LibraryOrdinal{
		resolved.Fingerprint: resolved, rejected.Fingerprint: rejected,
	}, found)

	// A later answer replaces the earlier one without moving the seen mark back.
	moved := resolved
	moved.Ordinal = 77
	moved.SeenGeneration = 0
	require.NoError(t, mediaDB.PutLibraryOrdinals(ctx, []database.LibraryOrdinal{moved}))
	found, err = mediaDB.GetLibraryOrdinals(ctx, []string{resolved.Fingerprint})
	require.NoError(t, err)
	assert.Equal(t, uint32(77), found[resolved.Fingerprint].Ordinal)
	assert.Equal(t, int64(3), found[resolved.Fingerprint].SeenGeneration)

	require.NoError(t, mediaDB.MarkLibraryOrdinalsSeen(ctx, []string{resolved.Fingerprint}, 5))
	removed, err := mediaDB.PruneLibraryOrdinals(ctx, 5)
	require.NoError(t, err)
	assert.Equal(t, int64(1), removed, "only the answer no walk met since generation 5 is dropped")
	found, err = mediaDB.GetLibraryOrdinals(ctx, []string{resolved.Fingerprint, rejected.Fingerprint})
	require.NoError(t, err)
	assert.Contains(t, found, resolved.Fingerprint)
	assert.NotContains(t, found, rejected.Fingerprint)

	require.NoError(t, mediaDB.ClearLibraryOrdinalCache(ctx))
	found, err = mediaDB.GetLibraryOrdinals(ctx, []string{resolved.Fingerprint})
	require.NoError(t, err)
	assert.Empty(t, found)

	_, err = mediaDB.GetLibraryOrdinals(ctx, []string{"md5:abc"})
	require.Error(t, err, "a fingerprint that is not a sha256 digest is refused")
}

func TestLibraryOrdinalLookupChunks(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	t.Cleanup(cleanup)
	ctx := context.Background()

	count := batchLookupIDsPerQuery*2 + 7
	ordinals := make([]database.LibraryOrdinal, 0, count)
	fingerprints := make([]string, 0, count)
	for i := range count {
		fingerprint := "sha256:" + strings.Repeat("0", 56) + hexWord(i)
		fingerprints = append(fingerprints, fingerprint)
		ordinals = append(ordinals, database.LibraryOrdinal{
			Fingerprint: fingerprint, Ordinal: uint32(i + 1), ResolvedAt: time.Unix(1, 0).UTC(),
		})
	}
	require.NoError(t, mediaDB.PutLibraryOrdinals(ctx, ordinals))
	found, err := mediaDB.GetLibraryOrdinals(ctx, fingerprints)
	require.NoError(t, err)
	assert.Len(t, found, count)
}

func hexWord(i int) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 8)
	for pos := 7; pos >= 0; pos-- {
		out[pos] = digits[i&0xf]
		i >>= 4
	}
	return string(out)
}

func TestLibraryInventoryState(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	t.Cleanup(cleanup)
	ctx := context.Background()

	empty, err := mediaDB.GetLibraryInventoryState(ctx)
	require.NoError(t, err)
	assert.Equal(t, database.LibraryInventoryState{}, empty)

	want := database.LibraryInventoryState{
		CommittedAt: time.Unix(1_700_000_000, 0).UTC(),
		Endpoint:    "https://api.zaparoo.com",
		SHA256:      strings.Repeat("a", 64),
		Generation:  9,
		ItemCount:   1234,
	}
	require.NoError(t, mediaDB.SetLibraryInventoryState(ctx, &want))
	got, err := mediaDB.GetLibraryInventoryState(ctx)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}
