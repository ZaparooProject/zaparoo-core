// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

package config

import (
	"maps"
	"net/url"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/rs/zerolog/log"
)

// CredentialEntry holds authentication credentials for a URL.
type CredentialEntry struct {
	Username string `toml:"username"`
	Password string `toml:"password"`
	Bearer   string `toml:"bearer"`
}

// schemeAliases maps protocol variants to their canonical form.
// This allows credentials configured for one scheme to match equivalent schemes.
var schemeAliases = map[string]string{
	"tcp": "mqtt",  // MQTT over TCP
	"ssl": "mqtts", // MQTT over TLS
	"ws":  "http",  // WebSocket
	"wss": "https", // WebSocket Secure
}

// authRootFormat represents the new clean format: ["url"] at root level
type authRootFormat map[string]CredentialEntry

// authCredsFormat represents the current format: [creds."url"]
type authCredsFormat struct {
	Creds map[string]CredentialEntry `toml:"creds"`
}

// authCredsWrapper is a wrapper for creds entries used by authAuthCredsFormat.
type authCredsWrapper struct {
	Creds map[string]CredentialEntry `toml:"creds"`
}

// authAuthCredsFormat represents the documented (but previously broken) format: [auth.creds."url"]
type authAuthCredsFormat struct {
	Auth authCredsWrapper `toml:"auth"`
}

// isValidAuthKey filters out TOML structural keys that get captured when
// parsing the root format in mixed-format files.
func isValidAuthKey(key string) bool {
	return key != "creds" && key != "auth"
}

// LoadAuthFromData parses auth.toml data supporting all three formats.
// Formats are merged, allowing users to mix formats in the same file.
//
// Supported formats:
//   - Root level: ["https://example.com"]
//   - Creds wrapper: [creds."https://example.com"]
//   - Auth.creds wrapper: [auth.creds."https://example.com"]
func LoadAuthFromData(data []byte) map[string]CredentialEntry {
	result := make(map[string]CredentialEntry)

	// Format 1: root level ["url"]
	var root authRootFormat
	if err := toml.Unmarshal(data, &root); err == nil {
		for k, v := range root {
			// Filter out non-URL keys like "creds" or "auth" that get captured
			// when parsing mixed-format files
			if isValidAuthKey(k) {
				result[k] = v
			}
		}
	}

	// Format 2: [creds."url"]
	var creds authCredsFormat
	if err := toml.Unmarshal(data, &creds); err == nil {
		maps.Copy(result, creds.Creds)
	}

	// Format 3: [auth.creds."url"]
	var authCreds authAuthCredsFormat
	if err := toml.Unmarshal(data, &authCreds); err == nil {
		maps.Copy(result, authCreds.Auth.Creds)
	}

	return result
}

// normalizeScheme converts scheme aliases to their canonical form.
// For example: tcp → mqtt, ssl → mqtts, ws → http, wss → https.
func normalizeScheme(scheme string) string {
	lower := strings.ToLower(scheme)
	if canonical, ok := schemeAliases[lower]; ok {
		return canonical
	}
	return lower
}

// isSchemelessKey returns true if the key does not contain a scheme (no "://").
func isSchemelessKey(key string) bool {
	return !strings.Contains(key, "://")
}

// LookupAuth finds credentials for a URL using fallback matching.
//
// The lookup tries 3 match types in order of decreasing specificity:
//  1. Exact scheme match - scheme, host, and path prefix must match exactly
//  2. Canonical scheme match - normalized schemes match (e.g., tcp://x matches mqtt://x config)
//  3. Schemeless host:port match - for entries like "broker:1883" that match any scheme
//
// This design allows:
//   - Strict scheme matching for security-sensitive protocols (http vs https)
//   - Flexible matching for protocols with multiple equivalent schemes (mqtt/tcp/ssl)
//   - Simple host:port entries for services where scheme doesn't matter
func LookupAuth(creds map[string]CredentialEntry, reqURL string) *CredentialEntry {
	if len(creds) == 0 {
		return nil
	}

	u, err := url.Parse(reqURL)
	if err != nil {
		log.Warn().Msgf("invalid auth request url: %s", reqURL)
		return nil
	}

	normalizedScheme := normalizeScheme(u.Scheme)
	hostPort := u.Host

	// Step 1: Exact scheme match (highest priority)
	for k, v := range creds {
		if isSchemelessKey(k) {
			continue
		}
		defURL, err := url.Parse(k)
		if err != nil {
			log.Error().Msgf("invalid auth config url: %s", k)
			continue
		}
		if strings.EqualFold(defURL.Scheme, u.Scheme) &&
			strings.EqualFold(defURL.Host, u.Host) &&
			strings.HasPrefix(u.Path, defURL.Path) {
			return &v
		}
	}

	// Step 2: Canonical scheme match (e.g., tcp://x matches mqtt://x config)
	for k, v := range creds {
		if isSchemelessKey(k) {
			continue
		}
		defURL, err := url.Parse(k)
		if err != nil {
			continue
		}
		if normalizeScheme(defURL.Scheme) == normalizedScheme &&
			strings.EqualFold(defURL.Host, u.Host) &&
			strings.HasPrefix(u.Path, defURL.Path) {
			return &v
		}
	}

	// Step 3: Schemeless host:port match (lowest priority, most flexible)
	for k, v := range creds {
		if !isSchemelessKey(k) {
			continue
		}
		if strings.EqualFold(k, hostPort) {
			return &v
		}
	}

	return nil
}
