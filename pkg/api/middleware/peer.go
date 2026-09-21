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

package middleware

import (
	"context"
	"net"
	"net/http"
)

type unixPeerKey struct{}

// PeerContext is an http.Server.ConnContext hook. Only server-observed transport,
// never RemoteAddr text or forwarded headers, can grant Unix-peer classification.
func PeerContext(ctx context.Context, conn net.Conn) context.Context {
	if addr := conn.LocalAddr(); addr != nil && addr.Network() == "unix" {
		return context.WithValue(ctx, unixPeerKey{}, true)
	}
	return ctx
}

func IsUnixPeer(r *http.Request) bool {
	unix, ok := r.Context().Value(unixPeerKey{}).(bool)
	return ok && unix
}

// IsLocalRequest classifies locality, not authentication. Unix requests still
// require an API key at the server boundary before receiving local authority.
func IsLocalRequest(r *http.Request) bool {
	return IsUnixPeer(r) || IsLoopbackAddr(r.RemoteAddr)
}

// UnixAuthMiddleware protects the whole private listener, including routes that
// intentionally permit remote bootstrap on TCP. Health alone remains available
// before credentials are delivered to the host. Missing key configuration denies
// access rather than turning authentication off.
func UnixAuthMiddleware(auth *AuthConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		authenticated := HTTPAuthMiddleware(auth)(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if IsUnixPeer(r) && r.URL.Path != "/health" {
				authenticated.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
