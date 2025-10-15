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

func TestParseSlugSyntax(t *testing.T) {
	t.Parallel()
	tests := []struct {
		wantErr error
		name    string
		input   string
		want    parser.Script
	}{
		{
			name:  "slug syntax - basic",
			input: `@snes::Super Mario World`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.slug", Args: []string{"snes/Super Mario World"}},
				},
			},
		},
		{
			name:  "slug syntax - with single tag",
			input: `@snes::Super Mario World;region:usa`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name:    "launch.slug",
						Args:    []string{"snes/Super Mario World"},
						AdvArgs: map[string]string{"tags": "region:usa"},
					},
				},
			},
		},
		{
			name:  "slug syntax - with multiple tags",
			input: `@snes::Super Mario World;region:usa;lang:en`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name:    "launch.slug",
						Args:    []string{"snes/Super Mario World"},
						AdvArgs: map[string]string{"tags": "region:usa,lang:en"},
					},
				},
			},
		},
		{
			name:  "slug syntax - hash in game name (no space before)",
			input: `@snes::F-Zero #1`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.slug", Args: []string{"snes/F-Zero #1"}},
				},
			},
		},
		{
			name:  "slug syntax - hash in game name then tag",
			input: `@snes::Game#Test;region:usa`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name:    "launch.slug",
						Args:    []string{"snes/Game#Test"},
						AdvArgs: map[string]string{"tags": "region:usa"},
					},
				},
			},
		},
		{
			name:  "slug syntax - escaped hash in game name",
			input: `@snes::Game^#1;region:usa`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name:    "launch.slug",
						Args:    []string{"snes/Game#1"},
						AdvArgs: map[string]string{"tags": "region:usa"},
					},
				},
			},
		},
		{
			name:  "slug syntax - with advanced args",
			input: `@snes::Super Mario World;region:usa?launcher=custom`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name: "launch.slug",
						Args: []string{"snes/Super Mario World"},
						AdvArgs: map[string]string{
							"tags":     "region:usa",
							"launcher": "custom",
						},
					},
				},
			},
		},
		{
			name:  "slug syntax - advanced args only (no tags)",
			input: `@snes::Super Mario World?launcher=custom`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name:    "launch.slug",
						Args:    []string{"snes/Super Mario World"},
						AdvArgs: map[string]string{"launcher": "custom"},
					},
				},
			},
		},
		{
			name:  "slug syntax - command chaining",
			input: `@snes::Super Mario World;region:usa||**delay:1000`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name:    "launch.slug",
						Args:    []string{"snes/Super Mario World"},
						AdvArgs: map[string]string{"tags": "region:usa"},
					},
					{Name: "delay", Args: []string{"1000"}},
				},
			},
		},
		{
			name:  "slug syntax - multiple slashes in game name",
			input: `@ps1::WCW/nWo Thunder;region:usa`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name:    "launch.slug",
						Args:    []string{"ps1/WCW/nWo Thunder"},
						AdvArgs: map[string]string{"tags": "region:usa"},
					},
				},
			},
		},
		{
			name:  "slug syntax - special characters in game name",
			input: `@arcade::Ms. Pac-Man;region:world`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name:    "launch.slug",
						Args:    []string{"arcade/Ms. Pac-Man"},
						AdvArgs: map[string]string{"tags": "region:world"},
					},
				},
			},
		},
		{
			name:  "slug syntax - ampersand in game name",
			input: `@genesis::Sonic & Knuckles;region:usa`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name:    "launch.slug",
						Args:    []string{"genesis/Sonic & Knuckles"},
						AdvArgs: map[string]string{"tags": "region:usa"},
					},
				},
			},
		},
		{
			name:  "slug syntax - three tags",
			input: `@snes::Game;region:usa;lang:en;type:game`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name:    "launch.slug",
						Args:    []string{"snes/Game"},
						AdvArgs: map[string]string{"tags": "region:usa,lang:en,type:game"},
					},
				},
			},
		},
		{
			name:  "slug syntax - tags with both advargs and command chain",
			input: `@snes::Game;region:usa?launcher=test||**delay:100`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name: "launch.slug",
						Args: []string{"snes/Game"},
						AdvArgs: map[string]string{
							"tags":     "region:usa",
							"launcher": "test",
						},
					},
					{Name: "delay", Args: []string{"100"}},
				},
			},
		},
		{
			name:  "slug syntax - trailing space (trimmed)",
			input: `@snes::Game Name  `,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.slug", Args: []string{"snes/Game Name"}},
				},
			},
		},
		{
			name:  "slug syntax - multiple spaces before tag",
			input: `@snes::Super Mario World ;region:usa`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name:    "launch.slug",
						Args:    []string{"snes/Super Mario World"},
						AdvArgs: map[string]string{"tags": "region:usa"},
					},
				},
			},
		},
		{
			name:  "slug syntax - multiple spaces between tags",
			input: `@snes::Game;region:usa ;lang:en`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name:    "launch.slug",
						Args:    []string{"snes/Game"},
						AdvArgs: map[string]string{"tags": "region:usa,lang:en"},
					},
				},
			},
		},
		{
			name:  "slug syntax - uppercase in tag (allowed)",
			input: `@snes::Super Mario World;REGION:USA`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name:    "launch.slug",
						Args:    []string{"snes/Super Mario World"},
						AdvArgs: map[string]string{"tags": "REGION:USA"},
					},
				},
			},
		},
		{
			name:  "slug syntax - tag with hyphen",
			input: `@snes::Game;region:north-america`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name:    "launch.slug",
						Args:    []string{"snes/Game"},
						AdvArgs: map[string]string{"tags": "region:north-america"},
					},
				},
			},
		},
		{
			name:  "slug syntax - hash with invalid char not a tag (slash)",
			input: `@snes::Game #Test/Invalid;region:usa`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name:    "launch.slug",
						Args:    []string{"snes/Game #Test/Invalid"},
						AdvArgs: map[string]string{"tags": "region:usa"},
					},
				},
			},
		},
		{
			name:  "slug syntax - hash with invalid char not a tag (dot)",
			input: `@snes::Game #v1.2;region:usa`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name:    "launch.slug",
						Args:    []string{"snes/Game #v1.2"},
						AdvArgs: map[string]string{"tags": "region:usa"},
					},
				},
			},
		},
		{
			name:  "slug syntax - hash with invalid char not a tag (parenthesis)",
			input: `@snes::Game #3(Special);region:usa`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name:    "launch.slug",
						Args:    []string{"snes/Game #3(Special)"},
						AdvArgs: map[string]string{"tags": "region:usa"},
					},
				},
			},
		},
		{
			name:  "slug syntax - hash without colon not a tag",
			input: `@snes::Game #Special;region:usa`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name:    "launch.slug",
						Args:    []string{"snes/Game #Special"},
						AdvArgs: map[string]string{"tags": "region:usa"},
					},
				},
			},
		},
		{
			name:  "slug syntax - multiple hyphens in tag",
			input: `@snes::Game;type:action-adventure-rpg`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name:    "launch.slug",
						Args:    []string{"snes/Game"},
						AdvArgs: map[string]string{"tags": "type:action-adventure-rpg"},
					},
				},
			},
		},
		{
			name:  "slug syntax - numbers in tag",
			input: `@snes::Game;players:2-4;year:1992`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name:    "launch.slug",
						Args:    []string{"snes/Game"},
						AdvArgs: map[string]string{"tags": "players:2-4,year:1992"},
					},
				},
			},
		},
		{
			name:  "slug syntax - hash with word after (no colon, user example)",
			input: `@SNES::Super #Mario World`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.slug", Args: []string{"SNES/Super #Mario World"}},
				},
			},
		},
		{
			name:  "slug syntax - multiple colon-less hashes in game name",
			input: `@snes::WWF #Raw is #War`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.slug", Args: []string{"snes/WWF #Raw is #War"}},
				},
			},
		},
		{
			name:  "slug syntax - string ends immediately after space+hash",
			input: `@snes::Game #`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.slug", Args: []string{"snes/Game #"}},
				},
			},
		},
		{
			name:  "slug syntax - invalid character in tag name (exclamation)",
			input: `@snes::Game #reg!on:jp;lang:en`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name:    "launch.slug",
						Args:    []string{"snes/Game #reg!on:jp"},
						AdvArgs: map[string]string{"tags": "lang:en"},
					},
				},
			},
		},
		{
			name:  "slug syntax - slash in tag value (invalid, becomes part of slug)",
			input: `@snes::Game;region:jp/eu;lang:en`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name:    "launch.slug",
						Args:    []string{"snes/Game;region:jp/eu"},
						AdvArgs: map[string]string{"tags": "lang:en"},
					},
				},
			},
		},
		{
			name:  "slug syntax - tag with empty value (invalid, becomes part of slug)",
			input: `@snes::Game;prototype:`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name: "launch.slug",
						Args: []string{"snes/Game;prototype:"},
					},
				},
			},
		},
		{
			name:  "slug syntax - hash-colon in game name (not a tag)",
			input: `@snes::Game #:hacked`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name: "launch.slug",
						Args: []string{"snes/Game #:hacked"},
					},
				},
			},
		},
		{
			name:  "slug syntax - tag with multiple colons (subtypes)",
			input: `@snes::Game;region:usa:ntsc;lang:en`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name:    "launch.slug",
						Args:    []string{"snes/Game"},
						AdvArgs: map[string]string{"tags": "region:usa:ntsc,lang:en"},
					},
				},
			},
		},
		{
			name:  "slug syntax - tag with hyphens throughout",
			input: `@snes::Game;version:1-2-3;type:action-rpg`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name:    "launch.slug",
						Args:    []string{"snes/Game"},
						AdvArgs: map[string]string{"tags": "version:1-2-3,type:action-rpg"},
					},
				},
			},
		},
		{
			name:  "slug syntax - tag with underscore (invalid, becomes part of slug)",
			input: `@snes::Game;my_tag:value;region:usa`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name:    "launch.slug",
						Args:    []string{"snes/Game;my_tag:value"},
						AdvArgs: map[string]string{"tags": "region:usa"},
					},
				},
			},
		},
		{
			name:  "slug syntax - tag with space (invalid)",
			input: `@snes::Game #region code:usa`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.slug", Args: []string{"snes/Game #region code:usa"}},
				},
			},
		},
		{
			name:  "slug syntax - hash at end of game name no space before",
			input: `@snes::Game#;region:usa`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name:    "launch.slug",
						Args:    []string{"snes/Game#"},
						AdvArgs: map[string]string{"tags": "region:usa"},
					},
				},
			},
		},
		{
			name:  "slug syntax - NOT operator single tag",
			input: `@snes::Super Mario World;-region:japan`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name:    "launch.slug",
						Args:    []string{"snes/Super Mario World"},
						AdvArgs: map[string]string{"tags": "-region:japan"},
					},
				},
			},
		},
		{
			name:  "slug syntax - OR operator multiple tags",
			input: `@snes::Game;~lang:en;~lang:ja`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name:    "launch.slug",
						Args:    []string{"snes/Game"},
						AdvArgs: map[string]string{"tags": "~lang:en,~lang:ja"},
					},
				},
			},
		},
		{
			name:  "slug syntax - mixed operators",
			input: `@ps1::Final Fantasy 7;region:usa;-region:japan;~lang:en;~lang:ja`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name:    "launch.slug",
						Args:    []string{"ps1/Final Fantasy 7"},
						AdvArgs: map[string]string{"tags": "region:usa,-region:japan,~lang:en,~lang:ja"},
					},
				},
			},
		},
		{
			name:  "slug syntax - NOT with advanced args",
			input: `@genesis::Sonic;-region:japan?launcher=custom`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name: "launch.slug",
						Args: []string{"genesis/Sonic"},
						AdvArgs: map[string]string{
							"tags":     "-region:japan",
							"launcher": "custom",
						},
					},
				},
			},
		},
		{
			name:  "slug syntax - OR with advanced args",
			input: `@arcade::Street Fighter;~players:1;~players:2?launcher=mame`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name: "launch.slug",
						Args: []string{"arcade/Street Fighter"},
						AdvArgs: map[string]string{
							"tags":     "~players:1,~players:2",
							"launcher": "mame",
						},
					},
				},
			},
		},
		{
			name:  "slug syntax - complex operators with command chain",
			input: `@snes::Mario;region:usa;-lang:jp;~version:1.0;~version:1.1||**delay:1000`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name:    "launch.slug",
						Args:    []string{"snes/Mario"},
						AdvArgs: map[string]string{"tags": "region:usa,-lang:jp,~version:1.0,~version:1.1"},
					},
					{Name: "delay", Args: []string{"1000"}},
				},
			},
		},
		{
			name:  "slug syntax - fallback: no :: separator treats as auto-launch",
			input: `@SomeFile`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch", Args: []string{"@SomeFile"}},
				},
			},
		},
		{
			name:  "slug syntax - fallback: @ with semicolon but no :: treats as auto-launch",
			input: `@File;Name`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch", Args: []string{"@File;Name"}},
				},
			},
		},
		{
			name:  "slug syntax - first tag without colon becomes part of slug",
			input: `@snes::Game;NotATag;region:usa`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name:    "launch.slug",
						Args:    []string{"snes/Game;NotATag"},
						AdvArgs: map[string]string{"tags": "region:usa"},
					},
				},
			},
		},
		{
			name:  "slug syntax - subsequent tags without colon are skipped",
			input: `@snes::Game;region:usa;InvalidTag;lang:en;AnotherBad`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name:    "launch.slug",
						Args:    []string{"snes/Game"},
						AdvArgs: map[string]string{"tags": "region:usa,lang:en"},
					},
				},
			},
		},
		{
			name:  "slug syntax - all tags invalid after first are skipped",
			input: `@snes::Game;region:usa;Bad1;Bad2;Bad3`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name:    "launch.slug",
						Args:    []string{"snes/Game"},
						AdvArgs: map[string]string{"tags": "region:usa"},
					},
				},
			},
		},
		{
			name:  "slug syntax - AND operator with + prefix",
			input: `@snes::Game;+region:usa;lang:en`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name:    "launch.slug",
						Args:    []string{"snes/Game"},
						AdvArgs: map[string]string{"tags": "+region:usa,lang:en"},
					},
				},
			},
		},
		{
			name:  "slug syntax - mixed operators with +",
			input: `@snes::Game;+region:usa;-lang:jp;~version:1-0;~version:1-1`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name:    "launch.slug",
						Args:    []string{"snes/Game"},
						AdvArgs: map[string]string{"tags": "+region:usa,-lang:jp,~version:1-0,~version:1-1"},
					},
				},
			},
		},
		// Edge case: Empty slug parts
		{
			name:  "slug syntax - empty system ID",
			input: `@::Game Name`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.slug", Args: []string{"/Game Name"}},
				},
			},
		},
		{
			name:  "slug syntax - empty game name",
			input: `@snes::`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.slug", Args: []string{"snes/"}},
				},
			},
		},
		{
			name:  "slug syntax - both system and game empty",
			input: `@::`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.slug", Args: []string{"/"}},
				},
			},
		},
		{
			name:  "slug syntax - empty game name with tag",
			input: `@snes::;region:usa`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name:    "launch.slug",
						Args:    []string{"snes/"},
						AdvArgs: map[string]string{"tags": "region:usa"},
					},
				},
			},
		},
		// Edge case: Operators without tags
		{
			name:  "slug syntax - just + operator (invalid, skipped)",
			input: `@snes::Game;+`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.slug", Args: []string{"snes/Game;+"}},
				},
			},
		},
		{
			name:  "slug syntax - just - operator (invalid, becomes part of slug)",
			input: `@snes::Game;-`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.slug", Args: []string{"snes/Game;-"}},
				},
			},
		},
		{
			name:  "slug syntax - just ~ operator (invalid, becomes part of slug)",
			input: `@snes::Game;~`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.slug", Args: []string{"snes/Game;~"}},
				},
			},
		},
		{
			name:  "slug syntax - operator at end after valid tag",
			input: `@snes::Game;region:usa;+`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name:    "launch.slug",
						Args:    []string{"snes/Game"},
						AdvArgs: map[string]string{"tags": "region:usa"},
					},
				},
			},
		},
		// Edge case: Escape sequences
		{
			name:  "slug syntax - escaping semicolon separator",
			input: `@snes::game^;name;region:usa`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name:    "launch.slug",
						Args:    []string{"snes/game;name"},
						AdvArgs: map[string]string{"tags": "region:usa"},
					},
				},
			},
		},
		{
			name:  "slug syntax - escaping colon in tag (still valid after escape)",
			input: `@snes::Game;region^:usa`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name:    "launch.slug",
						Args:    []string{"snes/Game"},
						AdvArgs: map[string]string{"tags": "region:usa"},
					},
				},
			},
		},
		{
			name:  "slug syntax - escaped space in slug",
			input: `@snes::Super^ Mario`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.slug", Args: []string{"snes/Super Mario"}},
				},
			},
		},
		{
			name:  "slug syntax - escape sequence in tag value",
			input: `@snes::Game;path:C^:^/Users`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.slug", Args: []string{"snes/Game;path:C:/Users"}},
				},
			},
		},
		// Edge case: Multiple :: separators (only first becomes /, rest stay as ::)
		{
			name:  "slug syntax - multiple :: in slug (only first becomes /)",
			input: `@snes::game::extra`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.slug", Args: []string{"snes/game::extra"}},
				},
			},
		},
		{
			name:  "slug syntax - multiple :: with tags (only first becomes /)",
			input: `@a::b::c;region:usa`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name:    "launch.slug",
						Args:    []string{"a/b::c"},
						AdvArgs: map[string]string{"tags": "region:usa"},
					},
				},
			},
		},
		{
			name:  "slug syntax - three :: separators (only first becomes /)",
			input: `@sys::game::extra::more`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.slug", Args: []string{"sys/game::extra::more"}},
				},
			},
		},
		// Edge case: Empty tags between semicolons
		{
			name:  "slug syntax - double semicolon (empty tag skipped)",
			input: `@snes::Game;;region:usa`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name:    "launch.slug",
						Args:    []string{"snes/Game"},
						AdvArgs: map[string]string{"tags": "region:usa"},
					},
				},
			},
		},
		{
			name:  "slug syntax - multiple empty tags",
			input: `@snes::Game;;;region:usa;;lang:en;`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name:    "launch.slug",
						Args:    []string{"snes/Game"},
						AdvArgs: map[string]string{"tags": "region:usa,lang:en"},
					},
				},
			},
		},
		{
			name:  "slug syntax - trailing semicolons",
			input: `@snes::Game;region:usa;;`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name:    "launch.slug",
						Args:    []string{"snes/Game"},
						AdvArgs: map[string]string{"tags": "region:usa"},
					},
				},
			},
		},
		{
			name:  "slug syntax - only semicolons after slug",
			input: `@snes::Game;;;`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.slug", Args: []string{"snes/Game"}},
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
