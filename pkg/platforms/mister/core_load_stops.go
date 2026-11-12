//go:build linux && !android

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
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	misterconfig "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/mistermain"
	"github.com/rs/zerolog/log"
)

// coreLoadWindow is how long after a launch the core it asked for can still
// come up. Main takes several seconds to load a core, longer while the card
// is busy, and an arcade set's ROMs add to that.
const coreLoadWindow = 60 * time.Second

// coreLoadStops remembers a menu request made while a launched core was still
// loading. Main drops commands it receives during a core load, so that
// request never takes effect, and the core comes up running a game Core has
// already reported as stopped.
type coreLoadStops struct {
	launchedAt    time.Time
	coreChangedAt time.Time
	stopUntil     time.Time
	launches      uint64
	mu            syncutil.Mutex
}

// launched records that a launch has begun. It supersedes a stop still
// waiting for a core: the core about to load is the one now wanted.
func (c *coreLoadStops) launched(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.launchedAt = now
	c.launches++
	c.stopUntil = time.Time{}
}

// launchCount identifies the latest launch, so work done for a stop can tell
// that a launch has superseded it.
func (c *coreLoadStops) launchCount() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.launches
}

// stopWhenLoaded records a menu request. It is kept only while a launch's
// core has not come up yet; otherwise the request reached a running core.
func (c *coreLoadStops) stopWhenLoaded(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.launchedAt.IsZero() || !c.coreChangedAt.Before(c.launchedAt) {
		return
	}
	until := c.launchedAt.Add(coreLoadWindow)
	if !now.Before(until) {
		return
	}
	c.stopUntil = until
}

// coreChanged records the core Main reports and says whether it is a core
// that a stop was meant for. The first change after the stop settles it either
// way: the menu means the stop took effect.
func (c *coreLoadStops) coreChanged(core string, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.coreChangedAt = now
	until := c.stopUntil
	c.stopUntil = time.Time{}
	if until.IsZero() || core == misterconfig.MenuCore {
		return false
	}
	return now.Before(until)
}

// Main ignores commands until it has finished loading a core, and it reports
// the new core name before then, so the menu is asked for until Main shows it.
var (
	coreLoadMenuAttempts = 8
	coreLoadMenuInterval = 1500 * time.Millisecond
	coreLoadCoreName     = mistermain.GetActiveCoreName
	coreLoadSendMenu     = mistermain.LaunchMenu
)

// StopCoreLoadedAfterStop is told by the tracker of each core change. When the
// core is one a stop was meant for, it returns to the menu and reports true, so
// the tracker leaves the game unpublished.
func (p *Platform) StopCoreLoadedAfterStop(core string) bool {
	if !p.coreLoads.coreChanged(core, time.Now()) {
		return false
	}
	log.Info().Str("core", core).Msg("core came up after a stop meant for it, returning to menu")
	go p.coreLoads.menuUntilShown(core, p.coreLoads.launchCount())
	return true
}

// menuUntilShown asks Main for the menu until it is showing, giving up when
// another core or a new launch takes over: that is something the user wants.
func (c *coreLoadStops) menuUntilShown(core string, launches uint64) {
	for range coreLoadMenuAttempts {
		time.Sleep(coreLoadMenuInterval)
		if c.launchCount() != launches {
			return
		}
		current := coreLoadCoreName()
		if current == misterconfig.MenuCore || (current != "" && current != core) {
			return
		}
		if err := coreLoadSendMenu(); err != nil {
			log.Warn().Err(err).Msg("failed to return to menu after a stop")
		}
	}
	log.Warn().Str("core", core).Msg("core a stop was meant for is still running")
}
