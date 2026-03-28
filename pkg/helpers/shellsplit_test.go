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

package helpers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSplitCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		wantErr error
		name    string
		input   string
		want    []string
	}{
		{
			name:  "simple command",
			input: "echo hello",
			want:  []string{"echo", "hello"},
		},
		{
			name:  "single argument",
			input: "reboot",
			want:  []string{"reboot"},
		},
		{
			name:  "empty string",
			input: "",
			want:  nil,
		},
		{
			name:  "only whitespace",
			input: "   \t  ",
			want:  nil,
		},
		{
			name:  "multiple spaces between args",
			input: "echo   hello   world",
			want:  []string{"echo", "hello", "world"},
		},
		{
			name:  "tabs as separators",
			input: "echo\thello\tworld",
			want:  []string{"echo", "hello", "world"},
		},
		{
			name:  "double quoted string",
			input: `echo "hello world"`,
			want:  []string{"echo", "hello world"},
		},
		{
			name:  "single quoted string",
			input: `echo 'hello world'`,
			want:  []string{"echo", "hello world"},
		},
		{
			name:  "mixed quotes",
			input: `echo "hello" 'world'`,
			want:  []string{"echo", "hello", "world"},
		},
		{
			name:  "backslash space outside quotes is not escape",
			input: `echo hello\ world`,
			want:  []string{"echo", `hello\`, "world"},
		},
		{
			name:  "backslash in double quotes is literal",
			input: `echo "hello\\world"`,
			want:  []string{"echo", `hello\\world`},
		},
		{
			name:  "backslash in single quotes is literal",
			input: `echo 'hello \"world\"'`,
			want:  []string{"echo", `hello \"world\"`},
		},
		{
			name:  "empty double quoted string",
			input: `echo ""`,
			want:  []string{"echo", ""},
		},
		{
			name:  "empty single quoted string",
			input: `echo ''`,
			want:  []string{"echo", ""},
		},
		{
			name:  "path with spaces in double quotes",
			input: `retroarch --fullscreen "/path/to/my game.sfc"`,
			want:  []string{"retroarch", "--fullscreen", "/path/to/my game.sfc"},
		},
		{
			name:  "path with special characters",
			input: `retroarch "/path/to/game$(evil).sfc"`,
			want:  []string{"retroarch", "/path/to/game$(evil).sfc"},
		},
		{
			name:  "adjacent quoted sections",
			input: `echo "hello"'world'`,
			want:  []string{"echo", "helloworld"},
		},
		{
			name:  "leading whitespace",
			input: "  echo hello",
			want:  []string{"echo", "hello"},
		},
		{
			name:  "trailing whitespace",
			input: "echo hello  ",
			want:  []string{"echo", "hello"},
		},
		{
			name:    "unclosed double quote",
			input:   `echo "hello`,
			wantErr: ErrUnclosedQuote,
		},
		{
			name:    "unclosed single quote",
			input:   `echo 'hello`,
			wantErr: ErrUnclosedQuote,
		},
		{
			name:  "backslash at end of input",
			input: `echo hello\`,
			want:  []string{"echo", `hello\`},
		},
		{
			name: "realistic custom launcher command",
			input: `/usr/bin/retroarch -L /usr/lib/cores/snes9x_libretro.so` +
				` "/home/user/roms/Super Mario World (USA).sfc"`,
			want: []string{
				"/usr/bin/retroarch", "-L",
				"/usr/lib/cores/snes9x_libretro.so",
				"/home/user/roms/Super Mario World (USA).sfc",
			},
		},
		{
			name:  "windows path in quotes",
			input: `"C:\Program Files\RetroArch\retroarch.exe" --fullscreen "C:\Games\game.sfc"`,
			want:  []string{`C:\Program Files\RetroArch\retroarch.exe`, "--fullscreen", `C:\Games\game.sfc`},
		},
		{
			name:  "unicode in arguments",
			input: "echo \"\u3053\u3093\u306b\u3061\u306f\u4e16\u754c\" caf\u00E9",
			want:  []string{"echo", "\u3053\u3093\u306b\u3061\u306f\u4e16\u754c", "caf\u00E9"},
		},
		{
			name:  "emoji in path",
			input: `play "/games/🎮 collection/mario.sfc"`,
			want:  []string{"play", "/games/🎮 collection/mario.sfc"},
		},
		{
			name:  "non-breaking space is not a separator",
			input: "echo hello\u00A0world",
			want:  []string{"echo", "hello\u00A0world"},
		},
		{
			name:  "smart quotes are literal",
			input: "echo \u201Chello\u201D",
			want:  []string{"echo", "\u201Chello\u201D"},
		},
		{
			name:  "backslash before any char in double quotes is literal",
			input: `echo "hello\nworld"`,
			want:  []string{"echo", `hello\nworld`},
		},
		{
			name:  "backslash before closing quote is literal",
			input: `echo "hello\"`,
			want:  []string{"echo", `hello\`},
		},
		{
			name:  "shell metacharacters are literal",
			input: `echo hello;world | foo & bar > out < in`,
			want:  []string{"echo", "hello;world", "|", "foo", "&", "bar", ">", "out", "<", "in"},
		},
		{
			name:  "dollar and backtick in double quotes are literal",
			input: "echo \"$HOME `whoami`\"",
			want:  []string{"echo", "$HOME `whoami`"},
		},
		{
			name:  "newline is not a separator",
			input: "echo hello\nworld",
			want:  []string{"echo", "hello\nworld"},
		},
		{
			name:  "carriage return is not a separator",
			input: "echo hello\rworld",
			want:  []string{"echo", "hello\rworld"},
		},
		{
			name:  "multiple empty quoted strings",
			input: `"" "" ""`,
			want:  []string{"", "", ""},
		},
		{
			name:  "empty quotes adjacent to content",
			input: `""hello`,
			want:  []string{"hello"},
		},
		{
			name:  "backslash outside quotes is literal",
			input: `echo \\hello`,
			want:  []string{"echo", `\\hello`},
		},
		{
			name:  "windows UNC path in quotes",
			input: `"\\server\share\path with spaces"`,
			want:  []string{`\\server\share\path with spaces`},
		},
		{
			name:  "triple alternating quote styles",
			input: `"hello"'world'"!"`,
			want:  []string{"helloworld!"},
		},
		{
			name:  "quotes mid-word",
			input: `he"ll"o`,
			want:  []string{"hello"},
		},
		{
			name:    "bare double quote",
			input:   `"`,
			wantErr: ErrUnclosedQuote,
		},
		{
			name:    "bare single quote",
			input:   `'`,
			wantErr: ErrUnclosedQuote,
		},
		{
			name:  "bare backslash only",
			input: `\`,
			want:  []string{`\`},
		},
		{
			name:  "percent signs are literal",
			input: `echo "%PATH%" %USERPROFILE%`,
			want:  []string{"echo", "%PATH%", "%USERPROFILE%"},
		},
		{
			name:  "unquoted windows path",
			input: `C:\Games\retroarch.exe --fullscreen`,
			want:  []string{`C:\Games\retroarch.exe`, "--fullscreen"},
		},
		{
			name:  "unquoted windows path with quoted arg",
			input: `C:\path\app.exe "arg with spaces" --flag`,
			want:  []string{`C:\path\app.exe`, "arg with spaces", "--flag"},
		},
		{
			name:  "single quotes wrap double quotes",
			input: `notify-send '"Game started"'`,
			want:  []string{"notify-send", `"Game started"`},
		},
		{
			name:  "double quotes wrap single quotes",
			input: `echo "it's here"`,
			want:  []string{"echo", "it's here"},
		},
		{
			name:  "backslash inside double quotes is literal",
			input: `"C:\Program Files\app.exe"`,
			want:  []string{`C:\Program Files\app.exe`},
		},
		{
			name:  "doubled double quote inside double quotes",
			input: `program "some ""arg"`,
			want:  []string{"program", `some "arg`},
		},
		{
			name:  "doubled single quote inside single quotes",
			input: `program 'it''s here'`,
			want:  []string{"program", "it's here"},
		},
		{
			name:  "multiple doubled quotes",
			input: `echo "she said ""hello"" to me"`,
			want:  []string{"echo", `she said "hello" to me`},
		},
		{
			name:  "doubled quote at start of quoted string",
			input: `echo """hello"`,
			want:  []string{"echo", `"hello`},
		},
		{
			name:  "doubled quote at end of quoted string",
			input: `echo "hello"""`,
			want:  []string{"echo", `hello"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := SplitCommand(tt.input)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func FuzzSplitCommand(f *testing.F) {
	f.Add(`echo hello`)
	f.Add(`echo "hello world"`)
	f.Add(`echo 'hello world'`)
	f.Add(`echo hello\ world`)
	f.Add(`echo "she said ""hello"""`)
	f.Add(`echo "hello\\world"`)
	f.Add(`"" "" ""`)
	f.Add(`"C:\Program Files\app.exe" --flag "C:\path"`)
	f.Add(`program 'it''s here'`)
	f.Add(`echo "hello"'world'"!"`)
	f.Add(`echo "$HOME" '%PATH%'`)
	f.Add("echo hello\nworld")
	f.Add(`echo hello\`)
	f.Add(`"`)
	f.Add(`'`)
	f.Add(`\`)
	f.Add(``)
	f.Add(`echo "hello\nworld"`)
	f.Add("echo \u00A0 \u201Chello\u201D")

	f.Fuzz(func(t *testing.T, input string) {
		result, err := SplitCommand(input)
		if err != nil {
			require.ErrorIs(t, err, ErrUnclosedQuote)
			return
		}

		if input == "" {
			assert.Nil(t, result)
		}

		if result != nil {
			assert.NotEmpty(t, result)
		}
	})
}
