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
	"context"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediascanner"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeScrape satisfies the Scrape field; these tests only exercise job
// selection, which requires the callback to be set but never calls it.
func fakeScrape(
	context.Context, *config.Instance, platforms.Platform, afero.Fs, *database.Database,
	scraper.ScrapeOptions, platforms.ScraperCustomOptions, chan<- scraper.ScrapeUpdate,
) error {
	return nil
}

func autoScraper(id string, systems, launchers []string) platforms.Scraper {
	return platforms.Scraper{
		ID: id, Name: id, SupportedSystemIDs: systems,
		SupportsFillMissing: true, AutoScrapeLaunchers: launchers, Scrape: fakeScrape,
	}
}

func arcadeScrapers() map[string]platforms.Scraper {
	arcade := autoScraper(
		"mister-arcade",
		[]string{systemdefs.SystemArcade, systemdefs.SystemCPS1, systemdefs.SystemCPS2},
		[]string{systemdefs.SystemArcade, systemdefs.SystemCPS1, systemdefs.SystemCPS2},
	)
	// A manual scraper shares the same launchers but never opts in.
	manual := platforms.Scraper{
		ID: "gamelist.xml", Name: "gamelist.xml", Scrape: fakeScrape,
	}
	return map[string]platforms.Scraper{arcade.ID: arcade, manual.ID: manual}
}

func TestScrapeSourceLaunchersCollectsOnlyOptedInScrapers(t *testing.T) {
	t.Parallel()
	assert.Equal(t,
		[]string{systemdefs.SystemArcade, systemdefs.SystemCPS1, systemdefs.SystemCPS2},
		scrapeSourceLaunchers(arcadeScrapers()),
		"a manual scraper contributes no launchers to watch")
}

func TestScrapeSourceLaunchersIgnoresAnUnimplementedScraper(t *testing.T) {
	t.Parallel()
	broken := autoScraper("broken", nil, []string{systemdefs.SystemArcade})
	broken.Scrape = nil
	assert.Empty(t, scrapeSourceLaunchers(map[string]platforms.Scraper{broken.ID: broken}))
}

func TestScrapeJobsForSourcesQueuesEveryArcadeSystemIndexed(t *testing.T) {
	t.Parallel()
	// A MiSTer index contributes the Arcade walk plus each granular hardware
	// system classification splits out of it.
	jobs := scrapeJobsForSources(arcadeScrapers(), []mediascanner.IndexedSource{
		{LauncherID: systemdefs.SystemArcade, SystemID: systemdefs.SystemArcade, Files: 2600},
		{LauncherID: systemdefs.SystemCPS1, SystemID: systemdefs.SystemCPS1, Files: 173},
		{LauncherID: systemdefs.SystemSNES, SystemID: systemdefs.SystemSNES, Files: 900},
	})

	require.Len(t, jobs, 1)
	assert.Equal(t, "mister-arcade", jobs[0].ScraperID)
	assert.Equal(t, []string{systemdefs.SystemArcade, systemdefs.SystemCPS1}, jobs[0].Systems)
	assert.True(t, jobs[0].FillMissing, "an index-triggered job never replaces existing metadata")
	assert.False(t, jobs[0].Force)
}

func TestScrapeJobsForSourcesQueuesEverySystemForAnAllLaunchersScraper(t *testing.T) {
	t.Parallel()
	scrapers := arcadeScrapers()
	docs := platforms.Scraper{
		ID: "mister-docs", Name: "mister-docs", SupportedSystemIDs: []string{},
		SupportsFillMissing: true, AutoScrapeAllLaunchers: true, Scrape: fakeScrape,
	}
	scrapers[docs.ID] = docs
	assert.True(t, scrapeSourcesAllLaunchers(scrapers))
	assert.False(t, scrapeSourcesAllLaunchers(arcadeScrapers()))

	jobs := scrapeJobsForSources(scrapers, []mediascanner.IndexedSource{
		{LauncherID: systemdefs.SystemArcade, SystemID: systemdefs.SystemArcade, Files: 2600},
		{LauncherID: systemdefs.SystemSNES, SystemID: systemdefs.SystemSNES, Files: 900},
		{LauncherID: "custom", SystemID: systemdefs.SystemGenesis, Files: 0},
	})

	require.Len(t, jobs, 2)
	assert.Equal(t, "mister-arcade", jobs[0].ScraperID)
	assert.Equal(t, []string{systemdefs.SystemArcade}, jobs[0].Systems)
	assert.Equal(t, "mister-docs", jobs[1].ScraperID)
	assert.Equal(t, []string{systemdefs.SystemArcade, systemdefs.SystemSNES}, jobs[1].Systems)
	assert.True(t, jobs[1].FillMissing)
}

// Files of a launcher that cannot run here are still the library's: a scraper
// keyed on the files fills them in, and one bound to launchers does not.
func TestScrapeJobsForSourcesUnavailableLauncherOnlyForAllLaunchersScrapers(t *testing.T) {
	t.Parallel()
	scrapers := arcadeScrapers()
	gdb := platforms.Scraper{
		ID: "gamedatabase", Name: "gamedatabase",
		SupportsFillMissing: true, AutoScrapeAllLaunchers: true, Scrape: fakeScrape,
	}
	scrapers[gdb.ID] = gdb

	jobs := scrapeJobsForSources(scrapers, []mediascanner.IndexedSource{
		{LauncherID: systemdefs.SystemArcade, SystemID: systemdefs.SystemArcade, Files: 20, Unavailable: true},
		{LauncherID: "RetroArchNES", SystemID: systemdefs.SystemNES, Files: 42000, Unavailable: true},
	})

	require.Len(t, jobs, 1, "the arcade scraper reads launcher data an unavailable launcher lacks")
	assert.Equal(t, "gamedatabase", jobs[0].ScraperID)
	assert.Equal(t, []string{systemdefs.SystemArcade, systemdefs.SystemNES}, jobs[0].Systems)
}

// orderedScraper is an all-launchers index scraper at a given place in the
// queue.
func orderedScraper(id string, order int) platforms.Scraper {
	return platforms.Scraper{
		ID: id, Name: id, SupportsFillMissing: true, AutoScrapeAllLaunchers: true,
		AutoScrapeOrder: order, Scrape: fakeScrape,
	}
}

func misterScrapers() map[string]platforms.Scraper {
	scrapers := map[string]platforms.Scraper{}
	for _, s := range []platforms.Scraper{
		orderedScraper("mister-docs", scraper.AutoOrderDocuments),
		orderedScraper("gamedatabase", scraper.AutoOrderCatalog),
		orderedScraper("mister-arcade", scraper.AutoOrderDevice),
		orderedScraper("another-device-source", scraper.AutoOrderDevice),
	} {
		scrapers[s.ID] = s
	}
	return scrapers
}

func jobIDs(jobs []database.ScrapeJob) []string {
	ids := make([]string, 0, len(jobs))
	for i := range jobs {
		ids = append(ids, jobs[i].ScraperID)
	}
	return ids
}

func TestScrapeJobsForSourcesRunInScraperOrderThenByID(t *testing.T) {
	t.Parallel()
	jobs := scrapeJobsForSources(misterScrapers(), []mediascanner.IndexedSource{
		{LauncherID: systemdefs.SystemArcade, SystemID: systemdefs.SystemArcade, Files: 10},
	})
	assert.Equal(t, []string{"another-device-source", "mister-arcade", "gamedatabase", "mister-docs"}, jobIDs(jobs),
		"index jobs only fill what is missing, so the order decides which source keeps a shared field")
}

func TestSortScrapeQueueKeepsRequestedJobsFirstAndOrdersIndexJobs(t *testing.T) {
	t.Parallel()
	fill := func(id string) database.ScrapeJob { return database.ScrapeJob{ScraperID: id, FillMissing: true} }
	queue := []database.ScrapeJob{
		fill("gamedatabase"),
		{ScraperID: "mister-docs", Force: true},
		fill("mister-docs"),
		{ScraperID: "gamelist.xml"},
		fill("retired-scraper"),
		fill("mister-arcade"),
	}
	sortScrapeQueue(queue, misterScrapers())

	assert.Equal(t,
		[]string{"mister-docs", "gamelist.xml", "retired-scraper", "mister-arcade", "gamedatabase", "mister-docs"},
		jobIDs(queue))
	assert.True(t, queue[0].Force, "jobs a user asked for keep their order ahead of index jobs")
	assert.False(t, queue[1].FillMissing)
	assert.True(t, queue[5].FillMissing)
}

func TestScrapeJobsForSourcesIgnoresEmptyContributions(t *testing.T) {
	t.Parallel()
	// A launcher that matched nothing has no rows to enrich, and an
	// unsupported system is not this scraper's to fill.
	jobs := scrapeJobsForSources(arcadeScrapers(), []mediascanner.IndexedSource{
		{LauncherID: systemdefs.SystemArcade, SystemID: systemdefs.SystemArcade, Files: 0},
		{LauncherID: systemdefs.SystemCPS1, SystemID: systemdefs.SystemCPS3, Files: 10},
	})
	assert.Empty(t, jobs)
}
