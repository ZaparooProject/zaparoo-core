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

package remote

import (
	"encoding/json"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/permissions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAllowlistNeverGrantsAdmin is the regression guard for the
// EffectiveRole admin trap: an unpaired-remote-shaped RequestEnv (empty
// ClientRole, IsLocal false) resolves to admin under
// permissions.Grant.EffectiveRole, so every method-backed allowlist entry
// must carry an explicit, capability-empty role.
func TestAllowlistNeverGrantsAdmin(t *testing.T) {
	t.Parallel()

	for opType, spec := range operationAllowlist {
		t.Run(opType, func(t *testing.T) {
			t.Parallel()
			if spec.method == "" {
				// Local operations never reach the permission-gated
				// registry; role has no meaning for them.
				return
			}
			require.NotEqual(t, permissions.RoleAdmin, spec.role)
			require.NotEmpty(t, spec.role, "method-backed entries must set an explicit role")
			grant := permissions.Grant{Role: spec.role}
			assert.Empty(t, grant.Capabilities())
		})
	}
}

// TestAllowlistMethodsExist catches an allowlist entry whose method name
// doesn't match anything registered — otherwise it would silently resolve
// to "internal_error" at runtime instead of failing a build-time check.
func TestAllowlistMethodsExist(t *testing.T) {
	t.Parallel()

	methodMap := api.NewMethodMap()
	for opType, spec := range operationAllowlist {
		if spec.method == "" {
			continue
		}
		t.Run(opType, func(t *testing.T) {
			t.Parallel()
			_, ok := methodMap.GetMethod(spec.method)
			assert.True(t, ok, "allowlisted method %q is not registered", spec.method)
		})
	}
}

// TestAllowlistEveryEntryIsWired ensures no operation type skips params
// translation (which is also where validation happens), that every
// method-backed entry converts its response to the wire shape, and that an
// entry dispatches exactly one way.
func TestAllowlistEveryEntryIsWired(t *testing.T) {
	t.Parallel()

	for opType, spec := range operationAllowlist {
		t.Run(opType, func(t *testing.T) {
			t.Parallel()
			require.NotNil(t, spec.translate, "every entry must translate and validate its params")
			require.True(t, spec.method != "" || spec.local != nil,
				"entry must dispatch either through the registry or locally")
			require.False(t, spec.method != "" && spec.local != nil,
				"entry must not set both method and local")
			if spec.method != "" {
				require.NotNil(t, spec.encode, "method-backed entries must encode their result for the wire")
			}
		})
	}
}

func TestTranslateLaunchersParams_RequiresSystemsFilter(t *testing.T) {
	t.Parallel()

	_, err := translateLaunchersParams(json.RawMessage(`{}`))
	require.Error(t, err)
	_, err = translateLaunchersParams(json.RawMessage(`{"systems":[]}`))
	require.Error(t, err)
	// A non-empty systems array containing an empty string is still
	// invalid, since it can never match a real system ID.
	_, err = translateLaunchersParams(json.RawMessage(`{"systems":[""]}`))
	require.Error(t, err)

	translated, err := translateLaunchersParams(json.RawMessage(`{"systems":["SNES"],"fuzzy_system":true}`))
	require.NoError(t, err)
	var params models.LaunchersParams
	require.NoError(t, json.Unmarshal(translated, &params))
	require.NotNil(t, params.Systems)
	assert.Equal(t, []string{"SNES"}, *params.Systems)
	require.NotNil(t, params.FuzzySystem)
	assert.True(t, *params.FuzzySystem)
}

// TestTranslateSearchParams_RejectsUnknownAndCamelCaseFields pins that the
// wire is snake_case and strict: Core's own camelCase names are unknown
// fields here, as is anything outside the remote search surface
// (pathPrefix, sort on search). Nothing is silently ignored.
func TestTranslateSearchParams_RejectsUnknownAndCamelCaseFields(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		`{"maxResults":10}`,
		`{"fuzzySystem":true}`,
		`{"path_prefix":"/roms"}`,
		`{"pathPrefix":"/roms"}`,
		`{"sort":"name-asc"}`,
	} {
		_, err := translateSearchParams(json.RawMessage(raw))
		require.Error(t, err, raw)
	}
}

func TestTranslateSearchParams_BoundsMaxResults(t *testing.T) {
	t.Parallel()

	_, err := translateSearchParams(json.RawMessage(`{"max_results":101}`))
	require.Error(t, err)
	_, err = translateSearchParams(json.RawMessage(`{"max_results":0}`))
	require.Error(t, err)
	_, err = translateSearchParams(json.RawMessage(`{"max_results":100}`))
	require.NoError(t, err)
}

// TestTranslateSearchParams_ProducesMethodParams pins the field-by-field
// mapping from the snake_case wire into Core's SearchParams: every wire
// field lands on its camelCase counterpart, and the method params decode
// back to exactly what was sent.
func TestTranslateSearchParams_ProducesMethodParams(t *testing.T) {
	t.Parallel()

	translated, err := translateSearchParams(json.RawMessage(`{
		"query": "sonic", "systems": ["Genesis", "SNES"], "fuzzy_system": true,
		"max_results": 5, "cursor": "c1", "tags": ["genre:platformer"], "letter": "s"
	}`))
	require.NoError(t, err)

	var params models.SearchParams
	require.NoError(t, json.Unmarshal(translated, &params))
	require.NotNil(t, params.Query)
	assert.Equal(t, "sonic", *params.Query)
	require.NotNil(t, params.Systems)
	assert.Equal(t, []string{"Genesis", "SNES"}, *params.Systems)
	require.NotNil(t, params.FuzzySystem)
	assert.True(t, *params.FuzzySystem)
	require.NotNil(t, params.MaxResults)
	assert.Equal(t, 5, *params.MaxResults)
	require.NotNil(t, params.Cursor)
	assert.Equal(t, "c1", *params.Cursor)
	require.NotNil(t, params.Tags)
	assert.Equal(t, []string{"genre:platformer"}, *params.Tags)
	require.NotNil(t, params.Letter)
	assert.Equal(t, "s", *params.Letter)
	assert.Nil(t, params.Sort, "sort is not part of the remote search surface")
	assert.Nil(t, params.PathPrefix, "pathPrefix is not part of the remote search surface")

	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(translated, &raw))
	assert.Contains(t, raw, "maxResults")
	assert.Contains(t, raw, "fuzzySystem")
	assert.NotContains(t, raw, "max_results")
	assert.NotContains(t, raw, "fuzzy_system")
}

func TestTranslateBrowseParams_ProducesMethodParams(t *testing.T) {
	t.Parallel()

	translated, err := translateBrowseParams(json.RawMessage(`{
		"path": "/roms/SNES", "systems": ["SNES"], "fuzzy_system": false,
		"max_results": 20, "cursor": "c2", "letter": "c", "sort": "filename-desc"
	}`))
	require.NoError(t, err)

	var params models.BrowseParams
	require.NoError(t, json.Unmarshal(translated, &params))
	require.NotNil(t, params.Path)
	assert.Equal(t, "/roms/SNES", *params.Path)
	require.NotNil(t, params.Systems)
	assert.Equal(t, []string{"SNES"}, *params.Systems)
	require.NotNil(t, params.FuzzySystem)
	assert.False(t, *params.FuzzySystem)
	require.NotNil(t, params.MaxResults)
	assert.Equal(t, 20, *params.MaxResults)
	require.NotNil(t, params.Cursor)
	assert.Equal(t, "c2", *params.Cursor)
	require.NotNil(t, params.Letter)
	assert.Equal(t, "c", *params.Letter)
	require.NotNil(t, params.Sort)
	assert.Equal(t, "filename-desc", *params.Sort)
	assert.Nil(t, params.RootView, "rootView is not part of the remote browse surface")
	assert.Nil(t, params.Tags, "tags is not part of the remote browse surface")

	_, err = translateBrowseParams(json.RawMessage(`{"sort":"random"}`))
	require.Error(t, err)
	_, err = translateBrowseParams(json.RawMessage(`{"root_view":"routes"}`))
	require.Error(t, err)
	_, err = translateBrowseParams(json.RawMessage(`{"maxResults":5}`))
	require.Error(t, err)
}

func TestTranslateSystemsParams(t *testing.T) {
	t.Parallel()

	translated, err := translateSystemsParams(json.RawMessage(`{"all":true}`))
	require.NoError(t, err)
	var params models.SystemsParams
	require.NoError(t, json.Unmarshal(translated, &params))
	assert.True(t, params.All)

	translated, err = translateSystemsParams(nil)
	require.NoError(t, err)
	var defaults models.SystemsParams
	require.NoError(t, json.Unmarshal(translated, &defaults))
	assert.False(t, defaults.All)

	_, err = translateSystemsParams(json.RawMessage(`{"tags":["x"]}`))
	require.Error(t, err, "tags is not part of the remote systems surface")
}

func TestTranslateNoParams(t *testing.T) {
	t.Parallel()

	translated, err := translateNoParams(nil)
	require.NoError(t, err)
	assert.JSONEq(t, `{}`, string(translated))
	_, err = translateNoParams(json.RawMessage(`{}`))
	require.NoError(t, err)
	_, err = translateNoParams(json.RawMessage(`{"foo":"bar"}`))
	require.Error(t, err)
}

func TestTranslateEchoParams(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{"message":"hi"}`)
	translated, err := translateEchoParams(raw)
	require.NoError(t, err)
	assert.Equal(t, raw, translated, "echo params pass through unchanged after validation")

	_, err = translateEchoParams(json.RawMessage(`{"message":"` + string(make([]byte, 257)) + `"}`))
	require.Error(t, err)
	_, err = translateEchoParams(json.RawMessage(`{"message":"hi","extra":1}`))
	require.Error(t, err)
}

func TestTranslateCommandParams(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{"value":"Genesis/Sonic.md"}`)
	translated, err := translateCommandParams(raw)
	require.NoError(t, err)
	assert.Equal(t, raw, translated)

	_, err = translateCommandParams(json.RawMessage(`{"value":"x","launcher":"y"}`))
	require.Error(t, err, "advanced args ride inside value, never as separate fields")
}
