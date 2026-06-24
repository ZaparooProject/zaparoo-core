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

package methods

import (
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
)

// setMediaUserFavorite records the favourite intent for a media path in UserDB,
// the source of truth; callers then materialize the media.db projection. The
// write is column-scoped and atomic so it cannot clobber a concurrent launcher
// override edit on the same path.
func setMediaUserFavorite(env *requests.RequestEnv, systemID, path string, favorite bool) error {
	if err := env.Database.UserDB.SetMediaUserFavorite(systemID, path, favorite); err != nil {
		return fmt.Errorf("failed to set media user favorite: %w", err)
	}
	return nil
}

// setMediaUserLauncherOverride records the launcher-override intent for a media
// path in UserDB. An empty launcherID clears the override. See
// setMediaUserFavorite for the concurrency guarantee.
func setMediaUserLauncherOverride(env *requests.RequestEnv, systemID, path, launcherID string) error {
	if err := env.Database.UserDB.SetMediaUserLauncherOverride(systemID, path, launcherID); err != nil {
		return fmt.Errorf("failed to set media user launcher override: %w", err)
	}
	return nil
}
