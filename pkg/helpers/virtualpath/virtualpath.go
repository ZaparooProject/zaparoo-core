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

package virtualpath

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// We can use actual URIs for paths, but we also define a "Virtual Path", which
// is a slightly simplified version of the URI standard. The only major difference
// is that fragments aren't supported in virtual paths, but that also means we
// can't simply use the Go url.Parse function since it's so strict. That's why
// we reimplement parts of the standard library here.

// CreateVirtualPath creates a properly encoded virtual path for media
// Example: "kodi-show", "123", "Some Hot/Cold" -> "kodi-show://123/Some%20Hot%2FCold"
// Both id and name are URL-encoded to ensure round-trip compatibility with ParseVirtualPathStr
// Note: Simple alphanumeric IDs like "123" or "monkey1" remain unchanged after encoding
func CreateVirtualPath(scheme, id, name string) string {
	return fmt.Sprintf("%s://%s/%s", scheme, url.PathEscape(id), url.PathEscape(name))
}

// VirtualPathResult holds parsed virtual path components
type VirtualPathResult struct {
	Scheme string
	ID     string
	Name   string
}

// URIComponents holds parsed URI components
type URIComponents struct {
	Scheme string
	Rest   string // Everything after ://
	Query  string
}

// ContainsControlChar checks if a string contains any control characters (0x00-0x1F, 0x7F)
// Returns true if control characters are found (invalid for URLs)
func ContainsControlChar(s string) bool {
	for i := range len(s) {
		c := s[i]
		if c < 0x20 || c == 0x7F {
			return true
		}
	}
	return false
}

// IsValidScheme validates that a scheme follows RFC 3986 rules:
// - Must start with a letter
// - Can contain letters, digits, '+', '-', '.'
func IsValidScheme(scheme string) bool {
	if scheme == "" {
		return false
	}
	// First character must be a letter
	c := scheme[0]
	if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') {
		return false
	}
	// Remaining characters must be alphanumeric or +-.
	for i := 1; i < len(scheme); i++ {
		c := scheme[i]
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') &&
			(c < '0' || c > '9') && c != '+' && c != '-' && c != '.' {
			return false
		}
	}
	return true
}

// ParseURIComponents manually parses a URI into its components
// Note: Does not extract fragments - they remain part of name/query for custom schemes
// Returns empty result if URI contains control characters or has invalid scheme
func ParseURIComponents(uri string) URIComponents {
	var result URIComponents

	// Validate no control characters
	if ContainsControlChar(uri) {
		return result // Return empty - invalid URI
	}

	// Extract query (everything after ?)
	if idx := strings.Index(uri, "?"); idx >= 0 {
		result.Query = uri[idx+1:]
		uri = uri[:idx]
	}

	// Extract scheme (everything before ://)
	if idx := strings.Index(uri, "://"); idx >= 0 {
		result.Scheme = uri[:idx]
		result.Rest = uri[idx+3:] // Skip ://

		// Validate scheme format (RFC 3986)
		if !IsValidScheme(result.Scheme) {
			return URIComponents{} // Return empty - invalid scheme
		}
	} else {
		result.Rest = uri
	}

	return result
}

// ParseVirtualPathStr parses a virtual path and returns its components with string ID
// Virtual path format: scheme://id/name (e.g., "steam://123/Game%20Name")
// Maps to URL structure: scheme://host/path where host=id, path=name
// Uses manual parsing to handle malformed URLs gracefully
func ParseVirtualPathStr(virtualPath string) (VirtualPathResult, error) {
	var result VirtualPathResult

	parsed := ParseURIComponents(virtualPath)

	if parsed.Scheme == "" {
		return result, errors.New("not a virtual path")
	}

	result.Scheme = parsed.Scheme

	// Require at least some content after scheme:// (even if just a slash)
	if parsed.Rest == "" {
		return result, errors.New("missing ID in virtual path")
	}

	// Split rest into ID and name (ID is before first /, name is after)
	idAndName := strings.SplitN(parsed.Rest, "/", 2)
	if len(idAndName) < 1 {
		return result, errors.New("missing ID in virtual path")
	}

	// Support empty ID for legacy cards (e.g., "steam:///Name")
	// Decode the ID component for future-proofing (handles encoded characters)
	idPart := idAndName[0]
	if idPart != "" {
		decodedID, err := url.PathUnescape(idPart)
		if err == nil {
			result.ID = decodedID
		} else {
			result.ID = idPart // Graceful fallback for invalid encoding
		}
	} else {
		result.ID = idPart // Empty ID allowed for legacy support
	}

	if len(idAndName) == 2 {
		// Remove trailing slash
		namePart := strings.TrimSuffix(idAndName[1], "/")
		if namePart != "" {
			// Try to decode, fallback to raw on error
			decoded, err := url.PathUnescape(namePart)
			if err == nil {
				result.Name = decoded
			} else {
				result.Name = namePart // Graceful fallback for invalid encoding
			}
		}
	}

	return result, nil
}

// ExtractSchemeID extracts the ID component from a scheme-based virtual path
// with proper URL decoding. Returns error if the path doesn't match the expected scheme.
// Example: ExtractSchemeID("steam://123/Game%20Name", "steam") -> "123", nil
func ExtractSchemeID(virtualPath, expectedScheme string) (string, error) {
	result, err := ParseVirtualPathStr(virtualPath)
	if err != nil {
		return "", fmt.Errorf("failed to parse virtual path: %w", err)
	}

	// RFC 3986: scheme comparison is case-insensitive
	if !strings.EqualFold(result.Scheme, expectedScheme) {
		return "", fmt.Errorf("scheme mismatch: expected %s, got %s", expectedScheme, result.Scheme)
	}

	return result.ID, nil
}
