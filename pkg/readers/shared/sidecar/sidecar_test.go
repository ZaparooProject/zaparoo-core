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

package sidecar

import (
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSidecar(t *testing.T) {
	t.Parallel()
	fields, err := Parse([]byte(`{
		"name": "  Tetris  ",
		"description": "Line one\r\nLine two\tend",
		"image": "art/cover.png",
		"future": {"nested": true}
	}`))
	require.NoError(t, err)
	assert.Equal(t, "Tetris", fields.Name)
	assert.Equal(t, "Line one\nLine two end", fields.Description)
	assert.Equal(t, "art/cover.png", fields.Image)
}

func TestParseSidecarDropsBadFieldsOnly(t *testing.T) {
	t.Parallel()
	fields, err := Parse([]byte(`{
		"name": 42,
		"description": "` + strings.Repeat("d", MaxDescriptionRunes+1) + `",
		"image": "` + strings.Repeat("i", MaxImagePathBytes+1) + `"
	}`))
	require.NoError(t, err)
	assert.Equal(t, Fields{}, fields)

	fields, err = Parse([]byte(`{"name": "` + strings.Repeat("n", MaxNameRunes) + `", "image": null}`))
	require.NoError(t, err)
	assert.Len(t, fields.Name, MaxNameRunes, "a name at the limit is kept")
	assert.Empty(t, fields.Image)
}

func TestParseSidecarStripsControlCharacters(t *testing.T) {
	t.Parallel()
	fields, err := Parse([]byte(`{"name": "Te\u0000tr\u001bis\u202e\nGame", "description": "a\u0007b\u2066c"}`))
	require.NoError(t, err)
	assert.Equal(t, "Tetris Game", fields.Name, "a name stays on one line")
	assert.Equal(t, "abc", fields.Description)
}

func TestParseSidecarRejectsNonObjects(t *testing.T) {
	t.Parallel()
	for _, data := range []string{``, `not json`, `[]`, `"name"`, `null`, `{"name": "x"`} {
		_, err := Parse([]byte(data))
		assert.Error(t, err, "input %q", data)
	}
}

func FuzzParseSidecar(f *testing.F) {
	f.Add([]byte(`{"name":"Tetris","description":"Falling blocks.","image":"cover.png"}`))
	f.Add([]byte(`{"name":42,"image":null,"extra":[1,2,3]}`))
	f.Add([]byte(`{"name":"\u202eevil\u0000","description":"a\r\nb"}`))
	f.Add([]byte(`[]`))
	f.Add([]byte(`{"name":`))
	f.Fuzz(func(t *testing.T, data []byte) {
		fields, err := Parse(data)
		if err != nil {
			assert.Equal(t, Fields{}, fields)
			return
		}
		assert.LessOrEqual(t, utf8.RuneCountInString(fields.Name), MaxNameRunes)
		assert.LessOrEqual(t, utf8.RuneCountInString(fields.Description), MaxDescriptionRunes)
		assert.LessOrEqual(t, len(fields.Image), MaxImagePathBytes)
		for _, r := range fields.Name {
			assert.False(t, unicode.IsControl(r) || IsBidiControl(r), "name keeps %U", r)
		}
		for _, r := range fields.Description {
			assert.False(t, (unicode.IsControl(r) && r != '\n') || IsBidiControl(r), "description keeps %U", r)
		}
		assert.Equal(t, strings.TrimSpace(fields.Name), fields.Name)
		assert.Equal(t, strings.TrimSpace(fields.Description), fields.Description)
	})
}

func TestImagePathParts(t *testing.T) {
	t.Parallel()
	parts, ok := ImagePathParts("art/cover.png")
	require.True(t, ok)
	assert.Equal(t, []string{"art", "cover.png"}, parts)

	for _, bad := range []string{
		"", "/cover.png", "../cover.png", "art/../x.png", "a//b.png",
		"./cover.png", "art\\cover.png", "art/", "a\x00.png",
	} {
		_, ok := ImagePathParts(bad)
		assert.False(t, ok, "%q", bad)
	}
}
