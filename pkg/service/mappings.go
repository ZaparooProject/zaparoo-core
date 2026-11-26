/*
Zaparoo Core
Copyright (c) 2026 The Zaparoo Project Contributors.
SPDX-License-Identifier: GPL-3.0-or-later

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

package service

import (
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/userdb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/rs/zerolog/log"
)

// getMapping returns the override for the first mapping the token matches,
// from the user database, the mappings folder or the platform, in that order.
//
//nolint:gocritic // single-use parameter in service function
func getMapping(
	cfg *config.Instance,
	db *database.Database,
	pl platforms.Platform,
	token tokens.Token,
) (string, bool) {
	var udb userdb.EnabledMappingsReader
	if db != nil && db.UserDB != nil {
		udb = db.UserDB
	}
	if match, ok := userdb.FindMapping(udb, cfg, &token); ok {
		log.Info().
			Str("source", match.Source).
			Str("type", match.Mapping.Type).
			Msg("launching with mapping override")
		return match.Mapping.Override, true
	}

	// check platform mappings
	return pl.LookupMapping(&token)
}
