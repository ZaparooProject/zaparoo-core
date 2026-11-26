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
	"errors"
	"path/filepath"
	"strconv"
	"strings"

	gozapscript "github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/validation"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/assets"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/userdb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
	"github.com/rs/zerolog/log"
)

// mappingSourcePlatform is a mapping the platform supplies itself. It has no
// ID because nothing can edit it.
const mappingSourcePlatform = "platform"

// HandleTokensResolve reports what the last token read would do if it were
// scanned again: the mapping it matches, the ZapScript that would run and
// the indexed game that ZapScript launches. Nothing runs.
//
//nolint:gocritic // single-use parameter in API handler
func HandleTokensResolve(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received tokens resolve request")

	var params models.TokensResolveParams
	if len(env.Params) > 0 {
		if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
			log.Warn().Err(err).Msg("invalid params")
			return nil, models.ClientErrf("invalid params: %w", err)
		}
	}

	last := env.State.GetLastScanned()
	if last.ScanTime.IsZero() {
		return nil, models.ClientErrf("no token has been read")
	}
	if params.ReaderID != nil && *params.ReaderID != "" && *params.ReaderID != last.ReaderID {
		return nil, models.ClientErrf("no token has been read on reader %s", *params.ReaderID)
	}

	text, data := zapscript.RedactToken(last.Text, last.Data)
	resp := models.TokensResolveResponse{
		Token: models.TokenResponse{
			Type:     last.Type,
			UID:      last.UID,
			Text:     text,
			Data:     data,
			ScanTime: last.ScanTime,
			ReaderID: last.ReaderID,
		},
		Redacted: text != last.Text || data != last.Data,
	}

	script, mapping := resolveTokenScript(&env, &last)
	resp.Mapping = mapping
	resp.Script = zapscript.RedactScript(script)
	if resp.Script != script {
		resp.Redacted = true
	}
	resp.Media = resolveScriptMedia(&env, script)

	return resp, nil
}

// resolveTokenScript returns the ZapScript a token runs, in the order the
// service checks: the user database and mappings folder, then the platform,
// then the token's own text.
func resolveTokenScript(env *requests.RequestEnv, t *tokens.Token) (string, *models.ResolvedMapping) {
	var udb userdb.EnabledMappingsReader
	if env.Database != nil && env.Database.UserDB != nil {
		udb = env.Database.UserDB
	}
	if match, ok := userdb.FindMapping(udb, env.Config, t); ok {
		id := ""
		if match.Source == userdb.MappingSourceDatabase {
			id = strconv.FormatInt(match.Mapping.DBID, 10)
		}
		return match.Mapping.Override, &models.ResolvedMapping{
			ID:     id,
			Source: match.Source,
			Label:  match.Mapping.Label,
		}
	}
	if env.Platform != nil {
		if override, ok := env.Platform.LookupMapping(t); ok {
			return override, &models.ResolvedMapping{Source: mappingSourcePlatform}
		}
	}
	return t.Text, nil
}

// resolveScriptMedia returns the indexed game the script's first launch
// names, by path or by title, or nil when it names none or the media
// database does not have it.
func resolveScriptMedia(env *requests.RequestEnv, script string) *models.ResolvedMedia {
	if strings.TrimSpace(script) == "" || env.Database == nil || env.Database.MediaDB == nil {
		return nil
	}
	parsed, err := gozapscript.NewParser(script).ParseScript()
	if err != nil {
		return nil
	}
	for _, cmd := range parsed.Cmds {
		if !zapscript.HasResolvedLaunchTarget(cmd.Name) {
			continue
		}
		if len(cmd.Args) == 0 {
			return nil
		}
		switch cmd.Name {
		case gozapscript.ZapScriptCmdLaunch:
			if row := findLaunchMedia(env, cmd.Args[0]); row != nil {
				return resolvedMedia(row.System.SystemID, row.System.Name, row.Path, row.Title.Name)
			}
			// A launch argument that names no file is taken as a title,
			// as the launch command takes it.
			if !zapscript.MightBeTitle(cmd.Args[0]) {
				return nil
			}
			return findTitleMedia(env, cmd)
		case gozapscript.ZapScriptCmdLaunchTitle:
			return findTitleMedia(env, cmd)
		default:
			return nil
		}
	}
	return nil
}

// findTitleMedia resolves a title launch the way launch.title does.
func findTitleMedia(env *requests.RequestEnv, cmd gozapscript.Command) *models.ResolvedMedia {
	if env.Platform == nil {
		return nil
	}
	cmdEnv := platforms.CmdEnv{
		Cmd:        cmd,
		Cfg:        env.Config,
		Database:   env.Database,
		ServiceCtx: env.Context,
		Launchers:  &platforms.LauncherSnapshot{},
	}
	result, err := zapscript.ResolveLaunchTitle(env.Platform, &cmdEnv)
	if err != nil {
		log.Debug().Err(err).Msg("tokens resolve: title lookup failed")
		return nil
	}
	return resolvedMedia(result.Result.SystemID, "", result.Result.Path, result.Result.Name)
}

func resolvedMedia(systemID, systemName, path, name string) *models.ResolvedMedia {
	if meta, err := assets.GetSystemMetadata(systemID); err == nil {
		systemName = meta.Name
	}
	return &models.ResolvedMedia{
		System:     systemID,
		SystemName: systemName,
		Path:       path,
		Name:       name,
	}
}

// findLaunchMedia looks up a launch argument that names a file, either an
// absolute path or a <system>/<path> value.
func findLaunchMedia(env *requests.RequestEnv, arg string) *database.MediaFullRow {
	if filepath.IsAbs(arg) {
		ids, err := env.Database.MediaDB.FindMediaIDsByPaths(env.Context, []string{arg})
		if err != nil || len(ids) != 1 {
			return nil
		}
		row, err := env.Database.MediaDB.GetMediaWithTitleAndSystem(env.Context, ids[0].DBID)
		if err != nil {
			return nil
		}
		return row
	}

	systemPart, rest, ok := strings.Cut(filepath.ToSlash(arg), "/")
	if !ok || systemPart == "" || rest == "" {
		return nil
	}
	system, err := systemdefs.LookupSystem(systemPart)
	if err != nil {
		return nil
	}
	row, err := resolveMediaBySystemAndPath(env, system.ID, rest)
	if err != nil {
		var clientErr *models.ClientError
		if !errors.As(err, &clientErr) {
			log.Debug().Err(err).Msg("tokens resolve: media lookup failed")
		}
		return nil
	}
	return row
}
