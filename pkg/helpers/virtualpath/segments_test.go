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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const segmentsTestID = "e765ffdf016aaf1d62ffff2bce443bc1271fb3e24a172ed6958d3d015b27a096"

// The encoding is part of stored media identity, so these values are fixed:
// changing how a segment escapes would orphan every row indexed before.
func TestCreateVirtualPathSegmentsIsStable(t *testing.T) {
	t.Parallel()
	prefix := "source://" + segmentsTestID + "/"
	tests := []struct {
		want     string
		segments []string
	}{
		{
			segments: []string{"NES", "Super Mario Bros. (World).nes"},
			want:     prefix + "NES/Super%20Mario%20Bros.%20%28World%29.nes",
		},
		{
			segments: []string{"Master System", "Sub Dir", "Game #3 & Friends? [!].sms"},
			want:     prefix + "Master%20System/Sub%20Dir/Game%20%233%20&%20Friends%3F%20%5B%21%5D.sms",
		},
		{
			segments: []string{"PSX", "100% Orange Juice.zip"},
			want:     prefix + "PSX/100%25%20Orange%20Juice.zip",
		},
		{
			segments: []string{"SNES", "Pokémon Café.sfc"},
			want:     prefix + "SNES/Pok%C3%A9mon%20Caf%C3%A9.sfc",
		},
		{
			segments: []string{"a+b=c;d,e:f@g$h'i!j*k~l_m-n.o"},
			want:     prefix + "a+b=c%3Bd%2Ce:f@g$h%27i%21j%2Ak~l_m-n.o",
		},
	}
	for _, tt := range tests {
		got, err := CreateVirtualPathSegments("source", segmentsTestID, tt.segments)
		require.NoError(t, err)
		assert.Equal(t, tt.want, got)

		parsed, err := ParseVirtualPathSegments(got)
		require.NoError(t, err)
		assert.Equal(t, "source", parsed.Scheme)
		assert.Equal(t, segmentsTestID, parsed.ID)
		assert.Equal(t, tt.segments, parsed.Segments)
	}
}

func TestCreateVirtualPathSegmentsRejectsUnusableInput(t *testing.T) {
	t.Parallel()
	for name, segments := range map[string][]string{
		"none":            nil,
		"empty segment":   {"NES", ""},
		"dot":             {"."},
		"dot dot":         {"NES", ".."},
		"slash":           {"a/b"},
		"backslash":       {`a\b`},
		"control":         {"a\x00b"},
		"delete":          {"a\x7fb"},
		"invalid utf-8":   {"a\xffb"},
		"segment too big": {strings.Repeat("x", maxSegmentLength+1)},
		"too many":        make([]string, maxSegments+1),
	} {
		_, err := CreateVirtualPathSegments("source", "id", segments)
		require.ErrorIs(t, err, ErrInvalidSegments, name)
	}
	_, err := CreateVirtualPathSegments("not a scheme", "id", []string{"a"})
	require.ErrorIs(t, err, ErrInvalidSegments)
	_, err = CreateVirtualPathSegments("source", "", []string{"a"})
	require.ErrorIs(t, err, ErrInvalidSegments)
}

// Only the canonical spelling parses, so one media item cannot be stored
// under two paths.
func TestParseVirtualPathSegmentsRejectsNonCanonical(t *testing.T) {
	t.Parallel()
	prefix := "source://" + segmentsTestID + "/"
	for _, value := range []string{
		prefix + "NES/Super Mario.nes",   // unescaped space
		prefix + "NES/Super%2fMario.nes", // escaped separator
		prefix + "NES/caf%c3%a9.nes",     // lowercase escape
		prefix + "NES//game.nes",         // empty segment
		prefix + "NES/game.nes/",         // trailing slash
		prefix + "NES/game.nes?x=1",      // query
		prefix + "NES/game.nes#frag",     // fragment
		prefix,                           // no segments
		"source://" + segmentsTestID,     // root only
		"/roms/NES/game.nes",             // not a virtual path
		prefix + "NES/%zz.nes",           // broken escape
	} {
		_, err := ParseVirtualPathSegments(value)
		require.ErrorIs(t, err, ErrInvalidSegments, value)
	}
}

func TestParseVirtualPathSegmentsBoundsLength(t *testing.T) {
	t.Parallel()
	segment := strings.Repeat("x", maxSegmentLength)
	segments := make([]string, maxSegments)
	for i := range segments {
		segments[i] = segment
	}
	_, err := CreateVirtualPathSegments("source", "id", segments)
	require.ErrorIs(t, err, ErrInvalidSegments)
	_, err = ParseVirtualPathSegments("source://id/" + strings.Repeat("x", maxPathLength))
	require.ErrorIs(t, err, ErrInvalidSegments)
}
