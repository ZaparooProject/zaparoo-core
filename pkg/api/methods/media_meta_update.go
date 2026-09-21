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
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/validation"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
)

type mediaMetaUpdatePatch struct {
	LauncherOverride    *string
	LauncherOverrideSet bool
}

func launcherOverridePropertyTypeTag() string {
	return tags.PropertyTypeTag(tags.TagPropertyLauncherOverride)
}

func HandleMediaMetaUpdate(env requests.RequestEnv) (any, error) { //nolint:gocritic // API handler shape
	var params models.MediaMetaUpdateParams
	if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
		return nil, models.ClientErrf("invalid params: %w", err)
	}

	mediaRef := mediaRefParam{
		MediaID: params.MediaID,
		System:  params.System,
		Path:    params.Path,
	}
	if err := validateMediaRef(mediaRef); err != nil {
		return nil, models.ClientErrf("invalid params: %w", err)
	}
	patch, err := parseMediaMetaUpdatePatch(params.Media)
	if err != nil {
		return nil, models.ClientErrf("invalid params: %w", err)
	}

	resolved, err := resolveMediaRefs(&env, []mediaRefParam{mediaRef})
	if err != nil {
		return nil, err
	}
	if len(resolved) != 1 || resolved[0].Err != nil || resolved[0].Row == nil {
		if len(resolved) == 1 && resolved[0].Err != nil {
			return nil, resolved[0].Err
		}
		return nil, models.ClientErrf("media not found")
	}

	row := resolved[0].Row
	if patch.LauncherOverrideSet {
		// The durable truth (UserDB) is written before the media.db projection.
		// If the projection write fails the truth is still saved and the next
		// reindex re-materializes it.
		launcherID := ""
		if patch.LauncherOverride != nil {
			launcherID, err = resolveLauncherOverrideID(&env, row.System.SystemID, *patch.LauncherOverride)
			if err != nil {
				return nil, err
			}
		}
		applyErr := database.ApplyMediaUserLauncherOverride(
			env.Context, env.Database, row.System.SystemID, row.Path, row.DBID, launcherID,
		)
		// The snapshot never inserts, so it is safe even when the write failed.
		snapshotMediaUserIdentity(&env, row.System.SystemID, row.Path)
		if applyErr != nil {
			return nil, fmt.Errorf("failed to apply media launcher override: %w", applyErr)
		}
	}

	return HandleMediaMeta(env)
}

func parseMediaMetaUpdatePatch(raw json.RawMessage) (mediaMetaUpdatePatch, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return mediaMetaUpdatePatch{}, errors.New("media update is required")
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return mediaMetaUpdatePatch{}, fmt.Errorf("media must be an object: %w", err)
	}
	if fields == nil {
		return mediaMetaUpdatePatch{}, errors.New("media must be an object")
	}

	var patch mediaMetaUpdatePatch
	for field, value := range fields {
		switch field {
		case "launcherOverride":
			patch.LauncherOverrideSet = true
			if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				continue
			}
			var launcherID string
			if err := json.Unmarshal(value, &launcherID); err != nil {
				return mediaMetaUpdatePatch{}, fmt.Errorf("media.launcherOverride must be a string or null: %w", err)
			}
			launcherID = strings.TrimSpace(launcherID)
			if launcherID == "" {
				return mediaMetaUpdatePatch{}, errors.New("media.launcherOverride cannot be empty; use null to clear")
			}
			patch.LauncherOverride = &launcherID
		default:
			return mediaMetaUpdatePatch{}, fmt.Errorf("unsupported media field: %s", field)
		}
	}
	if !patch.LauncherOverrideSet {
		return mediaMetaUpdatePatch{}, errors.New("no supported media updates provided")
	}
	return patch, nil
}

func resolveLauncherOverrideID(env *requests.RequestEnv, systemID, requested string) (string, error) {
	candidates := launcherCandidates(env)
	for i := range candidates {
		launcher := candidates[i]
		if !strings.EqualFold(launcher.ID, requested) {
			continue
		}
		if launcher.SystemID != "" && !strings.EqualFold(launcher.SystemID, systemID) {
			return "", models.ClientErrf("launcher %s does not support system %s", launcher.ID, systemID)
		}
		return launcher.ID, nil
	}
	return "", models.ClientErrf("launcher not found: %s", requested)
}

func launcherCandidates(env *requests.RequestEnv) []platforms.Launcher {
	if env.LauncherCache != nil {
		return env.LauncherCache.GetAllLaunchers()
	}
	if env.Platform == nil {
		return nil
	}
	return env.Platform.Launchers(env.Config)
}
