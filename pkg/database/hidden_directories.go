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
)

// A hidden folder is recorded in UserDB as the hidden flag of a MediaUserData
// row whose path is the folder's, the same preference a hidden file has. No
// row says which it is: a folder is a hidden path that names no indexed file
// and holds indexed media. Its projection in MediaDB is the list of those
// folders, which browse and search apply by path.

// ApplyDirectoryHidden records (or clears) the hide on one folder of one
// system in UserDB, the source of truth, then brings MediaDB's projection in
// line. It reports whether the projection changed.
func ApplyDirectoryHidden(ctx context.Context, db *Database, systemID, path string, hidden bool) (bool, error) {
	mediaUserFlagsMu.Lock()
	defer mediaUserFlagsMu.Unlock()

	// UserDB is written first, so a projection that cannot follow would leave
	// the two stores apart. Refuse before either is touched.
	if err := EnsureMediaWritable(db.MediaDB); err != nil {
		return false, err
	}
	if err := db.UserDB.SetMediaUserHidden(systemID, path, hidden); err != nil {
		return false, fmt.Errorf("failed to set hidden directory: %w", err)
	}
	return projectHiddenDirectories(ctx, db.UserDB, db.MediaDB)
}

// SyncHiddenDirectories brings MediaDB's hidden folder projection in line
// with UserDB. It is for whenever one of the two was replaced or rebuilt as a
// whole: startup, a reindex, a backup restore. A projection already in line
// writes nothing.
func SyncHiddenDirectories(ctx context.Context, userDB UserDBI, mediaDB MediaDBI) error {
	mediaUserFlagsMu.Lock()
	defer mediaUserFlagsMu.Unlock()
	_, err := projectHiddenDirectories(ctx, userDB, mediaDB)
	return err
}

func projectHiddenDirectories(ctx context.Context, userDB UserDBI, mediaDB MediaDBI) (bool, error) {
	rows, err := userDB.ListMediaUserData()
	if err != nil {
		return false, fmt.Errorf("failed to list media user data: %w", err)
	}
	return projectHiddenDirectoryRows(ctx, mediaDB, rows)
}

// projectHiddenDirectoryRows projects the hidden folders among rows, every
// MediaUserData row UserDB holds.
func projectHiddenDirectoryRows(ctx context.Context, mediaDB MediaDBI, rows []MediaUserData) (bool, error) {
	hidden := make([]MediaUserData, 0)
	for i := range rows {
		if rows[i].IsHidden {
			hidden = append(hidden, rows[i])
		}
	}
	dirs, err := hiddenDirectories(ctx, mediaDB, hidden)
	if err != nil {
		return false, err
	}
	changed, err := mediaDB.ReplaceHiddenDirectories(ctx, dirs)
	if err != nil {
		return false, fmt.Errorf("failed to project hidden directories: %w", err)
	}
	return changed, nil
}

// hiddenDirectories picks the folders out of the hidden rows: the paths that
// name no indexed file and hold indexed media for their system. A hidden path
// that is neither, such as a folder on a drive that is not connected, is left
// out until it is indexed again.
func hiddenDirectories(ctx context.Context, mediaDB MediaDBI, hidden []MediaUserData) ([]HiddenDirectory, error) {
	if len(hidden) == 0 {
		return nil, nil
	}
	files, err := resolveMediaUserDataIDs(ctx, mediaDB, hidden)
	if err != nil {
		return nil, err
	}
	systems := make(map[string]int64)
	var dirs []HiddenDirectory
	for i := range hidden {
		row := &hidden[i]
		if _, isFile := files[mediaUserDataKey{systemID: row.SystemID, path: row.Path}]; isFile {
			continue
		}
		systemDBID, known := systems[row.SystemID]
		if !known {
			system, sysErr := mediaDB.FindSystemBySystemID(row.SystemID)
			switch {
			case errors.Is(sysErr, sql.ErrNoRows):
				systemDBID = 0
			case sysErr != nil:
				return nil, fmt.Errorf("failed to resolve system %q: %w", row.SystemID, sysErr)
			default:
				systemDBID = system.DBID
			}
			systems[row.SystemID] = systemDBID
		}
		if systemDBID == 0 {
			continue
		}
		isDir, dirErr := mediaDB.HasMediaUnderDirectory(ctx, systemDBID, row.Path)
		if dirErr != nil {
			return nil, fmt.Errorf("failed to probe hidden directory: %w", dirErr)
		}
		if isDir {
			dirs = append(dirs, HiddenDirectory{SystemID: row.SystemID, Path: row.Path})
		}
	}
	return dirs, nil
}
