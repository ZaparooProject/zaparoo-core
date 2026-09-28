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

package mediascanner

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Issue #1572: a walk that ran for two hours on a 134k-entry folder left
// nothing in the log after "indexing system", so the stall looked like a
// hardware fault. A running walk must say so at Info, and say it is slow.
func TestWalkProgressReportsWhileRunning(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 9, 26, 15, 36, 17, 0, time.UTC)
	p := newWalkProgress("Amiga", "/media/usb0/games/Amiga", start, nil)

	assert.Equal(t, walkReport{}, p.observe(start.Add(time.Second), 10, "a"),
		"nothing is due in the first interval")
	assert.Equal(t, walkReport{status: true}, p.observe(start.Add(10*time.Second), 100, "b"),
		"a slow start alone is not yet a slow walk")

	assert.Equal(t, walkReport{progress: true, slow: true, status: true},
		p.observe(start.Add(walkProgressInterval), 300, "c"))
	assert.Equal(t, walkReport{}, p.observe(start.Add(walkProgressInterval+time.Second), 310, "d"),
		"progress waits for the next interval and the warning is given once")
	assert.Equal(t, walkReport{progress: true, status: true}, p.observe(start.Add(2*walkProgressInterval), 600, "e"))
}

func TestWalkProgressFastWalkIsNotSlow(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 9, 26, 15, 36, 17, 0, time.UTC)
	p := newWalkProgress("NES", "/media/fat/games/NES", start, nil)

	fast := int64(minWalkEntriesPerSec*walkProgressInterval.Seconds()) * 2
	report := p.observe(start.Add(walkProgressInterval), fast, "a")
	assert.Equal(t, walkReport{progress: true, status: true}, report)
	assert.False(t, p.warnedSlow.Load())
}

// A running walk feeds the indexing status clients show every
// walkStatusInterval, naming the folder being read.
func TestWalkProgressSendsStatusUpdates(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 9, 26, 15, 36, 17, 0, time.UTC)
	var got []walkUpdate
	p := newWalkProgress("Amiga", "/roms/Amiga", start, func(u walkUpdate) { got = append(got, u) })

	p.observe(start.Add(time.Second), 50, "/roms/Amiga/TOSEC/a.zip")
	assert.Empty(t, got, "no update before the first interval")
	p.observe(start.Add(walkStatusInterval), 120, "/roms/Amiga/TOSEC/b.zip")
	p.observe(start.Add(walkStatusInterval+time.Second), 130, "/roms/Amiga/TOSEC/c.zip")
	p.observe(start.Add(2*walkStatusInterval), 200, "/roms/Amiga/No-Intro/d.zip")
	assert.Equal(t, []walkUpdate{
		{dir: "/roms/Amiga/TOSEC", entries: 120},
		{dir: "/roms/Amiga/No-Intro", entries: 200},
	}, got)
}

// Issue #1572: a walk crawling for minutes is reported once as stalled, so the
// user can be told which folder it is stuck in. A healthy walk of a large
// library on MiSTer storage is never reported.
func TestWalkProgressReportsStallOnce(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 9, 26, 15, 36, 17, 0, time.UTC)
	var stalls []walkUpdate
	onUpdate := func(u walkUpdate) {
		if u.stalled {
			stalls = append(stalls, u)
		}
	}

	healthy := newWalkProgress("Amiga", "/roms/Amiga", start, onUpdate)
	healthy.observe(start.Add(walkStallElapsed), int64(270*walkStallElapsed.Seconds()), "/roms/Amiga/x/a.zip")
	assert.Empty(t, stalls, "270 entries/s is a healthy walk")

	crawling := newWalkProgress("Amiga", "/roms/Amiga", start, onUpdate)
	crawling.observe(start.Add(walkStallElapsed-time.Second), 100, "/roms/Amiga/x/a.zip")
	assert.Empty(t, stalls, "not judged before the stall window has passed")
	crawling.observe(start.Add(walkStallElapsed), 120, "/roms/Amiga/TOSEC/b.zip")
	crawling.observe(start.Add(walkStallElapsed+walkStatusInterval), 130, "/roms/Amiga/TOSEC/c.zip")
	assert.Equal(t, []walkUpdate{{dir: "/roms/Amiga/TOSEC", entries: 120, stalled: true}}, stalls)
}

// Issue #1572: with each entry costing tens of milliseconds, checking the
// pauser only every 200 entries let one work window run for seconds against a
// sleep capped at one second, so the throttled walk held a whole core.
func TestWalkProgressWaitIsDueByTimeAsWellAsCount(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 9, 26, 15, 36, 17, 0, time.UTC)
	p := newWalkProgress("Amiga", "/media/usb0/games/Amiga", start, nil)

	assert.False(t, p.waitDue(start.Add(time.Millisecond), 1))
	assert.True(t, p.waitDue(start.Add(time.Millisecond), walkEntryWaitInterval), "the entry count still triggers")
	assert.True(t, p.waitDue(start.Add(walkWaitInterval), 2), "a slow entry triggers by time")

	p.waited(start.Add(walkWaitInterval), start.Add(walkWaitInterval))
	assert.False(t, p.waitDue(start.Add(walkWaitInterval+time.Millisecond), 3))
	assert.True(t, p.waitDue(start.Add(2*walkWaitInterval), 4))
}

// A walk held by the pauser, paused while a game runs or throttled, is not
// slow storage: a walk paused for an hour mid-scan must not be reported as
// stalled or slow when it resumes at a healthy rate.
func TestWalkProgressLeavesPausedTimeOutOfTheRate(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 9, 26, 15, 36, 17, 0, time.UTC)
	var stalls []walkUpdate
	p := newWalkProgress("Amiga", "/roms/Amiga", start, func(u walkUpdate) {
		if u.stalled {
			stalls = append(stalls, u)
		}
	})

	p.observe(start.Add(time.Minute), 16000, "/roms/Amiga/a/x.zip")
	paused := start.Add(time.Minute)
	resumed := paused.Add(time.Hour)
	p.waited(paused, resumed)
	// A second worker held over the same hour overlaps and adds nothing.
	p.waited(paused.Add(time.Minute), resumed)

	report := p.observe(resumed.Add(5*time.Minute), 16000+270*300, "/roms/Amiga/b/y.zip")
	assert.False(t, report.stalled)
	assert.False(t, report.slow)
	assert.Empty(t, stalls)
	assert.Equal(t, int64(time.Hour), p.held.Load())

	crawling := newWalkProgress("Amiga", "/roms/Amiga", start, nil)
	crawling.waited(start, start.Add(time.Hour))
	report = crawling.observe(start.Add(time.Hour+walkStallElapsed), 1000, "/roms/Amiga/c/z.zip")
	assert.True(t, report.stalled, "a walk that crawls once resumed is still stalled")
}
