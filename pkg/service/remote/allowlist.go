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
	"context"
	"encoding/json"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/permissions"
)

// opSpec describes one allowlisted remote operation type. operationAllowlist
// is the single source of truth for what a Zaparoo Online remote operation
// can do on this device: an operation_type absent from it is refused with
// "unknown_type" before anything else runs.
type opSpec struct {
	// translate validates the operation's snake_case wire params and
	// returns the params the target expects: camelCase method params for a
	// method-backed entry, the validated wire params for a local one. It is
	// the only path params take into execution, so every entry must set it.
	translate func(json.RawMessage) (json.RawMessage, error)
	// encode converts a method's response into its snake_case wire shape
	// before the result cap is applied. Required for method-backed entries;
	// see wire.go for the field lists.
	encode func(any) (any, error)
	shrink func(json.RawMessage) (json.RawMessage, bool)
	local  func(m *manager, ctx context.Context, operationType string, raw json.RawMessage) operationResult
	method string
	role   permissions.Role
	limit  int
}

// operationAllowlist maps a remote operation_type to how this device
// executes it. role is omitted (zero value) for local entries: they never
// reach the permission-gated method registry, so it has no meaning there.
//
//nolint:gochecknoglobals // immutable allowlist; the single source of truth for what a remote operation can do
var operationAllowlist = map[string]opSpec{
	"echo": {
		local: (*manager).executeEcho, translate: translateEchoParams, limit: resultLimit,
	},
	"stop": {
		method: models.MethodStop, role: permissions.RoleRemote,
		translate: translateNoParams, encode: encodeEmptyResult, limit: resultLimit,
	},
	"systems": {
		method: models.MethodSystems, role: permissions.RoleRemote,
		translate: translateSystemsParams, encode: encodeSystemsResponse, limit: queryLimit,
	},
	"launchers": {
		method: models.MethodLaunchers, role: permissions.RoleRemote,
		translate: translateLaunchersParams, encode: encodeLaunchersResponse, limit: queryLimit,
	},
	"version": {
		method: models.MethodVersion, role: permissions.RoleRemote,
		translate: translateNoParams, encode: encodeVersionResponse, limit: resultLimit,
	},
	"media.search": {
		method: models.MethodMediaSearch, role: permissions.RoleRemote,
		translate: translateSearchParams, encode: encodeSearchResults, shrink: shrinkPage, limit: queryLimit,
	},
	"media.browse": {
		method: models.MethodMediaBrowse, role: permissions.RoleRemote,
		translate: translateBrowseParams, encode: encodeBrowseResults, shrink: shrinkPage, limit: queryLimit,
	},
	"launch": {
		local: (*manager).executeCommand, translate: translateCommandParams, limit: resultLimit,
	},
	"launch.system": {
		local: (*manager).executeCommand, translate: translateCommandParams, limit: resultLimit,
	},
	"mister.script": {
		local: (*manager).executeCommand, translate: translateCommandParams, limit: resultLimit,
	},
}
