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
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/rs/zerolog/log"
)

type apiKeyAuthenticatedContextKey struct{}

func withAPIKeyAuthentication(r *http.Request) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), apiKeyAuthenticatedContextKey{}, true))
}

// APIKeyAuthenticated reports whether static API-key middleware authenticated
// this request.
func APIKeyAuthenticated(r *http.Request) bool {
	authenticated, ok := r.Context().Value(apiKeyAuthenticatedContextKey{}).(bool)
	return ok && authenticated
}

// APIKeyProvider is a function that returns the current list of API keys.
// This allows the auth middleware to dynamically fetch keys on each request,
// supporting hot-reload of configuration.
type APIKeyProvider func() []string

// AuthConfig holds authentication configuration for the API.
// It uses a provider function to fetch keys dynamically, supporting hot-reload.
type AuthConfig struct {
	getKeys APIKeyProvider
	// listenerKeys, when set, replaces getKeys for requests accepted by the
	// listener marked with ListenerKeyScope, and for no other request.
	listenerKeys APIKeyProvider
}

// NewAuthConfig creates a new AuthConfig with a key provider function.
// The provider is called on each request to get the current list of valid keys,
// allowing configuration changes to take effect without server restart.
func NewAuthConfig(keyProvider APIKeyProvider) *AuthConfig {
	return &AuthConfig{
		getKeys: keyProvider,
	}
}

// NewListenerAuthConfig creates an AuthConfig for a server with two listeners
// that must not share credentials. listenerKeys authenticates only requests
// whose connection was accepted by the listener marked with ListenerKeyScope;
// every other request is checked against networkKeys. The decision follows the
// accepting listener, never anything the client sends, and an unmarked request
// falls back to networkKeys so a lost mark cannot widen the listener keys.
func NewListenerAuthConfig(networkKeys, listenerKeys APIKeyProvider) *AuthConfig {
	return &AuthConfig{
		getKeys:      networkKeys,
		listenerKeys: listenerKeys,
	}
}

// keysFor returns the provider that authenticates this request's listener.
func (a *AuthConfig) keysFor(r *http.Request) APIKeyProvider {
	if a.listenerKeys != nil && HasListenerKeyScope(r) {
		return a.listenerKeys
	}
	return a.getKeys
}

// Enabled returns true if authentication is enabled (at least one key configured).
// It reports the network keys; requests are checked against their own listener.
func (a *AuthConfig) Enabled() bool {
	return keysEnabled(a.getKeys)
}

// IsValidKey checks if the provided key is valid using constant-time comparison
// to prevent timing attacks. It checks the network keys; requests are checked
// against their own listener.
func (a *AuthConfig) IsValidKey(key string) bool {
	return keyIsValid(a.getKeys, key)
}

func keysEnabled(getKeys APIKeyProvider) bool {
	keys := getKeys()
	for _, k := range keys {
		if k != "" {
			return true
		}
	}
	return false
}

func keyIsValid(getKeys APIKeyProvider, key string) bool {
	if key == "" {
		return false
	}

	keys := getKeys()
	var found bool
	for _, k := range keys {
		if k != "" && subtle.ConstantTimeCompare([]byte(k), []byte(key)) == 1 {
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
			getKeys := auth.keysFor(r)
			if !IsUnixPeer(r) && (!keysEnabled(getKeys) || IsTrustedLoopback(r)) {
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

			if !keyIsValid(getKeys, key) {
				log.Debug().
					Str("path", r.URL.Path).
					Str("method", r.Method).
					Msg("invalid API key")
				http.Error(w, "Unauthorized: Invalid API key", http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, withAPIKeyAuthentication(r))
		})
	}
}

// WebSocketAuthHandler validates WebSocket connection requests.
// Returns true if the connection is allowed, false otherwise.
// If no keys are configured or the request is from localhost, all connections are allowed.
func WebSocketAuthHandler(auth *AuthConfig, r *http.Request) bool {
	getKeys := auth.keysFor(r)
	if !IsUnixPeer(r) && (!keysEnabled(getKeys) || IsTrustedLoopback(r)) {
		return true
	}

	key := extractKey(r)
	if key == "" {
		log.Debug().
			Str("path", r.URL.Path).
			Msg("websocket API key required but not provided")
		return false
	}

	if !keyIsValid(getKeys, key) {
		log.Debug().
			Str("path", r.URL.Path).
			Msg("websocket invalid API key")
		return false
	}

	*r = *withAPIKeyAuthentication(r)
	return true
}
