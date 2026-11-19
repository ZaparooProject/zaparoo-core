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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const (
	deviceInitPath  = "/api/auth/device/init"
	deviceTokenPath = "/api/auth/device/token" //nolint:gosec // G101: an API path, not a credential.
	// maxAuthBody caps a device-auth response.
	maxAuthBody = 64 << 10
)

// DeviceScopes are the permissions a linked device asks for: enough to list
// platforms and ROMs, download them and fetch their artwork.
var DeviceScopes = []string{"platforms.read", "roms.read", "assets.read"}

// Errors a device-token poll ends with, by RomM's OAuth device-flow codes.
var (
	// ErrLinkPending means the user has not approved the request yet.
	ErrLinkPending = errors.New("romm link approval pending")
	// ErrLinkSlowDown means the device polled faster than the server allows.
	ErrLinkSlowDown = errors.New("romm link polled too fast")
	// ErrLinkDenied means the user turned the request down.
	ErrLinkDenied = errors.New("romm link request denied")
	// ErrLinkExpired means the request expired or was already used.
	ErrLinkExpired = errors.New("romm link request expired")
)

// DeviceLinkRequest describes the device asking to be linked.
type DeviceLinkRequest struct {
	Identifier string
	Name       string
	Platform   string
	Version    string
}

// DeviceLink is a started link request. VerificationURL and
// VerificationURLComplete are absolute, under the server's URL.
type DeviceLink struct {
	DeviceCode              string
	UserCode                string
	VerificationURL         string
	VerificationURLComplete string
	ExpiresIn               int
	Interval                int
}

//nolint:tagliatelle // RomM's API uses snake_case JSON fields.
type deviceInitPayload struct {
	Identifier      string   `json:"client_device_identifier"`
	Name            string   `json:"name"`
	Client          string   `json:"client"`
	Platform        string   `json:"platform,omitempty"`
	Version         string   `json:"client_version,omitempty"`
	RequestedScopes []string `json:"requested_scopes"`
}

//nolint:tagliatelle // RomM's API uses snake_case JSON fields.
type deviceInitResponse struct {
	DeviceCode               string `json:"device_code"`
	UserCode                 string `json:"user_code"`
	VerificationPath         string `json:"verification_path"`
	VerificationPathComplete string `json:"verification_path_complete"`
	ExpiresIn                int    `json:"expires_in"`
	Interval                 int    `json:"interval"`
}

//nolint:tagliatelle // RomM's API uses snake_case JSON fields.
type deviceTokenPayload struct {
	DeviceCode string `json:"device_code"`
}

//nolint:tagliatelle // RomM's API uses snake_case JSON fields.
type deviceTokenResponse struct {
	AccessToken string `json:"access_token"`
}

type errorResponse struct {
	Detail any `json:"detail"`
}

// truncateField bounds a server-chosen string kept in Core's own fields.
func truncateField(s string, limit int) string {
	if len(s) > limit {
		return s[:limit]
	}
	return s
}

// StartDeviceLink asks the server to start a device link. The user approves
// it in RomM's web interface, after which PollDeviceToken returns the token.
func (c *Client) StartDeviceLink(ctx context.Context, req DeviceLinkRequest) (*DeviceLink, error) {
	payload := deviceInitPayload{
		Identifier:      truncateField(req.Identifier, 255),
		Name:            truncateField(req.Name, 255),
		Client:          "Zaparoo Core",
		Platform:        truncateField(req.Platform, 50),
		Version:         truncateField(req.Version, 50),
		RequestedScopes: DeviceScopes,
	}
	var created deviceInitResponse
	status, err := c.postJSON(ctx, deviceInitPath, payload, &created)
	if err != nil {
		return nil, err
	}
	if status != http.StatusCreated && status != http.StatusOK {
		if status == http.StatusNotFound || status == http.StatusMethodNotAllowed {
			return nil, fmt.Errorf("romm server does not support device linking (status %d), update it to 5.0 or later",
				status)
		}
		return nil, fmt.Errorf("romm server returned status %d", status)
	}
	if created.DeviceCode == "" || created.UserCode == "" {
		return nil, errors.New("romm link response is missing its device or user code")
	}
	verification, ok := c.webURL(created.VerificationPath)
	if !ok {
		return nil, errors.New("romm link response has an unusable verification path")
	}
	complete, ok := c.webURL(created.VerificationPathComplete)
	if !ok {
		complete = verification
	}
	return &DeviceLink{
		DeviceCode:              created.DeviceCode,
		UserCode:                created.UserCode,
		VerificationURL:         verification,
		VerificationURLComplete: complete,
		ExpiresIn:               created.ExpiresIn,
		Interval:                created.Interval,
	}, nil
}

// PollDeviceToken asks once whether a link was approved, and returns the
// access token when it was. A request still waiting answers ErrLinkPending
// or ErrLinkSlowDown; ErrLinkDenied and ErrLinkExpired end the flow.
func (c *Client) PollDeviceToken(ctx context.Context, deviceCode string) (string, error) {
	var (
		token  deviceTokenResponse
		failed errorResponse
	)
	status, err := c.postJSON(ctx, deviceTokenPath, deviceTokenPayload{DeviceCode: deviceCode}, &token, &failed)
	if err != nil {
		return "", err
	}
	switch status {
	case http.StatusOK:
		if strings.TrimSpace(token.AccessToken) == "" {
			return "", errors.New("romm token response has no access token")
		}
		return token.AccessToken, nil
	case http.StatusBadRequest:
		// RomM answers a validation failure with a list here, not a string.
		detail, isString := failed.Detail.(string)
		if !isString {
			return "", errors.New("romm server rejected the token request")
		}
		switch detail {
		case "authorization_pending":
			return "", ErrLinkPending
		case "slow_down":
			return "", ErrLinkSlowDown
		case "access_denied":
			return "", ErrLinkDenied
		case "expired_token":
			return "", ErrLinkExpired
		}
		return "", errors.New("romm server rejected the token request")
	case http.StatusTooManyRequests:
		return "", ErrLinkSlowDown
	default:
		if status >= 500 {
			return "", fmt.Errorf("%w: server returned status %d", errUnreachable, status)
		}
		return "", fmt.Errorf("romm server returned status %d", status)
	}
}

// postJSON sends body to an API path without credentials, since a device
// that is linking has none, and decodes a 200 or 201 answer into ok and a
// 400 answer into failed, when given. It returns the status code.
func (c *Client) postJSON(ctx context.Context, p string, body, ok any, failed ...any) (int, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return 0, fmt.Errorf("encode romm request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(p, nil), bytes.NewReader(data))
	if err != nil {
		return 0, fmt.Errorf("create romm request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req) //nolint:gosec // G704: URL is built from the server the user named.
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return 0, fmt.Errorf("romm request: %w", ctxErr)
		}
		return 0, fmt.Errorf("%w: %w", errUnreachable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxAuthBody+1))
	if err != nil {
		return 0, fmt.Errorf("%w: reading response: %w", errUnreachable, err)
	}
	if len(raw) > maxAuthBody {
		return 0, fmt.Errorf("romm response exceeds %d bytes", maxAuthBody)
	}
	var into any
	switch {
	case resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated:
		into = ok
	case resp.StatusCode == http.StatusBadRequest && len(failed) > 0:
		into = failed[0]
	}
	if into != nil {
		if err := json.Unmarshal(raw, into); err != nil {
			return 0, fmt.Errorf("decode romm response: %w", err)
		}
	}
	return resp.StatusCode, nil
}

// webURL joins a path RomM returns for its web interface, such as
// /pair/device?user_code=X, onto the server URL. Anything that is not a
// plain absolute path is refused, so the server cannot point the user at
// another host.
func (c *Client) webURL(p string) (string, bool) {
	p = strings.TrimSpace(p)
	if !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") || strings.ContainsAny(p, "\\\x00") {
		return "", false
	}
	ref, err := url.Parse(p)
	if err != nil || ref.Scheme != "" || ref.Host != "" || ref.User != nil {
		return "", false
	}
	u := *c.base
	u.Path = c.base.Path + ref.Path
	u.RawPath = ""
	u.RawQuery = ref.RawQuery
	u.Fragment = ""
	return u.String(), true
}
