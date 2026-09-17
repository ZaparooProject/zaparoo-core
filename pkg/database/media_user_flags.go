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
	"fmt"

	mediatags "github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
)

// mediaUserFlagsMu serializes flag edits, so one edit's UserDB write and the
// MediaDB projection that follows it cannot interleave with another's.
var mediaUserFlagsMu syncutil.Mutex

// ApplyMediaUserFlags records flag changes for one media path in UserDB, the
// source of truth, then brings the file's user tags in MediaDB in line with
// the row UserDB holds afterwards. Only the requested flags are written; the
// UserDB write itself clears a flag the model forbids beside a set one.
//
// The projection compares every flag with the file's current tags and writes
// only the differences, so a retry after a failed projection still removes a
// tag the failed attempt left behind. A mediaDBID of 0 skips the projection.
// It returns the flags whose MediaDB tag changed, with their new value.
func ApplyMediaUserFlags(
	ctx context.Context,
	db *Database,
	systemID, path string,
	mediaDBID int64,
	changes map[MediaUserFlag]bool,
) (map[MediaUserFlag]bool, error) {
	requested := MediaUserData{}
	for flag, value := range changes {
		if !value {
			continue
		}
		if !requested.setFlag(flag) {
			return nil, fmt.Errorf("unknown media user flag %q", flag)
		}
	}
	if err := requested.ValidateFlags(); err != nil {
		return nil, err
	}

	mediaUserFlagsMu.Lock()
	defer mediaUserFlagsMu.Unlock()

	for _, flag := range MediaUserFlags {
		value, ok := changes[flag]
		if !ok {
			continue
		}
		if err := db.UserDB.SetMediaUserFlag(systemID, path, flag, value); err != nil {
			return nil, fmt.Errorf("failed to set media user %s: %w", flag, err)
		}
	}
	written := make(map[MediaUserFlag]bool)
	if mediaDBID <= 0 {
		return written, nil
	}

	stored, _, err := db.UserDB.GetMediaUserData(systemID, path)
	if err != nil {
		return nil, fmt.Errorf("failed to read media user data: %w", err)
	}
	fileTags, err := db.MediaDB.GetMediaTagsByMediaDBID(ctx, mediaDBID)
	if err != nil {
		return nil, fmt.Errorf("failed to read media tag projection: %w", err)
	}
	projected := make(map[MediaUserFlag]bool, len(MediaUserFlags))
	for _, tag := range fileTags {
		if tag.Type == string(mediatags.TagTypeUser) {
			projected[MediaUserFlag(tag.Tag)] = true
		}
	}

	var add, remove []MediaTagRef
	for _, flag := range MediaUserFlags {
		want := stored.Flag(flag)
		if projected[flag] == want {
			continue
		}
		written[flag] = want
		ref := MediaTagRef{Type: string(mediatags.TagTypeUser), Tag: string(flag)}
		if want {
			add = append(add, ref)
		} else {
			remove = append(remove, ref)
		}
	}
	if len(written) == 0 {
		return written, nil
	}
	if err := db.MediaDB.UpdateMediaTags(ctx, mediaDBID, remove, add); err != nil {
		return nil, fmt.Errorf("failed to update media tag projection: %w", err)
	}
	return written, nil
}

// setFlag sets one flag to true, reporting false for an unknown flag.
func (d *MediaUserData) setFlag(flag MediaUserFlag) bool {
	switch flag {
	case MediaUserFlagFavorite:
		d.IsFavorite = true
	case MediaUserFlagHidden:
		d.IsHidden = true
	case MediaUserFlagLiked:
		d.IsLiked = true
	case MediaUserFlagDisliked:
		d.IsDisliked = true
	case MediaUserFlagPlayLater:
		d.IsPlayLater = true
	default:
		return false
	}
	return true
}
