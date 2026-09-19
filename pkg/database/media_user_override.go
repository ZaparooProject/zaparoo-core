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
	"errors"
	"fmt"

	mediatags "github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
)

func launcherOverrideTypeTag() string {
	return mediatags.PropertyTypeTag(mediatags.TagPropertyLauncherOverride)
}

// ApplyMediaUserLauncherOverride records a launcher override for one media
// path in UserDB, the source of truth, then writes it to the file's MediaDB
// property. An empty launcherID clears both. A mediaDBID of 0 skips the
// projection. It shares a lock with ApplyMediaUserFlags and
// ReconcileMediaUserData, so a reconcile cannot overwrite an edit with the
// value it read before the edit.
func ApplyMediaUserLauncherOverride(
	ctx context.Context,
	db *Database,
	systemID, path string,
	mediaDBID int64,
	launcherID string,
) error {
	mediaUserFlagsMu.Lock()
	defer mediaUserFlagsMu.Unlock()

	if err := db.UserDB.SetMediaUserLauncherOverride(systemID, path, launcherID); err != nil {
		return fmt.Errorf("failed to set media user launcher override: %w", err)
	}
	if mediaDBID <= 0 {
		return nil
	}
	if launcherID == "" {
		return clearLauncherOverrideProperty(ctx, db.MediaDB, mediaDBID)
	}
	return setLauncherOverrideProperty(ctx, db.MediaDB, mediaDBID, launcherID)
}

func setLauncherOverrideProperty(ctx context.Context, mediaDB MediaDBI, mediaDBID int64, launcherID string) error {
	if err := ensureLauncherOverrideTag(mediaDB); err != nil {
		return err
	}
	if err := mediaDB.UpsertMediaProperties(ctx, mediaDBID, []MediaProperty{{
		TypeTag: launcherOverrideTypeTag(),
		Text:    launcherID,
	}}); err != nil {
		return fmt.Errorf("failed to set media launcher override: %w", err)
	}
	return nil
}

// clearLauncherOverrideProperty removes the file's override. A database that
// never stored an override has no tag for it, and nothing to clear.
func clearLauncherOverrideProperty(ctx context.Context, mediaDB MediaDBI, mediaDBID int64) error {
	tagDBID, found, err := findLauncherOverrideTag(mediaDB)
	if err != nil || !found {
		return err
	}
	if err := mediaDB.DeleteMediaProperty(ctx, mediaDBID, tagDBID); err != nil {
		return fmt.Errorf("failed to clear media launcher override: %w", err)
	}
	return nil
}

func ensureLauncherOverrideTag(mediaDB MediaDBI) error {
	tagType, err := mediaDB.FindOrInsertTagType(TagType{
		Type:        string(mediatags.TagTypeProperty),
		IsExclusive: mediatags.IsExclusiveType(mediatags.TagTypeProperty),
	})
	if err != nil {
		return fmt.Errorf("failed to find or insert launcher override tag type: %w", err)
	}
	if _, err = mediaDB.FindOrInsertTag(Tag{
		TypeDBID: tagType.DBID,
		Tag:      string(mediatags.TagPropertyLauncherOverride),
	}); err != nil {
		return fmt.Errorf("failed to find or insert launcher override tag: %w", err)
	}
	return nil
}

func findLauncherOverrideTag(mediaDB MediaDBI) (tagDBID int64, found bool, err error) {
	tagType, err := mediaDB.FindTagType(TagType{Type: string(mediatags.TagTypeProperty)})
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("failed to find launcher override tag type: %w", err)
	}
	tag, err := mediaDB.FindTag(Tag{
		TypeDBID: tagType.DBID,
		Tag:      string(mediatags.TagPropertyLauncherOverride),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("failed to find launcher override tag: %w", err)
	}
	return tag.DBID, true, nil
}
