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

package kiosk

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"strings"

	gozapscript "github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/container"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/filters"
	mediatags "github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/pathutil"
	"github.com/rs/zerolog/log"
)

// Reasons a launch is refused, as sent to clients.
const (
	ReasonHidden     = "hidden"
	ReasonDenied     = "denied"
	ReasonNotAllowed = "notAllowed"
	ReasonDownload   = "download"
)

// ErrLaunchRefused is the error every content refusal wraps.
var ErrLaunchRefused = errors.New("launch is not allowed while the device is locked")

// RefusedError is a launch the locked device's content rule turned away.
type RefusedError struct {
	SystemID  string
	Path      string
	ProfileID string
	Reason    string
}

func (e *RefusedError) Error() string {
	return fmt.Sprintf("%s: %s", ErrLaunchRefused.Error(), e.Reason)
}

func (*RefusedError) Unwrap() error { return ErrLaunchRefused }

// CheckLaunch decides whether profileID, the empty string being the shared
// profile, may launch path in systemID right now. It is nil whenever the
// lock is not enforcing. path is empty for a launch of the system itself.
//
// final marks the check made just before the launch itself. A one-time
// approval is used up only then, so the earlier check a confirmation makes
// does not spend it.
//
// It fails closed: anything that cannot be matched, read or looked up is
// refused under an allowed-only rule, and a deny that cannot be evaluated
// never becomes an allow.
func (s *Service) CheckLaunch(ctx context.Context, profileID, systemID, path string, final bool) error {
	if !s.Locked() {
		return nil
	}
	canonical := ""
	if path != "" {
		canonical = pathutil.CanonicalMediaPath(path)
	}
	refuse := func(reason string) error {
		return &RefusedError{SystemID: systemID, Path: path, ProfileID: profileID, Reason: reason}
	}

	if canonical != "" && s.approved(systemID, canonical, final) {
		return nil
	}

	rule, err := s.db.UserDB.GetContentRule(profileID)
	if err != nil {
		log.Error().Err(err).Msg("kiosk: failed to read content rule, refusing launch")
		return refuse(ReasonNotAllowed)
	}
	mode := rule.Mode
	if mode == "" {
		mode = s.Status().Settings.DefaultContentMode
	}

	target := &launchTarget{svc: s, ctx: ctx, systemID: systemID, path: canonical}

	if canonical != "" && systemID != "" {
		data, found, dataErr := s.db.UserDB.GetMediaUserData(systemID, canonical)
		if dataErr != nil {
			log.Error().Err(dataErr).Msg("kiosk: failed to read media flags, refusing launch")
			return refuse(ReasonHidden)
		}
		if found && data.IsHidden {
			return refuse(ReasonHidden)
		}
	}

	for i := range rule.Entries {
		entry := &rule.Entries[i]
		if entry.Effect != database.ContentEffectDeny {
			continue
		}
		matched, matchErr := target.matches(entry)
		if matchErr != nil {
			// A deny that cannot be looked up may well apply. Letting the
			// launch through would turn a failed lookup into an allow.
			log.Error().Err(matchErr).Str("kind", entry.Kind).
				Msg("kiosk: failed to evaluate deny entry, refusing launch")
			return refuse(ReasonDenied)
		}
		if matched {
			return refuse(ReasonDenied)
		}
	}
	if mode != database.ContentModeAllowed {
		return nil
	}
	for i := range rule.Entries {
		entry := &rule.Entries[i]
		if entry.Effect != database.ContentEffectAllow {
			continue
		}
		matched, matchErr := target.matches(entry)
		if matchErr != nil {
			// An allow that cannot be looked up allows nothing.
			log.Error().Err(matchErr).Str("kind", entry.Kind).Msg("kiosk: failed to evaluate allow entry")
			continue
		}
		if matched {
			return nil
		}
	}
	return refuse(ReasonNotAllowed)
}

// SystemScope is how far a profile's content rule can be expressed as a set
// of systems, for narrowing a browse, a search or a random pick before it
// runs. It is a convenience for what is shown; only CheckLaunch decides a
// launch.
type SystemScope struct {
	// Allowed lists the only systems open to the profile. It is nil when
	// the rule does not limit systems to a list: every system is open
	// except Denied.
	Allowed []string
	// Denied lists systems closed to the profile whatever else allows.
	Denied []string
}

// Limited reports whether the scope narrows anything.
func (s *SystemScope) Limited() bool {
	return s.Allowed != nil || len(s.Denied) > 0
}

// Permits reports whether systemID is inside the scope.
func (s *SystemScope) Permits(systemID string) bool {
	if containsFold(s.Denied, systemID) {
		return false
	}
	return s.Allowed == nil || containsFold(s.Allowed, systemID)
}

// SystemScope returns the system scope of a profile's rule while the lock
// is enforcing, and an unlimited scope otherwise.
//
// An allowed-only rule narrows to the systems its system and media entries
// name. A rule that also allows by deck or by tag cannot be narrowed that
// way, since those reach into any system, so it leaves every system open
// here and relies on the launch check.
func (s *Service) SystemScope(profileID string) SystemScope {
	scope := SystemScope{}
	if !s.Locked() {
		return scope
	}
	rule, err := s.db.UserDB.GetContentRule(profileID)
	if err != nil {
		log.Error().Err(err).Msg("kiosk: failed to read content rule, exposing no systems")
		scope.Allowed = []string{}
		return scope
	}
	mode := rule.Mode
	if mode == "" {
		mode = s.Status().Settings.DefaultContentMode
	}
	for i := range rule.Entries {
		entry := &rule.Entries[i]
		if entry.Effect == database.ContentEffectDeny && entry.Kind == database.ContentKindSystem {
			scope.Denied = append(scope.Denied, entry.SystemID)
		}
	}
	if mode != database.ContentModeAllowed {
		return scope
	}
	allowed := []string{}
	for i := range rule.Entries {
		entry := &rule.Entries[i]
		if entry.Effect != database.ContentEffectAllow {
			continue
		}
		switch entry.Kind {
		case database.ContentKindSystem, database.ContentKindMedia:
			// A media entry opens its system for browsing too: the system
			// has to be reachable for the one game in it to be found.
			if entry.SystemID != "" && !containsFold(allowed, entry.SystemID) {
				allowed = append(allowed, entry.SystemID)
			}
		default:
			return scope
		}
	}
	scope.Allowed = allowed
	return scope
}

func containsFold(list []string, want string) bool {
	for _, item := range list {
		if strings.EqualFold(item, want) {
			return true
		}
	}
	return false
}

// launchTarget is one launch being judged. The deck and tag lookups cost a
// query each, so they are made at most once and only when an entry asks.
type launchTarget struct {
	ctx            context.Context
	svc            *Service
	decksErr       error
	tagsErr        error
	userTagsErr    error
	decks          map[string][]int64
	systemID       string
	path           string
	tags           []database.TagInfo
	userTags       []database.TagInfo
	decksLoaded    bool
	tagsLoaded     bool
	userTagsLoaded bool
}

// matches reports whether an entry covers the launch. The error is a lookup
// that failed, which is not the same as a lookup that found nothing: the
// caller decides which way a rule it could not read has to fall.
func (t *launchTarget) matches(entry *database.ContentRuleEntry) (bool, error) {
	switch entry.Kind {
	case database.ContentKindSystem:
		return t.systemID != "" && strings.EqualFold(entry.SystemID, t.systemID), nil
	case database.ContentKindMedia:
		return t.path != "" && t.systemID != "" &&
			strings.EqualFold(entry.SystemID, t.systemID) &&
			samePath(pathutil.CanonicalMediaPath(entry.Path), t.path), nil
	case database.ContentKindDeck:
		return t.inDeck(entry.Value)
	case database.ContentKindTags:
		return t.hasTags(entry.Value, entry.Effect == database.ContentEffectDeny)
	default:
		// A kind this build does not know matches nothing: an allow entry
		// written by a newer build opens nothing here, which is the safe
		// reading of a rule that cannot be understood.
		return false, nil
	}
}

func samePath(a, b string) bool {
	if runtime.GOOS == "windows" && !strings.Contains(a, "://") {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func (t *launchTarget) inDeck(deckID string) (bool, error) {
	if t.path == "" || t.systemID == "" || deckID == "" {
		return false, nil
	}
	if !t.decksLoaded {
		t.decksLoaded = true
		t.decks, t.decksErr = t.svc.db.UserDB.FindDeckItemsByAnchor(t.systemID, t.path)
	}
	if t.decksErr != nil {
		return false, fmt.Errorf("look up deck membership: %w", t.decksErr)
	}
	return len(t.decks[deckID]) > 0, nil
}

// errNoIndex reports a tag entry met with no media index to read tags from.
var errNoIndex = errors.New("media index is not available")

// hasTags reports whether the media carries the tag filter. For an allow
// entry, media with no indexed or user tags matches no filter, including one
// made only of exclusions, so a tag entry never opens what the index does
// not know. A deny entry is judged on the tags as they are, so an
// exclusion-only deny covers untagged media as it covers tagged media. A
// lookup that fails is reported as such.
func (t *launchTarget) hasTags(value string, deny bool) (bool, error) {
	if t.path == "" || t.systemID == "" {
		return false, nil
	}
	tagFilters, err := filters.ParseTagFilters(splitTagList(value))
	if err != nil {
		// An entry whose filter cannot be read cannot be evaluated either.
		return false, fmt.Errorf("parse tag filter: %w", err)
	}
	if len(tagFilters) == 0 {
		return false, nil
	}
	if !t.tagsLoaded {
		t.tagsLoaded = true
		if t.svc.db.MediaDB == nil {
			t.tagsErr = errNoIndex
		} else {
			t.tags, t.tagsErr = t.svc.db.MediaDB.GetZapScriptTagsBySystemAndPath(t.ctx, t.systemID, t.path)
		}
	}
	if t.tagsErr != nil {
		return false, fmt.Errorf("look up media tags: %w", t.tagsErr)
	}
	mediaTags := t.tags
	if slices.ContainsFunc(tagFilters, func(f gozapscript.TagFilter) bool {
		return strings.EqualFold(f.Type, string(mediatags.TagTypeUser))
	}) {
		if !t.userTagsLoaded {
			t.userTagsLoaded = true
			t.userTags, t.userTagsErr = t.loadUserTags()
		}
		if t.userTagsErr != nil {
			return false, fmt.Errorf("look up user flags: %w", t.userTagsErr)
		}
		mediaTags = append(slices.Clip(mediaTags), t.userTags...)
	}
	if len(mediaTags) == 0 && !deny {
		return false, nil
	}
	return tagsSatisfy(mediaTags, tagFilters), nil
}

// loadUserTags returns the user flags of indexed media as tags. They live in
// UserDB, not among the tags a title is told apart by. A disc of a
// multi-disc folder also carries the flags its sibling discs hold, since the
// folder is one game, shown as the folder shows them; hidden stays the disc's
// own.
func (t *launchTarget) loadUserTags() ([]database.TagInfo, error) {
	mediaDB := t.svc.db.MediaDB
	if mediaDB == nil {
		return nil, errNoIndex
	}
	system, err := mediaDB.FindSystemBySystemID(t.systemID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find system: %w", err)
	}
	indexed, err := mediaDB.FindMediaBySystemAndPaths(t.ctx, system.DBID, []string{t.path})
	if err != nil {
		return nil, fmt.Errorf("find media: %w", err)
	}
	if _, ok := indexed[t.path]; !ok {
		return nil, nil
	}
	own := database.MediaUserRef{SystemID: system.SystemID, Path: t.path}
	refs := []database.MediaUserRef{own}
	if container.MayHaveContainerTarget(t.path) {
		dir := container.ParentDir(t.path)
		sets, setErr := mediaDB.FindDiscSets(t.ctx, system.DBID, []string{dir})
		if setErr != nil {
			return nil, fmt.Errorf("find disc set: %w", setErr)
		}
		discs := sets[dir]
		if slices.ContainsFunc(discs, func(m database.Media) bool { return m.Path == t.path }) {
			refs = refs[:0]
			for i := range discs {
				refs = append(refs, database.MediaUserRef{SystemID: system.SystemID, Path: discs[i].Path})
			}
		}
	}
	data, err := t.svc.db.UserDB.GetMediaUserDataForPaths(refs)
	if err != nil {
		return nil, fmt.Errorf("read media user data: %w", err)
	}
	flagTags := func(ref database.MediaUserRef) []database.TagInfo {
		row, ok := data[ref]
		if !ok {
			return nil
		}
		var list []database.TagInfo
		for _, flag := range database.MediaUserFlags {
			if row.Flag(flag) {
				list = append(list, database.TagInfo{Type: string(mediatags.TagTypeUser), Tag: string(flag)})
			}
		}
		return list
	}
	out := flagTags(own)
	if len(refs) == 1 {
		return out, nil
	}
	siblings := make([][]database.TagInfo, 0, len(refs))
	for _, ref := range refs {
		if ref != own {
			siblings = append(siblings, flagTags(ref))
		}
	}
	out = database.MergeDiscSetUserTags(out, siblings...)
	return out, nil
}

func splitTagList(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func tagsSatisfy(tags []database.TagInfo, tagFilters []gozapscript.TagFilter) bool {
	has := func(f gozapscript.TagFilter) bool {
		for i := range tags {
			if strings.EqualFold(tags[i].Type, f.Type) && strings.EqualFold(tags[i].Tag, f.Value) {
				return true
			}
		}
		return false
	}
	anyOf, anyOfMet := false, false
	for _, f := range tagFilters {
		switch f.Operator {
		case gozapscript.TagOperatorNOT:
			if has(f) {
				return false
			}
		case gozapscript.TagOperatorOR:
			anyOf = true
			if has(f) {
				anyOfMet = true
			}
		case gozapscript.TagOperatorAND:
			if !has(f) {
				return false
			}
		}
	}
	return !anyOf || anyOfMet
}
