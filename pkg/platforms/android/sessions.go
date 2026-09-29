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

package android

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/assets"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/sessionevidence"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// A launcher with LifecycleExternal (every Android launcher) leaves Core's
// process, so the active-media tracker never sees it end. track persists a
// pending session before asking the host to dispatch, and later
// reconciliation - never dispatch itself - is what may confirm and time it.
// No database, or a UserDB that does not implement ExternalSessionStore,
// dispatches exactly as if this file did not exist.
func (p *Platform) track(
	ctx context.Context, definition *LaunchDefinition, launcherID, path string, dispatch func() error,
) error {
	db := p.database()
	if db == nil {
		return dispatch()
	}
	store, ok := db.UserDB.(database.ExternalSessionStore)
	if !ok {
		return dispatch()
	}
	state, err := p.host.ForegroundState()
	if err != nil {
		log.Warn().Err(err).Msg("Android foreground state unavailable; launch cannot be timed")
		return dispatch()
	}
	if !state.valid() {
		return errors.New("invalid Android foreground state")
	}
	session := p.sessionForLaunch(ctx, db.MediaDB, definition, launcherID, path, state)
	if beginErr := store.BeginExternalSession(ctx, session); beginErr != nil {
		return fmt.Errorf("persist external launch before dispatch: %w", beginErr)
	}
	dispatchErr := dispatch()
	nowMs := time.Now().UnixMilli()
	if dispatchErr != nil {
		if definitelyFailed(dispatchErr) {
			if _, abandonErr := store.AbandonExternalSession(ctx, session.LaunchID, nowMs); abandonErr != nil {
				log.Error().Err(abandonErr).Msg("Android launch failed but its session could not be abandoned")
			}
		}
		// An outcome-unknown or cancelled dispatch can still mean the host
		// started the target. Leave it pending for reconciliation instead of
		// guessing either failure or playtime.
		return dispatchErr
	}
	recorded, err := store.RecordExternalDispatch(ctx, session.LaunchID, nowMs)
	notify := p.mediaHistoryHooks().Changed
	switch {
	case err != nil:
		log.Error().Err(err).Msg("Android launch dispatched but its receipt could not be persisted")
	case recorded && notify != nil && session.Source == "foreground_events":
		// A foreground_events launch now has an in-progress history row.
		notify()
	}
	return nil
}

// definitelyFailed reports whether the host gave a definite reason the target
// never started. FailureHostUnavailable, FailureInvalidResponse and
// FailureOutcomeUnknown - and anything not a *HostError at all, such as a
// receipt-mismatch repair error or a cancelled-context wrap - are all
// ambiguous: the host may still have started the target.
func definitelyFailed(err error) bool {
	var hostErr *HostError
	if !errors.As(err, &hostErr) {
		return false
	}
	switch hostErr.Reason {
	case FailureHostUnavailable, FailureInvalidResponse, FailureOutcomeUnknown, "":
		return false
	default:
		return true
	}
}

// sessionForLaunch snapshots what reconciliation will need, entirely from
// the definition already being dispatched: no catalog lookup by launcher ID
// is needed at reconciliation time.
func (p *Platform) sessionForLaunch(
	ctx context.Context, mediaDB database.MediaDBI, definition *LaunchDefinition,
	launcherID, path string, state ForegroundState,
) *database.ExternalSession {
	session := &database.ExternalSession{
		LaunchID: uuid.NewString(), LauncherID: launcherID,
		MediaPath: path, MediaName: tags.ParseTitleFromFilename(helpers.GetPathName(path), false),
		SystemID: definition.System, SystemName: systemDisplayName(definition.System),
		Target: definition.Package,
		BootID: state.BootID, RequestedMs: state.SampledMs, RequestedElapsedMs: state.ElapsedMs,
		InitialInteractive: state.Interactive, InitialUnlocked: state.Unlocked,
		Status: "pending", Source: "host_return",
	}
	if state.Permission == "granted" {
		session.Source = "foreground_events"
	}
	if activeProfile := p.mediaHistoryHooks().ActiveProfileID; activeProfile != nil {
		if profileID := activeProfile(); profileID != "" {
			session.ProfileID = &profileID
		}
	}
	if identity, found, err := database.LookupMediaIdentity(ctx, mediaDB, session.SystemID, path); err == nil && found {
		session.MediaIdentity = &identity
		// History shows the indexed title, as the library does, not a file name.
		if identity.DisplayName != "" {
			session.MediaName = identity.DisplayName
		}
	}
	return session
}

// systemDisplayName never fails a launch over missing display metadata: the
// system ID itself is always a legible fallback.
func systemDisplayName(systemID string) string {
	meta, err := assets.GetSystemMetadata(systemID)
	if err != nil || meta.Name == "" {
		return systemID
	}
	return meta.Name
}

// Android records foreground events asynchronously, so a query ending at
// "now" can miss the target's pause stamped at the same instant and move the
// cursor past it for good. Queries end this far before the sampled wall clock.
const usageEventSettleMs = 1000

// returnSettleDelay lets the target's pause from a frontend return be written
// and outlast the foreground grace period, so one query can close the segment.
var returnSettleDelay = time.Duration(usageEventSettleMs+sessionevidence.ForegroundGraceMs+1000) *
	time.Millisecond

// maxDispatchDelayMs bounds the request-to-receipt gap the approximate
// estimate trusts; the host's own dispatch wait is a few seconds at most.
const maxDispatchDelayMs = 10_000

// maxEvidenceWindowMs keeps each host query bounded, however far behind the
// cursor has fallen; ReconcileExternalSessions loops to catch up.
const maxEvidenceWindowMs = 6 * 60 * 60 * 1000

// HostReturn is what the host observed of its own launcher UI, all from one
// boot: the UI process cannot outlive a reboot.
type HostReturn struct {
	// ObserverStartedElapsedMs is when the UI process now reporting started.
	// A launch dispatched before it came from a process that died, so no
	// return can ever be observed for it. Zero when unknown.
	ObserverStartedElapsedMs int64
	// ReturnedWallMs and ReturnedElapsedMs are the earliest launcher resume
	// the host has not yet delivered. Zero when there was none.
	ReturnedWallMs    int64
	ReturnedElapsedMs int64
}

// ReconcileSessions runs after the frontend returns: it closes approximate
// launches from the observed return, then queries foreground evidence once
// it has settled.
func (p *Platform) ReconcileSessions(ctx context.Context, observed HostReturn) error {
	lifecycleErr := p.reconcileHostReturnSessions(ctx, observed)
	select {
	case <-ctx.Done():
		return errors.Join(lifecycleErr, ctx.Err())
	case <-time.After(returnSettleDelay):
	}
	return errors.Join(lifecycleErr, p.ReconcileExternalSessions(ctx))
}

// reconcileHostReturnSessions estimates launches made without host
// foreground evidence. The launcher's own return is the only end it can
// observe, so each estimate is the time away from Zaparoo after a confirmed
// dispatch, ending at the first later return or the next Zaparoo launch. It
// never proves the target was played and is stored as approximate. An
// unknown dispatch outcome stays pending; a launch whose dispatching UI
// process died, or from another boot, is retired without time.
func (p *Platform) reconcileHostReturnSessions(ctx context.Context, observed HostReturn) error {
	db := p.database()
	if db == nil {
		return nil
	}
	store, ok := db.UserDB.(database.ExternalSessionStore)
	if !ok {
		return nil
	}
	sessions, err := store.UnresolvedExternalSessions(ctx)
	if err != nil {
		return fmt.Errorf("list unresolved external sessions: %w", err)
	}
	var state ForegroundState
	stateRead := false
	changed := false
	defer func() {
		if notify := p.mediaHistoryHooks().Changed; changed && notify != nil {
			notify()
		}
	}()
	for i := range sessions {
		session := &sessions[i]
		if session.Source != "host_return" || session.Status != "pending" || session.DispatchedMs == nil {
			continue
		}
		if !stateRead {
			if state, err = p.host.ForegroundState(); err != nil {
				return fmt.Errorf("read Android foreground state: %w", err)
			}
			stateRead = true
		}
		if session.BootID != state.BootID || session.RequestedElapsedMs < observed.ObserverStartedElapsedMs {
			staled, staleErr := store.MarkExternalSessionStale(ctx, session.LaunchID, state.SampledMs)
			if staleErr != nil {
				return fmt.Errorf("retire unobservable external launch: %w", staleErr)
			}
			changed = changed || staled
			continue
		}
		// Dispatch follows the request within the host's dispatch timeout. The
		// clamp keeps a wall-clock step between them from skewing the estimate;
		// elapsed time keeps it immune to steps while the target was away.
		dispatchDelay := min(max(*session.DispatchedMs-session.RequestedMs, 0), maxDispatchDelayMs)
		dispatchedElapsed := session.RequestedElapsedMs + dispatchDelay
		var endElapsed int64
		if observed.ReturnedElapsedMs > dispatchedElapsed {
			endElapsed = observed.ReturnedElapsedMs
		}
		// A later Zaparoo launch replaced this title even without a return.
		// Order by the monotonic clock: wall time can repeat or step.
		for j := range sessions {
			next := &sessions[j]
			if next.BootID == session.BootID && next.RequestedElapsedMs > dispatchedElapsed &&
				(endElapsed == 0 || next.RequestedElapsedMs < endElapsed) {
				endElapsed = next.RequestedElapsedMs
			}
		}
		if endElapsed == 0 {
			continue
		}
		closed, closeErr := store.CloseExternalSessionApproximate(ctx, session.LaunchID,
			*session.DispatchedMs, *session.DispatchedMs+endElapsed-dispatchedElapsed)
		if closeErr != nil {
			return fmt.Errorf("close approximate external launch %s: %w", session.LaunchID, closeErr)
		}
		changed = changed || closed
	}
	return nil
}

// ReconcileExternalSessions queries only recorded launch targets. The
// persisted cursor and store transaction make repeated host observations
// idempotent. It is run at startup and after every host return.
func (p *Platform) ReconcileExternalSessions(ctx context.Context) error {
	db := p.database()
	if db == nil {
		return nil
	}
	store, ok := db.UserDB.(database.ExternalSessionStore)
	if !ok {
		return nil
	}
	sessions, err := store.UnresolvedExternalSessions(ctx)
	if err != nil {
		return fmt.Errorf("list unresolved external sessions: %w", err)
	}
	// Applied evidence can write or close a history row. Tell clients once,
	// even when a later session fails, so they refetch what did commit.
	applied := false
	defer func() {
		if notify := p.mediaHistoryHooks().Changed; applied && notify != nil {
			notify()
		}
	}()
	for i := range sessions {
		session := &sessions[i]
		if session.Source != "foreground_events" {
			continue
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("session reconciliation cancelled: %w", ctxErr)
		}
		state, stateErr := p.host.ForegroundState()
		if stateErr != nil {
			return fmt.Errorf("read Android foreground state: %w", stateErr)
		}
		if state.BootID != session.BootID || state.Permission != "granted" {
			staled, staleErr := store.MarkExternalSessionStale(ctx, session.LaunchID, state.SampledMs)
			if staleErr != nil {
				return fmt.Errorf("retire unobservable external session: %w", staleErr)
			}
			applied = applied || staled
			continue
		}
		from := session.RequestedMs
		if session.CursorMs != nil {
			from = *session.CursorMs
		}
		to := state.SampledMs - usageEventSettleMs
		if to <= from {
			continue
		}
		// Keep each host query bounded. A cursor that has fallen far behind
		// catches up over repeated calls - every host return reconciles -
		// rather than in one unbounded query.
		if to-from > maxEvidenceWindowMs {
			to = from + maxEvidenceWindowMs
		}
		batch, queryErr := p.host.ForegroundEvents(ctx, session.LaunchID, session.Target, from, to)
		if queryErr != nil {
			return fmt.Errorf("query Android foreground events: %w", queryErr)
		}
		changed, applyErr := store.ApplyExternalEvidence(ctx, &batch)
		if applyErr != nil {
			return fmt.Errorf("reconcile external launch %s: %w", session.LaunchID, applyErr)
		}
		applied = applied || changed
	}
	return nil
}
