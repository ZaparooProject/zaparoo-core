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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/permissions"
)

// ErrForbidden is returned when a client's role does not grant the
// capability a method requires.
var ErrForbidden = errors.New("client role does not permit this method")

// requestGrant builds the permission grant for a request.
func requestGrant(env *requests.RequestEnv) permissions.Grant {
	return permissions.Grant{
		Role:    permissions.Role(env.ClientRole),
		IsLocal: env.IsLocal,
	}
}

// requireCapability returns a client error when the request's grant does
// not include the capability.
func requireCapability(env *requests.RequestEnv, capability permissions.Capability) error {
	if !requestGrant(env).Has(capability) {
		return models.ClientErrf("%w", ErrForbidden)
	}
	return nil
}

// requireProfileManagement permits trusted local UI requests and requires
// the profile-management capability from remote clients. Local profile PIN
// prompts are a UI nuisance barrier, not API authorization.
func requireProfileManagement(env *requests.RequestEnv) error {
	if env.IsLocal || requestGrant(env).Has(permissions.CapProfilesManage) {
		return nil
	}
	return models.ClientErrf("%w", ErrForbidden)
}
