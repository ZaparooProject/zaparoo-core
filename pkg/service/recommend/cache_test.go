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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlanCache(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	base := PlanKey{
		ProfileID: "dad", PreferencesRev: "7", HistoryVersion: "40.0", Day: "2026-11-17",
		IndexGeneration: 3, MaxResults: 12, MaxSections: 6,
	}
	cache := NewPlanCache()
	plan := &Plan{}
	cache.Put(&base, plan, now)

	got, ok := cache.Get(&base, now.Add(time.Minute))
	require.True(t, ok)
	assert.Same(t, plan, got)

	changes := map[string]func(*PlanKey){
		"another profile":    func(k *PlanKey) { k.ProfileID = "kid" },
		"the shared profile": func(k *PlanKey) { k.ProfileID = "" },
		"a flag changed":     func(k *PlanKey) { k.PreferencesRev = "8" },
		"a session ended":    func(k *PlanKey) { k.HistoryVersion = "41.0" },
		"a reindex":          func(k *PlanKey) { k.IndexGeneration = 4 },
		"the next day":       func(k *PlanKey) { k.Day = "2026-11-18" },
		"other systems":      func(k *PlanKey) { k.Systems = "NES" },
		"another size":       func(k *PlanKey) { k.MaxResults = 5 },
		"another count":      func(k *PlanKey) { k.MaxSections = 2 },
	}
	for name, change := range changes {
		key := base
		change(&key)
		_, held := cache.Get(&key, now)
		assert.False(t, held, name)
	}

	_, ok = cache.Get(&base, now.Add(planMaxAge+time.Second))
	assert.False(t, ok, "a plan is not served past its age")
	_, ok = cache.Get(&base, now)
	assert.False(t, ok, "and is dropped once it has aged out")
}

func TestPlanCache_EvictsLeastRecentlyUsed(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	cache := NewPlanCache()
	keys := make([]PlanKey, planCacheSize+1)
	for i := range keys {
		keys[i] = PlanKey{MaxResults: i + 1}
	}
	for i := range planCacheSize {
		cache.Put(&keys[i], &Plan{}, now)
	}
	_, ok := cache.Get(&keys[0], now)
	require.True(t, ok)
	cache.Put(&keys[planCacheSize], &Plan{}, now)

	_, ok = cache.Get(&keys[0], now)
	assert.True(t, ok, "a plan read recently is kept")
	_, ok = cache.Get(&keys[1], now)
	assert.False(t, ok, "the plan unused longest makes room")

	replaced := &Plan{}
	cache.Put(&keys[0], replaced, now)
	got, ok := cache.Get(&keys[0], now)
	require.True(t, ok)
	assert.Same(t, replaced, got)
}
