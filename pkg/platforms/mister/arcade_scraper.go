//go:build linux

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

package mister

import (
	"fmt"
	"path/filepath"
	"sync"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper/misterarcade"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/arcadedb"
)

// ArcadeSystemIDs returns the systems arcade descriptors are indexed under on
// this platform: Arcade itself, plus every granular hardware system
// classification splits out of it.
func ArcadeSystemIDs() []string {
	ids := make([]string, 0, len(misterArcadeSystemSpecs)+1)
	ids = append(ids, systemdefs.SystemArcade)
	for _, spec := range misterArcadeSystemSpecs {
		ids = append(ids, spec.systemID)
	}
	return ids
}

// ArcadeCatalog reads the platform's cached or embedded arcade catalog for the
// scraper.
func ArcadeCatalog(pl platforms.Platform) ([]misterarcade.Entry, error) {
	rows, err := arcadedb.ReadArcadeDb(pl)
	if err != nil {
		return nil, fmt.Errorf("read arcade catalog: %w", err)
	}
	return arcadeCatalogEntries(rows), nil
}

// arcadeCatalogEntries converts catalog rows to the scraper's own type. The
// scraper keeps a separate type because the catalog package is built only for
// the MiSTer platforms while the scraper is built everywhere.
func arcadeCatalogEntries(rows []arcadedb.ArcadeDbEntry) []misterarcade.Entry {
	entries := make([]misterarcade.Entry, 0, len(rows))
	for i := range rows {
		entries = append(entries, misterarcade.Entry{
			SetName:         rows[i].Setname,
			Region:          rows[i].Region,
			Version:         rows[i].Version,
			Alternative:     rows[i].Alternative,
			ParentTitle:     rows[i].ParentTitle,
			Platform:        rows[i].Platform,
			Series:          rows[i].Series,
			Homebrew:        rows[i].Homebrew,
			Bootleg:         rows[i].Bootleg,
			Year:            rows[i].Year,
			Manufacturer:    rows[i].Manufacturer,
			Category:        rows[i].Category,
			Resolution:      rows[i].Resolution,
			Rotation:        rows[i].Rotation,
			Players:         rows[i].Players,
			MoveInputs:      rows[i].MoveInputs,
			SpecialControls: rows[i].SpecialControls,
			NumButtons:      rows[i].NumButtons,
			Flip:            rows[i].Flip,
		})
	}
	return entries
}

// NewArcadeSetNameCache answers set-name lookups from the classification cache
// granular arcade indexing already wrote, so a scrape costs a map lookup per
// descriptor instead of re-reading thousands of them from SD storage. An entry
// only answers while the file still matches the size and mtime it was parsed
// at, and a cached empty set name is a real answer: that descriptor had none.
func NewArcadeSetNameCache(pl platforms.Platform) misterarcade.SetNameCache {
	path := filepath.Join(helpers.DataDir(pl), config.CacheDir, arcadeClassCacheFileName)
	var once sync.Once
	var entries map[string]arcadeClassCacheEntry
	return func(mraPath string) (string, bool) {
		once.Do(func() { entries = loadArcadeClassCache(path) })
		return cachedSetName(entries, mraPath)
	}
}

// NewArcadeScraper builds the arcade catalog scraper for a MiSTer platform.
// systems states which arcade systems that platform indexes.
func NewArcadeScraper(pl platforms.Platform, systems []string) platforms.Scraper {
	return misterarcade.NewPlatformScraper(systems, ArcadeCatalog, NewArcadeSetNameCache(pl))
}
