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

package shared

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const maxRedirects = 10

// SameOriginRedirect is an http.Client CheckRedirect for clients that send a
// media server token in a header. Go forwards custom headers on every
// redirect, and Authorization to the same host even over plain http, so a
// redirect may only stay on the original scheme, host and port, or upgrade
// http to https on the same host.
func SameOriginRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return fmt.Errorf("stopped after %d redirects", maxRedirects)
	}
	if len(via) == 0 {
		return nil
	}
	from, to := via[0].URL, req.URL
	if !strings.EqualFold(from.Hostname(), to.Hostname()) {
		return fmt.Errorf("refused redirect to another host: %s", to.Redacted())
	}
	switch {
	case strings.EqualFold(from.Scheme, to.Scheme) && originPort(from) == originPort(to):
		return nil
	case strings.EqualFold(from.Scheme, "http") && strings.EqualFold(to.Scheme, "https"):
		return nil
	default:
		return fmt.Errorf("refused redirect off the server's origin: %s", to.Redacted())
	}
}

func originPort(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	if strings.EqualFold(u.Scheme, "https") {
		return "443"
	}
	return "80"
}
