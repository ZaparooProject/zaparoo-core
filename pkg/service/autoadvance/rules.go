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

package autoadvance

import (
	"context"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	uievents "github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/events"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog/log"
)

// Choice values of the countdown prompts, plus the two ways a countdown ends
// without one.
const (
	choiceExpire = "expire"
	choicePlay   = "play"
	choiceStop   = "stop"
	choiceStay   = "stay"
	choiceHalt   = "halt"
)

// send queues an automatic move from the item of from to next. The item it
// launches is marked as engine-started, and the move counts as activity for
// idle timing so the timer does not run out again while that launch is still
// starting. The move is remembered so it can be tried again if refused.
func (e *Engine) send(from, next *playlists.Playlist) {
	next.EngineStarted = true
	next.EngineFromIndex = from.Index
	next.EngineFromArmed = from.Armed
	next.StopSeq = e.host.StopSeq()
	next.StopStamped = true
	e.queueMove(next, from.Armed)
}

// clearPlaylist closes the playlist from is, the way a stop would, unless it
// is no longer the open one by the time the queue gets to it.
func (e *Engine) clearPlaylist(slot string, from *playlists.Playlist) {
	e.queueClear(slot, from, false)
}

// finishPlaylist closes a playlist that has nowhere left to go, so it starts
// over the next time it loads.
func (e *Engine) finishPlaylist(slot string, from *playlists.Playlist) {
	e.queueClear(slot, from, true)
}

func (e *Engine) queueClear(slot string, from *playlists.Playlist, completed bool) {
	e.queueMove(&playlists.Playlist{
		ID: from.ID, Slot: slot, Clear: true, Completed: completed, EngineStarted: true,
		EngineFromIndex: from.Index, EngineFromArmed: from.Armed, StopSeq: e.host.StopSeq(), StopStamped: true,
	}, from.Armed)
}

func (e *Engine) queueMove(upd *playlists.Playlist, wantArmed bool) {
	e.touch(e.clock.Now())
	if e.host.StopInProgress() {
		// The person stopping the media wins over whatever was decided while
		// it was going away.
		log.Debug().Msg("auto-advance: move not made, a stop is in progress")
		return
	}
	e.retry = &retryMove{upd: upd, wantArmed: wantArmed}
	if !e.host.Enqueue(e.ctx, upd) {
		log.Debug().Msg("auto-advance: playlist update not queued, service is stopping")
	}
}

// retryDue repeats a refused move once its delay is up, if the playlist is
// still where the move left it.
func (e *Engine) retryDue(now time.Time) {
	r := e.retry
	if r == nil || r.due.IsZero() || now.Before(r.due) {
		return
	}
	r.due = time.Time{}
	cur := e.playlistFor(slotOf(r.upd))
	wanted := cur != nil && cur.ID == r.upd.ID && cur.Index == r.upd.EngineFromIndex
	if r.wantArmed {
		wanted = wanted && cur.Armed
	} else {
		wanted = wanted && cur.Playing && !cur.PausedByFailure
	}
	if !wanted || e.host.StopSeq() > r.upd.StopSeq {
		e.retry = nil
		return
	}
	again := *r.upd
	r.upd = &again
	if !e.host.Enqueue(e.ctx, r.upd) {
		e.retry = nil
	}
}

// settle brings every derived piece of state in line with the playlist and the
// clock, then acts on whatever has come due.
func (e *Engine) settle(now time.Time) {
	pls := e.host.ActivePlaylist()
	e.stopWait = e.host.StopInProgress()
	if e.stopWait {
		e.touch(now)
	}
	if stops := e.host.StopSeq(); stops != e.seenStop {
		// Someone asked for a stop that the media events have not reported
		// yet. Whatever idle action is due now was decided without it.
		e.seenStop = stops
		e.touch(now)
	}
	e.syncIdle(pls, now)
	if e.idleOn && e.watch == nil {
		// Without a live input watch nothing is known about the user, so an
		// idle playlist must not count the silence. The time starts over once
		// the watch is back.
		e.touch(now)
	}
	if p := e.primary.pending; p != nil && !now.Before(p.deadline) {
		e.primary.pending = nil
	}
	e.syncAdvance(pls, now)
	e.fireDue(pls, now)
	e.syncWatch(now)
}

func (e *Engine) rearm(timer clockwork.Timer, now time.Time) {
	deadline, ok := e.nextDeadline(e.host.ActivePlaylist(), now)
	timer.Stop()
	if !ok {
		return
	}
	timer.Reset(max(deadline.Sub(now), 0))
}

// advanceDeadline is when the running advance timer of the bound item ends.
func (e *Engine) advanceDeadline(pls *playlists.Playlist) (time.Time, bool) {
	b := e.primary.bound
	if b == nil || b.advFired || b.advSince.IsZero() || pls == nil ||
		pls.ID != b.plsID || pls.Index != b.index || pls.Advance <= 0 {
		return time.Time{}, false
	}
	return b.advSince.Add(b.advLeft), true
}

func (e *Engine) idleDeadline(pls *playlists.Playlist) (time.Time, bool) {
	if e.cd != nil || e.watch == nil || e.idleAction(pls) == idleNone {
		return time.Time{}, false
	}
	return e.lastActivity.Add(pls.Idle), true
}

func (e *Engine) nextDeadline(pls *playlists.Playlist, now time.Time) (time.Time, bool) {
	if e.stopWait {
		// There is no event for a stop ending; look again shortly.
		return now.Add(stopPoll), true
	}
	var next time.Time
	consider := func(t time.Time, ok bool) {
		if ok && (next.IsZero() || t.Before(next)) {
			next = t
		}
	}
	if e.cd != nil {
		consider(e.cd.deadline, true)
	}
	consider(e.advanceDeadline(pls))
	consider(e.idleDeadline(pls))
	if e.input != nil && e.watch == nil && e.wantWatch() != watchNone && e.watchRetry.After(now) {
		consider(e.watchRetry, true)
	}
	if p := e.primary.pending; p != nil {
		consider(p.deadline, true)
	}
	if h := e.handoff; h != nil {
		consider(h.deadline, true)
	}
	if r := e.retry; r != nil && !r.due.IsZero() {
		consider(r.due, true)
	}
	return next, !next.IsZero()
}

func (e *Engine) fireDue(pls *playlists.Playlist, now time.Time) {
	if e.stopWait {
		// Nothing is decided while a stop runs; its completion is reported
		// through the media ending, and a failed stop leaves things as they
		// were.
		return
	}
	e.retryDue(now)
	if h := e.handoff; h != nil && !now.Before(h.deadline) {
		// Nothing took over: the game did quit.
		e.handoff = nil
		e.onExited(h.ended, h.endedAt)
		return
	}
	if c := e.cd; c != nil {
		if !now.Before(c.deadline) {
			e.resolveCountdown(c, choiceExpire)
		}
		return
	}
	if t, ok := e.advanceDeadline(pls); ok && !now.Before(t) {
		e.onAdvanceExpired(pls, e.primary.bound, now)
		return
	}
	if t, ok := e.idleDeadline(pls); ok && !now.Before(t) {
		e.onIdleExpired(pls, now)
	}
}

// syncAdvance keeps the advance timer of the bound item in step with the
// playlist: it starts when the item's media appeared, freezes while the
// playlist is paused and runs on when it resumes.
func (e *Engine) syncAdvance(pls *playlists.Playlist, now time.Time) {
	b := e.primary.bound
	if b == nil {
		return
	}
	if pls == nil || pls.ID != b.plsID || pls.Index != b.index || pls.Advance <= 0 {
		b.advSince = time.Time{}
		b.advTotal = 0
		return
	}
	if b.advTotal != pls.Advance {
		b.advTotal = pls.Advance
		b.advLeft = pls.Advance
		b.advSince = time.Time{}
		b.advFired = false
		if f := e.frozen; f != nil && f.sawPaused && f.plsID == b.plsID &&
			f.index == b.index && f.total == pls.Advance {
			b.advLeft = f.left
		}
		e.frozen = nil
	}
	running := pls.Playing && !pls.PausedByFailure && !b.advFired
	switch {
	case running && b.advSince.IsZero():
		b.advSince = now
		if !b.initAt.IsZero() {
			b.advSince = b.initAt
		}
	case !running && !b.advSince.IsZero():
		b.advLeft = max(b.advLeft-now.Sub(b.advSince), 0)
		b.advSince = time.Time{}
	}
	b.initAt = time.Time{}
}

// onAdvanceExpired switches away from an item whose playing time is up, with
// a countdown first when one can be shown.
func (e *Engine) onAdvanceExpired(pls *playlists.Playlist, b *bound, now time.Time) {
	b.advFired = true
	b.advSince = time.Time{}
	next, ok := nextAfterCompletion(pls)
	if !ok {
		// The last item of a playlist that does not repeat has nowhere to go:
		// the playlist ends and the game carries on.
		e.finishPlaylist(mediaslot.Primary, pls)
		return
	}
	if e.canAnnounce(pls) {
		e.startCountdown(&countdown{
			kind:  cdSwitchAdvance,
			slot:  mediaslot.Primary,
			plsID: pls.ID,
			index: pls.Index,
			item:  b,
		}, "Switching to "+itemLabel(next.Current()), pls.EffectiveCountdown(), now)
		return
	}
	e.send(pls, next)
}

// canAnnounce reports whether a countdown ahead of a switch can be shown.
func (e *Engine) canAnnounce(pls *playlists.Playlist) bool {
	return e.ui != nil && e.hasRenderer && pls.EffectiveCountdown() > 0 && !e.promptBlocked()
}

// promptBlocked reports whether another UI event is open. Opening a prompt
// would replace it, such as a launch confirmation the user has yet to answer,
// so the engine goes without a prompt instead.
func (e *Engine) promptBlocked() bool {
	if e.ui == nil {
		return false
	}
	open := e.ui.State().Events
	for i := range open {
		if open[i].ID != e.promptID {
			return true
		}
	}
	return false
}

// syncIdle follows the playlist's idle settings, and treats a playlist newly
// loaded, armed or replaced as a person having just done something.
func (e *Engine) syncIdle(pls *playlists.Playlist, now time.Time) {
	on := e.input != nil && pls != nil && pls.Idle > 0 && len(pls.Items) > 0 &&
		(pls.Armed || (pls.Playing && !pls.PausedByFailure))
	if on && (!e.idleOn || pls.ID != e.idleID || (pls.Armed && !e.idleArmed)) {
		e.touch(now)
	}
	e.idleOn = on
	e.idleArmed = on && pls.Armed
	if on {
		e.idleID = pls.ID
	} else {
		e.idleID = ""
	}
}

// idleAction says what running out of idle time would do now. Armed playlists
// only start when nothing is running, and a playing one never rotates away
// from media it did not launch.
func (e *Engine) idleAction(pls *playlists.Playlist) idleAction {
	if !e.idleOn || e.watch == nil || pls == nil {
		return idleNone
	}
	switch {
	case pls.Armed:
		if !e.primaryLive && e.primary.launching == nil {
			return idleStart
		}
	case pls.Playing && !pls.PausedByFailure:
		if !e.primaryLive {
			if e.primary.launching == nil {
				return idleAdvance
			}
			return idleNone
		}
		if b := e.primary.bound; b != nil && b.gen == e.primaryGen && b.plsID == pls.ID && b.index == pls.Index {
			return idleRotate
		}
	}
	return idleNone
}

func (e *Engine) onIdleExpired(pls *playlists.Playlist, now time.Time) {
	switch e.idleAction(pls) {
	case idleNone:
	case idleStart:
		log.Info().Str("playlist", pls.ID).Msg("auto-advance: idle, starting armed playlist")
		e.send(pls, playlists.Play(*pls))
	case idleAdvance:
		next, ok := nextAfterCompletion(pls)
		if !ok {
			e.finishPlaylist(mediaslot.Primary, pls)
			return
		}
		log.Info().Str("playlist", pls.ID).Msg("auto-advance: idle, moving to the next item")
		e.send(pls, next)
	case idleRotate:
		next, ok := nextAfterCompletion(pls)
		if !ok {
			e.finishPlaylist(mediaslot.Primary, pls)
			return
		}
		if e.canAnnounce(pls) {
			e.startCountdown(&countdown{
				kind:         cdSwitchIdle,
				slot:         mediaslot.Primary,
				plsID:        pls.ID,
				index:        pls.Index,
				item:         e.primary.bound,
				inputCancels: true,
			}, "Switching to "+itemLabel(next.Current()), pls.EffectiveCountdown(), now)
			return
		}
		log.Info().Str("playlist", pls.ID).Msg("auto-advance: idle, rotating to the next item")
		e.send(pls, next)
	}
}

// startCountdown opens a countdown that ends wait after at. It replaces any
// countdown already open.
func (e *Engine) startCountdown(c *countdown, title string, wait time.Duration, at time.Time) {
	e.cancelCountdown()
	e.cdSeq++
	c.id = e.cdSeq
	c.deadline = at.Add(wait)
	if c.kind != cdExit && e.input == nil {
		c.inputCancels = false
	}
	ctx, cancel := context.WithCancel(e.ctx)
	c.cancel = cancel
	e.cd = c

	if e.ui == nil || e.promptBlocked() {
		return
	}
	choices := []uievents.Choice{
		{Value: choicePlay, Label: "Play now"},
		{Value: choiceStop, Label: "Stop playlist"},
	}
	if c.kind != cdExit {
		choices = []uievents.Choice{
			{Value: choiceStay, Label: "Stay on this"},
			{Value: choiceHalt, Label: "Stop rotating"},
		}
	}
	handle, err := e.ui.Open(ctx, &uievents.Request{
		Kind:        models.UIEventKindPicker,
		Title:       title,
		Choices:     choices,
		Timeout:     wait,
		Dismissible: true,
	})
	if err != nil {
		log.Warn().Err(err).Msg("auto-advance: failed to open countdown prompt")
		return
	}
	e.promptID = handle.ID
	c.prompted = true
	id := c.id
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		select {
		case result, ok := <-handle.Results:
			if ok {
				e.post(&event{kind: evUIResult, gen: id, result: result})
			}
			cancel()
		case <-ctx.Done():
		}
	}()
}

// cancelCountdown drops the open countdown without acting, dismissing its
// prompt. A cancelled idle rotation also counts as the user being active.
func (e *Engine) cancelCountdown() {
	c := e.cd
	if c == nil {
		return
	}
	e.cd = nil
	c.cancel()
	if c.kind == cdSwitchIdle {
		e.touch(e.clock.Now())
	}
}

func (e *Engine) onUIResult(ev *event) {
	c := e.cd
	if c == nil || c.id != ev.gen {
		return
	}
	switch ev.result.Resolution.Outcome {
	case models.UIOutcomeSelected:
		choice, isChoice := ev.result.Value.(string)
		if !isChoice {
			return
		}
		e.resolveCountdown(c, choice)
	case models.UIOutcomeTimedOut:
		e.resolveCountdown(c, choiceExpire)
	case models.UIOutcomeDismissed:
		log.Debug().Msg("auto-advance: countdown dismissed")
		e.cancelCountdown()
	case models.UIOutcomeSuperseded:
		// Something else took the screen. After an exit the user is busy with
		// that, so the move is off; a rotation carries on without a prompt.
		if c.kind == cdExit {
			log.Debug().Msg("auto-advance: countdown cancelled, its prompt was replaced")
			e.cancelCountdown()
		}
	case models.UIOutcomeConfirmed, models.UIOutcomeCompleted, models.UIOutcomeCancelled:
		// The prompt is gone but nobody declined: the countdown runs on.
	}
}

// resolveCountdown ends the open countdown with choice and acts on it, after
// checking the playlist is still where the countdown left it.
func (e *Engine) resolveCountdown(c *countdown, choice string) {
	e.cd = nil
	if choice != choiceExpire || !c.prompted {
		c.cancel()
	}
	// An expired prompt is left to time out on its own, so clients see it
	// end the same way whichever side noticed first; its context is released
	// once that result arrives.
	pls := e.playlistFor(c.slot)
	if pls == nil || pls.ID != c.plsID || pls.Index != c.index || !pls.Playing {
		return
	}
	if c.kind == cdExit {
		e.resolveExit(pls, choice)
		return
	}
	if e.primary.bound != c.item {
		return
	}
	switch choice {
	case choiceStay:
		if c.kind == cdSwitchAdvance {
			c.item.advFired = false
			c.item.advLeft = c.item.advTotal
			c.item.advSince = time.Time{}
		} else {
			e.touch(e.clock.Now())
		}
	case choiceHalt:
		log.Info().Str("playlist", pls.ID).Msg("auto-advance: rotation stopped from the prompt")
		e.host.Enqueue(e.ctx, playlists.StopRotating(*pls))
	case choiceExpire:
		if next, ok := nextAfterCompletion(pls); ok {
			e.send(pls, next)
		}
	}
}

func (e *Engine) resolveExit(pls *playlists.Playlist, choice string) {
	switch choice {
	case choiceStop:
		e.clearPlaylist(mediaslot.Primary, pls)
	case choicePlay, choiceExpire:
		next, ok := nextAfterCompletion(pls)
		if !ok {
			e.finishPlaylist(mediaslot.Primary, pls)
			return
		}
		e.send(pls, next)
	}
}

// wantWatch is the input watch the engine needs right now: a responsive one
// while a countdown that a press cancels is open, else a lazy one while an
// idle playlist waits.
func (e *Engine) wantWatch() watchMode {
	if e.input == nil {
		return watchNone
	}
	if c := e.cd; c != nil && c.inputCancels {
		return watchResponsive
	}
	if e.idleOn {
		return watchIdle
	}
	return watchNone
}

func (e *Engine) syncWatch(now time.Time) {
	want := e.wantWatch()
	if w := e.watch; w != nil && (want == watchNone || w.responsive != (want == watchResponsive)) {
		e.stopWatch()
	}
	if want == watchNone || e.watch != nil || now.Before(e.watchRetry) {
		return
	}
	e.watchSeq++
	gen := e.watchSeq
	responsive := want == watchResponsive
	ctx, cancel := context.WithCancel(e.ctx)
	e.watch = &watch{gen: gen, responsive: responsive, cancel: cancel}
	source := e.input
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		err := source.WatchInput(ctx, responsive, func() {
			e.post(&event{kind: evInput, gen: gen})
		})
		if ctx.Err() == nil {
			if err != nil {
				log.Warn().Err(err).Msg("auto-advance: input watch failed")
			}
			e.post(&event{kind: evWatchEnded, gen: gen})
		}
	}()
}

func (e *Engine) stopWatch() {
	if e.watch == nil {
		return
	}
	e.watch.cancel()
	e.watch = nil
}
