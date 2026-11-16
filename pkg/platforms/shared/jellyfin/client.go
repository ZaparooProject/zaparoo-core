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

// Package jellyfin indexes a Jellyfin server's library and plays its items on
// a player the server can remote control.
package jellyfin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/tlsroots"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/useragent"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
)

const (
	clientName         = "Zaparoo"
	requestTimeout     = 30 * time.Second
	maxResponseBytes   = 64 << 20
	maxErrorBodyBytes  = 4 << 10
	defaultDeviceLabel = "Zaparoo Core"
)

var (
	// ErrNotConfigured is returned when no Jellyfin server URL is set.
	ErrNotConfigured = errors.New("jellyfin server is not configured")
	// ErrNotLinked is returned when Core holds no access token for the
	// configured Jellyfin server.
	ErrNotLinked = errors.New("jellyfin server is not linked")
	// ErrUnauthorized is returned when the server rejects the access token.
	ErrUnauthorized = errors.New("jellyfin rejected the access token, link the server again")
	// ErrItemNotFound is returned when the server has no item with an ID.
	ErrItemNotFound = errors.New("jellyfin item not found")
)

// transport is shared by every client, so their connections are pooled and
// use the CA roots Core is configured with.
var transport = sync.OnceValue(func() http.RoundTripper {
	return tlsroots.Transport(&http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout: 10 * time.Second,
		MaxIdleConnsPerHost: 4,
		IdleConnTimeout:     90 * time.Second,
	})
})

func newHTTPClient() *http.Client {
	return &http.Client{Transport: useragent.Transport(transport())}
}

// Item is the part of a Jellyfin BaseItemDto Core uses.
//
//nolint:tagliatelle // Jellyfin's API uses PascalCase JSON fields.
type Item struct {
	ProviderIDs       map[string]string `json:"ProviderIds"`
	ParentIndexNumber *int              `json:"ParentIndexNumber"`
	IndexNumber       *int              `json:"IndexNumber"`
	ID                string            `json:"Id"`
	Name              string            `json:"Name"`
	Type              string            `json:"Type"`
	SeriesName        string            `json:"SeriesName"`
	Album             string            `json:"Album"`
	AlbumID           string            `json:"AlbumId"`
	AlbumArtist       string            `json:"AlbumArtist"`
	Artists           []string          `json:"Artists"`
	ProductionYear    int               `json:"ProductionYear"`
}

// ItemsPage is one page of a Jellyfin item query.
//
//nolint:tagliatelle // Jellyfin's API uses PascalCase JSON fields.
type ItemsPage struct {
	Items            []Item `json:"Items"`
	TotalRecordCount int    `json:"TotalRecordCount"`
	StartIndex       int    `json:"StartIndex"`
}

// Session is the part of a Jellyfin SessionInfo Core uses.
//
//nolint:tagliatelle // Jellyfin's API uses PascalCase JSON fields.
type Session struct {
	LastActivityDate      time.Time `json:"LastActivityDate"`
	NowPlayingItem        *Item     `json:"NowPlayingItem"`
	ID                    string    `json:"Id"`
	DeviceName            string    `json:"DeviceName"`
	Client                string    `json:"Client"`
	DeviceID              string    `json:"DeviceId"`
	SupportsRemoteControl bool      `json:"SupportsRemoteControl"`
}

// Client is the Jellyfin API surface the launchers, scanner and controls use.
type Client interface {
	// PlayerName is the configured player, empty when none is configured.
	PlayerName() string
	// DeviceID is the device ID Core identifies itself with.
	DeviceID() string
	CurrentUserID(ctx context.Context) (string, error)
	ControllableSessions(ctx context.Context) ([]Session, error)
	GetItem(ctx context.Context, id string) (*Item, error)
	ListItems(ctx context.Context, query url.Values) (*ItemsPage, error)
	ListAlbumArtists(ctx context.Context, query url.Values) (*ItemsPage, error)
	ShowEpisodes(ctx context.Context, showID string) ([]Item, error)
	Play(ctx context.Context, sessionID, command string, itemIDs []string) error
	SendPlaystate(ctx context.Context, sessionID, command string) error
}

// HTTPClient talks to a Jellyfin server over its REST API.
type HTTPClient struct {
	http     *http.Client
	base     *url.URL
	player   string
	deviceID string
	token    string
	userID   string
	mu       syncutil.Mutex
}

var _ Client = (*HTTPClient)(nil)

// NormalizeServerURL turns a user-entered server address into the base URL
// Core stores: http or https, no credentials, query or fragment, and no
// trailing slash. An address without a scheme is taken as http.
func NormalizeServerURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ErrNotConfigured
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid jellyfin server url: %w", err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("jellyfin server url must be http or https: %q", u.Scheme)
	}
	if u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("jellyfin server url must be a plain http(s) address")
	}
	u.Scheme = scheme
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	return u.String(), nil
}

// DeviceID is the stable device ID Core sends to Jellyfin. Jellyfin ties an
// access token to it and revokes a device's old token when the same device
// links again, so the link flow and the client must agree on it.
func DeviceID(cfg *config.Instance) string {
	if cfg != nil {
		if id := strings.TrimSpace(cfg.DeviceID()); id != "" {
			return id
		}
	}
	return deviceName()
}

func deviceName() string {
	hostname, err := os.Hostname()
	if err != nil || strings.TrimSpace(hostname) == "" {
		return defaultDeviceLabel
	}
	return hostname
}

// authorizationHeader builds Jellyfin's MediaBrowser Authorization header.
// Jellyfin decodes each value, so they are escaped to keep quotes and commas
// in a hostname from breaking the header. The token is omitted for calls made
// before the device is linked.
func authorizationHeader(deviceID, token string) string {
	parts := []string{
		`Client="` + url.PathEscape(clientName) + `"`,
		`Device="` + url.PathEscape(deviceName()) + `"`,
		`DeviceId="` + url.PathEscape(deviceID) + `"`,
		`Version="` + url.PathEscape(config.AppVersion) + `"`,
	}
	if token != "" {
		parts = append(parts, `Token="`+url.PathEscape(token)+`"`)
	}
	return "MediaBrowser " + strings.Join(parts, ", ")
}

// NewClientWithLauncherID returns a client for the server and player the
// launcher's merged configuration names. It fails with ErrNotConfigured when
// no server URL is set and ErrNotLinked when Core holds no token for it.
func NewClientWithLauncherID(cfg *config.Instance, launcherID string, groups []string) (*HTTPClient, error) {
	var defaults config.LaunchersDefault
	if cfg != nil {
		defaults = cfg.LookupLauncherDefaults(launcherID, groups)
	}
	serverURL, err := NormalizeServerURL(defaults.ServerURL)
	if err != nil {
		return nil, err
	}
	// The trailing slash lets a token stored under the base URL with or
	// without one match.
	cred := config.LookupAuth(config.GetAuthCfg(), serverURL+"/")
	if cred == nil || cred.Bearer == "" {
		return nil, ErrNotLinked
	}
	return NewClient(serverURL, cred.Bearer, strings.TrimSpace(defaults.Client), DeviceID(cfg))
}

// NewClient returns a client for serverURL authenticated with token, playing
// on the named player.
func NewClient(serverURL, token, player, deviceID string) (*HTTPClient, error) {
	normalized, err := NormalizeServerURL(serverURL)
	if err != nil {
		return nil, err
	}
	base, err := url.Parse(normalized)
	if err != nil {
		return nil, fmt.Errorf("invalid jellyfin server url: %w", err)
	}
	return &HTTPClient{
		http:     newHTTPClient(),
		base:     base,
		player:   player,
		deviceID: deviceID,
		token:    token,
	}, nil
}

// PlayerName returns the configured player name.
func (c *HTTPClient) PlayerName() string { return c.player }

// DeviceID returns the device ID Core identifies itself with.
func (c *HTTPClient) DeviceID() string { return c.deviceID }

// CurrentUserID returns the ID of the user the token belongs to.
func (c *HTTPClient) CurrentUserID(ctx context.Context) (string, error) {
	c.mu.Lock()
	cached := c.userID
	c.mu.Unlock()
	if cached != "" {
		return cached, nil
	}
	var user struct {
		ID string `json:"Id"` //nolint:tagliatelle // Jellyfin's API uses PascalCase JSON fields.
	}
	if err := c.do(ctx, http.MethodGet, "/Users/Me", nil, nil, &user); err != nil {
		return "", fmt.Errorf("failed to look up jellyfin user: %w", err)
	}
	if user.ID == "" {
		return "", errors.New("jellyfin returned a user without an ID")
	}
	c.mu.Lock()
	c.userID = user.ID
	c.mu.Unlock()
	return user.ID, nil
}

// ControllableSessions lists the sessions the token's user may remote control.
func (c *HTTPClient) ControllableSessions(ctx context.Context) ([]Session, error) {
	userID, err := c.CurrentUserID(ctx)
	if err != nil {
		return nil, err
	}
	var sessions []Session
	q := url.Values{"controllableByUserId": {userID}}
	if err := c.do(ctx, http.MethodGet, "/Sessions", q, nil, &sessions); err != nil {
		return nil, fmt.Errorf("failed to list jellyfin sessions: %w", err)
	}
	return sessions, nil
}

// GetItem returns one item, or ErrItemNotFound when the server has none with
// that ID.
func (c *HTTPClient) GetItem(ctx context.Context, id string) (*Item, error) {
	if !validItemID(id) {
		return nil, fmt.Errorf("invalid jellyfin item id %q", id)
	}
	userID, err := c.CurrentUserID(ctx)
	if err != nil {
		return nil, err
	}
	var item Item
	q := url.Values{"userId": {userID}}
	if err := c.do(ctx, http.MethodGet, "/Items/"+id, q, nil, &item); err != nil {
		return nil, err
	}
	return &item, nil
}

// ListItems runs an item query as the token's user.
func (c *HTTPClient) ListItems(ctx context.Context, query url.Values) (*ItemsPage, error) {
	return c.listPage(ctx, "/Items", query)
}

// ListAlbumArtists runs an album artist query as the token's user. Artists
// are not children of a library folder, so /Items does not list them.
func (c *HTTPClient) ListAlbumArtists(ctx context.Context, query url.Values) (*ItemsPage, error) {
	return c.listPage(ctx, "/Artists/AlbumArtists", query)
}

func (c *HTTPClient) listPage(ctx context.Context, path string, query url.Values) (*ItemsPage, error) {
	userID, err := c.CurrentUserID(ctx)
	if err != nil {
		return nil, err
	}
	q := cloneValues(query)
	q.Set("userId", userID)
	var page ItemsPage
	if err := c.do(ctx, http.MethodGet, path, q, nil, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

// ShowEpisodes lists a series' episodes.
func (c *HTTPClient) ShowEpisodes(ctx context.Context, showID string) ([]Item, error) {
	if !validItemID(showID) {
		return nil, fmt.Errorf("invalid jellyfin item id %q", showID)
	}
	userID, err := c.CurrentUserID(ctx)
	if err != nil {
		return nil, err
	}
	q := url.Values{"userId": {userID}, "Fields": {itemFields}}
	var page ItemsPage
	if err := c.do(ctx, http.MethodGet, "/Shows/"+showID+"/Episodes", q, nil, &page); err != nil {
		return nil, err
	}
	return page.Items, nil
}

// Play sends a play command for items to a session.
func (c *HTTPClient) Play(ctx context.Context, sessionID, command string, itemIDs []string) error {
	q := url.Values{"playCommand": {command}, "itemIds": {strings.Join(itemIDs, ",")}}
	return c.do(ctx, http.MethodPost, "/Sessions/"+url.PathEscape(sessionID)+"/Playing", q, nil, nil)
}

// SendPlaystate sends a playstate command such as PlayPause to a session.
func (c *HTTPClient) SendPlaystate(ctx context.Context, sessionID, command string) error {
	return c.do(ctx, http.MethodPost,
		"/Sessions/"+url.PathEscape(sessionID)+"/Playing/"+url.PathEscape(command), nil, nil, nil)
}

func (c *HTTPClient) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	return doRequest(ctx, c.http, c.base, authorizationHeader(c.deviceID, c.token), method, path, query, body, out)
}

// doRequest sends one API request and decodes a JSON reply into out. Network
// failures and server errors wrap ErrScannerSourceUnreachable so an index
// keeps what it had while the server is down.
func doRequest(
	ctx context.Context,
	client *http.Client,
	base *url.URL,
	auth, method, path string,
	query url.Values,
	body, out any,
) error {
	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	u := *base
	u.Path = base.Path + path
	u.RawPath = ""
	u.RawQuery = query.Encode()

	var reader io.Reader = http.NoBody
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to encode request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(reqCtx, method, u.String(), reader)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", auth)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := client.Do(req) //nolint:gosec // G704: URL from user config, the client's purpose
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return fmt.Errorf("%w: jellyfin request failed: %w", platforms.ErrScannerSourceUnreachable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return ErrUnauthorized
	case resp.StatusCode == http.StatusNotFound:
		return ErrItemNotFound
	case resp.StatusCode >= http.StatusInternalServerError:
		return fmt.Errorf("%w: jellyfin returned HTTP %d",
			platforms.ErrScannerSourceUnreachable, resp.StatusCode)
	case resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices:
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		return fmt.Errorf("jellyfin returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}

	if out == nil {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("%w: failed to read jellyfin response: %w", platforms.ErrScannerSourceUnreachable, err)
	}
	if len(data) > maxResponseBytes {
		return fmt.Errorf("jellyfin response exceeds %d bytes", maxResponseBytes)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("%w: invalid jellyfin JSON response: %w", platforms.ErrScannerSourceUnreachable, err)
	}
	return nil
}

func cloneValues(v url.Values) url.Values {
	out := make(url.Values, len(v)+1)
	for k, vals := range v {
		out[k] = append([]string(nil), vals...)
	}
	return out
}

// validItemID reports whether id looks like a Jellyfin item ID, a GUID with
// or without dashes. IDs come from cards, so anything else is refused before
// it reaches a request path.
func validItemID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for i := range len(id) {
		c := id[i]
		isHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
		if !isHex && c != '-' {
			return false
		}
	}
	return true
}
