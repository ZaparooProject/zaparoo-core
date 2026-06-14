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

// Package localmedia imports EmulationStation-style media folder artwork.
package localmedia

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediascanner"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esmedia"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const scraperID = "media-folder"

var artworkPropertyOrder = []tags.TagValue{ //nolint:gochecknoglobals // Stable scraper property order.
	tags.TagPropertyImageImage,
	tags.TagPropertyImageThumbnail,
	tags.TagPropertyImageBoxart,
	tags.TagPropertyImageBoxart3D,
	tags.TagPropertyImageBoxartSide,
	tags.TagPropertyImageBoxartBack,
	tags.TagPropertyImageScreenshot,
	tags.TagPropertyImageMarquee,
	tags.TagPropertyImageWheel,
	tags.TagPropertyImageFanart,
	tags.TagPropertyImageTitleshot,
	tags.TagPropertyImageMap,
}

type scraperImpl struct {
	db database.MediaDBI
	fs afero.Fs
}

// NewPlatformScraper returns a scraper that imports image paths from local
// EmulationStation media directories under each system folder.
func NewPlatformScraper() platforms.Scraper {
	return platforms.Scraper{
		ID:                 scraperID,
		Name:               "ES media folders",
		SupportedSystemIDs: []string{},
		Scrape: func(
			ctx context.Context,
			cfg *config.Instance,
			pl platforms.Platform,
			fs afero.Fs,
			db *database.Database,
			opts scraper.ScrapeOptions,
			_ platforms.ScraperCustomOptions,
			ch chan<- scraper.ScrapeUpdate,
		) error {
			systems, err := resolveSystemsFromPlatform(ctx, cfg, pl, db.MediaDB, opts.Systems)
			if err != nil {
				return fmt.Errorf("localmedia: resolve systems: %w", err)
			}
			s := &scraperImpl{db: db.MediaDB, fs: fs}
			go s.scrapeLoop(ctx, opts, systems, ch)
			return nil
		},
	}
}

func resolveSystemsFromPlatform(
	ctx context.Context,
	cfg *config.Instance,
	pl platforms.Platform,
	mdb database.MediaDBI,
	systemIDs []string,
) ([]scraper.ScrapeSystem, error) {
	indexed, err := mdb.IndexedSystems()
	if err != nil {
		return nil, fmt.Errorf("list indexed systems: %w", err)
	}

	wantedIDs := orderedScrapeSystemIDs(indexed, systemIDs)
	dbSystems := make(map[string]database.System, len(wantedIDs))
	sysDefs := make([]systemdefs.System, 0, len(wantedIDs))
	for _, sysID := range wantedIDs {
		sys, err := mdb.FindSystemBySystemID(sysID)
		if err != nil {
			return nil, fmt.Errorf("look up system %q: %w", sysID, err)
		}
		systemDef, err := systemdefs.GetSystem(sysID)
		if err != nil {
			log.Debug().Err(err).Str("system", sysID).Msg("localmedia: unknown system definition, skipping")
			continue
		}
		dbSystems[sysID] = sys
		sysDefs = append(sysDefs, *systemDef)
	}

	pathsBySystem := make(map[string][]string, len(sysDefs))
	for _, pathResult := range mediascanner.GetSystemPaths(ctx, cfg, pl, pl.RootDirs(cfg), sysDefs) {
		pathsBySystem[pathResult.System.ID] = append(pathsBySystem[pathResult.System.ID], pathResult.Path)
	}

	result := make([]scraper.ScrapeSystem, 0, len(sysDefs))
	for _, sys := range sysDefs {
		romPaths := pathsBySystem[sys.ID]
		if len(romPaths) == 0 {
			log.Debug().Str("system", sys.ID).Msg("localmedia: no launcher paths found, skipping")
			continue
		}
		result = append(result, scraper.ScrapeSystem{DBID: dbSystems[sys.ID].DBID, ID: sys.ID, ROMPaths: romPaths})
	}
	return result, nil
}

func orderedScrapeSystemIDs(indexed, requested []string) []string {
	indexedSet := make(map[string]struct{}, len(indexed))
	for _, id := range indexed {
		indexedSet[id] = struct{}{}
	}

	candidateIDs := indexed
	if len(requested) > 0 {
		candidateIDs = requested
	}

	seen := make(map[string]struct{}, len(candidateIDs))
	ordered := make([]string, 0, len(candidateIDs))
	for _, id := range candidateIDs {
		if _, ok := indexedSet[id]; !ok {
			continue
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		ordered = append(ordered, id)
	}
	return ordered
}

func (s *scraperImpl) scrapeLoop(
	ctx context.Context,
	opts scraper.ScrapeOptions,
	systems []scraper.ScrapeSystem,
	ch chan<- scraper.ScrapeUpdate,
) {
	defer close(ch)
	for systemIdx, system := range systems {
		if err := waitForScrape(ctx, opts); err != nil {
			ch <- scraper.ScrapeUpdate{FatalErr: err, Done: true}
			return
		}

		mediaRows, err := s.db.GetMediaBySystemID(system.ID)
		if err != nil {
			ch <- scraper.ScrapeUpdate{
				FatalErr: fmt.Errorf("localmedia: load media for %s: %w", system.ID, err),
				Done:     true,
			}
			return
		}

		processed, matched, skipped := 0, 0, 0
		availableDirs := s.availableDirsByRoot(system.ROMPaths)
		ch <- scraper.ScrapeUpdate{
			SystemID:    system.ID,
			Total:       len(mediaRows),
			TotalSteps:  len(systems),
			CurrentStep: systemIdx + 1,
		}

		for mediaIdx := range mediaRows {
			media := &mediaRows[mediaIdx]
			if err := waitForScrape(ctx, opts); err != nil {
				ch <- scraper.ScrapeUpdate{FatalErr: err, Done: true}
				return
			}

			props := s.mediaPropsForPath(media.Path, system.ROMPaths, availableDirs)
			staleDeleted := 0
			if opts.Force {
				var cleanupErr error
				staleDeleted, cleanupErr = s.deleteStaleLocalMediaProps(ctx, media, system.ROMPaths, props)
				if cleanupErr != nil {
					skipped++
					processed++
					ch <- scraper.ScrapeUpdate{
						Err:         cleanupErr,
						SystemID:    system.ID,
						Processed:   processed,
						Total:       len(mediaRows),
						Matched:     matched,
						Skipped:     skipped,
						TotalSteps:  len(systems),
						CurrentStep: systemIdx + 1,
					}
					continue
				}
			}

			if len(props) == 0 {
				if staleDeleted > 0 {
					matched++
				} else {
					skipped++
				}
			} else {
				write := &database.ScrapeWrite{
					Sentinel:   scraper.SentinelTagInfo(scraperID),
					MediaProps: props,
				}
				if opts.RunID != "" {
					write.MediaTags = append(write.MediaTags, scraper.RunTagInfo(scraperID, opts.RunID))
				}
				if err := s.db.ApplyScrapeResult(ctx, media.DBID, media.MediaTitleDBID, write); err != nil {
					skipped++
					processed++
					ch <- scraper.ScrapeUpdate{
						Err:         fmt.Errorf("localmedia: write media %d: %w", media.DBID, err),
						SystemID:    system.ID,
						Processed:   processed,
						Total:       len(mediaRows),
						Matched:     matched,
						Skipped:     skipped,
						TotalSteps:  len(systems),
						CurrentStep: systemIdx + 1,
					}
					continue
				}
				matched++
			}

			processed++
			ch <- scraper.ScrapeUpdate{
				SystemID:    system.ID,
				Processed:   processed,
				Total:       len(mediaRows),
				Matched:     matched,
				Skipped:     skipped,
				TotalSteps:  len(systems),
				CurrentStep: systemIdx + 1,
			}
		}
	}
	ch <- scraper.ScrapeUpdate{TotalSteps: len(systems), CurrentStep: len(systems), Done: true}
}

func waitForScrape(ctx context.Context, opts scraper.ScrapeOptions) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if opts.Pauser != nil {
		if err := opts.Pauser.Wait(ctx); err != nil {
			return fmt.Errorf("localmedia: wait while paused: %w", err)
		}
	}
	return nil
}

func (s *scraperImpl) availableDirsByRoot(roots []string) map[string]map[string]string {
	result := make(map[string]map[string]string, len(roots))
	for _, root := range roots {
		result[root] = esmedia.StatMediaDirsFS(s.fs, root)
	}
	return result
}

func (s *scraperImpl) mediaPropsForPath(
	path string,
	roots []string,
	availableDirs map[string]map[string]string,
) []database.MediaProperty {
	props := make([]database.MediaProperty, 0)
	for _, root := range roots {
		fallbackNames := esmedia.ArtworkFallbackNames(path, root)
		if len(fallbackNames) == 0 {
			continue
		}
		for _, propValue := range artworkPropertyOrder {
			file := esmedia.FindFileFS(
				s.fs,
				fallbackNames,
				esmedia.ArtworkDirCandidates[string(propValue)],
				availableDirs[root],
			)
			if file == nil {
				continue
			}
			props = append(props, database.MediaProperty{
				TypeTag:     tags.PropertyTypeTag(propValue),
				Text:        filepath.ToSlash(file.Path),
				ContentType: file.ContentType,
			})
		}
		if len(props) > 0 {
			return props
		}
	}
	return props
}

func (s *scraperImpl) deleteStaleLocalMediaProps(
	ctx context.Context,
	media *database.MediaWithFullPath,
	roots []string,
	foundProps []database.MediaProperty,
) (int, error) {
	existingProps, err := s.db.GetMediaPropertyMetadata(ctx, media.DBID)
	if err != nil {
		return 0, fmt.Errorf("localmedia: load media properties for %d: %w", media.DBID, err)
	}

	foundTypes := make(map[string]struct{}, len(foundProps))
	for _, prop := range foundProps {
		foundTypes[prop.TypeTag] = struct{}{}
	}

	deleted := 0
	for _, prop := range existingProps {
		if _, found := foundTypes[prop.TypeTag]; found {
			continue
		}
		if prop.TypeTagDBID == 0 || !isLocalMediaPropForPath(&prop, media.Path, roots) {
			continue
		}
		if err := s.db.DeleteMediaProperty(ctx, media.DBID, prop.TypeTagDBID); err != nil {
			return deleted, fmt.Errorf("localmedia: delete stale media property %d/%d: %w",
				media.DBID, prop.TypeTagDBID, err)
		}
		deleted++
	}
	return deleted, nil
}

func isLocalMediaPropForPath(prop *database.MediaProperty, mediaPath string, roots []string) bool {
	propValue, ok := imagePropertyValue(prop.TypeTag)
	if !ok || prop.Text == "" {
		return false
	}
	candidates := esmedia.ArtworkDirCandidates[propValue]
	if len(candidates) == 0 {
		return false
	}

	propPath := filepath.Clean(filepath.FromSlash(prop.Text))
	for _, root := range roots {
		fallbackNames := esmedia.ArtworkFallbackNames(mediaPath, root)
		if len(fallbackNames) == 0 {
			continue
		}
		for _, dir := range candidates {
			for _, name := range fallbackNames {
				candidate := filepath.Clean(filepath.Join(root, "media", dir, name))
				if propPath == candidate {
					return true
				}
			}
		}
	}
	return false
}

func imagePropertyValue(typeTag string) (string, bool) {
	prefix := string(tags.TagTypeProperty) + ":"
	if len(typeTag) <= len(prefix) || typeTag[:len(prefix)] != prefix {
		return "", false
	}
	value := typeTag[len(prefix):]
	if _, ok := esmedia.ArtworkDirCandidates[value]; !ok {
		return "", false
	}
	return value, true
}
