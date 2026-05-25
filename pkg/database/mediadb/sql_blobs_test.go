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
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUpsertMediaBlob_Insert verifies that a new blob is stored and its DBID
// is returned.
func TestUpsertMediaBlob_Insert(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	data := []byte("hello blob")
	dbid, err := mediaDB.UpsertMediaBlob(ctx, "text/plain", data)
	require.NoError(t, err)
	assert.Positive(t, dbid)

	var count int
	require.NoError(t, mediaDB.sql.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM MediaBlobs WHERE DBID = ?", dbid).Scan(&count))
	assert.Equal(t, 1, count)
}

// TestUpsertMediaBlob_Dedup verifies that inserting identical data twice
// returns the same DBID and stores only one row.
func TestUpsertMediaBlob_Dedup(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	data := []byte{0x01, 0x02, 0x03}
	dbid1, err := mediaDB.UpsertMediaBlob(ctx, "application/octet-stream", data)
	require.NoError(t, err)

	dbid2, err := mediaDB.UpsertMediaBlob(ctx, "application/octet-stream", data)
	require.NoError(t, err)

	assert.Equal(t, dbid1, dbid2, "identical data must resolve to the same DBID")

	var count int
	require.NoError(t, mediaDB.sql.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM MediaBlobs").Scan(&count))
	assert.Equal(t, 1, count, "only one row should exist for identical data")
}

// TestUpsertMediaBlob_DifferentData verifies that distinct blobs produce
// distinct DBIDs.
func TestUpsertMediaBlob_DifferentData(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	dbid1, err := mediaDB.UpsertMediaBlob(ctx, "image/png", []byte("image-a"))
	require.NoError(t, err)

	dbid2, err := mediaDB.UpsertMediaBlob(ctx, "image/png", []byte("image-b"))
	require.NoError(t, err)

	assert.NotEqual(t, dbid1, dbid2, "different data must produce different DBIDs")
}

// TestUpsertMediaBlob_DifferentContentType verifies that identical data stored
// under different content types produces distinct DBIDs (content type is part
// of the hash input).
func TestUpsertMediaBlob_DifferentContentType(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	data := []byte("same bytes")
	dbid1, err := mediaDB.UpsertMediaBlob(ctx, "image/png", data)
	require.NoError(t, err)

	dbid2, err := mediaDB.UpsertMediaBlob(ctx, "image/jpeg", data)
	require.NoError(t, err)

	assert.NotEqual(t, dbid1, dbid2, "same data with different content types must produce different DBIDs")

	var count int
	require.NoError(t, mediaDB.sql.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM MediaBlobs").Scan(&count))
	assert.Equal(t, 2, count, "two rows should exist for same data under different content types")
}

func TestUpsertMediaBlob_FramesContentTypeBeforeData(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	dbid1, err := mediaDB.UpsertMediaBlob(ctx, "ab", []byte("c"))
	require.NoError(t, err)

	dbid2, err := mediaDB.UpsertMediaBlob(ctx, "a", []byte("bc"))
	require.NoError(t, err)

	assert.NotEqual(t, dbid1, dbid2, "field boundaries must be part of the hash input")

	var count int
	require.NoError(t, mediaDB.sql.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM MediaBlobs").Scan(&count))
	assert.Equal(t, 2, count, "ambiguous concatenations must not dedupe together")
}

// TestGetMediaBlob_Found verifies that GetMediaBlob returns the correct row.
func TestGetMediaBlob_Found(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	data := []byte("blob content")
	dbid, err := mediaDB.UpsertMediaBlob(ctx, "image/jpeg", data)
	require.NoError(t, err)

	got, err := mediaDB.GetMediaBlob(ctx, dbid)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, dbid, got.DBID)
	assert.Equal(t, "image/jpeg", got.ContentType)
	assert.Equal(t, data, got.Data)
	assert.NotEmpty(t, got.Hash)
}

// TestGetMediaBlob_NotFound verifies that GetMediaBlob returns nil, nil for
// an unknown DBID.
func TestGetMediaBlob_NotFound(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()

	got, err := mediaDB.GetMediaBlob(context.Background(), 9999)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestGetMediaBlobDataCapped_FoundWithinCap(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	want := []byte("blob content")
	dbid, err := mediaDB.UpsertMediaBlob(ctx, "image/png", want)
	require.NoError(t, err)

	got, contentType, err := mediaDB.GetMediaBlobDataCapped(ctx, dbid, int64(len(want)))
	require.NoError(t, err)
	assert.Equal(t, want, got)
	assert.Equal(t, "image/png", contentType)
}

func TestGetMediaBlobDataCapped_OverCapReturnsNil(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	dbid, err := mediaDB.UpsertMediaBlob(ctx, "image/png", []byte("too big"))
	require.NoError(t, err)

	got, contentType, err := mediaDB.GetMediaBlobDataCapped(ctx, dbid, 1)
	require.NoError(t, err)
	assert.Nil(t, got)
	assert.Empty(t, contentType)
}

// TestPruneOrphanedBlobs_NoRefs verifies that a blob with no referencing
// property rows is deleted.
func TestPruneOrphanedBlobs_NoRefs(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	_, err := mediaDB.UpsertMediaBlob(ctx, "image/png", []byte("orphan"))
	require.NoError(t, err)

	deleted, err := mediaDB.PruneOrphanedBlobs(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted)

	var count int
	require.NoError(t, mediaDB.sql.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM MediaBlobs").Scan(&count))
	assert.Equal(t, 0, count)
}

// TestPruneOrphanedBlobs_WithRef verifies that a blob referenced by a
// MediaTitleProperties row is not deleted.
func TestPruneOrphanedBlobs_WithRef(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	data := []byte("kept blob")
	blobDBID, err := mediaDB.UpsertMediaBlob(ctx, "image/png", data)
	require.NoError(t, err)

	// Link the blob via a property.
	props := []database.MediaProperty{
		{TypeTag: "property:image-boxart", BlobDBID: &blobDBID},
	}
	require.NoError(t, mediaDB.UpsertMediaTitleProperties(ctx, 1, props))

	deleted, err := mediaDB.PruneOrphanedBlobs(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), deleted, "referenced blob must not be deleted")

	var count int
	require.NoError(t, mediaDB.sql.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM MediaBlobs").Scan(&count))
	assert.Equal(t, 1, count)
}

// TestPruneOrphanedBlobs_Mixed verifies that exactly one of two blobs (the
// unreferenced one) is pruned.
func TestPruneOrphanedBlobs_Mixed(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	keepID, err := mediaDB.UpsertMediaBlob(ctx, "image/png", []byte("keep"))
	require.NoError(t, err)

	_, err = mediaDB.UpsertMediaBlob(ctx, "image/png", []byte("orphan"))
	require.NoError(t, err)

	// Reference only the first blob.
	props := []database.MediaProperty{
		{TypeTag: "property:image-boxart", BlobDBID: &keepID},
	}
	require.NoError(t, mediaDB.UpsertMediaTitleProperties(ctx, 1, props))

	deleted, err := mediaDB.PruneOrphanedBlobs(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted, "exactly one orphaned blob should be pruned")

	var count int
	require.NoError(t, mediaDB.sql.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM MediaBlobs").Scan(&count))
	assert.Equal(t, 1, count, "the referenced blob must remain")
}

// TestPruneOrphanedBlobs_WithMediaPropsRef verifies that a blob referenced by
// a MediaProperties row (ROM-level) is not deleted by PruneOrphanedBlobs.
func TestPruneOrphanedBlobs_WithMediaPropsRef(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	data := []byte("rom-level blob")
	blobDBID, err := mediaDB.UpsertMediaBlob(ctx, "image/png", data)
	require.NoError(t, err)

	// Link the blob via a ROM-level MediaProperties row (Media DBID=1 from setup).
	props := []database.MediaProperty{
		{TypeTag: "property:image-boxart", BlobDBID: &blobDBID},
	}
	require.NoError(t, mediaDB.UpsertMediaProperties(ctx, 1, props))

	deleted, err := mediaDB.PruneOrphanedBlobs(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), deleted, "ROM-level referenced blob must not be deleted")

	var count int
	require.NoError(t, mediaDB.sql.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM MediaBlobs").Scan(&count))
	assert.Equal(t, 1, count, "the ROM-level referenced blob must remain")
}
