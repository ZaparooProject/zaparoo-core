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
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupImagePathsTestDB(t *testing.T) *MediaDB {
	t.Helper()
	db, cleanup := setupScraperTestDB(t)
	t.Cleanup(cleanup)
	_, err := db.sql.Load().ExecContext(context.Background(), `
		INSERT INTO Tags (DBID, TypeDBID, Tag) VALUES (3, 3, 'image-screenshot');
		INSERT INTO MediaProperties (MediaDBID, TypeTagDBID, Text) VALUES
			(1, 2, '/data/zaparoo/libretro-thumbnails/Nintendé/Named_Boxarts/A.png'),
			(1, 3, '/elsewhere/libretro-thumbnails/Nintendé/Named_Snaps/A.png'),
			(1, 1, '/data/zaparoo/libretro-thumbnails/not an image');
		INSERT INTO MediaTitleProperties (MediaTitleDBID, TypeTagDBID, Text) VALUES
			(1, 3, '/data/zaparoo/libretro-thumbnails/Nintendé/Named_Snaps/A.png');
	`)
	require.NoError(t, err)
	return db
}

func imagePropertyTexts(t *testing.T, db *MediaDB) []string {
	t.Helper()
	rows, err := db.sql.Load().QueryContext(context.Background(), `
		SELECT Text FROM MediaProperties UNION ALL SELECT Text FROM MediaTitleProperties ORDER BY 1`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var texts []string
	for rows.Next() {
		var text string
		require.NoError(t, rows.Scan(&text))
		texts = append(texts, text)
	}
	require.NoError(t, rows.Err())
	return texts
}

// Only image properties under the old folder are rewritten, at the media and
// title level alike, and non-ASCII folder names are counted in characters.
func TestRewriteImagePropertyPrefix(t *testing.T) {
	t.Parallel()
	db := setupImagePathsTestDB(t)
	applied := false

	n, err := db.RewriteImagePropertyPrefix(context.Background(),
		"/data/zaparoo/libretro-thumbnails/", "images://libretro/", "/",
		func() error {
			applied = true
			return nil
		})

	require.NoError(t, err)
	assert.True(t, applied)
	assert.Equal(t, int64(2), n)
	assert.Equal(t, []string{
		"/data/zaparoo/libretro-thumbnails/not an image",
		"/elsewhere/libretro-thumbnails/Nintendé/Named_Snaps/A.png",
		"images://libretro/Nintendé/Named_Boxarts/A.png",
		"images://libretro/Nintendé/Named_Snaps/A.png",
	}, imagePropertyTexts(t, db))
}

// Windows paths keep backslashes after the prefix; they become slashes.
func TestRewriteImagePropertyPrefixConvertsSeparators(t *testing.T) {
	t.Parallel()
	db, cleanup := setupScraperTestDB(t)
	t.Cleanup(cleanup)
	_, err := db.sql.Load().ExecContext(context.Background(), `
		INSERT INTO MediaProperties (MediaDBID, TypeTagDBID, Text) VALUES
			(1, 2, 'C:\Zaparoo\libretro-thumbnails\NES\Named_Boxarts\A.png')`)
	require.NoError(t, err)

	_, err = db.RewriteImagePropertyPrefix(context.Background(),
		`C:\Zaparoo\libretro-thumbnails\`, "images://libretro/", `\`, nil)

	require.NoError(t, err)
	assert.Equal(t, []string{"images://libretro/NES/Named_Boxarts/A.png"}, imagePropertyTexts(t, db))
}

// A failed move rolls the rewrite back, so the stored paths still find the
// files where they are.
func TestRewriteImagePropertyPrefixRollsBackWhenApplyFails(t *testing.T) {
	t.Parallel()
	db := setupImagePathsTestDB(t)
	before := imagePropertyTexts(t, db)
	moveErr := errors.New("cross-device link")

	_, err := db.RewriteImagePropertyPrefix(context.Background(),
		"/data/zaparoo/libretro-thumbnails/", "images://libretro/", "/",
		func() error { return moveErr })

	require.ErrorIs(t, err, moveErr)
	assert.Equal(t, before, imagePropertyTexts(t, db))
}
