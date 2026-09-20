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

package gamelistxml

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper/mra"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

// arcadeSetStem and readArcadeSetName delegate to the shared MRA reader so the
// gamelist's ZIP-path identities and the arcade catalog scraper agree on which
// descriptors may select a write target.
func arcadeSetStem(sourcePath string) string { return mra.SetStem(sourcePath) }

func readArcadeSetName(fs afero.Fs, filename string) string { return mra.ReadSetName(fs, filename) }

// indexArcadeSets maps each MAME set name referenced by the gamelist to the
// indexed MRA descriptors that declare it. Only set names the gamelist actually
// asks for are kept, so a system with no ROM-style entries costs one pass over
// the parsed games and never touches the filesystem.
func (g *GamelistXMLScraper) indexArcadeSets(
	ctx context.Context, rows []database.MediaWithFullPath, parsed parsedGamelistSystem,
) (map[string][]database.Media, error) {
	bySet := make(map[string][]database.Media)
	if !g.matchArcadeSets {
		return bySet, nil
	}
	wanted := make(map[string]struct{})
	for _, file := range parsed.Files {
		for i := range file.Games {
			if stem := arcadeSetStem(file.Games[i].Path); stem != "" {
				wanted[strings.ToLower(stem)] = struct{}{}
			}
		}
	}
	if len(wanted) == 0 {
		return bySet, nil
	}
	start := time.Now()
	var descriptors, unreadable int
	// Include already-scraped rows: a previous write cannot turn an ambiguous
	// set into a unique match on the next run. Missing rows are not launch targets.
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if row.IsMissing || !strings.EqualFold(filepath.Ext(row.Path), ".mra") {
			continue
		}
		descriptors++
		setName := readArcadeSetName(g.filesystem(), row.Path)
		if setName == "" {
			unreadable++
			continue
		}
		if _, ok := wanted[setName]; !ok {
			continue
		}
		bySet[setName] = append(bySet[setName], database.Media{
			DBID: row.DBID, MediaTitleDBID: row.MediaTitleDBID, Path: row.Path,
		})
	}
	// A silent zero here is indistinguishable from the feature being off, so
	// the read cost and the unusable descriptor count are always reported.
	log.Debug().
		Int("wanted_sets", len(wanted)).
		Int("descriptors_read", descriptors).
		Int("descriptors_unusable", unreadable).
		Int("indexed_sets", len(bySet)).
		Dur("duration", time.Since(start)).
		Msg("gamelistxml: indexed arcade set names")
	return bySet, nil
}

// arcadeMediaForSet reports known even for ambiguous or already-scraped sets so
// callers cannot turn a failed identity lookup into an unsafe slug/filename guess.
func arcadeMediaForSet(indexes loadRecordIndexes, sourcePath string) (media database.Media, known bool) {
	rows := indexes.ArcadeBySetName[strings.ToLower(arcadeSetStem(sourcePath))]
	if len(rows) == 0 {
		return database.Media{}, false
	}
	if len(rows) != 1 {
		log.Warn().Str("path", sourcePath).Int("matches", len(rows)).
			Msg("gamelistxml: ambiguous arcade setname, skipping")
		return database.Media{}, true
	}
	row, ok := indexes.MediaByPathFold[pathFoldKey(rows[0].Path)]
	if !ok || row.DBID != rows[0].DBID {
		return database.Media{}, true
	}
	return row, true
}
