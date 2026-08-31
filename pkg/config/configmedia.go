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

package config

import (
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
)

// Values for IndexDuringMedia/IndexDuringStreamingMedia: how background
// media work (indexing, scraping) behaves while media is playing in the
// primary slot.
const (
	IndexDuringMediaThrottle = "throttle"
	IndexDuringMediaPause    = "pause"
)

type Media struct {
	FilenameTags   *bool    `toml:"filename_tags,omitempty"`
	DefaultRegions []string `toml:"default_regions,omitempty,multiline"`
	DefaultLangs   []string `toml:"default_langs,omitempty,multiline"`
}

// pauseByDefaultSystems lists the storage-streaming systems most sensitive
// to competing background I/O: on-device testing showed these still glitch
// or crash under a heavy throttle, so background media work defaults to a
// full pause (rather than throttling) while one of these plays.
var pauseByDefaultSystems = map[string]bool{
	systemdefs.System3DO:      true,
	systemdefs.SystemCDI:      true,
	systemdefs.SystemJaguarCD: true,
	systemdefs.SystemSaturn:   true,
}

// heavyThrottleSystems lists the remaining CD-based/optical systems that
// stream continuously from storage during play but tolerate a heavy
// throttle of background media work rather than requiring a full pause.
var heavyThrottleSystems = map[string]bool{
	systemdefs.SystemAmigaCD32:      true,
	systemdefs.SystemMegaCD:         true,
	systemdefs.SystemNeoGeoCD:       true,
	systemdefs.SystemPCFX:           true,
	systemdefs.SystemPSX:            true,
	systemdefs.SystemTurboGrafx16CD: true,
}

// IsStreamingSystem reports whether systemID streams continuously from
// storage during play (CD-based/optical cores), which needs a heavier
// indexing throttle (or pause) to avoid audible dropouts.
func IsStreamingSystem(systemID string) bool {
	return pauseByDefaultSystems[systemID] || heavyThrottleSystems[systemID]
}

// FilenameTags returns whether filename tag parsing is enabled.
func (c *Instance) FilenameTags() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.vals.Media.FilenameTags == nil {
		return true
	}
	return *c.vals.Media.FilenameTags
}

// SetFilenameTags sets whether filename tag parsing is enabled.
func (c *Instance) SetFilenameTags(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Media.FilenameTags = &enabled
}

// MediaPausePolicy is the resolved background-work policy for the currently
// active SystemID: whether to pause entirely or throttle, and at what
// throttle level.
type MediaPausePolicy struct {
	Mode  string
	Level syncutil.ThrottleLevel
}

// ResolveMediaPausePolicy returns the background media work policy to apply
// while systemID is playing in the primary slot. This is fixed per tier and
// not user-configurable: on-device testing showed pause-tier systems still
// glitch or crash under a heavy throttle, so letting it be overridden down
// to a throttle risks reintroducing that failure.
//   - pauseByDefaultSystems (the most storage-sensitive CD/optical cores)
//     get a full pause.
//   - heavyThrottleSystems (other CD/optical cores) get a heavy throttle.
//   - all other systems get a light throttle.
func (*Instance) ResolveMediaPausePolicy(systemID string) MediaPausePolicy {
	switch {
	case pauseByDefaultSystems[systemID]:
		return MediaPausePolicy{Mode: IndexDuringMediaPause, Level: syncutil.ThrottleHeavy}
	case heavyThrottleSystems[systemID]:
		return MediaPausePolicy{Mode: IndexDuringMediaThrottle, Level: syncutil.ThrottleHeavy}
	default:
		return MediaPausePolicy{Mode: IndexDuringMediaThrottle, Level: syncutil.ThrottleLight}
	}
}

// DefaultRegions returns the list of default regions for media matching.
func (c *Instance) DefaultRegions() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.vals.Media.DefaultRegions) == 0 {
		// TODO: raw strings for now to avoid import cycle
		// TODO: should this auto-detect the locale?
		return []string{"us", "world"}
	}
	return c.vals.Media.DefaultRegions
}

// DefaultLangs returns the list of default languages for media matching.
func (c *Instance) DefaultLangs() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.vals.Media.DefaultLangs) == 0 {
		// TODO: raw strings for now to avoid import cycle
		// TODO: should this auto-detect the locale?
		return []string{"en"}
	}
	return c.vals.Media.DefaultLangs
}
