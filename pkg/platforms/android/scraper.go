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

// appScraper imports only on-device artwork. The package embedded in an app's
// canonical media identity remains the join key even when its label changes.
func (p *Platform) appScraper() platforms.Scraper {
	launchers := []string{installedAppsID}
	for i := range p.entries {
		definition := &p.entries[i].definition
		if definition.Strategy == StrategyApp && definition.System == systemdefs.SystemAndroid {
			launchers = append(launchers, definition.ID)
		}
	}
	return platforms.Scraper{
		ID: appScraperID, Name: "Installed Android apps",
		SupportedSystemIDs:  []string{systemdefs.SystemAndroid},
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

func scrapeApps(
	ctx context.Context, host Host, db database.MediaDBI, opts scraper.ScrapeOptions,
	ch chan<- scraper.ScrapeUpdate,
) {
	defer close(ch)
	finish := func(processed, matched, skipped int, err error) {
		ch <- scraper.ScrapeUpdate{
			Done: true, SystemID: systemdefs.SystemAndroid, Processed: processed, Total: processed,
			Matched: matched, Skipped: skipped, FatalErr: err, TotalSteps: 1, CurrentStep: 1,
		}
	}
	if ids := opts.SystemIDs(); len(ids) > 0 && !slices.Contains(ids, systemdefs.SystemAndroid) {
		finish(0, 0, 0, nil)
		return
	}
	var rows []database.MediaWithFullPath
	var completed map[int64]struct{}
	if opts.Scope != nil {
		selection, err := scraper.LoadScopedSelection(ctx, db, opts, appScraperID)
		if err != nil {
			finish(0, 0, 0, fmt.Errorf("android-apps: select scope: %w", err))
			return
		}
		rows, _ = selection.Pending()
	} else {
		var err error
		rows, err = db.GetMediaBySystemID(systemdefs.SystemAndroid)
		if err != nil {
			finish(0, 0, 0, fmt.Errorf("android-apps: load media: %w", err))
			return
		}
		if len(rows) == 0 {
			finish(0, 0, 0, nil)
			return
		}
		system, err := db.FindSystemBySystemID(systemdefs.SystemAndroid)
		if errors.Is(err, sql.ErrNoRows) {
			finish(0, 0, 0, nil)
			return
		}
		if err != nil {
			finish(0, 0, 0, fmt.Errorf("android-apps: look up system: %w", err))
			return
		}
		switch {
		case opts.RunID != "" && (opts.Force || opts.FillMissing):
			completed, err = db.GetScrapeRunMediaIDs(ctx, appScraperID, opts.RunID, system.DBID)
		case !opts.Force && !opts.FillMissing:
			completed, err = db.GetScrapedMediaIDs(ctx, appScraperID, system.DBID)
		}
		if err != nil {
			finish(0, 0, 0, fmt.Errorf("android-apps: load markers: %w", err))
			return
		}
	}
	processed, matched, skipped := 0, 0, 0
	icons := make(map[string]string)
	report := func() {
		select {
		case ch <- scraper.ScrapeUpdate{
			SystemID: systemdefs.SystemAndroid, Processed: processed, Total: len(rows),
			Matched: matched, Skipped: skipped, TotalSteps: 1, CurrentStep: 1,
		}:
		case <-ctx.Done():
		}
	}
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			finish(processed, matched, skipped, err)
			return
		}
		if opts.Pauser != nil {
			if err := opts.Pauser.Wait(ctx); err != nil {
				finish(processed, matched, skipped, err)
				return
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
		processed++
		if processed%25 == 0 {
			report()
		}
		icon := icons[identity.Package]
		if icon == "" {
			icon, err = host.AppIcon(identity.Package)
			if err != nil || icon == "" {
				log.Debug().Err(err).Str("package", identity.Package).Msg("android app icon unavailable")
				skipped++
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
			finish(processed, matched, skipped, fmt.Errorf("android-apps: write media %d: %w", row.DBID, err))
			return
		}
		matched++
	}
	finish(processed, matched, skipped, nil)
}
