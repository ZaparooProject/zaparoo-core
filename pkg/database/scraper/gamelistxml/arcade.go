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
	"encoding/xml"
	"errors"
	"io"
	"path"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
	"golang.org/x/net/html/charset"
)

const maxArcadeMRABytes = 256 * 1024

// arcadeSetStem treats ZIP paths as identities, never as files to open. Source
// bundles may contain absolute paths from a different machine, including Windows.
func arcadeSetStem(sourcePath string) string {
	if strings.ContainsAny(sourcePath, "\x00\r\n") || strings.Contains(sourcePath, "://") {
		return ""
	}
	base := path.Base(strings.ReplaceAll(strings.TrimSpace(sourcePath), `\`, "/"))
	ext := path.Ext(base)
	switch strings.ToLower(ext) {
	case ".zip", ".7z":
		base = strings.TrimSuffix(base, ext)
	case "":
	default:
		return ""
	}
	if base == "" || len(base) > 128 {
		return ""
	}
	for _, c := range base {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_', c == '-':
		default:
			return ""
		}
	}
	return base
}

// readArcadeSetName requires one complete, bounded descriptor. Unlike optional
// docs artwork lookup, this identity can select a different title's write target,
// so malformed documents and repeated setname elements must not select a row.
func readArcadeSetName(fs afero.Fs, filename string) string {
	info, err := fs.Stat(filename)
	if lstater, ok := fs.(afero.Lstater); ok {
		info, _, err = lstater.LstatIfPossible(filename)
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxArcadeMRABytes {
		return ""
	}
	file, err := fs.Open(filename)
	if err != nil {
		return ""
	}
	defer func() { _ = file.Close() }()
	limited := &io.LimitedReader{R: file, N: maxArcadeMRABytes + 1}
	decoder := xml.NewDecoder(limited)
	decoder.CharsetReader = charset.NewReaderLabel
	var mra struct {
		XMLName  xml.Name `xml:"misterromdescription"`
		SetNames []string `xml:"setname"`
	}
	if err := decoder.Decode(&mra); err != nil || len(mra.SetNames) != 1 {
		return ""
	}
	// Decode only consumes one element; reject additional documents or garbage.
	for {
		token, tokenErr := decoder.Token()
		if errors.Is(tokenErr, io.EOF) {
			break
		}
		if tokenErr != nil {
			return ""
		}
		switch data := token.(type) {
		case xml.CharData:
			if strings.TrimSpace(string(data)) != "" {
				return ""
			}
		case xml.Comment, xml.ProcInst:
		default:
			return ""
		}
	}
	if limited.N == 0 {
		return ""
	}
	setName := strings.TrimSpace(mra.SetNames[0])
	if arcadeSetStem(setName) != setName {
		return ""
	}
	return strings.ToLower(setName)
}

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
	// Include already-scraped rows: a previous write cannot turn an ambiguous
	// set into a unique match on the next run. Missing rows are not launch targets.
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if row.IsMissing || !strings.EqualFold(filepath.Ext(row.Path), ".mra") {
			continue
		}
		setName := readArcadeSetName(g.filesystem(), row.Path)
		if _, ok := wanted[setName]; !ok {
			continue
		}
		bySet[setName] = append(bySet[setName], database.Media{
			DBID: row.DBID, MediaTitleDBID: row.MediaTitleDBID, Path: row.Path,
		})
	}
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
