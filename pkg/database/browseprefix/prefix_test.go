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

package browseprefix

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseStem(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		stem string
		kind Kind
		rest string
	}{
		{name: "rank dash", stem: "001 - Sonic", kind: KindRank, rest: "Sonic"},
		{name: "rank period", stem: "42. Contra", kind: KindRank, rest: "Contra"},
		{name: "rank paren", stem: "7) Zelda", kind: KindRank, rest: "Zelda"},
		{name: "date full", stem: "1991-06-23 - Sonic", kind: KindDate, rest: "Sonic"},
		{name: "date dotted", stem: "1991.06.23 Sonic", kind: KindDate, rest: "Sonic"},
		{name: "date year", stem: "1991 - Sonic", kind: KindDate, rest: "Sonic"},
		{name: "bare numeric title", stem: "1942", kind: KindNone, rest: ""},
		{name: "number plus letter title", stem: "3D Worldrunner", kind: KindNone, rest: ""},
		{name: "invalid date", stem: "1991-13-01 - Sonic", kind: KindNone, rest: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ParseStem(tt.stem)
			assert.Equal(t, tt.kind, got.Kind)
			assert.Equal(t, tt.rest, got.Rest)
		})
	}
}

func TestStemFromPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		stem string
	}{
		{
			name: "filename with extension",
			path: filepath.Join("roms", "genesis", "001 - Sonic.gen"),
			stem: "001 - Sonic",
		},
		{
			name: "nested date file",
			path: filepath.Join("roms", "genesis", "history", "1991-06-23 - Sonic.zip"),
			stem: "1991-06-23 - Sonic",
		},
		{name: "no extension", path: filepath.Join("roms", "genesis", "Sonic"), stem: "Sonic"},
		{
			name: "dot in title",
			path: filepath.Join("roms", "genesis", "Sonic. The Hedgehog.gen"),
			stem: "Sonic. The Hedgehog",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.stem, StemFromPath(tt.path))
		})
	}
}

func TestStripWithPolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		path    string
		rest    string
		parsed  Kind
		policy  Policy
		strips  bool
		enabled bool
	}{
		{
			name:    "enabled rank policy strips rank prefix",
			path:    filepath.Join("roms", "genesis", "001 - Sonic.gen"),
			policy:  Policy{Kind: KindRank, Enabled: true},
			rest:    "Sonic",
			strips:  true,
			parsed:  KindRank,
			enabled: true,
		},
		{
			name:    "disabled policy keeps stem",
			path:    filepath.Join("roms", "genesis", "001 - Sonic.gen"),
			policy:  Policy{Kind: KindRank},
			rest:    "001 - Sonic",
			parsed:  KindRank,
			enabled: false,
		},
		{
			name:    "kind mismatch keeps stem",
			path:    filepath.Join("roms", "genesis", "Sonic.gen"),
			policy:  Policy{Kind: KindDate, Enabled: true},
			rest:    "Sonic",
			parsed:  KindNone,
			enabled: true,
		},
		{
			name:    "empty rest keeps stem",
			path:    filepath.Join("roms", "genesis", "001.gen"),
			policy:  Policy{Kind: KindRank, Enabled: true},
			rest:    "001",
			parsed:  KindNone,
			enabled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			stem := StemFromPath(tt.path)
			prefix := ParseStem(stem)
			got, ok := StripWithPolicy(stem, tt.policy)

			assert.Equal(t, tt.parsed, prefix.Kind)
			assert.Equal(t, tt.rest, got)
			assert.Equal(t, tt.strips, ok)
			assert.Equal(t, tt.enabled, tt.policy.Enabled)
		})
	}
}

func TestDetectPolicyForPathsUsesStems(t *testing.T) {
	t.Parallel()

	paths := []string{
		filepath.Join("roms", "nes", "1942.nes"),
		filepath.Join("roms", "nes", "007.nes"),
		filepath.Join("roms", "nes", "3D Worldrunner.nes"),
		filepath.Join("roms", "nes", "720 Degrees.nes"),
		filepath.Join("roms", "nes", "8 Eyes.nes"),
	}

	policy := DetectPolicyForPaths(paths, DefaultThreshold, DefaultMinFiles)
	assert.False(t, policy.Enabled)
}
