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

package advargs

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse_GlobalArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		raw      map[string]string
		wantWhen string
		wantErr  bool
	}{
		{
			name:     "empty args",
			raw:      map[string]string{},
			wantWhen: "",
			wantErr:  false,
		},
		{
			name:     "when arg set",
			raw:      map[string]string{"when": "true"},
			wantWhen: "true",
			wantErr:  false,
		},
		{
			name:     "when arg with expression result",
			raw:      map[string]string{"when": "1"},
			wantWhen: "1",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var args GlobalArgs
			err := Parse(tt.raw, &args, nil)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantWhen, args.When)
		})
	}
}

func TestGlobalArgs_ShouldRun(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		when string
		want bool
	}{
		{name: "empty when", when: "", want: true},
		{name: "true", when: "true", want: true},
		{name: "yes", when: "yes", want: true},
		{name: "false", when: "false", want: false},
		{name: "no", when: "no", want: false},
		// IsTruthy only accepts "true" and "yes", all other values are falsey
		{name: "1 is falsey", when: "1", want: false},
		{name: "0 is falsey", when: "0", want: false},
		{name: "random string is falsey", when: "random", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			args := GlobalArgs{When: tt.when}
			assert.Equal(t, tt.want, args.ShouldRun())
		})
	}
}

func TestParse_LaunchRandomArgs(t *testing.T) {
	t.Parallel()

	ctx := NewParseContext([]string{"steam", "retroarch", "mister"})

	tests := []struct {
		raw        map[string]string
		check      func(t *testing.T, args *LaunchRandomArgs)
		name       string
		errContain string
		wantErr    bool
	}{
		{
			name:    "empty args",
			raw:     map[string]string{},
			wantErr: false,
			check: func(t *testing.T, args *LaunchRandomArgs) {
				assert.Empty(t, args.Launcher)
				assert.Empty(t, args.Action)
				assert.Nil(t, args.Tags)
			},
		},
		{
			name:    "valid launcher",
			raw:     map[string]string{"launcher": "steam"},
			wantErr: false,
			check: func(t *testing.T, args *LaunchRandomArgs) {
				assert.Equal(t, "steam", args.Launcher)
			},
		},
		{
			name:       "invalid launcher",
			raw:        map[string]string{"launcher": "nonexistent"},
			wantErr:    true,
			errContain: "launcher",
		},
		{
			name:    "valid action run",
			raw:     map[string]string{"action": "run"},
			wantErr: false,
			check: func(t *testing.T, args *LaunchRandomArgs) {
				assert.Equal(t, "run", args.Action)
			},
		},
		{
			name:    "valid action details",
			raw:     map[string]string{"action": "details"},
			wantErr: false,
			check: func(t *testing.T, args *LaunchRandomArgs) {
				assert.Equal(t, "details", args.Action)
			},
		},
		{
			name:       "invalid action",
			raw:        map[string]string{"action": "invalid"},
			wantErr:    true,
			errContain: "action must be one of",
		},
		{
			name:    "valid tags",
			raw:     map[string]string{"tags": "region:usa,type:game"},
			wantErr: false,
			check: func(t *testing.T, args *LaunchRandomArgs) {
				require.Len(t, args.Tags, 2)
				assert.Equal(t, "region", args.Tags[0].Type)
				assert.Equal(t, "usa", args.Tags[0].Value)
			},
		},
		{
			name:       "invalid tags format",
			raw:        map[string]string{"tags": "invalid_format"},
			wantErr:    true,
			errContain: "invalid tags format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var args LaunchRandomArgs
			err := Parse(tt.raw, &args, ctx)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContain != "" {
					assert.Contains(t, err.Error(), tt.errContain)
				}
				return
			}

			require.NoError(t, err)
			if tt.check != nil {
				tt.check(t, &args)
			}
		})
	}
}

func TestParse_LaunchArgs(t *testing.T) {
	t.Parallel()

	ctx := NewParseContext([]string{"steam"})

	tests := []struct {
		raw        map[string]string
		check      func(t *testing.T, args *LaunchArgs)
		name       string
		errContain string
		wantErr    bool
	}{
		{
			name: "all fields",
			raw: map[string]string{
				"launcher": "steam", "system": "snes", "action": "run",
				"name": "test", "pre_notice": "notice",
			},
			wantErr: false,
			check: func(t *testing.T, args *LaunchArgs) {
				assert.Equal(t, "steam", args.Launcher)
				assert.Equal(t, "snes", args.System)
				assert.Equal(t, "run", args.Action)
				assert.Equal(t, "test", args.Name)
				assert.Equal(t, "notice", args.PreNotice)
			},
		},
		{
			name:       "invalid system",
			raw:        map[string]string{"system": "nonexistent_system_xyz"},
			wantErr:    true,
			errContain: "system",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var args LaunchArgs
			err := Parse(tt.raw, &args, ctx)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContain != "" {
					assert.Contains(t, err.Error(), tt.errContain)
				}
				return
			}

			require.NoError(t, err)
			if tt.check != nil {
				tt.check(t, &args)
			}
		})
	}
}

func TestParse_PlaylistArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		raw        map[string]string
		name       string
		errContain string
		wantMode   string
		wantErr    bool
	}{
		{
			name:     "empty mode",
			raw:      map[string]string{},
			wantErr:  false,
			wantMode: "",
		},
		{
			name:     "shuffle mode",
			raw:      map[string]string{"mode": "shuffle"},
			wantErr:  false,
			wantMode: "shuffle",
		},
		{
			name:       "invalid mode",
			raw:        map[string]string{"mode": "random"},
			wantErr:    true,
			errContain: "mode must be one of",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var args PlaylistArgs
			err := Parse(tt.raw, &args, nil)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContain != "" {
					assert.Contains(t, err.Error(), tt.errContain)
				}
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantMode, args.Mode)
		})
	}
}

func TestParse_TagFiltersDecoding(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		tagsStr  string
		wantTags []database.TagFilter
		wantErr  bool
		wantNil  bool
	}{
		{
			name:    "empty tags",
			tagsStr: "",
			wantErr: false,
			wantNil: true, // nil when not specified
		},
		{
			name:    "single tag",
			tagsStr: "region:usa",
			wantErr: false,
			wantTags: []database.TagFilter{
				{Type: "region", Value: "usa", Operator: database.TagOperatorAND},
			},
		},
		{
			name:    "multiple tags",
			tagsStr: "region:usa,type:game",
			wantErr: false,
			wantTags: []database.TagFilter{
				{Type: "region", Value: "usa", Operator: database.TagOperatorAND},
				{Type: "type", Value: "game", Operator: database.TagOperatorAND},
			},
		},
		{
			// "+" prefix means AND (same as no prefix), "~" is OR
			name:    "tag with explicit AND prefix",
			tagsStr: "+region:usa",
			wantErr: false,
			wantTags: []database.TagFilter{
				{Type: "region", Value: "usa", Operator: database.TagOperatorAND},
			},
		},
		{
			name:    "tag with OR operator (tilde prefix)",
			tagsStr: "~region:usa",
			wantErr: false,
			wantTags: []database.TagFilter{
				{Type: "region", Value: "usa", Operator: database.TagOperatorOR},
			},
		},
		{
			name:    "tag with NOT operator",
			tagsStr: "-region:usa",
			wantErr: false,
			wantTags: []database.TagFilter{
				{Type: "region", Value: "usa", Operator: database.TagOperatorNOT},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			raw := map[string]string{}
			if tt.tagsStr != "" {
				raw["tags"] = tt.tagsStr
			}

			var args LaunchRandomArgs
			err := Parse(raw, &args, nil)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			if tt.wantNil {
				assert.Nil(t, args.Tags)
			} else {
				assert.Equal(t, tt.wantTags, args.Tags)
			}
		})
	}
}

func TestParser_NewParser(t *testing.T) {
	t.Parallel()

	p := NewParser()
	require.NotNil(t, p)
	require.NotNil(t, p.validate)

	// Verify can parse successfully
	var args GlobalArgs
	err := p.Parse(map[string]string{"when": "true"}, &args, nil)
	require.NoError(t, err)
	assert.Equal(t, "true", args.When)
}

func TestParseContext(t *testing.T) {
	t.Parallel()

	launchers := []string{"steam", "retroarch"}
	ctx := NewParseContext(launchers)

	assert.Equal(t, launchers, ctx.LauncherIDs)
}
