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

// plexProbeTimeout bounds each check that a server answers at a connection
// plex.tv advertises.
const plexProbeTimeout = 5 * time.Second

// newPlexLinkClient, plexServerIdentity and plexLinkClock are replaced in
// tests.
var (
	newPlexLinkClient = func(clientID string) plexLinkClient {
		return plex.NewTVClient("", "", clientID, nil)
	}
	// plexServerIdentity returns the machine identifier of the server that
	// answers at serverURL.
	plexServerIdentity = func(ctx context.Context, serverURL, clientID string) (string, error) {
		id, err := plex.NewClient(serverURL, "", clientID, "", nil).Identity(ctx)
		if err != nil {
			return "", err //nolint:wrapcheck // the client error already names the request
		}
		return id.MachineIdentifier, nil
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
	server, candidates, msg := selectPlexServer(resources, serverURL)
	if msg != "" {
		return msg
	}
	connURL := serverURL
	if connURL == "" {
		connURL = reachablePlexConnection(ctx, server, candidates, plex.ClientIdentifier(cfg))
		if connURL == "" {
			return fmt.Sprintf("linking failed, %q did not answer at any address plex.tv lists; pass its url",
				server.Name)
		}
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
// account's only one, with the direct connections to try in order. Only
// servers the account owns can be linked, since a shared server's token
// cannot see the libraries the launchers index.
func selectPlexServer(
	resources []plex.Resource, serverURL string,
) (server *plex.Resource, candidates []string, failure string) {
	var servers []*plex.Resource
	for i := range resources {
		if resources[i].ProvidesRole("server") {
			servers = append(servers, &resources[i])
		}
	}
	if len(servers) == 0 {
		return nil, nil, "linking failed, the Plex account has no servers"
	}

	if serverURL != "" {
		matches := plexServersAt(servers, serverURL)
		switch len(matches) {
		case 0:
			return nil, nil, fmt.Sprintf("linking failed, no Plex server on the account answers at %s; %s",
				serverURL, describePlexServers(servers))
		case 1:
		default:
			return nil, nil, fmt.Sprintf("linking failed, several Plex servers on the account list %s; "+
				"pass one's own address; %s", serverURL, describePlexServers(matches))
		}
		s := matches[0]
		if !s.Owned {
			return nil, nil, fmt.Sprintf(
				"linking failed, the Plex account is not the admin of %q; sign in as its owner", s.Name)
		}
		return s, []string{serverURL}, ""
	}

	owned := make([]*plex.Resource, 0, len(servers))
	for _, s := range servers {
		if s.Owned {
			owned = append(owned, s)
		}
	}
	switch len(owned) {
	case 0:
		return nil, nil, "linking failed, the Plex account is not the admin of any server; sign in as its owner"
	case 1:
		conns := plexServerConnections(owned[0].Connections)
		if len(conns) == 0 {
			return nil, nil, fmt.Sprintf("linking failed, %q has no direct connection; pass its url", owned[0].Name)
		}
		return owned[0], conns, ""
	default:
		return nil, nil, "linking failed, several Plex servers found, start over with url set to one; " +
			describePlexServers(owned)
	}
}

// plexServersAt returns the servers that list a connection with serverURL
// as its URI or, when none does, by address and port.
func plexServersAt(servers []*plex.Resource, serverURL string) []*plex.Resource {
	var exact, byAddress []*plex.Resource
	for _, s := range servers {
		uriMatch, addrMatch := plexServerHasURL(s, serverURL)
		switch {
		case uriMatch:
			exact = append(exact, s)
		case addrMatch:
			byAddress = append(byAddress, s)
		}
	}
	if len(exact) > 0 {
		return exact
	}
	return byAddress
}

// reachablePlexConnection returns the first candidate where the server
// itself answers. plex.tv lists every address a server reported, including
// ones Core cannot reach, such as a Docker container's internal address.
func reachablePlexConnection(ctx context.Context, server *plex.Resource, candidates []string, clientID string) string {
	for _, candidate := range candidates {
		probeCtx, cancel := context.WithTimeout(ctx, plexProbeTimeout)
		id, err := plexServerIdentity(probeCtx, candidate, clientID)
		cancel()
		switch {
		case err != nil:
			log.Debug().Err(err).Msg("plex server did not answer at an advertised connection")
		case !strings.EqualFold(id, server.ClientIdentifier):
			log.Debug().Msg("another plex server answered at an advertised connection")
		default:
			return candidate
		}
	}
	return ""
}

// plexServerConnections lists the direct connections to try: local ones
// first, then remote HTTPS ones. Relays are never stored.
func plexServerConnections(conns []plex.Connection) []string {
	var local, remote []string
	for i := range conns {
		c := &conns[i]
		if c.Relay || c.URI == "" {
			continue
		}
		switch {
		case c.Local:
			local = append(local, plex.NormalizeURL(c.URI))
		case strings.EqualFold(c.Protocol, "https"):
			remote = append(remote, plex.NormalizeURL(c.URI))
		}
	}
	return append(local, remote...)
}

// plexServerHasURL reports whether a server lists a connection at
// serverURL as its URI, and whether one matches by address and port, so a
// plain LAN address matches the plex.direct name plex.tv gives the same
// connection.
func plexServerHasURL(s *plex.Resource, serverURL string) (uriMatch, addrMatch bool) {
	want, err := url.Parse(serverURL)
	if err != nil {
		return false, false
	}
	wantHostPort := plexHostPort(want)
	for i := range s.Connections {
		c := &s.Connections[i]
		if c.URI != "" && strings.EqualFold(plex.NormalizeURL(c.URI), serverURL) {
			uriMatch = true
		}
		if c.Address != "" && c.Port > 0 &&
			strings.EqualFold(net.JoinHostPort(c.Address, strconv.Itoa(int(c.Port))), wantHostPort) {
			addrMatch = true
		}
	}
	return uriMatch, addrMatch
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
		if conns := plexServerConnections(s.Connections); len(conns) > 0 {
			parts = append(parts, fmt.Sprintf("%q (%s)", s.Name, conns[0]))
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
	err := cfg.SaveAuthEntries(map[string]config.CredentialEntry{
		serverURL:  {Bearer: serverToken},
		clientsURL: {Bearer: accountToken},
	})
	if err != nil {
		return err //nolint:wrapcheck // the config error already says what failed
	}
	cfg.SetLauncherServerURL(shared.LauncherGroupPlex, serverURL)
	if err := cfg.Save(); err != nil {
		return err //nolint:wrapcheck // the config error already says what failed
	}
	return nil
}
