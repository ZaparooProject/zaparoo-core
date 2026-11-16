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
	"os"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/notifications"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/romm"
	"github.com/rs/zerolog/log"
)

// rommSlowDownStep is how much longer a RomM link waits between polls each
// time the server says it is polling too fast, as RFC 8628 asks.
const rommSlowDownStep = 5 * time.Second

// rommLinkClient is the part of the RomM client the link flow uses, so tests
// can drive it without a server.
type rommLinkClient interface {
	BaseURL() string
	StartDeviceLink(ctx context.Context, req romm.DeviceLinkRequest) (*romm.DeviceLink, error)
	PollDeviceToken(ctx context.Context, deviceCode string) (string, error)
}

// newRomMLinkClient builds the client a RomM link uses; tests replace it.
var newRomMLinkClient = func(serverURL string) (rommLinkClient, error) {
	return romm.NewClient(serverURL)
}

// startRomMAuthLink links Core to a RomM server with RomM's device flow. On
// approval the token is stored in auth.toml for the server's URL and the
// server becomes the RomM launcher's server_url.
//
// Unlike a Zaparoo link, it changes where Core gets games from and makes Core
// contact a server the caller names, so only a local or admin client may
// start it.
func startRomMAuthLink(env *requests.RequestEnv, serverURL string) (any, error) {
	if !isLocalOrAdmin(env) {
		return nil, models.ClientErrf("linking a RomM server requires a local or admin client")
	}
	if strings.TrimSpace(serverURL) == "" {
		return nil, models.ClientErrf("url is required to link a RomM server")
	}
	client, err := newRomMLinkClient(serverURL)
	if err != nil {
		return nil, models.ClientErr(remoteRequestError("invalid RomM server URL", err))
	}

	if startErr := beginAuthLinkStart(); startErr != nil {
		return nil, startErr
	}
	link, err := client.StartDeviceLink(env.Context, rommDeviceLinkRequest(env))
	if err != nil {
		finishAuthLinkStart(nil)
		log.Warn().Err(err).Msg("romm link request failed")
		return nil, remoteRequestError("failed to start RomM link", err)
	}

	interval := time.Duration(link.Interval) * time.Second
	if interval <= 0 {
		interval = deviceLinkDefaultInterval
	}
	ttl := time.Duration(link.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = deviceLinkDefaultTTL
	}
	expiresAt := time.Now().Add(ttl)
	status := models.AuthLinkStatusResponse{
		Status:                  models.AuthLinkStatusPending,
		Provider:                models.AuthLinkProviderRomM,
		UserCode:                link.UserCode,
		VerificationURL:         link.VerificationURL,
		VerificationURLComplete: link.VerificationURLComplete,
		ExpiresAt:               &expiresAt,
	}

	appCtx := context.Background()
	if env.State != nil {
		appCtx = env.State.GetContext()
	}
	//nolint:gosec // G118: session and polling goroutine both invoke the shared cancel function.
	linkCtx, cancel := context.WithDeadline(appCtx, expiresAt.Add(interval))
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
		pollRomMLink(linkCtx, session, deps, client, link.DeviceCode, interval)
	}()

	log.Info().Msg("settings.auth.link started for a RomM server")
	return status, nil
}

// rommDeviceLinkRequest names this device to RomM. The identifier is Core's
// device ID, so linking again updates the device RomM already lists.
func rommDeviceLinkRequest(env *requests.RequestEnv) romm.DeviceLinkRequest {
	req := romm.DeviceLinkRequest{Version: config.AppVersion}
	if env.Platform != nil {
		req.Platform = env.Platform.ID()
	}
	if env.Config != nil {
		req.Identifier = env.Config.DeviceID()
	}
	hostname, err := os.Hostname()
	if err != nil || strings.TrimSpace(hostname) == "" {
		hostname = "Zaparoo Core"
	}
	req.Name = hostname
	if req.Identifier == "" {
		req.Identifier = strings.TrimSpace(hostname + " " + req.Platform)
	}
	return req
}

// pollRomMLink polls a RomM link until it is approved, refused, expires or
// is cancelled.
func pollRomMLink(
	ctx context.Context,
	session *authLinkSession,
	deps *authLinkDeps,
	client rommLinkClient,
	deviceCode string,
	interval time.Duration,
) {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	consecutiveFailures := 0
	for {
		select {
		case <-ctx.Done():
			errMsg := "device linking stopped, start over to link this device"
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				errMsg = "link request expired, start over to link this device"
				log.Warn().Int("poll_failures", consecutiveFailures).Msg("romm link polling expired")
			}
			finishAuthLink(session, deps, models.AuthLinkStatusFailed, errMsg)
			return
		case <-timer.C:
		}

		token, err := client.PollDeviceToken(ctx, deviceCode)
		switch {
		case err == nil:
			if saveErr := saveRomMLink(deps.cfg, client.BaseURL(), token); saveErr != nil {
				log.Error().Err(saveErr).Msg("romm link approved but not saved")
				finishAuthLink(session, deps, models.AuthLinkStatusFailed,
					"linking failed, the RomM token could not be saved")
				return
			}
			log.Info().Msg("settings.auth.link completed for a RomM server")
			finishAuthLink(session, deps, models.AuthLinkStatusApproved, "")
			return
		case errors.Is(err, romm.ErrLinkPending):
			consecutiveFailures = 0
		case errors.Is(err, romm.ErrLinkSlowDown):
			interval += rommSlowDownStep
		case errors.Is(err, romm.ErrLinkDenied):
			log.Info().Msg("romm link request denied")
			finishAuthLink(session, deps, models.AuthLinkStatusFailed, "the link request was denied in RomM")
			return
		case errors.Is(err, romm.ErrLinkExpired):
			finishAuthLink(session, deps, models.AuthLinkStatusFailed,
				"link request expired, start over to link this device")
			return
		default:
			if ctx.Err() != nil {
				continue
			}
			consecutiveFailures++
			logDeviceLinkPollFailure(err, consecutiveFailures)
		}
		timer.Reset(interval)
	}
}

// saveRomMLink stores the token for the server's URL and makes the server
// the RomM launcher's.
func saveRomMLink(cfg *config.Instance, baseURL, token string) error {
	if cfg == nil {
		return errors.New("no config")
	}
	if err := cfg.SaveAuthEntry(baseURL, config.CredentialEntry{Bearer: token}); err != nil {
		return err //nolint:wrapcheck // the config error already says what failed
	}
	cfg.SetLauncherServerURL(romm.LauncherID, baseURL)
	if err := cfg.Save(); err != nil {
		return err //nolint:wrapcheck // the config error already says what failed
	}
	return nil
}
