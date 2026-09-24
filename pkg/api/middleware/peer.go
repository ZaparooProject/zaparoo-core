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

type listenerKeyScopeKey struct{}

// ListenerKeyScope marks the base context of one listener so that the listener
// keys of NewListenerAuthConfig apply to its connections. Use it only from
// http.Server.BaseContext, which the server calls with the accepting listener:
// the mark then records which listener accepted a connection rather than what
// kind of address the peer has.
func ListenerKeyScope(ctx context.Context) context.Context {
	return context.WithValue(ctx, listenerKeyScopeKey{}, true)
}

// HasListenerKeyScope reports whether the request arrived on the listener
// marked with ListenerKeyScope.
func HasListenerKeyScope(r *http.Request) bool {
	scoped, ok := r.Context().Value(listenerKeyScopeKey{}).(bool)
	return ok && scoped
}

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

type untrustedLoopbackKey struct{}

// UntrustedLoopback marks the base context of a server whose loopback clients
// are not local. A service embedded in an app shares its loopback interface
// with every other app on the device, so an address of 127.0.0.1 says nothing
// about who is connecting; those clients authenticate, pair and are filtered
// and rate limited like any other network client. Use it only from
// http.Server.BaseContext so that no client input can remove the mark.
func UntrustedLoopback(ctx context.Context) context.Context {
	return context.WithValue(ctx, untrustedLoopbackKey{}, true)
}

// IsTrustedLoopback reports whether the request comes from a loopback address
// on a server that treats loopback as local. It is the only place a TCP peer
// can gain locality; every exemption for loopback clients must go through it.
func IsTrustedLoopback(r *http.Request) bool {
	return !loopbackUntrusted(r) && IsLoopbackAddr(r.RemoteAddr)
}

func loopbackUntrusted(r *http.Request) bool {
	untrusted, ok := r.Context().Value(untrustedLoopbackKey{}).(bool)
	return ok && untrusted
}

// IsLocalRequest classifies locality, not authentication. Unix requests still
// require an API key at the server boundary before receiving local authority.
func IsLocalRequest(r *http.Request) bool {
	return IsUnixPeer(r) || IsTrustedLoopback(r)
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
