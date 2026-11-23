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

package externaldrive

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/imagefiles"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/pathutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/rs/zerolog/log"
)

const (
	// sidecarFileName is the optional metadata file beside zaparoo.txt.
	sidecarFileName = "zaparoo.json"
	// maxSidecarBytes bounds the metadata file. It holds a few short strings.
	maxSidecarBytes = 64 << 10
	// The text limits match a deck's name and description.
	maxSidecarNameRunes        = 100
	maxSidecarDescriptionRunes = 1000
	maxSidecarImagePathBytes   = 255
)

var errSidecarNotObject = errors.New("zaparoo.json is not a JSON object")

// sidecarFields is zaparoo.json after decoding, before the image is resolved
// against the drive. Unknown keys are ignored so the format can grow.
type sidecarFields struct {
	Name        string
	Description string
	Image       string
}

// parseSidecar decodes zaparoo.json. A field with the wrong type, too long
// or empty after cleaning is dropped on its own; only a file that is not a
// JSON object fails as a whole.
func parseSidecar(data []byte) (sidecarFields, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return sidecarFields{}, fmt.Errorf("decode zaparoo.json: %w", err)
	}
	if raw == nil {
		return sidecarFields{}, errSidecarNotObject
	}
	return sidecarFields{
		Name:        sidecarText(raw, "name", maxSidecarNameRunes, false),
		Description: sidecarText(raw, "description", maxSidecarDescriptionRunes, true),
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
	if len(s) > maxSidecarImagePathBytes {
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
		case unicode.IsControl(r), isBidiControl(r):
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

func isBidiControl(r rune) bool {
	return (r >= '\u202a' && r <= '\u202e') || (r >= '\u2066' && r <= '\u2069') ||
		r == '\u200e' || r == '\u200f' || r == '\u061c'
}

// sidecarImageExtension reports whether name ends in a cover format
// media.active.image serves.
func sidecarImageExtension(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png", ".jpg", ".jpeg":
		return true
	default:
		return false
	}
}

// resolveSidecarImage turns the image field into a path on the drive, or ""
// when it does not name a PNG or JPEG regular file under mount.
func resolveSidecarImage(mount, rel string) string {
	if rel == "" {
		return ""
	}
	if !sidecarImageExtension(rel) {
		log.Warn().Str("image", rel).Msg("ignoring zaparoo.json image: not a .png, .jpg or .jpeg file")
		return ""
	}
	path, info, err := pathutil.LocalFileUnder(mount, rel)
	if err != nil {
		log.Warn().Err(err).Str("image", rel).Msg("ignoring zaparoo.json image: not a file on the drive")
		return ""
	}
	if info.Size() > imagefiles.MaxBytes {
		log.Warn().Str("image", rel).Int64("size", info.Size()).Msg("ignoring zaparoo.json image: too large")
		return ""
	}
	return path
}

// loadSidecar reads zaparoo.json from the root of mount. It returns nil when
// there is no usable file or nothing in it survives validation. Any problem
// is logged and never stops the token from being scanned.
func loadSidecar(mount string) *tokens.Metadata {
	name := findFileInDir(mount, sidecarFileName)
	if name == "" {
		return nil
	}
	path, info, err := pathutil.LocalFileUnder(mount, filepath.Base(name))
	if err != nil {
		log.Warn().Err(err).Str("path", name).Msg("ignoring zaparoo.json: not a regular file")
		return nil
	}
	if info.Size() > maxSidecarBytes {
		log.Warn().Str("path", path).Int64("size", info.Size()).Msg("ignoring zaparoo.json: exceeds maximum size")
		return nil
	}
	data, err := readSidecarFile(path)
	if err != nil {
		log.Warn().Err(err).Str("path", path).Msg("ignoring zaparoo.json: read failed")
		return nil
	}
	fields, err := parseSidecar(data)
	if err != nil {
		log.Warn().Err(err).Str("path", path).Msg("ignoring zaparoo.json")
		return nil
	}
	meta := &tokens.Metadata{
		Name:        fields.Name,
		Description: fields.Description,
		ImagePath:   resolveSidecarImage(mount, fields.Image),
	}
	if *meta == (tokens.Metadata{}) {
		return nil
	}
	return meta
}

func readSidecarFile(path string) ([]byte, error) {
	//nolint:gosec // path was checked to be a regular file under the mount
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxSidecarBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	if len(data) > maxSidecarBytes {
		return nil, errors.New("exceeds maximum size")
	}
	return data, nil
}
