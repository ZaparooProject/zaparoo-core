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

package playtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/rs/zerolog/log"
)

// GrantMode selects what an extension grant does to the recipient's limits.
type GrantMode string

const (
	// GrantModeDuration adds time to the current session's allowance. It
	// applies to one session and is cleared when that session resets.
	GrantModeDuration GrantMode = "duration"
	// GrantModeToday waives the session limit for the recipient profile
	// until the next local midnight. The daily limit still applies.
	GrantModeToday GrantMode = "today"
)

const (
	// MinGrantDuration is the smallest duration grant accepted. Anything
	// shorter is not worth the scan and would be swallowed by the 30 second
	// check interval.
	MinGrantDuration = 1 * time.Minute
	// MaxGrantDuration is the largest duration a single grant may add.
	MaxGrantDuration = 24 * time.Hour
	// MaxSessionExtension caps the duration accumulated across every grant
	// applied to one session. Grants that would exceed it are rejected
	// rather than clamped, so the caller learns the grant did not apply.
	MaxSessionExtension = 24 * time.Hour
	// grantLedgerSize bounds the per-session idempotency ledger. Grants are
	// rare; this only needs to absorb retries and reader bounce.
	grantLedgerSize = 16
	// extensionsStateVersion is the schema version of the persisted
	// extensions snapshot. An unknown version fails closed.
	extensionsStateVersion = 1
)

// Grant rejection reasons. Callers map these onto their own error surfaces:
// a failed ZapScript command for the card path, a client error for the API.
var (
	// ErrGrantModeInvalid is returned for an unrecognized grant mode.
	ErrGrantModeInvalid = errors.New("unknown playtime extension mode")
	// ErrGrantDurationRange is returned when a duration grant falls outside
	// MinGrantDuration..MaxGrantDuration.
	ErrGrantDurationRange = errors.New("playtime extension duration out of range")
	// ErrGrantCapExceeded is returned when a grant would push the session's
	// accumulated extension past MaxSessionExtension.
	ErrGrantCapExceeded = errors.New("playtime extension would exceed the session cap")
	// ErrGrantNoSession is returned when a duration grant is attempted with
	// no session to extend.
	ErrGrantNoSession = errors.New("no playtime session to extend")
	// ErrGrantLimitsDisabled is returned when limits are not being enforced,
	// so there is nothing to extend.
	ErrGrantLimitsDisabled = errors.New("playtime limits are not enabled")
	// ErrGrantClockUnreliable is returned when a day-scoped grant is
	// attempted while the system clock cannot be trusted to find midnight.
	ErrGrantClockUnreliable = errors.New("system clock is unreliable")
	// ErrGrantStateChanged is returned when the session changed underneath a
	// grant while it was being applied. The caller may retry.
	ErrGrantStateChanged = errors.New("playtime session changed during grant")
	// ErrGrantUnavailable is returned when the grant cannot be persisted, so
	// it is refused rather than held only in memory.
	ErrGrantUnavailable = errors.New("playtime extension storage unavailable")
)

// GrantRequest asks for an extension to the effective playtime session.
// The recipient is never chosen by the caller: it is the profile currently
// governing playtime, so a grant cannot be aimed at somebody else.
type GrantRequest struct {
	AuthorizerProfileID string
	AuthorizerClientID  string
	Source              string
	IdempotencyKey      string
	Mode                GrantMode
	IdempotencyWindow   time.Duration
	Duration            time.Duration
}

// GrantResult describes an applied grant. It is also what a deduplicated
// repeat returns, so a retry sees the same answer as the original call.
type GrantResult struct {
	// ExpiresAt is when a day waiver lapses. Zero for duration grants.
	ExpiresAt time.Time
	// RecipientProfileID is the profile the grant applies to. Empty is the
	// shared profile, matching daily accounting elsewhere.
	RecipientProfileID string
	// AuthorizerProfileID is the profile that authorized the grant.
	AuthorizerProfileID string
	// Mode is the mode that was applied.
	Mode GrantMode
	// Duration is what this grant added. Zero for day waivers.
	Duration time.Duration
	// SessionExtension is the session's accumulated duration extension after
	// this grant.
	SessionExtension time.Duration
	// Replayed is true when the request granted no new time, either because a
	// matching idempotency key had already been applied or because the day
	// was already waived.
	Replayed bool
}

// sessionExtension is the duration granted to the current session. It is
// pinned to the profile that owned the session when the grant landed, so a
// later profile switch cannot inherit it.
type sessionExtension struct {
	updatedAt           time.Time
	recipientProfileID  string
	authorizerProfileID string
	total               time.Duration
}

// dayWaiver suspends the session limit for one profile until it expires.
type dayWaiver struct {
	expires             time.Time
	authorizerProfileID string
}

// appliedGrant is one entry in the per-session idempotency ledger.
type appliedGrant struct {
	at     time.Time
	expiry time.Time
	key    string
	result GrantResult
}

// Grant applies an extension to the effective session. It resolves the
// recipient, validates the request against current state, persists the new
// snapshot, and only then updates memory: a grant that cannot be stored is
// refused rather than surviving only until the next restart.
//
// On success the caller should treat the returned result as the record of
// what happened, including for a deduplicated repeat.
func (tm *LimitsManager) Grant(req *GrantRequest) (GrantResult, error) {
	now := tm.clock.Now()

	// Both helpers take tm.mu, so they must be called before it is held.
	if !tm.effectiveEnabled() {
		return GrantResult{}, ErrGrantLimitsDisabled
	}

	if req.Mode == GrantModeDuration {
		if req.Duration < MinGrantDuration || req.Duration > MaxGrantDuration {
			return GrantResult{}, fmt.Errorf("%w: %s", ErrGrantDurationRange, req.Duration)
		}
	} else if req.Mode != GrantModeToday {
		return GrantResult{}, fmt.Errorf("%w: %q", ErrGrantModeInvalid, req.Mode)
	}

	// A day waiver is anchored to the local calendar, so it needs a clock we
	// can believe. Duration grants are measured against the session, which
	// already falls back to monotonic elapsed time.
	if req.Mode == GrantModeToday && !helpers.IsClockReliable(now) {
		return GrantResult{}, fmt.Errorf("%w: year %d", ErrGrantClockUnreliable, now.Year())
	}

	result, err := tm.applyGrant(req, now)
	if err != nil {
		return GrantResult{}, err
	}

	if result.Replayed {
		return result, nil
	}

	log.Info().
		Str("mode", string(result.Mode)).
		Dur("duration", result.Duration).
		Dur("session_extension_total", result.SessionExtension).
		Time("expires_at", result.ExpiresAt).
		Str("recipient_profile_id", result.RecipientProfileID).
		Str("authorizer_profile_id", result.AuthorizerProfileID).
		Str("authorizer_client_id", req.AuthorizerClientID).
		Str("source", req.Source).
		Msg("playtime: extension granted")

	// Re-evaluate immediately so the new allowance and the re-armed warning
	// thresholds take effect now instead of at the next 30 second tick.
	tm.checkLimits()

	return result, nil
}

// applyGrant performs the locked portion of Grant: validate, persist, commit.
func (tm *LimitsManager) applyGrant(req *GrantRequest, now time.Time) (GrantResult, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if replay, ok := tm.lookupGrantLocked(req.IdempotencyKey, now); ok {
		return replay, nil
	}

	recipient := tm.effectiveProfileIDLocked()

	result := GrantResult{
		Mode:                req.Mode,
		RecipientProfileID:  recipient,
		AuthorizerProfileID: req.AuthorizerProfileID,
	}

	// Snapshot the state we are about to change so a persistence failure
	// leaves nothing behind.
	nextSession := tm.sessionExtension
	nextWaivers := tm.copyWaiversLocked(now)

	switch req.Mode {
	case GrantModeDuration:
		// Cooldown is the same session as the game that just stopped, and it
		// is when a grant is most useful: the limit stopped the game and the
		// player is about to relaunch. Only a reset session has nothing to
		// extend.
		if tm.state == StateReset {
			return GrantResult{}, ErrGrantNoSession
		}

		total := req.Duration
		if nextSession != nil && nextSession.recipientProfileID == recipient {
			total += nextSession.total
		}
		if total > MaxSessionExtension {
			return GrantResult{}, fmt.Errorf(
				"%w: %s granted, %s requested, %s cap",
				ErrGrantCapExceeded, tm.sessionExtensionTotalLocked(recipient), req.Duration, MaxSessionExtension,
			)
		}

		nextSession = &sessionExtension{
			recipientProfileID:  recipient,
			authorizerProfileID: req.AuthorizerProfileID,
			total:               total,
			updatedAt:           now,
		}
		result.Duration = req.Duration
		result.SessionExtension = total

	case GrantModeToday:
		// Repeating a day waiver is a no-op rather than a rolling extension:
		// the boundary is midnight either way.
		if existing, ok := nextWaivers[recipient]; ok && existing.expires.After(now) {
			result.ExpiresAt = existing.expires
			result.SessionExtension = tm.sessionExtensionTotalLocked(recipient)
			// Nothing new was granted, so this must not read as a fresh grant
			// to notification subscribers.
			result.Replayed = true
			tm.recordGrantLocked(req, &result, now)
			return result, nil
		}

		expires := nextLocalMidnight(now)
		nextWaivers[recipient] = dayWaiver{
			expires:             expires,
			authorizerProfileID: req.AuthorizerProfileID,
		}
		result.ExpiresAt = expires
		result.SessionExtension = tm.sessionExtensionTotalLocked(recipient)
	}

	if err := tm.persistExtensionsLocked(nextSession, nextWaivers); err != nil {
		return GrantResult{}, err
	}

	tm.sessionExtension = nextSession
	tm.dayWaivers = nextWaivers

	// Warning thresholds already given were measured against the old
	// allowance. Re-arm them so they fire again against the new one.
	tm.warningsGiven = make(map[time.Duration]bool)

	tm.recordGrantLocked(req, &result, now)

	return result, nil
}

// sessionExtensionTotalLocked returns the duration extension in force for
// recipient, or zero when the current grant belongs to another profile.
func (tm *LimitsManager) sessionExtensionTotalLocked(recipient string) time.Duration {
	if tm.sessionExtension == nil || tm.sessionExtension.recipientProfileID != recipient {
		return 0
	}
	return tm.sessionExtension.total
}

// dayWaiverExpiryLocked returns the expiry of recipient's active day waiver,
// or the zero time when none applies. Expired waivers read as absent whether
// or not they have been pruned yet, so correctness never depends on pruning.
func (tm *LimitsManager) dayWaiverExpiryLocked(recipient string, now time.Time) time.Time {
	waiver, ok := tm.dayWaivers[recipient]
	if !ok {
		return time.Time{}
	}
	// A waiver written under a good clock must not be honored (or expired)
	// while the clock is untrustworthy: "before midnight" is meaningless.
	if !helpers.IsClockReliable(now) {
		return time.Time{}
	}
	if !waiver.expires.After(now) {
		return time.Time{}
	}
	return waiver.expires
}

// copyWaiversLocked returns a copy of the waiver map with expired entries
// dropped. Callers mutate the copy and swap it in on success.
func (tm *LimitsManager) copyWaiversLocked(now time.Time) map[string]dayWaiver {
	next := make(map[string]dayWaiver, len(tm.dayWaivers))
	for profileID, waiver := range tm.dayWaivers {
		// Keep future waivers under an unreliable clock: the grant may
		// simply be waiting for time to be set. dayWaiverExpiryLocked
		// refuses to honor them until then.
		if !helpers.IsClockReliable(now) || waiver.expires.After(now) {
			next[profileID] = waiver
		}
	}
	return next
}

// lookupGrantLocked returns a previously applied grant for key, if it is
// still within its idempotency window.
func (tm *LimitsManager) lookupGrantLocked(key string, now time.Time) (GrantResult, bool) {
	if key == "" {
		return GrantResult{}, false
	}
	for i := range tm.grantLedger {
		entry := &tm.grantLedger[i]
		if entry.key != key {
			continue
		}
		if !entry.expiry.IsZero() && !entry.expiry.After(now) {
			continue
		}
		replay := entry.result
		replay.Replayed = true
		return replay, true
	}
	return GrantResult{}, false
}

// recordGrantLocked appends an applied grant to the bounded ledger.
func (tm *LimitsManager) recordGrantLocked(req *GrantRequest, result *GrantResult, now time.Time) {
	if req.IdempotencyKey == "" {
		return
	}
	entry := appliedGrant{
		key:    req.IdempotencyKey,
		at:     now,
		result: *result,
	}
	if req.IdempotencyWindow > 0 {
		entry.expiry = now.Add(req.IdempotencyWindow)
	}
	tm.grantLedger = append(tm.grantLedger, entry)
	if len(tm.grantLedger) > grantLedgerSize {
		tm.grantLedger = tm.grantLedger[len(tm.grantLedger)-grantLedgerSize:]
	}
}

// clearSessionExtensionLocked drops the current session's duration grant and
// its idempotency ledger. Day waivers outlive the session and are kept.
// Persistence failures are logged, not returned: the in-memory clear is the
// safe direction, and a stale stored grant is discarded at restore time when
// its recipient no longer matches.
func (tm *LimitsManager) clearSessionExtensionLocked() {
	if tm.sessionExtension == nil && len(tm.grantLedger) == 0 {
		return
	}
	tm.sessionExtension = nil
	tm.grantLedger = nil
	if err := tm.persistExtensionsLocked(nil, tm.dayWaivers); err != nil {
		log.Warn().Err(err).Msg("playtime: failed to clear stored session extension")
	}
}

// pruneExpiredWaivers drops lapsed day waivers and rewrites the stored
// snapshot when anything actually changed.
func (tm *LimitsManager) pruneExpiredWaivers() {
	now := tm.clock.Now()

	tm.mu.Lock()
	defer tm.mu.Unlock()

	if len(tm.dayWaivers) == 0 {
		return
	}
	next := tm.copyWaiversLocked(now)
	if len(next) == len(tm.dayWaivers) {
		return
	}
	if err := tm.persistExtensionsLocked(tm.sessionExtension, next); err != nil {
		log.Warn().Err(err).Msg("playtime: failed to prune stored day waivers")
		return
	}
	tm.dayWaivers = next
}

// nextLocalMidnight returns the start of the next local day. Building the
// date rather than adding 24 hours keeps the boundary correct across DST.
func nextLocalMidnight(now time.Time) time.Time {
	year, month, day := now.Date()
	return time.Date(year, month, day+1, 0, 0, 0, 0, now.Location())
}

// persistedExtensions is the stored form of the manager's grant state. It
// holds resolved profile IDs only: switch IDs are bearer credentials and
// never reach durable storage.
type persistedExtensions struct {
	Session    *persistedSessionExtension `json:"session,omitempty"`
	DayWaivers []persistedDayWaiver       `json:"dayWaivers,omitempty"`
	Version    int                        `json:"version"`
}

type persistedSessionExtension struct {
	UpdatedAt           time.Time `json:"updatedAt"`
	RecipientProfileID  string    `json:"recipientProfileId"`
	AuthorizerProfileID string    `json:"authorizerProfileId"`
	TotalSeconds        int64     `json:"totalSeconds"`
}

type persistedDayWaiver struct {
	Expires             time.Time `json:"expires"`
	ProfileID           string    `json:"profileId"`
	AuthorizerProfileID string    `json:"authorizerProfileId"`
}

// persistExtensionsLocked writes the complete snapshot, or deletes the key
// when there is nothing left to remember.
func (tm *LimitsManager) persistExtensionsLocked(
	session *sessionExtension,
	waivers map[string]dayWaiver,
) error {
	if tm.db == nil || tm.db.UserDB == nil {
		return ErrGrantUnavailable
	}

	if session == nil && len(waivers) == 0 {
		if err := tm.db.UserDB.DeleteDeviceState(database.DeviceStateKeyPlaytimeExtensions); err != nil {
			return fmt.Errorf("failed to delete playtime extension state: %w", err)
		}
		return nil
	}

	state := persistedExtensions{Version: extensionsStateVersion}
	if session != nil {
		state.Session = &persistedSessionExtension{
			RecipientProfileID:  session.recipientProfileID,
			AuthorizerProfileID: session.authorizerProfileID,
			TotalSeconds:        int64(session.total / time.Second),
			UpdatedAt:           session.updatedAt,
		}
	}
	for profileID, waiver := range waivers {
		state.DayWaivers = append(state.DayWaivers, persistedDayWaiver{
			ProfileID:           profileID,
			AuthorizerProfileID: waiver.authorizerProfileID,
			Expires:             waiver.expires,
		})
	}

	encoded, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("failed to encode playtime extension state: %w", err)
	}
	if err := tm.db.UserDB.SetDeviceState(
		database.DeviceStateKeyPlaytimeExtensions, string(encoded),
	); err != nil {
		return fmt.Errorf("failed to store playtime extension state: %w", err)
	}
	return nil
}

// RestoreExtensions reloads granted extensions after a restart. It must run
// after RestoreSessionFromHistory so it can see whether the session the
// duration grant belongs to actually came back.
//
// Day waivers restore on their own: they are scoped to a profile and a
// calendar day, not to a session. A duration grant only restores when its
// session was restored and the recipient still matches, so a grant cannot be
// carried into somebody else's session by restarting the service.
func (tm *LimitsManager) RestoreExtensions(now time.Time) {
	if tm.db == nil || tm.db.UserDB == nil {
		return
	}

	raw, ok, err := tm.db.UserDB.GetDeviceState(database.DeviceStateKeyPlaytimeExtensions)
	if err != nil {
		log.Warn().Err(err).Msg("playtime: failed to read stored extensions")
		return
	}
	if !ok || raw == "" {
		return
	}

	var state persistedExtensions
	if decodeErr := json.Unmarshal([]byte(raw), &state); decodeErr != nil {
		log.Warn().Err(decodeErr).Msg("playtime: discarding malformed extension state")
		tm.discardStoredExtensions()
		return
	}
	if state.Version != extensionsStateVersion {
		log.Warn().
			Int("version", state.Version).
			Int("expected", extensionsStateVersion).
			Msg("playtime: discarding extension state written by another version")
		tm.discardStoredExtensions()
		return
	}

	waivers := make(map[string]dayWaiver, len(state.DayWaivers))
	for i := range state.DayWaivers {
		stored := &state.DayWaivers[i]
		// Keep future waivers under an unreliable clock so a device that
		// boots before NTP does not silently drop a grant.
		if helpers.IsClockReliable(now) && !stored.Expires.After(now) {
			continue
		}
		waivers[stored.ProfileID] = dayWaiver{
			expires:             stored.Expires,
			authorizerProfileID: stored.AuthorizerProfileID,
		}
	}

	tm.mu.Lock()
	restoredSession := tm.state != StateReset
	recipient := tm.effectiveProfileIDLocked()

	var session *sessionExtension
	switch {
	case state.Session == nil:
	case !restoredSession:
		log.Info().Msg("playtime: discarding stored session extension, no session restored")
	case state.Session.RecipientProfileID != recipient:
		log.Info().
			Str("stored_profile_id", state.Session.RecipientProfileID).
			Str("effective_profile_id", recipient).
			Msg("playtime: discarding stored session extension, recipient changed")
	default:
		session = &sessionExtension{
			recipientProfileID:  state.Session.RecipientProfileID,
			authorizerProfileID: state.Session.AuthorizerProfileID,
			total:               time.Duration(state.Session.TotalSeconds) * time.Second,
			updatedAt:           state.Session.UpdatedAt,
		}
	}

	tm.sessionExtension = session
	tm.dayWaivers = waivers
	err = tm.persistExtensionsLocked(session, waivers)
	tm.mu.Unlock()

	if err != nil {
		log.Warn().Err(err).Msg("playtime: failed to rewrite extension state after restore")
	}

	if session != nil {
		log.Info().
			Dur("session_extension", session.total).
			Str("recipient_profile_id", session.recipientProfileID).
			Msg("playtime: restored session extension")
	}
	if len(waivers) > 0 {
		log.Info().Int("day_waivers", len(waivers)).Msg("playtime: restored day waivers")
	}
}

// discardStoredExtensions removes state this build cannot interpret, so a
// device does not carry an unreadable grant forward indefinitely.
func (tm *LimitsManager) discardStoredExtensions() {
	if err := tm.db.UserDB.DeleteDeviceState(database.DeviceStateKeyPlaytimeExtensions); err != nil {
		log.Warn().Err(err).Msg("playtime: failed to discard stored extensions")
	}
}
