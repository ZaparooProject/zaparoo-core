/*
Zaparoo Core
Copyright (c) 2026 The Zaparoo Project Contributors.
SPDX-License-Identifier: GPL-3.0-or-later

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

package helpers

import (
	"net/url"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/rs/zerolog/log"
)

// isValidPort checks if a port string is valid (empty or numeric)
func isValidPort(port string) bool {
	if port == "" {
		return true
	}
	// Must start with : and contain only digits
	if len(port) < 2 || port[0] != ':' {
		return false
	}
	for i := 1; i < len(port); i++ {
		if port[i] < '0' || port[i] > '9' {
			return false
		}
	}
	return true
}

func isHexDigit(b byte) bool {
	switch {
	case b >= '0' && b <= '9':
		return true
	case b >= 'a' && b <= 'f':
		return true
	case b >= 'A' && b <= 'F':
		return true
	default:
		return false
	}
}

func unhexByte(b byte) byte {
	switch {
	case b >= '0' && b <= '9':
		return b - '0'
	case b >= 'a' && b <= 'f':
		return b - 'a' + 10
	default:
		return b - 'A' + 10
	}
}

func shouldKeepCustomSegmentEscaped(r rune) bool {
	return r == '%' || r == '/' || r == '?' || r == '#' || r < 0x20 || r == 0x7F
}

// decodeCustomSchemeSegment decodes only percent-escaped bytes that are safe to
// materialize, preserving literal reserved characters and keeping structural
// escapes encoded so custom virtual paths keep their parse boundaries.
func decodeCustomSchemeSegment(raw string) string {
	if !strings.Contains(raw, "%") {
		return raw
	}

	decoded, err := url.PathUnescape(raw)
	if err != nil || !utf8.ValidString(decoded) {
		return raw
	}

	var result strings.Builder
	result.Grow(len(raw))

	for i := 0; i < len(raw); {
		if raw[i] != '%' {
			_ = result.WriteByte(raw[i])
			i++
			continue
		}

		start := i
		decodedBytes := make([]byte, 0, 4)
		for i+2 < len(raw) && raw[i] == '%' && isHexDigit(raw[i+1]) && isHexDigit(raw[i+2]) {
			decodedBytes = append(decodedBytes, unhexByte(raw[i+1])<<4|unhexByte(raw[i+2]))
			i += 3
		}

		if len(decodedBytes) == 0 || !utf8.Valid(decodedBytes) {
			return raw
		}

		rawOffset := start
		for len(decodedBytes) > 0 {
			r, size := utf8.DecodeRune(decodedBytes)
			if r == utf8.RuneError && size == 1 {
				return raw
			}

			rawChunk := raw[rawOffset : rawOffset+size*3]
			if shouldKeepCustomSegmentEscaped(r) {
				_, _ = result.WriteString(rawChunk)
			} else {
				_, _ = result.WriteRune(r)
			}

			rawOffset += size * 3
			decodedBytes = decodedBytes[size:]
		}
	}

	decodedResult := result.String()
	if strings.Count(raw, "#") == 1 {
		return strings.Replace(decodedResult, "#", "%23", 1)
	}

	return decodedResult
}

// DecodeURIIfNeeded applies URL decoding to URIs based on their scheme
// - Zaparoo custom schemes (steam://, kodi-*://, etc.): uses virtualpath.ParseVirtualPathStr for full decoding
// - Standard web schemes (http://, https://): decodes path component only
// - Other schemes: returns as-is (no decoding)
// Returns the original URI on decoding errors (graceful fallback)
// Uses manual parsing to handle malformed URLs gracefully
func DecodeURIIfNeeded(uri string) string {
	if !utf8.ValidString(uri) {
		return ""
	}
	// Quick check: only decode if contains both :// and % (URL encoding)
	if !strings.Contains(uri, "://") || !strings.Contains(uri, "%") {
		return uri
	}

	parsed := virtualpath.ParseURIComponents(uri)

	if parsed.Scheme == "" {
		return uri
	}

	schemeLower := strings.ToLower(parsed.Scheme)

	// Handle Zaparoo custom virtual paths
	if shared.IsCustomScheme(schemeLower) {
		// Decode each path segment independently so literal reserved characters
		// stay untouched while percent-escaped structural bytes remain encoded.
		rest := strings.TrimRight(parsed.Rest, "/")
		segments := strings.Split(rest, "/")
		for i, seg := range segments {
			segments[i] = decodeCustomSchemeSegment(seg)
		}
		reconstructed := parsed.Scheme + "://" + strings.Join(segments, "/")
		if parsed.Query != "" {
			reconstructed += "?" + parsed.Query
		}
		return reconstructed
	}

	// Handle standard web schemes (http/https)
	if shared.IsStandardSchemeForDecoding(schemeLower) {
		// Per RFC 3986, '#' introduces the fragment and takes precedence over
		// '?', which ParseURIComponents picks up as the query separator.
		// Extract fragment from the raw URI first so a fragment containing '?'
		// does not shift into the query component.
		var fragment string
		fragURI := uri
		if idx := strings.Index(uri, "#"); idx >= 0 {
			fragment = uri[idx+1:]
			fragURI = uri[:idx]
		}
		hParsed := virtualpath.ParseURIComponents(fragURI)
		query := hParsed.Query

		// Split rest into userinfo@host and path
		// Format: [userinfo@]host/path
		var userinfo, host, pathPart string
		rest := hParsed.Rest

		// Check for userinfo (use LastIndex to handle @ in passwords)
		if idx := strings.LastIndex(rest, "@"); idx >= 0 {
			userinfo = rest[:idx]
			rest = rest[idx+1:] // Everything after the last @
		}

		// Split host and path (first / separates them)
		// Special handling for IPv6 addresses in brackets
		if strings.HasPrefix(rest, "[") {
			// IPv6 literal - find closing bracket
			closeBracket := strings.Index(rest, "]")
			if closeBracket < 0 {
				// Malformed IPv6 - missing closing bracket
				log.Debug().Str("uri", uri).Msg("malformed IPv6 address - missing ]")
				return uri
			}
			// Host includes brackets and optional port after ]
			endOfHost := closeBracket + 1
			// Check for port after ] or path
			switch {
			case endOfHost < len(rest) && rest[endOfHost] == ':':
				// Port after ]
				portEnd := strings.Index(rest[endOfHost:], "/")
				if portEnd >= 0 {
					portEnd += endOfHost
					port := rest[endOfHost:portEnd]
					if !isValidPort(port) {
						log.Debug().Str("uri", uri).Str("port", port).Msg("invalid port in URI")
						return uri
					}
					host = rest[:portEnd]
					pathPart = rest[portEnd:]
				} else {
					port := rest[endOfHost:]
					if !isValidPort(port) {
						log.Debug().Str("uri", uri).Str("port", port).Msg("invalid port in URI")
						return uri
					}
					host = rest
					pathPart = ""
				}
			case endOfHost < len(rest) && rest[endOfHost] == '/':
				// Path after ]
				host = rest[:endOfHost]
				pathPart = rest[endOfHost:]
			default:
				// Just the IPv6 address
				host = rest
				pathPart = ""
			}
		} else {
			// Non-IPv6 host
			if idx := strings.Index(rest, "/"); idx >= 0 {
				host = rest[:idx]
				pathPart = rest[idx:] // Include leading /
				// Validate port if present
				if portIdx := strings.Index(host, ":"); portIdx >= 0 {
					port := host[portIdx:]
					if !isValidPort(port) {
						log.Debug().Str("uri", uri).Str("port", port).Msg("invalid port in URI")
						return uri
					}
				}
			} else {
				host = rest
				pathPart = ""
				// Validate port if present
				if portIdx := strings.Index(host, ":"); portIdx >= 0 {
					port := host[portIdx:]
					if !isValidPort(port) {
						log.Debug().Str("uri", uri).Str("port", port).Msg("invalid port in URI")
						return uri
					}
				}
			}
		}

		// Decode the path component
		decodedPath := pathPart
		if pathPart != "" {
			decoded, err := url.PathUnescape(pathPart)
			if err == nil && utf8.ValidString(decoded) {
				decodedPath = decoded
			} else if err != nil {
				log.Debug().Err(err).Str("uri", uri).Msg("failed to decode web URI path, using as-is")
			}
		}

		// Reconstruct URL: scheme://[userinfo@]host/decodedPath?query#fragment
		reconstructed := schemeLower + "://"
		if userinfo != "" {
			reconstructed += userinfo + "@"
		}
		reconstructed += host
		reconstructed += decodedPath
		if query != "" {
			reconstructed += "?" + query
		}
		if fragment != "" {
			reconstructed += "#" + fragment
		}
		return reconstructed
	}

	// Other schemes: no decoding
	return uri
}

func FilenameFromPath(p string) string {
	if p == "" || !utf8.ValidString(p) {
		return ""
	}

	// Handle URIs with manual parsing
	if strings.Contains(p, "://") {
		parsed := virtualpath.ParseURIComponents(p)

		if parsed.Scheme != "" {
			schemeLower := strings.ToLower(parsed.Scheme)

			// For custom Zaparoo schemes, use virtualpath.ParseVirtualPathStr
			if shared.IsCustomScheme(schemeLower) {
				result, err := virtualpath.ParseVirtualPathStr(p)
				if err == nil {
					// If no name component and no slash in rest, return the ID (legacy card support)
					// Example: "kodi-episode://666" → return "666"
					// But: "kodi-movie://555/" → return "" (has slash, empty name)
					if result.Name == "" {
						if !strings.Contains(parsed.Rest, "/") {
							return result.ID
						}
						return ""
					}

					// Check if the original path had URL-encoded slashes (%2F)
					// Split rest to get the name component (everything after first /)
					idAndName := strings.SplitN(parsed.Rest, "/", 2)
					if len(idAndName) == 2 {
						originalName := idAndName[1]
						// If the original has %2F, the slashes are part of the title
						// Return the full decoded name
						if strings.Contains(originalName, "%2F") || strings.Contains(originalName, "%2f") {
							return result.Name
						}
					}

					// No %2F encoding - unencoded slashes are path separators
					// Extract only the last segment
					if strings.Contains(result.Name, "/") {
						segments := strings.Split(result.Name, "/")
						return segments[len(segments)-1]
					}
					return result.Name
				}
			}

			// For http/https URLs, extract and decode the last path segment
			if shared.IsStandardSchemeForDecoding(schemeLower) {
				// Split rest into host and path
				var pathPart string
				rest := parsed.Rest
				// Strip fragment if present (for http/https)
				if idx := strings.Index(rest, "#"); idx >= 0 {
					rest = rest[:idx]
				}
				idx := strings.Index(rest, "/")
				if idx < 0 {
					// No path component - return domain as-is (e.g., "https://example.com" → "example.com")
					return rest
				}
				pathPart = rest[idx:] // Include leading /

				if pathPart != "" {
					// Trailing slash means directory, not a file
					if strings.HasSuffix(pathPart, "/") {
						return ""
					}
					// Get the last path segment
					lastSegment := path.Base(pathPart)
					// Empty or "/" or "." means no filename
					if lastSegment == "" || lastSegment == "/" || lastSegment == "." {
						return ""
					}
					// Decode URL encoding and return with extension intact
					decoded, err := url.PathUnescape(lastSegment)
					if err == nil && utf8.ValidString(decoded) {
						return decoded
					}
				}
			}
		}

		// Fallback: treat as regular path
	}

	// Regular file path - use existing logic
	// Convert to forward slash format for consistent cross-platform parsing
	// Replace backslashes with forward slashes to handle Windows paths on any OS
	normalizedPath := strings.ReplaceAll(p, "\\", "/")
	b := path.Base(normalizedPath)
	if b == "/" || b == "." {
		return ""
	}
	e := path.Ext(normalizedPath)
	if !IsValidExtension(e) {
		e = ""
	}
	r, _ := strings.CutSuffix(b, e)
	return r
}

// IsValidExtension checks if a file extension contains only valid characters
// Valid extensions contain only alphanumeric characters (and the leading dot)
// Examples: ".zip" ✓, ".tar" ✓, ".mp3" ✓, ".other thing" ✗, ".file-name" ✗
func IsValidExtension(ext string) bool {
	if ext == "" {
		return false
	}
	// Skip the leading dot
	if ext[0] == '.' {
		ext = ext[1:]
	}
	// Empty after removing dot means just "." which is invalid
	if ext == "" {
		return false
	}
	// Check each character is alphanumeric
	for _, ch := range ext {
		if (ch < 'a' || ch > 'z') && (ch < 'A' || ch > 'Z') && (ch < '0' || ch > '9') {
			return false
		}
	}
	return true
}
