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
