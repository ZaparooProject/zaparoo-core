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
	"errors"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/validation"
	"github.com/rs/zerolog/log"
)

// ErrLocalhostOnly is returned when a non-localhost client invokes a method
// that is restricted to local administration only.
var ErrLocalhostOnly = errors.New("method is only available from localhost")

// HandleClients lists paired clients (localhost-only).
//
//nolint:gocritic // single-use parameter in API handler
func HandleClients(env requests.RequestEnv) (any, error) {
	if !env.IsLocal {
		return nil, models.ClientErrf("%w", ErrLocalhostOnly)
	}

	log.Info().Msg("received clients list request")

	clients, err := env.Database.UserDB.ListClients()
	if err != nil {
		log.Error().Err(err).Msg("error listing paired clients")
		return nil, errors.New("error listing paired clients")
	}

	resp := models.ClientsResponse{
		Clients: make([]models.PairedClient, len(clients)),
	}
	for i, c := range clients {
		resp.Clients[i] = models.PairedClient{
			ClientID:   c.ClientID,
			ClientName: c.ClientName,
			CreatedAt:  c.CreatedAt,
			LastSeenAt: c.LastSeenAt,
		}
	}
	return resp, nil
}

// HandleClientsDelete revokes a paired client (localhost-only, forward-only;
// existing sessions survive until disconnect).
//
//nolint:gocritic // single-use parameter in API handler
func HandleClientsDelete(env requests.RequestEnv) (any, error) {
	if !env.IsLocal {
		return nil, models.ClientErrf("%w", ErrLocalhostOnly)
	}

	log.Info().Msg("received clients delete request")

	var params models.ClientsDeleteParams
	if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
		log.Warn().Err(err).Msg("invalid params")
		return nil, models.ClientErrf("invalid params: %w", err)
	}
	if params.ClientID == "" {
		return nil, models.ClientErrf("clientId is required")
	}

	if err := env.Database.UserDB.DeleteClient(params.ClientID); err != nil {
		return nil, models.ClientErrf("failed to delete client: %w", err)
	}
	return NoContent{}, nil
}
