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

package publishers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	pathpkg "path"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/rs/zerolog/log"
)

const (
	pixelCadeDefaultPort    = 8080
	pixelCadeRequestTimeout = 5 * time.Second

	PixelCadeModeStream = "stream"
	PixelCadeModeWrite  = "write"

	PixelCadeOnStopBlank   = "blank"
	PixelCadeOnStopMarquee = "marquee"
	PixelCadeOnStopNone    = "none"
)

// PixelCadePublisher transforms Zaparoo notifications into PixelCade REST API
// calls to display game marquee images on LED displays.
type PixelCadePublisher struct {
	client  *http.Client
	ctx     context.Context
	cancel  context.CancelFunc
	host    string
	baseURL string
	mode    string
	onStop  string
	filter  []string
}

// NewPixelCadePublisher creates a new PixelCade publisher for the given host
// and configuration options.
func NewPixelCadePublisher(host string, port int, mode, onStop string, filter []string) *PixelCadePublisher {
	if port == 0 {
		port = pixelCadeDefaultPort
	}
	if mode == "" {
		mode = PixelCadeModeStream
	}
	if onStop == "" {
		onStop = PixelCadeOnStopBlank
	}
	return &PixelCadePublisher{
		host:    host,
		baseURL: fmt.Sprintf("http://%s:%d", host, port),
		mode:    mode,
		onStop:  onStop,
		filter:  filter,
		client: &http.Client{
			Timeout: pixelCadeRequestTimeout,
		},
	}
}

// Start initializes the PixelCade publisher. Returns an error if the
// configuration is invalid.
func (p *PixelCadePublisher) Start(ctx context.Context) error {
	if p.host == "" {
		return errors.New("pixelcade publisher: host is required")
	}

	switch p.mode {
	case PixelCadeModeStream, PixelCadeModeWrite:
	default:
		return fmt.Errorf("pixelcade publisher: invalid mode %q (must be %q or %q)",
			p.mode, PixelCadeModeStream, PixelCadeModeWrite)
	}

	switch p.onStop {
	case PixelCadeOnStopBlank, PixelCadeOnStopMarquee, PixelCadeOnStopNone:
	default:
		return fmt.Errorf("pixelcade publisher: invalid on_stop %q (must be %q, %q, or %q)",
			p.onStop, PixelCadeOnStopBlank, PixelCadeOnStopMarquee, PixelCadeOnStopNone)
	}

	p.ctx, p.cancel = context.WithCancel(ctx)

	log.Info().Msgf(
		"pixelcade publisher: initialized (%s, mode: %s, on_stop: %s)",
		p.baseURL, p.mode, p.onStop,
	)
	return nil
}

// Publish handles a notification by transforming it into a PixelCade API call.
// Only media.started and media.stopped notifications are handled; all others
// are silently ignored.
func (p *PixelCadePublisher) Publish(notif models.Notification) error {
	if !MatchesFilter(p.filter, notif.Method) {
		return nil
	}

	switch notif.Method {
	case models.NotificationStarted:
		return p.handleMediaStarted(notif.Params)
	case models.NotificationStopped:
		return p.handleMediaStopped()
	default:
		return nil
	}
}

// Stop cancels the publisher context and releases resources.
func (p *PixelCadePublisher) Stop() {
	if p.cancel != nil {
		p.cancel()
	}
	log.Debug().Msg("pixelcade publisher: stopped")
}

func (p *PixelCadePublisher) handleMediaStarted(params json.RawMessage) error {
	var started models.MediaStartedParams
	if err := json.Unmarshal(params, &started); err != nil {
		return fmt.Errorf("pixelcade: failed to unmarshal media.started params: %w", err)
	}

	console := url.PathEscape(pixelCadeConsoleName(started.SystemID))
	rom := url.PathEscape(pixelCadeMediaIdentifier(started.MediaPath))
	query := url.Values{"event": {"GameStart"}}

	reqURL := fmt.Sprintf("%s/arcade/%s/%s/%s?%s", p.baseURL, p.mode, console, rom, query.Encode())
	return p.doRequest(reqURL)
}

func pixelCadeMediaIdentifier(mediaPath string) string {
	normalizedPath := strings.ReplaceAll(mediaPath, "\\", "/")
	identifier := pathpkg.Base(normalizedPath)

	if ext := pathpkg.Ext(identifier); ext != "" {
		identifier = strings.TrimSuffix(identifier, ext)
	}

	return identifier
}

func (p *PixelCadePublisher) handleMediaStopped() error {
	switch p.onStop {
	case PixelCadeOnStopBlank:
		return p.doRequest(p.baseURL + "/arcade/stream/black/dummy")
	case PixelCadeOnStopMarquee:
		return p.doRequest(p.baseURL + "/arcade/write/marquee/dummy")
	case PixelCadeOnStopNone:
		return nil
	default:
		return nil
	}
}

func (p *PixelCadePublisher) doRequest(reqURL string) error {
	req, err := http.NewRequestWithContext(p.ctx, http.MethodGet, reqURL, http.NoBody)
	if err != nil {
		return fmt.Errorf("pixelcade: failed to create request: %w", err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("pixelcade: request to %s failed: %w", reqURL, err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("pixelcade: unexpected status %d from %s", resp.StatusCode, reqURL)
	}

	log.Debug().Msgf("pixelcade publisher: sent request to %s", reqURL)
	return nil
}
