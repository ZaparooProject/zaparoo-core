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
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
)

const (
	MappingTypeID         = "id"
	MappingTypeValue      = "value"
	MappingTypeData       = "data"
	MatchTypeExact        = "exact"
	MatchTypePartial      = "partial"
	MatchTypeRegex        = "regex"
	LegacyMappingTypeUID  = "uid"
	LegacyMappingTypeText = "text"
)

var AllowedMappingTypes = []string{
	MappingTypeID,
	MappingTypeValue,
	MappingTypeData,
}

var AllowedMatchTypes = []string{
	MatchTypeExact,
	MatchTypePartial,
	MatchTypeRegex,
}

func NormalizeID(uid string) string {
	uid = strings.TrimSpace(uid)
	uid = strings.ToLower(uid)
	uid = strings.ReplaceAll(uid, ":", "")
	return uid
}

func (db *UserDB) AddMapping(m *database.Mapping) error {
	if !helpers.Contains(AllowedMappingTypes, m.Type) {
		return fmt.Errorf("invalid mapping type: %s", m.Type)
	}

	if !helpers.Contains(AllowedMatchTypes, m.Match) {
		return fmt.Errorf("invalid match type: %s", m.Match)
	}

	if m.Type == MappingTypeID {
		m.Pattern = NormalizeID(m.Pattern)
	}

	if m.Pattern == "" {
		return errors.New("missing pattern")
	}

	if m.Match == MatchTypeRegex {
		_, err := regexp.Compile(m.Pattern)
		if err != nil {
			return fmt.Errorf("invalid regex pattern: %s", m.Pattern)
		}
	}

	m.Added = time.Now().Unix()

	return sqlAddMapping(db.ctx, db.sql, *m)
}

func (db *UserDB) GetMapping(id int64) (database.Mapping, error) {
	return sqlGetMapping(db.ctx, db.sql, id)
}

func (db *UserDB) DeleteMapping(id int64) error {
	return sqlDeleteMapping(db.ctx, db.sql, id)
}

func (db *UserDB) UpdateMapping(id int64, m *database.Mapping) error {
	if !helpers.Contains(AllowedMappingTypes, m.Type) {
		return fmt.Errorf("invalid mapping type: %s", m.Type)
	}

	if !helpers.Contains(AllowedMatchTypes, m.Match) {
		return fmt.Errorf("invalid match type: %s", m.Match)
	}

	if m.Type == MappingTypeID {
		m.Pattern = NormalizeID(m.Pattern)
	}

	if m.Pattern == "" {
		return errors.New("missing pattern")
	}

	if m.Match == MatchTypeRegex {
		_, err := regexp.Compile(m.Pattern)
		if err != nil {
			return fmt.Errorf("invalid regex pattern: %s", m.Pattern)
		}
	}

	return sqlUpdateMapping(db.ctx, db.sql, id, *m)
}

func (db *UserDB) GetAllMappings() ([]database.Mapping, error) {
	return sqlGetAllMappings(db.ctx, db.sql)
}

func (db *UserDB) GetEnabledMappings() ([]database.Mapping, error) {
	return sqlGetEnabledMappings(db.ctx, db.sql)
}

func isCfgRegex(s string) bool {
	return len(s) > 2 && s[0] == '/' && s[len(s)-1] == '/'
}

// MappingsFromConfig converts the file-based mappings loaded into config into
// the database.Mapping shape used for matching and API responses. The returned
// mappings are always enabled and carry no database ID, since they originate
// from the mappings folder rather than UserDB.
func MappingsFromConfig(cfg *config.Instance) []database.Mapping {
	cfgMappings := cfg.Mappings()
	mappings := make([]database.Mapping, 0, len(cfgMappings))

	for _, m := range cfgMappings {
		var dbm database.Mapping
		dbm.Enabled = true
		dbm.Override = m.ZapScript

		switch m.TokenKey {
		case "data":
			dbm.Type = MappingTypeData
		case "value":
			dbm.Type = MappingTypeValue
		default:
			dbm.Type = MappingTypeID
		}

		switch {
		case isCfgRegex(m.MatchPattern):
			dbm.Match = MatchTypeRegex
			dbm.Pattern = m.MatchPattern[1 : len(m.MatchPattern)-1]
		case strings.Contains(m.MatchPattern, "*"):
			// TODO: this behaviour doesn't actually match "partial"
			// the old behaviour will need to be migrated to this one
			dbm.Match = MatchTypePartial
			dbm.Pattern = strings.ReplaceAll(m.MatchPattern, "*", "")
		default:
			dbm.Match = MatchTypeExact
			dbm.Pattern = m.MatchPattern
		}

		mappings = append(mappings, dbm)
	}

	return mappings
}
