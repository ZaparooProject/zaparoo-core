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

package romm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/tlsroots"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/useragent"
)

// LauncherID names the RomM launcher, and is the launcher a
// [[launchers.default]] entry names to set the server_url.
const LauncherID = "RomM"

const (
	// romPageSize is how many ROMs one list request asks for. The server
	// allows up to 10000, but a page with files and metadata attached runs to
	// a few KB per ROM.
	romPageSize = 500
	// maxRomsPerPlatform bounds how far a paged listing is followed, so a
	// server reporting an endless total cannot hold indexing forever.
	maxRomsPerPlatform = 200_000
	// maxListBody caps one page or platform list response.
	maxListBody = 32 << 20
	// maxDetailBody caps a single ROM response.
	maxDetailBody = 4 << 20
	// requestTimeout bounds one request from dialing to the end of its body,
	// so a server that stalls mid-response cannot hold indexing or scraping.
	// It allows a full list page over a slow link.
	requestTimeout = 5 * time.Minute
	// maxRedirects is how many redirects one request follows.
	maxRedirects = 10
	// assetPrefix is where RomM serves the cover and screenshot paths its API
	// returns without one.
	assetPrefix = "/assets/romm/resources/"
)

var (
	// ErrNotConfigured is returned when no RomM server URL is set.
	ErrNotConfigured = errors.New("romm server is not configured")
	// ErrUnauthorized is returned when the server refuses the stored token.
	ErrUnauthorized = errors.New("romm server refused the credentials")
	// ErrNotFound is returned for a ROM the server does not have.
	ErrNotFound = errors.New("romm rom not found")
	// errUnreachable marks a request that never got an HTTP answer.
	errUnreachable = errors.New("romm server is unreachable")
)

// transport is shared by every client, so their connections are pooled.
var transport = sync.OnceValue(func() http.RoundTripper {
	base := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ResponseHeaderTimeout: 60 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       90 * time.Second,
	}
	return tlsroots.Transport(base)
})

// Client talks to one RomM server.
type Client struct {
	http *http.Client
	base *url.URL
}

// ServerURL returns the configured RomM server URL, or "" when none is set.
func ServerURL(cfg *config.Instance) string {
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.LookupLauncherDefaults(LauncherID, nil).ServerURL)
}

// NormalizeServerURL turns a user-entered server address into the base URL
// requests are built from: an http or https URL with no query, fragment,
// credentials or trailing slash. An address without a scheme is taken as http,
// the way a RomM server on a home network is usually reached.
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
		return "", fmt.Errorf("invalid romm server url: %w", err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("romm server url must be http or https: %q", u.Scheme)
	}
	if u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("romm server url must be a plain http(s) address")
	}
	u.Scheme = scheme
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	return u.String(), nil
}

// NewClient returns a client for the server at serverURL.
func NewClient(serverURL string) (*Client, error) {
	normalized, err := NormalizeServerURL(serverURL)
	if err != nil {
		return nil, err
	}
	base, err := url.Parse(normalized)
	if err != nil {
		return nil, fmt.Errorf("invalid romm server url: %w", err)
	}
	c := &Client{base: base}
	c.http = &http.Client{
		Transport:     useragent.Transport(transport()),
		Timeout:       requestTimeout,
		CheckRedirect: c.checkRedirect,
	}
	return c, nil
}

// checkRedirect keeps requests on the configured server. A redirect may move
// an http server to https on the same host, but never to another host, out of
// the server's path, or from https down to http.
func (c *Client) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return fmt.Errorf("stopped after %d redirects", maxRedirects)
	}
	if c.owns(req.URL.String()) {
		return nil
	}
	upgraded := strings.EqualFold(c.base.Scheme, "http") && strings.EqualFold(req.URL.Scheme, "https") &&
		strings.EqualFold(req.URL.Hostname(), c.base.Hostname()) &&
		strings.HasPrefix(req.URL.Path, c.base.Path+"/")
	if upgraded {
		return nil
	}
	return fmt.Errorf("romm server redirected off the server to %s", req.URL.Redacted())
}

// ClientFromConfig returns a client for the configured server, or
// ErrNotConfigured when there is none.
func ClientFromConfig(cfg *config.Instance) (*Client, error) {
	serverURL := ServerURL(cfg)
	if serverURL == "" {
		return nil, ErrNotConfigured
	}
	return NewClient(serverURL)
}

// BaseURL is the server URL requests are built from.
func (c *Client) BaseURL() string {
	return c.base.String()
}

func (c *Client) endpoint(p string, query url.Values) string {
	u := *c.base
	u.Path = c.base.Path + p
	if query != nil {
		u.RawQuery = query.Encode()
	}
	return u.String()
}

// authorize attaches the credential stored in auth.toml for the request URL.
// It is looked up per request, so a token saved by linking is used at once.
func authorize(req *http.Request) {
	creds := config.LookupAuth(config.GetAuthCfg(), req.URL.String())
	if creds == nil {
		return
	}
	switch {
	case creds.Bearer != "":
		req.Header.Set("Authorization", "Bearer "+creds.Bearer)
	case creds.Username != "":
		token := base64.StdEncoding.EncodeToString([]byte(creds.Username + ":" + creds.Password))
		req.Header.Set("Authorization", "Basic "+token)
	}
}

func (c *Client) get(ctx context.Context, rawURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("create romm request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	authorize(req)
	resp, err := c.http.Do(req) //nolint:gosec // G704: URL is built from the configured server.
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("romm request: %w", ctxErr)
		}
		return nil, fmt.Errorf("%w: %w", errUnreachable, err)
	}
	return resp, nil
}

func (c *Client) getJSON(ctx context.Context, rawURL string, limit int64, into any) error {
	resp, err := c.get(ctx, rawURL)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if statusErr := statusError(resp); statusErr != nil {
		return statusErr
	}
	body := io.LimitReader(resp.Body, limit+1)
	data, err := io.ReadAll(body)
	if err != nil {
		return fmt.Errorf("%w: reading response: %w", errUnreachable, err)
	}
	if int64(len(data)) > limit {
		return fmt.Errorf("romm response exceeds %d bytes", limit)
	}
	if err := json.Unmarshal(data, into); err != nil {
		return fmt.Errorf("decode romm response: %w", err)
	}
	return nil
}

// statusError maps a non-200 answer to an error. The body is untrusted and is
// never included.
func statusError(resp *http.Response) error {
	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusUnauthorized, http.StatusForbidden:
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return fmt.Errorf("%w (status %d)", ErrUnauthorized, resp.StatusCode)
	case http.StatusNotFound:
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return ErrNotFound
	default:
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		if resp.StatusCode >= 500 {
			return fmt.Errorf("%w: server returned status %d", errUnreachable, resp.StatusCode)
		}
		return fmt.Errorf("romm server returned status %d", resp.StatusCode)
	}
}

// IsUnreachable reports whether err means the server could not be reached or
// failed on its side, rather than refusing or answering badly.
func IsUnreachable(err error) bool {
	return errors.Is(err, errUnreachable)
}

// Platforms lists the server's platforms.
func (c *Client) Platforms(ctx context.Context) ([]Platform, error) {
	var out []Platform
	if err := c.getJSON(ctx, c.endpoint("/api/platforms", nil), maxListBody, &out); err != nil {
		return nil, fmt.Errorf("list romm platforms: %w", err)
	}
	return out, nil
}

// Roms lists every ROM on one platform, following pages.
func (c *Client) Roms(ctx context.Context, platformID int) ([]Rom, error) {
	var out []Rom
	seen := make(map[int]struct{})
	for offset := 0; offset < maxRomsPerPlatform; {
		q := url.Values{}
		q.Set("platform_ids", strconv.Itoa(platformID))
		q.Set("limit", strconv.Itoa(romPageSize))
		q.Set("offset", strconv.Itoa(offset))
		q.Set("with_files", "true")
		q.Set("with_char_index", "false")
		q.Set("with_filter_values", "false")
		var page romPage
		if err := c.getJSON(ctx, c.endpoint("/api/roms", q), maxListBody, &page); err != nil {
			return nil, fmt.Errorf("list romm roms for platform %d: %w", platformID, err)
		}
		// Pages follow the server's default order, which can shift between
		// requests; a ROM seen twice is kept once.
		for i := range page.Items {
			if _, dup := seen[page.Items[i].ID]; dup {
				continue
			}
			seen[page.Items[i].ID] = struct{}{}
			out = append(out, page.Items[i])
		}
		offset += len(page.Items)
		if len(page.Items) == 0 || offset >= page.Total {
			break
		}
	}
	return out, nil
}

// Rom fetches one ROM.
func (c *Client) Rom(ctx context.Context, id int) (*Rom, error) {
	var rom Rom
	if err := c.getJSON(ctx, c.endpoint("/api/roms/"+strconv.Itoa(id), nil), maxDetailBody, &rom); err != nil {
		return nil, fmt.Errorf("get romm rom %d: %w", id, err)
	}
	if rom.ID != id {
		return nil, fmt.Errorf("romm returned rom %d for %d", rom.ID, id)
	}
	return &rom, nil
}

// ContentURL is where the ROM's files download from. fileName names the
// download: a multi-file ROM arrives as <fileName>.zip with a generated
// <fileName>.m3u inside when the server builds a playlist.
func (c *Client) ContentURL(id int, fileName string) string {
	q := url.Values{}
	q.Set("purpose", "play")
	return c.endpoint("/api/roms/"+strconv.Itoa(id)+"/content/"+fileName, q)
}

// AssetURL resolves a cover or screenshot path from the API against the
// server. Only server-relative paths are accepted: an absolute URL points at
// some other host, which Core does not fetch from.
func (c *Client) AssetURL(assetPath string) (string, bool) {
	assetPath = strings.TrimSpace(assetPath)
	if assetPath == "" || strings.Contains(assetPath, "://") || strings.HasPrefix(assetPath, "//") ||
		strings.ContainsAny(assetPath, "\\\x00") {
		return "", false
	}
	// Covers carry a cache-busting query; keep only the path.
	if i := strings.IndexAny(assetPath, "?#"); i >= 0 {
		assetPath = assetPath[:i]
	}
	assetPath = "/" + strings.TrimLeft(assetPath, "/")
	if !strings.HasPrefix(assetPath, "/assets/") {
		assetPath = strings.TrimSuffix(assetPrefix, "/") + assetPath
	}
	for _, segment := range strings.Split(assetPath, "/") {
		if segment == ".." || segment == "." {
			return "", false
		}
	}
	u := *c.base
	u.Path = c.base.Path + assetPath
	return u.String(), true
}

// Fetch downloads an asset URL from this server, up to limit bytes.
func (c *Client) Fetch(ctx context.Context, rawURL string, limit int64) ([]byte, error) {
	if !c.owns(rawURL) {
		return nil, errors.New("asset url is not on the romm server")
	}
	resp, err := c.get(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if statusErr := statusError(resp); statusErr != nil {
		return nil, statusErr
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("%w: reading asset: %w", errUnreachable, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("romm asset exceeds %d bytes", limit)
	}
	return data, nil
}

func (c *Client) owns(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Scheme, c.base.Scheme) && strings.EqualFold(u.Host, c.base.Host) &&
		strings.HasPrefix(u.Path, c.base.Path+"/")
}
