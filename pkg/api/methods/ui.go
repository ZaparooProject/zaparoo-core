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
)

var errUIServiceUnavailable = errors.New("UI event service is unavailable")

//nolint:gocritic // single-use parameter in API handler
func HandleUI(env requests.RequestEnv) (any, error) {
	if env.UI == nil {
		return nil, errUIServiceUnavailable
	}
	return env.UI.State(), nil
}

//nolint:gocritic // single-use parameter in API handler
func HandleUIRespond(env requests.RequestEnv) (any, error) {
	if env.UI == nil {
		return nil, errUIServiceUnavailable
	}

	var params models.UIRespondParams
	if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
		return nil, models.ClientErrf("invalid params: %w", err)
	}
	if err := env.UI.Respond(params.ID, params.Action, params.ChoiceID); err != nil {
		return nil, models.ClientErrf("UI response failed: %w", err)
	}
	return NoContent{}, nil
}
