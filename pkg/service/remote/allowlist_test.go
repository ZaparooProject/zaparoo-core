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

// TestAllowlistEveryEntryHasParamsGuard ensures no operation type skips
// parameter validation, whether it dispatches locally or through the
// registry.
func TestAllowlistEveryEntryHasParamsGuard(t *testing.T) {
	t.Parallel()

	for opType, spec := range operationAllowlist {
		t.Run(opType, func(t *testing.T) {
			t.Parallel()
			require.NotNil(t, spec.params)
			require.True(t, spec.method != "" || spec.local != nil,
				"entry must dispatch either through the registry or locally")
			require.False(t, spec.method != "" && spec.local != nil,
				"entry must not set both method and local")
		})
	}
}

func TestLaunchersParams_RequiresSystemsFilter(t *testing.T) {
	t.Parallel()

	require.Error(t, launchersParams(json.RawMessage(`{}`)))
	require.Error(t, launchersParams(json.RawMessage(`{"systems":[]}`)))
	assert.NoError(t, launchersParams(json.RawMessage(`{"systems":["SNES"]}`)))
}

func TestStrictParams_RejectsUnknownFields(t *testing.T) {
	t.Parallel()

	// pathPrefix and sort-on-search are deliberately not part of the remote
	// search surface (see remoteSearchParams); an unknown field must be
	// refused, not silently ignored.
	err := strictParams[remoteSearchParams](json.RawMessage(`{"pathPrefix":"/roms"}`))
	require.Error(t, err)
}

func TestStrictParams_RejectsOversizedMaxResults(t *testing.T) {
	t.Parallel()

	err := strictParams[remoteSearchParams](json.RawMessage(`{"maxResults":101}`))
	require.Error(t, err)
	assert.NoError(t, strictParams[remoteSearchParams](json.RawMessage(`{"maxResults":100}`)))
}
