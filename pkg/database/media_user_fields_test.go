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

package database_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/scantest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeUserImages resolves the image names it holds to a path.
type fakeUserImages map[string]string

func (f fakeUserImages) Path(name string) (string, error) {
	if path, ok := f[name]; ok {
		return path, nil
	}
	return "", errors.New("not stored")
}

func (f *reconcileFixture) row(t *testing.T, name string) *database.MediaFullRow {
	t.Helper()
	row, err := f.db.MediaDB.GetMediaWithTitleAndSystem(context.Background(), f.ids[name])
	require.NoError(t, err)
	require.NotNil(t, row)
	return row
}

func (f *reconcileFixture) apply(t *testing.T, name, field string, value *string) database.MediaUserFieldsSync {
	t.Helper()
	result, err := database.ApplyMediaUserField(context.Background(), f.db, f.row(t, name), field, value)
	require.NoError(t, err)
	return result
}

func (f *reconcileFixture) titleProperty(t *testing.T, name string, property tags.TagValue) (string, bool) {
	t.Helper()
	props, err := f.db.MediaDB.GetMediaTitleProperties(context.Background(), f.row(t, name).Title.DBID)
	require.NoError(t, err)
	return propertyText(props, property)
}

func (f *reconcileFixture) mediaProperty(t *testing.T, name string, property tags.TagValue) (string, bool) {
	t.Helper()
	props, err := f.db.MediaDB.GetMediaProperties(context.Background(), f.ids[name])
	require.NoError(t, err)
	return propertyText(props, property)
}

func propertyText(props []database.MediaProperty, property tags.TagValue) (string, bool) {
	for i := range props {
		if props[i].TypeTag == tags.PropertyTypeTag(property) {
			return props[i].Text, true
		}
	}
	return "", false
}

func (f *reconcileFixture) titleTags(t *testing.T, name string, tagType tags.TagType) []string {
	t.Helper()
	all, err := f.db.MediaDB.GetMediaTitleTagsByMediaTitleDBID(context.Background(), f.row(t, name).Title.DBID)
	require.NoError(t, err)
	var values []string
	for _, tag := range all {
		if tag.Type == string(tagType) {
			values = append(values, tags.UnpadTagValue(tag.Tag))
		}
	}
	return values
}

func ptr(value string) *string { return &value }

func TestApplyMediaUserFieldNameSetChangeAndReset(t *testing.T) {
	f := newReconcileFixture(t, "Alpha (USA).nes", "Alpha (Europe).nes", "Beta.nes")
	usa, europe := f.row(t, "Alpha (USA).nes"), f.row(t, "Alpha (Europe).nes")
	require.Equal(t, usa.Title.DBID, europe.Title.DBID, "the two versions share a title")
	originalTitle, originalSort := usa.Title.Name, usa.SortName

	result := f.apply(t, "Alpha (USA).nes", database.MediaUserFieldName, ptr("First Name"))
	assert.True(t, result.NameChanged)
	f.apply(t, "Alpha (Europe).nes", database.MediaUserFieldName, ptr("Second Name"))

	for _, name := range []string{"Alpha (USA).nes", "Alpha (Europe).nes"} {
		row := f.row(t, name)
		assert.Equal(t, "Second Name", row.Title.Name)
		assert.Equal(t, "Second Name", row.SortName)
		assert.Equal(t, usa.Title.Slug, row.Title.Slug, "the slug stays the filename's")
	}
	assert.Equal(t, "Beta", f.row(t, "Beta.nes").Title.Name)

	fields, err := f.db.UserDB.ListMediaUserFields()
	require.NoError(t, err)
	require.Len(t, fields, 1, "a title keeps one owner row per field")
	assert.Equal(t, europe.Path, fields[0].Path)

	f.apply(t, "Alpha (USA).nes", database.MediaUserFieldName, nil)
	row := f.row(t, "Alpha (USA).nes")
	assert.Equal(t, originalTitle, row.Title.Name)
	assert.Equal(t, originalSort, row.SortName)
	_, marked := f.titleProperty(t, "Alpha (USA).nes", tags.TagPropertyUserFields)
	assert.False(t, marked, "a title with no user field keeps no marker")
	fields, err = f.db.UserDB.ListMediaUserFields()
	require.NoError(t, err)
	assert.Empty(t, fields)
}

func TestReapplyMediaUserFieldsAfterScanRenamesTitle(t *testing.T) {
	f := newReconcileFixture(t, "Alpha.nes", "Beta.nes")
	f.apply(t, "Alpha.nes", database.MediaUserFieldName, ptr("Custom"))

	// A scan writes every title's name from its filename again.
	scantest.IndexMediaPaths(t, f.db.MediaDB, "NES", nesPath("Alpha.nes"), nesPath("Beta.nes"))

	scanned := f.row(t, "Alpha.nes")
	require.Equal(t, "Alpha", scanned.Title.Name, "the scan put the filename's name back")
	require.Equal(t, "Alpha", scanned.SortName)

	_, err := database.ReapplyMediaUserFields(context.Background(), f.db)
	require.NoError(t, err)
	row := f.row(t, "Alpha.nes")
	assert.Equal(t, "Custom", row.Title.Name)
	assert.Equal(t, "Custom", row.SortName)

	again, err := database.ReapplyMediaUserFields(context.Background(), f.db)
	require.NoError(t, err)
	assert.Zero(t, again, "a database already in line is not written")

	f.apply(t, "Alpha.nes", database.MediaUserFieldName, nil)
	assert.Equal(t, "Alpha", f.row(t, "Alpha.nes").Title.Name)
}

func TestApplyMediaUserFieldDescriptionSurvivesScrapeOverwrite(t *testing.T) {
	ctx := context.Background()
	f := newReconcileFixture(t, "Alpha.nes")
	description := tags.PropertyTypeTag(tags.TagPropertyDescription)
	titleDBID := f.row(t, "Alpha.nes").Title.DBID
	require.NoError(t, f.db.MediaDB.UpsertMediaTitleProperties(ctx, titleDBID, []database.MediaProperty{
		{TypeTag: description, Text: "scraped"},
	}))

	f.apply(t, "Alpha.nes", database.MediaUserFieldDescription, ptr("mine"))
	text, _ := f.titleProperty(t, "Alpha.nes", tags.TagPropertyDescription)
	assert.Equal(t, "mine", text)

	// A manual scrape upserts the property over the user's text.
	require.NoError(t, f.db.MediaDB.UpsertMediaTitleProperties(ctx, titleDBID, []database.MediaProperty{
		{TypeTag: description, Text: "scraped again"},
	}))
	require.NoError(t, database.ReconcileMediaUserData(ctx, f.db))
	text, _ = f.titleProperty(t, "Alpha.nes", tags.TagPropertyDescription)
	assert.Equal(t, "mine", text)

	result := f.apply(t, "Alpha.nes", database.MediaUserFieldDescription, nil)
	_, present := f.titleProperty(t, "Alpha.nes", tags.TagPropertyDescription)
	assert.False(t, present, "a reset removes the user's text")
	assert.Equal(t, []int64{f.ids["Alpha.nes"]}, result.ClearedMediaDBIDs, "the file is left for the scrapers")
}

func TestApplyMediaUserFieldTagsReplaceTheTypesValues(t *testing.T) {
	ctx := context.Background()
	f := newReconcileFixture(t, "Alpha.nes")
	titleDBID := f.row(t, "Alpha.nes").Title.DBID
	require.NoError(t, f.db.MediaDB.UpsertMediaTitleTags(ctx, titleDBID, []database.TagInfo{
		{Type: string(tags.TagTypeGenre), Tag: "action"},
		{Type: string(tags.TagTypeYear), Tag: "1990"},
	}))

	genre := database.MediaUserFieldTagPrefix + string(tags.TagTypeGenre)
	result := f.apply(t, "Alpha.nes", genre, ptr(database.EncodeMediaUserTags([]database.MediaUserTagValue{
		{Tag: "rpg"}, {Tag: "puzzle"},
	})))
	assert.True(t, result.TagsChanged)
	assert.ElementsMatch(t, []string{"rpg", "puzzle"}, f.titleTags(t, "Alpha.nes", tags.TagTypeGenre))
	assert.Equal(t, []string{"1990"}, f.titleTags(t, "Alpha.nes", tags.TagTypeYear), "other types are untouched")

	// A scrape adds a genre back; the reconcile restores the user's set.
	require.NoError(t, f.db.MediaDB.UpsertMediaTitleTags(ctx, titleDBID, []database.TagInfo{
		{Type: string(tags.TagTypeGenre), Tag: "action"},
	}))
	require.NoError(t, database.ReconcileMediaUserData(ctx, f.db))
	assert.ElementsMatch(t, []string{"rpg", "puzzle"}, f.titleTags(t, "Alpha.nes", tags.TagTypeGenre))

	f.apply(t, "Alpha.nes", genre, nil)
	assert.Empty(t, f.titleTags(t, "Alpha.nes", tags.TagTypeGenre))
}

func TestApplyMediaUserFieldImageIsPerFile(t *testing.T) {
	f := newReconcileFixture(t, "Alpha (USA).nes", "Alpha (Europe).nes")
	f.db.UserImages = fakeUserImages{"usa.png": "/data/media/images-user/usa.png"}
	boxart := database.MediaUserFieldImagePrefix + "boxart"

	result := f.apply(t, "Alpha (USA).nes", boxart, ptr("usa.png"))
	assert.True(t, result.ImagesChanged)
	path, _ := f.mediaProperty(t, "Alpha (USA).nes", tags.TagPropertyImageBoxart)
	assert.Equal(t, "/data/media/images-user/usa.png", path)
	_, present := f.mediaProperty(t, "Alpha (Europe).nes", tags.TagPropertyImageBoxart)
	assert.False(t, present, "the other version keeps its own art")

	// The stored file is gone, as after a restore onto another device.
	f.db.UserImages = fakeUserImages{}
	require.NoError(t, database.ReconcileMediaUserData(context.Background(), f.db))
	_, present = f.mediaProperty(t, "Alpha (USA).nes", tags.TagPropertyImageBoxart)
	assert.False(t, present)
	fields, err := f.db.UserDB.ListMediaUserFields()
	require.NoError(t, err)
	assert.Len(t, fields, 1, "UserDB still records the image")
}

func TestReconcileMediaUserDataRemovesFieldsUserDBNoLongerHolds(t *testing.T) {
	ctx := context.Background()
	f := newReconcileFixture(t, "Alpha.nes", "Beta.nes")
	f.apply(t, "Alpha.nes", database.MediaUserFieldName, ptr("Custom"))
	f.apply(t, "Alpha.nes", database.MediaUserFieldDescription, ptr("mine"))
	f.apply(t, "Beta.nes", database.MediaUserFieldDescription, ptr("kept"))

	// UserDB is replaced by one that only knows Beta's description.
	alpha := f.row(t, "Alpha.nes")
	for _, field := range []string{database.MediaUserFieldName, database.MediaUserFieldDescription} {
		require.NoError(t, f.db.UserDB.DeleteMediaUserField(
			alpha.System.SystemID, alpha.Title.Slug, []string{alpha.Path}, field,
		))
	}
	require.NoError(t, database.ReconcileMediaUserData(ctx, f.db))

	assert.Equal(t, "Alpha", f.row(t, "Alpha.nes").Title.Name)
	_, present := f.titleProperty(t, "Alpha.nes", tags.TagPropertyDescription)
	assert.False(t, present)
	text, _ := f.titleProperty(t, "Beta.nes", tags.TagPropertyDescription)
	assert.Equal(t, "kept", text)
}

func TestReapplyMediaUserFieldsFollowsSlugWhenFileIsGone(t *testing.T) {
	ctx := context.Background()
	f := newReconcileFixture(t, "Alpha (USA).nes", "Alpha (Europe).nes")
	usa := f.row(t, "Alpha (USA).nes")
	require.NoError(t, f.db.UserDB.SetMediaUserField(&database.MediaUserField{
		SystemID: usa.System.SystemID, Path: nesPath("Alpha (Japan).nes"),
		Field: database.MediaUserFieldDescription, Value: "from a file no longer here", Slug: usa.Title.Slug,
	}, nil))

	changed, err := database.ReapplyMediaUserFields(ctx, f.db)
	require.NoError(t, err)
	assert.Equal(t, 1, changed)
	text, _ := f.titleProperty(t, "Alpha (USA).nes", tags.TagPropertyDescription)
	assert.Equal(t, "from a file no longer here", text)
}

func TestReapplyMediaUserFieldsOntoRebuiltMediaDB(t *testing.T) {
	ctx := context.Background()
	f := newReconcileFixture(t, "Alpha.nes")
	f.db.UserImages = fakeUserImages{"cover.png": "/data/media/images-user/cover.png"}
	f.apply(t, "Alpha.nes", database.MediaUserFieldName, ptr("Custom"))
	f.apply(t, "Alpha.nes", database.MediaUserFieldDescription, ptr("mine"))
	f.apply(t, "Alpha.nes", database.MediaUserFieldTagPrefix+string(tags.TagTypeYear),
		ptr(database.EncodeMediaUserTags([]database.MediaUserTagValue{{Tag: "1987"}})))
	f.apply(t, "Alpha.nes", database.MediaUserFieldImagePrefix+"boxart", ptr("cover.png"))

	// The media database is lost and built again from nothing; UserDB is not.
	rebuilt := newReconcileFixture(t, "Alpha.nes")
	rebuilt.db.UserDB = f.db.UserDB
	rebuilt.db.UserImages = f.db.UserImages
	require.Equal(t, "Alpha", rebuilt.row(t, "Alpha.nes").Title.Name)

	changed, err := database.ReapplyMediaUserFields(ctx, rebuilt.db)
	require.NoError(t, err)
	assert.Equal(t, 1, changed)
	row := rebuilt.row(t, "Alpha.nes")
	assert.Equal(t, "Custom", row.Title.Name)
	assert.Equal(t, "Custom", row.SortName)
	text, _ := rebuilt.titleProperty(t, "Alpha.nes", tags.TagPropertyDescription)
	assert.Equal(t, "mine", text)
	assert.Equal(t, []string{"1987"}, rebuilt.titleTags(t, "Alpha.nes", tags.TagTypeYear))
	path, _ := rebuilt.mediaProperty(t, "Alpha.nes", tags.TagPropertyImageBoxart)
	assert.Equal(t, "/data/media/images-user/cover.png", path)

	// The name can still be reset to the scanner's on the rebuilt database.
	rebuilt.apply(t, "Alpha.nes", database.MediaUserFieldName, nil)
	assert.Equal(t, "Alpha", rebuilt.row(t, "Alpha.nes").Title.Name)
}

func TestReapplyMediaUserFieldsWithoutImageStoreLeavesImagesAlone(t *testing.T) {
	ctx := context.Background()
	f := newReconcileFixture(t, "Alpha.nes")
	f.db.UserImages = fakeUserImages{"cover.png": "/data/media/images-user/cover.png"}
	f.apply(t, "Alpha.nes", database.MediaUserFieldImagePrefix+"boxart", ptr("cover.png"))

	f.db.UserImages = nil
	_, err := database.ReapplyMediaUserFields(ctx, f.db)
	require.NoError(t, err)
	path, present := f.mediaProperty(t, "Alpha.nes", tags.TagPropertyImageBoxart)
	assert.True(t, present)
	assert.Equal(t, "/data/media/images-user/cover.png", path)
}

// A row found by its old slug whose file is now indexed under another title
// belongs to that title, on a live edit as on a reconcile.
func TestApplyMediaUserFieldLeavesRowsOfAnotherTitlesFile(t *testing.T) {
	f := newReconcileFixture(t, "Alpha (USA).nes", "Beta.nes")
	alpha := f.row(t, "Alpha (USA).nes")
	require.NoError(t, f.db.UserDB.SetMediaUserField(&database.MediaUserField{
		SystemID: alpha.System.SystemID, Path: nesPath("Beta.nes"),
		Field: database.MediaUserFieldDescription, Value: "Beta's own description", Slug: alpha.Title.Slug,
	}, nil))

	f.apply(t, "Alpha (USA).nes", database.MediaUserFieldName, ptr("Renamed Alpha"))

	_, held := f.titleProperty(t, "Alpha (USA).nes", tags.TagPropertyDescription)
	assert.False(t, held, "Beta's row must not reach Alpha's title")
	changed, err := database.ReapplyMediaUserFields(context.Background(), f.db)
	require.NoError(t, err)
	assert.Equal(t, 1, changed, "only Beta's title was out of line")
	text, _ := f.titleProperty(t, "Beta.nes", tags.TagPropertyDescription)
	assert.Equal(t, "Beta's own description", text)
}

// failingTitleMediaDB fails to list the files of one title.
type failingTitleMediaDB struct {
	*mediadb.MediaDB
	titleDBID int64
}

func (f failingTitleMediaDB) ListMediaByTitle(ctx context.Context, titleDBID int64) ([]database.Media, error) {
	if titleDBID == f.titleDBID {
		return nil, errors.New("listing failed")
	}
	media, err := f.MediaDB.ListMediaByTitle(ctx, titleDBID)
	if err != nil {
		return nil, fmt.Errorf("list media by title: %w", err)
	}
	return media, nil
}

func TestReapplyMediaUserFieldsContinuesPastATitleThatFails(t *testing.T) {
	ctx := context.Background()
	f := newReconcileFixture(t, "Alpha (USA).nes", "Beta.nes")
	alpha, beta := f.row(t, "Alpha (USA).nes"), f.row(t, "Beta.nes")
	for _, row := range []*database.MediaFullRow{alpha, beta} {
		require.NoError(t, f.db.UserDB.SetMediaUserField(&database.MediaUserField{
			SystemID: row.System.SystemID, Path: row.Path,
			Field: database.MediaUserFieldDescription, Value: "edited " + row.Title.Name, Slug: row.Title.Slug,
		}, nil))
	}
	realDB, ok := f.db.MediaDB.(*mediadb.MediaDB)
	require.True(t, ok)
	// Titles are re-applied in ID order, so fail the first.
	f.db.MediaDB = failingTitleMediaDB{MediaDB: realDB, titleDBID: min(alpha.Title.DBID, beta.Title.DBID)}

	changed, err := database.ReapplyMediaUserFields(ctx, f.db)
	require.Error(t, err)
	assert.Equal(t, 1, changed)
	later := alpha
	if beta.Title.DBID > alpha.Title.DBID {
		later = beta
	}
	f.db.MediaDB = realDB
	text, _ := f.titleProperty(t, filepath.Base(later.Path), tags.TagPropertyDescription)
	assert.Equal(t, "edited "+later.Title.Name, text)
}

// Stored text that does not decode is treated as holding nothing, never as an
// error that would stop a reconcile.
func TestMediaUserFieldsDecodeMalformedText(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "[]", database.EncodeMediaUserTags(nil))
	assert.Nil(t, database.DecodeMediaUserTags("not json"))
	assert.Equal(t, database.MediaUserFieldsMarker{}, database.DecodeMediaUserFieldsMarker("not json"))
	assert.Equal(t, database.MediaUserFieldsMarker{}, database.DecodeMediaUserFieldsMarker(""))
}
