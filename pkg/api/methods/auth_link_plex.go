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
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/notifications"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/plex"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog/log"
)

const (
	// plexLinkPollInterval is how often a pending Plex PIN is checked. plex.tv
	// gives no interval of its own.
	plexLinkPollInterval = 2 * time.Second
	plexLinkURL          = "https://plex.tv/link"
)

// plexLinkClient is the part of the plex.tv client the link flow uses.
type plexLinkClient interface {
	ClientsURL() string
	CreatePin(ctx context.Context) (*plex.Pin, error)
	CheckPin(ctx context.Context, id int64) (*plex.Pin, error)
	Resources(ctx context.Context, token string) ([]plex.Resource, error)
}

// newPlexLinkClient and plexLinkClock are replaced in tests.
var (
	newPlexLinkClient = func(clientID string) plexLinkClient {
		return plex.NewTVClient("", "", clientID, nil)
	}
	plexLinkClock = clockwork.NewRealClock()
)

// startPlexAuthLink links Core to a Plex account with a plex.tv PIN. On
// approval the server's token and the account token are stored in auth.toml
// and the server becomes the Plex launchers' server_url. With several
// servers on the account, url picks one.
//
// It changes where Core plays media from, so only a local or admin client
// may start it.
func startPlexAuthLink(env *requests.RequestEnv, rawURL string) (any, error) {
	if !isLocalOrAdmin(env) {
		return nil, models.ClientErrf("linking a Plex account requires a local or admin client")
	}
	serverURL, err := parsePlexServerURL(rawURL)
	if err != nil {
		return nil, models.ClientErr(err)
	}
	client := newPlexLinkClient(plex.ClientIdentifier(env.Config))

	if startErr := beginAuthLinkStart(); startErr != nil {
		return nil, startErr
	}
	pin, err := client.CreatePin(env.Context)
	if err != nil {
		finishAuthLinkStart(nil)
		log.Warn().Err(err).Msg("plex link request failed")
		return nil, remoteRequestError("failed to start Plex link", err)
	}

	clock := plexLinkClock
	now := clock.Now()
	ttl := time.Duration(pin.ExpiresIn) * time.Second
	if !pin.ExpiresAt.IsZero() && pin.ExpiresAt.After(now) {
		ttl = pin.ExpiresAt.Sub(now)
	}
	if ttl <= 0 {
		ttl = deviceLinkDefaultTTL
	}
	expiresAt := now.Add(ttl)
	status := models.AuthLinkStatusResponse{
		Status:          models.AuthLinkStatusPending,
		Provider:        models.AuthLinkProviderPlex,
		UserCode:        pin.Code,
		VerificationURL: plexLinkURL,
		ExpiresAt:       &expiresAt,
	}

	appCtx := context.Background()
	if env.State != nil {
		appCtx = env.State.GetContext()
	}
	//nolint:gosec // G118: session and polling goroutine both invoke the shared cancel function.
	linkCtx, cancel := context.WithDeadline(appCtx, time.Now().Add(ttl+plexLinkPollInterval))
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
		pollPlexLink(linkCtx, session, deps, client, clock, int64(pin.ID), expiresAt, serverURL)
	}()

	log.Info().Msg("settings.auth.link started for a Plex account")
	return status, nil
}

// parsePlexServerURL checks an optional server URL. Empty means pick the
// account's only server.
func parsePlexServerURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	normalized := plex.NormalizeURL(raw)
	u, err := url.Parse(normalized)
	if err != nil || u.Hostname() == "" {
		return "", errors.New("invalid Plex server URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", errors.New("plex server URL must use http or https")
	}
	if u.User != nil {
		return "", errors.New("plex server URL must not contain credentials")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("plex server URL must not contain a query")
	}
	return normalized, nil
}

// pollPlexLink checks the PIN until the user enters it, it expires or the
// link is cancelled.
func pollPlexLink(
	ctx context.Context,
	session *authLinkSession,
	deps *authLinkDeps,
	client plexLinkClient,
	clock clockwork.Clock,
	pinID int64,
	expiresAt time.Time,
	serverURL string,
) {
	const expiredMsg = "link code expired, start over to link this device"
	timer := clock.NewTimer(plexLinkPollInterval)
	defer timer.Stop()
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
		case <-timer.Chan():
		}
		if clock.Now().After(expiresAt) {
			finishAuthLink(session, deps, models.AuthLinkStatusFailed, expiredMsg)
			return
		}

		pin, err := client.CheckPin(ctx, pinID)
		switch {
		case errors.Is(err, plex.ErrPinExpired):
			finishAuthLink(session, deps, models.AuthLinkStatusFailed, expiredMsg)
			return
		case err != nil:
			if ctx.Err() != nil {
				continue
			}
			consecutiveFailures++
			logDeviceLinkPollFailure(err, consecutiveFailures)
		case pin.AuthToken != "":
			if msg := completePlexLink(ctx, deps.cfg, client, pin.AuthToken, serverURL); msg != "" {
				finishAuthLink(session, deps, models.AuthLinkStatusFailed, msg)
				return
			}
			log.Info().Msg("settings.auth.link completed for a Plex account")
			finishAuthLink(session, deps, models.AuthLinkStatusApproved, "")
			return
		default:
			consecutiveFailures = 0
		}
		timer.Reset(plexLinkPollInterval)
	}
}

// completePlexLink picks the server and stores the tokens. It returns the
// message to show when linking cannot finish.
func completePlexLink(
	ctx context.Context, cfg *config.Instance, client plexLinkClient, accountToken, serverURL string,
) string {
	resources, err := client.Resources(ctx, accountToken)
	if err != nil {
		log.Warn().Err(err).Msg("plex link approved but servers could not be listed")
		return "linking failed, the Plex servers on the account could not be listed"
	}
	server, connURL, msg := selectPlexServer(resources, serverURL)
	if msg != "" {
		return msg
	}
	serverToken := server.AccessToken
	if serverToken == "" {
		serverToken = accountToken
	}
	if err := savePlexLink(cfg, client.ClientsURL(), connURL, serverToken, accountToken); err != nil {
		log.Error().Err(err).Msg("plex link approved but not saved")
		return "linking failed, the Plex token could not be saved"
	}
	return ""
}

// selectPlexServer picks the server to link: the one serverURL names, or the
// account's only one. Only servers the account owns can be linked, since a
// shared server's token cannot see the libraries the launchers index.
func selectPlexServer(
	resources []plex.Resource, serverURL string,
) (server *plex.Resource, connURL, failure string) {
	var servers []*plex.Resource
	for i := range resources {
		if resources[i].ProvidesRole("server") {
			servers = append(servers, &resources[i])
		}
	}
	if len(servers) == 0 {
		return nil, "", "linking failed, the Plex account has no servers"
	}

	if serverURL != "" {
		for _, s := range servers {
			if !plexServerHasURL(s, serverURL) {
				continue
			}
			if !s.Owned {
				return nil, "", fmt.Sprintf(
					"linking failed, the Plex account is not the admin of %q; sign in as its owner", s.Name)
			}
			return s, serverURL, ""
		}
		return nil, "", fmt.Sprintf("linking failed, no Plex server on the account answers at %s; %s",
			serverURL, describePlexServers(servers))
	}

	owned := make([]*plex.Resource, 0, len(servers))
	for _, s := range servers {
		if s.Owned {
			owned = append(owned, s)
		}
	}
	switch len(owned) {
	case 0:
		return nil, "", "linking failed, the Plex account is not the admin of any server; sign in as its owner"
	case 1:
		conn := plexServerConnection(owned[0].Connections)
		if conn == "" {
			return nil, "", fmt.Sprintf("linking failed, %q has no direct connection; pass its url", owned[0].Name)
		}
		return owned[0], conn, ""
	default:
		return nil, "", "linking failed, several Plex servers found, start over with url set to one; " +
			describePlexServers(owned)
	}
}

// plexServerConnection prefers a local connection, then a remote HTTPS one.
// Relays are never stored.
func plexServerConnection(conns []plex.Connection) string {
	remote := ""
	for i := range conns {
		c := &conns[i]
		if c.Relay || c.URI == "" {
			continue
		}
		if c.Local {
			return plex.NormalizeURL(c.URI)
		}
		if remote == "" && strings.EqualFold(c.Protocol, "https") {
			remote = plex.NormalizeURL(c.URI)
		}
	}
	return remote
}

// plexServerHasURL reports whether a server lists a connection at serverURL,
// either as its URI or by address and port, so a plain LAN address matches
// the plex.direct name plex.tv gives the same connection.
func plexServerHasURL(s *plex.Resource, serverURL string) bool {
	want, err := url.Parse(serverURL)
	if err != nil {
		return false
	}
	wantHostPort := plexHostPort(want)
	for i := range s.Connections {
		c := &s.Connections[i]
		if c.URI != "" && strings.EqualFold(plex.NormalizeURL(c.URI), serverURL) {
			return true
		}
		if c.Address != "" && c.Port > 0 &&
			strings.EqualFold(net.JoinHostPort(c.Address, strconv.Itoa(int(c.Port))), wantHostPort) {
			return true
		}
	}
	return false
}

func plexHostPort(u *url.URL) string {
	port := u.Port()
	if port == "" {
		port = "80"
		if u.Scheme == "https" {
			port = "443"
		}
	}
	return net.JoinHostPort(u.Hostname(), port)
}

func describePlexServers(servers []*plex.Resource) string {
	parts := make([]string, 0, len(servers))
	for _, s := range servers {
		if conn := plexServerConnection(s.Connections); conn != "" {
			parts = append(parts, fmt.Sprintf("%q (%s)", s.Name, conn))
		} else {
			parts = append(parts, strconv.Quote(s.Name))
		}
	}
	return "servers found: " + strings.Join(parts, ", ")
}

// savePlexLink stores the server and account tokens and makes the server
// every Plex launcher's.
func savePlexLink(cfg *config.Instance, clientsURL, serverURL, serverToken, accountToken string) error {
	if cfg == nil {
		return errors.New("no config")
	}
	if err := cfg.SaveAuthEntry(serverURL, config.CredentialEntry{Bearer: serverToken}); err != nil {
		return err //nolint:wrapcheck // the config error already says what failed
	}
	if err := cfg.SaveAuthEntry(clientsURL, config.CredentialEntry{Bearer: accountToken}); err != nil {
		return err //nolint:wrapcheck // the config error already says what failed
	}
	cfg.SetLauncherServerURL(shared.LauncherGroupPlex, serverURL)
	if err := cfg.Save(); err != nil {
		return err //nolint:wrapcheck // the config error already says what failed
	}
	return nil
}
