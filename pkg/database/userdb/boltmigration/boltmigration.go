package boltmigration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/pkg/database/userdb"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/rs/zerolog/log"
	"github.com/wizzomafizzo/mrext/pkg/utils"

	bolt "go.etcd.io/bbolt"
)

const (
	BucketMappings  = "mappings"
	MappingTypeUID  = "uid"
	MappingTypeText = "text"
)

type Mapping struct {
	Id       string `json:"id"`
	Added    int64  `json:"added"`
	Label    string `json:"label"`
	Enabled  bool   `json:"enabled"`
	Type     string `json:"type"`
	Match    string `json:"match"`
	Pattern  string `json:"pattern"`
	Override string `json:"override"`
}

func dbFile(pl platforms.Platform) string {
	return filepath.Join(pl.Settings().DataDir, "tapto.db")
}

func Exists(pl platforms.Platform) bool {
	_, err := os.Stat(dbFile(pl))
	return err == nil
}

type Database struct {
	bdb *bolt.DB
}

func Open(pl platforms.Platform) (*Database, error) {
	db, err := bolt.Open(dbFile(pl), 0600, &bolt.Options{})
	if err != nil {
		return nil, err
	}

	return &Database{bdb: db}, nil
}

func (d *Database) Close() error {
	return d.bdb.Close()
}

func (d *Database) GetMappings() ([]Mapping, error) {
	var ms = make([]Mapping, 0)

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
				return err
			}

			ps := strings.Split(string(k), ":")
			if len(ps) != 2 {
				return fmt.Errorf("invalid mapping key: %s", k)
			}

			m.Id = ps[1]

			ms = append(ms, m)
		}

		return nil
	})

	return ms, err
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
		err := oldDB.Close()
		if err != nil {
			log.Warn().Msgf("error closing old DB: %s", err)
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

		if oldMapping.Type == MappingTypeText {
			newMapping.Type = userdb.MappingTypeValue
		} else if oldMapping.Type == MappingTypeUID {
			newMapping.Type = userdb.MappingTypeUID
		} else {
			newMapping.Type = oldMapping.Type
		}

		err := newDB.AddMapping(newMapping)
		if err != nil {
			log.Warn().Msgf("error migrating mapping: %s", err)
			errors++
		}
	}

	dbFile := dbFile(pl)
	if errors > 0 {
		log.Warn().Msgf("%d errors migrating old mappings", errors)
		err := utils.MoveFile(dbFile, dbFile+".error")
		if err != nil {
			return err
		}
	} else {
		log.Info().Msg("successfully migrated old mappings")
		err := utils.MoveFile(dbFile, dbFile+".migrated")
		if err != nil {
			return err
		}
	}

	return nil
}
