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

package database

import (
	"errors"
	"net/url"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ScrapeScope is a resolved selection, persisted unchanged across restarts.
// Single items pin all three identity fields so a reused DBID cannot select a
// different path after an index rebuild. A nil scope retains legacy selection.
type ScrapeScope struct {
	SystemID string `json:"system"`
	Path     string `json:"path"`
	MediaID  int64  `json:"mediaId,omitempty"`
	Subtree  bool   `json:"subtree,omitempty"`
}

// CanonicalScrapePath accepts indexed URI identities for files, and absolute
// native filesystem paths otherwise. It never accesses or resolves symlinks.
func CanonicalScrapePath(value string, subtree bool) (string, error) {
	if value == "" || len(value) > 4096 || !utf8.ValidString(value) || strings.ContainsFunc(value, unicode.IsControl) {
		return "", errors.New("path must be nonempty UTF-8 without control characters and at most 4096 bytes")
	}
	if !filepath.IsAbs(value) {
		uri, err := url.Parse(value)
		if err == nil && uri.Scheme != "" && strings.Index(value, "://") == len(uri.Scheme) {
			if subtree {
				return "", errors.New("subtree paths cannot be virtual URIs")
			}
			return value, nil
		}
		return "", errors.New("path must be absolute; use the canonical indexed path")
	}
	for _, component := range strings.Split(filepath.ToSlash(value), "/") {
		if component == ".." {
			return "", errors.New("path cannot contain parent traversal")
		}
	}
	return filepath.ToSlash(filepath.Clean(value)), nil
}

// Validate fails closed for malformed persisted selectors rather than treating
// an empty or unrecognized selection as a whole-system request.
func (s ScrapeScope) Validate() error {
	if s.SystemID == "" || strings.TrimSpace(s.SystemID) != s.SystemID {
		return errors.New("scope requires a system")
	}
	if (s.Subtree && s.MediaID != 0) || (!s.Subtree && s.MediaID <= 0) {
		return errors.New("scope requires either a subtree or a positive media ID")
	}
	path, err := CanonicalScrapePath(s.Path, s.Subtree)
	if err != nil {
		return err
	}
	if path != s.Path {
		return errors.New("scope path must be canonical")
	}
	return nil
}
