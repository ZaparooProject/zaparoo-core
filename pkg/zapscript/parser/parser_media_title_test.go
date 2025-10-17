// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

package parser_test

import (
	"errors"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript/parser"
	"github.com/google/go-cmp/cmp"
)

func TestParseMediaTitleSyntax(t *testing.T) {
	t.Parallel()
	tests := []struct {
		wantErr error
		name    string
		input   string
		want    parser.Script
	}{
		// Basic format tests
		{
			name:  "basic media title",
			input: `@snes/Super Mario World`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.title", Args: []string{"snes/Super Mario World"}},
				},
			},
		},
		{
			name:  "with system name containing spaces",
			input: `@Sega Genesis/Sonic the Hedgehog`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.title", Args: []string{"Sega Genesis/Sonic the Hedgehog"}},
				},
			},
		},
		{
			name:  "with special characters in title",
			input: `@arcade/Ms. Pac-Man`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.title", Args: []string{"arcade/Ms. Pac-Man"}},
				},
			},
		},
		{
			name:  "with ampersand in title",
			input: `@genesis/Sonic & Knuckles`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.title", Args: []string{"genesis/Sonic & Knuckles"}},
				},
			},
		},
		{
			name:  "with multiple slashes in title",
			input: `@ps1/WCW/nWo Thunder`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.title", Args: []string{"ps1/WCW/nWo Thunder"}},
				},
			},
		},

		// Parentheses (filename metadata for tag extraction)
		{
			name:  "with single parenthesis group",
			input: `@snes/Super Mario World (USA)`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.title", Args: []string{"snes/Super Mario World (USA)"}},
				},
			},
		},
		{
			name:  "with multiple parenthesis groups",
			input: `@snes/Super Mario World (USA) (Rev 1)`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.title", Args: []string{"snes/Super Mario World (USA) (Rev 1)"}},
				},
			},
		},
		{
			name:  "with canonical tag in parenthesis",
			input: `@snes/Game (year:1994)`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.title", Args: []string{"snes/Game (year:1994)"}},
				},
			},
		},
		{
			name:  "with canonical tags in multiple parenthesis groups",
			input: `@snes/Game (region:us) (year:1994) (lang:en)`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.title", Args: []string{"snes/Game (region:us) (year:1994) (lang:en)"}},
				},
			},
		},
		{
			name:  "with mixed filename and canonical tags",
			input: `@snes/Super Mario World (USA) (year:1991) (Rev A)`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.title", Args: []string{"snes/Super Mario World (USA) (year:1991) (Rev A)"}},
				},
			},
		},
		{
			name:  "with tag operators in parentheses",
			input: `@snes/Game (-unfinished:beta) (+region:us)`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.title", Args: []string{"snes/Game (-unfinished:beta) (+region:us)"}},
				},
			},
		},

		// Advanced args
		{
			name:  "with single advanced arg",
			input: `@snes/Super Mario World?launcher=custom`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name:    "launch.title",
						Args:    []string{"snes/Super Mario World"},
						AdvArgs: map[string]string{"launcher": "custom"},
					},
				},
			},
		},
		{
			name:  "with multiple advanced args",
			input: `@snes/Game?launcher=custom&tags=region:us`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name: "launch.title",
						Args: []string{"snes/Game"},
						AdvArgs: map[string]string{
							"launcher": "custom",
							"tags":     "region:us",
						},
					},
				},
			},
		},
		{
			name:  "with parentheses and advanced args",
			input: `@snes/Game (USA) (year:1994)?launcher=custom`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name:    "launch.title",
						Args:    []string{"snes/Game (USA) (year:1994)"},
						AdvArgs: map[string]string{"launcher": "custom"},
					},
				},
			},
		},

		// Escape sequences
		{
			name:  "escaped slash in title",
			input: `@snes/Game^/Name`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.title", Args: []string{"snes/Game/Name"}},
				},
			},
		},
		{
			name:  "escaped space",
			input: `@snes/Super^ Mario`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.title", Args: []string{"snes/Super Mario"}},
				},
			},
		},
		{
			name:  "escaped question mark",
			input: `@snes/What^?`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.title", Args: []string{"snes/What?"}},
				},
			},
		},
		{
			name:  "escaped parenthesis",
			input: `@snes/Game^(2^)`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.title", Args: []string{"snes/Game(2)"}},
				},
			},
		},

		// Command chaining
		{
			name:  "chained with delay command",
			input: `@snes/Super Mario World||**delay:1000`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.title", Args: []string{"snes/Super Mario World"}},
					{Name: "delay", Args: []string{"1000"}},
				},
			},
		},
		{
			name:  "chained with parentheses and command",
			input: `@snes/Game (USA)||**delay:500`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.title", Args: []string{"snes/Game (USA)"}},
					{Name: "delay", Args: []string{"500"}},
				},
			},
		},

		// Whitespace handling
		{
			name:  "trailing space trimmed",
			input: `@snes/Game Name  `,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.title", Args: []string{"snes/Game Name"}},
				},
			},
		},
		{
			name:  "leading space after slash",
			input: `@snes/ Game Name`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.title", Args: []string{"snes/ Game Name"}},
				},
			},
		},
		{
			name:  "spaces in parentheses preserved",
			input: `@snes/Game ( USA ) ( Rev 1 )`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.title", Args: []string{"snes/Game ( USA ) ( Rev 1 )"}},
				},
			},
		},

		// Invalid format (fallback to auto-launch)
		{
			name:  "no slash separator - fallback to auto-launch",
			input: `@SomeFile`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch", Args: []string{"@SomeFile"}},
				},
			},
		},
		{
			name:  "empty after @ - fallback to auto-launch",
			input: `@`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch", Args: []string{"@"}},
				},
			},
		},
		{
			name:  "only system no slash - fallback to auto-launch",
			input: `@snes`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch", Args: []string{"@snes"}},
				},
			},
		},
		{
			name:  "with parentheses but no slash - fallback to auto-launch",
			input: `@File (USA)`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch", Args: []string{"@File (USA)"}},
				},
			},
		},

		// Edge cases
		{
			name:  "empty system ID",
			input: `@/Game Name`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.title", Args: []string{"/Game Name"}},
				},
			},
		},
		{
			name:  "empty game name",
			input: `@snes/`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.title", Args: []string{"snes/"}},
				},
			},
		},
		{
			name:  "just slash",
			input: `@/`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.title", Args: []string{"/"}},
				},
			},
		},
		{
			name:  "multiple consecutive slashes",
			input: `@snes///Game`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.title", Args: []string{"snes///Game"}},
				},
			},
		},
		{
			name:  "unicode characters in title",
			input: `@sfc/ドラゴンクエストVII`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.title", Args: []string{"sfc/ドラゴンクエストVII"}},
				},
			},
		},
		{
			name:  "unicode in system and title",
			input: `@スーパーファミコン/ゼルダの伝説`, //nolint:gosmopolitan // Japanese test
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.title", Args: []string{"スーパーファミコン/ゼルダの伝説"}}, //nolint:gosmopolitan // Japanese test
				},
			},
		},
		{
			name:  "numbers in system ID",
			input: `@3do/Road Rash`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.title", Args: []string{"3do/Road Rash"}},
				},
			},
		},
		{
			name:  "hyphens in system ID",
			input: `@sega-cd/Sonic CD`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.title", Args: []string{"sega-cd/Sonic CD"}},
				},
			},
		},

		// Complex real-world examples
		{
			name:  "complex with everything",
			input: `@Sega Genesis/Sonic & Knuckles (USA) (Rev A) (year:1994)?launcher=custom&tags=region:us`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name: "launch.title",
						Args: []string{"Sega Genesis/Sonic & Knuckles (USA) (Rev A) (year:1994)"},
						AdvArgs: map[string]string{
							"launcher": "custom",
							"tags":     "region:us",
						},
					},
				},
			},
		},
		{
			name:  "long title with multiple metadata groups",
			input: `@ps1/Final Fantasy VII (USA) (Disc 1) (Rev 1) (year:1997) (lang:en)`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name: "launch.title",
						Args: []string{"ps1/Final Fantasy VII (USA) (Disc 1) (Rev 1) (year:1997) (lang:en)"},
					},
				},
			},
		},
		{
			name:  "with nested parentheses in title",
			input: `@snes/Game (Prototype (Beta))`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.title", Args: []string{"snes/Game (Prototype (Beta))"}},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := parser.NewParser(tt.input)
			got, err := p.ParseScript()
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("ParseScript() error = %v, wantErr = %v", err, tt.wantErr)
				return
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("ParseScript() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
