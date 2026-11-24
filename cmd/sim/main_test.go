//go:build linux && !android

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

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseServe(t *testing.T) {
	t.Parallel()
	opts, err := parseServe(nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"sim0"}, opts.readers)
	assert.Zero(t, opts.port)

	opts, err = parseServe([]string{"--fakes", "romm, libretro", "--readers", "a,b", "--port", "17000", "--foreground"})
	require.NoError(t, err)
	assert.Equal(t, []string{"romm", "libretro"}, opts.fakes)
	assert.Equal(t, []string{"a", "b"}, opts.readers)
	assert.Equal(t, 17000, opts.port)
	assert.True(t, opts.foreground)

	for name, args := range map[string][]string{
		"real port":    {"--port", "7497"},
		"unknown fake": {"--fakes", "updates"},
		"two onlines":  {"--fakes", "online", "--online-url", "http://127.0.0.1:1"},
		"stray arg":    {"extra"},
		"bad flag":     {"--nope"},
	} {
		_, err := parseServe(args)
		require.Error(t, err, name)
	}
}

func TestExtractJSONFlag(t *testing.T) {
	t.Parallel()
	out, found := extractJSONFlag([]string{"tap", "--json", "x"})
	assert.True(t, found)
	assert.Equal(t, []string{"tap", "x"}, out)

	out, found = extractJSONFlag([]string{"api", "--", "--json"})
	assert.False(t, found, "arguments after -- are left alone")
	assert.Equal(t, []string{"api", "--", "--json"}, out)
}

func TestSplitList(t *testing.T) {
	t.Parallel()
	assert.Nil(t, splitList(""))
	assert.Equal(t, []string{"a", "b"}, splitList(" a,,b ,"))
}
