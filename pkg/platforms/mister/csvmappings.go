//go:build linux

/*
Zaparoo Core
Copyright (C) 2023, 2024 Callan Barrett

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

package mister

import (
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/fsnotify/fsnotify"
	"github.com/gocarina/gocsv"
	"github.com/rs/zerolog/log"
)

type CsvMappingEntry struct {
	MatchUID  string `csv:"match_uid"`
	MatchText string `csv:"match_text"`
	Text      string `csv:"text"`
}

func LoadCsvMappings() (uidMappings, textMappings map[string]string, err error) {
	uids := make(map[string]string)
	texts := make(map[string]string)

	if _, statErr := os.Stat(config.LegacyMappingsPath); errors.Is(statErr, os.ErrNotExist) {
		log.Debug().Msg("no legacy mappings file found, skipping processing")
		return nil, nil, nil
	}

	f, err := os.Open(config.LegacyMappingsPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open legacy mappings file: %w", err)
	}
	defer func(c io.Closer) {
		_ = c.Close()
	}(f)

	entries := make([]CsvMappingEntry, 0)
	err = gocsv.Unmarshal(f, &entries)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal CSV mappings: %w", err)
	}

	count := 0
	for i, entry := range entries {
		if entry.MatchUID == "" && entry.MatchText == "" {
			log.Warn().Msgf("entry %d has no ID or text, skipping", i+1)
			continue
		}

		if entry.MatchUID != "" {
			uid := strings.TrimSpace(entry.MatchUID)
			uid = strings.ToLower(uid)
			uid = strings.ReplaceAll(uid, ":", "")
			uids[uid] = strings.TrimSpace(entry.Text)
		}

		if entry.MatchText != "" {
			text := strings.TrimSpace(entry.MatchText)
			texts[text] = strings.TrimSpace(entry.Text)
		}

		count++
	}
	log.Info().Msgf("loaded %d entries from database", count)

	return uids, texts, nil
}

func StartCsvMappingsWatcher(
	getLoadTime func() time.Time,
	setMappings func(map[string]string, map[string]string),
) (func() error, error) {
	var closeDbWatcher func() error
	dbWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Error().Msgf("error creating mappings watcher: %s", err)
	} else {
		closeDbWatcher = dbWatcher.Close
	}

	go func() {
		// this turned out to be not trivial to say the least, mostly due to
		// the fact the fsnotify library does not implement the IN_CLOSE_WRITE
		// inotify event, which signals the file has finished being written
		// see: https://github.com/fsnotify/fsnotify/issues/372
		//
		// during a standard write operation, a file may emit multiple write
		// events, including when the file could be half-written
		//
		// it's also the case that editors may delete the file and create a new
		// one, which kills the active watcher
		const delay = 1 * time.Second
		for {
			select {
			case event, ok := <-dbWatcher.Events:
				if !ok {
					return
				}
				if event.Has(fsnotify.Write) {
					// usually receives multiple write events, just act on the first
					if time.Since(getLoadTime()) < delay {
						continue
					}
					time.Sleep(delay)
					log.Info().Msg("database changed, reloading")
					uids, texts, err := LoadCsvMappings()
					if err != nil {
						log.Error().Msgf("error loading database: %s", err)
					} else {
						setMappings(uids, texts)
					}
				} else if event.Has(fsnotify.Remove) {
					// editors may also delete the file on write
					time.Sleep(delay)
					_, err := os.Stat(config.LegacyMappingsPath)
					if err == nil {
						err = dbWatcher.Add(config.LegacyMappingsPath)
						if err != nil {
							log.Error().Msgf("error watching database: %s", err)
						}
						log.Info().Msg("database changed, reloading")
						uids, texts, err := LoadCsvMappings()
						if err != nil {
							log.Error().Msgf("error loading database: %s", err)
						} else {
							setMappings(uids, texts)
						}
					}
				}
			case err, ok := <-dbWatcher.Errors:
				if !ok {
					return
				}
				log.Error().Msgf("watcher error: %s", err)
			}
		}
	}()

	if _, err := os.Stat(config.LegacyMappingsPath); err == nil {
		err = dbWatcher.Add(config.LegacyMappingsPath)
		if err != nil {
			log.Error().Msgf("error watching database: %s", err)
		}
	}

	return closeDbWatcher, nil
}
