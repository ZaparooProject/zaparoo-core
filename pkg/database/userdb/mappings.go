package userdb

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
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

func (db *UserDB) AddMapping(m database.Mapping) error {
	if !utils.Contains(AllowedMappingTypes, m.Type) {
		return fmt.Errorf("invalid mapping type: %s", m.Type)
	}

	if !utils.Contains(AllowedMatchTypes, m.Match) {
		return fmt.Errorf("invalid match type: %s", m.Match)
	}

	if m.Type == MappingTypeID {
		m.Pattern = NormalizeID(m.Pattern)
	}

	if m.Pattern == "" {
		return fmt.Errorf("missing pattern")
	}

	if m.Match == MatchTypeRegex {
		_, err := regexp.Compile(m.Pattern)
		if err != nil {
			return fmt.Errorf("invalid regex pattern: %s", m.Pattern)
		}
	}

	m.Added = time.Now().Unix()

	return sqlAddMapping(db.sql, m)
}

func (db *UserDB) GetMapping(id int64) (database.Mapping, error) {
	return sqlGetMapping(db.sql, id)
}

func (db *UserDB) DeleteMapping(id int64) error {
	return sqlDeleteMapping(db.sql, id)
}

func (db *UserDB) UpdateMapping(id int64, m database.Mapping) error {
	if !utils.Contains(AllowedMappingTypes, m.Type) {
		return fmt.Errorf("invalid mapping type: %s", m.Type)
	}

	if !utils.Contains(AllowedMatchTypes, m.Match) {
		return fmt.Errorf("invalid match type: %s", m.Match)
	}

	if m.Type == MappingTypeID {
		m.Pattern = NormalizeID(m.Pattern)
	}

	if m.Pattern == "" {
		return fmt.Errorf("missing pattern")
	}

	if m.Match == MatchTypeRegex {
		_, err := regexp.Compile(m.Pattern)
		if err != nil {
			return fmt.Errorf("invalid regex pattern: %s", m.Pattern)
		}
	}

	return sqlUpdateMapping(db.sql, id, m)
}

func (db *UserDB) GetAllMappings() ([]database.Mapping, error) {
	return sqlGetAllMappings(db.sql)
}

func (db *UserDB) GetEnabledMappings() ([]database.Mapping, error) {
	return sqlGetEnabledMappings(db.sql)
}
