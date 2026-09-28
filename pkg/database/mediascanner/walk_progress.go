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
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/rs/zerolog/log"
)

const (
	// walkProgressInterval is how often a walk still running reports itself
	// in a default log. One system's walk can run for hours on a huge folder,
	// and without this the log says nothing between "indexing system" and the
	// end of the walk.
	walkProgressInterval = 30 * time.Second
	// A finished walk is slow when it averaged fewer than minWalkEntriesPerSec
	// over at least minSlowWalkElapsed. A 33K-entry directory legitimately
	// takes ~19s on MiSTer ARM + USB 2.0, so only a genuinely sluggish
	// filesystem trips it. A walk still running is only judged once it has run
	// for walkProgressInterval, so a slow first directory is not mistaken for a
	// slow walk.
	minSlowWalkElapsed   = 5 * time.Second
	minWalkEntriesPerSec = 500.0
	// walkWaitInterval bounds the time between pauser checks, alongside
	// walkEntryWaitInterval. The throttle sleeps in proportion to the work
	// window it just saw, capped at a second, so on storage where each entry
	// costs tens of milliseconds a 200-entry window ran for many seconds and
	// the walk held a core for ~90% of the time instead of the throttle's
	// share.
	walkWaitInterval = 20 * time.Millisecond
	// walkStatusInterval is how often a running walk updates the indexing
	// status clients show, so a long walk visibly moves.
	walkStatusInterval = 2 * time.Second
	// A walk is stalled once it has run for walkStallElapsed averaging fewer
	// than walkStallEntriesPerSec. The bar sits far below the slow-walk
	// warning: a healthy walk of a 134k-entry Amiga library on a MiSTer SD
	// card holds about 270 entries/s, and the #1572 walk that never finished
	// was well under 20.
	walkStallElapsed       = 5 * time.Minute
	walkStallEntriesPerSec = 100.0
)

// walkUpdate is what a running walk tells its caller.
type walkUpdate struct {
	// dir is the folder being read.
	dir     string
	entries int64
	// stalled is set once, on the update that finds the walk stalled.
	stalled bool
}

// walkProgress reports a directory walk while it runs: an Info line every
// walkProgressInterval, and one warning once the walk proves slow rather than
// only after it finishes, which a walk that never finishes cannot reach.
// It is safe for concurrent walk workers.
type walkProgress struct {
	start time.Time
	// heldUntil is the end of the latest pauser wait. Guarded by heldMu, with
	// held, the total time the walk has spent held by the pauser. Workers
	// waiting at once overlap, so only time past heldUntil is added.
	heldUntil time.Time
	// onUpdate, when set, receives a walkUpdate every walkStatusInterval and
	// when the walk is found stalled.
	onUpdate      func(walkUpdate)
	systemID      string
	root          string
	held          atomic.Int64
	lastReport    atomic.Int64
	lastWait      atomic.Int64
	lastStatus    atomic.Int64
	heldMu        syncutil.Mutex
	warnedSlow    atomic.Bool
	reportedStall atomic.Bool
}

func newWalkProgress(systemID, root string, start time.Time, onUpdate func(walkUpdate)) *walkProgress {
	p := &walkProgress{start: start, heldUntil: start, systemID: systemID, root: root, onUpdate: onUpdate}
	p.lastReport.Store(start.UnixNano())
	p.lastWait.Store(start.UnixNano())
	p.lastStatus.Store(start.UnixNano())
	return p
}

// waitDue reports whether the walk should check its pauser now, having
// scanned entries so far.
func (p *walkProgress) waitDue(now time.Time, entries int64) bool {
	return entries%walkEntryWaitInterval == 0 || now.UnixNano()-p.lastWait.Load() >= int64(walkWaitInterval)
}

// waited records a pauser check that began at began and returned at now. The
// time it held the walk, paused while a game runs or throttled, is not the
// storage's doing, so it is left out of the walk's rate.
func (p *walkProgress) waited(began, now time.Time) {
	p.lastWait.Store(now.UnixNano())
	p.heldMu.Lock()
	defer p.heldMu.Unlock()
	if began.Before(p.heldUntil) {
		began = p.heldUntil
	}
	if now.After(began) {
		p.held.Add(int64(now.Sub(began)))
		p.heldUntil = now
	}
}

// walkReport says what observe found due. It is returned for tests.
type walkReport struct {
	progress bool
	slow     bool
	status   bool
	stalled  bool
}

// observe records that entries have been scanned by now, with current the
// entry being visited, and logs whatever is due.
func (p *walkProgress) observe(now time.Time, entries int64, current string) walkReport {
	var report walkReport
	elapsed := now.Sub(p.start)
	// active is the time the walk was free to read. Rate and the slow and
	// stall judgements use it, so a walk paused for a game is not slow.
	active := elapsed - time.Duration(p.held.Load())
	last := p.lastReport.Load()
	if now.UnixNano()-last >= int64(walkProgressInterval) && p.lastReport.CompareAndSwap(last, now.UnixNano()) {
		report.progress = true
	}
	rate := float64(entries) / active.Seconds()
	if active >= walkProgressInterval && !p.warnedSlow.Load() &&
		rate < minWalkEntriesPerSec && p.warnedSlow.CompareAndSwap(false, true) {
		report.slow = true
	}
	if active >= walkStallElapsed && !p.reportedStall.Load() &&
		rate < walkStallEntriesPerSec && p.reportedStall.CompareAndSwap(false, true) {
		report.stalled = true
	}
	lastStatus := p.lastStatus.Load()
	if now.UnixNano()-lastStatus >= int64(walkStatusInterval) &&
		p.lastStatus.CompareAndSwap(lastStatus, now.UnixNano()) {
		report.status = true
	}
	if (report.status || report.stalled) && p.onUpdate != nil {
		p.onUpdate(walkUpdate{dir: filepath.Dir(current), entries: entries, stalled: report.stalled})
	}
	if report.stalled {
		log.Warn().
			Str("system", p.systemID).
			Str("path", p.root).
			Str("current", current).
			Int64("entriesScanned", entries).
			Dur("elapsed", elapsed).
			Float64("entriesPerSec", rate).
			Msg("directory walk has stalled")
	}
	if !report.progress && !report.slow {
		return report
	}

	if report.progress {
		log.Info().
			Str("system", p.systemID).
			Str("path", p.root).
			Str("current", current).
			Int64("entriesScanned", entries).
			Dur("elapsed", elapsed).
			Float64("entriesPerSec", rate).
			Msg("directory walk in progress")
	}
	if report.slow {
		log.Warn().
			Str("system", p.systemID).
			Str("path", p.root).
			Str("current", current).
			Int64("entriesScanned", entries).
			Dur("elapsed", elapsed).
			Float64("entriesPerSec", rate).
			Msg("directory walk is slow - possible stale mount or degraded storage; " +
				"an empty .zaparooignore file in a folder excludes it from indexing")
	}
	return report
}
