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
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/rs/zerolog/log"
)

// AuthConfig holds authentication configuration for the API.
// Authentication is enabled when at least one API key is configured.
type AuthConfig struct {
	keys map[string]struct{}
}

// NewAuthConfig creates a new AuthConfig from a list of API keys.
// If no keys are provided, authentication is disabled.
func NewAuthConfig(keys []string) *AuthConfig {
	keySet := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		if k != "" {
			keySet[k] = struct{}{}
		}
	}
	return &AuthConfig{
		keys: keySet,
	}
}

// Enabled returns true if authentication is enabled (at least one key configured).
func (a *AuthConfig) Enabled() bool {
	return len(a.keys) > 0
}

// IsValidKey checks if the provided key is valid using constant-time comparison
// to prevent timing attacks.
func (a *AuthConfig) IsValidKey(key string) bool {
	if key == "" {
		return false
	}

	// Iterate through all keys and use constant-time comparison for each.
	// We check all keys to maintain constant time regardless of which key matches.
	var found bool
	for k := range a.keys {
		if subtle.ConstantTimeCompare([]byte(k), []byte(key)) == 1 {
			found = true
		}
	}
	return found
}

// extractKey extracts the API key from the request.
// Checks Authorization header first (Bearer token), then falls back to "key" query parameter.
func extractKey(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if token, found := strings.CutPrefix(auth, "Bearer "); found {
		return token
	}
	return r.URL.Query().Get("key")
}

// HTTPAuthMiddleware creates an HTTP middleware that validates API key authentication.
// If no keys are configured or the request is from localhost, all requests pass through.
// Returns 401 Unauthorized if keys are configured but no valid key is provided.
func HTTPAuthMiddleware(auth *AuthConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !auth.Enabled() || IsLoopbackAddr(r.RemoteAddr) {
				next.ServeHTTP(w, r)
				return
			}

			key := extractKey(r)
			if key == "" {
				log.Debug().
					Str("path", r.URL.Path).
					Str("method", r.Method).
					Msg("API key required but not provided")
				http.Error(w, "Unauthorized: API key required", http.StatusUnauthorized)
				return
			}

			if !auth.IsValidKey(key) {
				log.Debug().
					Str("path", r.URL.Path).
					Str("method", r.Method).
					Msg("invalid API key")
				http.Error(w, "Unauthorized: Invalid API key", http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// WebSocketAuthHandler validates WebSocket connection requests.
// Returns true if the connection is allowed, false otherwise.
// If no keys are configured or the request is from localhost, all connections are allowed.
func WebSocketAuthHandler(auth *AuthConfig, r *http.Request) bool {
	if !auth.Enabled() || IsLoopbackAddr(r.RemoteAddr) {
		return true
	}

	key := extractKey(r)
	if key == "" {
		log.Debug().
			Str("path", r.URL.Path).
			Msg("websocket API key required but not provided")
		return false
	}

	if !auth.IsValidKey(key) {
		log.Debug().
			Str("path", r.URL.Path).
			Msg("websocket invalid API key")
		return false
	}

	return true
}
