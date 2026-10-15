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

package kodi

import (
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/jonboulle/clockwork"
)

// Expectation is exported for the external watcher tests.
type Expectation = expectation

// QueueExpectation is a launch that plays a Kodi-side queue.
var QueueExpectation = expectation{queue: true}

// SongExpectation identifies a single song launch.
func SongExpectation(id int) Expectation { return expectation{itemType: "song", itemID: id} }

// NewWatchTestClient builds a client whose notification socket uses wsPort and
// whose watcher timing is controlled by the caller.
func NewWatchTestClient(
	serverURL, wsPort string, clock clockwork.Clock, firstPlay, grace time.Duration, settled func(),
) KodiClient {
	return &Client{
		url: strings.TrimSuffix(serverURL, "/") + "/jsonrpc",
		watch: &watchSettings{
			port:             wsPort,
			clock:            clock,
			firstPlayTimeout: firstPlay,
			grace:            grace,
			settled:          settled,
			dialTimeout:      dialTimeout,
		},
	}
}

// LaunchTracked exposes launchTracked.
func RunTracked(
	c KodiClient, opts *platforms.LaunchOptions, exp Expectation, launch func(KodiClient) error,
) error {
	return launchTracked(c, opts, exp, launch)
}

// WatcherActive reports whether a watcher is registered for the server URL.
func WatcherActive(serverURL string) bool {
	watchers.mu.Lock()
	defer watchers.mu.Unlock()
	return watchers.byURL[strings.TrimSuffix(serverURL, "/")+"/jsonrpc"] != nil
}

// SetRepeatHook runs fn after each successful repeat-off request.
func SetRepeatHook(c KodiClient, fn func()) { c.(*Client).watch.repeatOff = fn }

// SetDialTimeout overrides the notification dial cap on a test client.
func SetDialTimeout(c KodiClient, d time.Duration) { c.(*Client).watch.dialTimeout = d }
