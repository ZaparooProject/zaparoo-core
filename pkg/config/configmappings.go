package config

import (
	"os"
	"path/filepath"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
	"github.com/rs/zerolog/log"
)

type MappingsEntry struct {
	TokenKey     string `toml:"token_key,omitempty"`
	MatchPattern string `toml:"match_pattern"`
	ZapScript    string `toml:"zapscript"`
}

type Mappings struct {
	Entry []MappingsEntry `toml:"entry,omitempty"`
}

func (c *Instance) LoadMappings(mappingsDir string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	_, err := os.Stat(mappingsDir)
	if err != nil {
		return err
	}

	var mapFiles []string

	err = filepath.WalkDir(
		mappingsDir,
		func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}

			if d.IsDir() {
				return nil
			}

			if strings.ToLower(filepath.Ext(d.Name())) != ".toml" {
				return nil
			}

			mapFiles = append(mapFiles, path)

			return nil
		},
	)
	if err != nil {
		return err
	}
	log.Info().Msgf("found %d mapping files", len(mapFiles))

	filesCounts := 0
	mappingsCount := 0

	for _, mapPath := range mapFiles {
		log.Debug().Msgf("loading mapping file: %s", mapPath)

		//nolint:gosec // Safe: reads mapping config files from controlled application directories
		data, err := os.ReadFile(mapPath)
		if err != nil {
			log.Error().Msgf("error reading mapping file: %s", mapPath)
			continue
		}

		var newVals Values
		err = toml.Unmarshal(data, &newVals)
		if err != nil {
			log.Error().Msgf("error parsing mapping file: %s", mapPath)
			continue
		}

		c.vals.Mappings.Entry = append(c.vals.Mappings.Entry, newVals.Mappings.Entry...)

		filesCounts++
		mappingsCount += len(newVals.Mappings.Entry)
	}

	log.Info().Msgf("loaded %d mapping files, %d mappings", filesCounts, mappingsCount)

	return nil
}

func (c *Instance) Mappings() []MappingsEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Mappings.Entry
}
