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

//nolint:revive // custom validation tags (letter, etc.) are unknown to revive
package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/permissions"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/validation"
)

// opSpec describes one allowlisted remote operation type. operationAllowlist
// is the single source of truth for what a Zaparoo Online remote operation
// can do on this device: an operation_type absent from it is refused with
// "unknown_type" before anything else runs.
type opSpec struct {
	params func(json.RawMessage) error
	shrink func(json.RawMessage) (json.RawMessage, bool)
	local  func(m *manager, ctx context.Context, operationType string, raw json.RawMessage) operationResult
	method string
	role   permissions.Role
	limit  int
}

//nolint:tagliatelle // Matches models.SearchParams' tags exactly so raw params forward unchanged.
type remoteSearchParams struct {
	Query       *string   `json:"query"`
	Systems     *[]string `json:"systems" validate:"omitempty,dive,min=1"`
	FuzzySystem *bool     `json:"fuzzySystem,omitempty"`
	MaxResults  *int      `json:"maxResults" validate:"omitempty,gt=0,max=100"`
	Cursor      *string   `json:"cursor,omitempty"`
	Tags        *[]string `json:"tags,omitempty" validate:"omitempty,dive,min=1"`
	Letter      *string   `json:"letter,omitempty" validate:"omitempty,letter"`
	Sort        *string   `json:"sort,omitempty" validate:"omitempty,oneof=name-asc name-desc filename-asc filename-desc"`
}

//nolint:tagliatelle // Matches models.BrowseParams' tags exactly so raw params forward unchanged.
type remoteBrowseParams struct {
	Path        *string   `json:"path,omitempty"`
	Systems     *[]string `json:"systems" validate:"omitempty,dive,min=1"`
	FuzzySystem *bool     `json:"fuzzySystem,omitempty"`
	MaxResults  *int      `json:"maxResults,omitempty" validate:"omitempty,gt=0,max=100"`
	Cursor      *string   `json:"cursor,omitempty"`
	Letter      *string   `json:"letter,omitempty" validate:"omitempty,letter"`
	Sort        *string   `json:"sort,omitempty" validate:"omitempty,oneof=name-asc name-desc filename-asc filename-desc"`
}

//nolint:tagliatelle // Matches models.SystemsParams' tags exactly so raw params forward unchanged.
type remoteSystemsParams struct {
	All bool `json:"all,omitempty"`
}

//nolint:tagliatelle // Matches models.LaunchersParams' tags exactly so raw params forward unchanged.
type remoteLaunchersParams struct {
	Systems     *[]string `json:"systems,omitempty" validate:"omitempty,dive,min=1"`
	FuzzySystem *bool     `json:"fuzzySystem,omitempty"`
}

type echoParams struct {
	Message string `json:"message" validate:"max=256"`
}

//nolint:tagliatelle // Wire shape follows remote Online API contract; matched independently in command.go.
type commandParams struct {
	Value string `json:"value"`
}

// strictParams decodes raw into T with unknown fields and trailing JSON
// rejected, then validates it against T's `validate` tags. The decoded
// value is discarded — it exists only to gate what raw is allowed to
// contain before runMethod forwards raw itself to the target API method.
func strictParams[T any](raw json.RawMessage) error {
	var params T
	if err := decodeParams(raw, &params); err != nil {
		return err
	}
	if err := validation.DefaultValidator.Validate(&params); err != nil {
		return fmt.Errorf("validate remote operation params: %w", err)
	}
	return nil
}

// launchersParams applies strictParams' checks plus the one launchers-
// specific rule: a systems filter is required. The unfiltered launchers
// list has no shrink path and can exceed queryLimit on platforms with many
// launchers (250+ on MiSTer), so remote callers must always scope the call.
func launchersParams(raw json.RawMessage) error {
	var params remoteLaunchersParams
	if err := decodeParams(raw, &params); err != nil {
		return err
	}
	if err := validation.DefaultValidator.Validate(&params); err != nil {
		return fmt.Errorf("validate remote operation params: %w", err)
	}
	if params.Systems == nil || len(*params.Systems) == 0 {
		return errors.New("systems filter is required")
	}
	return nil
}

// operationAllowlist maps a remote operation_type to how this device
// executes it. role is omitted (zero value) for local entries: they never
// reach the permission-gated method registry, so it has no meaning there.
//
//nolint:gochecknoglobals // immutable allowlist; the single source of truth for what a remote operation can do
var operationAllowlist = map[string]opSpec{
	"echo": {
		local: (*manager).executeEcho, params: strictParams[echoParams], limit: resultLimit,
	},
	"stop": {
		method: models.MethodStop, role: permissions.RoleRemote,
		params: requireEmptyParams, limit: resultLimit,
	},
	"systems": {
		method: models.MethodSystems, role: permissions.RoleRemote,
		params: strictParams[remoteSystemsParams], limit: queryLimit,
	},
	"launchers": {
		method: models.MethodLaunchers, role: permissions.RoleRemote,
		params: launchersParams, limit: queryLimit,
	},
	"version": {
		method: models.MethodVersion, role: permissions.RoleRemote,
		params: requireEmptyParams, limit: resultLimit,
	},
	"media.search": {
		method: models.MethodMediaSearch, role: permissions.RoleRemote,
		params: strictParams[remoteSearchParams], shrink: shrinkPage, limit: queryLimit,
	},
	"media.browse": {
		method: models.MethodMediaBrowse, role: permissions.RoleRemote,
		params: strictParams[remoteBrowseParams], shrink: shrinkPage, limit: queryLimit,
	},
	"launch": {
		local: (*manager).executeCommand, params: strictParams[commandParams], limit: resultLimit,
	},
	"launch.system": {
		local: (*manager).executeCommand, params: strictParams[commandParams], limit: resultLimit,
	},
	"mister.script": {
		local: (*manager).executeCommand, params: strictParams[commandParams], limit: resultLimit,
	},
}
