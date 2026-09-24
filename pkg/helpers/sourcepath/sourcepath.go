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

// Package sourcepath defines provider-independent, source-relative media identity.
package sourcepath

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"
	"unicode/utf8"
)

const Scheme = "source"

var ErrInvalid = errors.New("invalid source-relative media identity")

// ID is stable across permission loss and reinsertion of the same opaque source.
// A reference is not a filesystem path and must never be normalized as one.
func ID(reference string) string {
	sum := sha256.Sum256([]byte("zaparoo-host-source-v1\x00" + reference))
	return hex.EncodeToString(sum[:])
}

func ValidName(name string) bool {
	if name == "" || name == "." || name == ".." || len(name) > 1024 || !utf8.ValidString(name) {
		return false
	}
	for _, r := range name {
		if r < 32 || r == 127 || r == '/' || r == '\\' {
			return false
		}
	}
	return true
}

func Format(id string, parts []string) (string, error) {
	decoded, err := hex.DecodeString(id)
	if err != nil || len(decoded) != sha256.Size || id != strings.ToLower(id) || len(parts) == 0 || len(parts) > 64 {
		return "", ErrInvalid
	}
	escaped := make([]string, len(parts))
	for i, part := range parts {
		if !ValidName(part) {
			return "", ErrInvalid
		}
		escaped[i] = url.PathEscape(part)
	}
	value := Scheme + "://" + id + "/" + strings.Join(escaped, "/")
	if len(value) > 16384 {
		return "", ErrInvalid
	}
	return value, nil
}

func Parse(value string) (id string, parts []string, err error) {
	if len(value) > 16384 {
		return "", nil, ErrInvalid
	}
	u, err := url.Parse(value)
	if err != nil || u.Scheme != Scheme || u.User != nil || u.RawQuery != "" ||
		u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return "", nil, ErrInvalid
	}
	parts = strings.Split(strings.TrimPrefix(u.EscapedPath(), "/"), "/")
	for i, part := range parts {
		parts[i], err = url.PathUnescape(part)
		if err != nil {
			return "", nil, ErrInvalid
		}
	}
	canonical, err := Format(u.Host, parts)
	if err != nil || canonical != value {
		return "", nil, ErrInvalid
	}
	return u.Host, parts, nil
}
