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
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ErrPinExpired reports a link PIN plex.tv no longer knows.
var ErrPinExpired = errors.New("plex link code expired")

// Pin is a plex.tv link PIN. AuthToken is empty until the user enters Code
// at plex.tv/link.
type Pin struct {
	ExpiresAt time.Time `json:"expiresAt"`
	Code      string    `json:"code"`
	AuthToken string    `json:"authToken"`
	ID        FlexInt   `json:"id"`
	ExpiresIn FlexInt   `json:"expiresIn"`
}

// TVClient talks to plex.tv for account linking.
type TVClient struct {
	http       *http.Client
	baseURL    string
	clientsURL string
	clientID   string
}

// NewTVClient builds a plex.tv client. Empty URLs use plex.tv itself; a nil
// httpClient uses Core's default transport.
func NewTVClient(baseURL, clientsURL, clientID string, httpClient *http.Client) *TVClient {
	if baseURL == "" {
		baseURL = TVBaseURL
	}
	if clientsURL == "" {
		clientsURL = ClientsBaseURL
	}
	if httpClient == nil {
		httpClient = newHTTPClient()
	}
	return &TVClient{
		http:       httpClient,
		baseURL:    NormalizeURL(baseURL),
		clientsURL: NormalizeURL(clientsURL),
		clientID:   clientID,
	}
}

// ClientsURL is the plex.tv host an account token is stored against.
func (c *TVClient) ClientsURL() string { return c.clientsURL }

// CreatePin asks plex.tv for a short code the user enters at plex.tv/link.
func (c *TVClient) CreatePin(ctx context.Context) (*Pin, error) {
	var pin Pin
	err := doRequest(ctx, c.http, http.MethodPost, c.baseURL+"/api/v2/pins?strong=false",
		plexHeaders(c.clientID, ""), &pin)
	if err != nil {
		return nil, fmt.Errorf("failed to create plex link code: %w", err)
	}
	if pin.ID <= 0 || strings.TrimSpace(pin.Code) == "" {
		return nil, errors.New("plex.tv returned an incomplete link code")
	}
	return &pin, nil
}

// CheckPin returns the PIN's current state. A PIN plex.tv has dropped is
// ErrPinExpired.
func (c *TVClient) CheckPin(ctx context.Context, id int64) (*Pin, error) {
	var pin Pin
	err := doRequest(ctx, c.http, http.MethodGet, c.baseURL+"/api/v2/pins/"+strconv.FormatInt(id, 10),
		plexHeaders(c.clientID, ""), &pin)
	if errors.Is(err, ErrNotFound) {
		return nil, ErrPinExpired
	}
	if err != nil {
		return nil, fmt.Errorf("failed to check plex link code: %w", err)
	}
	return &pin, nil
}

// Resources lists every server and player on the account, relays included.
func (c *TVClient) Resources(ctx context.Context, token string) ([]Resource, error) {
	return fetchResources(ctx, c.http, c.clientsURL, c.clientID, token, true)
}
