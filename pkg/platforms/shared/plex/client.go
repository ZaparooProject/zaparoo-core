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

package plex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/useragent"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
)

const (
	// DefaultServerURL is the server a launcher uses with no server_url set.
	DefaultServerURL = "http://localhost:32400"
	// TVBaseURL is plex.tv, where PINs are created and checked.
	TVBaseURL = "https://plex.tv"
	// ClientsBaseURL is the plex.tv host that lists an account's devices.
	ClientsBaseURL = "https://clients.plex.tv"

	product          = "Zaparoo"
	pageSize         = 500
	maxResponseBytes = 64 << 20
	requestTimeout   = 30 * time.Second
)

// ErrNotFound reports a library item, PIN or endpoint the server says does
// not exist.
var ErrNotFound = errors.New("plex: not found")

// PlexClient is the part of a Plex server client the launchers use.
//
//nolint:revive // PlexClient mirrors the Kodi package's KodiClient name.
type PlexClient interface {
	// ServerURL is the server's base URL, without a trailing slash.
	ServerURL() string
	// PlayerName is the configured player name; empty means any one player.
	PlayerName() string
	// ClientIdentifier is the stable identity Core uses with Plex.
	ClientIdentifier() string
	// ServerToken is the access token stored for the server.
	ServerToken() string
	// AccountToken is the plex.tv account token, if Core has one.
	AccountToken() string
	Sections(ctx context.Context) ([]Section, error)
	// Items lists a section's items of one type page by page.
	Items(ctx context.Context, sectionKey string, plexType int, filter url.Values,
		fn func([]Metadata) error) error
	// Metadata returns one item; a missing item is ErrNotFound.
	Metadata(ctx context.Context, ratingKey string) (*Metadata, error)
	// Leaves lists the playable items under a show, album or artist.
	Leaves(ctx context.Context, ratingKey string, fn func([]Metadata) error) error
	Identity(ctx context.Context) (*Identity, error)
	CreatePlayQueue(ctx context.Context, machineID, ratingKey, queueType string) (*PlayQueue, error)
	// Clients lists the Companion players the server knows.
	Clients(ctx context.Context) ([]ServerClient, error)
	// PlayerResources lists the players registered to the Plex account.
	PlayerResources(ctx context.Context) ([]Resource, error)
}

// Client talks to one Plex server and, for player discovery, plex.tv.
type Client struct {
	http       *http.Client
	serverURL  string
	clientsURL string
	clientID   string
	playerName string
}

var _ PlexClient = (*Client)(nil)

// NewClientWithLauncherID builds a client from the server_url and client
// settings that apply to a launcher and its groups.
func NewClientWithLauncherID(cfg *config.Instance, launcherID string, groups []string) PlexClient {
	var serverURL, playerName string
	if cfg != nil {
		defaults := cfg.LookupLauncherDefaults(launcherID, groups)
		serverURL = defaults.ServerURL
		playerName = defaults.Client
	}
	if strings.TrimSpace(serverURL) == "" {
		serverURL = DefaultServerURL
	}
	return NewClient(serverURL, ClientsBaseURL, ClientIdentifier(cfg), playerName, nil)
}

// newHTTPClient is the client every Plex request goes through. Requests
// carry X-Plex-Token, so redirects must stay on the server's origin.
func newHTTPClient() *http.Client {
	return &http.Client{Transport: useragent.Transport(nil), CheckRedirect: shared.SameOriginRedirect}
}

// NewClient builds a client for a server URL. A nil httpClient uses Core's
// default transport.
func NewClient(serverURL, clientsURL, clientID, playerName string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = newHTTPClient()
	}
	return &Client{
		http:       httpClient,
		serverURL:  NormalizeURL(serverURL),
		clientsURL: NormalizeURL(clientsURL),
		clientID:   clientID,
		playerName: strings.TrimSpace(playerName),
	}
}

// NormalizeURL adds a missing http scheme and drops a trailing slash.
func NormalizeURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw != "" && !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	return strings.TrimRight(raw, "/")
}

// ClientIdentifier is the X-Plex-Client-Identifier Core sends. Plex ties
// tokens and devices to it, so it is derived from Core's persisted device ID
// and stays the same across restarts.
func ClientIdentifier(cfg *config.Instance) string {
	if cfg != nil {
		if id := strings.TrimSpace(cfg.DeviceID()); id != "" {
			return "zaparoo-core-" + id
		}
	}
	host, err := os.Hostname()
	if err != nil {
		host = ""
	}
	sum := sha256.Sum256([]byte("zaparoo-core:" + host))
	return "zaparoo-core-" + hex.EncodeToString(sum[:8])
}

// deviceName is the name Plex shows for Core, the host's name when it has one.
func deviceName() string {
	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		return "Zaparoo Core"
	}
	return host
}

func lookupToken(rawURL string) string {
	if cred := config.LookupAuth(config.GetAuthCfg(), rawURL); cred != nil {
		return cred.Bearer
	}
	return ""
}

func (c *Client) ServerURL() string        { return c.serverURL }
func (c *Client) PlayerName() string       { return c.playerName }
func (c *Client) ClientIdentifier() string { return c.clientID }
func (c *Client) ServerToken() string      { return lookupToken(c.serverURL) }
func (c *Client) AccountToken() string     { return lookupToken(c.clientsURL) }

// plexHeaders are the identification headers every Plex request carries.
func plexHeaders(clientID, token string) http.Header {
	h := http.Header{}
	h.Set("Accept", "application/json")
	h.Set("X-Plex-Product", product)
	h.Set("X-Plex-Version", config.AppVersion)
	h.Set("X-Plex-Client-Identifier", clientID)
	// The server's Companion proxy refuses timeline polls, and leaves
	// commands hanging, from a controller without a device name.
	h.Set("X-Plex-Device-Name", deviceName())
	if token != "" {
		h.Set("X-Plex-Token", token)
	}
	return h
}

// doRequest sends one request and decodes a JSON body into out when out is
// not nil.
func doRequest(
	ctx context.Context,
	client *http.Client,
	method, rawURL string,
	header http.Header,
	out any,
) error {
	body, err := doRawRequest(ctx, client, method, rawURL, header)
	if err != nil || out == nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("%w: invalid plex JSON response: %w", platforms.ErrScannerSourceUnreachable, err)
	}
	return nil
}

// doRawRequest sends one request and returns the body. A failure to reach
// the host wraps ErrScannerSourceUnreachable so a scan keeps the existing
// index. URLs are left out of errors because Plex puts tokens in some of them.
func doRawRequest(
	ctx context.Context,
	client *http.Client,
	method, rawURL string,
	header http.Header,
) ([]byte, error) {
	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, method, rawURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("plex request: %w", stripURL(err))
	}
	for k, v := range header {
		req.Header[k] = v
	}
	resp, err := client.Do(req) //nolint:gosec // G704: the URL is the configured Plex server or player
	if err != nil {
		return nil, fmt.Errorf("plex request: %w: %w", platforms.ErrScannerSourceUnreachable, stripURL(err))
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, ErrNotFound
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, fmt.Errorf("%w: plex refused the request (status %d), link Plex again",
			platforms.ErrScannerSourceUnreachable, resp.StatusCode)
	case resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices:
		return nil, fmt.Errorf("%w: unexpected HTTP status from plex: %d",
			platforms.ErrScannerSourceUnreachable, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("plex response: %w: %w", platforms.ErrScannerSourceUnreachable, err)
	}
	if len(body) > maxResponseBytes {
		return nil, fmt.Errorf("plex response exceeds %d bytes", maxResponseBytes)
	}
	return body, nil
}

// stripURL drops the URL a url.Error carries, which can hold a token.
func stripURL(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err
	}
	return err
}

func (c *Client) serverRequest(ctx context.Context, method, path string, query url.Values,
	extra http.Header, out any,
) error {
	rawURL := c.serverURL + path
	if len(query) > 0 {
		rawURL += "?" + query.Encode()
	}
	header := plexHeaders(c.clientID, c.ServerToken())
	for k, v := range extra {
		header[k] = v
	}
	return doRequest(ctx, c.http, method, rawURL, header, out)
}

// Sections lists the server's library sections.
func (c *Client) Sections(ctx context.Context) ([]Section, error) {
	var resp containerResponse
	if err := c.serverRequest(ctx, http.MethodGet, "/library/sections", nil, nil, &resp); err != nil {
		return nil, fmt.Errorf("failed to list plex library sections: %w", err)
	}
	return resp.MediaContainer.Directory, nil
}

// Items lists a section's items of one type, page by page.
func (c *Client) Items(
	ctx context.Context, sectionKey string, plexType int, filter url.Values,
	fn func([]Metadata) error,
) error {
	if !validKey(sectionKey) {
		return fmt.Errorf("invalid plex section key %q", sectionKey)
	}
	query := url.Values{}
	for k, v := range filter {
		query[k] = v
	}
	query.Set("type", strconv.Itoa(plexType))
	query.Set("includeGuids", "1")
	return c.paged(ctx, "/library/sections/"+sectionKey+"/all", query, fn)
}

// Leaves lists every playable item under a show, album or artist.
func (c *Client) Leaves(ctx context.Context, ratingKey string, fn func([]Metadata) error) error {
	if !validKey(ratingKey) {
		return fmt.Errorf("invalid plex item key %q", ratingKey)
	}
	query := url.Values{}
	query.Set("includeGuids", "1")
	return c.paged(ctx, "/library/metadata/"+ratingKey+"/allLeaves", query, fn)
}

func (c *Client) paged(ctx context.Context, path string, query url.Values, fn func([]Metadata) error) error {
	start := 0
	total := 0
	firstKey := ""
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("plex listing cancelled: %w", err)
		}
		query.Set("X-Plex-Container-Start", strconv.Itoa(start))
		query.Set("X-Plex-Container-Size", strconv.Itoa(pageSize))
		header := http.Header{}
		header.Set("X-Plex-Container-Start", strconv.Itoa(start))
		header.Set("X-Plex-Container-Size", strconv.Itoa(pageSize))
		var resp containerResponse
		if err := c.serverRequest(ctx, http.MethodGet, path, query, header, &resp); err != nil {
			return fmt.Errorf("failed to list plex items: %w", err)
		}
		if size := int(resp.MediaContainer.TotalSize); size > 0 {
			total = size
		}
		items := resp.MediaContainer.Metadata
		// A server that ignores the paging parameters returns the first page
		// again, so a repeat ends the listing like an empty page does.
		repeated := start > 0 && len(items) > 0 && items[0].RatingKey != "" && items[0].RatingKey == firstKey
		if len(items) == 0 || repeated {
			if start < total {
				return fmt.Errorf("%w: plex listing stopped at %d of %d items",
					platforms.ErrScannerSourceUnreachable, start, total)
			}
			return nil
		}
		if start == 0 {
			firstKey = items[0].RatingKey
		}
		if err := fn(items); err != nil {
			return err
		}
		start += len(items)
		// A short page does not end a listing on its own: the server may cap
		// the page size below the one asked for.
		if total > 0 && start >= total {
			return nil
		}
	}
}

// Metadata returns one library item by rating key.
func (c *Client) Metadata(ctx context.Context, ratingKey string) (*Metadata, error) {
	if !validKey(ratingKey) {
		return nil, fmt.Errorf("invalid plex item key %q", ratingKey)
	}
	query := url.Values{}
	query.Set("includeGuids", "1")
	var resp containerResponse
	err := c.serverRequest(ctx, http.MethodGet, "/library/metadata/"+ratingKey, query, nil, &resp)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get plex item %s: %w", ratingKey, err)
	}
	if len(resp.MediaContainer.Metadata) == 0 {
		return nil, ErrNotFound
	}
	return &resp.MediaContainer.Metadata[0], nil
}

// Identity returns the server's machine identifier and version.
func (c *Client) Identity(ctx context.Context) (*Identity, error) {
	var resp containerResponse
	if err := c.serverRequest(ctx, http.MethodGet, "/identity", nil, nil, &resp); err != nil {
		return nil, fmt.Errorf("failed to get plex server identity: %w", err)
	}
	if resp.MediaContainer.MachineIdentifier == "" {
		return nil, errors.New("plex server did not report a machine identifier")
	}
	return &Identity{
		MachineIdentifier: resp.MediaContainer.MachineIdentifier,
		Version:           resp.MediaContainer.Version,
	}, nil
}

// libraryURI is the uri a play queue is built from. Plex expands a show,
// album or artist into its episodes or tracks in order.
func libraryURI(machineID, ratingKey string) string {
	return "server://" + machineID + "/com.plexapp.plugins.library/library/metadata/" + ratingKey
}

// CreatePlayQueue asks the server for a queue holding the item, or the
// item's episodes or tracks for a show, album or artist.
func (c *Client) CreatePlayQueue(ctx context.Context, machineID, ratingKey, queueType string) (*PlayQueue, error) {
	if !validKey(ratingKey) {
		return nil, fmt.Errorf("invalid plex item key %q", ratingKey)
	}
	query := url.Values{}
	query.Set("type", queueType)
	query.Set("uri", libraryURI(machineID, ratingKey))
	query.Set("shuffle", "0")
	query.Set("continuous", "0")
	query.Set("repeat", "0")
	var resp containerResponse
	if err := c.serverRequest(ctx, http.MethodPost, "/playQueues", query, nil, &resp); err != nil {
		return nil, fmt.Errorf("failed to create plex play queue: %w", err)
	}
	mc := resp.MediaContainer
	if mc.PlayQueueID <= 0 || len(mc.Metadata) == 0 {
		return nil, errors.New("plex returned an empty play queue")
	}
	return &PlayQueue{
		ID:             int64(mc.PlayQueueID),
		SelectedItemID: int64(mc.PlayQueueSelectedItemID),
		Items:          mc.Metadata,
	}, nil
}

// Clients lists the Companion players the server currently sees.
func (c *Client) Clients(ctx context.Context) ([]ServerClient, error) {
	var resp containerResponse
	if err := c.serverRequest(ctx, http.MethodGet, "/clients", nil, nil, &resp); err != nil {
		return nil, fmt.Errorf("failed to list plex clients: %w", err)
	}
	return resp.MediaContainer.Server, nil
}

// PlayerResources lists the account's registered players. Without an
// account token there is nothing to ask for.
func (c *Client) PlayerResources(ctx context.Context) ([]Resource, error) {
	token := c.AccountToken()
	if token == "" {
		return nil, nil
	}
	resources, err := fetchResources(ctx, c.http, c.clientsURL, c.clientID, token, false)
	if err != nil {
		return nil, err
	}
	players := make([]Resource, 0, len(resources))
	for i := range resources {
		if resources[i].ProvidesRole("player") {
			players = append(players, resources[i])
		}
	}
	return players, nil
}

func fetchResources(
	ctx context.Context, client *http.Client, clientsURL, clientID, token string, includeRelay bool,
) ([]Resource, error) {
	query := url.Values{}
	query.Set("includeHttps", "1")
	if !includeRelay {
		query.Set("includeRelay", "0")
	}
	var resources []Resource
	err := doRequest(ctx, client, http.MethodGet, clientsURL+"/api/v2/resources?"+query.Encode(),
		plexHeaders(clientID, token), &resources)
	if err != nil {
		return nil, fmt.Errorf("failed to list plex.tv resources: %w", err)
	}
	return resources, nil
}

// validKey reports whether a key is a plain Plex numeric key, so a value
// from a card cannot change the request path.
func validKey(key string) bool {
	if key == "" || len(key) > 32 {
		return false
	}
	for _, r := range key {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
