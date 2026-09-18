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

package librarysync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/backup"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript/titles"
	"github.com/rs/zerolog/log"
)

const (
	// DeviceStateKeyStateSince is the pull cursor for personal state.
	DeviceStateKeyStateSince = "library_state_since"
	// DeviceStateKeyStateLink records the endpoint and device link the
	// personal state bookkeeping belongs to.
	DeviceStateKeyStateLink = "library_state_link"
	// DeviceStateKeyStateMatchGeneration is the index generation the last
	// attempt to find copies of unmatched games ran against.
	DeviceStateKeyStateMatchGeneration = "library_state_match_generation"

	statePushBatch     = 500
	statePullLimit     = 500
	statePullMaxPages  = 200
	statePushMaxRounds = 3
	maxStateTags       = 64
	maxStateTagPart    = 128
	maxStateTitle      = 512

	statusApplied  = "applied"
	statusConflict = "conflict"
	statusRejected = "rejected"
)

// StateResult summarizes one personal state pass.
type StateResult struct {
	Pulled    int
	Pushed    int
	Conflicts int
	Rejected  int
}

// stateFields is the synced part of one game's personal state.
type stateFields struct {
	Intent   string
	Reaction string
	Favorite bool
}

var defaultStateFields = stateFields{Intent: database.LibraryIntentNone, Reaction: database.LibraryReactionNone}

func (f stateFields) isDefault() bool {
	return f == defaultStateFields
}

// fieldMask names the fields of a game's state a pull is allowed to touch on
// this device's copies. Fields outside the mask are the device's own business.
type fieldMask struct {
	Favorite bool
	Intent   bool
	Reaction bool
}

// adoptedFields is the mask of fields the account changed since the base and
// the merge adopted: a field the user changed here since the base keeps its
// local value, so applying it would be a no-op at best and a revert at worst.
func adoptedFields(held, server, target stateFields) fieldMask {
	return fieldMask{
		Favorite: server.Favorite != held.Favorite && target.Favorite == server.Favorite,
		Intent:   server.Intent != held.Intent && target.Intent == server.Intent,
		Reaction: server.Reaction != held.Reaction && target.Reaction == server.Reaction,
	}
}

// presentFields is the mask of fields that hold a value, for a row the
// device has never applied to a copy: nothing is turned off, only added.
func presentFields(fields stateFields) fieldMask {
	return fieldMask{
		Favorite: fields.Favorite,
		Intent:   fields.Intent != database.LibraryIntentNone,
		Reaction: fields.Reaction != database.LibraryReactionNone,
	}
}

// titleIdentity names a game the way both this device and the account do.
type titleIdentity struct {
	MediaType string
	SystemID  string
	CoreSlug  string
	Variants  []string
}

func newTitleIdentity(mediaType, systemID, coreSlug string, variants []string) titleIdentity {
	sorted := append([]string{}, variants...)
	sort.Strings(sorted)
	return titleIdentity{MediaType: mediaType, SystemID: systemID, CoreSlug: coreSlug, Variants: sorted}
}

func (id *titleIdentity) key() string {
	return id.MediaType + "\x1f" + id.SystemID + "\x1f" + id.CoreSlug + "\x1f" + strings.Join(id.Variants, "\x1e")
}

// localGame is every user data row this device holds for one title identity.
type localGame struct {
	identity titleIdentity
	title    string
	rows     []database.MediaUserData
}

// fields rolls the per-file flags up to the game: a game is a favorite, on
// the play-later list or liked when any copy is, and disliked only when no
// copy is liked or a favorite.
func (g *localGame) fields() stateFields {
	fields := defaultStateFields
	liked, disliked := false, false
	for i := range g.rows {
		row := &g.rows[i]
		fields.Favorite = fields.Favorite || row.IsFavorite
		if row.IsPlayLater {
			fields.Intent = database.LibraryIntentPlayLater
		}
		liked = liked || row.IsLiked
		disliked = disliked || row.IsDisliked
	}
	switch {
	case liked:
		fields.Reaction = database.LibraryReactionLiked
	case disliked && !fields.Favorite:
		fields.Reaction = database.LibraryReactionDisliked
	}
	return fields
}

// preferredTags returns the version tags of the one starred copy, or nil
// when no single copy is starred and the device has no opinion.
func (g *localGame) preferredTags() []string {
	var starred *database.MediaUserData
	for i := range g.rows {
		if !g.rows[i].IsFavorite {
			continue
		}
		if starred != nil {
			return nil
		}
		starred = &g.rows[i]
	}
	if starred == nil {
		return nil
	}
	return versionTags(starred.Tags)
}

func baseFields(base *database.LibraryStateSyncRow) stateFields {
	if base == nil || base.Deleted {
		return defaultStateFields
	}
	return stateFields{Favorite: base.Favorite, Intent: base.Intent, Reaction: base.Reaction}
}

func knownIntent(intent string) bool {
	return intent == database.LibraryIntentNone || intent == database.LibraryIntentPlayLater
}

func knownReaction(reaction string) bool {
	return reaction == database.LibraryReactionNone || reaction == database.LibraryReactionLiked ||
		reaction == database.LibraryReactionDisliked
}

// desiredFields is the state this device wants the account to hold for a
// game. A game the device holds no copy of keeps the account's state, with
// only what the user set here layered on top; a value this Core does not
// know is kept unless the user set a value of their own.
func desiredFields(game *localGame, base *database.LibraryStateSyncRow) stateFields {
	local := defaultStateFields
	if game != nil {
		local = game.fields()
	}
	if base == nil {
		return local
	}
	held := baseFields(base)
	if base.Unmatched {
		desired := held
		if local.Favorite {
			desired.Favorite = true
		}
		if local.Intent != database.LibraryIntentNone {
			desired.Intent = local.Intent
		}
		if local.Reaction != database.LibraryReactionNone {
			desired.Reaction = local.Reaction
		}
		if local.Reaction == database.LibraryReactionDisliked {
			desired.Favorite = false
		} else if desired.Favorite && desired.Reaction == database.LibraryReactionDisliked {
			desired.Reaction = database.LibraryReactionNone
		}
		return desired
	}
	if !knownIntent(held.Intent) && local.Intent == database.LibraryIntentNone {
		local.Intent = held.Intent
	}
	if !knownReaction(held.Reaction) && local.Reaction == database.LibraryReactionNone {
		local.Reaction = held.Reaction
	}
	return local
}

// mergeFields keeps every field the device changed since the base and takes
// the account's value for the rest, then resolves the one forbidden pair in
// favor of the side that changed.
func mergeFields(desired, base, server stateFields) stateFields {
	merged := server
	favoriteChanged := desired.Favorite != base.Favorite
	reactionChanged := desired.Reaction != base.Reaction
	if favoriteChanged {
		merged.Favorite = desired.Favorite
	}
	if desired.Intent != base.Intent {
		merged.Intent = desired.Intent
	}
	if reactionChanged {
		merged.Reaction = desired.Reaction
	}
	if merged.Favorite && merged.Reaction == database.LibraryReactionDisliked {
		if reactionChanged && !favoriteChanged {
			merged.Favorite = false
		} else {
			merged.Reaction = database.LibraryReactionNone
		}
	}
	return merged
}

// versionTags returns the identity tags that tell versions of one game apart
// (region, language, revision and the like), bounded to what the account
// accepts.
func versionTags(typeValues []string) []string {
	out := make([]string, 0, len(typeValues))
	seen := make(map[string]struct{}, len(typeValues))
	for _, tv := range typeValues {
		tagType, value, found := strings.Cut(tv, ":")
		if !found || tagType == "" || value == "" || tags.IsGameVariantTag(tagType, value) ||
			utf8.RuneCountInString(tagType) > maxStateTagPart || utf8.RuneCountInString(value) > maxStateTagPart {
			continue
		}
		role, err := database.MediaIdentityTagRoleForPolicy(database.CurrentMediaIdentityPolicyVersion, tagType)
		if err != nil || role != database.MediaIdentityTagRoleIdentity {
			continue
		}
		if _, dup := seen[tv]; dup {
			continue
		}
		seen[tv] = struct{}{}
		out = append(out, tv)
	}
	sort.Strings(out)
	if len(out) > maxStateTags {
		out = out[:maxStateTags]
	}
	return out
}

func containsAll(have, want []string) bool {
	set := make(map[string]struct{}, len(have))
	for _, tag := range have {
		set[tag] = struct{}{}
	}
	for _, tag := range want {
		if _, ok := set[tag]; !ok {
			return false
		}
	}
	return true
}

func syncedSystemMediaType(systemID string) (slugs.MediaType, bool) {
	system, err := systemdefs.GetSystem(systemID)
	if err != nil {
		return "", false
	}
	mediaType := system.GetMediaType()
	return mediaType, syncedMediaTypes[mediaType]
}

// SyncState converges personal state with the account: it pulls what
// changed there first, so local bases are current, then pushes what changed
// here.
func (s *Service) SyncState(ctx context.Context) (StateResult, error) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()

	if !s.cfg.LibrarySyncEnabled() {
		return StateResult{}, ErrDisabled
	}
	// A half-built index cannot say which copies of a game this device has,
	// so a pulled flag would land on whichever copy happened to be indexed
	// already and stay there: flags are never moved afterwards. Wait for the
	// index instead. Local edits are already recorded; only their trip to the
	// account waits.
	if !mediaDBSettled(s.db.MediaDB) {
		return StateResult{}, ErrNotSettled
	}
	client, err := s.client()
	if err != nil {
		return StateResult{}, err
	}
	pass := &statePass{svc: s, client: client, changed: make(map[string]bool)}
	if err := pass.checkLink(); err != nil {
		return StateResult{}, err
	}
	if err := pass.loadBases(); err != nil {
		return StateResult{}, err
	}
	if err := pass.loadGames(ctx); err != nil {
		return StateResult{}, err
	}
	if err := pass.rematchUnmatched(ctx); err != nil {
		return pass.result, err
	}
	if err := pass.pull(ctx); err != nil {
		return pass.result, err
	}
	if err := pass.push(ctx); err != nil {
		return pass.result, err
	}
	if pass.result.Pulled > 0 || pass.result.Pushed > 0 || pass.result.Conflicts > 0 {
		log.Info().Int("pulled", pass.result.Pulled).Int("pushed", pass.result.Pushed).
			Int("conflicts", pass.result.Conflicts).Int("rejected", pass.result.Rejected).
			Msg("library state synced")
	}
	return pass.result, nil
}

// statePass holds one pass's view of the bookkeeping and local rows.
type statePass struct {
	svc     *Service
	client  *backup.OnlineClient
	bases   map[string]*database.LibraryStateSyncRow
	games   map[string]*localGame
	changed map[string]bool
	// unresolved remembers the rows this pass could not name from the index,
	// so a row whose file is gone is not looked up again on every reload.
	unresolved map[string]struct{}
	result     StateResult
}

func (p *statePass) userDB() database.UserDBI {
	return p.svc.db.UserDB
}

// checkLink drops the bookkeeping when the device was linked again or
// pointed at another server since it was written, since its bases and
// cursor describe an account this link may not be.
func (p *statePass) checkLink() error {
	link := p.client.BaseURL() + "#" + p.client.CredentialTag()
	stored, found, err := p.userDB().GetDeviceState(DeviceStateKeyStateLink)
	if err != nil {
		return fmt.Errorf("read library state link: %w", err)
	}
	if found && stored == link {
		return nil
	}
	if err := p.userDB().ClearLibraryStateSync(); err != nil {
		return fmt.Errorf("clear library state bookkeeping: %w", err)
	}
	if err := p.userDB().SetDeviceState(DeviceStateKeyStateSince, "0"); err != nil {
		return fmt.Errorf("reset library state cursor: %w", err)
	}
	if err := p.userDB().SetDeviceState(DeviceStateKeyStateLink, link); err != nil {
		return fmt.Errorf("store library state link: %w", err)
	}
	return nil
}

func (p *statePass) loadBases() error {
	rows, err := p.userDB().ListLibraryStateSync()
	if err != nil {
		return fmt.Errorf("read library state bookkeeping: %w", err)
	}
	p.bases = make(map[string]*database.LibraryStateSyncRow, len(rows))
	for i := range rows {
		p.bases[rows[i].IdentityKey] = &rows[i]
	}
	return nil
}

// loadGames groups this device's user data rows by title identity. Rows
// holding only local preferences (hidden, a launcher choice) never sync, and
// a row without an identity snapshot takes one from the index first.
func (p *statePass) loadGames(ctx context.Context) error {
	rows, err := p.userDB().ListMediaUserData()
	if err != nil {
		return fmt.Errorf("read media user data: %w", err)
	}
	p.games = make(map[string]*localGame)
	for i := range rows {
		row := rows[i]
		if !row.IsFavorite && !row.IsLiked && !row.IsDisliked && !row.IsPlayLater {
			continue
		}
		mediaType, synced := syncedSystemMediaType(row.SystemID)
		if !synced {
			continue
		}
		if row.Slug == "" && !p.fillSnapshot(ctx, &row) {
			continue
		}
		identity := newTitleIdentity(string(mediaType), row.SystemID, row.Slug, tags.GameVariantTagStrings(row.Tags))
		key := identity.key()
		game, ok := p.games[key]
		if !ok {
			game = &localGame{identity: identity}
			p.games[key] = game
		}
		if game.title == "" || row.IsFavorite {
			game.title = row.MediaName
		}
		game.rows = append(game.rows, row)
	}
	return nil
}

func (p *statePass) fillSnapshot(ctx context.Context, row *database.MediaUserData) bool {
	key := row.SystemID + "\x00" + row.Path
	if p.unresolved == nil {
		p.unresolved = make(map[string]struct{})
	}
	if _, skip := p.unresolved[key]; skip {
		return false
	}
	identity, found, err := database.LookupMediaIdentity(ctx, p.svc.db.MediaDB, row.SystemID, row.Path)
	if err != nil || !found {
		p.unresolved[key] = struct{}{}
		return false
	}
	tagStrings := identity.LegacyTags()
	if err := p.userDB().SetMediaUserSnapshot(
		row.SystemID, row.Path, identity.DisplayName, identity.CoreSlug, tagStrings,
	); err != nil {
		log.Debug().Err(err).Str("path", row.Path).Msg("failed to store media user identity snapshot")
	}
	row.MediaName = identity.DisplayName
	row.Slug = identity.CoreSlug
	row.Tags = tagStrings
	return true
}

// rematchUnmatched looks again for copies of games the account holds state
// for but this device held no copy of, once the index has changed.
func (p *statePass) rematchUnmatched(ctx context.Context) error {
	generation, err := p.svc.db.MediaDB.IndexGeneration()
	if err != nil || !mediaDBSettled(p.svc.db.MediaDB) {
		return nil //nolint:nilerr // an unreadable or busy index is retried next pass
	}
	value := strconv.FormatInt(generation, 10)
	stored, _, err := p.userDB().GetDeviceState(DeviceStateKeyStateMatchGeneration)
	if err != nil {
		return fmt.Errorf("read library state match generation: %w", err)
	}
	if stored == value {
		return nil
	}
	for key, base := range p.bases {
		if !base.Unmatched {
			continue
		}
		identity := newTitleIdentity(base.MediaType, base.SystemID, base.CoreSlug, base.VariantTags)
		desired := desiredFields(p.games[key], base)
		matched, starred, applyErr := p.applyDelta(
			ctx, &identity, presentFields(desired), desired, base.PreferredTags)
		if applyErr != nil {
			return applyErr
		}
		if matched {
			base.Unmatched = false
			if starred != nil {
				base.SentPreferredTags = starred
			}
			p.changed[key] = true
		}
	}
	if err := p.flush(ctx); err != nil {
		return err
	}
	if err := p.userDB().SetDeviceState(DeviceStateKeyStateMatchGeneration, value); err != nil {
		return fmt.Errorf("store library state match generation: %w", err)
	}
	return nil
}

// pull applies every row that changed on the account since the cursor.
func (p *statePass) pull(ctx context.Context) error {
	raw, _, err := p.userDB().GetDeviceState(DeviceStateKeyStateSince)
	if err != nil {
		return fmt.Errorf("read library state cursor: %w", err)
	}
	since, _ := strconv.ParseInt(raw, 10, 64)
	for range statePullMaxPages {
		var page statePullResponse
		query := url.Values{}
		query.Set("since", strconv.FormatInt(since, 10))
		query.Set("limit", strconv.Itoa(statePullLimit))
		err := p.client.RetryRateLimited(ctx, func() error {
			page = statePullResponse{}
			return p.client.DoJSON(ctx, http.MethodGet, pathState+"?"+query.Encode(), nil, &page)
		})
		if err != nil {
			return fmt.Errorf("pull library state: %w", err)
		}
		if page.Reset {
			if resetErr := p.dropSyncedState(ctx); resetErr != nil {
				return resetErr
			}
		}
		for i := range page.Items {
			if applyErr := p.applyServerRow(ctx, &page.Items[i], false); applyErr != nil {
				return applyErr
			}
		}
		if err := p.flush(ctx); err != nil {
			return err
		}
		if page.NextSince > since || page.Reset {
			since = page.NextSince
			if err := p.userDB().SetDeviceState(DeviceStateKeyStateSince, strconv.FormatInt(since, 10)); err != nil {
				return fmt.Errorf("store library state cursor: %w", err)
			}
		}
		if !page.HasMore {
			return nil
		}
	}
	return nil
}

// dropSyncedState forgets the bookkeeping after the account erased its
// state. Nothing local is touched: the device's own flags are the user's
// data, and they push again as new rows on the next push.
func (p *statePass) dropSyncedState(ctx context.Context) error {
	log.Info().Msg("library state was erased on the account; forgetting sync bookkeeping, local state is kept")
	if err := p.userDB().ClearLibraryStateSync(); err != nil {
		return fmt.Errorf("clear library state bookkeeping: %w", err)
	}
	p.bases = make(map[string]*database.LibraryStateSyncRow)
	p.changed = make(map[string]bool)
	return p.loadGames(ctx)
}

// applyServerRow adopts one row from the account, keeping any field this
// device changed since its base, and applies only the fields the account
// changed to this device's copies. conflict marks a row returned for a
// failed push, which is applied whatever its revision.
func (p *statePass) applyServerRow(ctx context.Context, row *stateRow, conflict bool) error {
	identity := newTitleIdentity(row.MediaType, row.SystemID, row.CoreSlug, row.VariantTags)
	key := identity.key()
	base := p.bases[key]
	if !conflict && base != nil && base.Revision >= row.Revision {
		return nil
	}
	if !conflict {
		p.result.Pulled++
	}
	game := p.games[key]
	server := row.fields()
	if base == nil && game == nil && server.isDefault() {
		// A cleared game this device never held needs no bookkeeping.
		return nil
	}
	held := baseFields(base)
	desired := desiredFields(game, base)
	target := mergeFields(desired, held, server)

	matched := false
	var starred []string
	if game != nil || !target.isDefault() {
		var err error
		matched, starred, err = p.applyDelta(
			ctx, &identity, adoptedFields(held, server, target), target, row.PreferredTags)
		if err != nil {
			return err
		}
	}
	next := row.bookkeeping(&identity)
	next.Unmatched = !matched && !server.isDefault()
	switch {
	case starred != nil:
		next.SentPreferredTags = starred
	case base != nil:
		next.SentPreferredTags = base.SentPreferredTags
	}
	p.bases[key] = next
	p.changed[key] = true
	return nil
}

// flush writes changed bookkeeping rows and rebuilds the local view.
func (p *statePass) flush(ctx context.Context) error {
	if len(p.changed) == 0 {
		return nil
	}
	upserts := make([]database.LibraryStateSyncRow, 0, len(p.changed))
	deletes := make([]string, 0)
	for key := range p.changed {
		if base, ok := p.bases[key]; ok {
			upserts = append(upserts, *base)
		} else {
			deletes = append(deletes, key)
		}
	}
	if err := p.userDB().UpsertLibraryStateSync(upserts); err != nil {
		return fmt.Errorf("store library state bookkeeping: %w", err)
	}
	if err := p.userDB().DeleteLibraryStateSync(deletes); err != nil {
		return fmt.Errorf("remove library state bookkeeping: %w", err)
	}
	p.changed = make(map[string]bool)
	return p.loadGames(ctx)
}

// push sends every game whose desired state differs from its base, and
// resolves conflicts by merging and retrying.
func (p *statePass) push(ctx context.Context) error {
	for range statePushMaxRounds {
		items, keys := p.dirtyItems()
		if len(items) == 0 {
			return nil
		}
		retry := false
		for start := 0; start < len(items); start += statePushBatch {
			end := min(start+statePushBatch, len(items))
			var response statePushResponse
			request := statePushRequest{Items: items[start:end]}
			err := p.client.RetryRateLimited(ctx, func() error {
				response = statePushResponse{}
				return p.client.DoJSON(ctx, http.MethodPost, pathState, &request, &response)
			})
			if err != nil {
				return fmt.Errorf("push library state: %w", err)
			}
			again, handleErr := p.handlePushResults(ctx, items[start:end], keys[start:end], &response)
			if handleErr != nil {
				return handleErr
			}
			retry = retry || again
			if err := p.flush(ctx); err != nil {
				return err
			}
		}
		if !retry {
			return nil
		}
	}
	return nil
}

func (p *statePass) handlePushResults(
	ctx context.Context, items []statePushItem, keys []string, response *statePushResponse,
) (bool, error) {
	retry := false
	for _, result := range response.Items {
		if result.Index < 0 || result.Index >= len(items) {
			continue
		}
		key := keys[result.Index]
		item := &items[result.Index]
		switch result.Status {
		case statusApplied:
			p.result.Pushed++
			if result.State == nil {
				delete(p.bases, key)
			} else {
				identity := newTitleIdentity(item.MediaType, item.SystemID, item.CoreSlug, item.Tags)
				next := result.State.bookkeeping(&identity)
				next.Unmatched = p.games[key] == nil && p.bases[key] != nil && p.bases[key].Unmatched
				switch {
				case item.PreferredTags != nil:
					next.SentPreferredTags = append([]string{}, (*item.PreferredTags)...)
				case p.bases[key] != nil:
					next.SentPreferredTags = p.bases[key].SentPreferredTags
				}
				p.bases[key] = next
			}
			p.changed[key] = true
		case statusConflict:
			p.result.Conflicts++
			if result.State == nil {
				// The account erased this game's state. Local flags stay:
				// without a base the game pushes again as a new row.
				delete(p.bases, key)
				p.changed[key] = true
				retry = true
				continue
			}
			if err := p.applyServerRow(ctx, result.State, true); err != nil {
				return retry, err
			}
			retry = true
		case statusRejected:
			p.result.Rejected++
			base := p.bases[key]
			if base == nil {
				base = &database.LibraryStateSyncRow{
					IdentityKey: key, MediaType: item.MediaType, SystemID: item.SystemID, CoreSlug: item.CoreSlug,
					VariantTags: item.Tags, Intent: database.LibraryIntentNone, Reaction: database.LibraryReactionNone,
				}
				p.bases[key] = base
			}
			if base.RejectedHash != item.hash() {
				log.Warn().Str("system", item.SystemID).Str("game", item.CoreSlug).Str("code", result.Code).
					Msg("library state for a game was rejected")
			}
			base.RejectedCode = result.Code
			base.RejectedHash = item.hash()
			p.changed[key] = true
		}
	}
	return retry, nil
}

// dirtyItems builds a push item for every game whose desired state differs
// from its base, in a stable order.
func (p *statePass) dirtyItems() (items []statePushItem, itemKeys []string) {
	keys := make([]string, 0, len(p.games)+len(p.bases))
	seen := make(map[string]struct{}, len(p.games)+len(p.bases))
	for key := range p.games {
		keys = append(keys, key)
		seen[key] = struct{}{}
	}
	for key := range p.bases {
		if _, ok := seen[key]; !ok {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)

	items = make([]statePushItem, 0)
	itemKeys = make([]string, 0)
	for _, key := range keys {
		game := p.games[key]
		base := p.bases[key]
		desired := desiredFields(game, base)
		held := baseFields(base)
		// The starred copy names the version to launch. It is sent when the
		// game becomes a favorite and whenever the star moves to another
		// version while the game stays one. A version the account chose on
		// its own is left alone until the star moves, and a star that
		// already matches the account's choice has nothing to add.
		var preferred []string
		if game != nil && desired.Favorite {
			preferred = game.preferredTags()
		}
		preferredChanged := preferred != nil && (base == nil ||
			(!sameStrings(preferred, base.SentPreferredTags) && !sameStrings(preferred, base.PreferredTags)))
		if desired == held && !preferredChanged {
			continue
		}
		var identity titleIdentity
		title := ""
		if game != nil {
			identity = game.identity
			title = game.title
		} else {
			identity = newTitleIdentity(base.MediaType, base.SystemID, base.CoreSlug, base.VariantTags)
			title = base.Title
		}
		if len(identity.Variants) > maxStateTags {
			continue
		}
		item := statePushItem{
			MediaType: identity.MediaType,
			SystemID:  identity.SystemID,
			CoreSlug:  identity.CoreSlug,
			Title:     boundTitle(title),
			Tags:      identity.Variants,
		}
		if base != nil {
			item.BaseRevision = base.Revision
		}
		if desired.Favorite != held.Favorite {
			favorite := desired.Favorite
			item.Favorite = &favorite
		}
		if preferredChanged {
			item.PreferredTags = &preferred
		}
		if desired.Intent != held.Intent {
			item.Intent = desired.Intent
		}
		if desired.Reaction != held.Reaction {
			item.Reaction = desired.Reaction
		}
		if base != nil && base.RejectedHash != "" && base.RejectedHash == item.hash() {
			continue
		}
		items = append(items, item)
		itemKeys = append(itemKeys, key)
	}
	return items, itemKeys
}

// sameStrings reports whether two tag lists hold the same tags, in any order.
func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	sortedA := append([]string{}, a...)
	sortedB := append([]string{}, b...)
	sort.Strings(sortedA)
	sort.Strings(sortedB)
	for i := range sortedA {
		if sortedA[i] != sortedB[i] {
			return false
		}
	}
	return true
}

func boundTitle(title string) string {
	title = strings.TrimSpace(title)
	if utf8.RuneCountInString(title) <= maxStateTitle {
		return title
	}
	return string([]rune(title)[:maxStateTitle])
}

// copyRef is one file of a game on this device.
type copyRef struct {
	systemID string
	path     string
	name     string
	slug     string
	versions []string
	tags     []string
	// result is the indexed file as a title search sees it, for choosing
	// the copy a launch would pick. Zero for a file that is not indexed.
	result    database.SearchResultWithCursor
	current   database.MediaUserData
	mediaDBID int64
	hasRow    bool
}

func (ref *copyRef) has(flag database.MediaUserFlag) bool {
	return ref.hasRow && ref.current.Flag(flag)
}

// findCopies returns every copy of a game this device holds: indexed files
// with the same slug and exactly the same variant tags, and user data rows
// for the game whose files are not indexed right now.
func (p *statePass) findCopies(ctx context.Context, identity *titleIdentity) ([]copyRef, error) {
	mediaDB := p.svc.db.MediaDB
	key := identity.key()
	byPath := make(map[string]*copyRef)
	copies := make([]copyRef, 0)

	results, err := mediaDB.SearchMediaBySlug(ctx, identity.SystemID, identity.CoreSlug, nil)
	if err != nil {
		return nil, fmt.Errorf("find copies of %s/%s: %w", identity.SystemID, identity.CoreSlug, err)
	}
	if len(results) > 0 {
		ids := make([]int64, 0, len(results))
		for i := range results {
			ids = append(ids, results[i].MediaID)
		}
		tagsByID, tagErr := mediaDB.GetMediaTagsByMediaDBIDs(ctx, ids)
		if tagErr != nil {
			return nil, fmt.Errorf("read tags of %s/%s: %w", identity.SystemID, identity.CoreSlug, tagErr)
		}
		mediaType, _ := syncedSystemMediaType(identity.SystemID)
		for i := range results {
			observed, buildErr := database.BuildMediaIdentity(
				mediaType, results[i].SystemID, results[i].Name, identity.CoreSlug, tagsByID[results[i].MediaID],
			)
			if buildErr != nil {
				continue
			}
			tagStrings := observed.LegacyTags()
			candidate := newTitleIdentity(identity.MediaType, results[i].SystemID, observed.CoreSlug,
				tags.GameVariantTagStrings(tagStrings))
			if candidate.key() != key {
				continue
			}
			result := results[i]
			result.Tags = tagsByID[results[i].MediaID]
			copies = append(copies, copyRef{
				systemID: results[i].SystemID, path: results[i].Path, name: observed.DisplayName,
				slug: observed.CoreSlug, mediaDBID: results[i].MediaID, tags: tagStrings,
				versions: versionTags(tagStrings), result: result,
			})
		}
	}
	for i := range copies {
		byPath[copies[i].systemID+"\x00"+copies[i].path] = &copies[i]
	}
	if game := p.games[key]; game != nil {
		for i := range game.rows {
			row := game.rows[i]
			if existing, ok := byPath[row.SystemID+"\x00"+row.Path]; ok {
				existing.current = row
				existing.hasRow = true
				continue
			}
			copies = append(copies, copyRef{
				systemID: row.SystemID, path: row.Path, name: row.MediaName, slug: row.Slug, tags: row.Tags,
				versions: versionTags(row.Tags), current: row, hasRow: true,
			})
		}
	}
	return copies, nil
}

// applyDelta applies the masked fields of target to this device's copies of
// a game. A field turned off is cleared on every copy. A field turned on is
// left alone when any copy already holds it, and otherwise set on the copy
// this device would launch for the game. A flag a copy holds is never moved
// to another copy: the user put it there. It reports whether any copy
// exists and, when it starred one copy, that copy's version, so the star it
// placed is not mistaken for one the user moved.
func (p *statePass) applyDelta(
	ctx context.Context, identity *titleIdentity, mask fieldMask, target stateFields, preferred []string,
) (matched bool, starred []string, err error) {
	copies, err := p.findCopies(ctx, identity)
	if err != nil {
		return false, nil, err
	}
	if len(copies) == 0 {
		return false, nil, nil
	}
	off := make([]database.MediaUserFlag, 0, 4)
	on := make([]database.MediaUserFlag, 0, 3)
	if mask.Favorite {
		if target.Favorite {
			on = append(on, database.MediaUserFlagFavorite)
		} else {
			off = append(off, database.MediaUserFlagFavorite)
		}
	}
	if mask.Intent && knownIntent(target.Intent) {
		if target.Intent == database.LibraryIntentPlayLater {
			on = append(on, database.MediaUserFlagPlayLater)
		} else {
			off = append(off, database.MediaUserFlagPlayLater)
		}
	}
	if mask.Reaction && knownReaction(target.Reaction) {
		// The reaction is one value: the old one goes everywhere, the new
		// one is added.
		switch target.Reaction {
		case database.LibraryReactionLiked:
			off = append(off, database.MediaUserFlagDisliked)
			on = append(on, database.MediaUserFlagLiked)
		case database.LibraryReactionDisliked:
			off = append(off, database.MediaUserFlagLiked)
			on = append(on, database.MediaUserFlagDisliked)
		default:
			off = append(off, database.MediaUserFlagLiked, database.MediaUserFlagDisliked)
		}
	}

	changes := make([]map[database.MediaUserFlag]bool, len(copies))
	for i := range copies {
		changes[i] = make(map[database.MediaUserFlag]bool)
		for _, flag := range off {
			changes[i][flag] = false
		}
	}
	if len(on) > 0 {
		targets := p.launchCopies(identity.SystemID, copies, preferred)
		for _, flag := range on {
			held := false
			for i := range copies {
				if copies[i].has(flag) && !changes[i][flag] {
					held = true
					break
				}
			}
			if held {
				continue
			}
			for _, i := range targets {
				changes[i][flag] = true
			}
			if flag == database.MediaUserFlagFavorite && len(targets) == 1 {
				starred = copies[targets[0]].versions
			}
		}
	}
	for i := range copies {
		if err := p.applyToCopy(ctx, &copies[i], changes[i]); err != nil {
			return true, nil, err
		}
	}
	return true, starred, nil
}

// launchCopies returns the indexes of the copies a new flag goes on: the
// copies of the account's preferred version when this device holds any,
// narrowed to the one a title launch would pick when several remain. A game
// whose files are all missing from the index has no launch choice, so every
// copy takes the flag.
func (p *statePass) launchCopies(systemID string, copies []copyRef, preferred []string) []int {
	candidates := make([]int, 0, len(copies))
	for i := range copies {
		if copies[i].mediaDBID > 0 {
			candidates = append(candidates, i)
		}
	}
	if len(candidates) == 0 {
		for i := range copies {
			candidates = append(candidates, i)
		}
		return candidates
	}
	if len(preferred) > 0 {
		// A copy holding every preferred tag and more of its own, a
		// translation of the named release say, is a different version. Take
		// those only when the device holds no copy of the version itself,
		// rather than leaving the choice to the launch ranking, which has
		// nothing to separate interchangeable copies and can land on the
		// extra tags instead.
		matching := make([]int, 0, len(candidates))
		exact := make([]int, 0, len(candidates))
		for _, i := range candidates {
			if !containsAll(copies[i].versions, preferred) {
				continue
			}
			matching = append(matching, i)
			if len(copies[i].versions) == len(preferred) {
				exact = append(exact, i)
			}
		}
		switch {
		case len(exact) > 0:
			candidates = exact
		case len(matching) > 0:
			candidates = matching
		}
	}
	if len(candidates) == 1 {
		return candidates
	}
	results := make([]database.SearchResultWithCursor, 0, len(candidates))
	for _, i := range candidates {
		results = append(results, copies[i].result)
	}
	var launchers []platforms.Launcher
	if p.svc.launchers != nil {
		launchers = p.svc.launchers(systemID)
	}
	picked, _ := titles.SelectBestResult(results, nil, p.svc.cfg, 1, launchers)
	for _, i := range candidates {
		if picked.MediaID != 0 && copies[i].mediaDBID == picked.MediaID {
			return []int{i}
		}
	}
	return candidates[:1]
}

// applyToCopy sets the flags in changes on one copy, when they differ from
// what the copy holds. The write goes through ApplyMediaUserFlags, so it takes
// the same lock as an edit from the API and brings the file's browse tags in
// line with the row UserDB stores afterwards, forbidden-pair clears included:
// starring a copy clears its dislike, and the dislike tag goes with it.
func (p *statePass) applyToCopy(ctx context.Context, ref *copyRef, changes map[database.MediaUserFlag]bool) error {
	wanted := make(map[database.MediaUserFlag]bool, len(changes))
	for _, flag := range database.MediaUserFlags {
		value, ok := changes[flag]
		if !ok || ref.current.Flag(flag) == value {
			continue
		}
		wanted[flag] = value
	}
	if len(wanted) == 0 {
		return nil
	}
	if !ref.hasRow && ref.mediaDBID > 0 {
		// A new user data row takes the identity it was matched by, so it is
		// named the same way after the index is rebuilt.
		if err := p.userDB().SetMediaUserSnapshot(ref.systemID, ref.path, ref.name, ref.slug, ref.tags); err != nil {
			log.Debug().Err(err).Str("path", ref.path).Msg("failed to store media user identity snapshot")
		}
	}
	if _, err := database.ApplyMediaUserFlags(
		ctx, p.svc.db, ref.systemID, ref.path, ref.mediaDBID, wanted,
	); err != nil {
		return fmt.Errorf("apply synced state to %s: %w", ref.path, err)
	}
	return nil
}

// statePushRequest and the types below follow the personal state routes of
// the Library sync contract.
type statePushRequest struct {
	Items []statePushItem `json:"items"`
}

//nolint:tagliatelle // Wire shape follows the Zaparoo Online API contract.
type statePushItem struct {
	Favorite      *bool     `json:"favorite,omitempty"`
	PreferredTags *[]string `json:"preferred_tags,omitempty"`
	MediaType     string    `json:"media_type"`
	SystemID      string    `json:"system_id"`
	CoreSlug      string    `json:"core_slug"`
	Title         string    `json:"title,omitempty"`
	Intent        string    `json:"intent,omitempty"`
	Reaction      string    `json:"reaction,omitempty"`
	Tags          []string  `json:"tags"`
	BaseRevision  int64     `json:"base_revision"`
}

// hash identifies what an item asks for, without its base revision, so a
// rejected request is not repeated until the user changes something.
func (item *statePushItem) hash() string {
	copied := *item
	copied.BaseRevision = 0
	encoded, err := json.Marshal(&copied)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:8])
}

type statePushResponse struct {
	Items []statePushResult `json:"items"`
}

type statePushResult struct {
	State  *stateRow `json:"state"`
	Status string    `json:"status"`
	Code   string    `json:"code"`
	Index  int       `json:"index"`
}

//nolint:tagliatelle // Wire shape follows the Zaparoo Online API contract.
type statePullResponse struct {
	Items     []stateRow `json:"items"`
	NextSince int64      `json:"next_since"`
	HasMore   bool       `json:"has_more"`
	Reset     bool       `json:"reset"`
}

//nolint:tagliatelle // Wire shape follows the Zaparoo Online API contract.
type stateRow struct {
	Title         *string  `json:"title"`
	MediaType     string   `json:"media_type"`
	SystemID      string   `json:"system_id"`
	CoreSlug      string   `json:"core_slug"`
	Intent        string   `json:"intent"`
	Reaction      string   `json:"reaction"`
	VariantTags   []string `json:"variant_tags"`
	PreferredTags []string `json:"preferred_tags"`
	Revision      int64    `json:"revision"`
	Favorite      bool     `json:"favorite"`
	Deleted       bool     `json:"deleted"`
}

func (row *stateRow) fields() stateFields {
	if row.Deleted {
		return defaultStateFields
	}
	fields := stateFields{Favorite: row.Favorite, Intent: row.Intent, Reaction: row.Reaction}
	if fields.Intent == "" {
		fields.Intent = database.LibraryIntentNone
	}
	if fields.Reaction == "" {
		fields.Reaction = database.LibraryReactionNone
	}
	return fields
}

func (row *stateRow) bookkeeping(identity *titleIdentity) *database.LibraryStateSyncRow {
	fields := row.fields()
	title := ""
	if row.Title != nil {
		title = *row.Title
	}
	preferred := append([]string{}, row.PreferredTags...)
	return &database.LibraryStateSyncRow{
		IdentityKey:   identity.key(),
		MediaType:     identity.MediaType,
		SystemID:      identity.SystemID,
		CoreSlug:      identity.CoreSlug,
		VariantTags:   identity.Variants,
		Title:         title,
		Favorite:      fields.Favorite,
		Intent:        fields.Intent,
		Reaction:      fields.Reaction,
		PreferredTags: preferred,
		Revision:      row.Revision,
		Deleted:       row.Deleted,
	}
}
