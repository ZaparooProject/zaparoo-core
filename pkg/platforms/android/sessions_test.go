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
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/stretchr/testify/require"
)

type approxClose struct {
	launchID       string
	startMs, endMs int64
}

// sessionStoreProbe is an in-memory database.ExternalSessionStore. The real
// SQL transactions are tested in pkg/database/userdb; this probe lets these
// tests focus on how the platform orchestrates them.
type sessionStoreProbe struct {
	database.UserDBI
	beginErr        error
	applyErr        error
	sessions        map[string]*database.ExternalSession
	applyStatus     string
	sessionOrder    []string
	evidenceBatches []database.ForegroundEvidence
	approxCloses    []approxClose
	staled          []string
	abandoned       []string
	mu              syncutil.Mutex
}

func (p *sessionStoreProbe) BeginExternalSession(_ context.Context, session *database.ExternalSession) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.beginErr != nil {
		return p.beginErr
	}
	if p.sessions == nil {
		p.sessions = make(map[string]*database.ExternalSession)
	}
	snapshot := *session
	p.sessions[session.LaunchID] = &snapshot
	p.sessionOrder = append(p.sessionOrder, session.LaunchID)
	return nil
}

func (p *sessionStoreProbe) RecordExternalDispatch(_ context.Context, launchID string, atMs int64) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	session, ok := p.sessions[launchID]
	if !ok || session.DispatchedMs != nil {
		return false, nil
	}
	ms := atMs
	session.DispatchedMs = &ms
	return true, nil
}

func (p *sessionStoreProbe) AbandonExternalSession(_ context.Context, launchID string, _ int64) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.abandoned = append(p.abandoned, launchID)
	if session, ok := p.sessions[launchID]; ok {
		session.Status = "abandoned"
	}
	return true, nil
}

func (p *sessionStoreProbe) UnresolvedExternalSessions(context.Context) ([]database.ExternalSession, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []database.ExternalSession
	for _, id := range p.sessionOrder {
		session := p.sessions[id]
		switch session.Status {
		case "pending", "confirmed", "active", "suspended":
			out = append(out, *session)
		}
	}
	return out, nil
}

func (p *sessionStoreProbe) MarkExternalSessionStale(_ context.Context, launchID string, _ int64) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.staled = append(p.staled, launchID)
	session, ok := p.sessions[launchID]
	if !ok {
		return false, errors.New("unknown session")
	}
	switch session.Status {
	case "stale", "abandoned", "closed":
		return false, nil
	}
	session.Status = "stale"
	return true, nil
}

func (p *sessionStoreProbe) CloseExternalSessionApproximate(
	_ context.Context, launchID string, startMs, endMs int64,
) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.approxCloses = append(p.approxCloses, approxClose{launchID: launchID, startMs: startMs, endMs: endMs})
	if session, ok := p.sessions[launchID]; ok {
		session.Status = "closed"
	}
	return true, nil
}

func (p *sessionStoreProbe) ApplyExternalEvidence(_ context.Context, batch *database.ForegroundEvidence) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.evidenceBatches = append(p.evidenceBatches, *batch)
	if p.applyErr != nil {
		return false, p.applyErr
	}
	if session, ok := p.sessions[batch.LaunchID]; ok {
		if p.applyStatus != "" {
			session.Status = p.applyStatus
		}
		cursor := batch.QueryToMs
		session.CursorMs = &cursor
	}
	return true, nil
}

func trackedPlatform(ctx context.Context, t *testing.T, host Host, store database.UserDBI) *Platform {
	t.Helper()
	platform, err := New(platforms.Settings{DataDir: "/data"}, host)
	require.NoError(t, err)
	require.NoError(t, platform.StartPost(ctx, nil, fixedContext{ctx: ctx}, nil, nil,
		&database.Database{UserDB: store}, nil))
	return platform
}

// testDefinition is a valid, self-contained app-strategy definition. track
// only needs a System and Package to build a session from; it never
// dispatches for real in these tests.
func testDefinition() *LaunchDefinition {
	return &LaunchDefinition{
		Version: 3, ID: "GameNative.Steam", Name: "GameNative", System: systemdefs.SystemPC,
		Package: gameNativePackage, Activity: gameNativeActivity, Action: gameNativeAction,
		Strategy: StrategyApp, StorageAccess: "none", Repair: "x",
	}
}

func TestTrackPersistsBeforeDispatchAndRecordsSuccess(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	host := &fakeHost{state: &defaultForegroundState}
	store := &sessionStoreProbe{}
	platform := trackedPlatform(ctx, t, host, store)

	dispatched := false
	err := platform.track(ctx, testDefinition(), "GameNative.Steam", "source://test/PC/game.steam",
		func() error { dispatched = true; return nil })
	require.NoError(t, err)
	require.True(t, dispatched, "the dispatch closure itself must still run")
	require.Len(t, store.sessionOrder, 1)
	session := store.sessions[store.sessionOrder[0]]
	require.Equal(t, "foreground_events", session.Source, "granted permission tracks by foreground events")
	require.NotNil(t, session.DispatchedMs)
}

func TestTrackHostReturnPathHasNoImmediateHistoryNotification(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	deniedState := ForegroundState{BootID: "boot-1", Permission: "denied", SampledMs: 1000, ElapsedMs: 500}
	host := &fakeHost{state: &deniedState}
	store := &sessionStoreProbe{}
	platform := trackedPlatform(ctx, t, host, store)
	notified := false
	platform.SetMediaHistoryHooks(platforms.MediaHistoryHooks{Changed: func() { notified = true }})

	err := platform.track(ctx, testDefinition(), "GameNative.Steam", "source://test/PC/game.steam",
		func() error { return nil })
	require.NoError(t, err)
	require.Len(t, store.sessionOrder, 1)
	session := store.sessions[store.sessionOrder[0]]
	require.Equal(t, "host_return", session.Source)
	require.False(t, notified, "a host_return dispatch is not yet playtime worth telling clients about")
}

func TestTrackAbandonsOnDefiniteFailure(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	host := &fakeHost{state: &defaultForegroundState}
	store := &sessionStoreProbe{}
	platform := trackedPlatform(ctx, t, host, store)

	dispatchErr := &HostError{Reason: FailureNotInstalled}
	err := platform.track(ctx, testDefinition(), "GameNative.Steam", "source://test/PC/game.steam",
		func() error { return dispatchErr })
	require.ErrorIs(t, err, dispatchErr)
	require.Len(t, store.abandoned, 1)
}

func TestTrackLeavesPendingOnAmbiguousFailure(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	host := &fakeHost{state: &defaultForegroundState}
	store := &sessionStoreProbe{}
	platform := trackedPlatform(ctx, t, host, store)

	for _, reason := range []FailureReason{FailureHostUnavailable, FailureInvalidResponse, FailureOutcomeUnknown} {
		dispatchErr := &HostError{Reason: reason}
		err := platform.track(ctx, testDefinition(), "GameNative.Steam", "source://test/PC/game.steam",
			func() error { return dispatchErr })
		require.ErrorIs(t, err, dispatchErr)
	}
	require.Empty(t, store.abandoned, "an ambiguous outcome must stay pending, never abandoned")
}

func TestTrackInvalidForegroundStateRefusesLaunch(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	badState := ForegroundState{}
	host := &fakeHost{state: &badState}
	store := &sessionStoreProbe{}
	platform := trackedPlatform(ctx, t, host, store)

	dispatched := false
	err := platform.track(ctx, testDefinition(), "GameNative.Steam", "source://test/PC/game.steam",
		func() error { dispatched = true; return nil })
	require.Error(t, err)
	require.False(t, dispatched, "a host reporting garbage state must not be trusted to time a launch")
	require.Empty(t, store.sessionOrder)
}

func TestTrackWithoutDatabaseDispatchesUntracked(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	host := &fakeHost{state: &defaultForegroundState}
	platform, err := New(platforms.Settings{DataDir: "/data"}, host)
	require.NoError(t, err)
	require.NoError(t, platform.StartPost(ctx, nil, fixedContext{ctx: ctx}, nil, nil, nil, nil))

	dispatched := false
	trackErr := platform.track(ctx, testDefinition(), "GameNative.Steam", "source://test/PC/game.steam",
		func() error { dispatched = true; return nil })
	require.NoError(t, trackErr)
	require.True(t, dispatched)
}

func TestTrackWithoutStoreCapabilityDispatchesUntracked(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	host := &fakeHost{state: &defaultForegroundState}
	platform := trackedPlatform(ctx, t, host, nil)

	dispatched := false
	err := platform.track(ctx, testDefinition(), "GameNative.Steam", "source://test/PC/game.steam",
		func() error { dispatched = true; return nil })
	require.NoError(t, err)
	require.True(t, dispatched, "a UserDB that does not implement ExternalSessionStore must not block dispatch")
}

func TestDefinitelyFailedClassification(t *testing.T) {
	t.Parallel()
	require.False(t, definitelyFailed(errors.New("plain error")))
	require.False(t, definitelyFailed(&HostError{Reason: FailureHostUnavailable}))
	require.False(t, definitelyFailed(&HostError{Reason: FailureOutcomeUnknown}))
	require.True(t, definitelyFailed(&HostError{Reason: FailureNotInstalled}))
	require.True(t, definitelyFailed(&HostError{Reason: FailureCancelled}))
	// Wrapped the way dispatch()/dispatchApp() actually wrap a host error.
	wrapped := repairError(platforms.LaunchRepairLauncherNotInstalled, nil, "x")
	require.False(t, definitelyFailed(wrapped), "a repair error alone, with no HostError in its chain, is ambiguous")
}

func TestReconcileExternalSessionsConfirmsAndNotifiesOnce(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	host := &fakeHost{state: &defaultForegroundState}
	store := &sessionStoreProbe{applyStatus: "closed"}
	platform := trackedPlatform(ctx, t, host, store)
	notifications := 0
	platform.SetMediaHistoryHooks(platforms.MediaHistoryHooks{Changed: func() { notifications++ }})

	require.NoError(t, store.BeginExternalSession(ctx, &database.ExternalSession{
		LaunchID: "launch-1", SystemID: "PC", SystemName: "PC", MediaPath: "source://test/PC/game.steam",
		MediaName: "Game", LauncherID: "GameNative.Steam", Target: "app.gamenative",
		BootID: "boot-1", RequestedMs: 500, Status: "pending", Source: "foreground_events",
	}))

	require.NoError(t, platform.ReconcileExternalSessions(ctx))
	require.Len(t, host.evidenceQueries, 1)
	require.Equal(t, "launch-1", host.evidenceQueries[0].launchID)
	require.Equal(t, "app.gamenative", host.evidenceQueries[0].target)
	require.Len(t, store.evidenceBatches, 1)
	require.Equal(t, 1, notifications)
}

func TestReconcileExternalSessionsStalesOnBootChange(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	host := &fakeHost{state: &defaultForegroundState}
	store := &sessionStoreProbe{}
	platform := trackedPlatform(ctx, t, host, store)

	require.NoError(t, store.BeginExternalSession(ctx, &database.ExternalSession{
		LaunchID: "launch-1", SystemID: "PC", SystemName: "PC", MediaPath: "source://test/PC/game.steam",
		MediaName: "Game", LauncherID: "GameNative.Steam", Target: "app.gamenative",
		BootID: "boot-0", RequestedMs: 500, Status: "pending", Source: "foreground_events",
	}))

	require.NoError(t, platform.ReconcileExternalSessions(ctx))
	require.Equal(t, []string{"launch-1"}, store.staled)
	require.Empty(t, host.evidenceQueries, "a session from a stale boot is never queried")
}

func TestReconcileHostReturnSessionsClosesApproximateOnReturn(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	host := &fakeHost{state: &defaultForegroundState}
	store := &sessionStoreProbe{}
	platform := trackedPlatform(ctx, t, host, store)

	require.NoError(t, store.BeginExternalSession(ctx, &database.ExternalSession{
		LaunchID: "launch-1", SystemID: "PC", SystemName: "PC", MediaPath: "source://test/PC/game.steam",
		MediaName: "Game", LauncherID: "GameNative.Steam", Target: "app.gamenative",
		BootID: "boot-1", RequestedMs: 1000, RequestedElapsedMs: 1000, Status: "pending", Source: "host_return",
	}))
	dispatchedMs := int64(1200)
	store.sessions["launch-1"].DispatchedMs = &dispatchedMs

	err := platform.reconcileHostReturnSessions(ctx, HostReturn{
		ObserverStartedElapsedMs: 500, ReturnedWallMs: 10_000, ReturnedElapsedMs: 10_000,
	})
	require.NoError(t, err)
	require.Len(t, store.approxCloses, 1)
	require.Equal(t, "launch-1", store.approxCloses[0].launchID)
}

func TestReconcileHostReturnSessionsStalesADeadObserverProcess(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	host := &fakeHost{state: &defaultForegroundState}
	store := &sessionStoreProbe{}
	platform := trackedPlatform(ctx, t, host, store)

	require.NoError(t, store.BeginExternalSession(ctx, &database.ExternalSession{
		LaunchID: "launch-1", SystemID: "PC", SystemName: "PC", MediaPath: "source://test/PC/game.steam",
		MediaName: "Game", LauncherID: "GameNative.Steam", Target: "app.gamenative",
		BootID: "boot-1", RequestedMs: 1000, RequestedElapsedMs: 100, Status: "pending", Source: "host_return",
	}))
	dispatchedMs := int64(1200)
	store.sessions["launch-1"].DispatchedMs = &dispatchedMs

	// The reporting UI process started after this launch was requested, so
	// the process that dispatched it is gone and can never be observed
	// returning.
	err := platform.reconcileHostReturnSessions(ctx, HostReturn{ObserverStartedElapsedMs: 5000})
	require.NoError(t, err)
	require.Equal(t, []string{"launch-1"}, store.staled)
}

func TestReconcileHostReturnSessionsStalesAnUndispatchedDeadObserverSession(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	host := &fakeHost{state: &defaultForegroundState}
	store := &sessionStoreProbe{}
	platform := trackedPlatform(ctx, t, host, store)

	// Never dispatched: the outcome was ambiguous (host-unavailable, say) and
	// nothing ever recorded a receipt for it.
	require.NoError(t, store.BeginExternalSession(ctx, &database.ExternalSession{
		LaunchID: "launch-1", SystemID: "PC", SystemName: "PC", MediaPath: "source://test/PC/game.steam",
		MediaName: "Game", LauncherID: "GameNative.Steam", Target: "app.gamenative",
		BootID: "boot-1", RequestedMs: 1000, RequestedElapsedMs: 100, Status: "pending", Source: "host_return",
	}))

	err := platform.reconcileHostReturnSessions(ctx, HostReturn{ObserverStartedElapsedMs: 5000})
	require.NoError(t, err)
	require.Equal(t, []string{"launch-1"}, store.staled,
		"an undispatched session from a dead observer process must not linger pending forever")
}

func TestReconcileExternalSessionsOneFailureDoesNotBlockAnother(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	host := &fakeHost{state: &defaultForegroundState}
	store := &sessionStoreProbe{applyStatus: "closed", applyErr: errors.New("boom")}
	platform := trackedPlatform(ctx, t, host, store)

	require.NoError(t, store.BeginExternalSession(ctx, &database.ExternalSession{
		LaunchID: "launch-broken", SystemID: "PC", SystemName: "PC", MediaPath: "source://test/PC/a.steam",
		MediaName: "A", LauncherID: "GameNative.Steam", Target: "app.gamenative",
		BootID: "boot-1", RequestedMs: 500, Status: "pending", Source: "foreground_events",
	}))
	require.NoError(t, store.BeginExternalSession(ctx, &database.ExternalSession{
		LaunchID: "launch-ok", SystemID: "PC", SystemName: "PC", MediaPath: "source://test/PC/b.steam",
		MediaName: "B", LauncherID: "GameNative.Steam", Target: "app.gamenative",
		BootID: "boot-1", RequestedMs: 600, Status: "pending", Source: "foreground_events",
	}))

	err := platform.ReconcileExternalSessions(ctx)
	require.Error(t, err, "the failing session's error is still surfaced")
	require.Len(t, host.evidenceQueries, 2, "both sessions must still be queried in the same pass")
}
