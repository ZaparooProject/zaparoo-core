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

package zapscript

import (
	"context"
	"errors"
	"time"

	"github.com/ZaparooProject/go-zapscript"
	apimodels "github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/decks"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript/titles"
	"github.com/rs/zerolog/log"
)

const (
	// pickerPathLookupBudget bounds the indexed path lookups that name the
	// games of a picker's items.
	pickerPathLookupBudget = 2 * time.Second
	// pickerTitleLookupBudget bounds every title resolution for one picker
	// together, so a long list of title launches cannot hold the picker up.
	// Items not reached in time show without a game.
	pickerTitleLookupBudget = time.Second
)

// launchersForSystem returns the launchers a title launch ranks matches
// with.
func launchersForSystem(systemID string) []platforms.Launcher {
	if helpers.GlobalLauncherCache == nil {
		return nil
	}
	return helpers.GlobalLauncherCache.GetLaunchersBySystem(systemID)
}

// playlistChoiceMedia names the local game each playlist item launches, by
// item index. An item that launches a file with an explicit system, the way
// an anchored deck item does, names that file when it is indexed and present.
// A title launch names the file it resolves to with acceptable confidence.
// Anything else, or anything not resolved in time, names no game.
func playlistChoiceMedia(env *platforms.CmdEnv, items []playlists.PlaylistItem) []*apimodels.UIChoiceMedia {
	out := make([]*apimodels.UIChoiceMedia, len(items))
	if env == nil || env.Database == nil || env.Database.MediaDB == nil || len(items) == 0 {
		return out
	}
	mediaDB := env.Database.MediaDB
	parent := env.ServiceCtx
	if parent == nil {
		parent = context.Background()
	}

	pathCtx, cancelPaths := context.WithTimeout(parent, pickerPathLookupBudget)
	defer cancelPaths()
	var titleItems []int
	for i := range items {
		systemID, path, ok := launchWithSystem(items[i].ZapScript)
		if !ok {
			if _, _, isTitle := decks.ParseTitleLaunch(items[i].ZapScript); isTitle {
				titleItems = append(titleItems, i)
			}
			continue
		}
		out[i] = indexedChoiceMedia(pathCtx, mediaDB, systemID, path)
	}

	if len(titleItems) == 0 {
		return out
	}
	titleCtx, cancelTitles := context.WithTimeout(parent, pickerTitleLookupBudget)
	defer cancelTitles()
	for _, i := range titleItems {
		if titleCtx.Err() != nil {
			break
		}
		out[i] = titleChoiceMedia(titleCtx, env, items[i].ZapScript)
	}
	return out
}

// launchWithSystem returns the system and path of a script that is a single
// launch of one path with an explicit system.
func launchWithSystem(script string) (systemID, path string, ok bool) {
	parsed, err := zapscript.NewParser(script).ParseScript()
	if err != nil || len(parsed.Cmds) != 1 {
		return "", "", false
	}
	cmd := parsed.Cmds[0]
	if cmd.Name != zapscript.ZapScriptCmdLaunch || len(cmd.Args) != 1 || cmd.Args[0] == "" {
		return "", "", false
	}
	raw := cmd.AdvArgs.Get(zapscript.KeySystem)
	if raw == "" {
		return "", "", false
	}
	system, err := systemdefs.LookupSystem(raw)
	if err != nil || system == nil {
		return "", "", false
	}
	return system.ID, cmd.Args[0], true
}

func indexedChoiceMedia(
	ctx context.Context, mediaDB database.MediaDBI, systemID, path string,
) *apimodels.UIChoiceMedia {
	system, err := mediaDB.FindSystemBySystemID(systemID)
	if err != nil {
		return nil
	}
	found, err := mediaDB.FindMediaBySystemAndPaths(ctx, system.DBID, []string{path})
	if err != nil {
		log.Debug().Err(err).Str("system", systemID).Msg("picker item media lookup failed")
		return nil
	}
	media, ok := found[path]
	if !ok || media.IsMissing {
		return nil
	}
	return choiceMedia(ctx, mediaDB, systemID, path, media.SortName)
}

func titleChoiceMedia(ctx context.Context, env *platforms.CmdEnv, script string) *apimodels.UIChoiceMedia {
	system, gameName, ok := decks.ParseTitleLaunch(script)
	if !ok {
		return nil
	}
	result, err := titles.ResolveTitle(ctx, &titles.ResolveParams{
		MediaDB:   env.Database.MediaDB,
		Cfg:       env.Cfg,
		SystemID:  system.ID,
		GameName:  gameName,
		MediaType: system.GetMediaType(),
		Launchers: launchersForSystem(system.ID),
	})
	switch {
	case errors.Is(err, titles.ErrNoMatch), errors.Is(err, titles.ErrLowConfidence):
		return nil
	case err != nil:
		log.Debug().Err(err).Str("script", script).Msg("picker item title lookup failed")
		return nil
	case result == nil || result.Confidence < titles.ConfidenceAcceptable || result.Result.Path == "":
		return nil
	}
	return choiceMedia(ctx, env.Database.MediaDB, result.Result.SystemID, result.Result.Path, result.Result.Name)
}

// choiceMedia names a game the way the library does, falling back to the
// given name when the file has no recorded identity.
func choiceMedia(
	ctx context.Context, mediaDB database.MediaDBI, systemID, path, fallback string,
) *apimodels.UIChoiceMedia {
	name := fallback
	if identity, found, err := database.LookupMediaIdentity(ctx, mediaDB, systemID, path); err == nil && found &&
		identity.DisplayName != "" {
		name = identity.DisplayName
	}
	return &apimodels.UIChoiceMedia{System: systemID, Path: path, Name: name}
}
