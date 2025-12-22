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
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// ============================================================================
// LookupAuth Property Tests
// ============================================================================

// TestPropertyLookupAuthEmptyAlwaysNil verifies empty auth returns nil.
func TestPropertyLookupAuthEmptyAlwaysNil(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		// Generate any URL-like string
		url := rapid.StringMatching(`https?://[a-z]+\.[a-z]+(/[a-z]*)?`).Draw(t, "url")

		result := LookupAuth(Auth{}, url)
		if result != nil {
			t.Fatalf("Empty auth should return nil, got %v for URL %q", result, url)
		}
	})
}

// TestPropertyLookupAuthExactMatchReturns verifies exact matches work.
func TestPropertyLookupAuthExactMatchReturns(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		host := rapid.StringMatching(`[a-z]{3,10}`).Draw(t, "host")
		user := rapid.StringMatching(`[a-z]{3,10}`).Draw(t, "user")
		pass := rapid.StringMatching(`[a-z]{3,10}`).Draw(t, "pass")

		configURL := "https://" + host + ".com"
		cred := CredentialEntry{Username: user, Password: pass}

		authCfg := Auth{
			Creds: map[string]CredentialEntry{
				configURL: cred,
			},
		}

		result := LookupAuth(authCfg, configURL)
		if result == nil {
			t.Fatalf("Expected match for URL %q", configURL)
			return
		}
		if result.Username != user || result.Password != pass {
			t.Fatalf("Credential mismatch: expected %s/%s, got %s/%s",
				user, pass, result.Username, result.Password)
		}
	})
}

// TestPropertyLookupAuthCaseInsensitiveHost verifies host matching is case-insensitive.
func TestPropertyLookupAuthCaseInsensitiveHost(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		host := rapid.StringMatching(`[a-z]{3,10}`).Draw(t, "host")

		configURL := "https://" + strings.ToLower(host) + ".com"
		requestURL := "https://" + strings.ToUpper(host) + ".com"

		cred := CredentialEntry{Username: "user", Password: "pass"}
		authCfg := Auth{
			Creds: map[string]CredentialEntry{
				configURL: cred,
			},
		}

		result := LookupAuth(authCfg, requestURL)
		if result == nil {
			t.Fatalf("Case-insensitive host match failed: config=%q, request=%q",
				configURL, requestURL)
		}
	})
}

// TestPropertyLookupAuthPathPrefixMatch verifies path prefix matching works.
func TestPropertyLookupAuthPathPrefixMatch(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		host := rapid.StringMatching(`[a-z]{3,10}`).Draw(t, "host")
		basePath := "/" + rapid.StringMatching(`[a-z]{1,5}`).Draw(t, "basePath")
		subPath := "/" + rapid.StringMatching(`[a-z]{1,5}`).Draw(t, "subPath")

		configURL := "https://" + host + ".com" + basePath
		requestURL := "https://" + host + ".com" + basePath + subPath

		cred := CredentialEntry{Username: "user", Password: "pass"}
		authCfg := Auth{
			Creds: map[string]CredentialEntry{
				configURL: cred,
			},
		}

		result := LookupAuth(authCfg, requestURL)
		if result == nil {
			t.Fatalf("Path prefix match failed: config=%q, request=%q",
				configURL, requestURL)
		}
	})
}

// TestPropertyLookupAuthSchemeMismatchReturnsNil verifies scheme must match.
func TestPropertyLookupAuthSchemeMismatchReturnsNil(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		host := rapid.StringMatching(`[a-z]{3,10}`).Draw(t, "host")

		configURL := "https://" + host + ".com"
		requestURL := "http://" + host + ".com" // Different scheme

		cred := CredentialEntry{Username: "user", Password: "pass"}
		authCfg := Auth{
			Creds: map[string]CredentialEntry{
				configURL: cred,
			},
		}

		result := LookupAuth(authCfg, requestURL)
		if result != nil {
			t.Fatalf("Scheme mismatch should return nil: config=%q, request=%q",
				configURL, requestURL)
		}
	})
}

// ============================================================================
// isWindowsStylePath Property Tests
// ============================================================================

// TestPropertyIsWindowsStylePathDriveLetter verifies drive letter detection.
func TestPropertyIsWindowsStylePathDriveLetter(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		// Generate a valid drive letter (A-Z or a-z)
		driveLetters := []rune("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz")
		driveLetter := rapid.SampledFrom(driveLetters).Draw(t, "drive")
		path := string(driveLetter) + ":\\test"

		if !isWindowsStylePath(path) {
			t.Fatalf("Expected Windows path for %q", path)
		}
	})
}

// TestPropertyIsWindowsStylePathUNC verifies UNC path detection.
func TestPropertyIsWindowsStylePathUNC(t *testing.T) {
	t.Parallel()

	// Backslash UNC
	if !isWindowsStylePath("\\\\server\\share") {
		t.Fatal("Expected Windows path for UNC with backslashes")
	}

	// Forward slash UNC
	if !isWindowsStylePath("//server/share") {
		t.Fatal("Expected Windows path for UNC with forward slashes")
	}
}

// TestPropertyIsWindowsStylePathUnixNotWindows verifies Unix paths are not Windows.
func TestPropertyIsWindowsStylePathUnixNotWindows(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		// Generate Unix-style path starting with /
		pathPart := rapid.StringMatching(`[a-z]{1,10}`).Draw(t, "path")
		path := "/" + pathPart

		if isWindowsStylePath(path) {
			t.Fatalf("Unix path should not be Windows style: %q", path)
		}
	})
}
