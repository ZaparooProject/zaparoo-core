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
	"slices"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const appScraperID = "android-apps"

// appScraperSystems are the two systems installed-app launchers index into:
// the synced game system, and the browsable-only system Library sync never
// uploads (see installedAppsLauncherFor). The icon scraper covers both, so a
// non-game app still gets its on-device icon even though it is never synced.
//
//nolint:gochecknoglobals // immutable
var appScraperSystems = []string{systemdefs.SystemAndroid, systemdefs.SystemApplication}

// appScraper imports only on-device artwork. The package embedded in an app's
// canonical media identity remains the join key even when its label changes.
func (p *Platform) appScraper() platforms.Scraper {
	launchers := []string{installedAppsID, installedAppsNonGameID}
	for i := range p.entries {
		definition := &p.entries[i].definition
		if definition.Strategy == StrategyApp &&
			(definition.System == systemdefs.SystemAndroid || definition.System == systemdefs.SystemApplication) {
			launchers = append(launchers, definition.ID)
		}
	}
	return platforms.Scraper{
		ID: appScraperID, Name: "Installed Android apps",
		SupportedSystemIDs:  appScraperSystems,
		AutoScrapeLaunchers: launchers, SupportsFillMissing: true,
		Scrape: func(
			ctx context.Context, _ *config.Instance, _ platforms.Platform, _ afero.Fs,
			db *database.Database, opts scraper.ScrapeOptions, _ platforms.ScraperCustomOptions,
			ch chan<- scraper.ScrapeUpdate,
		) error {
			if opts.FillMissing && opts.Force {
				return errors.New("android-apps: fill-missing and force are mutually exclusive")
			}
			if p.host == nil || db == nil || db.MediaDB == nil {
				return errors.New("android-apps: host and media database are required")
			}
			go scrapeApps(ctx, p.host, db.MediaDB, opts, ch)
			return nil
		},
	}
}

// scrapeAppSystems is which of the two Android app systems a run covers: a
// scope names exactly one system; an unscoped request's own system filter
// (if any) narrows the pair; otherwise both are covered.
func scrapeAppSystems(opts scraper.ScrapeOptions) []string {
	if opts.Scope != nil {
		return []string{opts.Scope.SystemID}
	}
	ids := opts.SystemIDs()
	if len(ids) == 0 {
		return appScraperSystems
	}
	systems := make([]string, 0, len(appScraperSystems))
	for _, id := range appScraperSystems {
		if slices.Contains(ids, id) {
			systems = append(systems, id)
		}
	}
	return systems
}

type appScrapeCounts struct {
	processed, matched, skipped int
}

// scrapeApps runs scrapeAppsForSystem for each system this request covers,
// in the same single-step-per-system shape libretrothumbs' multi-system run
// already uses, so Core's progress UI shows one step per system rather than
// one run silently covering two.
func scrapeApps(
	ctx context.Context, host Host, db database.MediaDBI, opts scraper.ScrapeOptions,
	ch chan<- scraper.ScrapeUpdate,
) {
	defer close(ch)
	systems := scrapeAppSystems(opts)
	if len(systems) == 0 {
		ch <- scraper.ScrapeUpdate{Done: true, TotalSteps: 1, CurrentStep: 1}
		return
	}
	icons := make(map[string]string)
	var all appScrapeCounts
	for step, systemID := range systems {
		got, fatal := scrapeAppsForSystem(ctx, host, db, opts, systemID, icons, step+1, len(systems), ch)
		all.processed += got.processed
		all.matched += got.matched
		all.skipped += got.skipped
		if fatal != nil {
			ch <- scraper.ScrapeUpdate{
				Done: true, SystemID: systemID, Processed: all.processed, Total: all.processed,
				Matched: all.matched, Skipped: all.skipped, FatalErr: fatal,
				TotalSteps: len(systems), CurrentStep: step + 1,
			}
			return
		}
	}
	ch <- scraper.ScrapeUpdate{
		Done: true, Processed: all.processed, Total: all.processed, Matched: all.matched,
		Skipped: all.skipped, TotalSteps: len(systems), CurrentStep: len(systems),
	}
}

// scrapeAppsForSystem scrapes on-device icons for one system's rows. icons
// is shared package-keyed cache across every system this run covers, since a
// package's icon does not depend on which system its launcher filed it
// under. It answers its counts and a fatal error, exactly as
// libretrothumbs.scrapeSystem does for its own multi-system run.
func scrapeAppsForSystem(
	ctx context.Context, host Host, db database.MediaDBI, opts scraper.ScrapeOptions,
	systemID string, icons map[string]string, step, steps int, ch chan<- scraper.ScrapeUpdate,
) (appScrapeCounts, error) {
	var c appScrapeCounts
	var rows []database.MediaWithFullPath
	var completed map[int64]struct{}
	if opts.Scope != nil {
		selection, err := scraper.LoadScopedSelection(ctx, db, opts, appScraperID)
		if err != nil {
			return c, fmt.Errorf("android-apps: select scope: %w", err)
		}
		rows, _ = selection.Pending()
	} else {
		var err error
		rows, err = db.GetMediaBySystemID(systemID)
		if err != nil {
			return c, fmt.Errorf("android-apps: load media: %w", err)
		}
		if len(rows) == 0 {
			return c, nil
		}
		system, err := db.FindSystemBySystemID(systemID)
		if errors.Is(err, sql.ErrNoRows) {
			return c, nil
		}
		if err != nil {
			return c, fmt.Errorf("android-apps: look up system: %w", err)
		}
		switch {
		case opts.RunID != "" && (opts.Force || opts.FillMissing):
			completed, err = db.GetScrapeRunMediaIDs(ctx, appScraperID, opts.RunID, system.DBID)
		case !opts.Force && !opts.FillMissing:
			completed, err = db.GetScrapedMediaIDs(ctx, appScraperID, system.DBID)
		}
		if err != nil {
			return c, fmt.Errorf("android-apps: load markers: %w", err)
		}
	}
	report := func() {
		select {
		case ch <- scraper.ScrapeUpdate{
			SystemID: systemID, Processed: c.processed, Total: len(rows),
			Matched: c.matched, Skipped: c.skipped, TotalSteps: steps, CurrentStep: step,
		}:
		case <-ctx.Done():
		}
	}
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return c, fmt.Errorf("android-apps: %w", err)
		}
		if opts.Pauser != nil {
			if err := opts.Pauser.Wait(ctx); err != nil {
				return c, fmt.Errorf("android-apps: wait while paused: %w", err)
			}
		}
		if row.IsMissing {
			continue
		}
		if _, done := completed[row.DBID]; done {
			continue
		}
		identity, err := ParseAppPath(row.Path)
		if err != nil {
			continue
		}
		c.processed++
		if c.processed%25 == 0 {
			report()
		}
		icon := icons[identity.Package]
		if icon == "" {
			icon, err = host.AppIcon(identity.Package)
			if err != nil || icon == "" {
				log.Debug().Err(err).Str("package", identity.Package).Msg("android app icon unavailable")
				c.skipped++
				continue
			}
			icons[identity.Package] = icon
		}
		write := &database.ScrapeWrite{
			Sentinel: scraper.SentinelTagInfo(appScraperID), FillMissing: opts.FillMissing,
			MediaProps: []database.MediaProperty{{
				TypeTag: tags.PropertyTypeTag(tags.TagPropertyImageImage), Text: icon,
			}},
		}
		if opts.RunID != "" {
			write.MediaTags = append(write.MediaTags, scraper.RunTagInfo(appScraperID, opts.RunID))
		}
		if err := db.ApplyScrapeResult(ctx, row.DBID, row.MediaTitleDBID, write); err != nil {
			return c, fmt.Errorf("android-apps: write media %d: %w", row.DBID, err)
		}
		c.matched++
	}
	return c, nil
}
