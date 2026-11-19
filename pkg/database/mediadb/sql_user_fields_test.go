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
	"database/sql"
	"errors"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListMediaUserFieldTitles_NoMarkersIsEmpty(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	titles, err := mediaDB.ListMediaUserFieldTitles(context.Background())
	require.NoError(t, err)
	assert.Empty(t, titles)
}

// A failed lookup is not a database without markers: a reconcile that read
// it as one would leave stale user fields in place.
func TestListMediaUserFieldTitles_ReportsLookupFailure(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := mediaDB.ListMediaUserFieldTitles(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

// userFieldsTitle seeds one NES title with the given files and returns the
// title's DBID and its files' DBIDs in insert order.
func userFieldsTitle(t *testing.T, mediaDB *MediaDB, name string, paths ...string) (titleDBID int64, fileIDs []int64) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, mediaDB.SeedCanonicalTagDefinitions(ctx))
	system := insertSystemWithMedia(t, mediaDB, "NES", name, paths[0])
	sqlDB := mediaDB.sql.Load()
	require.NoError(t, sqlDB.QueryRowContext(ctx,
		`SELECT DBID FROM MediaTitles WHERE Name = ?`, name).Scan(&titleDBID))
	for _, path := range paths[1:] {
		require.NoError(t, mediaDB.BeginTransaction(false))
		_, err := mediaDB.InsertMedia(database.Media{
			SystemDBID: system.DBID, MediaTitleDBID: titleDBID,
			Path: path, ParentDir: "/roms/nes/", SortName: name,
		})
		require.NoError(t, err)
		require.NoError(t, mediaDB.CommitTransaction())
	}
	files, err := mediaDB.ListMediaByTitle(ctx, titleDBID)
	require.NoError(t, err)
	require.Len(t, files, len(paths))
	fileIDs = make([]int64, 0, len(files))
	for _, file := range files {
		fileIDs = append(fileIDs, file.DBID)
	}
	return titleDBID, fileIDs
}

func userFieldsTitleName(t *testing.T, mediaDB *MediaDB, titleDBID int64) (name string, sortNames []string) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, mediaDB.sql.Load().QueryRowContext(ctx,
		`SELECT Name FROM MediaTitles WHERE DBID = ?`, titleDBID).Scan(&name))
	files, err := mediaDB.ListMediaByTitle(ctx, titleDBID)
	require.NoError(t, err)
	sortNames = make([]string, 0, len(files))
	for _, file := range files {
		sortNames = append(sortNames, file.SortName)
	}
	return name, sortNames
}

func userFieldsTitleTags(t *testing.T, mediaDB *MediaDB, titleDBID int64, tagType string) []string {
	t.Helper()
	rows, err := mediaDB.sql.Load().QueryContext(context.Background(), `
		SELECT t.Tag FROM MediaTitleTags mtt
		JOIN Tags t ON t.DBID = mtt.TagDBID
		JOIN TagTypes tt ON tt.DBID = t.TypeDBID
		WHERE mtt.MediaTitleDBID = ? AND tt.Type = ? ORDER BY t.Tag`, titleDBID, tagType)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var values []string
	for rows.Next() {
		var value string
		require.NoError(t, rows.Scan(&value))
		values = append(values, value)
	}
	require.NoError(t, rows.Err())
	return values
}

func userFieldsTitleProperty(t *testing.T, mediaDB *MediaDB, titleDBID int64, typeTag string) (string, bool) {
	t.Helper()
	var text string
	err := mediaDB.sql.Load().QueryRowContext(context.Background(), `
		SELECT mtp.Text FROM MediaTitleProperties mtp
		JOIN Tags t ON t.DBID = mtp.TypeTagDBID
		WHERE mtp.MediaTitleDBID = ? AND t.Tag = ?`, titleDBID, typeTag).Scan(&text)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false
	}
	require.NoError(t, err)
	return text, true
}

func userFieldsFileProperty(t *testing.T, mediaDB *MediaDB, mediaDBID int64, typeTag string) (string, bool) {
	t.Helper()
	var text string
	err := mediaDB.sql.Load().QueryRowContext(context.Background(), `
		SELECT mp.Text FROM MediaProperties mp
		JOIN Tags t ON t.DBID = mp.TypeTagDBID
		WHERE mp.MediaDBID = ? AND t.Tag = ?`, mediaDBID, typeTag).Scan(&text)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false
	}
	require.NoError(t, err)
	return text, true
}

func TestSyncMediaUserFields_UnknownTitleChangesNothing(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	name := "Edited"

	result, err := mediaDB.SyncMediaUserFields(context.Background(), &database.MediaUserTitleFields{
		TitleDBID: 999_999, Name: &name,
	})
	require.NoError(t, err)
	assert.Equal(t, database.MediaUserFieldsSync{}, result)
}

// A user name goes onto the title and every file of it, and removing it puts
// back the names the scanner gave.
func TestSyncMediaUserFields_NameIsAppliedAndRestored(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()
	titleDBID, _ := userFieldsTitle(t, mediaDB, "Scanner Name",
		"/roms/nes/Scanner Name (USA).nes", "/roms/nes/Scanner Name (Europe).nes")
	name := "User Name"

	result, err := mediaDB.SyncMediaUserFields(ctx, &database.MediaUserTitleFields{
		TitleDBID: titleDBID, Name: &name,
	})
	require.NoError(t, err)
	assert.True(t, result.Changed)
	assert.True(t, result.NameChanged)
	assert.Equal(t, "NES", result.SystemID)
	titleName, sortNames := userFieldsTitleName(t, mediaDB, titleDBID)
	assert.Equal(t, "User Name", titleName)
	assert.Equal(t, []string{"User Name", "User Name"}, sortNames)

	titles, err := mediaDB.ListMediaUserFieldTitles(ctx)
	require.NoError(t, err)
	assert.Equal(t, []int64{titleDBID}, titles)
	marker, ok := userFieldsTitleProperty(t, mediaDB, titleDBID, string(tags.TagPropertyUserFields))
	require.True(t, ok)
	decoded := database.DecodeMediaUserFieldsMarker(marker)
	assert.Equal(t, []string{database.MediaUserFieldName}, decoded.Fields)
	assert.Equal(t, "Scanner Name", decoded.OriginalName)

	again, err := mediaDB.SyncMediaUserFields(ctx, &database.MediaUserTitleFields{
		TitleDBID: titleDBID, Name: &name,
	})
	require.NoError(t, err)
	assert.False(t, again.Changed, "a title already in line is not written")

	reset, err := mediaDB.SyncMediaUserFields(ctx, &database.MediaUserTitleFields{TitleDBID: titleDBID})
	require.NoError(t, err)
	assert.True(t, reset.NameChanged)
	assert.Empty(t, reset.ClearedMediaDBIDs, "the scanner's name comes back, so nothing is left to scrape")
	titleName, sortNames = userFieldsTitleName(t, mediaDB, titleDBID)
	assert.Equal(t, "Scanner Name", titleName)
	assert.Equal(t, []string{"Scanner Name", "Scanner Name"}, sortNames)

	_, ok = userFieldsTitleProperty(t, mediaDB, titleDBID, string(tags.TagPropertyUserFields))
	assert.False(t, ok, "a marker that records nothing is removed")
	titles, err = mediaDB.ListMediaUserFieldTitles(ctx)
	require.NoError(t, err)
	assert.Empty(t, titles)
}

// A name the scanner rewrote after the user's was applied is the scanner's
// now, so removing the user's name leaves it alone.
func TestSyncMediaUserFields_ResetKeepsANameTheScannerRewrote(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()
	titleDBID, _ := userFieldsTitle(t, mediaDB, "Old Name", "/roms/nes/Old Name.nes")
	name := "User Name"
	_, err := mediaDB.SyncMediaUserFields(ctx, &database.MediaUserTitleFields{TitleDBID: titleDBID, Name: &name})
	require.NoError(t, err)
	_, err = mediaDB.sql.Load().ExecContext(ctx,
		`UPDATE MediaTitles SET Name = 'Rescanned Name' WHERE DBID = ?`, titleDBID)
	require.NoError(t, err)

	_, err = mediaDB.SyncMediaUserFields(ctx, &database.MediaUserTitleFields{TitleDBID: titleDBID})
	require.NoError(t, err)
	titleName, _ := userFieldsTitleName(t, mediaDB, titleDBID)
	assert.Equal(t, "Rescanned Name", titleName)
}

func TestSyncMediaUserFields_DescriptionIsSetAndCleared(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()
	titleDBID, fileIDs := userFieldsTitle(t, mediaDB, "Game", "/roms/nes/Game.nes")
	description := "The user's words"
	descriptionTag := string(tags.TagPropertyDescription)

	result, err := mediaDB.SyncMediaUserFields(ctx, &database.MediaUserTitleFields{
		TitleDBID: titleDBID, Description: &description,
	})
	require.NoError(t, err)
	assert.True(t, result.Changed)
	text, ok := userFieldsTitleProperty(t, mediaDB, titleDBID, descriptionTag)
	require.True(t, ok)
	assert.Equal(t, description, text)

	result, err = mediaDB.SyncMediaUserFields(ctx, &database.MediaUserTitleFields{TitleDBID: titleDBID})
	require.NoError(t, err)
	assert.True(t, result.Changed)
	assert.Equal(t, fileIDs, result.ClearedMediaDBIDs)
	_, ok = userFieldsTitleProperty(t, mediaDB, titleDBID, descriptionTag)
	assert.False(t, ok)
}

// A description the user never owned is the scrapers', and a sync without
// one leaves it in place.
func TestSyncMediaUserFields_LeavesAScrapedDescription(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()
	titleDBID, _ := userFieldsTitle(t, mediaDB, "Game", "/roms/nes/Game.nes")
	_, err := mediaDB.sql.Load().ExecContext(ctx, `
		INSERT INTO MediaTitleProperties (MediaTitleDBID, TypeTagDBID, Text)
		SELECT ?, DBID, 'scraped' FROM Tags WHERE Tag = ?`, titleDBID, string(tags.TagPropertyDescription))
	require.NoError(t, err)

	result, err := mediaDB.SyncMediaUserFields(ctx, &database.MediaUserTitleFields{TitleDBID: titleDBID})
	require.NoError(t, err)
	assert.False(t, result.Changed)
	text, ok := userFieldsTitleProperty(t, mediaDB, titleDBID, string(tags.TagPropertyDescription))
	require.True(t, ok)
	assert.Equal(t, "scraped", text)
}

func TestSyncMediaUserFields_TagsAreReplacedAndCleared(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()
	titleDBID, fileIDs := userFieldsTitle(t, mediaDB, "Game", "/roms/nes/Game.nes")
	genre := string(tags.TagTypeGenre)
	year := string(tags.TagTypeYear)

	result, err := mediaDB.SyncMediaUserFields(ctx, &database.MediaUserTitleFields{
		TitleDBID: titleDBID,
		Tags: map[string][]database.MediaUserTagValue{
			genre: {{Tag: "action"}, {Tag: "puzzle"}, {Tag: "action"}},
			// An exclusive type keeps one value.
			year: {{Tag: "1990"}, {Tag: "1991"}},
		},
	})
	require.NoError(t, err)
	assert.True(t, result.TagsChanged)
	assert.Equal(t, []string{"action", "puzzle"}, userFieldsTitleTags(t, mediaDB, titleDBID, genre))
	assert.Equal(t, []string{"1990"}, userFieldsTitleTags(t, mediaDB, titleDBID, year))

	result, err = mediaDB.SyncMediaUserFields(ctx, &database.MediaUserTitleFields{
		TitleDBID: titleDBID,
		Tags: map[string][]database.MediaUserTagValue{
			genre: {{Tag: "action"}},
			year:  {{Tag: "1990"}},
		},
	})
	require.NoError(t, err)
	assert.True(t, result.TagsChanged)
	assert.Empty(t, result.ClearedMediaDBIDs, "a replaced tag type is still the user's")
	assert.Equal(t, []string{"action"}, userFieldsTitleTags(t, mediaDB, titleDBID, genre))

	result, err = mediaDB.SyncMediaUserFields(ctx, &database.MediaUserTitleFields{
		TitleDBID: titleDBID,
		Tags:      map[string][]database.MediaUserTagValue{year: {{Tag: "1990"}}},
	})
	require.NoError(t, err)
	assert.True(t, result.TagsChanged)
	assert.Equal(t, fileIDs, result.ClearedMediaDBIDs)
	assert.Empty(t, userFieldsTitleTags(t, mediaDB, titleDBID, genre))
	assert.Equal(t, []string{"1990"}, userFieldsTitleTags(t, mediaDB, titleDBID, year))
}

// Images belong to one file: clearing one file's image reports only that
// file, and SkipImages leaves stored images alone.
func TestSyncMediaUserFields_ImagesArePerFile(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()
	titleDBID, fileIDs := userFieldsTitle(t, mediaDB, "Game",
		"/roms/nes/Game (USA).nes", "/roms/nes/Game (Europe).nes")
	boxart := string(tags.TagPropertyImageBoxart)
	images := map[int64]map[string]string{
		fileIDs[0]: {"boxart": "/images/usa.png"},
		fileIDs[1]: {"boxart": "/images/europe.png"},
	}

	result, err := mediaDB.SyncMediaUserFields(ctx, &database.MediaUserTitleFields{
		TitleDBID: titleDBID, Images: images,
	})
	require.NoError(t, err)
	assert.True(t, result.ImagesChanged)
	path, ok := userFieldsFileProperty(t, mediaDB, fileIDs[1], boxart)
	require.True(t, ok)
	assert.Equal(t, "/images/europe.png", path)
	titles, err := mediaDB.ListMediaUserFieldTitles(ctx)
	require.NoError(t, err)
	assert.Equal(t, []int64{titleDBID}, titles, "a file's marker lists its title")

	result, err = mediaDB.SyncMediaUserFields(ctx, &database.MediaUserTitleFields{
		TitleDBID: titleDBID, SkipImages: true,
	})
	require.NoError(t, err)
	assert.False(t, result.ImagesChanged)
	_, ok = userFieldsFileProperty(t, mediaDB, fileIDs[0], boxart)
	assert.True(t, ok)

	result, err = mediaDB.SyncMediaUserFields(ctx, &database.MediaUserTitleFields{
		TitleDBID: titleDBID,
		Images:    map[int64]map[string]string{fileIDs[0]: {"boxart": "/images/usa.png"}},
	})
	require.NoError(t, err)
	assert.True(t, result.ImagesChanged)
	assert.Equal(t, []int64{fileIDs[1]}, result.ClearedMediaDBIDs)
	_, ok = userFieldsFileProperty(t, mediaDB, fileIDs[1], boxart)
	assert.False(t, ok)
	_, ok = userFieldsFileProperty(t, mediaDB, fileIDs[0], boxart)
	assert.True(t, ok)
}

func TestSyncMediaUserFields_RefusesDuringATransaction(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	titleDBID, _ := userFieldsTitle(t, mediaDB, "Game", "/roms/nes/Game.nes")
	require.NoError(t, mediaDB.BeginTransaction(false))
	t.Cleanup(func() { _ = mediaDB.RollbackTransaction() })

	_, err := mediaDB.SyncMediaUserFields(context.Background(), &database.MediaUserTitleFields{
		TitleDBID: titleDBID,
	})
	require.ErrorIs(t, err, ErrTransactionActive)
}
