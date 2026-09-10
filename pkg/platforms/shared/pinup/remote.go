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
package pinup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// EventEmuExit is the Popper function event, as accepted by the web remote's
// /pupkey/<id> route, that closes the running table through the emulator's
// exit script and returns Popper to the wheel. The number is Popper's own.
const EventEmuExit = 15

const (
	// DefaultServerPort is where Popper itself starts PuPServer when the web
	// remote is enabled in its menu script.
	DefaultServerPort = 80
	// CoreServerPort is the port Core uses when it has to start PuPServer.
	// VPin Studio uses 8091 for the same purpose, so a different port keeps the
	// two from fighting over one process.
	CoreServerPort = 8095
	// ServerSocketPort is the websocket port PuPServer requires alongside
	// -wwwport. Core only uses the HTTP side.
	ServerSocketPort = 8888

	remoteRequestTimeout = 5 * time.Second
	remoteBodyLimit      = 64 << 10
)

// HTTPDoer is the part of http.Client the remote needs.
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// Remote talks to Popper's web remote (PuPServer.exe). Every call is a GET
// that PuPServer answers with 200 while PinUpMenu.exe is running.
type Remote struct {
	client  HTTPDoer
	baseURL string
}

// ServerURL is the local web remote address for a port.
func ServerURL(port int) string {
	return "http://127.0.0.1:" + strconv.Itoa(port)
}

// NewRemote validates the base URL and returns a client for it.
func NewRemote(baseURL string, client HTTPDoer) (*Remote, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return nil, fmt.Errorf("parse PinUP Popper server URL: %w", err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, fmt.Errorf("PinUP Popper server URL %q must be http(s) with a host", baseURL)
	}
	if client == nil {
		return nil, errors.New("PinUP Popper remote needs an HTTP client")
	}
	return &Remote{client: client, baseURL: strings.TrimRight(parsed.String(), "/")}, nil
}

// BaseURL returns the normalised server URL.
func (r *Remote) BaseURL() string {
	return r.baseURL
}

// Probe checks that the web remote answers.
func (r *Remote) Probe(ctx context.Context) error {
	return r.get(ctx, "/function/getcuritem")
}

// LaunchGame asks Popper to start a table by GameID, exactly as selecting it
// on the wheel would.
func (r *Remote) LaunchGame(ctx context.Context, gameID int) error {
	if gameID <= 0 {
		return fmt.Errorf("invalid Popper game ID %d", gameID)
	}
	return r.get(ctx, "/function/launchgame/"+strconv.Itoa(gameID))
}

// SendEvent triggers one of Popper's function events.
func (r *Remote) SendEvent(ctx context.Context, event int) error {
	if event <= 0 {
		return fmt.Errorf("invalid Popper event %d", event)
	}
	return r.get(ctx, "/pupkey/"+strconv.Itoa(event))
}

func (r *Remote) get(ctx context.Context, path string) error {
	ctx, cancel := context.WithTimeout(ctx, remoteRequestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.baseURL+path, http.NoBody)
	if err != nil {
		return fmt.Errorf("build PinUP Popper request: %w", err)
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("PinUP Popper web remote %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, remoteBodyLimit))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("PinUP Popper web remote %s returned %s", path, resp.Status)
	}
	return nil
}
