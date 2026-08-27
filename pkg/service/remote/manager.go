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

// Package remote implements the device-side remote operations client: it
// advertises capability to the Online API, long-polls for queued operations,
// executes them locally, and reports results back, replaying any result the
// device couldn't report the first time.
package remote

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	gozapscript "github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/permissions"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/backup"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/profiles"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	uievents "github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/events"
	"github.com/rs/zerolog/log"
)

const (
	protocolVersion      = 1
	waitTimeoutSeconds   = 25
	requestTimeout       = 30 * time.Second
	longRetry            = 5 * time.Minute
	minBackoff           = time.Second
	maxBackoff           = 30 * time.Second
	resultLimit          = 4 << 10
	queryLimit           = 32 << 10
	resultReplayInterval = 30 * time.Second
	// resultReplayBatchLimit caps how many unreported results a single
	// replayStoredResults call posts. run() calls it again every
	// resultReplayInterval, so a backlog larger than this drains over
	// several cycles instead of one call holding resultMu (indirectly, one
	// post at a time) for as long as the whole backlog takes to post.
	resultReplayBatchLimit = 20
	// commandsRetention keeps executed commands around long enough to serve
	// as an owner-facing activity log (see remote.activity), not just for
	// replay-protection purposes.
	commandsRetention = 7 * 24 * time.Hour
)

var errUnauthorized = errors.New("remote operations device credential rejected")

// MethodResolver looks up a registered API method handler by name. Satisfied
// structurally by *api.MethodMap. Kept as a narrow interface, rather than
// importing pkg/api directly, so a test can inject a two-entry fake instead
// of standing up the whole registry, and so this package's dependency graph
// stays limited to pkg/api/models and pkg/api/permissions.
type MethodResolver interface {
	GetMethod(name string) (func(requests.RequestEnv) (any, error), bool)
}

// Deps are the dependencies Start needs from the running service. RunZapScript
// is a callback rather than a direct function reference because the actual
// ZapScript runner lives in the parent service package, which imports this
// package to call Start — calling back the other way would be a cycle.
type Deps struct {
	Platform        platforms.Platform
	Config          *config.Instance
	State           *state.State
	DB              *database.Database
	Profiles        *profiles.Service
	PlaybackManager audio.PlaybackManager
	UI              *uievents.Service
	ConfirmQueue    chan chan error
	PlaylistQueue   chan *playlists.Playlist
	IndexPauser     *syncutil.Pauser
	ScrapePauser    *syncutil.Pauser
	BackupPauser    *syncutil.Pauser
	// Methods resolves API method handlers for allowlisted operations that
	// dispatch through the shared registry instead of bespoke adapters. See
	// allowlist.go.
	Methods      MethodResolver
	RunZapScript func(
		ctx context.Context, token tokens.Token, plsc playlists.PlaylistController,
		exprEnv *gozapscript.ArgExprEnv, inHookContext bool,
	) error
}

type manager struct {
	deps          Deps
	httpClient    *http.Client
	execute       func(context.Context, *operationEnvelope) operationResult
	executionSlot chan struct{}
	resultMu      syncutil.Mutex
}

// Start launches the remote operations polling loop in a background goroutine
// registered on wg. It returns immediately.
func Start(ctx context.Context, deps *Deps, wg *sync.WaitGroup) {
	m := &manager{
		deps:          *deps,
		httpClient:    &http.Client{},
		executionSlot: make(chan struct{}, 1),
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		m.run(ctx)
	}()
}

func (m *manager) run(ctx context.Context) {
	var workers sync.WaitGroup
	defer workers.Wait()

	if _, err := m.deps.DB.UserDB.PruneRemoteCommands(time.Now().Add(-commandsRetention)); err != nil {
		log.Warn().Err(err).Msg("failed to prune remote command ledger")
	}
	advertised := false
	replayed := false
	lastReplay := time.Time{}
	lastPrune := time.Now()
	backoff := minBackoff
	for ctx.Err() == nil {
		bearer := m.deviceBearer()
		enabled := m.deps.Config.RemoteControlEnabled()
		if !enabled || bearer == "" {
			if advertised && bearer != "" {
				if err := m.sendCapabilityHeartbeat(ctx); err != nil {
					log.Warn().Err(err).Msg("remote operations capability withdrawal failed")
				}
			}
			advertised = false
			replayed = false
			lastReplay = time.Time{}
			m.sleepWhileEligible(ctx, time.Second, false)
			continue
		}

		if !advertised {
			if err := m.sendCapabilityHeartbeat(ctx); err != nil {
				if isUnauthorized(err) {
					m.markUnlinkedIfSharedEndpoint()
					m.sleepWhileEligible(ctx, time.Minute, true)
					continue
				}
				log.Warn().Err(err).Msg("remote operations capability heartbeat failed")
				m.sleepWhileEligible(ctx, m.jitter(backoff), true)
				backoff = min(backoff*2, maxBackoff)
				continue
			}
			advertised = true
			backoff = minBackoff
		}
		now := time.Now()
		if resultsReplayDue(replayed, lastReplay, now) {
			m.replayStoredResults(ctx)
			replayed = true
			lastReplay = now
		}
		if time.Since(lastPrune) >= 24*time.Hour {
			if _, err := m.deps.DB.UserDB.PruneRemoteCommands(time.Now().Add(-commandsRetention)); err != nil {
				log.Warn().Err(err).Msg("failed to prune remote command ledger")
			}
			lastPrune = time.Now()
		}

		envelope, hasWork, err := m.waitOnce(ctx, bearer)
		if err != nil {
			var httpErr *httpError
			switch {
			case isUnauthorized(err):
				m.markUnlinkedIfSharedEndpoint()
				advertised = false
				m.sleepWhileEligible(ctx, time.Minute, true)
			case errors.As(err, &httpErr) && httpErr.status == http.StatusNotFound:
				m.sleepWhileEligible(ctx, longRetry, true)
			case errors.As(err, &httpErr) && (httpErr.status == http.StatusTooManyRequests ||
				httpErr.status == http.StatusServiceUnavailable):
				delay := httpErr.retryAfter
				if delay <= 0 {
					delay = maxBackoff
				}
				m.sleepWhileEligible(ctx, delay, true)
			default:
				log.Warn().Err(err).Dur("retry_in", backoff).Msg("remote operations wait failed")
				m.sleepWhileEligible(ctx, m.jitter(backoff), true)
				backoff = min(backoff*2, maxBackoff)
			}
			continue
		}
		backoff = minBackoff
		if !hasWork {
			continue
		}
		if envelope.Type != "operation_target" {
			log.Debug().Str("type", envelope.Type).Msg("ignoring unsupported remote work envelope")
			continue
		}
		if envelope.Operation == nil {
			log.Warn().Msg("ignoring remote operation envelope without operation")
			continue
		}

		m.dispatchOperation(ctx, &workers, envelope.Operation)
	}
}

// dispatchOperation delivers one operation envelope: synchronously if the
// single execution slot is already taken (a busy rejection), or in its own
// tracked goroutine once the slot is acquired.
//
// A busy rejection deliberately gets no goroutine: it's a quick, bounded
// round trip (a DB write or two plus one or two HTTP posts), and handling it
// inline naturally caps how fast the next dispatch can happen to that round
// trip's own duration. A server that answers /wait instantly every time (a
// misbehaving or compromised RemoteControlBaseURL, squarely within this
// feature's threat model) would otherwise let busy dispatches spawn
// goroutines without bound, since executionSlot only gates the one real
// execution slot, not dispatch itself.
func (m *manager) dispatchOperation(ctx context.Context, workers *sync.WaitGroup, operation *operationEnvelope) {
	select {
	case m.executionSlot <- struct{}{}:
	default:
		m.handleOperation(ctx, operation, true)
		return
	}
	workers.Add(1)
	go func(op operationEnvelope) {
		defer workers.Done()
		defer func() { <-m.executionSlot }()
		m.handleOperation(ctx, &op, false)
	}(*operation)
}

func (m *manager) sendCapabilityHeartbeat(ctx context.Context) error {
	capabilities := make(map[string]any)
	if sameEndpoint(
		m.deps.Config.RemoteControlBaseURL(), m.deps.Config.BackupRemoteBaseURL(),
	) {
		capabilities["backup"] = 1
	}
	if m.deps.Config.RemoteControlEnabled() {
		capabilities["remote_operations"] = map[string]any{"version": 1, "enabled": true}
	}
	body := map[string]any{
		"core_version": config.AppVersion,
		"capabilities": capabilities,
	}
	if err := m.doJSON(ctx, http.MethodPost, "/v1/device/heartbeat", body, nil); err != nil {
		return fmt.Errorf("send remote operations capability heartbeat: %w", err)
	}
	return nil
}

func (m *manager) markUnlinkedIfSharedEndpoint() {
	if !sameEndpoint(
		m.deps.Config.RemoteControlBaseURL(), m.deps.Config.BackupRemoteBaseURL(),
	) {
		return
	}
	backup.NewManager(m.deps.Config, m.deps.Platform, m.deps.DB).
		WithCoordinator(m.deps.State.BackupCoordinator()).MarkRemoteUnlinked()
}

func isUnauthorized(err error) bool {
	if errors.Is(err, errUnauthorized) {
		return true
	}
	var httpErr *httpError
	return errors.As(err, &httpErr) && httpErr.status == http.StatusUnauthorized
}

func sameEndpoint(first, second string) bool {
	firstURL, firstErr := url.Parse(strings.TrimRight(first, "/"))
	secondURL, secondErr := url.Parse(strings.TrimRight(second, "/"))
	if firstErr != nil || secondErr != nil {
		return false
	}
	firstURL.Scheme = strings.ToLower(firstURL.Scheme)
	firstURL.Host = strings.ToLower(firstURL.Host)
	secondURL.Scheme = strings.ToLower(secondURL.Scheme)
	secondURL.Host = strings.ToLower(secondURL.Host)
	return firstURL.String() == secondURL.String()
}

func (m *manager) deviceBearer() string {
	baseURL := strings.TrimRight(m.deps.Config.RemoteControlBaseURL(), "/")
	entry := config.LookupAuth(config.GetAuthCfg(), config.RemoteAuthLookupURL(baseURL))
	if entry == nil {
		return ""
	}
	return entry.Bearer
}

func (m *manager) waitOnce(
	ctx context.Context, bearer string,
) (waitEnvelope, bool, error) {
	var envelope waitEnvelope
	requestCtx, cancel := context.WithTimeout(ctx, 40*time.Second)
	eligibilityDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-eligibilityDone:
				return
			case <-requestCtx.Done():
				return
			case <-ticker.C:
				if !m.deps.Config.RemoteControlEnabled() || m.deviceBearer() != bearer {
					cancel()
					return
				}
			}
		}
	}()
	defer func() {
		close(eligibilityDone)
		cancel()
	}()
	endpoint, err := buildEndpoint(
		m.deps.Config.RemoteControlBaseURL(),
		"/v1/device/remote-sessions/wait?timeout="+strconv.Itoa(waitTimeoutSeconds),
	)
	if err != nil {
		return envelope, false, err
	}
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return envelope, false, fmt.Errorf("build remote operations wait: %w", err)
	}
	m.setRequestHeaders(req, bearer, false)
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return envelope, false, fmt.Errorf("send remote operations wait: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Debug().Err(closeErr).Msg("failed to close remote operations wait response")
		}
	}()
	switch resp.StatusCode {
	case http.StatusOK:
		if err := decodeBoundedJSON(resp.Body, 64<<10, &envelope); err != nil {
			return envelope, false, fmt.Errorf("decode remote operations wait: %w", err)
		}
		return envelope, true, nil
	case http.StatusNoContent:
		return envelope, false, nil
	case http.StatusUnauthorized:
		return envelope, false, errUnauthorized
	default:
		return envelope, false, decodeHTTPError(resp)
	}
}

func (m *manager) sleepWhileEligible(ctx context.Context, duration time.Duration, requireEnabled bool) {
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		remaining := time.Until(deadline)
		timer := time.NewTimer(min(remaining, time.Second))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if requireEnabled && (!m.deps.Config.RemoteControlEnabled() || m.deviceBearer() == "") {
			return
		}
	}
}

func resultsReplayDue(replayed bool, lastReplay, now time.Time) bool {
	return !replayed || now.Sub(lastReplay) >= resultReplayInterval
}

func (*manager) jitter(duration time.Duration) time.Duration {
	if duration <= time.Millisecond {
		return duration
	}
	floor := duration / 2
	span := duration - floor
	random, err := cryptorand.Int(cryptorand.Reader, big.NewInt(int64(span)))
	if err != nil {
		return duration
	}
	return floor + time.Duration(random.Int64())
}

// requestEnv builds the RequestEnv for a dispatched API method call. role
// must never be empty: requests.RequestEnv.ClientRole == "" combined with
// IsLocal == false resolves to admin under permissions.Grant.EffectiveRole
// (the unpaired-remote-is-admin backward-compatibility rule) — every remote
// operation must carry an explicit, capability-empty role instead. See
// permissions.RoleRemote.
func (m *manager) requestEnv(ctx context.Context, role permissions.Role, params json.RawMessage) requests.RequestEnv {
	if role == "" {
		role = permissions.RoleRemote
	}
	return requests.RequestEnv{
		Context: ctx, Platform: m.deps.Platform, Config: m.deps.Config, State: m.deps.State,
		Database: m.deps.DB, Profiles: m.deps.Profiles, LauncherCache: helpers.GlobalLauncherCache,
		PlaybackManager: m.deps.PlaybackManager, UI: m.deps.UI, TokenQueue: nil,
		ConfirmQueue: m.deps.ConfirmQueue, IndexPauser: m.deps.IndexPauser, ScrapePauser: m.deps.ScrapePauser,
		BackupPauser: m.deps.BackupPauser, Params: params, ClientRole: string(role), IsLocal: false,
	}
}
