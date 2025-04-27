package database

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	bolt "go.etcd.io/bbolt"
)

const (
	MappingTypeUID   = "uid"
	MappingTypeText  = "text"
	MappingTypeData  = "data"
	MatchTypeExact   = "exact"
	MatchTypePartial = "partial"
	MatchTypeRegex   = "regex"
)

var AllowedMappingTypes = []string{
	MappingTypeUID,
	MappingTypeText,
	MappingTypeData,
}

var AllowedMatchTypes = []string{
	MatchTypeExact,
	MatchTypePartial,
	MatchTypeRegex,
}

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

func mappingKey(id string) []byte {
	return []byte(fmt.Sprintf("mappings:%s", id))
}

func NormalizeID(uid string) string {
	uid = strings.TrimSpace(uid)
	uid = strings.ToLower(uid)
	uid = strings.ReplaceAll(uid, ":", "")
	return uid
}

func (d *Database) AddMapping(m Mapping) error {
	if !utils.Contains(AllowedMappingTypes, m.Type) {
		return fmt.Errorf("invalid mapping type: %s", m.Type)
	}

	if !utils.Contains(AllowedMatchTypes, m.Match) {
		return fmt.Errorf("invalid match type: %s", m.Match)
	}

	if m.Type == MappingTypeUID {
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

	md, err := json.Marshal(m)
	if err != nil {
		return err
	}

	return d.bdb.Update(func(txn *bolt.Tx) error {
		b := txn.Bucket([]byte(BucketMappings))
		id, _ := b.NextSequence()
		return b.Put(mappingKey(strconv.Itoa(int(id))), md)
	})
}

func (d *Database) GetMapping(id string) (Mapping, error) {
	var m Mapping

	err := d.bdb.View(func(txn *bolt.Tx) error {
		b := txn.Bucket([]byte(BucketMappings))

		v := b.Get(mappingKey(id))
		if v == nil {
			return fmt.Errorf("mapping not found: %s", id)
		}

		return json.Unmarshal(v, &m)
	})

	return m, err
}

func (d *Database) DeleteMapping(id string) error {
	return d.bdb.Update(func(txn *bolt.Tx) error {
		b := txn.Bucket([]byte(BucketMappings))
		return b.Delete(mappingKey(id))
	})
}

func (d *Database) UpdateMapping(id string, m Mapping) error {
	if !utils.Contains(AllowedMappingTypes, m.Type) {
		return fmt.Errorf("invalid mapping type: %s", m.Type)
	}

	if !utils.Contains(AllowedMatchTypes, m.Match) {
		return fmt.Errorf("invalid match type: %s", m.Match)
	}

	if m.Type == MappingTypeUID {
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

	md, err := json.Marshal(m)
	if err != nil {
		return err
	}

	return d.bdb.Update(func(txn *bolt.Tx) error {
		b := txn.Bucket([]byte(BucketMappings))
		return b.Put(mappingKey(id), md)
	})
}

func (d *Database) GetAllMappings() ([]Mapping, error) {
	var ms = make([]Mapping, 0)

	err := d.bdb.View(func(txn *bolt.Tx) error {
		b := txn.Bucket([]byte(BucketMappings))

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

func (d *Database) GetEnabledMappings() ([]Mapping, error) {
	ms, err := d.GetAllMappings()
	if err != nil {
		return nil, err
	}

	var enabled = make([]Mapping, 0)
	for _, m := range ms {
		if m.Enabled {
			enabled = append(enabled, m)
		}
	}

	return enabled, nil
}
