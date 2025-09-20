// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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
	"fmt"
	"sync"

	scraperpkg "github.com/ZaparooProject/zaparoo-core/v2/pkg/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/scraper/igdb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/scraper/screenscraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/scraper/thegamesdb"
	"github.com/rs/zerolog/log"
)

// ScraperRegistry manages the registration and lookup of scraper implementations
type ScraperRegistry struct {
	scrapers map[string]scraperpkg.Scraper
	mu       sync.RWMutex
}

// NewScraperRegistry creates a new scraper registry
func NewScraperRegistry() *ScraperRegistry {
	registry := &ScraperRegistry{
		scrapers: make(map[string]scraperpkg.Scraper),
	}

	registry.registerDefaultScrapers()
	return registry
}

// registerDefaultScrapers registers all built-in scraper implementations
func (sr *ScraperRegistry) registerDefaultScrapers() {
	// Register ScreenScraper
	sr.Register("screenscraper", screenscraper.NewScreenScraper())

	// Register TheGamesDB
	sr.Register("thegamesdb", thegamesdb.NewTheGamesDB())

	// Register IGDB
	sr.Register("igdb", igdb.NewIGDB())

	log.Info().Int("count", len(sr.scrapers)).Msg("Registered scrapers")
}

// Register registers a scraper with the given name
func (sr *ScraperRegistry) Register(name string, scraper scraperpkg.Scraper) {
	sr.mu.Lock()
	defer sr.mu.Unlock()
	sr.scrapers[name] = scraper
	log.Debug().Str("name", name).Msg("Registered scraper")
}

// Get returns a scraper by name
func (sr *ScraperRegistry) Get(name string) (scraperpkg.Scraper, error) {
	sr.mu.RLock()
	defer sr.mu.RUnlock()
	scraper, exists := sr.scrapers[name]
	if !exists {
		return nil, fmt.Errorf("scraper '%s' not found", name)
	}
	return scraper, nil
}

// GetNames returns all registered scraper names
func (sr *ScraperRegistry) GetNames() []string {
	sr.mu.RLock()
	defer sr.mu.RUnlock()
	names := make([]string, 0, len(sr.scrapers))
	for name := range sr.scrapers {
		names = append(names, name)
	}
	return names
}

// HasScraper checks if a scraper with the given name is registered
func (sr *ScraperRegistry) HasScraper(name string) bool {
	sr.mu.RLock()
	defer sr.mu.RUnlock()
	_, exists := sr.scrapers[name]
	return exists
}

// Count returns the number of registered scrapers
func (sr *ScraperRegistry) Count() int {
	sr.mu.RLock()
	defer sr.mu.RUnlock()
	return len(sr.scrapers)
}