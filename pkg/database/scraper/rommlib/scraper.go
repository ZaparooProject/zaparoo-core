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

// Package rommlib imports game metadata and artwork from a RomM server into
// the media database. Games indexed from RomM carry their ROM ID in their
// path; a game the device already has is matched to RomM's copy by file name.
package rommlib

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper/ssgenre"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/bgpriority"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/romm"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const (
	// ScraperID names this scraper in media.scrape and its status.
	ScraperID   = "romm"
	scraperName = "RomM"
	// fingerprintVersion is bumped when a change to matching or writes means
	// an unchanged system has to be scraped again.
	fingerprintVersion = 1
	// imageMaxBytes bounds one downloaded image.
	imageMaxBytes = 16 << 20
	// maxDescriptionLength bounds a stored description.
	maxDescriptionLength = 4000
)

// freeSpaceFloor is the free space an image download must leave on the image
// folder's filesystem. A variable so tests can lower it.
var freeSpaceFloor = uint64(config.MinFreeDiskBytes)

// imageTypes are the image formats kept, by sniffed content type.
var imageTypes = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/webp": ".webp",
	"image/gif":  ".gif",
}

// NewPlatformScraper returns the RomM scraper. It runs after every index
// while a RomM server is configured, and can be run by hand otherwise, when
// it reports that no server is set.
func NewPlatformScraper(cfg *config.Instance) platforms.Scraper {
	return newScraper(cfg, Dir, helpers.FreeDiskSpace)
}

// Dir is the folder artwork is stored in: the romm folder of the configured
// image folder.
func Dir(cfg *config.Instance, pl platforms.Platform) string {
	dataDir := helpers.DataDir(pl)
	if cfg == nil {
		return filepath.Join(config.ResolveImageDir("", dataDir), config.ImageRomMDir)
	}
	return filepath.Join(cfg.ImageDir(dataDir), config.ImageRomMDir)
}

func newScraper(
	cfg *config.Instance,
	dir func(*config.Instance, platforms.Platform) string,
	freeSpace func(string) (uint64, error),
) platforms.Scraper {
	return platforms.Scraper{
		ID:                     ScraperID,
		Name:                   scraperName,
		SupportsFillMissing:    true,
		AutoScrapeAllLaunchers: romm.ServerURL(cfg) != "",
		AutoScrapeOrder:        scraper.AutoOrderLibraryServer,
		Scrape: func(
			ctx context.Context, cfg *config.Instance, pl platforms.Platform, fs afero.Fs,
			db *database.Database, opts scraper.ScrapeOptions, _ platforms.ScraperCustomOptions,
			ch chan<- scraper.ScrapeUpdate,
		) error {
			if opts.FillMissing && opts.Force {
				return errors.New("romm: fill-missing and force are mutually exclusive")
			}
			if db == nil || db.MediaDB == nil {
				return errors.New("romm: media database is required")
			}
			c, err := romm.ClientFromConfig(cfg)
			if err != nil {
				return fmt.Errorf("romm: %w", err)
			}
			if fs == nil {
				fs = afero.NewOsFs()
			}
			r := &run{
				db: db.MediaDB, client: c, fs: fs, opts: opts, freeSpace: freeSpace,
				root: dir(cfg, pl), server: serverKey(c.BaseURL()),
			}
			go r.loop(ctx, ch)
			return nil
		},
	}
}

// serverKey names a server's artwork folder. ROM IDs are only unique on one
// server, so artwork from two servers must not share a folder.
func serverKey(baseURL string) string {
	return strconv.FormatUint(scraper.NameDigest(baseURL), 16)
}

type run struct {
	db        database.MediaDBI
	fs        afero.Fs
	client    *romm.Client
	freeSpace func(string) (uint64, error)
	unmapped  scraper.UnmappedValues
	root      string
	server    string
	opts      scraper.ScrapeOptions
	// lowSpace stops artwork downloads for the rest of the run once the
	// image folder's filesystem is short of space. Text metadata still lands.
	lowSpace bool
}

type stats struct {
	Processed int
	Matched   int
	Skipped   int
}

func (r *run) loop(ctx context.Context, ch chan<- scraper.ScrapeUpdate) {
	defer close(ch)
	defer r.unmapped.LogSummary(ScraperID)
	bgpriority.Apply()

	if r.opts.Scope != nil {
		r.scrapeScope(ctx, ch)
		return
	}
	started := time.Now()
	systems, err := romm.ServerSystems(ctx, r.client)
	if err != nil {
		r.finishWithError(ctx, err, "", stats{}, 1, 1, ch)
		return
	}
	if wanted := r.opts.SystemIDs(); len(wanted) > 0 {
		systems = slices.DeleteFunc(systems, func(id string) bool {
			return !slices.ContainsFunc(wanted, func(w string) bool { return strings.EqualFold(w, id) })
		})
	}
	total := stats{}
	for i, systemID := range systems {
		if err := scraper.Wait(ctx, r.opts, ScraperID); err != nil {
			break
		}
		got, sysErr := r.scrapeSystem(ctx, systemID, i+1, len(systems), ch)
		total.Processed += got.Processed
		total.Matched += got.Matched
		total.Skipped += got.Skipped
		if sysErr != nil {
			r.finishWithError(ctx, sysErr, systemID, total, len(systems), i+1, ch)
			return
		}
	}
	log.Debug().Int("processed", total.Processed).Int("matched", total.Matched).
		Dur("total", time.Since(started)).Msg("romm: scrape complete")
	steps := max(len(systems), 1)
	ch <- scraper.ScrapeUpdate{
		Done: true, Processed: total.Processed, Total: total.Processed,
		Matched: total.Matched, Skipped: total.Skipped, TotalSteps: steps, CurrentStep: steps,
	}
}

// finishWithError ends a run. A server that cannot be reached or refuses
// the stored token ends it with only a log line: indexing queues this scraper
// whenever a server is configured, and a server that is off is not a fault in
// Core, nor an unreadable metadata file to raise in the inbox.
func (r *run) finishWithError(
	ctx context.Context, err error, systemID string, total stats, steps, step int,
	ch chan<- scraper.ScrapeUpdate,
) {
	update := scraper.ScrapeUpdate{
		Done: true, SystemID: systemID, Processed: total.Processed, Total: total.Processed,
		Matched: total.Matched, Skipped: total.Skipped, TotalSteps: steps, CurrentStep: step,
	}
	switch {
	case ctx.Err() != nil:
	case sourceUnavailable(err):
		log.Warn().Err(err).Str("server", r.client.BaseURL()).Msg("romm: server unavailable, scrape stopped")
	default:
		update.FatalErr = fmt.Errorf("romm: %w", err)
	}
	ch <- update
}

func sourceUnavailable(err error) bool {
	return romm.IsUnreachable(err) || errors.Is(err, romm.ErrUnauthorized) ||
		errors.Is(err, context.DeadlineExceeded)
}

// sourceState describes a system's ROMs on the server: which ones there are
// and when each last changed.
func sourceState(server string, roms []romm.Rom) string {
	var sum uint64
	for i := range roms {
		sum += scraper.NameDigest(strconv.Itoa(roms[i].ID) + "@" + roms[i].UpdatedAt)
	}
	return fmt.Sprintf("server=%s roms=%d digest=%x", server, len(roms), sum)
}

func (r *run) scrapeSystem(
	ctx context.Context, systemID string, step, steps int, ch chan<- scraper.ScrapeUpdate,
) (stats, error) {
	roms, err := romm.SystemRoms(ctx, r.client, systemID)
	if err != nil {
		return stats{}, fmt.Errorf("list romm roms for %s: %w", systemID, err)
	}
	state := sourceState(r.server, roms)
	if r.opts.FillMissing &&
		scraper.SystemUnchanged(ctx, r.db, ScraperID, systemID, fingerprintVersion, state) {
		log.Debug().Str("system", systemID).Msg("romm: server and library unchanged, system skipped")
		ch <- scraper.ScrapeUpdate{SystemID: systemID, TotalSteps: steps, CurrentStep: step}
		return stats{}, nil
	}

	media, err := r.db.GetMediaBySystemID(systemID)
	if err != nil {
		return stats{}, fmt.Errorf("load media: %w", err)
	}
	result := stats{}
	if len(media) > 0 {
		system, sysErr := r.db.FindSystemBySystemID(systemID)
		if sysErr != nil {
			return stats{}, fmt.Errorf("find system: %w", sysErr)
		}
		done, doneErr := r.completed(ctx, system.DBID)
		if doneErr != nil {
			return stats{}, doneErr
		}
		slices.SortFunc(media, func(a, b database.MediaWithFullPath) int { return strings.Compare(a.Path, b.Path) })
		result, err = r.apply(ctx, newIndex(roms), media, done, func(processed, matched, total, skipped int) {
			select {
			case ch <- scraper.ScrapeUpdate{
				SystemID: systemID, Processed: processed, Total: total, Matched: matched,
				Skipped: skipped, TotalSteps: steps, CurrentStep: step,
			}:
			case <-ctx.Done():
			}
		})
		if err != nil {
			return result, err
		}
	}
	if rememberErr := scraper.RememberSystem(
		ctx, r.db, ScraperID, systemID, fingerprintVersion, state,
	); rememberErr != nil {
		log.Debug().Err(rememberErr).Msg("romm: system fingerprint not stored")
	}
	return result, nil
}

// completed lists the media rows this run leaves alone.
func (r *run) completed(ctx context.Context, systemDBID int64) (map[int64]struct{}, error) {
	var (
		done map[int64]struct{}
		err  error
	)
	switch {
	case r.opts.RunID != "" && (r.opts.Force || r.opts.FillMissing):
		done, err = r.db.GetScrapeRunMediaIDs(ctx, ScraperID, r.opts.RunID, systemDBID)
	case !r.opts.Force && !r.opts.FillMissing:
		done, err = r.db.GetScrapedMediaIDs(ctx, ScraperID, systemDBID)
	}
	if err != nil {
		return nil, fmt.Errorf("load scraped media: %w", err)
	}
	if done == nil {
		done = map[int64]struct{}{}
	}
	return done, nil
}

// apply matches media to ROMs and writes them a batch at a time, so artwork
// downloads and progress interleave with the writes.
func (r *run) apply(
	ctx context.Context, idx *index, media []database.MediaWithFullPath, done map[int64]struct{},
	report func(processed, matched, total, skipped int),
) (stats, error) {
	type pending struct {
		rom *romm.Rom
		row *database.MediaWithFullPath
	}
	var matched []pending
	result := stats{}
	for i := range media {
		row := &media[i]
		if row.IsMissing {
			continue
		}
		if _, ok := done[row.DBID]; ok {
			continue
		}
		result.Processed++
		rom := idx.match(row.Path)
		if rom == nil {
			result.Skipped++
			continue
		}
		matched = append(matched, pending{rom: rom, row: row})
	}
	written := 0
	for start := 0; start < len(matched); start += scraper.WriteBatchSize {
		end := min(start+scraper.WriteBatchSize, len(matched))
		targets := make([]database.ScrapeWriteTarget, 0, end-start)
		for _, m := range matched[start:end] {
			if err := ctx.Err(); err != nil {
				return result, err //nolint:wrapcheck // callers match context errors directly
			}
			write := r.buildWrite(ctx, m.rom)
			write.FillMissing = r.opts.FillMissing
			targets = append(targets, database.ScrapeWriteTarget{
				MediaDBID: m.row.DBID, MediaTitleDBID: m.row.MediaTitleDBID, Write: write,
			})
		}
		if err := scraper.ApplyTargets(ctx, r.db, r.opts, ScraperID, targets, nil); err != nil {
			return result, err //nolint:wrapcheck // already names the scraper
		}
		written += len(targets)
		result.Matched = written
		report(result.Skipped+written, written, result.Processed, result.Skipped)
	}
	return result, nil
}

func (r *run) scrapeScope(ctx context.Context, ch chan<- scraper.ScrapeUpdate) {
	selection, err := scraper.LoadScopedSelection(ctx, r.db, r.opts, ScraperID)
	if err != nil {
		ch <- scraper.ScrapeUpdate{FatalErr: fmt.Errorf("romm: %w", err), Done: true}
		return
	}
	media, _ := selection.Pending()
	if len(media) == 0 {
		scraper.ApplyScopedTargets(ctx, r.db, r.opts, selection, nil, ch)
		return
	}
	roms, err := romm.SystemRoms(ctx, r.client, r.opts.Scope.SystemID)
	if err != nil {
		r.finishWithError(ctx, err, r.opts.Scope.SystemID, stats{}, 1, 1, ch)
		return
	}
	idx := newIndex(roms)
	targets := make([]database.ScrapeWriteTarget, 0, len(media))
	for i := range media {
		rom := idx.match(media[i].Path)
		if rom == nil {
			continue
		}
		write := r.buildWrite(ctx, rom)
		write.FillMissing = r.opts.FillMissing
		targets = append(targets, database.ScrapeWriteTarget{
			MediaDBID: media[i].DBID, MediaTitleDBID: media[i].MediaTitleDBID, Write: write,
		})
	}
	scraper.ApplyScopedTargets(ctx, r.db, r.opts, selection, targets, ch)
}

// index finds the RomM ROM a media row is, by ID for rows indexed from RomM
// and by file name for the device's own files. A name two ROMs share
// matches neither.
type index struct {
	byID   map[int]*romm.Rom
	byName map[string]*romm.Rom
	byStem map[string]*romm.Rom
}

func newIndex(roms []romm.Rom) *index {
	idx := &index{
		byID:   make(map[int]*romm.Rom, len(roms)),
		byName: make(map[string]*romm.Rom, len(roms)),
		byStem: make(map[string]*romm.Rom, len(roms)),
	}
	add := func(m map[string]*romm.Rom, key string, rom *romm.Rom) {
		if key == "" {
			return
		}
		if prev, ok := m[key]; ok && prev != rom {
			m[key] = nil
			return
		}
		m[key] = rom
	}
	for i := range roms {
		rom := &roms[i]
		idx.byID[rom.ID] = rom
		for _, name := range romm.MatchNames(rom) {
			add(idx.byName, name, rom)
		}
		add(idx.byStem, strings.ToLower(strings.TrimSpace(rom.FSNameNoExt)), rom)
	}
	return idx
}

func (idx *index) match(p string) *romm.Rom {
	if romm.IsRomPath(p) {
		id, err := romm.ParseRomPath(p)
		if err != nil {
			return nil
		}
		return idx.byID[id]
	}
	base := p
	if j := strings.LastIndexAny(base, `/\`); j >= 0 {
		base = base[j+1:]
	}
	base = strings.ToLower(base)
	if base == "" {
		return nil
	}
	if rom, ok := idx.byName[base]; ok {
		return rom
	}
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	return idx.byStem[stem]
}

// buildWrite maps one ROM's metadata to tags, a description and artwork.
func (r *run) buildWrite(ctx context.Context, rom *romm.Rom) *database.ScrapeWrite {
	write := &database.ScrapeWrite{Sentinel: scraper.SentinelTagInfo(ScraperID)}
	if r.opts.RunID != "" {
		write.MediaTags = append(write.MediaTags, scraper.RunTagInfo(ScraperID, r.opts.RunID))
	}
	seen := make(map[string]struct{})
	addTag := func(list *[]database.TagInfo, tag database.TagInfo) {
		key := tag.Type + ":" + tag.Tag
		if _, dup := seen[key]; dup {
			return
		}
		seen[key] = struct{}{}
		*list = append(*list, tag)
	}
	meta := &rom.Metadatum

	if year := releaseYear(meta.FirstReleaseDate); year != "" {
		addTag(&write.TitleTags, database.TagInfo{Type: string(tags.TagTypeYear), Tag: year})
	}
	addCompanies := func(tagType tags.TagType, names []string) {
		for _, raw := range names {
			name := cleanText(raw)
			if name == "" {
				continue
			}
			normalized := string(tags.NormalizeCompanyName(name))
			if tags.IsValidTagValue(tagType, normalized) {
				addTag(&write.TitleTags, database.TagInfo{Type: string(tagType), Tag: normalized, Label: name})
			}
		}
	}
	addCompanies(tags.TagTypeDeveloper, meta.Developers)
	addCompanies(tags.TagTypePublisher, meta.Publishers)
	for _, raw := range meta.Genres {
		values, unmapped := ssgenre.Lookup(raw)
		for _, value := range values {
			addTag(&write.TitleTags, database.TagInfo{Type: string(tags.TagTypeGenre), Tag: string(value)})
		}
		for _, piece := range unmapped {
			r.unmapped.Note(ScraperID, tags.TagTypeGenre, piece)
		}
	}
	if players := maxPlayers(meta.PlayerCount); players != "" {
		if tags.IsCanonicalValue(tags.TagTypePlayers, tags.TagValue(players)) {
			addTag(&write.TitleTags, database.TagInfo{Type: string(tags.TagTypePlayers), Tag: players})
		} else {
			r.unmapped.Note(ScraperID, tags.TagTypePlayers, meta.PlayerCount)
		}
	}
	if rating := ratingValue(meta.AverageRating); rating != "" && tags.IsValidTagValue(tags.TagTypeRating, rating) {
		addTag(&write.TitleTags, database.TagInfo{Type: string(tags.TagTypeRating), Tag: rating})
	}
	for _, word := range rom.Regions {
		if value, ok := tags.LookupRegionWord(strings.TrimSpace(word)); ok {
			addTag(&write.MediaTags, database.TagInfo{Type: string(tags.TagTypeRegion), Tag: string(value)})
		} else if strings.TrimSpace(word) != "" {
			r.unmapped.Note(ScraperID, tags.TagTypeRegion, word)
		}
	}
	for _, word := range rom.Languages {
		if value, ok := tags.LookupLanguageWord(strings.TrimSpace(word)); ok {
			addTag(&write.MediaTags, database.TagInfo{Type: string(tags.TagTypeLang), Tag: string(value)})
		} else if strings.TrimSpace(word) != "" {
			r.unmapped.Note(ScraperID, tags.TagTypeLang, word)
		}
	}
	if description := truncateText(cleanText(rom.Summary), maxDescriptionLength); description != "" {
		write.TitleProps = append(write.TitleProps, database.MediaProperty{
			TypeTag: tags.PropertyTypeTag(tags.TagPropertyDescription), Text: description,
		})
	}
	write.MediaProps = r.artwork(ctx, rom)
	return write
}

// artwork downloads a ROM's cover and first screenshot, or reuses copies an
// earlier run stored, and returns them as image properties.
func (r *run) artwork(ctx context.Context, rom *romm.Rom) []database.MediaProperty {
	cover := rom.PathCoverLarge
	if cover == "" {
		cover = rom.PathCoverSmall
	}
	var screenshot string
	if len(rom.MergedScreenshots) > 0 {
		screenshot = rom.MergedScreenshots[0]
	}
	images := []struct {
		asset    string
		name     string
		property tags.TagValue
	}{
		{asset: cover, name: "cover", property: tags.TagPropertyImageBoxart},
		{asset: screenshot, name: "screenshot", property: tags.TagPropertyImageScreenshot},
	}
	var props []database.MediaProperty
	for _, image := range images {
		if image.asset == "" {
			continue
		}
		rel, err := r.image(ctx, rom.ID, image.name, image.asset)
		if err != nil {
			log.Debug().Err(err).Int("rom", rom.ID).Str("image", image.name).Msg("romm: artwork not stored")
			continue
		}
		props = append(props, database.MediaProperty{
			TypeTag: tags.PropertyTypeTag(image.property),
			Text:    config.ImagePath(config.ImageRomMDir + "/" + rel),
		})
	}
	return props
}

// image returns the slash-separated path, under the romm image folder, of
// one artwork file, downloading it unless it is already stored.
func (r *run) image(ctx context.Context, romID int, name, asset string) (string, error) {
	relDir := r.server + "/" + strconv.Itoa(romID)
	dir := filepath.Join(r.root, r.server, strconv.Itoa(romID))
	if !r.opts.Force {
		for _, ext := range []string{".png", ".jpg", ".webp", ".gif"} {
			if info, err := r.fs.Stat(filepath.Join(dir, name+ext)); err == nil && info.Size() > 0 {
				return relDir + "/" + name + ext, nil
			}
		}
	}
	if r.lowSpace {
		return "", errors.New("image folder is low on space")
	}
	if r.freeSpace != nil {
		if free, err := r.freeSpace(r.root); err == nil && free < freeSpaceFloor {
			r.lowSpace = true
			log.Warn().Str("dir", r.root).Uint64("freeMB", free/(1024*1024)).
				Msg("romm: not enough free space for artwork, downloads stopped")
			return "", errors.New("image folder is low on space")
		}
	}
	assetURL, ok := r.client.AssetURL(asset)
	if !ok {
		return "", fmt.Errorf("unusable asset path %q", asset)
	}
	data, err := r.client.Fetch(ctx, assetURL, imageMaxBytes)
	if err != nil {
		return "", fmt.Errorf("fetch %s: %w", asset, err)
	}
	ext, ok := imageTypes[http.DetectContentType(data)]
	if !ok {
		return "", fmt.Errorf("%s is not a supported image", asset)
	}
	if err := writeFile(r.fs, filepath.Join(dir, name+ext), data); err != nil {
		return "", err
	}
	// A format change leaves the older copy behind; drop it so the stored
	// path is the only one.
	for _, other := range []string{".png", ".jpg", ".webp", ".gif"} {
		if other != ext {
			if rmErr := r.fs.Remove(filepath.Join(dir, name+other)); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
				log.Debug().Err(rmErr).Msg("romm: stale artwork not removed")
			}
		}
	}
	return relDir + "/" + name + ext, nil
}

// writeFile replaces path atomically, so an interrupted run never leaves a
// truncated image a later run would reuse.
func writeFile(fs afero.Fs, path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := fs.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	tmpFile, err := afero.TempFile(fs, dir, filepath.Base(path)+".*.part")
	if err != nil {
		return fmt.Errorf("create temp file for %s: %w", path, err)
	}
	tmp := tmpFile.Name()
	_, writeErr := tmpFile.Write(data)
	if closeErr := tmpFile.Close(); writeErr == nil {
		writeErr = closeErr
	}
	if writeErr != nil {
		_ = fs.Remove(tmp)
		return fmt.Errorf("write %s: %w", tmp, writeErr)
	}
	if err := fs.Rename(tmp, path); err != nil {
		_ = fs.Remove(tmp)
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}

// releaseYear reads RomM's first release date, a Unix time in milliseconds.
// Values small enough to be seconds are read as seconds.
func releaseYear(date *int64) string {
	if date == nil || *date <= 0 {
		return ""
	}
	var t time.Time
	if *date > 1e11 {
		t = time.UnixMilli(*date)
	} else {
		t = time.Unix(*date, 0)
	}
	year := strconv.Itoa(t.UTC().Year())
	if !tags.IsValidTagValue(tags.TagTypeYear, year) {
		return ""
	}
	return year
}

// maxPlayers reads the highest player count in RomM's text, such as "1-4".
func maxPlayers(s string) string {
	best := 0
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsDigit(r) }) {
		if n, err := strconv.Atoi(part); err == nil && n > best {
			best = n
		}
	}
	if best == 0 {
		return ""
	}
	return strconv.Itoa(best)
}

// ratingValue rounds RomM's average rating, on a 0-100 scale, to a whole
// number.
func ratingValue(rating *float64) string {
	if rating == nil || math.IsNaN(*rating) || *rating <= 0 || *rating > 100 {
		return ""
	}
	return strconv.Itoa(int(math.Round(*rating)))
}

// truncateText cuts text to at most limit runes, at a word boundary.
func truncateText(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	cut := string(runes[:limit])
	if idx := strings.LastIndexByte(cut, ' '); idx > limit/2 {
		cut = cut[:idx]
	}
	return strings.TrimSpace(cut)
}

// cleanText collapses whitespace and drops control characters from a field.
func cleanText(value string) string {
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	})
	return strings.Join(fields, " ")
}
