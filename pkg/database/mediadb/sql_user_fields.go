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
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
)

// userFieldsFile is one file of the title a sync works on.
type userFieldsFile struct {
	sortName string
	marker   database.MediaUserFieldsMarker
	dbid     int64
	hadRow   bool
}

// userFieldsSync carries one SyncMediaUserFields transaction.
type userFieldsSync struct {
	writeCtx    *scrapeWriteTxContext
	want        *database.MediaUserTitleFields
	tx          *sql.Tx
	titleName   string
	titleMarker database.MediaUserFieldsMarker
	files       []userFieldsFile
	result      database.MediaUserFieldsSync
	markerTag   int64
	titleHadRow bool
}

// SyncMediaUserFields makes one title and its files carry exactly the user
// fields given: values the title lacks are written, and values its marker
// lists that are no longer wanted are removed. A name is the exception to
// removal: the name the scanner gave is put back. Everything happens in one
// transaction, and a title already in line is not written.
func (db *MediaDB) SyncMediaUserFields(
	ctx context.Context, want *database.MediaUserTitleFields,
) (database.MediaUserFieldsSync, error) {
	result, err := db.syncMediaUserFieldsTx(ctx, want)
	if err != nil {
		return database.MediaUserFieldsSync{}, err
	}
	if !result.Changed {
		return result, nil
	}
	db.browseListings.Clear()
	if result.TagsChanged {
		db.addScrapeTagSystems([]string{result.SystemID})
	}
	if result.ImagesChanged {
		clearCoverAvailabilityCacheFor(db.sql.Load())
		db.scrapeImageChangesMu.Lock()
		if db.scrapeImageSystems == nil {
			db.scrapeImageSystems = make(map[string]struct{})
		}
		db.scrapeImageSystems[result.SystemID] = struct{}{}
		db.scrapeImageChangesMu.Unlock()
	}
	return result, nil
}

func (db *MediaDB) syncMediaUserFieldsTx(
	ctx context.Context, want *database.MediaUserTitleFields,
) (database.MediaUserFieldsSync, error) {
	db.sqlMu.Lock()
	defer db.sqlMu.Unlock()

	sqlDB := db.sql.Load()
	if sqlDB == nil {
		return database.MediaUserFieldsSync{}, ErrNullSQL
	}
	if db.inTransaction {
		return database.MediaUserFieldsSync{}, ErrTransactionActive
	}
	tx, err := sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return database.MediaUserFieldsSync{}, fmt.Errorf("begin media user fields transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	s := &userFieldsSync{tx: tx, writeCtx: newScrapeWriteTxContext(tx), want: want}
	found, err := s.load(ctx)
	if err != nil || !found {
		return database.MediaUserFieldsSync{}, err
	}
	for _, step := range []func(context.Context) error{
		s.syncName, s.syncDescription, s.syncTags, s.syncImages, s.writeMarkers,
	} {
		if err := step(ctx); err != nil {
			return database.MediaUserFieldsSync{}, err
		}
	}
	if !s.result.Changed {
		return s.result, nil
	}
	if err := tx.Commit(); err != nil {
		return database.MediaUserFieldsSync{}, fmt.Errorf("commit media user fields transaction: %w", err)
	}
	committed = true
	return s.result, nil
}

// load reads the title, its files and their markers. found is false when the
// title does not exist.
func (s *userFieldsSync) load(ctx context.Context) (found bool, err error) {
	err = s.tx.QueryRowContext(ctx, `
		SELECT mt.Name, s.SystemID FROM MediaTitles mt
		JOIN Systems s ON s.DBID = mt.SystemDBID WHERE mt.DBID = ?`,
		s.want.TitleDBID,
	).Scan(&s.titleName, &s.result.SystemID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read title for media user fields: %w", err)
	}

	typeDBID, _, err := s.writeCtx.resolveTagType(ctx, string(tags.TagTypeProperty))
	if err != nil {
		return false, err
	}
	s.markerTag, err = s.writeCtx.resolveTag(
		ctx, typeDBID, string(tags.TagTypeProperty), tags.PadTagValue(string(tags.TagPropertyUserFields)), "",
	)
	if err != nil {
		return false, err
	}

	var titleText string
	err = s.tx.QueryRowContext(ctx,
		`SELECT Text FROM MediaTitleProperties WHERE MediaTitleDBID = ? AND TypeTagDBID = ?`,
		s.want.TitleDBID, s.markerTag,
	).Scan(&titleText)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return false, fmt.Errorf("read title user fields marker: %w", err)
	default:
		s.titleHadRow = true
		s.titleMarker = database.DecodeMediaUserFieldsMarker(titleText)
	}

	rows, err := s.tx.QueryContext(ctx, `
		SELECT m.DBID, m.SortName, mp.Text FROM Media m
		LEFT JOIN MediaProperties mp ON mp.MediaDBID = m.DBID AND mp.TypeTagDBID = ?
		WHERE m.MediaTitleDBID = ? ORDER BY m.DBID`,
		s.markerTag, s.want.TitleDBID,
	)
	if err != nil {
		return false, fmt.Errorf("read files for media user fields: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var file userFieldsFile
		var text sql.NullString
		if err := rows.Scan(&file.dbid, &file.sortName, &text); err != nil {
			return false, fmt.Errorf("scan file for media user fields: %w", err)
		}
		if text.Valid {
			file.hadRow = true
			file.marker = database.DecodeMediaUserFieldsMarker(text.String)
		}
		s.files = append(s.files, file)
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("iterate files for media user fields: %w", err)
	}
	return true, nil
}

// nextName decides what one name (a title's Name or a file's SortName) should
// become, and updates the marker that remembers the scanner's own name for
// it. write is false when the stored name is already right.
func nextName(want *string, current string, marker *database.MediaUserFieldsMarker) (name string, write bool) {
	if want == nil {
		applied, original := marker.AppliedName, marker.OriginalName
		marker.AppliedName, marker.OriginalName = "", ""
		// A name the scanner rewrote since is already the scanner's.
		if applied == "" || current != applied || original == "" || original == current {
			return "", false
		}
		return original, true
	}
	// The stored name is the scanner's unless it is the name last applied
	// here, so it is what to go back to when the user's name is removed.
	if marker.AppliedName == "" || current != marker.AppliedName {
		marker.OriginalName = current
	}
	marker.AppliedName = *want
	return *want, current != *want
}

func (s *userFieldsSync) syncName(ctx context.Context) error {
	s.titleMarker.Fields = setMarkerField(s.titleMarker.Fields, database.MediaUserFieldName, s.want.Name != nil)
	if name, write := nextName(s.want.Name, s.titleName, &s.titleMarker); write {
		if _, err := s.tx.ExecContext(ctx,
			`UPDATE MediaTitles SET Name = ? WHERE DBID = ?`, name, s.want.TitleDBID,
		); err != nil {
			return fmt.Errorf("write title name: %w", err)
		}
		s.result.Changed, s.result.NameChanged = true, true
	}
	for i := range s.files {
		file := &s.files[i]
		if name, write := nextName(s.want.Name, file.sortName, &file.marker); write {
			if _, err := s.tx.ExecContext(ctx,
				`UPDATE Media SET SortName = ? WHERE DBID = ?`, name, file.dbid,
			); err != nil {
				return fmt.Errorf("write file sort name: %w", err)
			}
			s.result.Changed, s.result.NameChanged = true, true
		}
	}
	return nil
}

func (s *userFieldsSync) syncDescription(ctx context.Context) error {
	typeTag := tags.PropertyTypeTag(tags.TagPropertyDescription)
	owned := slices.Contains(s.titleMarker.Fields, database.MediaUserFieldDescription)
	s.titleMarker.Fields = setMarkerField(
		s.titleMarker.Fields, database.MediaUserFieldDescription, s.want.Description != nil,
	)
	if s.want.Description == nil {
		if !owned {
			return nil
		}
		typeTagDBID, err := s.writeCtx.resolvePropertyTypeTag(ctx, typeTag)
		if err != nil {
			return err
		}
		if _, err := s.tx.ExecContext(ctx,
			`DELETE FROM MediaTitleProperties WHERE MediaTitleDBID = ? AND TypeTagDBID = ?`,
			s.want.TitleDBID, typeTagDBID,
		); err != nil {
			return fmt.Errorf("clear user description: %w", err)
		}
		s.result.Changed = true
		s.clearedTitleField()
		return nil
	}
	typeTagDBID, err := s.writeCtx.resolvePropertyTypeTag(ctx, typeTag)
	if err != nil {
		return err
	}
	changed, err := upsertMediaTitleProperty(ctx, s.tx, s.want.TitleDBID, typeTagDBID, &database.MediaProperty{
		TypeTag: typeTag, Text: *s.want.Description,
	})
	if err != nil {
		return err
	}
	s.result.Changed = s.result.Changed || changed
	return nil
}

func (s *userFieldsSync) syncTags(ctx context.Context) error {
	tagTypes := make(map[string]struct{}, len(s.want.Tags))
	for tagType := range s.want.Tags {
		tagTypes[tagType] = struct{}{}
	}
	for _, field := range s.titleMarker.Fields {
		if tagType, ok := strings.CutPrefix(field, database.MediaUserFieldTagPrefix); ok {
			tagTypes[tagType] = struct{}{}
		}
	}
	ordered := make([]string, 0, len(tagTypes))
	for tagType := range tagTypes {
		ordered = append(ordered, tagType)
	}
	sort.Strings(ordered)

	for _, tagType := range ordered {
		values, owned := s.want.Tags[tagType]
		field := database.MediaUserFieldTagPrefix + tagType
		s.titleMarker.Fields = setMarkerField(s.titleMarker.Fields, field, owned)
		typeDBID, exclusive, err := s.writeCtx.resolveTagType(ctx, tagType)
		if err != nil {
			return err
		}
		if exclusive && len(values) > 1 {
			values = values[:1]
		}
		wantIDs := make([]int64, 0, len(values))
		for _, value := range values {
			tagDBID, tagErr := s.writeCtx.resolveTag(ctx, typeDBID, tagType, tags.PadTagValue(value.Tag), value.Label)
			if tagErr != nil {
				return tagErr
			}
			if !slices.Contains(wantIDs, tagDBID) {
				wantIDs = append(wantIDs, tagDBID)
			}
		}
		haveIDs, err := s.titleTagIDs(ctx, typeDBID)
		if err != nil {
			return err
		}
		slices.Sort(wantIDs)
		if slices.Equal(haveIDs, wantIDs) {
			continue
		}
		if _, err := s.tx.ExecContext(ctx, `
			DELETE FROM MediaTitleTags
			WHERE MediaTitleDBID = ? AND TagDBID IN (SELECT DBID FROM Tags WHERE TypeDBID = ?)`,
			s.want.TitleDBID, typeDBID,
		); err != nil {
			return fmt.Errorf("replace user %s tags: %w", tagType, err)
		}
		for _, tagDBID := range wantIDs {
			if _, err := s.tx.ExecContext(ctx,
				`INSERT OR IGNORE INTO MediaTitleTags (MediaTitleDBID, TagDBID) VALUES (?, ?)`,
				s.want.TitleDBID, tagDBID,
			); err != nil {
				return fmt.Errorf("write user %s tag: %w", tagType, err)
			}
		}
		s.result.Changed, s.result.TagsChanged = true, true
		if !owned {
			s.clearedTitleField()
		}
	}
	return nil
}

// titleTagIDs returns the title's tags of one type, sorted.
func (s *userFieldsSync) titleTagIDs(ctx context.Context, typeDBID int64) ([]int64, error) {
	rows, err := s.tx.QueryContext(ctx, `
		SELECT mtt.TagDBID FROM MediaTitleTags mtt
		JOIN Tags t ON t.DBID = mtt.TagDBID
		WHERE mtt.MediaTitleDBID = ? AND t.TypeDBID = ? ORDER BY mtt.TagDBID`,
		s.want.TitleDBID, typeDBID,
	)
	if err != nil {
		return nil, fmt.Errorf("read title tags of a type: %w", err)
	}
	defer func() { _ = rows.Close() }()
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan title tag: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate title tags of a type: %w", err)
	}
	return ids, nil
}

func (s *userFieldsSync) syncImages(ctx context.Context) error {
	if s.want.SkipImages {
		return nil
	}
	for i := range s.files {
		file := &s.files[i]
		want := s.want.Images[file.dbid]
		kinds := make(map[string]struct{}, len(want))
		for kind := range want {
			kinds[kind] = struct{}{}
		}
		for _, field := range file.marker.Fields {
			if kind, ok := strings.CutPrefix(field, database.MediaUserFieldImagePrefix); ok {
				kinds[kind] = struct{}{}
			}
		}
		ordered := make([]string, 0, len(kinds))
		for kind := range kinds {
			ordered = append(ordered, kind)
		}
		sort.Strings(ordered)

		for _, kind := range ordered {
			path, owned := want[kind]
			typeTag := tags.PropertyTypeTag(tags.TagValue("image-" + kind))
			typeTagDBID, err := s.writeCtx.resolvePropertyTypeTag(ctx, typeTag)
			if err != nil {
				return err
			}
			file.marker.Fields = setMarkerField(file.marker.Fields, database.MediaUserFieldImagePrefix+kind, owned)
			if owned {
				changed, upsertErr := upsertMediaProperty(ctx, s.tx, file.dbid, typeTagDBID, &database.MediaProperty{
					TypeTag: typeTag, Text: path,
				})
				if upsertErr != nil {
					return upsertErr
				}
				if changed {
					s.result.Changed, s.result.ImagesChanged = true, true
				}
				continue
			}
			if _, err := s.tx.ExecContext(ctx,
				`DELETE FROM MediaProperties WHERE MediaDBID = ? AND TypeTagDBID = ?`, file.dbid, typeTagDBID,
			); err != nil {
				return fmt.Errorf("clear user %s image: %w", kind, err)
			}
			s.result.Changed, s.result.ImagesChanged = true, true
			if !slices.Contains(s.result.ClearedMediaDBIDs, file.dbid) {
				s.result.ClearedMediaDBIDs = append(s.result.ClearedMediaDBIDs, file.dbid)
			}
		}
	}
	return nil
}

// clearedTitleField records that a title-level field was removed, which
// leaves every file of the title for the scrapers to fill again.
func (s *userFieldsSync) clearedTitleField() {
	for i := range s.files {
		if !slices.Contains(s.result.ClearedMediaDBIDs, s.files[i].dbid) {
			s.result.ClearedMediaDBIDs = append(s.result.ClearedMediaDBIDs, s.files[i].dbid)
		}
	}
}

func (s *userFieldsSync) writeMarkers(ctx context.Context) error {
	changed, err := writeUserFieldsMarker(ctx, s.tx, "MediaTitleProperties", "MediaTitleDBID",
		s.want.TitleDBID, s.markerTag, &s.titleMarker, s.titleHadRow)
	if err != nil {
		return err
	}
	s.result.Changed = s.result.Changed || changed
	for i := range s.files {
		file := &s.files[i]
		changed, err = writeUserFieldsMarker(ctx, s.tx, "MediaProperties", "MediaDBID",
			file.dbid, s.markerTag, &file.marker, file.hadRow)
		if err != nil {
			return err
		}
		s.result.Changed = s.result.Changed || changed
	}
	return nil
}

// writeUserFieldsMarker stores a marker, or removes the row of one that now
// records nothing. table and column are constants of this file.
func writeUserFieldsMarker(
	ctx context.Context, tx *sql.Tx, table, column string, ownerDBID, markerTag int64,
	marker *database.MediaUserFieldsMarker, hadRow bool,
) (bool, error) {
	if marker.IsEmpty() {
		if !hadRow {
			return false, nil
		}
		//nolint:gosec // table and column are constants of this file
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM `+table+` WHERE `+column+` = ? AND TypeTagDBID = ?`, ownerDBID, markerTag,
		); err != nil {
			return false, fmt.Errorf("remove user fields marker: %w", err)
		}
		return true, nil
	}
	//nolint:gosec // table and column are constants of this file
	result, err := tx.ExecContext(ctx, `
		INSERT INTO `+table+` (`+column+`, TypeTagDBID, Text, BlobDBID) VALUES (?, ?, ?, NULL)
		ON CONFLICT(`+column+`, TypeTagDBID) DO UPDATE SET Text = excluded.Text
		WHERE `+table+`.Text IS NOT excluded.Text`,
		ownerDBID, markerTag, marker.Encode(),
	)
	if err != nil {
		return false, fmt.Errorf("write user fields marker: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read user fields marker write result: %w", err)
	}
	return rows > 0, nil
}

// setMarkerField adds field to fields or removes it.
func setMarkerField(fields []string, field string, present bool) []string {
	index := slices.Index(fields, field)
	switch {
	case present && index < 0:
		return append(fields, field)
	case !present && index >= 0:
		return slices.Delete(fields, index, index+1)
	default:
		return fields
	}
}

// ListMediaUserFieldTitles returns the titles that carry a user-fields
// marker, on the title or on any of its files.
func (db *MediaDB) ListMediaUserFieldTitles(ctx context.Context) ([]int64, error) {
	sqlDB := db.sql.Load()
	if sqlDB == nil {
		return nil, ErrNullSQL
	}
	typeTag := tags.PropertyTypeTag(tags.TagPropertyUserFields)
	markerTag, err := resolvePropertyTypeTag(ctx, sqlDB, typeTag)
	if err != nil {
		// A database that never held a user field has no marker tag either.
		return nil, nil //nolint:nilerr // an unseeded marker tag means no markers
	}
	rows, err := sqlDB.QueryContext(ctx, `
		SELECT MediaTitleDBID FROM MediaTitleProperties WHERE TypeTagDBID = ?
		UNION
		SELECT m.MediaTitleDBID FROM MediaProperties mp
		JOIN Media m ON m.DBID = mp.MediaDBID WHERE mp.TypeTagDBID = ?
		ORDER BY 1`, markerTag, markerTag)
	if err != nil {
		return nil, fmt.Errorf("list titles with user fields: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan title with user fields: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate titles with user fields: %w", err)
	}
	return ids, nil
}

// ListMediaByTitle returns the files of one title.
func (db *MediaDB) ListMediaByTitle(ctx context.Context, mediaTitleDBID int64) ([]database.Media, error) {
	sqlDB := db.sql.Load()
	if sqlDB == nil {
		return nil, ErrNullSQL
	}
	rows, err := sqlDB.QueryContext(ctx, `
		SELECT DBID, MediaTitleDBID, SystemDBID, Path, ParentDir, SortName, IsMissing
		FROM Media WHERE MediaTitleDBID = ? ORDER BY DBID`, mediaTitleDBID)
	if err != nil {
		return nil, fmt.Errorf("list files of a title: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var files []database.Media
	for rows.Next() {
		var file database.Media
		if err := rows.Scan(
			&file.DBID, &file.MediaTitleDBID, &file.SystemDBID, &file.Path, &file.ParentDir, &file.SortName,
			&file.IsMissing,
		); err != nil {
			return nil, fmt.Errorf("scan file of a title: %w", err)
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate files of a title: %w", err)
	}
	return files, nil
}

var _ database.MediaUserFieldsDB = (*MediaDB)(nil)
