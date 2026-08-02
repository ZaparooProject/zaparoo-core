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
	"strings"

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

var deprioritizedPropertyMatchTags = map[database.MediaTagRef]struct{}{
	{Type: string(tags.TagTypeUnfinished), Tag: string(tags.TagUnfinishedSample)}:         {},
	{Type: string(tags.TagTypeUnfinished), Tag: string(tags.TagUnfinishedPreview)}:        {},
	{Type: string(tags.TagTypeUnfinished), Tag: string(tags.TagUnfinishedPrerelease)}:     {},
	{Type: string(tags.TagTypeUnlicensed), Tag: string(tags.TagUnlicensedBootleg)}:        {},
	{Type: string(tags.TagTypeUnlicensed), Tag: string(tags.TagUnlicensedHack)}:           {},
	{Type: string(tags.TagTypeUnlicensed), Tag: string(tags.TagUnlicensedClone)}:          {},
	{Type: string(tags.TagTypeUnlicensed), Tag: string(tags.TagUnlicensedTranslation)}:    {},
	{Type: string(tags.TagTypeUnlicensed), Tag: string(tags.TagUnlicensedTranslationOld)}: {},
	{Type: string(tags.TagTypeDump), Tag: string(tags.TagDumpHacked)}:                     {},
	{Type: string(tags.TagTypeDump), Tag: string(tags.TagDumpModified)}:                   {},
	{Type: string(tags.TagTypeDump), Tag: string(tags.TagDumpTranslated)}:                 {},
	{Type: string(tags.TagTypeDump), Tag: string(tags.TagDumpBad)}:                        {},
}

var deprioritizedUnfinishedPrefixes = []string{
	string(tags.TagUnfinishedAlpha),
	string(tags.TagUnfinishedBeta),
	string(tags.TagUnfinishedProto),
	string(tags.TagUnfinishedDemo),
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

	if len(matches) == 0 {
		log.Info().Any("properties", properties).Msg("no indexed media matched scanned properties")
		return
	}

	selected := matches[0]
	if len(matches) > 1 {
		var preferredNonVariant bool
		selected, preferredNonVariant = selectPreferredPropertyMatch(ctx, svc, matches)
		selection := "first"
		if preferredNonVariant {
			selection = "non_variant"
		}
		log.Warn().Any("properties", properties).Int("matches", len(matches)).
			Str("selection", selection).
			Str("selected_path", selected.Path).
			Msg("scan property matched multiple media; selecting preferred match")
	}

	token.Text = gozapscript.Command{
		Name: gozapscript.ZapScriptCmdLaunch,
		Args: []string{selected.Path},
	}.String()
	log.Info().Str("system", selected.SystemID).Str("path", selected.Path).
		Msg("resolved scan by property match")
}

func selectPreferredPropertyMatch(
	ctx context.Context,
	svc *ServiceContext,
	matches []database.SearchResult,
) (database.SearchResult, bool) {
	mediaIDs := make([]int64, len(matches))
	for i := range matches {
		mediaIDs[i] = matches[i].MediaID
	}

	mediaTags, err := svc.DB.MediaDB.GetMediaTagsByMediaDBIDs(ctx, mediaIDs)
	if err != nil {
		log.Warn().Err(err).Msg("failed to load media tags for property match ranking")
		return matches[0], false
	}

	firstPreferred := -1
	deprioritized := 0
	for i := range matches {
		if isDeprioritizedPropertyMatch(mediaTags[matches[i].MediaID]) {
			deprioritized++
			continue
		}
		if firstPreferred == -1 {
			firstPreferred = i
		}
	}

	if deprioritized > 0 && firstPreferred >= 0 {
		return matches[firstPreferred], true
	}
	return matches[0], false
}

func isDeprioritizedPropertyMatch(mediaTags []database.TagInfo) bool {
	for _, mediaTag := range mediaTags {
		if _, ok := deprioritizedPropertyMatchTags[database.MediaTagRef{
			Type: mediaTag.Type,
			Tag:  mediaTag.Tag,
		}]; ok {
			return true
		}
		if mediaTag.Type != string(tags.TagTypeUnfinished) {
			continue
		}
		for _, prefix := range deprioritizedUnfinishedPrefixes {
			if strings.HasPrefix(mediaTag.Tag, prefix) {
				return true
			}
		}
	}
	return false
}
