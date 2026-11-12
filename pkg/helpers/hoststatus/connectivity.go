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

package hoststatus

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/useragent"
	"github.com/rs/zerolog/log"
)

// DefaultConnectivityURLs are plain-HTTP endpoints that answer 204 with no
// body. They are run by different operators so that one being blocked on a
// network does not read as the internet being down. Plain HTTP is deliberate:
// it is what lets a captive portal answer in the internet's place and be
// recognised.
var DefaultConnectivityURLs = []string{
	"http://connectivitycheck.gstatic.com/generate_204",
	"http://cp.cloudflare.com/generate_204",
	"http://edge-http.microsoft.com/captiveportal/generate_204",
}

const connectivityProbeTimeout = 3 * time.Second

// Prober decides whether the device can reach the internet by asking known
// endpoints for an answer nothing else would give.
type Prober struct {
	client *http.Client
	urls   []string
}

// NewProber returns a prober over urls. transport may be nil for the default.
func NewProber(urls []string, transport http.RoundTripper) *Prober {
	return &Prober{
		urls: urls,
		client: &http.Client{
			Timeout:   connectivityProbeTimeout,
			Transport: useragent.Transport(transport),
			// A redirect is a portal's answer, not something to follow.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// Probe asks each endpoint in turn and stops at the first that proves the
// internet is there. The empty state is returned when ctx ends first.
func (p *Prober) Probe(ctx context.Context) InternetState {
	portal := false
	for _, url := range p.urls {
		status, err := p.fetch(ctx, url)
		if ctx.Err() != nil {
			return ""
		}
		if err != nil {
			log.Debug().Err(err).Str("url", url).Msg("connectivity probe failed")
			continue
		}
		switch {
		case status == http.StatusNoContent:
			return InternetFull
		case status >= 200 && status < 400:
			portal = true
		}
	}
	if portal {
		return InternetPortal
	}
	return InternetNone
}

func (p *Prober) fetch(ctx context.Context, url string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return 0, err //nolint:wrapcheck // logged with the URL by the caller
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return 0, err //nolint:wrapcheck // logged with the URL by the caller
	}
	// A portal page is discarded unread beyond what lets the connection be
	// reused.
	_, _ = io.CopyN(io.Discard, resp.Body, 4096)
	if closeErr := resp.Body.Close(); closeErr != nil {
		log.Debug().Err(closeErr).Msg("closing connectivity probe response")
	}
	return resp.StatusCode, nil
}
