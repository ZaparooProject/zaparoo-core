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
	media   map[string]database.Media
	dirs    map[string]map[string]string
}

// ES ScummVM launch files name a target in their contents. Filenames and engine
// game IDs alone cannot distinguish multiple configured versions of a game.
func (g *GamelistXMLScraper) scummVMMarkerSource(
	sources *scraper.SourceIndex, path string,
) (scraper.MediaSource, bool) {
	if !strings.EqualFold(filepath.Ext(path), ".scummvm") {
		return scraper.MediaSource{}, false
	}
	file, err := g.filesystem().Open(path)
	if err != nil {
		return scraper.MediaSource{}, false
	}
	defer file.Close() //nolint:errcheck // Read-only metadata source.
	const maxTargetSize = 1024
	data, err := io.ReadAll(io.LimitReader(file, maxTargetSize+1))
	if err != nil || len(data) > maxTargetSize {
		return scraper.MediaSource{}, false
	}
	target := strings.TrimSpace(strings.TrimPrefix(string(data), "\xef\xbb\xbf"))
	if target == "" || virtualpath.ContainsControlChar(target) {
		return scraper.MediaSource{}, false
	}
	return sources.ForMedia(virtualpath.CreateVirtualPath(shared.SchemeScummVM, target, ""))
}

func indexSourceMedia(indexes loadRecordIndexes, sources *scraper.SourceIndex) map[string]database.Media {
	byIdentity := make(map[string]database.Media)
	for _, media := range indexes.MediaByPathFold {
		if !sources.HasMedia(media.Path) {
			continue
		}
		key := scraper.VirtualMediaKey(media.Path)
		if _, exists := byIdentity[key]; exists {
			byIdentity[key] = database.Media{}
		} else {
			byIdentity[key] = media
		}
	}
	return byIdentity
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

// Source identities outrank titles: scraper names may differ from ScummVM's
// descriptions, and neither name is sufficient to distinguish configured targets.
func (g *GamelistXMLScraper) matchSourceRecord(
	indexes loadRecordIndexes, sourceRecords *sourceRecordIndex,
	file *parsedGamelistFile, game *esapi.Game, resolved string,
) *GamelistRecord {
	if sourceRecords == nil || len(sourceRecords.media) == 0 {
		return nil
	}
	source, ok := sourceRecords.sources.ForMedia(game.Path)
	if !ok && resolved != "" {
		source, ok = sourceRecords.sources.ForDirectory(resolved)
		if !ok {
			source, ok = g.scummVMMarkerSource(sourceRecords.sources, resolved)
		}
	}
	if !ok {
		return nil
	}
	media := sourceRecords.media[scraper.VirtualMediaKey(source.MediaPath)]
	if media.DBID == 0 {
		return nil
	}
	key := pathFoldKey(media.Path)
	if _, pending := indexes.MediaByPathFold[key]; !pending {
		return nil
	}
	delete(indexes.MediaByPathFold, key)

	root := filepath.Dir(source.Directory)
	// Unlike physical ROMs, distinct virtual targets can have equal directory
	// basenames on different drives. Only their own directory owns fallback art.
	dirs := []map[string]string{sourceRecords.dirs[root]}
	if file.AssetRootPath != "" {
		dirs = append([]map[string]string{sourceRecords.dirs[file.AssetRootPath]}, dirs...)
	}
	return &GamelistRecord{
		Game: *game, SystemRootPath: file.RootPath, ROMRootPath: root, AssetRootPath: file.AssetRootPath,
		SourceDirectory: source.Directory, MediaDirsByRoot: dirs,
		MatchKind: gamelistMatchSource, MatchedTitleDBID: media.MediaTitleDBID, MatchedMediaDBID: media.DBID,
		MediaLevelWriteSafe: true, RequireExistingImage: file.RequireExistingImage,
	}
}
