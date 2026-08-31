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

package methods

import (
	"encoding/json"
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/validation"
	"github.com/rs/zerolog/log"
)

// defaultRemoteActivityLimit is used when no limit is requested. The upper
// bound (100) is enforced by RemoteActivityParams' validate tag, which
// rejects an out-of-range request rather than silently clamping it.
const defaultRemoteActivityLimit = 20

// remoteActivityOrigin mirrors the shape remote.operationOrigin writes into
// RemoteCommand.Origin (pkg/service/remote/operations.go). Duplicated here
// rather than imported: it is the device's own on-disk wire shape, and
// pkg/api/methods must not import pkg/service/remote (which already imports
// pkg/api/models).
//
//nolint:tagliatelle // Matches the on-disk shape written by remote.operationOrigin, not a wire response.
type remoteActivityOrigin struct {
	Kind    string `json:"kind"`
	KeyName string `json:"key_name,omitempty"`
}

// HandleRemoteActivity returns the most recent entries from the remote
// operations ledger, as a local-owner-facing audit trail of what a linked
// account's remote commands have actually done. Not part of the remote
// operation allowlist — a remote caller reading its own history back is a
// separate decision, not made here.
//
//nolint:gocritic // single-use parameter in API handler
func HandleRemoteActivity(env requests.RequestEnv) (any, error) {
	if !isLocalOrAdmin(&env) {
		return nil, models.ClientErrf("remote activity requires a local or admin client")
	}

	var params models.RemoteActivityParams
	if len(env.Params) > 0 {
		if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
			return nil, models.ClientErrf("invalid params: %w", err)
		}
	}

	// Limit is fully validated above (max=100), not clamped, so an
	// out-of-range request is rejected rather than silently reinterpreted.
	limit := defaultRemoteActivityLimit
	if params.Limit != nil {
		limit = *params.Limit
	}

	if env.Database == nil || env.Database.UserDB == nil {
		return models.RemoteActivityResponse{Entries: []models.RemoteActivityEntry{}}, nil
	}

	commands, err := env.Database.UserDB.ListRecentRemoteCommands(limit)
	if err != nil {
		log.Error().Err(err).Msg("error listing remote command activity")
		return nil, fmt.Errorf("error listing remote command activity: %w", err)
	}

	entries := make([]models.RemoteActivityEntry, 0, len(commands))
	for _, command := range commands {
		var origin remoteActivityOrigin
		if len(command.Origin) > 0 {
			// Best-effort: origin is device-written wire data, not
			// attacker-controlled input, so a decode failure just means an
			// older/unrecognised shape rather than something to reject.
			_ = json.Unmarshal(command.Origin, &origin)
		}
		entries = append(entries, models.RemoteActivityEntry{
			CreatedAt:     command.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
			OperationType: command.OperationType,
			OriginKind:    origin.Kind,
			OriginKeyName: origin.KeyName,
			State:         command.State,
			Status:        command.ResultStatus,
			ErrorCode:     command.ErrorCode,
		})
	}

	return models.RemoteActivityResponse{Entries: entries}, nil
}
