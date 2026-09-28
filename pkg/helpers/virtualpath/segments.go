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

package virtualpath

import (
	"errors"
	"net/url"
	"strings"
	"unicode/utf8"
)

// A virtual path's name may be a path of several segments rather than one
// title: scheme://id/dir/sub/file. Each segment is escaped on its own, so an
// unescaped slash is always a separator and never part of a name. This is the
// form for media that lives in a folder tree Core cannot open directly. The
// single-name form made by CreateVirtualPath is the one-segment case, except
// that it escapes a slash inside its name.

// Bounds on a multi-segment virtual path. They keep a hostile or broken
// source from producing identities the database and API cannot carry.
const (
	maxSegments      = 64
	maxSegmentLength = 1024
	maxPathLength    = 16384
)

// ErrInvalidSegments reports a multi-segment virtual path that is not in its
// canonical form, or a segment that cannot be part of one.
var ErrInvalidSegments = errors.New("invalid multi-segment virtual path")

// ValidSegment reports whether name can be one segment of a multi-segment
// virtual path: non-empty, not "." or "..", valid UTF-8, bounded, and free of
// control characters and path separators.
func ValidSegment(name string) bool {
	if name == "" || name == "." || name == ".." || len(name) > maxSegmentLength ||
		!utf8.ValidString(name) {
		return false
	}
	for _, r := range name {
		if r < 32 || r == 127 || r == '/' || r == '\\' {
			return false
		}
	}
	return true
}

// CreateVirtualPathSegments builds scheme://id/seg/.../seg, escaping the ID
// and each segment on its own. The result is the canonical form that
// ParseVirtualPathSegments accepts.
func CreateVirtualPathSegments(scheme, id string, segments []string) (string, error) {
	if !IsValidScheme(scheme) || id == "" || ContainsControlChar(id) ||
		len(segments) == 0 || len(segments) > maxSegments {
		return "", ErrInvalidSegments
	}
	escaped := make([]string, len(segments))
	for i, segment := range segments {
		if !ValidSegment(segment) {
			return "", ErrInvalidSegments
		}
		escaped[i] = url.PathEscape(segment)
	}
	value := scheme + "://" + url.PathEscape(id) + "/" + strings.Join(escaped, "/")
	if len(value) > maxPathLength {
		return "", ErrInvalidSegments
	}
	return value, nil
}

// SegmentsResult is a parsed multi-segment virtual path.
type SegmentsResult struct {
	Scheme   string
	ID       string
	Segments []string
}

// ParseVirtualPathSegments splits a multi-segment virtual path into its
// scheme, ID and decoded segments. It accepts only the canonical form, so a
// value that parses is byte-identical to the one CreateVirtualPathSegments
// would make: two spellings of one media item cannot both be stored.
func ParseVirtualPathSegments(value string) (SegmentsResult, error) {
	if len(value) > maxPathLength {
		return SegmentsResult{}, ErrInvalidSegments
	}
	parsed := ParseURIComponents(value)
	if parsed.Scheme == "" || parsed.Query != "" || strings.Contains(value, "?") ||
		strings.Contains(value, "#") {
		return SegmentsResult{}, ErrInvalidSegments
	}
	rawID, rawRest, found := strings.Cut(parsed.Rest, "/")
	if !found || rawID == "" || rawRest == "" {
		return SegmentsResult{}, ErrInvalidSegments
	}
	id, err := url.PathUnescape(rawID)
	if err != nil {
		return SegmentsResult{}, ErrInvalidSegments
	}
	rawSegments := strings.Split(rawRest, "/")
	segments := make([]string, len(rawSegments))
	for i, raw := range rawSegments {
		segments[i], err = url.PathUnescape(raw)
		if err != nil {
			return SegmentsResult{}, ErrInvalidSegments
		}
	}
	canonical, err := CreateVirtualPathSegments(parsed.Scheme, id, segments)
	if err != nil || canonical != value {
		return SegmentsResult{}, ErrInvalidSegments
	}
	return SegmentsResult{Scheme: parsed.Scheme, ID: id, Segments: segments}, nil
}
