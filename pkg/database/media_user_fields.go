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

package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/rs/zerolog/log"
)

// Field keys of user-edited metadata, as stored in UserDB's MediaUserFields
// and listed in the media database's user-fields marker. A tag field is
// MediaUserFieldTagPrefix plus the tag type, an image field is
// MediaUserFieldImagePrefix plus the image kind ("boxart", "screenshot").
const (
	MediaUserFieldName        = "name"
	MediaUserFieldDescription = "description"
	MediaUserFieldTagPrefix   = "tag:"
	MediaUserFieldImagePrefix = "image:"
)

// MediaUserField is one metadata field the user edited by hand. UserDB owns
// it; the media database carries a copy. Path is the file the edit was made
// on and Slug snapshots that file's title, so a title-level field still finds
// its title when the file is gone.
type MediaUserField struct {
	SystemID  string
	Path      string
	Field     string
	Value     string
	Slug      string
	CreatedAt int64
	UpdatedAt int64
}

// IsMediaUserFieldTitleLevel reports whether a field belongs to a title, and
// so to every file of it, rather than to one file. Only images are per file.
func IsMediaUserFieldTitleLevel(field string) bool {
	return !strings.HasPrefix(field, MediaUserFieldImagePrefix)
}

// MediaUserTagValue is one tag of a user-edited tag field.
type MediaUserTagValue struct {
	Tag   string `json:"tag"`
	Label string `json:"label,omitempty"`
}

// EncodeMediaUserTags encodes a tag field's value.
func EncodeMediaUserTags(values []MediaUserTagValue) string {
	if values == nil {
		values = []MediaUserTagValue{}
	}
	data, err := json.Marshal(values)
	if err != nil {
		return "[]"
	}
	return string(data)
}

// DecodeMediaUserTags decodes a tag field's value. A value that does not
// decode is an empty set.
func DecodeMediaUserTags(value string) []MediaUserTagValue {
	var values []MediaUserTagValue
	if err := json.Unmarshal([]byte(value), &values); err != nil {
		return nil
	}
	return values
}

// MediaUserTitleFields is everything the user owns on one title and its
// files: the state SyncMediaUserFields makes the media database match. A nil
// Name or Description, a tag type absent from Tags and an image kind absent
// from a file's Images are not owned, and a copy the marker still lists for
// them is removed.
type MediaUserTitleFields struct {
	Name        *string
	Description *string
	// Tags maps a tag type to the full set of values the title carries for it.
	Tags map[string][]MediaUserTagValue
	// Images maps a file's Media DBID to its image kinds and the path of the
	// stored image for each.
	Images    map[int64]map[string]string
	TitleDBID int64
	// SkipImages leaves every file's images as they are, for a caller that
	// cannot tell which stored images exist.
	SkipImages bool
}

// MediaUserFieldsSync reports what one SyncMediaUserFields call changed.
type MediaUserFieldsSync struct {
	SystemID string
	// ClearedMediaDBIDs are files the title's cleared fields leave without a
	// value the scrapers could fill again: every file of the title for a
	// title-level field, the one file for an image.
	ClearedMediaDBIDs []int64
	Changed           bool
	NameChanged       bool
	TagsChanged       bool
	ImagesChanged     bool
}

// MediaUserFieldsMarker is the user-fields marker of one title or file: the
// fields it holds a user value for, and what its name was before the user's.
type MediaUserFieldsMarker struct {
	AppliedName  string   `json:"appliedName,omitempty"`
	OriginalName string   `json:"originalName,omitempty"`
	Fields       []string `json:"fields,omitempty"`
}

// DecodeMediaUserFieldsMarker decodes a marker property's text. Text that
// does not decode is an empty marker.
func DecodeMediaUserFieldsMarker(text string) MediaUserFieldsMarker {
	var marker MediaUserFieldsMarker
	if text == "" {
		return marker
	}
	if err := json.Unmarshal([]byte(text), &marker); err != nil {
		return MediaUserFieldsMarker{}
	}
	return marker
}

// Encode returns the marker's property text, with its fields sorted.
func (m *MediaUserFieldsMarker) Encode() string {
	sort.Strings(m.Fields)
	data, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return string(data)
}

// IsEmpty reports whether the marker records nothing and should not be kept.
func (m *MediaUserFieldsMarker) IsEmpty() bool {
	return len(m.Fields) == 0 && m.AppliedName == "" && m.OriginalName == ""
}

// MediaUserFieldsDB is the media database's side of user-edited metadata,
// layered over MediaDBI without expanding that interface.
type MediaUserFieldsDB interface {
	// SyncMediaUserFields makes one title and its files carry exactly the
	// user fields given, in one transaction, and writes nothing when they
	// already do.
	SyncMediaUserFields(ctx context.Context, want *MediaUserTitleFields) (MediaUserFieldsSync, error)
	// ListMediaUserFieldTitles returns the titles that carry a user-fields
	// marker, on the title or on any of its files.
	ListMediaUserFieldTitles(ctx context.Context) ([]int64, error)
	// ListMediaByTitle returns the files of one title.
	ListMediaByTitle(ctx context.Context, mediaTitleDBID int64) ([]Media, error)
}

// MediaUserImageStore resolves the name of a stored user image to the path of
// its file, with an error when the file is not there.
type MediaUserImageStore interface {
	Path(name string) (string, error)
}

// mediaUserFieldsDB returns the media database's user-fields side, false when
// the attached media database has none (a mock, or none at all).
func mediaUserFieldsDB(db *Database) (MediaUserFieldsDB, bool) {
	if db == nil || db.MediaDB == nil {
		return nil, false
	}
	fieldsDB, ok := db.MediaDB.(MediaUserFieldsDB)
	return fieldsDB, ok
}

// ApplyMediaUserField records one user-edited field of a file in UserDB, the
// source of truth, then brings the file's title in the media database in line.
// A nil value removes the field. The UserDB write comes first: when the media
// database write fails, the next reconcile or reindex repairs the copy.
func ApplyMediaUserField(
	ctx context.Context, db *Database, row *MediaFullRow, field string, value *string,
) (MediaUserFieldsSync, error) {
	mediaUserFlagsMu.Lock()
	defer mediaUserFlagsMu.Unlock()

	// See ApplyMediaUserFlags: refuse before UserDB is written, not after.
	if err := EnsureMediaWritable(db.MediaDB); err != nil {
		return MediaUserFieldsSync{}, err
	}
	fieldsDB, hasFieldsDB := mediaUserFieldsDB(db)
	files := []Media{row.Media}
	if hasFieldsDB {
		listed, err := fieldsDB.ListMediaByTitle(ctx, row.Title.DBID)
		if err != nil {
			return MediaUserFieldsSync{}, fmt.Errorf("failed to list the title's files: %w", err)
		}
		files = listed
	}
	titlePaths := make([]string, 0, len(files))
	for i := range files {
		titlePaths = append(titlePaths, files[i].Path)
	}

	systemID := row.System.SystemID
	if value == nil {
		paths := []string{row.Path}
		if IsMediaUserFieldTitleLevel(field) {
			paths = titlePaths
		}
		if err := db.UserDB.DeleteMediaUserField(systemID, row.Title.Slug, paths, field); err != nil {
			return MediaUserFieldsSync{}, fmt.Errorf("failed to remove media user field %s: %w", field, err)
		}
	} else if err := db.UserDB.SetMediaUserField(&MediaUserField{
		SystemID: systemID, Path: row.Path, Field: field, Value: *value, Slug: row.Title.Slug,
	}, titlePaths); err != nil {
		return MediaUserFieldsSync{}, fmt.Errorf("failed to set media user field %s: %w", field, err)
	}
	if !hasFieldsDB {
		return MediaUserFieldsSync{}, nil
	}

	rows, err := db.UserDB.GetMediaUserFieldsForTitle(systemID, row.Title.Slug, titlePaths)
	if err != nil {
		return MediaUserFieldsSync{}, fmt.Errorf("failed to read the title's media user fields: %w", err)
	}
	result, err := fieldsDB.SyncMediaUserFields(ctx, foldMediaUserFields(row.Title.DBID, rows, files, db.UserImages))
	if err != nil {
		return MediaUserFieldsSync{}, fmt.Errorf("failed to write media user fields: %w", err)
	}
	return result, nil
}

// foldMediaUserFields builds what a title should carry from its UserDB rows,
// given oldest first so the newest row of a field decides it. An image whose
// file is not stored is left out, and the file then shows its scraped art.
func foldMediaUserFields(
	titleDBID int64, rows []MediaUserField, files []Media, images MediaUserImageStore,
) *MediaUserTitleFields {
	state := &MediaUserTitleFields{
		TitleDBID:  titleDBID,
		Tags:       make(map[string][]MediaUserTagValue),
		Images:     make(map[int64]map[string]string),
		SkipImages: images == nil,
	}
	mediaByPath := make(map[string]int64, len(files))
	for i := range files {
		mediaByPath[files[i].Path] = files[i].DBID
	}
	for i := range rows {
		row := &rows[i]
		switch {
		case row.Field == MediaUserFieldName:
			state.Name = &row.Value
		case row.Field == MediaUserFieldDescription:
			state.Description = &row.Value
		case strings.HasPrefix(row.Field, MediaUserFieldTagPrefix):
			state.Tags[strings.TrimPrefix(row.Field, MediaUserFieldTagPrefix)] = DecodeMediaUserTags(row.Value)
		case strings.HasPrefix(row.Field, MediaUserFieldImagePrefix):
			mediaDBID, indexed := mediaByPath[row.Path]
			if !indexed || images == nil {
				continue
			}
			path, err := images.Path(row.Value)
			if err != nil {
				log.Debug().Err(err).Str("path", row.Path).Str("field", row.Field).
					Msg("user image is not stored; leaving the scraped art")
				continue
			}
			if state.Images[mediaDBID] == nil {
				state.Images[mediaDBID] = make(map[string]string)
			}
			state.Images[mediaDBID][strings.TrimPrefix(row.Field, MediaUserFieldImagePrefix)] = path
		}
	}
	return state
}

// ReapplyMediaUserFields makes the media database carry exactly the user
// fields UserDB holds: after a reindex, which rebuilds titles and rewrites
// their names, after a scrape, which overwrites values, and after UserDB was
// replaced as a whole. A media database already in line is not written.
func ReapplyMediaUserFields(ctx context.Context, db *Database) (changed int, err error) {
	mediaUserFlagsMu.Lock()
	defer mediaUserFlagsMu.Unlock()
	return reconcileMediaUserFields(ctx, db)
}

// reconcileMediaUserFields is ReapplyMediaUserFields for a caller that holds
// mediaUserFlagsMu.
func reconcileMediaUserFields(ctx context.Context, db *Database) (changed int, err error) {
	fieldsDB, ok := mediaUserFieldsDB(db)
	if !ok || db.UserDB == nil {
		return 0, nil
	}
	rows, err := db.UserDB.ListMediaUserFields()
	if err != nil {
		return 0, fmt.Errorf("failed to list media user fields: %w", err)
	}
	marked, err := fieldsDB.ListMediaUserFieldTitles(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to list titles with media user fields: %w", err)
	}
	if len(rows) == 0 && len(marked) == 0 {
		return 0, nil
	}
	byTitle, err := groupMediaUserFieldsByTitle(ctx, db.MediaDB, rows)
	if err != nil {
		return 0, err
	}
	titleIDs := make([]int64, 0, len(byTitle)+len(marked))
	for titleDBID := range byTitle {
		titleIDs = append(titleIDs, titleDBID)
	}
	// A title that carries a marker and no rows holds copies UserDB no
	// longer has.
	for _, titleDBID := range marked {
		if _, held := byTitle[titleDBID]; !held {
			titleIDs = append(titleIDs, titleDBID)
		}
	}
	sort.Slice(titleIDs, func(i, j int) bool { return titleIDs[i] < titleIDs[j] })

	for _, titleDBID := range titleIDs {
		files, listErr := fieldsDB.ListMediaByTitle(ctx, titleDBID)
		if listErr != nil {
			return changed, fmt.Errorf("failed to list the files of title %d: %w", titleDBID, listErr)
		}
		result, syncErr := fieldsDB.SyncMediaUserFields(
			ctx, foldMediaUserFields(titleDBID, byTitle[titleDBID], files, db.UserImages),
		)
		if syncErr != nil {
			return changed, fmt.Errorf("failed to write the user fields of title %d: %w", titleDBID, syncErr)
		}
		if result.Changed {
			changed++
		}
	}
	return changed, nil
}

// groupMediaUserFieldsByTitle resolves each row to the title it belongs to,
// keeping the rows' order. A row whose file is not indexed follows its slug
// when it is a title-level field, and is skipped otherwise: a harmless orphan.
func groupMediaUserFieldsByTitle(
	ctx context.Context, mediaDB MediaDBI, rows []MediaUserField,
) (map[int64][]MediaUserField, error) {
	bySystem := make(map[string][]int)
	for i := range rows {
		bySystem[rows[i].SystemID] = append(bySystem[rows[i].SystemID], i)
	}
	titleOf := make(map[int]int64, len(rows))
	for systemID, indexes := range bySystem {
		system, err := mediaDB.FindSystemBySystemID(systemID)
		if errors.Is(err, sql.ErrNoRows) {
			continue // system not indexed
		}
		if err != nil {
			return nil, fmt.Errorf("failed to resolve system %q: %w", systemID, err)
		}
		paths := make([]string, 0, len(indexes))
		for _, i := range indexes {
			paths = append(paths, rows[i].Path)
		}
		mediaByPath, err := mediaDB.FindMediaBySystemAndPaths(ctx, system.DBID, paths)
		if err != nil {
			return nil, fmt.Errorf("failed to look up media for system %q: %w", systemID, err)
		}
		titleBySlug := make(map[string]int64)
		for _, i := range indexes {
			if media, indexed := mediaByPath[rows[i].Path]; indexed {
				titleOf[i] = media.MediaTitleDBID
				continue
			}
			slug := rows[i].Slug
			if slug == "" || !IsMediaUserFieldTitleLevel(rows[i].Field) {
				continue
			}
			titleDBID, looked := titleBySlug[slug]
			if !looked {
				title, findErr := mediaDB.FindMediaTitleBySystemAndSlug(ctx, system.DBID, slug)
				switch {
				case errors.Is(findErr, sql.ErrNoRows) || (findErr == nil && title == nil):
				case findErr != nil:
					return nil, fmt.Errorf("failed to look up title %q of system %q: %w", slug, systemID, findErr)
				default:
					titleDBID = title.DBID
				}
				titleBySlug[slug] = titleDBID
			}
			if titleDBID != 0 {
				titleOf[i] = titleDBID
			}
		}
	}
	byTitle := make(map[int64][]MediaUserField)
	for i := range rows {
		if titleDBID, ok := titleOf[i]; ok {
			byTitle[titleDBID] = append(byTitle[titleDBID], rows[i])
		}
	}
	return byTitle, nil
}
