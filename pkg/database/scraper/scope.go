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

package scraper

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/container"
)

// SystemIDs makes a resolved scope authoritative even for non-API callers.
func (o ScrapeOptions) SystemIDs() []string {
	if o.Scope != nil {
		return []string{o.Scope.SystemID}
	}
	return o.Systems
}

type ScopedSelection struct {
	Completed map[int64]struct{}
	Media     []database.MediaWithFullPath
	Titles    []database.TitleWithSystem
}

func LoadScopedSelection(
	ctx context.Context, db database.MediaDBI, opts ScrapeOptions, scraperID string,
) (ScopedSelection, error) {
	var result ScopedSelection
	if opts.Scope == nil {
		return result, errors.New("scoped scrape requires a scope")
	}
	rows, err := db.GetScrapeMedia(ctx, *opts.Scope)
	if err != nil {
		return result, fmt.Errorf("load scoped media: %w", err)
	}
	seenTitles := make(map[int64]bool)
	for i := range rows {
		row := &rows[i]
		result.Media = append(result.Media, database.MediaWithFullPath{
			DBID: row.DBID, MediaTitleDBID: row.MediaTitleDBID, Path: row.Path,
			ParentDir: row.ParentDir, SortName: row.SortName, SystemID: row.System.SystemID,
		})
		if !seenTitles[row.Title.DBID] {
			seenTitles[row.Title.DBID] = true
			result.Titles = append(result.Titles, database.TitleWithSystem{
				DBID: row.Title.DBID, SystemDBID: row.Title.SystemDBID, SystemID: row.System.SystemID,
				Slug: row.Title.Slug, Name: row.Title.Name,
			})
		}
	}
	if !opts.Force || opts.RunID != "" {
		runID := ""
		if opts.Force {
			runID = opts.RunID
		}
		result.Completed, err = db.GetScopedScrapeMediaIDs(ctx, *opts.Scope, scraperID, runID)
		if err != nil {
			return result, fmt.Errorf("load scoped markers: %w", err)
		}
	}
	return result, nil
}

// Pending keeps already-completed media out of matching and force cleanup.
func (s ScopedSelection) Pending() ([]database.MediaWithFullPath, []database.TitleWithSystem) {
	var media []database.MediaWithFullPath
	var titles []database.TitleWithSystem
	pendingTitles := make(map[int64]bool)
	for _, row := range s.Media {
		if _, done := s.Completed[row.DBID]; done {
			continue
		}
		media = append(media, row)
		pendingTitles[row.MediaTitleDBID] = true
	}
	for _, title := range s.Titles {
		if pendingTitles[title.DBID] {
			titles = append(titles, title)
		}
	}
	return media, titles
}

// ContainerResolver separates allowed write targets from directory context.
// A single selected row must not make an otherwise ambiguous folder collapse.
type ContainerResolver interface {
	Resolve(string) *database.Media
	HasMedia(string) bool
}

type scopedContainers struct {
	selected *container.Index
	targets  map[string]*database.Media
}

func (s *scopedContainers) HasMedia(path string) bool { return s.selected.HasMedia(path) }
func (s *scopedContainers) Resolve(path string) *database.Media {
	return s.targets[strings.TrimRight(filepath.ToSlash(path), "/")+"/"]
}

func ScopedContainers(
	ctx context.Context, db database.MediaDBI, systemID string, media []database.MediaWithFullPath,
) (ContainerResolver, error) {
	result := &scopedContainers{targets: make(map[string]*database.Media)}
	rows := make([]database.Media, 0, len(media))
	for _, m := range media {
		rows = append(rows, database.Media{DBID: m.DBID, Path: m.Path, ParentDir: m.ParentDir})
		parent := container.ParentDir(m.Path)
		if parent == "" || strings.Contains(parent, "://") {
			continue
		}
		if _, ok := result.targets[parent]; ok {
			continue
		}
		target, err := db.FindSingleContainerLaunchMediaBySystemID(ctx, systemID, parent)
		if err != nil {
			return nil, fmt.Errorf("resolve scoped container: %w", err)
		}
		result.targets[parent] = target
	}
	result.selected = container.NewIndex(rows)
	return result, nil
}

// ApplyScopedTargets counts selected media, not source records. It also checks
// write identities independently of scraper matching and deduplicates sources.
func ApplyScopedTargets(
	ctx context.Context, db database.MediaDBI, opts ScrapeOptions, selection ScopedSelection,
	targets []database.ScrapeWriteTarget, ch chan<- ScrapeUpdate,
) {
	allowed := make(map[int64]int64, len(selection.Media))
	for _, media := range selection.Media {
		allowed[media.DBID] = media.MediaTitleDBID
	}
	seen := make(map[int64]bool)
	pending := make([]database.ScrapeWriteTarget, 0, len(targets))
	for _, target := range targets {
		if title, ok := allowed[target.MediaDBID]; !ok || title != target.MediaTitleDBID {
			ch <- ScrapeUpdate{FatalErr: errors.New("scraper attempted an out-of-scope write"), Done: true}
			return
		}
		_, completed := selection.Completed[target.MediaDBID]
		if seen[target.MediaDBID] || completed {
			continue
		}
		seen[target.MediaDBID] = true
		pending = append(pending, target)
	}
	status := ScrapeUpdate{
		SystemID: opts.Scope.SystemID, Total: len(selection.Media), TotalSteps: 1, CurrentStep: 1,
		Processed: len(selection.Media) - len(pending), Skipped: len(selection.Media) - len(pending),
	}
	ch <- status
	for _, target := range pending {
		if err := opts.Pauser.Wait(ctx); err != nil {
			status.Done = true
			ch <- status
			return
		}
		if err := ctx.Err(); err != nil {
			status.Done = true
			ch <- status
			return
		}
		status.Err = db.ApplyScrapeResult(ctx, target.MediaDBID, target.MediaTitleDBID, target.Write)
		status.Processed++
		if status.Err != nil {
			status.Skipped++
		} else {
			status.Matched++
		}
		ch <- status
	}
	status.Done = true
	ch <- status
}
