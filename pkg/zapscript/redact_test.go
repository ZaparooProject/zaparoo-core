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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testSwitchID = "sw-7f3a9c21"

func TestRedactScript_RemovesCredentials(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		// keep lists substrings that must survive, so redaction stays
		// useful for diagnosis rather than blanking everything.
		keep []string
	}{
		{
			name:  "profile card",
			input: "**profile:" + testSwitchID,
			keep:  []string{"profile"},
		},
		{
			name:  "extension card keeps its amount",
			input: "**playtime.extend:15m?profile=" + testSwitchID,
			keep:  []string{"playtime.extend", "15m"},
		},
		{
			name:  "today extension",
			input: "**playtime.extend:today?profile=" + testSwitchID,
			keep:  []string{"playtime.extend", "today"},
		},
		{
			name:  "credential removed from a multi-command script",
			input: "**profile:" + testSwitchID + "||**launch:/games/snes/mario.sfc",
			keep:  []string{"launch", "/games/snes/mario.sfc"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := RedactScript(tt.input)

			assert.NotContains(t, got, testSwitchID, "the credential must not survive")
			assert.Contains(t, got, RedactedPlaceholder)
			for _, keep := range tt.keep {
				assert.Contains(t, got, keep, "non-sensitive content should stay readable")
			}
		})
	}
}

func TestRedactScript_LeavesOrdinaryScriptsAlone(t *testing.T) {
	t.Parallel()

	inputs := []string{
		"**launch:/games/snes/mario.sfc",
		"**launch.random:snes",
		"**playlist.play||**delay:500",
		"/games/snes/mario.sfc",
		"",
	}

	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, input, RedactScript(input),
				"a script with no credential should be returned untouched")
		})
	}
}

// Text that cannot be parsed cannot be shown to be free of credentials, so
// it is dropped rather than passed through.
func TestRedactScript_FailsClosedOnUnparseableText(t *testing.T) {
	t.Parallel()

	malformed := `**profile:"` + testSwitchID

	got := RedactScript(malformed)
	assert.NotContains(t, got, testSwitchID)
	assert.Equal(t, redactedScript, got)
}

// A quoted credential does not appear literally in the source, so the
// substitution can miss it. The verification pass has to catch that.
func TestRedactScript_HandlesQuotedCredential(t *testing.T) {
	t.Parallel()

	quoted := `**profile:"sw with spaces"`

	got := RedactScript(quoted)
	assert.NotContains(t, got, "sw with spaces")
}

func TestHasSensitiveScript(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "profile card", input: "**profile:" + testSwitchID, want: true},
		{
			name:  "extension card",
			input: "**playtime.extend:15m?profile=" + testSwitchID,
			want:  true,
		},
		{name: "launch", input: "**launch:/games/snes/mario.sfc", want: false},
		{name: "plain text", input: "just some text", want: false},
		{name: "empty", input: "", want: false},
		{name: "malformed fails closed", input: `**profile:"unterminated`, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, HasSensitiveScript(tt.input))
		})
	}
}

// The raw data payload is an unparsed copy of the same content, so it cannot
// be redacted in place and has to be dropped entirely.
func TestRedactToken_DropsDataForSensitiveTokens(t *testing.T) {
	t.Parallel()

	text, data := RedactToken("**profile:"+testSwitchID, "raw-ndef-bytes")
	assert.NotContains(t, text, testSwitchID)
	assert.Empty(t, data)

	text, data = RedactToken("**launch:/games/snes/mario.sfc", "raw-ndef-bytes")
	assert.Equal(t, "**launch:/games/snes/mario.sfc", text)
	assert.Equal(t, "raw-ndef-bytes", data, "an ordinary token keeps its payload")
}

// The redacted form must stay valid ZapScript so anything that re-parses
// stored history does not start failing.
func TestRedactScript_OutputStaysParseable(t *testing.T) {
	t.Parallel()

	inputs := []string{
		"**profile:" + testSwitchID,
		"**playtime.extend:15m?profile=" + testSwitchID,
		"**profile:" + testSwitchID + "||**launch:/games/snes/mario.sfc",
	}

	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			t.Parallel()

			redacted := RedactScript(input)
			require.NotEmpty(t, strings.TrimSpace(redacted))

			// Re-running redaction must be stable, since history is redacted
			// both when written and when served.
			assert.Equal(t, redacted, RedactScript(redacted),
				"redaction should be idempotent")
		})
	}
}
