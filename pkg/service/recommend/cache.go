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

package recommend

import (
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
)

const (
	// planCacheSize is how many plans are kept: a few profiles and filters.
	planCacheSize = 4
	// planMaxAge bounds how long a plan is served. Scraped tags and synced
	// history change without moving anything the key holds.
	planMaxAge = 10 * time.Minute
)

// PlanKey names everything a plan was built from. Two requests share a plan
// only when every part matches, so a different profile, a changed flag, a new
// play session or a reindex each build their own.
type PlanKey struct {
	ProfileID       string
	PreferencesRev  string
	HistoryVersion  string
	Day             string
	Systems         string
	IndexGeneration int64
	MaxResults      int
	MaxSections     int
	Queue           bool
}

type planEntry struct {
	built time.Time
	plan  *Plan
	key   PlanKey
}

// PlanCache holds recently built plans, most recently used first.
type PlanCache struct {
	entries []planEntry
	mu      syncutil.Mutex
}

// NewPlanCache returns an empty cache.
func NewPlanCache() *PlanCache {
	return &PlanCache{}
}

// Get returns the plan built for key, if one is held and still fresh.
func (c *PlanCache) Get(key *PlanKey, now time.Time) (*Plan, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.entries {
		if c.entries[i].key != *key {
			continue
		}
		entry := c.entries[i]
		if now.Sub(entry.built) > planMaxAge || now.Before(entry.built) {
			c.entries = append(c.entries[:i], c.entries[i+1:]...)
			return nil, false
		}
		copy(c.entries[1:i+1], c.entries[:i])
		c.entries[0] = entry
		return entry.plan, true
	}
	return nil, false
}

// Put stores the plan built for key, dropping the least recently used plan
// when the cache is full.
func (c *PlanCache) Put(key *PlanKey, plan *Plan, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.entries {
		if c.entries[i].key == *key {
			c.entries = append(c.entries[:i], c.entries[i+1:]...)
			break
		}
	}
	c.entries = append([]planEntry{{key: *key, plan: plan, built: now}}, c.entries...)
	if len(c.entries) > planCacheSize {
		c.entries = c.entries[:planCacheSize]
	}
}
