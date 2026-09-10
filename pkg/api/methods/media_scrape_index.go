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

package methods

import (
	"fmt"
	"slices"
	"sort"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediascanner"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/google/uuid"
)

func scrapeSourceLaunchers(scrapers map[string]platforms.Scraper) []string {
	var ids []string
	for _, s := range scrapers {
		if s.SupportsFillMissing && s.Scrape != nil {
			ids = append(ids, s.AutoScrapeLaunchers...)
		}
	}
	sort.Strings(ids)
	return slices.Compact(ids)
}

func scrapeJobsForSources(
	scrapers map[string]platforms.Scraper, sources []mediascanner.IndexedSource,
) []database.ScrapeJob {
	var jobs []database.ScrapeJob
	for id, s := range scrapers {
		if !s.SupportsFillMissing || s.Scrape == nil {
			continue
		}
		var systems []string
		for _, source := range sources {
			if source.Files <= 0 || !slices.Contains(s.AutoScrapeLaunchers, source.LauncherID) {
				continue
			}
			if len(s.SupportedSystemIDs) > 0 && !slices.Contains(s.SupportedSystemIDs, source.SystemID) {
				continue
			}
			systems = append(systems, source.SystemID)
		}
		sort.Strings(systems)
		systems = slices.Compact(systems)
		if len(systems) > 0 {
			jobs = append(jobs, database.ScrapeJob{ScraperID: id, Systems: systems, FillMissing: true})
		}
	}
	slices.SortFunc(jobs, func(a, b database.ScrapeJob) int {
		if a.ScraperID < b.ScraperID {
			return -1
		}
		if a.ScraperID > b.ScraperID {
			return 1
		}
		return 0
	})
	return jobs
}

func sameScrapeJob(a, b *database.ScrapeJob) bool {
	if a.ScraperID != b.ScraperID || a.Force != b.Force || a.FillMissing != b.FillMissing {
		return false
	}
	// Scope is part of a job's identity. A single-file request and a
	// whole-system one name the same scraper and the same system, so ignoring
	// it here would let the dedupe below discard one of them.
	if !sameScrapeScope(a.Scope, b.Scope) {
		return false
	}
	x, y := slices.Clone(a.Systems), slices.Clone(b.Systems)
	sort.Strings(x)
	sort.Strings(y)
	return slices.Equal(slices.Compact(x), slices.Compact(y))
}

// sameScrapeScope compares two optional scopes, treating absent as unscoped.
func sameScrapeScope(a, b *database.ScrapeScope) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// enqueueScrapeJobs runs while indexing owns the media write lease. It only
// stores ordinary jobs; the existing service watcher runs them after optimization.
func enqueueScrapeJobs(db database.MediaDBI, jobs []database.ScrapeJob) error {
	if len(jobs) == 0 {
		return nil
	}
	s := scrapingStatusInstance
	s.mu.Lock()
	defer s.mu.Unlock()
	op, found, err := db.GetScrapingOperation()
	if err != nil {
		return fmt.Errorf("read queued scrape jobs: %w", err)
	}
	status := op.Status
	if found && (op.Version == 0 || status == "") {
		status, err = db.GetScrapingStatus()
		if err != nil {
			return fmt.Errorf("read queued scrape status: %w", err)
		}
	}
	var queued []database.ScrapeJob
	if found && op.IsResumable(status) {
		queued = append(queued, database.ScrapeJob{
			ScraperID: op.ScraperID, RunID: op.RunID,
			Systems: op.Systems, Force: op.Force, FillMissing: op.FillMissing,
			// A scoped run that is in flight when an index finishes is folded
			// back into the queue here. Dropping its scope would turn a
			// single-file request into a scrape of the whole system.
			Scope: op.Scope,
		})
		queued = append(queued, op.Pending...)
	}
	for i := range jobs {
		duplicate := slices.ContainsFunc(queued, func(job database.ScrapeJob) bool {
			return sameScrapeJob(&job, &jobs[i])
		})
		if !duplicate {
			job := jobs[i]
			job.RunID = uuid.NewString()
			queued = append(queued, job)
		}
	}
	first := queued[0]
	op = database.ScrapingOperation{
		Version: 1, Status: mediadb.IndexingStatusPending,
		ScraperID: first.ScraperID, Systems: first.Systems, RunID: first.RunID, Force: first.Force,
		FillMissing: first.FillMissing, Pending: queued[1:],
		// The head becomes the current operation again, scope included.
		Scope: first.Scope,
	}
	if err := op.Validate(); err != nil {
		return fmt.Errorf("validate scrape queue: %w", err)
	}
	if err := db.SetScrapingOperation(op); err != nil {
		return fmt.Errorf("persist scrape queue: %w", err)
	}
	if err := db.SetScrapingStatus(mediadb.IndexingStatusPending); err != nil {
		return fmt.Errorf("scrape queue saved but legacy status update failed: %w", err)
	}
	return nil
}
