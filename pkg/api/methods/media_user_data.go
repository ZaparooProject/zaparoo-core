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
	"context"
	"fmt"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/rs/zerolog/log"
)

// setMediaUserFavorite records the favourite intent for a media path in UserDB,
// the source of truth; callers then materialize the media.db projection. The
// write is column-scoped and atomic so it cannot clobber a concurrent launcher
// override edit on the same path.
func setMediaUserFavorite(env *requests.RequestEnv, systemID, path string, favorite bool) error {
	if err := env.Database.UserDB.SetMediaUserFavorite(systemID, path, favorite); err != nil {
		return fmt.Errorf("failed to set media user favorite: %w", err)
	}
	snapshotMediaUserIdentity(env, systemID, path)
	return nil
}

// setMediaUserLauncherOverride records the launcher-override intent for a media
// path in UserDB. An empty launcherID clears the override. See
// setMediaUserFavorite for the concurrency guarantee.
func setMediaUserLauncherOverride(env *requests.RequestEnv, systemID, path, launcherID string) error {
	if err := env.Database.UserDB.SetMediaUserLauncherOverride(systemID, path, launcherID); err != nil {
		return fmt.Errorf("failed to set media user launcher override: %w", err)
	}
	snapshotMediaUserIdentity(env, systemID, path)
	return nil
}

// snapshotMediaUserIdentity best-effort captures the scanner's identity
// (display name + disambiguating tags) onto the user-data row just written.
// MediaDB is disposable, so this is the only durable record of what the path
// identified when the user marked it. Failures are logged, never surfaced:
// user intent was already recorded.
func snapshotMediaUserIdentity(env *requests.RequestEnv, systemID, path string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	identity, found := database.LookupMediaIdentity(ctx, env.Database.MediaDB, systemID, path)
	if !found {
		return
	}
	if err := env.Database.UserDB.SetMediaUserSnapshot(systemID, path, identity.Name, identity.Tags); err != nil {
		log.Warn().Err(err).Str("path", path).Msg("failed to store media user identity snapshot")
	}
}
