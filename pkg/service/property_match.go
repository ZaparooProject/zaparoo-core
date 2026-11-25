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
	"regexp"
	"slices"
	"strings"

	gozapscript "github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
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

	disc := discFromProperties(properties)
	if disc.kind == "" {
		resolveImageMatch(ctx, svc, token, properties)
		return
	}

	if disc.kind == readers.DiscKindAudio {
		resolveAudioDisc(svc, token, disc)
		return
	}

	physical := func() bool { return resolvePhysicalDisc(svc, token, disc) }
	image := func() bool { return resolveImageMatch(ctx, svc, token, properties) }
	order := svc.Config.OpticalDriveLaunchOrder()
	var resolved bool
	switch order {
	case config.DiscLaunchDiscFirst:
		resolved = physical() || image()
	case config.DiscLaunchDiscOnly:
		resolved = physical()
	case config.DiscLaunchImageOnly:
		resolved = image()
	default:
		resolved = image() || physical()
	}
	if !resolved {
		log.Info().Str("order", order).Str("system", disc.system).Str("drive", disc.drive).
			Msg("nothing can launch the scanned disc")
	}
}

// resolveImageMatch launches the indexed media a scan property identifies,
// such as a library image of the game on a disc, and reports whether it set
// one.
func resolveImageMatch(
	ctx context.Context,
	svc *ServiceContext,
	token *tokens.Token,
	properties []readers.ScanProperty,
) bool {
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
		return false
	}

	if len(matches) == 0 {
		log.Info().Any("properties", properties).Msg("no indexed media matched scanned properties")
		return false
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
	return true
}

// scannedDisc is what an optical drive reported about the disc it read.
type scannedDisc struct {
	kind   string
	system string
	drive  string
}

var discDrivePattern = regexp.MustCompile(`^sr\d{1,3}$`)

func discFromProperties(properties []readers.ScanProperty) scannedDisc {
	var disc scannedDisc
	for _, property := range properties {
		switch property.Name {
		case readers.PropertyDiscKind:
			disc.kind = property.Value
		case readers.PropertyDiscSystem:
			disc.system = property.Value
		case readers.PropertyDiscDrive:
			if discDrivePattern.MatchString(property.Value) {
				disc.drive = property.Value
			}
		}
	}
	return disc
}

// launcherForPath reports whether an available launcher would play path. A
// launcher that only runs when named counts only as the one named.
func launcherForPath(svc *ServiceContext, scheme, path, named string) bool {
	all, ok := svc.LauncherCache.(interface{ GetAllLaunchers() []platforms.Launcher })
	if !ok {
		return false
	}
	launchers := all.GetAllLaunchers()
	for i := range launchers {
		launcher := &launchers[i]
		if named != "" && !strings.EqualFold(launcher.ID, named) {
			continue
		}
		if named == "" && launcher.ExplicitOnly {
			continue
		}
		if !slices.ContainsFunc(launcher.Schemes, func(s string) bool { return strings.EqualFold(s, scheme) }) {
			continue
		}
		if launcher.Test != nil && !launcher.Test(svc.Config, path) {
			continue
		}
		if launcher.Availability != nil {
			if err := launcher.Availability(svc.Config); err != nil {
				log.Debug().Err(err).Str("launcher", launcher.ID).Msg("launcher for the disc is unavailable")
				continue
			}
		}
		return true
	}
	return false
}

// resolvePhysicalDisc launches the disc in the drive itself, when the disc's
// system is known and a launcher can play discs from a drive.
func resolvePhysicalDisc(svc *ServiceContext, token *tokens.Token, disc scannedDisc) bool {
	if disc.system == "" || disc.drive == "" {
		return false
	}
	path := readers.PhysicalDiscScheme + "://" + disc.drive + "/" + disc.system
	if !launcherForPath(svc, readers.PhysicalDiscScheme, path, "") {
		log.Info().Str("path", path).Msg("nothing can play the disc from the drive")
		return false
	}
	token.Text = gozapscript.Command{
		Name: gozapscript.ZapScriptCmdLaunch,
		Args: []string{path},
	}.String()
	log.Info().Str("path", path).Msg("resolved scan to the disc in the drive")
	return true
}

// resolveAudioDisc plays an audio CD from the drive with the configured
// audio CD launcher, or whichever launcher handles CD audio.
func resolveAudioDisc(svc *ServiceContext, token *tokens.Token, disc scannedDisc) {
	if disc.drive == "" {
		return
	}
	path := readers.CDAudioScheme + "://" + disc.drive
	named := svc.Config.OpticalDriveAudioCDLauncher()
	if !launcherForPath(svc, readers.CDAudioScheme, path, named) {
		log.Info().Str("drive", disc.drive).Str("launcher", named).Msg("nothing can play the audio CD")
		return
	}
	cmd := gozapscript.Command{
		Name: gozapscript.ZapScriptCmdLaunch,
		Args: []string{path},
	}
	if named != "" {
		cmd.AdvArgs = cmd.AdvArgs.With(gozapscript.KeyLauncher, named)
	}
	token.Text = cmd.String()
	log.Info().Str("drive", disc.drive).Msg("resolved scan to the audio CD in the drive")
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
