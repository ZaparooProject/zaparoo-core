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
	"errors"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/notifications"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/jellyfin"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog/log"
)

const (
	// jellyfinLinkInterval is how often a Jellyfin link asks whether the code
	// has been entered.
	jellyfinLinkInterval = 3 * time.Second
	// jellyfinLinkTTL matches how long Jellyfin keeps a Quick Connect request.
	jellyfinLinkTTL = 10 * time.Minute
)

// jellyfinLinkClient is the part of the Jellyfin client the link flow uses,
// so tests can point it at a fake server.
type jellyfinLinkClient interface {
	BaseURL() string
	VerificationURL() string
	QuickConnectEnabled(ctx context.Context) (bool, error)
	InitiateQuickConnect(ctx context.Context) (*jellyfin.QuickConnect, error)
	QuickConnectState(ctx context.Context, secret string) (*jellyfin.QuickConnect, error)
	AuthenticateWithQuickConnect(ctx context.Context, secret string) (string, error)
}

// newJellyfinLinkClient builds the client a Jellyfin link uses; tests replace it.
var newJellyfinLinkClient = func(serverURL, deviceID string) (jellyfinLinkClient, error) {
	return jellyfin.NewLinkClient(serverURL, deviceID)
}

const quickConnectDisabledMsg = "Quick Connect is disabled on this Jellyfin server; " +
	"enable it in Dashboard → General"

// jellyfinLinkClock times a Jellyfin link's polls and expiry; tests replace it.
var jellyfinLinkClock = clockwork.NewRealClock()

// startJellyfinAuthLink links Core to a Jellyfin server with Quick Connect.
// The user enters the code in a signed-in Jellyfin app. On approval the token
// is stored in auth.toml for the server's URL and the server becomes the
// Jellyfin launchers' server_url.
//
// It makes Core contact a server the caller names and changes what Core
// indexes and plays, so only a local or admin client may start it.
func startJellyfinAuthLink(env *requests.RequestEnv, serverURL string) (any, error) {
	if !isLocalOrAdmin(env) {
		return nil, models.ClientErrf("linking a Jellyfin server requires a local or admin client")
	}
	if strings.TrimSpace(serverURL) == "" {
		return nil, models.ClientErrf("url is required to link a Jellyfin server")
	}
	client, err := newJellyfinLinkClient(serverURL, jellyfin.DeviceID(env.Config))
	if err != nil {
		return nil, models.ClientErr(remoteRequestError("invalid Jellyfin server URL", err))
	}

	if startErr := beginAuthLinkStart(); startErr != nil {
		return nil, startErr
	}
	enabled, err := client.QuickConnectEnabled(env.Context)
	if err == nil && !enabled {
		err = jellyfin.ErrQuickConnectDisabled
	}
	var qc *jellyfin.QuickConnect
	if err == nil {
		qc, err = client.InitiateQuickConnect(env.Context)
	}
	if errors.Is(err, jellyfin.ErrQuickConnectDisabled) {
		finishAuthLinkStart(nil)
		return nil, models.ClientErrf("%s", quickConnectDisabledMsg)
	}
	if err != nil {
		finishAuthLinkStart(nil)
		log.Warn().Err(err).Msg("jellyfin link request failed")
		return nil, remoteRequestError("failed to start Jellyfin link", err)
	}

	clock := jellyfinLinkClock
	expiresAt := clock.Now().Add(jellyfinLinkTTL)
	status := models.AuthLinkStatusResponse{
		Status:          models.AuthLinkStatusPending,
		Provider:        models.AuthLinkProviderJellyfin,
		UserCode:        qc.Code,
		VerificationURL: client.VerificationURL(),
		ExpiresAt:       &expiresAt,
	}

	appCtx := context.Background()
	if env.State != nil {
		appCtx = env.State.GetContext()
	}
	// The poll loop ends the link at expiresAt on the link clock; the context
	// deadline only backs it up.
	//nolint:gosec // G118: session and polling goroutine both invoke the shared cancel function.
	linkCtx, cancel := context.WithTimeout(appCtx, jellyfinLinkTTL+jellyfinLinkInterval)
	deps := &authLinkDeps{cfg: env.Config, db: env.Database, pl: env.Platform}
	if env.State != nil {
		deps.ns = env.State.Notifications
	}
	session := &authLinkSession{cancel: cancel, status: status}
	finishAuthLinkStart(session)
	if deps.ns != nil {
		payload := status
		redactAuthLinkStatus(&payload)
		notifications.AuthLinkStatus(deps.ns, &payload)
	}
	go func() {
		defer cancel()
		pollJellyfinLink(linkCtx, session, deps, client, clock, qc.Secret, expiresAt)
	}()

	log.Info().Msg("settings.auth.link started for a Jellyfin server")
	return status, nil
}

// pollJellyfinLink polls a Quick Connect request until it is approved,
// expires or is cancelled.
func pollJellyfinLink(
	ctx context.Context,
	session *authLinkSession,
	deps *authLinkDeps,
	client jellyfinLinkClient,
	clock clockwork.Clock,
	secret string,
	expiresAt time.Time,
) {
	const expiredMsg = "link request expired, start over to link this device"
	consecutiveFailures := 0
	for {
		select {
		case <-ctx.Done():
			errMsg := "device linking stopped, start over to link this device"
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				errMsg = expiredMsg
			}
			finishAuthLink(session, deps, models.AuthLinkStatusFailed, errMsg)
			return
		case <-clock.After(jellyfinLinkInterval):
		}
		if !clock.Now().Before(expiresAt) {
			log.Warn().Int("poll_failures", consecutiveFailures).Msg("jellyfin link polling expired")
			finishAuthLink(session, deps, models.AuthLinkStatusFailed, expiredMsg)
			return
		}

		state, err := client.QuickConnectState(ctx, secret)
		switch {
		case err == nil && state.Authenticated:
			token, authErr := client.AuthenticateWithQuickConnect(ctx, secret)
			if authErr != nil {
				log.Error().Err(authErr).Msg("jellyfin link approved but no token was issued")
				finishAuthLink(session, deps, models.AuthLinkStatusFailed,
					"linking failed, Jellyfin did not issue a token")
				return
			}
			if saveErr := saveJellyfinLink(deps.cfg, client.BaseURL(), token); saveErr != nil {
				log.Error().Err(saveErr).Msg("jellyfin link approved but not saved")
				finishAuthLink(session, deps, models.AuthLinkStatusFailed,
					"linking failed, the Jellyfin token could not be saved")
				return
			}
			log.Info().Msg("settings.auth.link completed for a Jellyfin server")
			finishAuthLink(session, deps, models.AuthLinkStatusApproved, "")
			return
		case err == nil:
			consecutiveFailures = 0
		case errors.Is(err, jellyfin.ErrQuickConnectExpired):
			finishAuthLink(session, deps, models.AuthLinkStatusFailed, expiredMsg)
			return
		case errors.Is(err, jellyfin.ErrQuickConnectDisabled):
			finishAuthLink(session, deps, models.AuthLinkStatusFailed, quickConnectDisabledMsg)
			return
		default:
			if ctx.Err() != nil {
				continue
			}
			consecutiveFailures++
			logDeviceLinkPollFailure(err, consecutiveFailures)
		}
	}
}

// saveJellyfinLink stores the token for the server's URL and makes the server
// the Jellyfin launchers'. The entry is named after the launcher group, so
// all six launchers pick it up.
func saveJellyfinLink(cfg *config.Instance, baseURL, token string) error {
	if cfg == nil {
		return errors.New("no config")
	}
	if err := cfg.SaveAuthEntry(baseURL, config.CredentialEntry{Bearer: token}); err != nil {
		return err //nolint:wrapcheck // the config error already says what failed
	}
	cfg.SetLauncherServerURL(shared.LauncherGroupJellyfin, baseURL)
	if err := cfg.Save(); err != nil {
		return err //nolint:wrapcheck // the config error already says what failed
	}
	return nil
}
