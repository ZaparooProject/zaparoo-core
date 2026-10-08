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
)

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
	if err := db.UserDB.SetDirectoryHidden(systemID, path, hidden); err != nil {
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
	dirs, err := userDB.ListHiddenDirectories()
	if err != nil {
		return false, fmt.Errorf("failed to list hidden directories: %w", err)
	}
	changed, err := mediaDB.ReplaceHiddenDirectories(ctx, dirs)
	if err != nil {
		return false, fmt.Errorf("failed to project hidden directories: %w", err)
	}
	return changed, nil
}
