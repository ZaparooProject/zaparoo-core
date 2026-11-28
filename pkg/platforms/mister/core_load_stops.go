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
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	misterconfig "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/mgls"
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
	menuAt        time.Time // a menu request a launch must wait out
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

// loading reports whether a launch's core may still be coming up: Main has
// reported no core since the launch, and the load window is still open.
func (c *coreLoadStops) loading(now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.launchedAt.IsZero() && c.coreChangedAt.Before(c.launchedAt) &&
		now.Before(c.launchedAt.Add(coreLoadWindow))
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

// requestMenu sends a menu request and records it, so the next launch waits
// for the menu to come up before sending its own load.
func (c *coreLoadStops) requestMenu(send func() error) error {
	c.mu.Lock()
	c.menuAt = time.Now()
	c.mu.Unlock()
	if err := send(); err != nil {
		c.mu.Lock()
		c.menuAt = time.Time{}
		c.mu.Unlock()
		return err
	}
	return nil
}

// Main starts a new process for every core it loads, the menu included, and
// only reads its command interface again once that process is up. A load
// sent before then is dropped: a card put straight back after a lift loses
// its game, and a second card tapped while the first one's core loads never
// launches. Main writes CORENAME once the new core is up, even when reloading
// the menu over itself, and a load sent the instant it does is still dropped:
// hardware, 2026-11-28, lost 6 of 6, while 0.1s to 1.2s later all landed.
// The menu is up in about 2.5s on an idle card, but SD load stretches that:
// 12s while a backup ran, over 40s under heavy reads. Any load sent before
// then is lost, so the wait is as long as a core load is allowed to take.
var (
	mainUpTimeout    = coreLoadWindow
	mainSettle       = 300 * time.Millisecond
	mainPollInterval = 50 * time.Millisecond
	mainCoreState    = coreNameWritten
	lastLoadSent     = mgls.LastLoadSent
)

// waitForMain holds a launch until Main has come up after the last menu
// request or core load Core sent it, then settled. It gives up
// mainUpTimeout after that request.
func (c *coreLoadStops) waitForMain() {
	c.mu.Lock()
	menu := c.menuAt
	c.mu.Unlock()
	requested := menu
	if load := lastLoadSent(); load.After(requested) {
		requested = load
	}
	if !requested.IsZero() {
		c.waitForMainSince(requested)
	}
	c.mu.Lock()
	if !menu.IsZero() && c.menuAt.Equal(menu) {
		c.menuAt = time.Time{}
	}
	c.mu.Unlock()
}

func (*coreLoadStops) waitForMainSince(requested time.Time) {
	deadline := requested.Add(mainUpTimeout)
	waited := false
	for {
		core, written, err := mainCoreState()
		// Any core written since the request means a Main is reading
		// commands again, usually the core asked for but possibly one Main
		// was already loading when it dropped the request.
		if err == nil && core != "" && !written.Before(requested) {
			if wait := time.Until(written.Add(mainSettle)); wait > 0 {
				time.Sleep(wait)
				waited = true
			}
			if waited {
				log.Debug().Str("core", core).Dur("since_request", time.Since(requested)).
					Msg("Main is up after the last request, launching")
			}
			return
		}
		if !time.Now().Before(deadline) {
			if waited {
				log.Warn().Msg("Main did not come up after the last request, launching anyway")
			}
			return
		}
		time.Sleep(mainPollInterval)
		waited = true
	}
}

// coreNameWritten reads the core Main reports and when it last wrote it.
func coreNameWritten() (string, time.Time, error) {
	data, err := os.ReadFile(misterconfig.CoreNameFile)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("read core name: %w", err)
	}
	info, err := os.Stat(misterconfig.CoreNameFile)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("stat core name: %w", err)
	}
	return strings.TrimSpace(string(data)), info.ModTime(), nil
}

// Main ignores commands until it has finished loading a core, and it reports
// the new core name before then, so the menu is asked for until Main shows it.
var (
	coreLoadMenuAttempts = 8
	coreLoadMenuInterval = 1500 * time.Millisecond
	coreLoadCoreName     = mistermain.GetActiveCoreName
	coreLoadSendMenu     = mistermain.LaunchMenu
	// stopCoreName is the core a stop checks for the menu.
	stopCoreName = mistermain.GetActiveCoreName
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
		if err := c.requestMenu(coreLoadSendMenu); err != nil {
			log.Warn().Err(err).Msg("failed to return to menu after a stop")
		}
	}
	log.Warn().Str("core", core).Msg("core a stop was meant for is still running")
}
