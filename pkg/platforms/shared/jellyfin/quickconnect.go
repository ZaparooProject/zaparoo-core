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

package jellyfin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

var (
	// ErrQuickConnectDisabled is returned when the server has Quick Connect
	// turned off.
	ErrQuickConnectDisabled = errors.New("quick connect is disabled on this jellyfin server")
	// ErrQuickConnectExpired is returned when the server no longer knows a
	// Quick Connect request.
	ErrQuickConnectExpired = errors.New("jellyfin quick connect request expired")
)

// QuickConnect is the state of a Quick Connect request.
//
//nolint:tagliatelle // Jellyfin's API uses PascalCase JSON fields.
type QuickConnect struct {
	Secret        string `json:"Secret"`
	Code          string `json:"Code"`
	Authenticated bool   `json:"Authenticated"`
}

// LinkClient runs Jellyfin's Quick Connect flow, which links Core to a server
// without Core ever seeing the user's password.
type LinkClient struct {
	http     *http.Client
	base     *url.URL
	deviceID string
}

// NewLinkClient returns a Quick Connect client for serverURL.
func NewLinkClient(serverURL, deviceID string) (*LinkClient, error) {
	normalized, err := NormalizeServerURL(serverURL)
	if err != nil {
		return nil, err
	}
	base, err := url.Parse(normalized)
	if err != nil {
		return nil, fmt.Errorf("invalid jellyfin server url: %w", err)
	}
	return &LinkClient{
		http:     newHTTPClient(),
		base:     base,
		deviceID: deviceID,
	}, nil
}

// BaseURL returns the normalized server URL.
func (c *LinkClient) BaseURL() string { return c.base.String() }

// VerificationURL is the page of the server's web app where a signed-in user
// enters a Quick Connect code.
func (c *LinkClient) VerificationURL() string { return c.base.String() + "/web/#/quickconnect" }

func (c *LinkClient) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	return doRequest(ctx, c.http, c.base, authorizationHeader(c.deviceID, ""), method, path, query, body, out)
}

// QuickConnectEnabled reports whether the server accepts Quick Connect.
func (c *LinkClient) QuickConnectEnabled(ctx context.Context) (bool, error) {
	var enabled bool
	if err := c.do(ctx, http.MethodGet, "/QuickConnect/Enabled", nil, nil, &enabled); err != nil {
		return false, fmt.Errorf("failed to check jellyfin quick connect: %w", err)
	}
	return enabled, nil
}

// InitiateQuickConnect starts a request and returns its code and secret.
func (c *LinkClient) InitiateQuickConnect(ctx context.Context) (*QuickConnect, error) {
	var qc QuickConnect
	err := c.do(ctx, http.MethodPost, "/QuickConnect/Initiate", nil, nil, &qc)
	if errors.Is(err, ErrUnauthorized) {
		return nil, ErrQuickConnectDisabled
	}
	if err != nil {
		return nil, fmt.Errorf("failed to start jellyfin quick connect: %w", err)
	}
	if qc.Secret == "" || qc.Code == "" {
		return nil, errors.New("jellyfin returned an incomplete quick connect request")
	}
	return &qc, nil
}

// QuickConnectState reports whether a user has approved the request.
func (c *LinkClient) QuickConnectState(ctx context.Context, secret string) (*QuickConnect, error) {
	var qc QuickConnect
	err := c.do(ctx, http.MethodGet, "/QuickConnect/Connect", url.Values{"secret": {secret}}, nil, &qc)
	if errors.Is(err, ErrItemNotFound) {
		return nil, ErrQuickConnectExpired
	}
	// Jellyfin answers 401 here when Quick Connect has been turned off.
	if errors.Is(err, ErrUnauthorized) {
		return nil, ErrQuickConnectDisabled
	}
	if err != nil {
		return nil, fmt.Errorf("failed to poll jellyfin quick connect: %w", err)
	}
	return &qc, nil
}

// AuthenticateWithQuickConnect trades an approved request for an access token.
func (c *LinkClient) AuthenticateWithQuickConnect(ctx context.Context, secret string) (string, error) {
	var result struct {
		AccessToken string `json:"AccessToken"` //nolint:tagliatelle // Jellyfin's API uses PascalCase JSON fields.
	}
	body := map[string]string{"Secret": secret}
	if err := c.do(ctx, http.MethodPost, "/Users/AuthenticateWithQuickConnect", nil, body, &result); err != nil {
		return "", fmt.Errorf("failed to authenticate with jellyfin quick connect: %w", err)
	}
	if result.AccessToken == "" {
		return "", errors.New("jellyfin returned no access token")
	}
	return result.AccessToken, nil
}
