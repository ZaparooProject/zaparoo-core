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
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const (
	maxMetadataBytes   = int64(8 * 1024 * 1024)
	maxMetadataRecords = 100_000
	directoryReadBatch = 256

	gameInfoFileName = "gameinfo.tsv"
	// Synopsis files are named per language, and which languages a pack ships
	// varies by system, so they are globbed rather than probed by name.
	synopsisFilePrefix  = "synopsis_"
	synopsisFileSuffix  = ".tsv"
	defaultSynopsisLang = "en"
)

// artworkRecord is one resolvable entry from a pack. ImagePath is empty for a
// game the pack holds metadata but no image for; those still carry year, genre
// and synopsis so a title can show details without artwork.
type artworkRecord struct {
	Name      string
	Key       string
	ImagePath string
	// SlugUnique is false when another key in the same pack reduces to the
	// same bare title. Falling back to a title match then serves a coin-flip
	// image, which the format treats as worse than serving none.
	SlugUnique bool
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

func loadSourceRecords(ctx context.Context, fs afero.Fs, source sourceDir, langs []string) (sourceRecords, error) {
	switch source.Kind {
	case sourceArtwork:
		return loadArtworkRecords(ctx, fs, source.Path, langs)
	case sourceManuals:
		manuals, err := loadManualRecords(ctx, fs, source.Path)
		return sourceRecords{Manuals: manuals}, err
	default:
		return sourceRecords{}, errors.New("misterdocs: unknown source kind")
	}
}

func loadArtworkRecords(ctx context.Context, fs afero.Fs, dir string, langs []string) (sourceRecords, error) {
	images, err := imageFilesByStem(ctx, fs, dir)
	if err != nil {
		return sourceRecords{}, err
	}
	result := sourceRecords{
		Artwork:  make([]artworkRecord, 0, len(images)),
		GameInfo: make(map[string]gameInfoRecord),
		Synopsis: make(map[string]string),
	}
	if err := loadArtworkIndex(ctx, fs, dir, images, &result); err != nil {
		return sourceRecords{}, err
	}
	// Only index rows may fall back to a bare-title match. Records added
	// after this point resolve by exact name alone, which is all the format
	// promises for an image the index does not mention.
	markUniqueSlugs(result.Artwork)
	loadArtworkMetadata(ctx, fs, dir, langs, &result)
	appendUnindexedRecords(images, &result)
	return result, nil
}

// loadArtworkIndex resolves every catalogued dump to the image that represents
// it. A pack with no index still serves images filed under their own key, so a
// missing index is not an error; appendUnindexedRecords covers those images.
func loadArtworkIndex(
	ctx context.Context,
	fs afero.Fs,
	dir string,
	images map[string]string,
	result *sourceRecords,
) error {
	indexPath := filepath.Join(dir, indexFileName)
	if !isRegularFile(fs, indexPath) {
		return nil
	}
	rows, err := readTSV(ctx, fs, indexPath)
	if err != nil {
		return fmt.Errorf("misterdocs: parse artwork index: %w", err)
	}
	nameCol, nameOK := rows.columns["name"]
	keyCol, keyOK := rows.columns["key"]
	if !nameOK || !keyOK {
		return errors.New("misterdocs: index.tsv requires name and key columns")
	}
	recordsByName := make(map[string]artworkRecord)
	recordOrder := make([]string, 0, len(rows.records))
	ambiguousNames := make(map[string]struct{})
	for _, row := range rows.records {
		if err := ctx.Err(); err != nil {
			return err
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
		if _, ambiguous := ambiguousNames[nameKey]; ambiguous {
			result.RowErrors++
			continue
		}
		if existing, duplicate := recordsByName[nameKey]; duplicate {
			result.RowErrors++
			if !strings.EqualFold(existing.Key, key) {
				// Two keys claim one name, so the row accepted earlier is
				// unusable too and is retracted along with this one.
				delete(recordsByName, nameKey)
				ambiguousNames[nameKey] = struct{}{}
				result.RowErrors++
			}
			continue
		}
		recordsByName[nameKey] = artworkRecord{Name: name, Key: key, ImagePath: imagePath}
		recordOrder = append(recordOrder, nameKey)
	}
	for _, nameKey := range recordOrder {
		if record, ok := recordsByName[nameKey]; ok {
			result.Artwork = append(result.Artwork, record)
		}
	}
	return nil
}

func loadArtworkMetadata(ctx context.Context, fs afero.Fs, dir string, langs []string, result *sourceRecords) {
	gameInfoPath := filepath.Join(dir, gameInfoFileName)
	if isRegularFile(fs, gameInfoPath) {
		parsed, rowErrors, parseErr := loadGameInfo(ctx, fs, gameInfoPath)
		result.RowErrors += rowErrors
		if parseErr != nil {
			result.RowErrors++
			log.Warn().Err(parseErr).Str("dir", dir).Msg("misterdocs: skipped optional game metadata")
		} else {
			result.GameInfo = parsed
		}
	}
	synopsisPath := synopsisFileForLangs(ctx, fs, dir, langs)
	if synopsisPath == "" {
		return
	}
	parsed, rowErrors, parseErr := loadSynopsis(ctx, fs, synopsisPath)
	result.RowErrors += rowErrors
	if parseErr != nil {
		result.RowErrors++
		log.Warn().Err(parseErr).Str("path", synopsisPath).Msg("misterdocs: skipped optional synopsis")
		return
	}
	result.Synopsis = parsed
}

// appendUnindexedRecords adds what the index does not cover. Every image no
// row names is filed under its own key, because a key used as a filename
// resolves whether or not the index mentions it - and it is the only step left
// when a pack ships with no index at all. Every gameinfo key with neither an
// image nor a row becomes a metadata-only record, so a game the pack holds
// details but no artwork for still gets them; the key itself, a catalogue name
// or a setname, is the only handle such a game has. Neither kind is an index
// row, so neither is eligible for the bare-title fallback.
func appendUnindexedRecords(images map[string]string, result *sourceRecords) {
	names := make(map[string]struct{}, len(result.Artwork))
	keys := make(map[string]struct{}, len(result.Artwork))
	for i := range result.Artwork {
		names[strings.ToLower(result.Artwork[i].Name)] = struct{}{}
		keys[strings.ToLower(result.Artwork[i].Key)] = struct{}{}
	}
	stems := make([]string, 0, len(images))
	for stem := range images {
		if _, ok := names[stem]; !ok {
			stems = append(stems, stem)
		}
	}
	sort.Strings(stems)
	for _, stem := range stems {
		path := images[stem]
		name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		result.Artwork = append(result.Artwork, artworkRecord{Name: name, Key: name, ImagePath: path})
		keys[stem] = struct{}{}
	}
	infoKeys := make([]string, 0, len(result.GameInfo))
	for key := range result.GameInfo {
		lowered := strings.ToLower(key)
		if _, ok := keys[lowered]; ok {
			continue
		}
		// A gameinfo key that is already an index name resolves through that
		// row to its representative image and metadata; a second record for
		// it would only re-match the same media.
		if _, ok := names[lowered]; ok {
			continue
		}
		infoKeys = append(infoKeys, key)
	}
	sort.Strings(infoKeys)
	for _, key := range infoKeys {
		result.Artwork = append(result.Artwork, artworkRecord{Name: key, Key: key})
	}
}

// markUniqueSlugs flags the records whose bare title identifies exactly one
// key, which are the only ones allowed to match a title rather than a dump.
func markUniqueSlugs(records []artworkRecord) {
	recordSlugs := make([]string, len(records))
	keysBySlug := make(map[string]map[string]struct{}, len(records))
	for i := range records {
		slug := slugs.Slugify(slugs.MediaTypeGame, records[i].Name)
		recordSlugs[i] = slug
		if slug == "" {
			continue
		}
		if keysBySlug[slug] == nil {
			keysBySlug[slug] = make(map[string]struct{}, 1)
		}
		keysBySlug[slug][strings.ToLower(records[i].Key)] = struct{}{}
	}
	for i := range records {
		records[i].SlugUnique = recordSlugs[i] != "" && len(keysBySlug[recordSlugs[i]]) == 1
	}
}

// synopsisFileForLangs picks a synopsis file by preferred language. A pack
// ships a language file only where the source held text in it, so the set
// varies per system and has to be globbed rather than assumed.
func synopsisFileForLangs(ctx context.Context, fs afero.Fs, dir string, langs []string) string {
	entries, err := afero.ReadDir(fs, dir)
	if err != nil {
		return ""
	}
	byLang := make(map[string]string, len(entries))
	available := make([]string, 0, len(entries))
	for _, entry := range entries {
		if ctx.Err() != nil {
			return ""
		}
		name := strings.ToLower(entry.Name())
		if entry.IsDir() || !strings.HasPrefix(name, synopsisFilePrefix) ||
			!strings.HasSuffix(name, synopsisFileSuffix) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if !isRegularFile(fs, path) {
			continue
		}
		lang := strings.TrimSuffix(strings.TrimPrefix(name, synopsisFilePrefix), synopsisFileSuffix)
		if lang == "" {
			continue
		}
		byLang[lang] = path
		available = append(available, lang)
	}
	if len(available) == 0 {
		return ""
	}
	for _, lang := range append(append([]string{}, langs...), defaultSynopsisLang) {
		lang = strings.ToLower(strings.TrimSpace(lang))
		if path := byLang[lang]; path != "" {
			return path
		}
		if sep := strings.IndexAny(lang, "-_"); sep > 0 {
			if path := byLang[lang[:sep]]; path != "" {
				return path
			}
		}
	}
	sort.Strings(available)
	return byLang[available[0]]
}

// loadGameInfo returns the metadata rows keyed by pack key. rowErrors counts
// rows dropped for repeating a key; the first row for a key is kept.
func loadGameInfo(
	ctx context.Context,
	fs afero.Fs,
	path string,
) (records map[string]gameInfoRecord, rowErrors int, err error) {
	rows, readErr := readTSV(ctx, fs, path)
	if readErr != nil {
		return nil, 0, fmt.Errorf("misterdocs: parse gameinfo: %w", readErr)
	}
	keyCol, ok := rows.columns["key"]
	if !ok {
		return nil, 0, errors.New("misterdocs: gameinfo.tsv requires key column")
	}
	result := make(map[string]gameInfoRecord, len(rows.records))
	for _, row := range rows.records {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, rowErrors, ctxErr
		}
		key := field(row, keyCol)
		if key == "" {
			continue
		}
		if _, duplicate := result[key]; duplicate {
			// Keep the first row. Dropping the whole file over one repeated
			// key would cost every other game its metadata.
			rowErrors++
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
	return result, rowErrors, nil
}

// loadSynopsis returns descriptions keyed by pack key. rowErrors counts rows
// dropped for repeating a key; the first row for a key is kept.
func loadSynopsis(
	ctx context.Context,
	fs afero.Fs,
	path string,
) (records map[string]string, rowErrors int, err error) {
	rows, readErr := readTSV(ctx, fs, path)
	if readErr != nil {
		return nil, 0, fmt.Errorf("misterdocs: parse synopsis: %w", readErr)
	}
	keyCol, keyOK := rows.columns["key"]
	synopsisCol, synopsisOK := rows.columns["synopsis"]
	if !keyOK || !synopsisOK {
		return nil, 0, errors.New("misterdocs: synopsis file requires key and synopsis columns")
	}
	result := make(map[string]string, len(rows.records))
	for _, row := range rows.records {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, rowErrors, ctxErr
		}
		key, synopsis, ok := rowValues(row, keyCol, synopsisCol)
		if !ok || key == "" || synopsis == "" {
			continue
		}
		if _, duplicate := result[key]; duplicate {
			rowErrors++
			continue
		}
		result[key] = synopsis
	}
	return result, rowErrors, nil
}

func loadManualRecords(ctx context.Context, fs afero.Fs, dir string) ([]string, error) {
	directory, err := fs.Open(dir)
	if err != nil {
		return nil, fmt.Errorf("misterdocs: read manuals directory %q: %w", dir, err)
	}
	defer func() { _ = directory.Close() }()

	result := make([]string, 0)
	for {
		entries, readErr := directory.Readdir(directoryReadBatch)
		for _, entry := range entries {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			result, err = appendManualRecord(fs, dir, entry, result)
			if err != nil {
				return nil, err
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("misterdocs: read manuals directory %q: %w", dir, readErr)
		}
	}
	sort.Strings(result)
	return result, nil
}

func appendManualRecord(
	fs afero.Fs,
	dir string,
	entry os.FileInfo,
	result []string,
) ([]string, error) {
	if entry.IsDir() || entry.Mode()&os.ModeSymlink != 0 ||
		!strings.EqualFold(filepath.Ext(entry.Name()), ".pdf") {
		return result, nil
	}
	path := filepath.Join(dir, entry.Name())
	if !isRegularFile(fs, path) {
		return result, nil
	}
	if len(result) >= maxMetadataRecords {
		return nil, fmt.Errorf("misterdocs: manuals directory exceeds %d records", maxMetadataRecords)
	}
	return append(result, path), nil
}

func imageFilesByStem(ctx context.Context, fs afero.Fs, dir string) (map[string]string, error) {
	directory, err := fs.Open(dir)
	if err != nil {
		return nil, fmt.Errorf("misterdocs: read artwork directory %q: %w", dir, err)
	}
	defer func() { _ = directory.Close() }()

	result := make(map[string]string)
	ambiguous := make(map[string]struct{})
	imageCount := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		entries, readErr := directory.Readdir(directoryReadBatch)
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if entry.IsDir() || entry.Mode()&os.ModeSymlink != 0 ||
				!supportedImageExt(filepath.Ext(entry.Name())) {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			if !isRegularFile(fs, path) {
				continue
			}
			if imageCount >= maxMetadataRecords {
				return nil, fmt.Errorf("misterdocs: artwork directory exceeds %d records", maxMetadataRecords)
			}
			imageCount++
			stem := strings.ToLower(strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())))
			if _, duplicate := result[stem]; duplicate {
				delete(result, stem)
				ambiguous[stem] = struct{}{}
				continue
			}
			if _, duplicate := ambiguous[stem]; duplicate {
				continue
			}
			result[stem] = path
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("misterdocs: read artwork directory %q: %w", dir, readErr)
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
