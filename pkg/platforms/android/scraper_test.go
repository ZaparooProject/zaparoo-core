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

package android

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/stretchr/testify/require"
)

type appScrapeDB struct {
	database.MediaDBI
	systemErr error
	completed map[int64]struct{}
	media     []database.MediaWithFullPath
	writes    []database.ScrapeWriteTarget
}

func (db *appScrapeDB) FindSystemBySystemID(string) (database.System, error) {
	if db.systemErr != nil {
		return database.System{}, db.systemErr
	}
	return database.System{DBID: 9}, nil
}

func (db *appScrapeDB) GetMediaBySystemID(string) ([]database.MediaWithFullPath, error) {
	return db.media, nil
}

func (db *appScrapeDB) GetScrapeRunMediaIDs(context.Context, string, string, int64) (map[int64]struct{}, error) {
	return db.completed, nil
}

func (db *appScrapeDB) GetScrapedMediaIDs(context.Context, string, int64) (map[int64]struct{}, error) {
	return db.completed, nil
}

func (db *appScrapeDB) ApplyScrapeResult(_ context.Context, mediaID, titleID int64, write *database.ScrapeWrite) error {
	db.writes = append(db.writes, database.ScrapeWriteTarget{
		MediaDBID: mediaID, MediaTitleDBID: titleID, Write: write,
	})
	return nil
}

func TestAppScraperFillsIconsByPackageWithoutChangingTitles(t *testing.T) {
	t.Parallel()
	host := &fakeHost{icons: map[string]string{"com.example.game": "/private/game.png"}}
	platform := &Platform{host: host}
	s := platform.appScraper()
	require.Equal(t, appScraperID, s.ID)
	require.Contains(t, s.AutoScrapeLaunchers, installedAppsID)
	require.True(t, s.SupportsFillMissing)
	db := &appScrapeDB{
		media: []database.MediaWithFullPath{
			{DBID: 1, MediaTitleDBID: 11, Path: (AppIdentity{Package: "com.example.game", Name: "Game"}).AppPath()},
			{
				DBID: 2, MediaTitleDBID: 12,
				Path: (AppIdentity{Package: "com.example.game", Variant: "arcade", Name: "Arcade"}).AppPath(),
			},
			{
				DBID: 3, MediaTitleDBID: 13,
				Path: (AppIdentity{Package: "com.example.missing", Name: "Missing"}).AppPath(),
			},
			{DBID: 4, MediaTitleDBID: 14, Path: "source://documents/something"},
		},
	}
	updates := make(chan scraper.ScrapeUpdate, 8)
	require.NoError(t, s.Scrape(t.Context(), nil, nil, nil, &database.Database{MediaDB: db},
		scraper.ScrapeOptions{FillMissing: true, RunID: "run-one", Systems: []string{systemdefs.SystemAndroid}},
		platforms.ScraperCustomOptions{}, updates))
	var last scraper.ScrapeUpdate
	for update := range updates {
		last = update
	}
	require.True(t, last.Done)
	require.NoError(t, last.FatalErr)
	require.Equal(t, 3, last.Processed)
	require.Equal(t, 3, last.Total, "a finished run reports its total, not zero")
	require.Equal(t, 2, last.Matched)
	require.Equal(t, 1, last.Skipped)
	require.Equal(t, []string{"com.example.game", "com.example.missing"}, host.iconCalls)
	require.Len(t, db.writes, 2)
	for _, target := range db.writes {
		require.True(t, target.Write.FillMissing)
		require.Empty(t, target.Write.TitleProps)
		require.Empty(t, target.Write.TitleTags)
		require.Equal(t, scraper.SentinelTagInfo(appScraperID), target.Write.Sentinel)
		require.Equal(t, []database.TagInfo{scraper.RunTagInfo(appScraperID, "run-one")}, target.Write.MediaTags)
		require.Equal(t, []database.MediaProperty{{
			TypeTag: tags.PropertyTypeTag(tags.TagPropertyImageImage), Text: "/private/game.png",
		}}, target.Write.MediaProps)
	}
}

func TestAppScraperDoesNotRevisitCompletedRows(t *testing.T) {
	t.Parallel()
	host := &fakeHost{icons: map[string]string{"com.example.game": "/private/game.png"}}
	p := &Platform{host: host}
	db := &appScrapeDB{
		media: []database.MediaWithFullPath{{
			DBID: 1, MediaTitleDBID: 11,
			Path: (AppIdentity{Package: "com.example.game", Name: "Game"}).AppPath(),
		}},
		completed: map[int64]struct{}{1: {}},
	}
	updates := make(chan scraper.ScrapeUpdate, 1)
	require.NoError(t, p.appScraper().Scrape(t.Context(), nil, nil, nil, &database.Database{MediaDB: db},
		scraper.ScrapeOptions{RunID: "run-one", FillMissing: true}, platforms.ScraperCustomOptions{}, updates))
	for update := range updates {
		require.NoError(t, update.FatalErr)
	}
	require.Empty(t, host.iconCalls)
	require.Empty(t, db.writes)
}

func TestAppScraperFinishesCleanlyWhenNoAppsAreIndexed(t *testing.T) {
	t.Parallel()
	host := &fakeHost{}
	p := &Platform{host: host}
	db := &appScrapeDB{}
	updates := make(chan scraper.ScrapeUpdate, 1)
	require.NoError(t, p.appScraper().Scrape(t.Context(), nil, nil, nil, &database.Database{MediaDB: db},
		scraper.ScrapeOptions{}, platforms.ScraperCustomOptions{}, updates))
	var last scraper.ScrapeUpdate
	for update := range updates {
		last = update
	}
	require.True(t, last.Done)
	require.NoError(t, last.FatalErr)
	require.Zero(t, last.Processed)
	require.Empty(t, host.iconCalls, "an empty library must never look up the Android system row")
}

func TestAppScraperFinishesCleanlyWhenAndroidSystemNotYetRegistered(t *testing.T) {
	t.Parallel()
	host := &fakeHost{icons: map[string]string{"com.example.game": "/private/game.png"}}
	p := &Platform{host: host}
	db := &appScrapeDB{
		media: []database.MediaWithFullPath{{
			DBID: 1, MediaTitleDBID: 11,
			Path: (AppIdentity{Package: "com.example.game", Name: "Game"}).AppPath(),
		}},
		systemErr: sql.ErrNoRows,
	}
	updates := make(chan scraper.ScrapeUpdate, 1)
	require.NoError(t, p.appScraper().Scrape(t.Context(), nil, nil, nil, &database.Database{MediaDB: db},
		scraper.ScrapeOptions{}, platforms.ScraperCustomOptions{}, updates))
	var last scraper.ScrapeUpdate
	for update := range updates {
		last = update
	}
	require.True(t, last.Done)
	require.NoError(t, last.FatalErr, "no Android system row yet is not a fatal error, just nothing to do")
	require.Zero(t, last.Processed)
	require.Empty(t, host.iconCalls)
}

func TestAppScraperPropagatesSystemLookupError(t *testing.T) {
	t.Parallel()
	host := &fakeHost{}
	p := &Platform{host: host}
	db := &appScrapeDB{
		media: []database.MediaWithFullPath{{
			DBID: 1, MediaTitleDBID: 11,
			Path: (AppIdentity{Package: "com.example.game", Name: "Game"}).AppPath(),
		}},
		systemErr: errors.New("db unavailable"),
	}
	updates := make(chan scraper.ScrapeUpdate, 1)
	require.NoError(t, p.appScraper().Scrape(t.Context(), nil, nil, nil, &database.Database{MediaDB: db},
		scraper.ScrapeOptions{}, platforms.ScraperCustomOptions{}, updates))
	var last scraper.ScrapeUpdate
	for update := range updates {
		last = update
	}
	require.True(t, last.Done)
	require.Error(t, last.FatalErr,
		"a real lookup failure must surface, not be swallowed like the not-yet-registered case")
}

func TestAppScraperReportsProgressEveryTwentyFiveRows(t *testing.T) {
	t.Parallel()
	host := &fakeHost{icons: map[string]string{"com.example.game": "/private/game.png"}}
	p := &Platform{host: host}
	media := make([]database.MediaWithFullPath, 30)
	for i := range media {
		media[i] = database.MediaWithFullPath{
			DBID: int64(i + 1), MediaTitleDBID: int64(i + 1),
			Path: (AppIdentity{Package: "com.example.game", Variant: fmt.Sprintf("v%d", i), Name: "Game"}).AppPath(),
		}
	}
	db := &appScrapeDB{media: media}
	updates := make(chan scraper.ScrapeUpdate, 40)
	require.NoError(t, p.appScraper().Scrape(t.Context(), nil, nil, nil, &database.Database{MediaDB: db},
		scraper.ScrapeOptions{Systems: []string{systemdefs.SystemAndroid}}, platforms.ScraperCustomOptions{}, updates))
	var sawProgressAt25 bool
	var last scraper.ScrapeUpdate
	for update := range updates {
		if !update.Done && update.Processed == 25 {
			sawProgressAt25 = true
		}
		last = update
	}
	require.True(t, sawProgressAt25, "a long-running scrape must report progress before it finishes")
	require.True(t, last.Done)
	require.NoError(t, last.FatalErr)
	require.Equal(t, 30, last.Processed)
	require.Equal(t, 30, last.Total, "a finished run reports its total, not zero")
	require.Equal(t, 30, last.Matched)
}

func TestAppScraperIncludesProfileLaunchers(t *testing.T) {
	t.Parallel()
	p := &Platform{host: &fakeHost{}, entries: []catalogEntry{{
		definition: LaunchDefinition{ID: "Android.Game", Strategy: StrategyApp, System: systemdefs.SystemAndroid},
	}}}
	s := p.appScraper()
	require.Contains(t, s.AutoScrapeLaunchers, "Android.Game")
	require.Contains(t, s.AutoScrapeLaunchers, installedAppsID)
	require.Contains(t, s.AutoScrapeLaunchers, installedAppsNonGameID)
	require.Contains(t, p.Scrapers(nil), appScraperID)
}

// Both installed-apps systems are covered, so a non-game app still gets its
// on-device icon even though Library sync never uploads it.
func TestAppScraperCoversBothInstalledAppsSystems(t *testing.T) {
	t.Parallel()
	require.ElementsMatch(t, []string{systemdefs.SystemAndroid, systemdefs.SystemApplication}, appScraperSystems)
	p := &Platform{host: &fakeHost{}}
	s := p.appScraper()
	require.ElementsMatch(t, appScraperSystems, s.SupportedSystemIDs)
}

// An unscoped run with no system filter scrapes both systems, sharing the
// icon cache across them: a package only looked up once either way.
func TestAppScraperRunsBothSystemsAndSharesIconCache(t *testing.T) {
	t.Parallel()
	host := &fakeHost{icons: map[string]string{"com.example.game": "/private/game.png"}}
	db := &appScrapeDB{
		media: []database.MediaWithFullPath{{
			DBID: 1, MediaTitleDBID: 11,
			Path: (AppIdentity{Package: "com.example.game", Name: "Game"}).AppPath(),
		}},
	}
	p := &Platform{host: host}
	updates := make(chan scraper.ScrapeUpdate, 8)
	require.NoError(t, p.appScraper().Scrape(t.Context(), nil, nil, nil, &database.Database{MediaDB: db},
		scraper.ScrapeOptions{}, platforms.ScraperCustomOptions{}, updates))
	var last scraper.ScrapeUpdate
	for update := range updates {
		last = update
	}
	require.True(t, last.Done)
	require.NoError(t, last.FatalErr)
	// The fake database answers the same one row for either system, so this
	// is exercising that both systems really ran, not asserting real content.
	require.Equal(t, 2, last.Processed)
	require.Equal(t, 2, last.Total, "a finished run reports its total, not zero")
	require.Equal(t, 2, last.Matched)
	require.Equal(t, 2, last.TotalSteps)
	require.Equal(t, 2, last.CurrentStep)
	require.Equal(t, []string{"com.example.game"}, host.iconCalls, "the icon cache is shared across both systems")
}
