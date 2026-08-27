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

package misterdocs

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const (
	maxMetadataBytes   = int64(8 * 1024 * 1024)
	maxMetadataRecords = 100_000
)

type artworkRecord struct {
	Name      string
	Key       string
	ImagePath string
}

type gameInfoRecord struct {
	Name      string
	Year      string
	Genre     string
	Developer string
	Players   string
}

type sourceRecords struct {
	GameInfo  map[string]gameInfoRecord
	Synopsis  map[string]string
	Artwork   []artworkRecord
	Manuals   []string
	RowErrors int
}

func loadSourceRecords(ctx context.Context, fs afero.Fs, source sourceDir) (sourceRecords, error) {
	switch source.Kind {
	case sourceArtwork:
		return loadArtworkRecords(ctx, fs, source.Path)
	case sourceManuals:
		manuals, err := loadManualRecords(ctx, fs, source.Path)
		return sourceRecords{Manuals: manuals}, err
	default:
		return sourceRecords{}, errors.New("misterdocs: unknown source kind")
	}
}

func loadArtworkRecords(ctx context.Context, fs afero.Fs, dir string) (sourceRecords, error) {
	images, err := imageFilesByStem(fs, dir)
	if err != nil {
		return sourceRecords{}, err
	}
	rows, err := readTSV(ctx, fs, filepath.Join(dir, indexFileName))
	if err != nil {
		return sourceRecords{}, fmt.Errorf("misterdocs: parse artwork index: %w", err)
	}
	result := sourceRecords{
		Artwork:  make([]artworkRecord, 0, len(rows.records)),
		GameInfo: make(map[string]gameInfoRecord),
		Synopsis: make(map[string]string),
	}
	nameCol, nameOK := rows.columns["name"]
	keyCol, keyOK := rows.columns["key"]
	if !nameOK || !keyOK {
		return sourceRecords{}, errors.New("misterdocs: index.tsv requires name and key columns")
	}
	seenNames := make(map[string]struct{})
	for _, row := range rows.records {
		if err := ctx.Err(); err != nil {
			return sourceRecords{}, err
		}
		name, key, ok := rowValues(row, nameCol, keyCol)
		if !ok || name == "" || key == "" {
			result.RowErrors++
			continue
		}
		imagePath := images[strings.ToLower(key)]
		if imagePath == "" {
			result.RowErrors++
			continue
		}
		nameKey := strings.ToLower(name)
		if _, duplicate := seenNames[nameKey]; duplicate {
			result.RowErrors++
			continue
		}
		seenNames[nameKey] = struct{}{}
		result.Artwork = append(result.Artwork, artworkRecord{Name: name, Key: key, ImagePath: imagePath})
	}

	if isRegularFile(fs, filepath.Join(dir, "gameinfo.tsv")) {
		parsed, parseErr := loadGameInfo(ctx, fs, filepath.Join(dir, "gameinfo.tsv"))
		if parseErr != nil {
			result.RowErrors++
			log.Warn().Err(parseErr).Str("dir", dir).Msg("misterdocs: skipped optional game metadata")
		} else {
			result.GameInfo = parsed
		}
	}
	if isRegularFile(fs, filepath.Join(dir, "synopsis_en.tsv")) {
		parsed, parseErr := loadSynopsis(ctx, fs, filepath.Join(dir, "synopsis_en.tsv"))
		if parseErr != nil {
			result.RowErrors++
			log.Warn().Err(parseErr).Str("dir", dir).Msg("misterdocs: skipped optional synopsis")
		} else {
			result.Synopsis = parsed
		}
	}
	return result, nil
}

func loadGameInfo(ctx context.Context, fs afero.Fs, path string) (map[string]gameInfoRecord, error) {
	rows, err := readTSV(ctx, fs, path)
	if err != nil {
		return nil, fmt.Errorf("misterdocs: parse gameinfo: %w", err)
	}
	keyCol, ok := rows.columns["key"]
	if !ok {
		return nil, errors.New("misterdocs: gameinfo.tsv requires key column")
	}
	result := make(map[string]gameInfoRecord, len(rows.records))
	for _, row := range rows.records {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		key := field(row, keyCol)
		if key == "" {
			continue
		}
		result[key] = gameInfoRecord{
			Name:      fieldByName(row, rows.columns, "name"),
			Year:      fieldByName(row, rows.columns, "year"),
			Genre:     fieldByName(row, rows.columns, "genre"),
			Developer: fieldByName(row, rows.columns, "developer"),
			Players:   fieldByName(row, rows.columns, "players"),
		}
	}
	return result, nil
}

func loadSynopsis(ctx context.Context, fs afero.Fs, path string) (map[string]string, error) {
	rows, err := readTSV(ctx, fs, path)
	if err != nil {
		return nil, fmt.Errorf("misterdocs: parse synopsis: %w", err)
	}
	keyCol, keyOK := rows.columns["key"]
	synopsisCol, synopsisOK := rows.columns["synopsis"]
	if !keyOK || !synopsisOK {
		return nil, errors.New("misterdocs: synopsis_en.tsv requires key and synopsis columns")
	}
	result := make(map[string]string, len(rows.records))
	for _, row := range rows.records {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		key, synopsis, ok := rowValues(row, keyCol, synopsisCol)
		if ok && key != "" && synopsis != "" {
			result[key] = synopsis
		}
	}
	return result, nil
}

func loadManualRecords(ctx context.Context, fs afero.Fs, dir string) ([]string, error) {
	entries, err := afero.ReadDir(fs, dir)
	if err != nil {
		return nil, fmt.Errorf("misterdocs: read manuals directory %q: %w", dir, err)
	}
	result := make([]string, 0, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(result) >= maxMetadataRecords {
			return nil, fmt.Errorf("misterdocs: manuals directory exceeds %d records", maxMetadataRecords)
		}
		if entry.IsDir() || entry.Mode()&os.ModeSymlink != 0 || !strings.EqualFold(filepath.Ext(entry.Name()), ".pdf") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if isRegularFile(fs, path) {
			result = append(result, path)
		}
	}
	return result, nil
}

func imageFilesByStem(fs afero.Fs, dir string) (map[string]string, error) {
	entries, err := afero.ReadDir(fs, dir)
	if err != nil {
		return nil, fmt.Errorf("misterdocs: read artwork directory %q: %w", dir, err)
	}
	result := make(map[string]string)
	for _, entry := range entries {
		if entry.IsDir() || entry.Mode()&os.ModeSymlink != 0 || !supportedImageExt(filepath.Ext(entry.Name())) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if !isRegularFile(fs, path) {
			continue
		}
		stem := strings.ToLower(strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())))
		if _, exists := result[stem]; !exists {
			result[stem] = path
		}
	}
	return result, nil
}

func supportedImageExt(ext string) bool {
	switch strings.ToLower(ext) {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif":
		return true
	default:
		return false
	}
}

type tsvRows struct {
	columns map[string]int
	records [][]string
}

func readTSV(ctx context.Context, fs afero.Fs, path string) (tsvRows, error) {
	info, err := lstat(fs, path)
	if err != nil {
		return tsvRows{}, fmt.Errorf("inspect metadata %q: %w", path, err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return tsvRows{}, errors.New("metadata path is not a regular file")
	}
	if info.Size() > maxMetadataBytes {
		return tsvRows{}, fmt.Errorf("metadata exceeds %d-byte limit", maxMetadataBytes)
	}
	file, err := fs.Open(path)
	if err != nil {
		return tsvRows{}, fmt.Errorf("open metadata %q: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	reader := csv.NewReader(io.LimitReader(file, maxMetadataBytes+1))
	reader.Comma = '\t'
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true
	header, err := reader.Read()
	if err != nil {
		return tsvRows{}, fmt.Errorf("read metadata header %q: %w", path, err)
	}
	columns := make(map[string]int, len(header))
	for i, value := range header {
		value = strings.TrimPrefix(strings.TrimSpace(value), "#")
		columns[strings.ToLower(value)] = i
	}
	result := tsvRows{columns: columns, records: make([][]string, 0)}
	totalRecords := 0
	for {
		if err := ctx.Err(); err != nil {
			return tsvRows{}, fmt.Errorf("parse metadata %q: %w", path, err)
		}
		record, readErr := reader.Read()
		if errors.Is(readErr, io.EOF) {
			return result, nil
		}
		if readErr != nil {
			return tsvRows{}, fmt.Errorf("read metadata row %q: %w", path, readErr)
		}
		totalRecords++
		if totalRecords > maxMetadataRecords {
			return tsvRows{}, fmt.Errorf("metadata exceeds %d-record limit", maxMetadataRecords)
		}
		valid := true
		for i := range record {
			record[i] = strings.TrimSpace(record[i])
			if !utf8.ValidString(record[i]) {
				valid = false
				break
			}
		}
		if valid {
			result.records = append(result.records, record)
		}
	}
}

func field(row []string, column int) string {
	if column < 0 || column >= len(row) {
		return ""
	}
	return row[column]
}

func fieldByName(row []string, columns map[string]int, name string) string {
	column, ok := columns[name]
	if !ok {
		return ""
	}
	return field(row, column)
}

func rowValues(row []string, columns ...int) (first, second string, ok bool) {
	if len(columns) != 2 {
		return "", "", false
	}
	first, second = field(row, columns[0]), field(row, columns[1])
	return first, second, first != "" || second != ""
}

func normalizePlayers(value string) string {
	maxPlayers := 0
	for _, part := range strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == '-' || r == '/' || r == ';' || r == ' '
	}) {
		players, err := strconv.Atoi(part)
		if err == nil && players > maxPlayers {
			maxPlayers = players
		}
	}
	if maxPlayers == 0 {
		return ""
	}
	return strconv.Itoa(maxPlayers)
}
