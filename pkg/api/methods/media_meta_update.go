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
	"database/sql"
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
		if patch.LauncherOverride == nil {
			if err := clearMediaLauncherOverride(&env, row.DBID); err != nil {
				return nil, err
			}
		} else {
			launcherID, err := resolveLauncherOverrideID(&env, row.System.SystemID, *patch.LauncherOverride)
			if err != nil {
				return nil, err
			}
			if err := setMediaLauncherOverride(&env, row.DBID, launcherID); err != nil {
				return nil, err
			}
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

func setMediaLauncherOverride(env *requests.RequestEnv, mediaDBID int64, launcherID string) error {
	if err := ensureLauncherOverridePropertyTag(env.Database.MediaDB); err != nil {
		return err
	}
	prop := database.MediaProperty{
		TypeTag: launcherOverridePropertyTypeTag(),
		Text:    launcherID,
	}
	if err := env.Database.MediaDB.UpsertMediaProperties(
		env.Context, mediaDBID, []database.MediaProperty{prop},
	); err != nil {
		return fmt.Errorf("failed to set media launcher override: %w", err)
	}
	return nil
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

func ensureLauncherOverridePropertyTag(mediaDB database.MediaDBI) error {
	tagType, err := mediaDB.FindOrInsertTagType(database.TagType{
		Type:        string(tags.TagTypeProperty),
		IsExclusive: tags.IsExclusiveType(tags.TagTypeProperty),
	})
	if err != nil {
		return fmt.Errorf("failed to find or insert launcher override tag type: %w", err)
	}
	_, err = mediaDB.FindOrInsertTag(database.Tag{
		TypeDBID: tagType.DBID,
		Tag:      string(tags.TagPropertyLauncherOverride),
	})
	if err != nil {
		return fmt.Errorf("failed to find or insert launcher override tag: %w", err)
	}
	return nil
}

func clearMediaLauncherOverride(env *requests.RequestEnv, mediaDBID int64) error {
	mediaDB := env.Database.MediaDB
	tagType, err := mediaDB.FindTagType(database.TagType{Type: string(tags.TagTypeProperty)})
	if err != nil {
		return ignoreMissingPropertyTag(err)
	}
	tagRow, err := mediaDB.FindTag(database.Tag{
		TypeDBID: tagType.DBID,
		Tag:      string(tags.TagPropertyLauncherOverride),
	})
	if err != nil {
		return ignoreMissingPropertyTag(err)
	}
	if err := mediaDB.DeleteMediaProperty(env.Context, mediaDBID, tagRow.DBID); err != nil {
		return fmt.Errorf("failed to clear media launcher override: %w", err)
	}
	return nil
}

func ignoreMissingPropertyTag(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	return err
}
