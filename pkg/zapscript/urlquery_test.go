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

package zapscript

import (
	"testing"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFoldURLQuery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		wantArgs map[string]string
		name     string
		script   string
		wantPath string
	}{
		{
			name:     "stream query kept",
			script:   "https://stream.radiofrance.fr/fip/fip.m3u8?id=radiofrance",
			wantPath: "https://stream.radiofrance.fr/fip/fip.m3u8?id=radiofrance",
			wantArgs: map[string]string{},
		},
		{
			name:     "launch args stay launch args",
			script:   "https://example.com/live.m3u8?sid=1&slot=background",
			wantPath: "https://example.com/live.m3u8?sid=1",
			wantArgs: map[string]string{"slot": "background"},
		},
		{
			name:     "only launch args",
			script:   "https://example.com/a.mp3?slot=background",
			wantPath: "https://example.com/a.mp3",
			wantArgs: map[string]string{"slot": "background"},
		},
		{
			name:     "file path untouched",
			script:   "**launch:/music/a.mp3?slot=background",
			wantPath: "/music/a.mp3",
			wantArgs: map[string]string{"slot": "background"},
		},
		{
			name:     "explicit launch of a url",
			script:   "**launch:https://example.com/listen?type=http&when=1",
			wantPath: "https://example.com/listen?type=http",
			wantArgs: map[string]string{"when": "1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			script, err := zapscript.NewParser(tt.script).ParseScript()
			require.NoError(t, err)
			require.Len(t, script.Cmds, 1)
			cmd := foldURLQuery(script.Cmds[0])
			assert.Equal(t, tt.wantPath, cmd.Args[0])
			assert.Equal(t, tt.wantArgs, cmd.AdvArgs.Raw())
		})
	}
}

// Other commands keep their arguments as written.
func TestFoldURLQuery_OnlyLaunch(t *testing.T) {
	t.Parallel()

	script, err := zapscript.NewParser("**http.get:https://example.com/hook?id=1").ParseScript()
	require.NoError(t, err)
	cmd := foldURLQuery(script.Cmds[0])
	assert.Equal(t, script.Cmds[0].Args, cmd.Args)
	assert.Equal(t, script.Cmds[0].AdvArgs.Raw(), cmd.AdvArgs.Raw())
}

func TestLocationZapScript(t *testing.T) {
	t.Parallel()

	tests := []struct {
		location string
		want     string
	}{
		{
			location: "https://ice1.somafm.com/groovesalad-128-mp3",
			want:     "https://ice1.somafm.com/groovesalad-128-mp3",
		},
		{
			location: "https://stream.radiofrance.fr/fip/fip.m3u8?id=radiofrance",
			want:     "https://stream.radiofrance.fr/fip/fip.m3u8?id=radiofrance",
		},
		{
			location: "https://example.com/live?system=1",
			want:     literalLaunch("https://example.com/live?system=1"),
		},
		{
			location: "http://example.com:8000/;stream.mp3||x",
			want:     literalLaunch("http://example.com:8000/;stream.mp3||x"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.location, func(t *testing.T) {
			t.Parallel()
			got := LocationZapScript(tt.location)
			assert.Equal(t, tt.want, got)
			script, err := zapscript.NewParser(got).ParseScript()
			require.NoError(t, err)
			require.Len(t, script.Cmds, 1)
			cmd := foldURLQuery(script.Cmds[0])
			assert.Equal(t, []string{tt.location}, cmd.Args)
			assert.True(t, cmd.AdvArgs.IsEmpty())
		})
	}
}
