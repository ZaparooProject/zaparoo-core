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
	"slices"
	"sort"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/container"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediascanner"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/bgpriority"
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
	db              database.MediaDBI
	fs              afero.Fs
	sourcesBySystem map[string][]scraper.MediaSource
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
			systems, sources, err := resolveSystemsFromPlatform(ctx, cfg, pl, fs, db.MediaDB, opts.SystemIDs())
			if err != nil {
				return fmt.Errorf("localmedia: resolve systems: %w", err)
			}
			s := &scraperImpl{db: db.MediaDB, fs: fs, sourcesBySystem: sources}
			go s.scrapeLoop(ctx, opts, systems, ch)
			return nil
		},
	}
}

func resolveSystemsFromPlatform(
	ctx context.Context,
	cfg *config.Instance,
	pl platforms.Platform,
	fs afero.Fs,
	mdb database.MediaDBI,
	systemIDs []string,
) ([]scraper.ScrapeSystem, map[string][]scraper.MediaSource, error) {
	indexed, err := mdb.IndexedSystems()
	if err != nil {
		return nil, nil, fmt.Errorf("list indexed systems: %w", err)
	}

	wantedIDs := orderedScrapeSystemIDs(indexed, systemIDs)
	dbSystems := make(map[string]database.System, len(wantedIDs))
	sysDefs := make([]systemdefs.System, 0, len(wantedIDs))
	for _, sysID := range wantedIDs {
		sys, err := mdb.FindSystemBySystemID(sysID)
		if err != nil {
			return nil, nil, fmt.Errorf("look up system %q: %w", sysID, err)
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

	sourcesBySystem := make(map[string][]scraper.MediaSource)
	result := make([]scraper.ScrapeSystem, 0, len(sysDefs))
	for _, sys := range sysDefs {
		romPaths := pathsBySystem[sys.ID]
		var sources scraper.Sources
		if provider, ok := pl.(platforms.ScrapeSourceProvider); ok {
			var sourceErr error
			sources, sourceErr = provider.ScrapeSources(ctx, cfg, fs, sys.ID)
			if sourceErr != nil {
				return nil, nil, fmt.Errorf("resolve scrape sources for %s: %w", sys.ID, sourceErr)
			}
			for _, root := range sources.Roots {
				if !slices.Contains(romPaths, root) {
					romPaths = append(romPaths, root)
				}
			}
		}
		if len(romPaths) == 0 {
			log.Debug().Str("system", sys.ID).Msg("localmedia: no launcher paths found, skipping")
			continue
		}
		sourcesBySystem[sys.ID] = sources.Media
		result = append(result, scraper.ScrapeSystem{DBID: dbSystems[sys.ID].DBID, ID: sys.ID, ROMPaths: romPaths})
	}
	return result, sourcesBySystem, nil
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
	// Lowest CPU/IO priority for the whole scrape run; the locked thread
	// dies with this goroutine so the change never leaks.
	bgpriority.Apply()
	if opts.Scope != nil && len(systems) == 0 {
		selection, err := scraper.LoadScopedSelection(ctx, s.db, opts, scraperID)
		if err != nil {
			ch <- scraper.ScrapeUpdate{FatalErr: err, Done: true}
			return
		}
		scraper.ApplyScopedTargets(ctx, s.db, opts, selection, nil, ch)
		return
	}
	for systemIdx, system := range systems {
		if err := waitForScrape(ctx, opts); err != nil {
			ch <- scraper.ScrapeUpdate{FatalErr: err, Done: true}
			return
		}

		var mediaRows []database.MediaWithFullPath
		var completed map[int64]struct{}
		var err error
		if opts.Scope != nil {
			var selection scraper.ScopedSelection
			selection, err = scraper.LoadScopedSelection(ctx, s.db, opts, scraperID)
			mediaRows, completed = selection.Media, selection.Completed
		} else {
			mediaRows, err = s.db.GetMediaBySystemID(system.ID)
		}
		if err != nil {
			ch <- scraper.ScrapeUpdate{
				FatalErr: fmt.Errorf("localmedia: load media for %s: %w", system.ID, err),
				Done:     true,
			}
			return
		}

		processed, matched, skipped := 0, 0, 0
		availableDirs := s.availableDirsByRoot(system.ROMPaths)
		var sources *scraper.SourceIndex
		if len(s.sourcesBySystem[system.ID]) > 0 {
			sources = scraper.NewSourceIndex(s.sourcesBySystem[system.ID])
		}
		var containers scraper.ContainerResolver = containerIndexForMedia(mediaRows)
		if opts.Scope != nil {
			containers, err = scraper.ScopedContainers(ctx, s.db, system.ID, mediaRows)
			if err != nil {
				ch <- scraper.ScrapeUpdate{FatalErr: err, Done: true}
				return
			}
		}
		// A scoped run loads only the media in scope, so the directories it can
		// see are not the system's. ReplaceDirectoryProperties swaps the whole
		// per-system snapshot, so deriving one from a scoped selection would
		// delete every other directory's folder artwork for this system.
		var directoryPaths []string
		if opts.Scope == nil {
			directoryPaths = indexedDirectoryPaths(mediaRows, system.ROMPaths)
		}
		total := len(mediaRows) + len(directoryPaths)
		ch <- scraper.ScrapeUpdate{
			SystemID:    system.ID,
			Total:       total,
			TotalSteps:  len(systems),
			CurrentStep: systemIdx + 1,
		}

		for mediaIdx := range mediaRows {
			media := &mediaRows[mediaIdx]
			if err := waitForScrape(ctx, opts); err != nil {
				ch <- scraper.ScrapeUpdate{FatalErr: err, Done: true}
				return
			}

			if _, done := completed[media.DBID]; done {
				processed++
				skipped++
				ch <- scraper.ScrapeUpdate{
					SystemID: system.ID, Total: len(mediaRows),
					Processed: processed, Matched: matched, Skipped: skipped,
					TotalSteps: len(systems), CurrentStep: systemIdx + 1,
				}
				continue
			}
			roots, names, cleanupNames := mediaArtworkNames(media, system.ROMPaths, containers, sources, opts.Force)
			props := s.mediaPropsForNames(names, roots, availableDirs)
			staleDeleted := 0
			if opts.Force {
				var cleanupErr error
				staleDeleted, cleanupErr = s.deleteStaleLocalMediaPropsNamed(
					ctx, media, roots, props, cleanupNames,
				)
				if cleanupErr != nil {
					skipped++
					processed++
					ch <- scraper.ScrapeUpdate{
						Err:         cleanupErr,
						SystemID:    system.ID,
						Processed:   processed,
						Total:       total,
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
						Total:       total,
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
				Total:       total,
				Matched:     matched,
				Skipped:     skipped,
				TotalSteps:  len(systems),
				CurrentStep: systemIdx + 1,
			}
		}

		directoryProps := make([]database.DirectoryProperty, 0)
		for _, directoryPath := range directoryPaths {
			if err := waitForScrape(ctx, opts); err != nil {
				ch <- scraper.ScrapeUpdate{FatalErr: err, Done: true}
				return
			}

			props := s.directoryPropsForPath(directoryPath, system.ROMPaths, availableDirs)
			if len(props) == 0 {
				skipped++
			} else {
				directoryProps = append(directoryProps, props...)
				matched++
			}
			processed++
			ch <- scraper.ScrapeUpdate{
				SystemID:    system.ID,
				Processed:   processed,
				Total:       total,
				Matched:     matched,
				Skipped:     skipped,
				TotalSteps:  len(systems),
				CurrentStep: systemIdx + 1,
			}
		}
		if err := s.replaceDirectoryPropertiesUnlessScoped(ctx, opts, system.DBID, directoryProps); err != nil {
			ch <- scraper.ScrapeUpdate{
				FatalErr:    fmt.Errorf("localmedia: replace directory properties for %s: %w", system.ID, err),
				SystemID:    system.ID,
				Processed:   processed,
				Total:       total,
				Matched:     matched,
				Skipped:     skipped,
				TotalSteps:  len(systems),
				CurrentStep: systemIdx + 1,
				Done:        true,
			}
			return
		}

		if opts.Scope != nil {
			ch <- scraper.ScrapeUpdate{
				SystemID: system.ID, Total: len(mediaRows), Processed: processed, Matched: matched, Skipped: skipped,
				TotalSteps: 1, CurrentStep: 1, Done: true,
			}
			return
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

func indexedDirectoryPaths(rows []database.MediaWithFullPath, roots []string) []string {
	directories := make(map[string]struct{})
	// The roots are fixed for the whole call, and filepath.Abs on a relative
	// one is a getwd syscall, so resolving them per row would charge a system
	// its row count in syscalls for an answer that never changes. An empty
	// entry marks a root that would not resolve.
	rootAbsolute := make([]string, len(roots))
	for i, root := range roots {
		if abs, err := filepath.Abs(root); err == nil {
			rootAbsolute[i] = filepath.Clean(abs)
		}
	}
	for i := range rows {
		if rows[i].IsMissing {
			continue
		}
		for rootIndex, root := range roots {
			resolved := esmedia.ResolvePath(rows[i].Path, root)
			if resolved == "" {
				continue
			}
			rootAbs := rootAbsolute[rootIndex]
			if rootAbs == "" {
				break
			}
			dir := filepath.Dir(resolved)
			for dir != rootAbs && esmedia.PathWithinRoot(dir, rootAbs) {
				if dir == "." || dir == string(filepath.Separator) {
					break
				}
				directories[filepath.ToSlash(filepath.Clean(dir))] = struct{}{}
				parent := filepath.Dir(dir)
				if parent == dir {
					break
				}
				dir = parent
			}
			break
		}
	}

	paths := make([]string, 0, len(directories))
	for path := range directories {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func (s *scraperImpl) directoryPropsForPath(
	directoryPath string,
	roots []string,
	availableDirs map[string]map[string]string,
) []database.DirectoryProperty {
	var fallbackNames []string
	for _, root := range roots {
		fallbackNames = esmedia.DirectoryArtworkFallbackNames(directoryPath, root)
		if len(fallbackNames) > 0 {
			break
		}
	}
	if len(fallbackNames) == 0 {
		return nil
	}

	orderedDirs := make([]map[string]string, 0, len(roots))
	for _, root := range roots {
		orderedDirs = append(orderedDirs, availableDirs[root])
	}

	props := make([]database.DirectoryProperty, 0)
	for _, propValue := range artworkPropertyOrder {
		file := esmedia.FindFileAcrossRootsFS(
			s.fs,
			fallbackNames,
			esmedia.ArtworkDirCandidates[string(propValue)],
			orderedDirs,
		)
		if file == nil {
			continue
		}
		props = append(props, database.DirectoryProperty{
			Path:    filepath.ToSlash(filepath.Clean(directoryPath)),
			TypeTag: tags.PropertyTypeTag(propValue),
			Text:    filepath.ToSlash(file.Path),
		})
	}
	return props
}

func (s *scraperImpl) mediaPropsForPath(
	path string,
	roots []string,
	availableDirs map[string]map[string]string,
	isContainerTarget bool,
) []database.MediaProperty {
	return s.mediaPropsForNames(artworkFallbackNames(path, roots, isContainerTarget), roots, availableDirs)
}

func (s *scraperImpl) mediaPropsForNames(
	fallbackNames, roots []string, availableDirs map[string]map[string]string,
) []database.MediaProperty {
	if len(fallbackNames) == 0 {
		return nil
	}

	// roots is in RootDirs order, so the first root with a match wins.
	orderedDirs := make([]map[string]string, 0, len(roots))
	for _, root := range roots {
		orderedDirs = append(orderedDirs, availableDirs[root])
	}

	props := make([]database.MediaProperty, 0)
	for _, propValue := range artworkPropertyOrder {
		file := esmedia.FindFileAcrossRootsFS(
			s.fs,
			fallbackNames,
			esmedia.ArtworkDirCandidates[string(propValue)],
			orderedDirs,
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
	return props
}

// containerIndexForMedia builds the directory-container view of a system's media
// so artwork named after a disc folder can be matched to the file that folder
// launches.
func containerIndexForMedia(rows []database.MediaWithFullPath) *container.Index {
	media := make([]database.Media, 0, len(rows))
	for i := range rows {
		media = append(media, database.Media{
			DBID:           rows[i].DBID,
			MediaTitleDBID: rows[i].MediaTitleDBID,
			Path:           rows[i].Path,
			ParentDir:      rows[i].ParentDir,
			IsMissing:      rows[i].IsMissing,
		})
	}
	return container.NewIndex(media)
}

func isContainerLaunchTarget(containers scraper.ContainerResolver, media *database.MediaWithFullPath) bool {
	parent := media.ParentDir
	if parent == "" {
		parent = container.ParentDir(media.Path)
	}
	target := containers.Resolve(parent)
	return target != nil && target.DBID == media.DBID
}

// artworkFallbackNames derives the artwork filenames to look for from the root
// that actually contains the game (its home root); the same relative names are
// then searched under every root's media/ dir so art can live on a different
// drive than the rom. A file that is the single launch target of its directory
// also answers to art named after that directory, which is how EmulationStation
// stores art for a folder it shows as one game.
func artworkFallbackNames(path string, roots []string, isContainerTarget bool) []string {
	for _, root := range roots {
		names := esmedia.ArtworkFallbackNames(path, root)
		if len(names) == 0 {
			continue
		}
		if isContainerTarget {
			names = append(names, esmedia.ContainerArtworkFallbackNames(path, root)...)
		}
		return names
	}
	return nil
}

// deleteStaleLocalMediaProps drops properties this scraper wrote that a forced
// rescrape no longer finds. Container-style names are always considered: a
// directory that used to collapse to one game may since have gained nested
// media, and its folder artwork still needs clearing.
func (s *scraperImpl) deleteStaleLocalMediaProps(
	ctx context.Context,
	media *database.MediaWithFullPath,
	roots []string,
	foundProps []database.MediaProperty,
) (int, error) {
	names := artworkFallbackNames(media.Path, roots, true)
	return s.deleteStaleLocalMediaPropsNamed(ctx, media, roots, foundProps, names)
}

func (s *scraperImpl) deleteStaleLocalMediaPropsNamed(
	ctx context.Context, media *database.MediaWithFullPath, roots []string,
	foundProps []database.MediaProperty, fallbackNames []string,
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
		if prop.TypeTagDBID == 0 || !isLocalMediaPropForNames(&prop, roots, fallbackNames) {
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

func isLocalMediaPropForNames(prop *database.MediaProperty, roots, fallbackNames []string) bool {
	propValue, ok := imagePropertyValue(prop.TypeTag)
	if !ok || prop.Text == "" {
		return false
	}
	candidates := esmedia.ArtworkDirCandidates[propValue]
	if len(candidates) == 0 {
		return false
	}

	// Match the prop path against the media/ convention under any root (art may
	// live cross-drive), using the same names a scrape would have written.
	if len(fallbackNames) == 0 {
		return false
	}

	propPath := filepath.Clean(filepath.FromSlash(prop.Text))
	for _, root := range roots {
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

// replaceDirectoryPropertiesUnlessScoped writes the system's folder-artwork
// snapshot, and does nothing for a scoped run.
//
// ReplaceDirectoryProperties swaps the complete set for the system. A scoped
// run only ever sees the directories of the media in its scope, so letting it
// write would delete the folder artwork of every directory it did not look at.
func (s *scraperImpl) replaceDirectoryPropertiesUnlessScoped(
	ctx context.Context, opts scraper.ScrapeOptions, systemDBID int64,
	props []database.DirectoryProperty,
) error {
	if opts.Scope != nil {
		return nil
	}
	if _, err := s.db.ReplaceDirectoryProperties(ctx, systemDBID, props); err != nil {
		return fmt.Errorf("localmedia: replace directory properties: %w", err)
	}
	return nil
}
