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

// Package sidecar parses zaparoo.json, the optional metadata file a drive or
// disc carries beside zaparoo.txt. Its contents are untrusted.
package sidecar

import (
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rs/zerolog/log"
)

const (
	// FileName is the optional metadata file beside zaparoo.txt.
	FileName = "zaparoo.json"
	// MaxBytes bounds the metadata file. It holds a few short strings.
	MaxBytes = 64 << 10
	// The text limits match a deck's name and description.
	MaxNameRunes        = 100
	MaxDescriptionRunes = 1000
	MaxImagePathBytes   = 255
)

var errSidecarNotObject = errors.New("zaparoo.json is not a JSON object")

// Fields is zaparoo.json after decoding, before the image is resolved
// against the drive. Unknown keys are ignored so the format can grow.
type Fields struct {
	Name        string
	Description string
	Image       string
}

// Parse decodes zaparoo.json. A field with the wrong type, too long
// or empty after cleaning is dropped on its own; only a file that is not a
// JSON object fails as a whole.
func Parse(data []byte) (Fields, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return Fields{}, fmt.Errorf("decode zaparoo.json: %w", err)
	}
	if raw == nil {
		return Fields{}, errSidecarNotObject
	}
	return Fields{
		Name:        sidecarText(raw, "name", MaxNameRunes, false),
		Description: sidecarText(raw, "description", MaxDescriptionRunes, true),
		Image:       sidecarImage(raw),
	}, nil
}

func sidecarString(raw map[string]json.RawMessage, key string) (string, bool) {
	value, present := raw[key]
	if !present {
		return "", false
	}
	var s string
	if err := json.Unmarshal(value, &s); err != nil {
		log.Warn().Str("field", key).Msg("ignoring zaparoo.json field: not a string")
		return "", false
	}
	return s, true
}

func sidecarText(raw map[string]json.RawMessage, key string, maxRunes int, multiline bool) string {
	s, ok := sidecarString(raw, key)
	if !ok {
		return ""
	}
	s = cleanSidecarText(s, multiline)
	if utf8.RuneCountInString(s) > maxRunes {
		log.Warn().Str("field", key).Int("max", maxRunes).Msg("ignoring zaparoo.json field: too long")
		return ""
	}
	return s
}

func sidecarImage(raw map[string]json.RawMessage) string {
	s, ok := sidecarString(raw, "image")
	if !ok {
		return ""
	}
	if len(s) > MaxImagePathBytes {
		log.Warn().Str("field", "image").Msg("ignoring zaparoo.json field: too long")
		return ""
	}
	return s
}

// cleanSidecarText drops control and bidirectional override characters,
// keeping line breaks only where multiline allows them, and trims the result.
func cleanSidecarText(s string, multiline bool) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n' && multiline:
			b.WriteRune(r)
		case r == '\t' || r == '\n' || r == '\r':
			if !multiline || r == '\t' {
				b.WriteRune(' ')
			}
		case unicode.IsControl(r), IsBidiControl(r):
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

// IsBidiControl reports whether r reorders displayed text.
func IsBidiControl(r rune) bool {
	return (r >= '\u202a' && r <= '\u202e') || (r >= '\u2066' && r <= '\u2069') ||
		r == '\u200e' || r == '\u200f' || r == '\u061c'
}

// ImageExtension reports whether name ends in a cover format
// media.active.image serves.
func ImageExtension(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".png", ".jpg", ".jpeg":
		return true
	default:
		return false
	}
}

// ImagePathParts splits the image field into the names to walk from the root
// of a disc. It rejects absolute paths, empty or dot components, parent
// references and backslashes, so the lookup can never leave the disc root.
func ImagePathParts(rel string) ([]string, bool) {
	if rel == "" || strings.HasPrefix(rel, "/") || strings.ContainsAny(rel, "\\\x00") {
		return nil, false
	}
	parts := strings.Split(rel, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return nil, false
		}
	}
	return parts, true
}
