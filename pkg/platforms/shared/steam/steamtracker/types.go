/*
Zaparoo Core
Copyright (c) 2026 The Zaparoo Project Contributors.
SPDX-License-Identifier: GPL-3.0-or-later

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

package steamtracker

import (
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
)

// DefaultPollInterval is the default interval for game state scanning.
const DefaultPollInterval = 2 * time.Second

// GameStartCallback is called when a Steam game starts.
// appID is the Steam App ID. pid is the process ID where available, or a
// platform lifecycle identifier when the OS does not expose one.
// gamePath is the game executable path.
type GameStartCallback func(appID int, pid int, gamePath string)

// GameStopCallback is called when a Steam game exits.
// pid identifies the stopped process or platform lifecycle instance.
type GameStopCallback func(appID, pid int)

type launchOwnership struct {
	mu    syncutil.Mutex
	appID int
	pid   int
}

func (o *launchOwnership) set(appID, pid int) {
	o.mu.Lock()
	o.appID = appID
	o.pid = pid
	o.mu.Unlock()
}

func (o *launchOwnership) clearIfMatches(appID, pid int) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.appID != appID || o.pid != pid {
		return false
	}
	o.appID = 0
	o.pid = 0
	return true
}

// TrackedGame represents a currently tracked Steam game.
type TrackedGame struct {
	StartTime time.Time
	GamePath  string
	AppID     int
	PID       int
}
