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
	"unicode/utf8"

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

// The pre-check is what keeps redaction off the parser for ordinary tokens,
// so it has to recognise every spelling that can reach a credential.
func TestMayCarryCredential(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		want bool
	}{
		{name: "profile card", text: "**profile:" + testSwitchID, want: true},
		{name: "uppercase profile card", text: "**PROFILE:" + testSwitchID, want: true},
		{name: "mixed case profile card", text: "**PrOfIlE:" + testSwitchID, want: true},
		{name: "profile clear", text: "**profile.clear", want: true},
		{
			name: "extension card",
			text: "**playtime.extend:15m?profile=" + testSwitchID,
			want: true,
		},
		{
			name: "uppercase extension card",
			text: "**PLAYTIME.EXTEND:15m?PROFILE=" + testSwitchID,
			want: true,
		},
		{
			name: "credential in a chain",
			text: "**launch:/games/snes/mario.sfc||**profile:" + testSwitchID,
			want: true,
		},
		{name: "plain launch", text: "**launch:/games/snes/mario.sfc", want: false},
		{name: "media title", text: "@SNES/Super Mario World", want: false},
		{name: "plain text", text: "just some text", want: false},
		{name: "empty", text: "", want: false},
		{name: "already redacted script", text: redactedScript, want: false},
		{
			name: "playtime command that carries no credential",
			text: "**playtime.pause",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, mayCarryCredential(tt.text))
		})
	}
}

// Every command scriptCredentials can pull a credential out of must be named
// in credentialCommands, or the pre-check would skip the parse that would
// have found it.
func TestCredentialCommands_CoverScriptCredentials(t *testing.T) {
	t.Parallel()

	bearers := []string{
		"**profile:" + testSwitchID,
		"**playtime.extend:15m?profile=" + testSwitchID,
	}

	for _, text := range bearers {
		t.Run(text, func(t *testing.T) {
			t.Parallel()
			require.True(t, mayCarryCredential(text),
				"a script this carries a credential must not skip the parse")
			assert.NotContains(t, RedactScript(text), testSwitchID)
		})
	}
}

// Malformed text that cannot name a credential-bearing command is left
// readable. The parse is skipped, so nothing about it can be shown to be
// sensitive, and blanking it would only lose diagnostic value.
func TestRedactScript_KeepsMalformedTextWithoutCredentialCommand(t *testing.T) {
	t.Parallel()

	malformed := `**launch:"unterminated`

	assert.Equal(t, malformed, RedactScript(malformed))
	assert.False(t, HasSensitiveScript(malformed))
}

// A long unrelated argument must not stop a credential elsewhere in the same
// script from being removed.
func TestRedactToken_RemovesCredentialAlongsideLongText(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("A", 4000)
	text := "**launch:" + long + "||**profile:" + testSwitchID

	gotText, gotData := RedactToken(text, "deadbeef")

	assert.NotContains(t, gotText, testSwitchID)
	assert.Contains(t, gotText, long, "unrelated content should survive")
	assert.Empty(t, gotData, "the raw payload of a sensitive token is dropped")
}

// RedactToken keeps the payload of a token that carries no credential.
func TestRedactToken_KeepsDataForOrdinaryToken(t *testing.T) {
	t.Parallel()

	text := "**launch:/games/snes/mario.sfc"

	gotText, gotData := RedactToken(text, "deadbeef")

	assert.Equal(t, text, gotText)
	assert.Equal(t, "deadbeef", gotData)
}

// An advanced argument key reaches the command decoder case-insensitively, so
// `?PROFILE=` authorizes an extension exactly as `?profile=` does. Redaction
// has to fold case the same way, or a working extension card writes its
// credential to the log and to history in the clear.
func TestRedactScript_RemovesCredentialFromMixedCaseAdvArg(t *testing.T) {
	t.Parallel()

	spellings := []string{"profile", "PROFILE", "Profile", "pRoFiLe"}

	for _, key := range spellings {
		t.Run(key, func(t *testing.T) {
			t.Parallel()

			text := "**playtime.extend:15m?" + key + "=" + testSwitchID

			got := RedactScript(text)
			assert.NotContains(t, got, testSwitchID, "the credential must not survive")
			assert.Contains(t, got, "15m", "non-sensitive content should stay readable")

			gotText, gotData := RedactToken(text, "deadbeef")
			assert.NotContains(t, gotText, testSwitchID)
			assert.Empty(t, gotData, "the raw payload of a sensitive token is dropped")

			assert.True(t, HasSensitiveScript(text))
		})
	}
}

// A script can spell the key more than one way at once, and every value is a
// usable credential, so every one has to go.
func TestRedactScript_RemovesEveryCaseVariantOfTheProfileArg(t *testing.T) {
	t.Parallel()

	second := "sw-0000ffff"
	text := "**playtime.extend:15m?profile=" + testSwitchID + "&PROFILE=" + second

	got := RedactScript(text)

	assert.NotContains(t, got, testSwitchID)
	assert.NotContains(t, got, second)
}

// ForLog is what a reader driver calls on text nothing has bounded yet, so it
// has to both redact and cap. A script within the limit is untouched beyond
// redaction; a longer one is cut, and its real size reported.
func TestForLog(t *testing.T) {
	t.Parallel()

	t.Run("ordinary text is unchanged", func(t *testing.T) {
		t.Parallel()
		text := "**launch:/games/snes/mario.sfc"
		assert.Equal(t, text, ForLog(text))
	})

	t.Run("credential is redacted", func(t *testing.T) {
		t.Parallel()
		got := ForLog("**profile:" + testSwitchID)
		assert.NotContains(t, got, testSwitchID)
		assert.Contains(t, got, RedactedPlaceholder)
	})

	t.Run("text at the limit is not truncated", func(t *testing.T) {
		t.Parallel()
		text := "**echo:" + strings.Repeat("A", MaxScriptLength-len("**echo:"))
		require.Len(t, text, MaxScriptLength)
		assert.Equal(t, text, ForLog(text))
	})

	t.Run("longer text is cut and its size reported", func(t *testing.T) {
		t.Parallel()
		text := "**echo:" + strings.Repeat("A", 300000)
		got := ForLog(text)
		assert.Less(t, len(got), MaxScriptLength+64, "a log line must not carry the whole payload")
		assert.Contains(t, got, "(300007 bytes)")
	})

	t.Run("a cut never splits a rune", func(t *testing.T) {
		t.Parallel()
		// Three-byte runes do not divide evenly into the limit, so the cut
		// lands mid-rune unless it is moved back to a boundary.
		text := "**echo:" + strings.Repeat("あ", 4000)
		got := ForLog(text)
		assert.True(t, utf8.ValidString(got), "log text must stay valid UTF-8")
	})

	t.Run("a credential inside the kept portion still goes", func(t *testing.T) {
		t.Parallel()
		text := "**profile:" + testSwitchID + "||**echo:" + strings.Repeat("A", 300000)
		assert.NotContains(t, ForLog(text), testSwitchID)
	})
}
