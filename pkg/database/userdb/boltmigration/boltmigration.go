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

package boltmigration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/userdb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/rs/zerolog/log"
	bolt "go.etcd.io/bbolt"
)

const (
	BucketMappings  = "mappings"
	MappingTypeUID  = "uid"
	MappingTypeText = "text"
)

type Mapping struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Type     string `json:"type"`
	Match    string `json:"match"`
	Pattern  string `json:"pattern"`
	Override string `json:"override"`
	Added    int64  `json:"added"`
	Enabled  bool   `json:"enabled"`
}

func dbFile(pl platforms.Platform) string {
	return filepath.Join(helpers.DataDir(pl), "tapto.db")
}

func Exists(pl platforms.Platform) bool {
	_, err := os.Stat(dbFile(pl))
	return err == nil
}

type Database struct {
	bdb *bolt.DB
}

func Open(pl platforms.Platform) (*Database, error) {
	db, err := bolt.Open(dbFile(pl), 0o600, &bolt.Options{})
	if err != nil {
		return nil, fmt.Errorf("failed to open bolt database: %w", err)
	}

	return &Database{bdb: db}, nil
}

func (d *Database) Close() error {
	if err := d.bdb.Close(); err != nil {
		return fmt.Errorf("failed to close bolt database: %w", err)
	}
	return nil
}

func (d *Database) GetMappings() ([]Mapping, error) {
	ms := make([]Mapping, 0)

	err := d.bdb.View(func(txn *bolt.Tx) error {
		b := txn.Bucket([]byte(BucketMappings))
		if b == nil {
			return fmt.Errorf("bucket %q does not exist", BucketMappings)
		}

		c := b.Cursor()
		prefix := []byte("mappings:")
		for k, v := c.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, v = c.Next() {
			var m Mapping
			err := json.Unmarshal(v, &m)
			if err != nil {
				return fmt.Errorf("failed to unmarshal mapping data: %w", err)
			}

			ps := strings.Split(string(k), ":")
			if len(ps) != 2 {
				return fmt.Errorf("invalid mapping key: %s", k)
			}

			m.ID = ps[1]

			ms = append(ms, m)
		}

		return nil
	})
	if err != nil {
		return ms, fmt.Errorf("failed to view bolt database: %w", err)
	}

	return ms, nil
}

func MaybeMigrate(pl platforms.Platform, newDB *userdb.UserDB) error {
	if !Exists(pl) {
		return nil
	}

	oldDB, err := Open(pl)
	if err != nil {
		return err
	}
	defer func(oldDB *Database) {
		closeErr := oldDB.Close()
		if closeErr != nil {
			log.Warn().Msgf("error closing old DB: %s", closeErr)
		}
	}(oldDB)

	mappings, err := oldDB.GetMappings()
	if err != nil {
		return err
	}

	var errors int
	for _, oldMapping := range mappings {
		newMapping := database.Mapping{
			Added:    oldMapping.Added,
			Label:    oldMapping.Label,
			Enabled:  oldMapping.Enabled,
			Match:    oldMapping.Match,
			Pattern:  oldMapping.Pattern,
			Override: oldMapping.Override,
		}

		switch oldMapping.Type {
		case MappingTypeText:
			newMapping.Type = userdb.MappingTypeValue
		case MappingTypeUID:
			newMapping.Type = userdb.MappingTypeID
		default:
			newMapping.Type = oldMapping.Type
		}

		addErr := newDB.AddMapping(&newMapping)
		if addErr != nil {
			log.Warn().Msgf("error migrating mapping: %s", addErr)
			errors++
		}
	}

	err = oldDB.Close()
	if err != nil {
		log.Warn().Msgf("error closing old DB: %s", err)
	}

	oldDBPath := dbFile(pl)
	if errors > 0 {
		log.Warn().Msgf("%d errors migrating old mappings", errors)
		err := os.Rename(oldDBPath, oldDBPath+".error")
		if err != nil {
			return fmt.Errorf("failed to rename old database file to .error: %w", err)
		}
	} else {
		log.Info().Msg("successfully migrated old mappings")
		err := os.Rename(oldDBPath, oldDBPath+".migrated")
		if err != nil {
			return fmt.Errorf("failed to rename old database file to .migrated: %w", err)
		}
	}

	return nil
}
