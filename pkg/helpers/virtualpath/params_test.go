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
	"net/url"
	"testing"
	"unicode/utf8"

	gozapscript "github.com/ZaparooProject/go-zapscript"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateVirtualPathWithParams(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "kodi-movie://12/Inception%20%282010%29;tmdb=27205;imdb=tt1375666",
		CreateVirtualPathWithParams("kodi-movie", "12", "Inception (2010)",
			[]string{"tmdb=27205", "imdb=tt1375666"}))
	assert.Equal(t, "kodi-movie://12/Inception",
		CreateVirtualPathWithParams("kodi-movie", "12", "Inception", nil))
	assert.Equal(t, "kodi-movie://12/Up%3B%20Down;tmdb=1",
		CreateVirtualPathWithParams("kodi-movie", "12", "Up; Down", []string{"tmdb=1"}),
		"a ';' in the name must be escaped so it cannot start the parameters")
}

func TestParseParams(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want url.Values
		name string
		path string
	}{
		{
			name: "semicolon form",
			path: "plex-movie://1/Inception%20%282010%29;tmdb=27205;imdb=tt1375666",
			want: url.Values{"tmdb": {"27205"}, "imdb": {"tt1375666"}},
		},
		{
			name: "legacy query form",
			path: "kodi-show://1/Breaking%20Bad?tvdb=81189&imdb=tt0903747",
			want: url.Values{"tvdb": {"81189"}, "imdb": {"tt0903747"}},
		},
		{
			name: "both forms are merged",
			path: "kodi-show://1/Show;tvdb=1?tmdb=2",
			want: url.Values{"tvdb": {"1"}, "tmdb": {"2"}},
		},
		{
			name: "escaped values",
			path: "jellyfin-song://a/Song;mb_track=a%3Bb%26c",
			want: url.Values{"mb_track": {"a;b&c"}},
		},
		{
			name: "no parameters",
			path: "kodi-movie://12/Inception",
			want: url.Values{},
		},
		{
			name: "bare semicolon in a hand-written name is not a parameter",
			path: "kodi-movie://12/Up;Down",
			want: url.Values{},
		},
		{
			name: "semicolon in the ID is not a parameter",
			path: "kodi-movie://12;tmdb=1",
			want: url.Values{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseParams(tt.path)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}

	_, err := ParseParams("kodi-movie://12/X;tmdb=%zz")
	require.Error(t, err)
}

func TestParseVirtualPathStrDropsParams(t *testing.T) {
	t.Parallel()

	got, err := ParseVirtualPathStr("plex-movie://1/Inception%20%282010%29;tmdb=27205;imdb=tt1375666")
	require.NoError(t, err)
	assert.Equal(t, VirtualPathResult{Scheme: "plex-movie", ID: "1", Name: "Inception (2010)"}, got)

	got, err = ParseVirtualPathStr("kodi-movie://12/Up;Down")
	require.NoError(t, err)
	assert.Equal(t, "Up;Down", got.Name)

	got, err = ParseVirtualPathStr("kodi-movie://12/Up%3B%20Down;tmdb=1")
	require.NoError(t, err)
	assert.Equal(t, "Up; Down", got.Name)
}

// TestParamsPathIsPlainZapScript pins the reason ';' was chosen: a path with
// parameters reaches the launcher whole when written on a card as it is.
func TestParamsPathIsPlainZapScript(t *testing.T) {
	t.Parallel()

	path := CreateVirtualPathWithParams("kodi-episode", "40", "Lost - Pilot, Part 1",
		[]string{"tvdb=127131", "season=1", "episode=1", "show_imdb=tt0411008"})
	script, err := gozapscript.NewParser(path).ParseScript()
	require.NoError(t, err)
	require.Len(t, script.Cmds, 1)
	assert.Equal(t, gozapscript.ZapScriptCmdLaunch, script.Cmds[0].Name)
	assert.Equal(t, []string{path}, script.Cmds[0].Args)
	assert.Empty(t, script.Cmds[0].AdvArgs.Raw())
}

// FuzzVirtualPathParamsRoundTrip checks that any name and parameter value
// survive a path built with CreateVirtualPathWithParams, both directly and
// after a ZapScript parse.
func FuzzVirtualPathParamsRoundTrip(f *testing.F) {
	f.Add("12", "Inception (2010)", "27205")
	f.Add("a/b", "Up; Down", "x;y&z=w")
	f.Add("", "", "")
	f.Add("1", "?launcher=x", "#t")
	f.Add("1", "**launch.random:x", "a||b")
	f.Fuzz(func(t *testing.T, id, name, value string) {
		for _, s := range []string{id, name, value} {
			if ContainsControlChar(s) || !utf8.ValidString(s) {
				t.Skip()
			}
		}
		path := CreateVirtualPathWithParams("kodi-movie", id, name, []string{"tmdb=" + url.QueryEscape(value)})

		parsed, err := ParseVirtualPathStr(path)
		require.NoError(t, err)
		assert.Equal(t, id, parsed.ID)
		assert.Equal(t, name, parsed.Name)

		params, err := ParseParams(path)
		require.NoError(t, err)
		assert.Equal(t, []string{value}, params["tmdb"])

		script, err := gozapscript.NewParser(path).ParseScript()
		require.NoError(t, err)
		require.Len(t, script.Cmds, 1)
		assert.Equal(t, []string{path}, script.Cmds[0].Args)
	})
}
