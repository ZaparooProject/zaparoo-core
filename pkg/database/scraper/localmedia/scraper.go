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
	"runtime/debug"
	"slices"
	"sort"
	"strings"

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
	db       database.MediaDBI
	fs       afero.Fs
	listings *sourceDirListings
	// dirNames holds one listing of each media directory of the system being
	// scraped, so a row's candidate filenames are ruled out in memory instead
	// of with a stat apiece.
	dirNames *esmedia.DirNames
}

// NewPlatformScraper returns a scraper that imports image paths from local
// EmulationStation media directories under each system folder. A platform
// that also grants source roots (Android's host-managed folders) gets the
// same artwork convention read through platforms.SourceRootReader instead of
// a filesystem, since a source root has no filesystem Core can open.
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
			systems, err := resolveSystemsFromPlatform(ctx, cfg, pl, fs, db.MediaDB, opts.SystemIDs())
			if err != nil {
				return fmt.Errorf("localmedia: resolve systems: %w", err)
			}
			s := &scraperImpl{db: db.MediaDB, fs: fs}
			if reader, ok := pl.(platforms.SourceRootReader); ok {
				s.listings = newSourceDirListings(reader)
			}
			go s.scrapeLoop(ctx, opts, systems, ch)
			return nil
		},
	}
}

func resolveSystemsFromPlatform(
	ctx context.Context,
	cfg *config.Instance,
	pl platforms.Platform,
	_ afero.Fs,
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

	virtualSystems := make(map[string]struct{})
	launchers := pl.Launchers(cfg)
	for i := range launchers {
		if launchers[i].SystemID != "" && len(launchers[i].Schemes) > 0 {
			virtualSystems[launchers[i].SystemID] = struct{}{}
		}
	}

	// A granted source root has no filesystem Core can walk with RootDirs, so
	// GetSystemPaths above never finds anything for it; GetMediaSourceRoots
	// is reserved for provenance a scanner explicitly records (ScanResult.
	// Source), which an ordinary file walk - including Android's source-root
	// walk - never sets. Join each granted root against the system's own
	// launcher folders instead, the same way media.browse's root discovery
	// already does for the identical reason (buildSystemBrowseRouteCandidates's
	// addSourceRoute), so a folder cover can be found without depending on
	// either.
	var sourceRootRefs []string
	if reader, ok := pl.(platforms.SourceRootReader); ok {
		refs, refsErr := reader.SourceRoots(ctx)
		if refsErr != nil {
			return nil, fmt.Errorf("list source roots: %w", refsErr)
		}
		sourceRootRefs = refs
	}
	sourceFoldersBySystem := make(map[string][]string, len(launchers))
	for i := range launchers {
		if launchers[i].SystemID == "" || launchers[i].SkipFilesystemScan {
			continue
		}
		sysID := launchers[i].SystemID
		for _, folder := range launchers[i].Folders {
			if filepath.IsAbs(folder) {
				continue
			}
			if !slices.Contains(sourceFoldersBySystem[sysID], folder) {
				sourceFoldersBySystem[sysID] = append(sourceFoldersBySystem[sysID], folder)
			}
		}
	}

	result := make([]scraper.ScrapeSystem, 0, len(sysDefs))
	for _, sys := range sysDefs {
		romPaths := pathsBySystem[sys.ID]
		sourceRoots, sourceErr := mdb.GetMediaSourceRoots(ctx, sys.ID)
		if sourceErr != nil {
			return nil, fmt.Errorf("resolve indexed media source roots for %s: %w", sys.ID, sourceErr)
		}
		for _, root := range sourceRoots {
			if !slices.Contains(romPaths, root) {
				romPaths = append(romPaths, root)
			}
		}
		for _, root := range sourceRootRefs {
			for _, folder := range sourceFoldersBySystem[sys.ID] {
				romPath := strings.TrimSuffix(root, "/") + "/" + folder
				if !slices.Contains(romPaths, romPath) {
					romPaths = append(romPaths, romPath)
				}
			}
		}
		if len(romPaths) == 0 {
			if _, virtual := virtualSystems[sys.ID]; virtual {
				log.Warn().Str("system", sys.ID).
					Msg("virtual media has no indexed metadata sources; reindex this system before scraping")
			} else {
				log.Debug().Str("system", sys.ID).Msg("localmedia: no launcher paths found, skipping")
			}
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
	for systemIdx := range systems {
		if !s.scrapeSystem(ctx, opts, systems, systemIdx, ch) {
			return
		}
	}
	ch <- scraper.ScrapeUpdate{TotalSteps: len(systems), CurrentStep: len(systems), Done: true}
}

// scrapeSystem scrapes one system. It returns false when the run has ended,
// having already sent the final update.
//
//nolint:gocognit,gocyclo,cyclop,funlen // one system's load, per-row, and folder artwork sequence
func (s *scraperImpl) scrapeSystem(
	ctx context.Context,
	opts scraper.ScrapeOptions,
	systems []scraper.ScrapeSystem,
	systemIdx int,
	ch chan<- scraper.ScrapeUpdate,
) bool {
	system := systems[systemIdx]
	if err := waitForScrape(ctx, opts); err != nil {
		ch <- scraper.ScrapeUpdate{FatalErr: err, Done: true}
		return false
	}

	availableDirs := s.availableDirsByRoot(ctx, system.ROMPaths)
	if opts.Scope == nil && !opts.Force && !hasArtworkDirs(availableDirs) {
		return s.finishSystemWithoutArtwork(ctx, opts, systems, systemIdx, ch)
	}
	// Hand the system's working set back to the OS before the next one, so a
	// run's peak is one system rather than the sum of them.
	defer debug.FreeOSMemory()
	s.dirNames = esmedia.NewDirNames(s.fs)
	defer func() { s.dirNames = nil }()

	var completed map[int64]struct{}
	var selection scraper.ScopedSelection
	var scan systemScan
	var containers launchTargetResolver
	var err error
	if opts.Scope != nil {
		// A scoped run loads only the media in scope, so the directories it
		// can see are not the system's. ReplaceDirectoryProperties swaps the
		// whole per-system snapshot, so deriving one from a scoped selection
		// would delete every other directory's folder artwork for this system.
		selection, err = scraper.LoadScopedSelection(ctx, s.db, opts, scraperID)
		completed = selection.Completed
		scan.rows = len(selection.Media)
	} else {
		scan, err = s.scanSystem(ctx, system)
		containers = scan.targets
	}
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			err = ctxErr
		}
		ch <- scraper.ScrapeUpdate{
			FatalErr: fmt.Errorf("localmedia: load media for %s: %w", system.ID, err),
			Done:     true,
		}
		return false
	}
	directoryPaths := scan.directoryPaths

	processed, matched, skipped := 0, 0, 0
	indexedSources, sourceErr := s.db.GetMediaSourcesForScrape(ctx, system.ID, opts.Scope)
	if sourceErr != nil {
		ch <- scraper.ScrapeUpdate{FatalErr: sourceErr, Done: true}
		return false
	}
	var sources *scraper.SourceIndex
	if len(indexedSources) > 0 {
		sources = scraper.NewSourceIndex(indexedSources)
	}
	if opts.Scope != nil {
		containers, err = scraper.ScopedContainers(ctx, s.db, system.ID, selection.Media)
		if err != nil {
			ch <- scraper.ScrapeUpdate{FatalErr: err, Done: true}
			return false
		}
	}
	total := scan.rows + len(directoryPaths)
	ch <- scraper.ScrapeUpdate{
		SystemID:    system.ID,
		Total:       total,
		TotalSteps:  len(systems),
		CurrentStep: systemIdx + 1,
	}

	// processRow scrapes one media row, returning false when the run ended.
	processRow := func(media *database.MediaWithFullPath) bool {
		if err := waitForScrape(ctx, opts); err != nil {
			ch <- scraper.ScrapeUpdate{FatalErr: err, Done: true}
			return false
		}

		if _, done := completed[media.DBID]; done {
			processed++
			skipped++
			ch <- scraper.ScrapeUpdate{
				SystemID: system.ID, Total: scan.rows,
				Processed: processed, Matched: matched, Skipped: skipped,
				TotalSteps: len(systems), CurrentStep: systemIdx + 1,
			}
			return true
		}
		roots, names, cleanupNames := mediaArtworkNames(media, system.ROMPaths, containers, sources, opts.Force)
		props := s.mediaPropsForNames(ctx, names, roots, availableDirs)
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
				return true
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
				return true
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
		return true
	}

	if opts.Scope != nil {
		for i := range selection.Media {
			if !processRow(&selection.Media[i]) {
				return false
			}
		}
	} else {
		// Rows are read a page at a time so writes can run between pages and
		// no more than a page is held, however large the system.
		afterPath := ""
		for {
			page, pageErr := s.db.GetMediaPageBySystemID(ctx, system.ID, afterPath, mediaPageSize)
			if pageErr != nil {
				if ctxErr := ctx.Err(); ctxErr != nil {
					pageErr = ctxErr
				}
				ch <- scraper.ScrapeUpdate{
					FatalErr: fmt.Errorf("localmedia: load media for %s: %w", system.ID, pageErr),
					Done:     true,
				}
				return false
			}
			for i := range page {
				if !processRow(&page[i]) {
					return false
				}
			}
			if len(page) < mediaPageSize {
				break
			}
			afterPath = page[len(page)-1].Path
		}
	}
	mediaCount := scan.rows

	directoryProps := make([]database.DirectoryProperty, 0)
	for _, directoryPath := range directoryPaths {
		if err := waitForScrape(ctx, opts); err != nil {
			ch <- scraper.ScrapeUpdate{FatalErr: err, Done: true}
			return false
		}

		props := s.directoryPropsForPath(ctx, directoryPath, system.ROMPaths, availableDirs)
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
		return false
	}

	if opts.Scope != nil {
		ch <- scraper.ScrapeUpdate{
			SystemID: system.ID, Total: mediaCount, Processed: processed, Matched: matched, Skipped: skipped,
			TotalSteps: 1, CurrentStep: 1, Done: true,
		}
		return false
	}
	return true
}

// hasArtworkDirs reports whether any root has a media directory an artwork
// lookup would search. Without one no media or folder lookup can find a file.
func hasArtworkDirs(availableDirs map[string]map[string]string) bool {
	for _, dirs := range availableDirs {
		if len(dirs) == 0 {
			continue
		}
		for _, propValue := range artworkPropertyOrder {
			for _, dir := range esmedia.DirectoryArtworkDirCandidates(string(propValue)) {
				if _, ok := dirs[dir]; ok {
					return true
				}
			}
		}
	}
	return false
}

// finishSystemWithoutArtwork completes an ordinary run over a system whose
// roots hold no artwork directories. Every row would be looked up and skipped,
// so none is read; the folder artwork snapshot is still replaced with the
// empty set such a run finds.
func (s *scraperImpl) finishSystemWithoutArtwork(
	ctx context.Context,
	opts scraper.ScrapeOptions,
	systems []scraper.ScrapeSystem,
	systemIdx int,
	ch chan<- scraper.ScrapeUpdate,
) bool {
	system := systems[systemIdx]
	ch <- scraper.ScrapeUpdate{SystemID: system.ID, TotalSteps: len(systems), CurrentStep: systemIdx + 1}
	err := s.replaceDirectoryPropertiesUnlessScoped(ctx, opts, system.DBID, make([]database.DirectoryProperty, 0))
	if err != nil {
		ch <- scraper.ScrapeUpdate{
			FatalErr:    fmt.Errorf("localmedia: replace directory properties for %s: %w", system.ID, err),
			SystemID:    system.ID,
			TotalSteps:  len(systems),
			CurrentStep: systemIdx + 1,
			Done:        true,
		}
		return false
	}
	return true
}

// mediaPageSize is how many media rows an ordinary run holds at once.
const mediaPageSize = 500

// launchTargetResolver answers which media row a directory launches as one
// game, from a whole system or from a scoped selection.
type launchTargetResolver interface {
	Resolve(dir string) *database.Media
}

// systemScan is what an ordinary run needs from every row of a system before
// it can process any one of them.
type systemScan struct {
	targets        container.LaunchTargetMap
	directoryPaths []string
	rows           int
}

// scanSystem streams a system's media once, collecting the directories that
// can carry folder artwork and each directory's launch target. It holds state
// per directory, not per row; the rows themselves are read again in pages.
func (s *scraperImpl) scanSystem(ctx context.Context, system scraper.ScrapeSystem) (systemScan, error) {
	directories := newDirectoryCollector(system.ROMPaths)
	targets := container.NewLaunchTargets()
	rows := 0
	err := s.db.ForEachMediaBySystemID(ctx, system.ID, func(m *database.MediaWithFullPath) error {
		rows++
		if m.IsMissing {
			return nil
		}
		directories.add(m.Path)
		targets.Add(&database.Media{
			DBID: m.DBID, MediaTitleDBID: m.MediaTitleDBID, Path: m.Path, ParentDir: m.ParentDir,
		})
		return nil
	})
	if err != nil {
		return systemScan{}, fmt.Errorf("stream media: %w", err)
	}
	return systemScan{rows: rows, directoryPaths: directories.paths(), targets: targets.Map()}, nil
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

func (s *scraperImpl) availableDirsByRoot(ctx context.Context, roots []string) map[string]map[string]string {
	result := make(map[string]map[string]string, len(roots))
	for _, root := range roots {
		if platforms.IsSourcePath(root) {
			if s.listings != nil {
				result[root] = statSourceMediaDirs(ctx, s.listings, root)
			}
			continue
		}
		result[root] = esmedia.StatMediaDirsFS(s.fs, root)
	}
	return result
}

// directoryCollector gathers the directories between present media and the
// root holding them, which are the directories folder artwork can describe.
type directoryCollector struct {
	directories map[string]struct{}
	roots       []string
	// rootAbsolute holds each root resolved once: filepath.Abs on a relative
	// root is a getwd syscall, so resolving it per row would charge a system
	// its row count in syscalls for an answer that never changes. An empty
	// entry marks a root that would not resolve.
	rootAbsolute []string
}

func newDirectoryCollector(roots []string) *directoryCollector {
	c := &directoryCollector{
		directories:  make(map[string]struct{}),
		roots:        roots,
		rootAbsolute: make([]string, len(roots)),
	}
	for i, root := range roots {
		if abs, err := filepath.Abs(root); err == nil {
			c.rootAbsolute[i] = filepath.Clean(abs)
		}
	}
	return c
}

// add records the directories above one present media path.
func (c *directoryCollector) add(path string) {
	for rootIndex, root := range c.roots {
		if platforms.IsSourcePath(root) {
			relative := sourceRelativeSegments(path, root)
			if relative == nil {
				continue
			}
			for _, dir := range sourceAncestorDirectories(path, root) {
				c.directories[dir] = struct{}{}
			}
			return
		}
		resolved := esmedia.ResolvePath(path, root)
		if resolved == "" {
			continue
		}
		rootAbs := c.rootAbsolute[rootIndex]
		if rootAbs == "" {
			return
		}
		dir := filepath.Dir(resolved)
		for dir != rootAbs && esmedia.PathWithinRoot(dir, rootAbs) {
			if dir == "." || dir == string(filepath.Separator) {
				return
			}
			c.directories[filepath.ToSlash(filepath.Clean(dir))] = struct{}{}
			parent := filepath.Dir(dir)
			if parent == dir {
				return
			}
			dir = parent
		}
		return
	}
}

func (c *directoryCollector) paths() []string {
	paths := make([]string, 0, len(c.directories))
	for path := range c.directories {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

// findArtworkFile searches every root's media/<subdir> candidates in order
// for the first fallback name that exists, dispatching each root to a real
// filesystem check or a source-root listing check depending on its kind -
// the one place esmedia.FindFileFS/FindFileAcrossRootsFS and their
// source-root equivalent findSourceFile actually get called from, so a
// system whose roots mix a real folder and a source root (both lookup
// against both on, like art living on a different drive than the rom) still
// works as one search.
func (s *scraperImpl) findArtworkFile(
	ctx context.Context, roots, candidates, fallbackNames []string, availableDirs map[string]map[string]string,
) *esmedia.File {
	for _, root := range roots {
		dirs := availableDirs[root]
		if len(dirs) == 0 {
			continue
		}
		var file *esmedia.File
		if platforms.IsSourcePath(root) {
			if s.listings != nil {
				file = findSourceFile(ctx, s.listings, fallbackNames, candidates, dirs)
			}
		} else {
			file = esmedia.FindFileIn(s.fs, s.dirNames, fallbackNames, candidates, dirs)
		}
		if file != nil {
			return file
		}
	}
	return nil
}

// directoryArtworkFallbackNames is directoryPropsForPath's fallback-name
// resolution, trying every root (a directory belongs to exactly one) with
// the matching scheme's helper.
func directoryArtworkFallbackNames(directoryPath string, roots []string) []string {
	for _, root := range roots {
		var names []string
		if platforms.IsSourcePath(root) {
			names = sourceDirectoryArtworkFallbackNames(directoryPath, root)
		} else {
			names = esmedia.DirectoryArtworkFallbackNames(directoryPath, root)
		}
		if len(names) > 0 {
			return names
		}
	}
	return nil
}

func (s *scraperImpl) directoryPropsForPath(
	ctx context.Context,
	directoryPath string,
	roots []string,
	availableDirs map[string]map[string]string,
) []database.DirectoryProperty {
	fallbackNames := directoryArtworkFallbackNames(directoryPath, roots)
	if len(fallbackNames) == 0 {
		return nil
	}

	props := make([]database.DirectoryProperty, 0)
	for _, propValue := range artworkPropertyOrder {
		file := s.findArtworkFile(
			ctx, roots, esmedia.DirectoryArtworkDirCandidates(string(propValue)), fallbackNames, availableDirs,
		)
		if file == nil {
			continue
		}
		propPath := directoryPath
		if !platforms.IsSourcePath(directoryPath) {
			propPath = filepath.ToSlash(filepath.Clean(directoryPath))
		}
		props = append(props, database.DirectoryProperty{
			Path:    propPath,
			TypeTag: tags.PropertyTypeTag(propValue),
			Text:    file.Path,
		})
	}
	return props
}

func (s *scraperImpl) mediaPropsForPath(
	ctx context.Context,
	path string,
	roots []string,
	availableDirs map[string]map[string]string,
	isContainerTarget bool,
) []database.MediaProperty {
	return s.mediaPropsForNames(ctx, artworkFallbackNames(path, roots, isContainerTarget), roots, availableDirs)
}

func (s *scraperImpl) mediaPropsForNames(
	ctx context.Context, fallbackNames, roots []string, availableDirs map[string]map[string]string,
) []database.MediaProperty {
	if len(fallbackNames) == 0 {
		return nil
	}

	props := make([]database.MediaProperty, 0)
	for _, propValue := range artworkPropertyOrder {
		file := s.findArtworkFile(
			ctx, roots, esmedia.ArtworkDirCandidates[string(propValue)], fallbackNames, availableDirs,
		)
		if file == nil {
			continue
		}
		props = append(props, database.MediaProperty{
			TypeTag:     tags.PropertyTypeTag(propValue),
			Text:        file.Path,
			ContentType: file.ContentType,
		})
	}
	return props
}

func isContainerLaunchTarget(containers launchTargetResolver, media *database.MediaWithFullPath) bool {
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
		if platforms.IsSourcePath(root) {
			names := sourceArtworkFallbackNames(path, root)
			if len(names) == 0 {
				continue
			}
			if isContainerTarget {
				names = append(names, sourceContainerArtworkFallbackNames(path, root)...)
			}
			return names
		}
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

	if platforms.IsSourcePath(prop.Text) {
		for _, root := range roots {
			if !platforms.IsSourcePath(root) {
				continue
			}
			for _, dir := range candidates {
				for _, name := range fallbackNames {
					if isSourceSegmentsPropForNames(prop.Text, root, dir, name) {
						return true
					}
				}
			}
		}
		return false
	}

	propPath := filepath.Clean(filepath.FromSlash(prop.Text))
	for _, root := range roots {
		if platforms.IsSourcePath(root) {
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
