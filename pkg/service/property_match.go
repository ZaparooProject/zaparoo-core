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

package service

import (
	"context"

	gozapscript "github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/rs/zerolog/log"
)

var dbMatchProperties = map[string]struct{}{
	string(tags.TagPropertyGameID): {},
}

// resolveTokenProperties looks for a launch implied by properties a reader
// identified about the scan. It only runs when no explicit mapping already
// resolves the token; mappings always win.
func resolveTokenProperties(
	ctx context.Context,
	svc *ServiceContext,
	token *tokens.Token,
	properties []readers.ScanProperty,
) {
	if token == nil || len(properties) == 0 || token.Text != "" {
		return
	}

	if _, hasMapping := getMapping(svc.Config, svc.DB, svc.Platform, *token); hasMapping {
		return
	}

	matches := make([]database.SearchResult, 0, 1)
	seen := make(map[int64]struct{})
	attempted := false
	for _, property := range properties {
		if _, ok := dbMatchProperties[property.Name]; !ok {
			continue
		}
		attempted = true

		results, err := svc.DB.MediaDB.SearchMediaByProperty(ctx, property.System, property.Name, property.Value)
		if err != nil {
			log.Warn().Err(err).
				Str("system", property.System).
				Str("property", property.Name).
				Str("value", property.Value).
				Msg("failed to resolve scan property")
			continue
		}
		for _, result := range results {
			if _, ok := seen[result.MediaID]; ok {
				continue
			}
			seen[result.MediaID] = struct{}{}
			matches = append(matches, result)
		}
	}
	if !attempted {
		return
	}

	switch len(matches) {
	case 0:
		log.Info().Any("properties", properties).Msg("no indexed media matched scanned properties")
	case 1:
		token.Text = gozapscript.Command{
			Name: gozapscript.ZapScriptCmdLaunch,
			Args: []string{matches[0].Path},
		}.String()
		log.Info().Str("system", matches[0].SystemID).Str("path", matches[0].Path).
			Msg("resolved scan by property match")
	default:
		log.Warn().Any("properties", properties).Int("matches", len(matches)).
			Msg("scan property matched multiple media")
	}
}
