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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/notifications"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
	"github.com/rs/zerolog/log"
)

// Reverse device link flow (RFC 8628 device-authorization style): the device
// starts a link request, displays a user code / QR URL, and polls until the
// user approves it in Zaparoo Online. On approval, the poll returns a claim
// token that goes through the same redemption pipeline as the forward flow.

const (
	// deviceLinkDefaultBaseURL is the auth server the link flow targets when
	// no explicit url param is given. Linking is an account concern, so this
	// is fixed to the official API rather than inheriting the backup base
	// URL from config; local development passes url explicitly.
	deviceLinkDefaultBaseURL  = "https://api.zaparoo.com"
	deviceLinkCreatePath      = "/v1/device-link-requests"
	deviceLinkPollPath        = "/v1/device-link-requests/poll"
	deviceLinkDefaultInterval = 5 * time.Second
	deviceLinkDefaultTTL      = 10 * time.Minute
)

//nolint:tagliatelle // Remote API contract uses snake_case JSON fields.
type deviceLinkCreateResponse struct {
	ExpiresAt               time.Time `json:"expires_at"`
	DeviceCode              string    `json:"device_code"`
	UserCode                string    `json:"user_code"`
	VerificationURL         string    `json:"verification_url"`
	VerificationURLComplete string    `json:"verification_url_complete"`
	Interval                int       `json:"interval"`
}

//nolint:tagliatelle // Remote API contract uses snake_case JSON fields.
type deviceLinkPollRequest struct {
	DeviceCode string `json:"device_code"`
}

//nolint:tagliatelle // Remote API contract uses snake_case JSON fields.
type deviceLinkPollResponse struct {
	Status   string `json:"status"`
	Token    string `json:"token,omitempty"`
	ClaimURL string `json:"claim_url,omitempty"`
	Interval int    `json:"interval"`
}

// authLinkSession is the single active reverse-link flow. The device code is
// held by the polling goroutine only and never exposed through the API.
type authLinkSession struct {
	cancel context.CancelFunc
	status models.AuthLinkStatusResponse
}

var (
	authLinkMu      syncutil.Mutex
	activeAuthLink  *authLinkSession
	errNoAuthListen = errors.New("no active link request")
)

// authLinkDeps are the long-lived dependencies the polling goroutine
// captures; all of them outlive the originating API request.
type authLinkDeps struct {
	cfg     *config.Instance
	db      *database.Database
	pl      platforms.Platform
	ns      chan<- models.Notification
	fetchWK wellKnownFetcher
}

// HandleSettingsAuthLink starts a reverse link flow against the official
// Zaparoo API (or an explicit url param) and returns the user code and
// verification URLs to display. Progress is pushed via auth.link.status
// notifications and pollable via settings.auth.link.status.
//
//nolint:gocritic // API dispatch requires RequestEnv by value.
func HandleSettingsAuthLink(env requests.RequestEnv, fetchWK wellKnownFetcher) (any, error) {
	if !env.IsLocal {
		return nil, models.ClientErrf("device linking requires a local client")
	}
	var params models.SettingsAuthLinkParams
	if len(env.Params) > 0 {
		if err := json.Unmarshal(env.Params, &params); err != nil {
			return nil, models.ClientErrf("invalid params: %w", err)
		}
	}
	baseURL := params.URL
	if baseURL == "" {
		baseURL = deviceLinkDefaultBaseURL
	}
	if err := config.ValidateBackupRemoteBaseURL(baseURL); err != nil {
		return nil, models.ClientErrf("invalid link URL: %w", err)
	}
	baseURL = strings.TrimRight(baseURL, "/")

	created, err := createDeviceLinkRequest(env.Context, baseURL, env.Platform.ID(), env.Config.DeviceID())
	if err != nil {
		return nil, err
	}

	interval := time.Duration(created.Interval) * time.Second
	if interval <= 0 {
		interval = deviceLinkDefaultInterval
	}
	expiresAt := created.ExpiresAt
	if expiresAt.IsZero() {
		expiresAt = time.Now().Add(deviceLinkDefaultTTL)
	}

	status := models.AuthLinkStatusResponse{
		Status:                  models.AuthLinkStatusPending,
		UserCode:                created.UserCode,
		VerificationURL:         created.VerificationURL,
		VerificationURLComplete: created.VerificationURLComplete,
		ExpiresAt:               &expiresAt,
	}

	// The flow must outlive this request: derive from the app context, with
	// a deadline just past the link request's server-side expiry.
	appCtx := context.Background()
	if env.State != nil {
		appCtx = env.State.GetContext()
	}
	linkCtx, cancel := context.WithDeadline(appCtx, expiresAt.Add(interval))

	deps := &authLinkDeps{
		cfg:     env.Config,
		db:      env.Database,
		pl:      env.Platform,
		fetchWK: fetchWK,
	}
	if env.State != nil {
		deps.ns = env.State.Notifications
	}

	authLinkMu.Lock()
	if activeAuthLink != nil && activeAuthLink.cancel != nil {
		activeAuthLink.cancel()
	}
	activeAuthLink = &authLinkSession{cancel: cancel, status: status}
	authLinkMu.Unlock()

	session := activeAuthLink
	go pollDeviceLink(linkCtx, session, deps, baseURL, created.DeviceCode, interval)

	log.Info().Str("user_code", created.UserCode).Msg("settings.auth.link started")
	return status, nil
}

// HandleSettingsAuthLinkStatus returns the current link flow state.
//
//nolint:gocritic // API dispatch requires RequestEnv by value.
func HandleSettingsAuthLinkStatus(_ requests.RequestEnv) (any, error) {
	authLinkMu.Lock()
	defer authLinkMu.Unlock()
	if activeAuthLink == nil {
		return models.AuthLinkStatusResponse{Status: models.AuthLinkStatusNone}, nil
	}
	return activeAuthLink.status, nil
}

// HandleSettingsAuthLinkCancel stops the active link flow, if any.
//
//nolint:gocritic // API dispatch requires RequestEnv by value.
func HandleSettingsAuthLinkCancel(env requests.RequestEnv) (any, error) {
	if !env.IsLocal {
		return nil, models.ClientErrf("device linking requires a local client")
	}
	authLinkMu.Lock()
	defer authLinkMu.Unlock()
	if activeAuthLink == nil || activeAuthLink.status.Status != models.AuthLinkStatusPending {
		return models.AuthLinkStatusResponse{Status: models.AuthLinkStatusNone}, errNoAuthListen
	}
	activeAuthLink.cancel()
	activeAuthLink.status.Status = models.AuthLinkStatusCancelled
	return activeAuthLink.status, nil
}

// createDeviceLinkRequest starts a link request on the auth server.
func createDeviceLinkRequest(
	ctx context.Context,
	baseURL, platform, deviceHint string,
) (*deviceLinkCreateResponse, error) {
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, baseURL+deviceLinkCreatePath, http.NoBody,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create link request: %w", err)
	}
	req.Header.Set(zapscript.HeaderZaparooOS, runtime.GOOS)
	req.Header.Set(zapscript.HeaderZaparooArch, runtime.GOARCH)
	req.Header.Set(zapscript.HeaderZaparooPlatform, platform)
	if deviceHint != "" {
		req.Header.Set(headerZaparooDeviceHint, deviceHint)
	}

	resp, err := claimClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to contact link server: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Debug().Err(closeErr).Msg("error closing link response body")
		}
	}()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, helpers.MaxResponseBodySize))
		return nil, fmt.Errorf(
			"link server returned status %d: %s",
			resp.StatusCode, strings.TrimSpace(string(body)),
		)
	}

	var created deviceLinkCreateResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, helpers.MaxResponseBodySize)).Decode(&created); err != nil {
		return nil, fmt.Errorf("failed to decode link response: %w", err)
	}
	if created.DeviceCode == "" || created.UserCode == "" {
		return nil, errors.New("link response missing device or user code")
	}
	return &created, nil
}

// pollDeviceLink polls the link request until it is approved, expires, or is
// cancelled. On approval the returned claim token is redeemed through the
// same pipeline as the forward flow.
func pollDeviceLink(
	ctx context.Context,
	session *authLinkSession,
	deps *authLinkDeps,
	baseURL, deviceCode string,
	interval time.Duration,
) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Deadline (request expired) or superseded/cancelled.
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				finishAuthLink(session, deps, models.AuthLinkStatusFailed,
					"link request expired, start over to link this device")
			}
			return
		case <-ticker.C:
		}

		poll, err := pollDeviceLinkOnce(ctx, baseURL, deviceCode)
		if err != nil {
			var terminal *authLinkTerminalError
			if errors.As(err, &terminal) {
				finishAuthLink(session, deps, models.AuthLinkStatusFailed, terminal.reason)
				return
			}
			// Transient (network) errors: keep polling until expiry.
			log.Debug().Err(err).Msg("device link poll failed, retrying")
			continue
		}
		if poll.Status != "approved" {
			continue
		}

		_, err = performClaim(ctx, deps.cfg, deps.db, deps.pl, poll.ClaimURL, poll.Token, deps.fetchWK)
		if err != nil {
			log.Warn().Err(err).Msg("device link claim redemption failed")
			finishAuthLink(session, deps, models.AuthLinkStatusFailed,
				"linking failed, start over to link this device")
			return
		}
		log.Info().Msg("settings.auth.link completed")
		finishAuthLink(session, deps, models.AuthLinkStatusApproved, "")
		return
	}
}

// authLinkTerminalError marks poll failures that end the flow (as opposed to
// transient network errors, which are retried until expiry).
type authLinkTerminalError struct {
	reason string
}

func (e *authLinkTerminalError) Error() string { return e.reason }

func pollDeviceLinkOnce(ctx context.Context, baseURL, deviceCode string) (*deviceLinkPollResponse, error) {
	body, err := json.Marshal(deviceLinkPollRequest{DeviceCode: deviceCode})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal poll request: %w", err)
	}
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, baseURL+deviceLinkPollPath, bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create poll request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := claimClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to contact link server: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Debug().Err(closeErr).Msg("error closing poll response body")
		}
	}()

	switch resp.StatusCode {
	case http.StatusOK:
		var poll deviceLinkPollResponse
		if err := json.NewDecoder(io.LimitReader(resp.Body, helpers.MaxResponseBodySize)).Decode(&poll); err != nil {
			return nil, fmt.Errorf("failed to decode poll response: %w", err)
		}
		return &poll, nil
	case http.StatusUnauthorized:
		// Expired, never created, or the claim was already collected: the
		// request is consumed server-side and the flow must start over.
		return nil, &authLinkTerminalError{reason: "link request expired, start over to link this device"}
	case http.StatusTooManyRequests:
		return nil, errors.New("link server rate limited poll")
	default:
		return nil, fmt.Errorf("link server returned status %d", resp.StatusCode)
	}
}

// finishAuthLink records a terminal state and notifies clients, unless the
// session was already superseded by a newer one.
func finishAuthLink(session *authLinkSession, deps *authLinkDeps, status, errMsg string) {
	authLinkMu.Lock()
	if activeAuthLink == session {
		activeAuthLink.status.Status = status
		activeAuthLink.status.Error = errMsg
	}
	notify := activeAuthLink == session
	payload := session.status
	payload.Status = status
	payload.Error = errMsg
	authLinkMu.Unlock()

	if notify && deps.ns != nil {
		notifications.AuthLinkStatus(deps.ns, &payload)
	}
}
