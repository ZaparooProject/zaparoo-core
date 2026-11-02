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
	"strings"
	"time"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediascanner"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/pathutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript/titles"
	"github.com/rs/zerolog/log"
)

const optionalDBEnrichmentTimeout = 500 * time.Millisecond

type mediaPathRef struct {
	SystemID string
	Path     string
}

func mediaResponseRelativePath(env *requests.RequestEnv, systemID, path string) *string {
	if env == nil || env.LauncherCache == nil || env.Platform == nil {
		return nil
	}

	rootDirs := env.Platform.RootDirs(env.Config)
	rel := env.LauncherCache.ToRelativePath(rootDirs, systemID, path)
	if rel == path {
		return nil
	}
	return &rel
}

func mediaResponseMediaIDs(env *requests.RequestEnv, refs []mediaPathRef) map[mediaPathRef]int64 {
	if env == nil || env.Database == nil {
		return nil
	}
	ctx, cancel := optionalDBEnrichmentContext(env.Context)
	defer cancel()
	return mediaIDsByPath(ctx, env.Database.MediaDB, refs)
}

// activeMediaEnrichment resolves the media ID and title zapScript for the
// running primary media. A file played from outside the index, such as an
// Operator cartridge dump, takes both from the indexed title it matches. If
// the matched title's command cannot be loaded, the command built from the
// played name is kept: it still launches the title, though the title
// resolver may pick a different variant than the one the media ID names.
// A failed path lookup is not a miss, so it never falls back to a slug match.
func activeMediaEnrichment(
	env *requests.RequestEnv, systemID, path, name string,
) (mediaID int64, zapScript string) {
	var zapScriptTags []database.TagInfo
	if env.Database != nil && env.Database.MediaDB != nil {
		tags, err := env.Database.MediaDB.GetZapScriptTagsBySystemAndPath(env.Context, systemID, path)
		if err != nil {
			log.Debug().Err(err).Msgf("could not get tags for %s:%s", systemID, path)
		} else {
			zapScriptTags = tags
		}
	}
	zapScript = database.BuildTitleZapScript(systemID, name, zapScriptTags)
	if env.Database == nil || env.Database.MediaDB == nil {
		return 0, zapScript
	}

	ref := mediaPathRef{SystemID: systemID, Path: path}
	pathCtx, cancelPath := optionalDBEnrichmentContext(env.Context)
	rows, err := resolveMediaPathIDs(pathCtx, env.Database.MediaDB, []mediaPathRef{ref})
	cancelPath()
	if err != nil {
		log.Debug().Err(err).Msg("could not resolve active media ID by path")
		return 0, zapScript
	}
	if row := rows[ref]; row.DBID > 0 {
		return row.DBID, zapScript
	}

	addMediaIdentityMatches(env, rows, []identityRef{{Ref: ref}})
	match, ok := rows[ref]
	if !ok || match.DBID <= 0 {
		return 0, zapScript
	}
	ctx, cancel := optionalDBEnrichmentContext(env.Context)
	defer cancel()
	zapScripts := mediaTitleZapScripts(ctx, env.Database.MediaDB, []database.MediaRef{{
		MediaDBID:      match.DBID,
		MediaTitleDBID: match.MediaTitleDBID,
	}})
	if matched := zapScripts[match.DBID]; matched != "" {
		zapScript = matched
	}
	return match.DBID, zapScript
}

// formatCoverColor renders a 0xRRGGBB cover colour as "#rrggbb".
func formatCoverColor(value uint32) string {
	return fmt.Sprintf("#%06x", value&0xffffff)
}

// mediaCoverColors looks up recorded cover colours for one page of media
// rows. Colour is optional enrichment: failures and timeouts return no
// colours rather than failing the page.
func mediaCoverColors(ctx context.Context, mediaDB database.MediaDBI, mediaIDs []int64) map[int64]string {
	ids := make([]int64, 0, len(mediaIDs))
	for _, id := range mediaIDs {
		if id > 0 {
			ids = append(ids, id)
		}
	}
	if mediaDB == nil || len(ids) == 0 {
		return nil
	}
	lookupCtx, cancel := optionalDBEnrichmentContext(ctx)
	defer cancel()
	colors, err := mediaDB.GetMediaCoverColors(lookupCtx, ids)
	if err != nil {
		log.Debug().Err(err).Msg("could not enrich media cover colours")
		return nil
	}
	formatted := make(map[int64]string, len(colors))
	for id, value := range colors {
		formatted[id] = formatCoverColor(value)
	}
	return formatted
}

func optionalDBEnrichmentContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	//nolint:gosec // Caller owns and invokes returned cancel function.
	return context.WithTimeout(parent, optionalDBEnrichmentTimeout)
}

// enrichPlaybackState fills playback details on an ActiveMedia entry when the entry
// belongs to the native-audio launcher and a PlaybackManager is available. slot is
// the normalized slot string ("primary" or "background").
func enrichPlaybackState(env *requests.RequestEnv, entry *models.ActiveMedia, slot string) {
	if env.PlaybackManager == nil {
		return
	}
	if entry.LauncherID != platforms.NativeAudioLauncherID {
		return
	}
	state := env.PlaybackManager.State(slot)
	posMs := state.Position.Milliseconds()
	durMs := state.Duration.Milliseconds()
	entry.PositionMs = &posMs
	entry.DurationMs = &durMs
	switch {
	case state.Paused:
		entry.PlaybackState = models.MediaPlaybackStatePaused
	case state.Playing:
		entry.PlaybackState = models.MediaPlaybackStatePlaying
	default:
		entry.PlaybackState = models.MediaPlaybackStateStopped
	}
}

// toPlaylistState converts the internal Playlist representation to the API model.
func toPlaylistState(p *playlists.Playlist) models.PlaylistState {
	items := make([]models.PlaylistItemInfo, 0, len(p.Items))
	for _, item := range p.Items {
		items = append(items, models.PlaylistItemInfo{
			Name:      item.Name,
			ZapScript: item.ZapScript,
		})
	}

	repeat := "none"
	switch {
	case p.LoopOne:
		repeat = "one"
	case p.Loop:
		repeat = "all"
	}

	state := models.PlaylistState{
		ID:      p.ID,
		Name:    p.Name,
		Slot:    p.Slot,
		Items:   items,
		Index:   p.Index,
		Total:   len(p.Items),
		Playing: p.Playing,
		Repeat:  repeat,
		Unsafe:  p.Unsafe,
		Armed:   p.Armed,
	}
	if p.Advance > 0 {
		ms := p.Advance.Milliseconds()
		state.AdvanceMs = &ms
	}
	if p.Idle > 0 {
		ms := p.Idle.Milliseconds()
		state.IdleMs = &ms
	}
	if p.CountdownSet {
		ms := p.Countdown.Milliseconds()
		state.CountdownMs = &ms
	}
	return state
}

func resolveMediaPathIDs(
	ctx context.Context, db database.MediaDBI, refs []mediaPathRef,
) (map[mediaPathRef]database.MediaPathID, error) {
	if db == nil || len(refs) == 0 {
		return map[mediaPathRef]database.MediaPathID{}, nil
	}

	// Media.Path is stored in canonical forward-slash form, but history and
	// playtime rows keep the path the launch was given, which on Windows can
	// carry backslashes. Look up by the canonical form and hand each row back
	// under the caller's own ref, or Windows entries never get their tags.
	wanted := make(map[mediaPathRef]bool, len(refs))
	refsByCanonical := make(map[mediaPathRef][]mediaPathRef, len(refs))
	paths := make([]string, 0, len(refs))
	seenPaths := make(map[string]bool, len(refs))
	for _, ref := range refs {
		if ref.SystemID == "" || ref.Path == "" || wanted[ref] {
			continue
		}
		wanted[ref] = true
		canonical := pathutil.CanonicalMediaPath(ref.Path)
		key := mediaPathRef{SystemID: ref.SystemID, Path: canonical}
		refsByCanonical[key] = append(refsByCanonical[key], ref)
		if !seenPaths[canonical] {
			seenPaths[canonical] = true
			paths = append(paths, canonical)
		}
	}
	if len(paths) == 0 {
		return map[mediaPathRef]database.MediaPathID{}, nil
	}

	started := time.Now()
	rows, err := db.FindMediaIDsByPaths(ctx, paths)
	if err != nil {
		return nil, fmt.Errorf("resolve media IDs by path: %w", err)
	}

	resolved := make(map[mediaPathRef]database.MediaPathID, len(rows))
	for _, row := range rows {
		if row.DBID <= 0 {
			continue
		}
		key := mediaPathRef{SystemID: row.SystemID, Path: pathutil.CanonicalMediaPath(row.Path)}
		for _, ref := range refsByCanonical[key] {
			resolved[ref] = row
		}
	}

	log.Debug().
		Int("refs", len(refs)).
		Int("paths", len(paths)).
		Int("resolved", len(resolved)).
		Dur("duration", time.Since(started)).
		Msg("media ID enrichment timing")
	return resolved, nil
}

func resolveMediaIDsByPath(
	ctx context.Context, db database.MediaDBI, refs []mediaPathRef,
) (map[mediaPathRef]int64, error) {
	rows, err := resolveMediaPathIDs(ctx, db, refs)
	if err != nil {
		return nil, err
	}
	mediaIDs := make(map[mediaPathRef]int64, len(rows))
	for ref, row := range rows {
		mediaIDs[ref] = row.DBID
	}
	return mediaIDs, nil
}

func mediaIDsByPath(ctx context.Context, db database.MediaDBI, refs []mediaPathRef) map[mediaPathRef]int64 {
	mediaIDs, err := resolveMediaIDsByPath(ctx, db, refs)
	if err != nil {
		log.Debug().Err(err).Msg("could not resolve media IDs by path")
		return nil
	}
	return mediaIDs
}

// identityRef pairs a played path with the identity recorded for it.
type identityRef struct {
	Identity *database.MediaIdentity
	Ref      mediaPathRef
}

// identityKey groups refs that share one match: the same title with the
// same identity-role tags, which drive the variant pick.
type identityKey struct {
	SystemID string
	CoreSlug string
	Tags     string
}

func identityTagFilters(identity *database.MediaIdentity) []zapscript.TagFilter {
	tagFilters := make([]zapscript.TagFilter, 0, len(identity.Tags))
	for _, tag := range identity.Tags {
		if tag.Role != database.MediaIdentityTagRoleIdentity {
			continue
		}
		tagFilters = append(tagFilters, zapscript.TagFilter{
			Type:     tag.Type,
			Value:    tag.Value,
			Operator: zapscript.TagOperatorAND,
		})
	}
	return tagFilters
}

func newIdentityKey(systemID string, identity *database.MediaIdentity) identityKey {
	var tags strings.Builder
	for _, filter := range identityTagFilters(identity) {
		_, _ = tags.WriteString(filter.Type)
		_ = tags.WriteByte(':')
		_, _ = tags.WriteString(filter.Value)
		_ = tags.WriteByte(',')
	}
	return identityKey{SystemID: systemID, CoreSlug: identity.CoreSlug, Tags: tags.String()}
}

// resolveMediaIdentityIDs matches played files that are not in the index,
// such as Operator cartridge dumps, to an indexed row of the same title. It
// matches the identity's core slug exactly within the system, the same key
// Online matches plays on, and picks one row among the title's variants with
// the launch resolver's selection rules. Unmatched refs are left out.
func resolveMediaIdentityIDs(
	env *requests.RequestEnv, refs []identityRef,
) map[mediaPathRef]database.MediaPathID {
	resolved := make(map[mediaPathRef]database.MediaPathID)
	if env == nil || env.Database == nil || env.Database.MediaDB == nil || env.Config == nil || len(refs) == 0 {
		return resolved
	}
	ctx, cancel := optionalDBEnrichmentContext(env.Context)
	defer cancel()

	started := time.Now()
	matches := make(map[identityKey]database.MediaPathID, len(refs))
	for _, ref := range refs {
		if ref.Identity == nil || ref.Identity.CoreSlug == "" || ref.Ref.SystemID == "" {
			continue
		}
		key := newIdentityKey(ref.Ref.SystemID, ref.Identity)
		match, seen := matches[key]
		if !seen {
			var err error
			match, err = matchMediaIdentity(ctx, env, key, ref.Identity)
			if err != nil {
				log.Debug().Err(err).Str("system", key.SystemID).Str("slug", key.CoreSlug).
					Msg("could not match media identity to an indexed title")
				if ctx.Err() != nil {
					break
				}
			}
			matches[key] = match
		}
		if match.DBID > 0 {
			resolved[ref.Ref] = match
		}
	}

	log.Debug().
		Int("refs", len(refs)).
		Int("resolved", len(resolved)).
		Dur("duration", time.Since(started)).
		Msg("media identity enrichment timing")
	return resolved
}

func matchMediaIdentity(
	ctx context.Context, env *requests.RequestEnv, key identityKey, identity *database.MediaIdentity,
) (database.MediaPathID, error) {
	results, err := env.Database.MediaDB.SearchMediaBySlug(ctx, key.SystemID, key.CoreSlug, nil)
	if err != nil {
		return database.MediaPathID{}, fmt.Errorf("search media by slug: %w", err)
	}
	if len(results) == 0 {
		return database.MediaPathID{}, nil
	}
	tagFilters := identityTagFilters(identity)
	var launchers []platforms.Launcher
	if env.LauncherCache != nil {
		launchers = env.LauncherCache.GetLaunchersBySystem(key.SystemID)
	}
	best, _ := titles.SelectBestResult(results, tagFilters, env.Config, 1.0, launchers)
	return database.MediaPathID{
		SystemID:       best.SystemID,
		Path:           best.Path,
		DBID:           best.MediaID,
		MediaTitleDBID: best.MediaTitleID,
	}, nil
}

// addMediaIdentityMatches fills rows for refs the path lookup missed with
// the indexed title their identity matches. A ref without a recorded
// identity, such as a play whose identity snapshot has not landed yet or
// the running media, gets the identity the indexer would give its file,
// but only once the media database is settled: mid-index a library file
// may just not be indexed yet.
func addMediaIdentityMatches(
	env *requests.RequestEnv, rows map[mediaPathRef]database.MediaPathID, refs []identityRef,
) {
	missed := make([]identityRef, 0, len(refs))
	settledKnown, settled := false, false
	for _, ref := range refs {
		if row, ok := rows[ref.Ref]; ok && row.DBID > 0 {
			continue
		}
		if ref.Identity == nil {
			if env == nil || env.Config == nil || env.Database == nil || env.Database.MediaDB == nil {
				continue
			}
			identity, ok := mediascanner.FallbackMediaIdentity(env.Config, ref.Ref.SystemID, ref.Ref.Path)
			if !ok {
				continue
			}
			if !settledKnown {
				settledKnown = true
				settled = mediascanner.MediaDBSettled(env.Database.MediaDB)
			}
			if !settled {
				continue
			}
			ref.Identity = &identity
		}
		if ref.Identity.CoreSlug == "" {
			continue
		}
		missed = append(missed, ref)
	}
	if len(missed) == 0 {
		return
	}
	for ref, row := range resolveMediaIdentityIDs(env, missed) {
		rows[ref] = row
	}
}

// resolvedMediaRefs flattens resolved rows into media IDs keyed by path ref
// and a MediaRef list deduplicated by media DBID in entry order, ready for
// batch cover-status and tag lookups.
func resolvedMediaRefs(
	order []mediaPathRef, rows map[mediaPathRef]database.MediaPathID,
) (map[mediaPathRef]int64, []database.MediaRef) {
	mediaIDs := make(map[mediaPathRef]int64, len(rows))
	refs := make([]database.MediaRef, 0, len(rows))
	seen := make(map[int64]struct{}, len(rows))
	for _, ref := range order {
		row := rows[ref]
		if row.DBID <= 0 {
			continue
		}
		mediaIDs[ref] = row.DBID
		if _, ok := seen[row.DBID]; ok {
			continue
		}
		seen[row.DBID] = struct{}{}
		refs = append(refs, database.MediaRef{
			MediaDBID:      row.DBID,
			MediaTitleDBID: row.MediaTitleDBID,
		})
	}
	return mediaIDs, refs
}

// mediaTagsByRefs batch-loads tags for resolved media rows. The bool reports
// whether tags are known: false means the lookup failed, so callers must omit
// tags rather than report the media as untagged.
func mediaTagsByRefs(
	ctx context.Context, db database.MediaDBI, refs []database.MediaRef,
) (map[int64][]database.TagInfo, bool) {
	if db == nil || len(refs) == 0 {
		return map[int64][]database.TagInfo{}, true
	}
	started := time.Now()
	tags, err := db.GetMediaTagsByMediaRefs(ctx, refs)
	if err != nil {
		log.Debug().Err(err).Msg("could not resolve media tags by ref")
		return nil, false
	}
	log.Debug().
		Int("refs", len(refs)).
		Int("tagged", len(tags)).
		Dur("duration", time.Since(started)).
		Msg("media tag enrichment timing")
	return tags, true
}

// mediaTitleZapScripts batch-loads the ZapScript title command for resolved
// media rows. It is optional enrichment: a failure returns no scripts, so
// callers omit the field rather than fail the page.
func mediaTitleZapScripts(
	ctx context.Context, db database.MediaDBI, refs []database.MediaRef,
) map[int64]string {
	if db == nil || len(refs) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(refs))
	for _, ref := range refs {
		ids = append(ids, ref.MediaDBID)
	}
	scripts, err := db.GetTitleZapScriptsByMediaDBIDs(ctx, ids)
	if err != nil {
		log.Debug().Err(err).Msg("could not resolve media zapscripts")
		return nil
	}
	return scripts
}

// mediaEntryTags returns the tags for one response entry: nil (omitted from
// JSON) when the media is unresolved or tags are unknown, and an empty slice
// when the media is indexed but untagged.
func mediaEntryTags(known bool, mediaID int64, tags map[int64][]database.TagInfo) []database.TagInfo {
	if !known || mediaID <= 0 {
		return nil
	}
	if entryTags := tags[mediaID]; entryTags != nil {
		return entryTags
	}
	return []database.TagInfo{}
}
