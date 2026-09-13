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
	"io"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esapi"
)

type sourceRecordIndex struct {
	sources *scraper.SourceIndex
	dirs    map[string]map[string]string
}

// ES ScummVM launch files name a target in their contents. This is an input
// format adapter only: target ownership comes from centrally indexed sources.
func (g *GamelistXMLScraper) scummVMMarkerSource(
	sources *scraper.SourceIndex, path string,
) (database.MediaSource, bool) {
	if !strings.EqualFold(filepath.Ext(path), ".scummvm") {
		return database.MediaSource{}, false
	}
	file, err := g.filesystem().Open(path)
	if err != nil {
		return database.MediaSource{}, false
	}
	defer file.Close() //nolint:errcheck // Read-only metadata source.
	const maxTargetSize = 1024
	data, err := io.ReadAll(io.LimitReader(file, maxTargetSize+1))
	if err != nil || len(data) > maxTargetSize {
		return database.MediaSource{}, false
	}
	target := strings.TrimSpace(strings.TrimPrefix(string(data), "\xef\xbb\xbf"))
	if target == "" || virtualpath.ContainsControlChar(target) {
		return database.MediaSource{}, false
	}
	return sources.ForMedia(virtualpath.CreateVirtualPath(shared.SchemeScummVM, target, ""))
}

func withoutSourceTitleMatches(
	byTitle map[int64][]database.Media, sources *scraper.SourceIndex,
) map[int64][]database.Media {
	filtered := make(map[int64][]database.Media, len(byTitle))
	for titleID, rows := range byTitle {
		for _, media := range rows {
			if !sources.HasMedia(media.Path) {
				filtered[titleID] = append(filtered[titleID], media)
			}
		}
	}
	return filtered
}

// Source identities outrank titles: scraper names may differ from launcher
// display names, and neither name safely distinguishes configured targets.
func (g *GamelistXMLScraper) matchSourceRecord(
	indexes loadRecordIndexes, sourceRecords *sourceRecordIndex,
	file *parsedGamelistFile, game *esapi.Game, resolved string,
) *GamelistRecord {
	if sourceRecords == nil {
		return nil
	}
	source, ok := sourceRecords.sources.ForMedia(game.Path)
	if !ok && resolved != "" {
		source, ok = sourceRecords.sources.ForPath(resolved)
		if !ok {
			source, ok = g.scummVMMarkerSource(sourceRecords.sources, resolved)
		}
	}
	if !ok {
		return nil
	}
	key := pathFoldKey(source.MediaPath)
	media, exists := indexes.MediaByPathFold[key]
	if !exists || media.DBID != source.MediaDBID {
		return nil
	}
	delete(indexes.MediaByPathFold, key)

	dirs := []map[string]string{sourceRecords.dirs[source.SourceRoot]}
	if file.AssetRootPath != "" {
		dirs = append([]map[string]string{sourceRecords.dirs[file.AssetRootPath]}, dirs...)
	}
	return &GamelistRecord{
		Game: *game, SystemRootPath: file.RootPath, ROMRootPath: source.SourceRoot,
		AssetRootPath: file.AssetRootPath, SourceDirectory: source.SourcePath, MediaDirsByRoot: dirs,
		MatchKind: gamelistMatchSource, MatchedTitleDBID: media.MediaTitleDBID, MatchedMediaDBID: media.DBID,
		MediaLevelWriteSafe: true, RequireExistingImage: file.RequireExistingImage,
	}
}
