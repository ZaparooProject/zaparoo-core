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

package database

import (
	"container/list"
	"slices"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
)

// BrowseListingCacheLimit is the number of listings BrowseListingCache keeps.
const BrowseListingCacheLimit = 32

// BrowseListingKey names one listing: what is listed, how it is scoped and
// ordered, and the visibility revision it was read under. Every field is a
// string so the key stays comparable; callers flatten slices into them.
type BrowseListingKey struct {
	Path          string
	Sources       string
	Systems       string
	RootView      string
	Sort          string
	Tags          string
	Revision      string
	IncludeHidden bool
}

// BrowseListingTotals are a whole listing's directory and file counts.
type BrowseListingTotals struct {
	Dirs  int
	Files int
}

type browseListingEntry struct {
	index  *BrowseIndexResult
	totals *BrowseListingTotals
	key    BrowseListingKey
}

// BrowseListingCache keeps what is expensive to work out about a listing and
// does not change between its pages: the letter index and the totals. It holds
// at most BrowseListingCacheLimit listings and evicts the least recently used.
// It is held in memory only, and the zero value is ready to use.
//
// A value computed while the rows changed must not outlive the change, so a
// caller takes a ticket from Begin before it reads, and a store whose ticket is
// out of date is dropped. Moving to a new visibility revision empties the cache,
// which also keeps it from filling up with listings no request can reach again.
type BrowseListingCache struct {
	entries    map[BrowseListingKey]*list.Element
	order      list.List
	revision   string
	generation uint64
	mu         syncutil.Mutex
}

// Begin returns the ticket for a read that runs under revision.
func (c *BrowseListingCache) Begin(revision string) uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.moveToRevisionLocked(revision)
	return c.generation
}

// Clear forgets every listing and invalidates outstanding tickets.
func (c *BrowseListingCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.clearLocked()
}

// ClearTagFiltered forgets the listings read through a tags filter, the only
// ones a tag write can change, and invalidates outstanding tickets. A scrape
// writes tags for every game it visits, so clearing everything would leave
// unfiltered listings with no cache for the whole run.
func (c *BrowseListingCache) ClearTagFiltered() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, element := range c.entries {
		if key.Tags != "" {
			c.order.Remove(element)
			delete(c.entries, key)
		}
	}
	c.generation++
}

// Len returns the number of listings held.
func (c *BrowseListingCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

// Totals returns a listing's cached totals.
func (c *BrowseListingCache) Totals(key *BrowseListingKey) (BrowseListingTotals, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry := c.touchLocked(key)
	if entry == nil || entry.totals == nil {
		return BrowseListingTotals{}, false
	}
	return *entry.totals, true
}

// StoreTotals records a listing's totals unless ticket has gone out of date.
func (c *BrowseListingCache) StoreTotals(key *BrowseListingKey, ticket uint64, totals BrowseListingTotals) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry := c.entryForStoreLocked(key, ticket); entry != nil {
		entry.totals = &totals
	}
}

// Index returns a copy of a listing's cached letter index.
func (c *BrowseListingCache) Index(key *BrowseListingKey) (BrowseIndexResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry := c.touchLocked(key)
	if entry == nil || entry.index == nil {
		return BrowseIndexResult{}, false
	}
	return cloneBrowseIndexResult(entry.index), true
}

// StoreIndex records a listing's letter index unless ticket has gone out of date.
func (c *BrowseListingCache) StoreIndex(key *BrowseListingKey, ticket uint64, index *BrowseIndexResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry := c.entryForStoreLocked(key, ticket); entry != nil {
		stored := cloneBrowseIndexResult(index)
		entry.index = &stored
	}
}

func cloneBrowseIndexResult(index *BrowseIndexResult) BrowseIndexResult {
	cloned := *index
	cloned.Buckets = slices.Clone(index.Buckets)
	return cloned
}

func (c *BrowseListingCache) moveToRevisionLocked(revision string) {
	if c.revision != revision {
		c.clearLocked()
		c.revision = revision
	}
}

func (c *BrowseListingCache) clearLocked() {
	c.entries = nil
	c.order.Init()
	c.generation++
}

func (c *BrowseListingCache) touchLocked(key *BrowseListingKey) *browseListingEntry {
	element, ok := c.entries[*key]
	if !ok {
		return nil
	}
	c.order.MoveToFront(element)
	entry, ok := element.Value.(*browseListingEntry)
	if !ok {
		return nil
	}
	return entry
}

func (c *BrowseListingCache) entryForStoreLocked(key *BrowseListingKey, ticket uint64) *browseListingEntry {
	if ticket != c.generation || key.Revision != c.revision {
		return nil
	}
	if entry := c.touchLocked(key); entry != nil {
		return entry
	}
	if c.entries == nil {
		c.entries = make(map[BrowseListingKey]*list.Element, BrowseListingCacheLimit)
	}
	if len(c.entries) >= BrowseListingCacheLimit {
		if oldest := c.order.Back(); oldest != nil {
			if evicted, ok := c.order.Remove(oldest).(*browseListingEntry); ok {
				delete(c.entries, evicted.key)
			}
		}
	}
	entry := &browseListingEntry{key: *key}
	c.entries[*key] = c.order.PushFront(entry)
	return entry
}
