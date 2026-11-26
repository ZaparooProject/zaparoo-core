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

package userdb

import (
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/rs/zerolog/log"
)

// Where a mapping is kept: the user database, which the API can edit, or a
// file in the mappings folder, which it cannot.
const (
	MappingSourceDatabase = "database"
	MappingSourceFile     = "file"
)

// MappingMatch is the mapping a token matched and where it is kept.
type MappingMatch struct {
	Source  string
	Mapping database.Mapping
}

// EnabledMappingsReader is the part of the user database FindMapping reads.
type EnabledMappingsReader interface {
	GetEnabledMappings() ([]database.Mapping, error)
}

// FindMapping returns the first mapping the token matches: enabled database
// mappings first, then the mappings folder. Platform mappings are the
// platform's own and are not checked here.
func FindMapping(udb EnabledMappingsReader, cfg *config.Instance, t *tokens.Token) (MappingMatch, bool) {
	var ms []database.Mapping
	if udb != nil {
		dbMappings, err := udb.GetEnabledMappings()
		if err != nil {
			log.Error().Err(err).Msgf("error getting db mappings")
		}
		ms = dbMappings
	}
	for i := range ms {
		if MappingMatches(&ms[i], t) {
			return MappingMatch{Source: MappingSourceDatabase, Mapping: ms[i]}, true
		}
	}
	if cfg != nil {
		fileMappings := MappingsFromConfig(cfg)
		for i := range fileMappings {
			if MappingMatches(&fileMappings[i], t) {
				return MappingMatch{Source: MappingSourceFile, Mapping: fileMappings[i]}, true
			}
		}
	}
	return MappingMatch{}, false
}

// MappingMatches reports whether a mapping's pattern matches the part of the
// token its type names.
func MappingMatches(m *database.Mapping, t *tokens.Token) bool {
	switch m.Type {
	case MappingTypeID:
		return MatchesID(m, t)
	case MappingTypeValue:
		return MatchesValue(m, t)
	case MappingTypeData:
		return MatchesData(m, t)
	}
	return false
}

// MatchesID matches a mapping's pattern against the token's UID, both
// normalized, except a regex pattern, which is used as written.
func MatchesID(m *database.Mapping, t *tokens.Token) bool {
	uid := NormalizeID(t.UID)
	pattern := NormalizeID(m.Pattern)

	switch m.Match {
	case MatchTypeExact:
		log.Trace().Msgf("checking exact match: %s == %s", pattern, uid)
		return uid == pattern
	case MatchTypePartial:
		log.Trace().Msgf("checking partial match: %s contains %s", pattern, uid)
		return strings.Contains(uid, pattern)
	case MatchTypeRegex:
		log.Trace().Msgf("checking regex match: %s matches %s", m.Pattern, uid)
		return matchesRegex(m.Pattern, uid)
	}

	return false
}

// MatchesValue matches a mapping's pattern against the token's text.
func MatchesValue(m *database.Mapping, t *tokens.Token) bool {
	return matchesString(m, t.Text)
}

// MatchesData matches a mapping's pattern against the token's data.
func MatchesData(m *database.Mapping, t *tokens.Token) bool {
	return matchesString(m, t.Data)
}

func matchesString(m *database.Mapping, s string) bool {
	switch m.Match {
	case MatchTypeExact:
		return s == m.Pattern
	case MatchTypePartial:
		return strings.Contains(s, m.Pattern)
	case MatchTypeRegex:
		return matchesRegex(m.Pattern, s)
	}
	return false
}

func matchesRegex(pattern, s string) bool {
	re, err := helpers.CachedCompile(pattern)
	if err != nil {
		log.Error().Err(err).Msgf("error compiling regex")
		return false
	}
	return re.MatchString(s)
}
