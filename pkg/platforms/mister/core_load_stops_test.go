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
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	misterconfig "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/stretchr/testify/assert"
)

func TestCoreLoadStops(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 10, 26, 8, 15, 40, 0, time.UTC)
	at := start.Add

	tests := []struct {
		run  func(c *coreLoadStops) bool
		name string
		want bool
	}{
		{
			// Hardware, 2026-10-26 08:15: a stop 3 s into an NES launch was
			// dropped by Main and NES came up 9 s later.
			name: "core that comes up after a stop made during its load is stopped",
			run: func(c *coreLoadStops) bool {
				c.launched(at(0))
				c.stopWhenLoaded(at(3 * time.Second))
				return c.coreChanged("NES", at(9*time.Second))
			},
			want: true,
		},
		{
			name: "menu arriving first means the stop took effect",
			run: func(c *coreLoadStops) bool {
				c.launched(at(0))
				c.stopWhenLoaded(at(3 * time.Second))
				c.coreChanged(misterconfig.MenuCore, at(4*time.Second))
				return c.coreChanged("NES", at(20*time.Second))
			},
			want: false,
		},
		{
			name: "stop of a core that had already loaded is not held",
			run: func(c *coreLoadStops) bool {
				c.launched(at(0))
				c.coreChanged("NES", at(5*time.Second))
				c.stopWhenLoaded(at(10 * time.Second))
				return c.coreChanged("SNES", at(30*time.Second))
			},
			want: false,
		},
		{
			name: "a new launch supersedes the stop",
			run: func(c *coreLoadStops) bool {
				c.launched(at(0))
				c.stopWhenLoaded(at(3 * time.Second))
				c.launched(at(4 * time.Second))
				return c.coreChanged("NES", at(9*time.Second))
			},
			want: false,
		},
		{
			name: "core arriving after the window is the user's",
			run: func(c *coreLoadStops) bool {
				c.launched(at(0))
				c.stopWhenLoaded(at(3 * time.Second))
				return c.coreChanged("NES", at(coreLoadWindow+time.Second))
			},
			want: false,
		},
		{
			name: "stop with no launch is not held",
			run: func(c *coreLoadStops) bool {
				c.stopWhenLoaded(at(0))
				return c.coreChanged("NES", at(time.Second))
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var c coreLoadStops
			assert.Equal(t, tt.want, tt.run(&c))
		})
	}
}

// fakeMain stands in for Main's core name and command interface: it ignores
// the first ignored menu requests, as Main does while it finishes loading a
// core, then shows the menu.
type fakeMain struct {
	core    string
	ignored int
	sends   int
	mu      syncutil.Mutex
}

func (m *fakeMain) coreName() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.core
}

func (m *fakeMain) sendMenu() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sends++
	if m.sends > m.ignored {
		m.core = misterconfig.MenuCore
	}
	return nil
}

func withFakeMain(t *testing.T, m *fakeMain) {
	t.Helper()
	attempts, interval := coreLoadMenuAttempts, coreLoadMenuInterval
	name, send := coreLoadCoreName, coreLoadSendMenu
	coreLoadMenuInterval = time.Millisecond
	coreLoadCoreName, coreLoadSendMenu = m.coreName, m.sendMenu
	t.Cleanup(func() {
		coreLoadMenuAttempts, coreLoadMenuInterval = attempts, interval
		coreLoadCoreName, coreLoadSendMenu = name, send
	})
}

// Hardware, 2026-10-26 08:36: a menu request sent as NES came up was ignored
// and NES kept running.
func TestMenuUntilShownRetriesWhileMainIgnoresIt(t *testing.T) {
	m := &fakeMain{core: "NES", ignored: 2}
	withFakeMain(t, m)

	var c coreLoadStops
	c.menuUntilShown("NES", c.launchCount())

	assert.Equal(t, misterconfig.MenuCore, m.coreName())
	assert.Equal(t, 3, m.sends)
}

func TestMenuUntilShownLeavesAnotherCoreAlone(t *testing.T) {
	m := &fakeMain{core: "SNES"}
	withFakeMain(t, m)

	var c coreLoadStops
	c.menuUntilShown("NES", c.launchCount())

	assert.Zero(t, m.sends, "a different core is something the user started")
}

func TestMenuUntilShownStopsForANewLaunch(t *testing.T) {
	m := &fakeMain{core: "NES", ignored: 100}
	withFakeMain(t, m)

	var c coreLoadStops
	launches := c.launchCount()
	c.launched(time.Now())
	c.menuUntilShown("NES", launches)

	assert.Zero(t, m.sends, "the launch supersedes the stop")
}
