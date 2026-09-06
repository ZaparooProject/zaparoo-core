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
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
	"golang.org/x/net/html/charset"
)

// maxArcadeMRAHeaderBytes bounds the descriptor header a set name may be read
// from. It is not a file size limit: MiSTer MRAs embed their ROM payload as
// base64 <part> data and run to tens of megabytes, and refusing those would
// leave real arcade games with no identity at all.
const maxArcadeMRAHeaderBytes = 256 * 1024

const (
	arcadeMRARootElement    = "misterromdescription"
	arcadeMRASetNameElement = "setname"
	// arcadeMRAPayloadElement opens the embedded ROM data. Everything that
	// identifies the set is written before it.
	arcadeMRAPayloadElement = "rom"
)

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

// readArcadeSetName requires one complete, bounded descriptor header. Unlike
// optional docs artwork lookup, this identity can select a different title's
// write target, so malformed documents and repeated setname elements must not
// select a row.
func readArcadeSetName(fs afero.Fs, filename string) string {
	// Lstat, not Stat: Arcade Organizer aliases the same descriptor under its
	// category folders, and an alias that reached the index would read as a
	// second row for one set and block it as ambiguous. Skipping links keeps
	// the canonical `_Arcade` row as the only identity source.
	var info os.FileInfo
	var err error
	if lstater, ok := fs.(afero.Lstater); ok {
		info, _, err = lstater.LstatIfPossible(filename)
	} else {
		info, err = fs.Stat(filename)
	}
	if err != nil || !info.Mode().IsRegular() {
		return ""
	}
	file, err := fs.Open(filename)
	if err != nil {
		return ""
	}
	defer func() { _ = file.Close() }()
	decoder := xml.NewDecoder(&io.LimitedReader{R: file, N: maxArcadeMRAHeaderBytes})
	decoder.CharsetReader = charset.NewReaderLabel
	setName, ok := decodeArcadeSetName(decoder)
	if !ok || arcadeSetStem(setName) != setName {
		return ""
	}
	return strings.ToLower(setName)
}

// decodeArcadeSetName returns the descriptor's single <setname>. Every element
// ahead of the ROM payload is examined, so a repeated, nested or absent set
// name still yields nothing; parsing then stops rather than reading megabytes
// of base64 that can carry no identity. A header that is malformed, outruns the
// read bound, names another root, or is trailed by a second document is not an
// identity source.
func decodeArcadeSetName(decoder *xml.Decoder) (setName string, ok bool) {
	var setNames, depth int
	var rootClosed bool
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return setName, rootClosed && setNames == 1
		}
		if err != nil {
			return "", false
		}
		switch data := token.(type) {
		case xml.StartElement:
			if rootClosed {
				return "", false
			}
			depth++
			switch {
			case depth == 1:
				if data.Name.Local != arcadeMRARootElement {
					return "", false
				}
			case depth == 2 && data.Name.Local == arcadeMRAPayloadElement:
				return setName, setNames == 1
			case depth == 2 && data.Name.Local == arcadeMRASetNameElement:
				setNames++
				if err := decoder.DecodeElement(&setName, &data); err != nil {
					return "", false
				}
				setName = strings.TrimSpace(setName)
				depth--
			default:
				if err := decoder.Skip(); err != nil {
					return "", false
				}
				depth--
			}
		case xml.EndElement:
			depth--
			if depth == 0 {
				rootClosed = true
			}
		case xml.CharData:
			if depth == 0 && strings.TrimSpace(string(data)) != "" {
				return "", false
			}
		case xml.Comment, xml.ProcInst:
		case xml.Directive:
			// Tolerated only ahead of the document it declares.
			if rootClosed {
				return "", false
			}
		default:
			return "", false
		}
	}
}

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
