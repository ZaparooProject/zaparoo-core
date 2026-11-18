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
	"slices"
	"sort"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/imagefiles"
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
	Images map[int64]map[string]string
	// IsUserImage reports whether an image property's value is a user image,
	// so a user image that is no longer wanted is removed and art a scraper
	// wrote in its place is not. Nil counts every value as a user image.
	IsUserImage func(path string) bool
	TitleDBID   int64
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
	// Holds reports whether path names a file in the store, there or not.
	Holds(path string) bool
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
	ownedPaths := []string{row.Path}
	if IsMediaUserFieldTitleLevel(field) {
		ownedPaths = titlePaths
		if hasFieldsDB {
			owned, err := titleOwnedMediaUserFieldPaths(ctx, db, row, titlePaths, field)
			if err != nil {
				return MediaUserFieldsSync{}, err
			}
			ownedPaths = owned
		}
	}
	if value == nil {
		if err := db.UserDB.DeleteMediaUserField(systemID, "", ownedPaths, field); err != nil {
			return MediaUserFieldsSync{}, fmt.Errorf("failed to remove media user field %s: %w", field, err)
		}
	} else if err := db.UserDB.SetMediaUserField(&MediaUserField{
		SystemID: systemID, Path: row.Path, Field: field, Value: *value, Slug: row.Title.Slug,
	}, ownedPaths); err != nil {
		return MediaUserFieldsSync{}, fmt.Errorf("failed to set media user field %s: %w", field, err)
	}
	if !hasFieldsDB {
		return MediaUserFieldsSync{}, nil
	}

	rows, err := db.UserDB.GetMediaUserFieldsForTitle(systemID, row.Title.Slug, titlePaths)
	if err != nil {
		return MediaUserFieldsSync{}, fmt.Errorf("failed to read the title's media user fields: %w", err)
	}
	// A row matched by slug can belong to a file indexed under another title.
	// Resolve ownership the way a reconcile does, so both fold the same rows.
	byTitle, err := groupMediaUserFieldsByTitle(ctx, db.MediaDB, rows)
	if err != nil {
		return MediaUserFieldsSync{}, err
	}
	fields, err := foldMediaUserFields(row.Title.DBID, byTitle[row.Title.DBID], files, db.UserImages)
	if err != nil {
		db.QueueMediaUserDataReconcile()
		return MediaUserFieldsSync{}, err
	}
	result, err := fieldsDB.SyncMediaUserFields(ctx, fields)
	if err != nil {
		return MediaUserFieldsSync{}, fmt.Errorf("failed to write media user fields: %w", err)
	}
	if result.Changed {
		scope := &ScrapeScope{SystemID: systemID, Path: row.Path, MediaID: row.DBID}
		if scope.Validate() != nil {
			scope = nil
		}
		syncMediaUserFieldLinks(ctx, db, []string{systemID}, scope)
	}
	return result, nil
}

// syncMediaUserFieldLinks copies a title's edited fields onto the symlink
// aliases of its files, which take their target's data only when synced. A
// failure leaves the aliases as they were until the next scrape or index
// syncs them.
func syncMediaUserFieldLinks(ctx context.Context, db *Database, systemIDs []string, scope *ScrapeScope) {
	if err := db.MediaDB.SyncLinkedMedia(ctx, systemIDs, scope); err != nil {
		log.Warn().Err(err).Strs("systems", systemIDs).Msg("failed to copy media user fields onto linked media")
	}
}

// titleOwnedMediaUserFieldPaths returns the paths whose row of a title-level
// field belongs to row's title: the title's own files, and files that carried
// its slug and no longer resolve. A file that carried the slug but is now
// indexed under another title keeps its row; that title owns it.
func titleOwnedMediaUserFieldPaths(
	ctx context.Context, db *Database, row *MediaFullRow, titlePaths []string, field string,
) ([]string, error) {
	rows, err := db.UserDB.GetMediaUserFieldsForTitle(row.System.SystemID, row.Title.Slug, titlePaths)
	if err != nil {
		return nil, fmt.Errorf("failed to read the title's media user fields: %w", err)
	}
	byTitle, err := groupMediaUserFieldsByTitle(ctx, db.MediaDB, rows)
	if err != nil {
		return nil, err
	}
	paths := slices.Clone(titlePaths)
	for _, owned := range byTitle[row.Title.DBID] {
		if owned.Field == field && !slices.Contains(paths, owned.Path) {
			paths = append(paths, owned.Path)
		}
	}
	return paths, nil
}

// foldMediaUserFields builds what a title should carry from its UserDB rows,
// given oldest first so the newest row of a field decides it. An image whose
// file is not stored is left out, and the file then shows its scraped art. An
// image folder that cannot be checked fails the fold, so a passing error does
// not clear user art that is still there.
func foldMediaUserFields(
	titleDBID int64, rows []MediaUserField, files []Media, images MediaUserImageStore,
) (*MediaUserTitleFields, error) {
	state := &MediaUserTitleFields{
		TitleDBID:  titleDBID,
		Tags:       make(map[string][]MediaUserTagValue),
		Images:     make(map[int64]map[string]string),
		SkipImages: images == nil,
	}
	if images != nil {
		state.IsUserImage = images.Holds
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
			if errors.Is(err, imagefiles.ErrNotFound) {
				log.Debug().Err(err).Str("path", row.Path).Str("field", row.Field).
					Msg("user image is not stored; leaving the scraped art")
				continue
			}
			if err != nil {
				return nil, fmt.Errorf("failed to find the user image for %s: %w", row.Field, err)
			}
			if state.Images[mediaDBID] == nil {
				state.Images[mediaDBID] = make(map[string]string)
			}
			state.Images[mediaDBID][strings.TrimPrefix(row.Field, MediaUserFieldImagePrefix)] = path
		}
	}
	return state, nil
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

	// One title that fails does not hold back the titles after it.
	var errs []error
	var linkSystems []string
	for _, titleDBID := range titleIDs {
		if ctxErr := ctx.Err(); ctxErr != nil {
			errs = append(errs, fmt.Errorf("re-applying media user fields stopped: %w", ctxErr))
			break
		}
		files, listErr := fieldsDB.ListMediaByTitle(ctx, titleDBID)
		if listErr != nil {
			errs = append(errs, fmt.Errorf("failed to list the files of title %d: %w", titleDBID, listErr))
			continue
		}
		fields, foldErr := foldMediaUserFields(titleDBID, byTitle[titleDBID], files, db.UserImages)
		if foldErr != nil {
			errs = append(errs, fmt.Errorf("failed to build the user fields of title %d: %w", titleDBID, foldErr))
			continue
		}
		result, syncErr := fieldsDB.SyncMediaUserFields(ctx, fields)
		if syncErr != nil {
			errs = append(errs, fmt.Errorf("failed to write the user fields of title %d: %w", titleDBID, syncErr))
			continue
		}
		if result.Changed {
			changed++
			if result.SystemID != "" && !slices.Contains(linkSystems, result.SystemID) {
				linkSystems = append(linkSystems, result.SystemID)
			}
		}
	}
	if len(linkSystems) > 0 && ctx.Err() == nil {
		sort.Strings(linkSystems)
		syncMediaUserFieldLinks(ctx, db, linkSystems, nil)
	}
	return changed, errors.Join(errs...)
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
