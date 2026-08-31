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
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/userdb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/rs/zerolog/log"
)

func checkMappingUID(m *database.Mapping, t *tokens.Token) bool {
	uid := userdb.NormalizeID(t.UID)
	pattern := userdb.NormalizeID(m.Pattern)

	switch m.Match {
	case userdb.MatchTypeExact:
		log.Trace().Msgf("checking exact match: %s == %s", pattern, uid)
		return uid == pattern
	case userdb.MatchTypePartial:
		log.Trace().Msgf("checking partial match: %s contains %s", pattern, uid)
		return strings.Contains(uid, pattern)
	case userdb.MatchTypeRegex:
		// don't normalize regex pattern
		log.Trace().Msgf("checking regex match: %s matches %s", m.Pattern, uid)
		re, err := helpers.CachedCompile(m.Pattern)
		if err != nil {
			log.Error().Err(err).Msgf("error compiling regex")
			return false
		}
		return re.MatchString(uid)
	}

	return false
}

func checkMappingText(m *database.Mapping, t *tokens.Token) bool {
	switch m.Match {
	case userdb.MatchTypeExact:
		return t.Text == m.Pattern
	case userdb.MatchTypePartial:
		return strings.Contains(t.Text, m.Pattern)
	case userdb.MatchTypeRegex:
		re, err := helpers.CachedCompile(m.Pattern)
		if err != nil {
			log.Error().Err(err).Msgf("error compiling regex")
			return false
		}
		return re.MatchString(t.Text)
	}

	return false
}

func checkMappingData(m *database.Mapping, t *tokens.Token) bool {
	switch m.Match {
	case userdb.MatchTypeExact:
		return t.Data == m.Pattern
	case userdb.MatchTypePartial:
		return strings.Contains(t.Data, m.Pattern)
	case userdb.MatchTypeRegex:
		re, err := helpers.CachedCompile(m.Pattern)
		if err != nil {
			log.Error().Err(err).Msgf("error compiling regex")
			return false
		}
		return re.MatchString(t.Data)
	}

	return false
}

//nolint:gocritic // single-use parameter in service function
func getMapping(
	cfg *config.Instance,
	db *database.Database,
	pl platforms.Platform,
	token tokens.Token,
) (string, bool) {
	// TODO: need a way to identify the source of a match so it can be
	// reported and debugged by the user if there's issues

	// check db mappings
	ms, err := db.UserDB.GetEnabledMappings()
	if err != nil {
		log.Error().Err(err).Msgf("error getting db mappings")
	}

	// load config mappings after
	ms = append(ms, userdb.MappingsFromConfig(cfg)...)

	for _, m := range ms {
		switch m.Type {
		case userdb.MappingTypeID:
			if checkMappingUID(&m, &token) {
				log.Info().Msg("launching with db/cfg id match override")
				return m.Override, true
			}
		case userdb.MappingTypeValue:
			if checkMappingText(&m, &token) {
				log.Info().Msg("launching with db/cfg value match override")
				return m.Override, true
			}
		case userdb.MappingTypeData:
			if checkMappingData(&m, &token) {
				log.Info().Msg("launching with db/cfg data match override")
				return m.Override, true
			}
		}
	}

	// check platform mappings
	return pl.LookupMapping(&token)
}
