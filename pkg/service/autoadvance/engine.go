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

// Package autoadvance moves playlists on without anyone pressing next.
//
// A playlist item is "bound" when the launch that played it published media
// in its slot: only then does the engine know that when that media ends, the
// item is what ended, and only then may it act. Media that appeared any other
// way, such as a game started from the menu, is never bound and is never
// touched. Every automatic move is also guarded: the playlist must still be
// the active one, on the same item, playing and not stopped by a failure, so
// a user who got there first always wins.
//
// What happens when bound media ends depends on why it ended. Media that
// finished moves the playlist on at once. A game that exited by itself gets a
// cancellable "up next" countdown, or, if it was started automatically and
// quit within seconds, pauses the playlist instead, since the user plainly did
// not want it. Anything Core stopped or replaced never advances.
//
// Two timers rotate a playlist while its media is still running. An advance
// timer runs from the moment an item's media appears and freezes while the
// playlist is paused. An idle timer measures time without input and only runs
// where the platform reports keyboard, mouse and controller activity; it
// starts a loaded ("armed") playlist, rotates a playing one, and carries on a
// playlist whose game has exited, but never rotates away from media the
// playlist did not launch.
//
// All state belongs to one goroutine. Observer callbacks and the other inputs
// only append to an unbounded queue, so they never block the code that
// reports them, and no event is ever dropped. Deadlines are never trusted to a
// timer firing: after every event and every wakeup the loop compares each
// deadline with the clock, so a late or spurious wakeup does no harm.
package autoadvance

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	uievents "github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/events"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog/log"
)

const (
	// pendingBindWindow is how long after an item's launch returns that media
	// published by an external lifecycle tracker still counts as the item's.
	pendingBindWindow = 30 * time.Second
	// handoffMaxRun is how briefly externally published media may run and
	// still count as a stage of a handoff rather than a game that was played;
	// handoffWindow is how long its replacement gets to appear.
	handoffMaxRun = 3 * time.Second
	// stopPoll is how often the engine looks for the end of a running stop.
	stopPoll      = 100 * time.Millisecond
	handoffWindow = 15 * time.Second
	// moveRetryDelay and moveMaxRetries pace the retries of a move the queue
	// handler refused because another launch was still settling.
	moveRetryDelay = 200 * time.Millisecond
	moveMaxRetries = 5
	// quickQuitWindow is how soon after an automatic start an exit counts as
	// the user rejecting the game.
	quickQuitWindow = 30 * time.Second
	// inputIgnoreWindow is how long after a game exits input is ignored, so
	// the button presses that quit it do not cancel the countdown.
	inputIgnoreWindow = 2 * time.Second
	// watchRetryDelay is how long to wait before reopening an input watch
	// that stopped on its own.
	watchRetryDelay = 30 * time.Second
)

// ErrIdleUnsupported is returned when idle rotation is asked for on a device
// that cannot see all of the input it would have to wait for.
var ErrIdleUnsupported = errors.New(
	"idle rotation is not available: this device cannot detect keyboard, mouse and controller activity",
)

// IdleSupported reports whether the platform can detect keyboard, mouse and
// controller activity, which idle rotation needs: without all three it would
// switch games under someone who is still playing.
func IdleSupported(pl platforms.Platform) bool {
	source := platforms.ResolveInputActivity(pl)
	return source != nil && source.InputCoverage().Has(platforms.InputAll)
}

// Host is what the engine needs from the rest of the service.
type Host interface {
	// ActivePlaylist and BackgroundPlaylist return the playlist open in each
	// slot, or nil.
	ActivePlaylist() *playlists.Playlist
	BackgroundPlaylist() *playlists.Playlist
	// ActiveMediaGeneration returns the generation of the primary media
	// playing now.
	ActiveMediaGeneration() (gen uint64, ok bool)
	// PausePlaylistIfCurrent pauses pls in slot if it is still the open one.
	PausePlaylistIfCurrent(slot string, pls *playlists.Playlist) bool
	// StopSeq counts the stops of primary media Core was asked to make, once
	// when each starts and again when it completes.
	StopSeq() uint64
	// StopInProgress reports whether a stop of the primary media is running.
	StopInProgress() bool
	// Enqueue sends an update to the playlist queue, giving up when ctx ends.
	Enqueue(ctx context.Context, pls *playlists.Playlist) bool
}

// UIOpener shows a UI event to the user.
type UIOpener interface {
	Open(ctx context.Context, request *uievents.Request) (*uievents.Handle, error)
	// State reports the UI events open now, so a prompt never replaces one
	// that is not the engine's.
	State() models.UIStateResponse
}

// Config wires an Engine.
type Config struct {
	Clock clockwork.Clock
	Host  Host
	// UI opens countdown prompts. It may be nil.
	UI UIOpener
	// Input reports user input. It may be nil, in which case input never
	// cancels a countdown and idle rotation cannot run.
	Input platforms.InputActivitySource
	// PublishesLater reports whether media from the launcher with this ID is
	// published by an external tracker after the launch call returns. Media
	// that waits on a launch is bound only if it says so. Nil accepts any
	// launcher.
	PublishesLater func(launcherID string) bool
	// HasRenderer reports that the platform shows UI events itself. Without
	// one a countdown before a rotation is skipped and the switch is made
	// silently; the countdown after an exit runs either way, because a client
	// may answer it.
	HasRenderer bool
}

type eventKind int

const (
	evMediaStarted eventKind = iota
	evMediaEnded
	evPlaylistChanged
	evLaunching
	evLaunched
	evUserActivity
	evInput
	evWatchEnded
	evUIResult
	evRefused
	evBarrier
)

type event struct {
	start  *state.MediaStart
	end    *state.MediaEnd
	done   chan struct{}
	result uievents.Result
	fn     func()
	pls    *playlists.Playlist
	at     time.Time
	slot   string
	plsID  string
	gen    uint64
	index  int
	kind   eventKind
	engine bool
	async  bool
	failed bool
}

// bound is a playlist item whose launch published media in its slot.
type bound struct {
	started  time.Time
	advSince time.Time
	// initAt is when the item's media appeared, used once to start its
	// advance timer from the moment of publication.
	initAt time.Time
	plsID  string
	media  models.ActiveMedia
	// advTotal is the Advance the timer was set up for; a change restarts it.
	advTotal time.Duration
	advLeft  time.Duration
	gen      uint64
	index    int
	// advFired marks a timer that ran out and is waiting on a switch or a
	// prompt.
	advFired      bool
	engineStarted bool
	// external marks media bound after its launch returned, published by an
	// external tracker.
	external bool
}

type launchWindow struct {
	plsID         string
	index         int
	engineStarted bool
	// bound is set once media was published inside the window.
	bound bool
}

// pendingBind is an item whose launcher publishes media itself, later and from
// its own goroutine, so the first media to appear within the window is the
// item's.
type pendingBind struct {
	deadline      time.Time
	plsID         string
	index         int
	engineStarted bool
}

// handoff is an item whose externally published media ended almost at once,
// as when LaunchBox hands a game to Steam: the media that replaces it is
// published by the same launch, so the item waits for it instead of treating
// the end as the game quitting.
type handoff struct {
	endedAt  time.Time
	deadline time.Time
	ended    *bound
}

// retryMove is the engine's latest move, kept in case the queue handler
// refuses it because another launch is still settling.
type retryMove struct {
	due       time.Time
	upd       *playlists.Playlist
	tries     int
	wantArmed bool
}

type slotState struct {
	launching *launchWindow
	pending   *pendingBind
	bound     *bound
}

type countdownKind int

const (
	cdExit countdownKind = iota
	cdSwitchAdvance
	cdSwitchIdle
)

type countdown struct {
	deadline time.Time
	// ignoreInputUntil keeps the presses that ended the game from cancelling.
	ignoreInputUntil time.Time
	cancel           context.CancelFunc
	item             *bound
	slot             string
	plsID            string
	id               uint64
	index            int
	kind             countdownKind
	// inputCancels is set when a press cancels it; scans cancel the same
	// countdowns.
	inputCancels bool
	// prompted is set when a prompt is open for it.
	prompted bool
}

type watch struct {
	cancel     context.CancelFunc
	gen        uint64
	responsive bool
}

// frozenAdvance is the time left on an item's advance timer when the item's
// media was stopped by a pause, so resuming continues the timer rather than
// restarting it.
type frozenAdvance struct {
	plsID     string
	total     time.Duration
	left      time.Duration
	index     int
	sawPaused bool
}

type watchMode int

const (
	watchNone watchMode = iota
	watchIdle
	watchResponsive
)

type idleAction int

const (
	idleNone idleAction = iota
	// idleStart starts a loaded playlist.
	idleStart
	// idleAdvance moves on from a game that has exited.
	idleAdvance
	// idleRotate switches away from the playlist's own running media.
	idleRotate
)

// Engine is the auto-advance state machine. Create one with New. A nil
// *Engine accepts every call and does nothing, so callers never need to check
// whether it is wired.
type Engine struct {
	clock        clockwork.Clock
	host         Host
	ui           UIOpener
	input        platforms.InputActivitySource
	ctx          context.Context
	cancel       context.CancelFunc
	wake         chan struct{}
	cd           *countdown
	watch        *watch
	frozen       *frozenAdvance
	retry        *retryMove
	handoff      *handoff
	publishes    func(string) bool
	promptID     string
	lastActivity time.Time
	watchRetry   time.Time
	primary      slotState
	background   slotState
	idleID       string
	queue        []event
	mu           syncutil.Mutex
	wg           sync.WaitGroup
	cdSeq        uint64
	primaryGen   uint64
	seenStop     uint64
	watchSeq     uint64
	hasRenderer  bool
	closed       bool
	idleOn       bool
	idleArmed    bool
	primaryLive  bool
	stopWait     bool
}

// New creates an engine. Call Start to run it.
func New(cfg *Config) *Engine {
	clock := cfg.Clock
	if clock == nil {
		clock = clockwork.NewRealClock()
	}
	return &Engine{
		clock:       clock,
		host:        cfg.Host,
		ui:          cfg.UI,
		input:       cfg.Input,
		hasRenderer: cfg.HasRenderer,
		publishes:   cfg.PublishesLater,
		wake:        make(chan struct{}, 1),
	}
}

// Start runs the engine until ctx is cancelled or Stop is called.
func (e *Engine) Start(ctx context.Context) {
	if e == nil {
		return
	}
	e.ctx, e.cancel = context.WithCancel(ctx)
	if gen, ok := e.host.ActiveMediaGeneration(); ok {
		e.primaryGen = gen
		e.primaryLive = true
	}
	e.lastActivity = e.clock.Now()
	e.seenStop = e.host.StopSeq()
	e.wg.Add(1)
	go e.run()
}

// Stop ends the engine and waits for everything it started.
func (e *Engine) Stop() {
	if e == nil || e.cancel == nil {
		return
	}
	e.cancel()
	e.wg.Wait()
}

// MediaStarted implements state.MediaObserver.
func (e *Engine) MediaStarted(start state.MediaStart) { //nolint:gocritic // the observer interface passes it by value
	e.post(&event{kind: evMediaStarted, start: &start})
}

// MediaEnded implements state.MediaObserver.
func (e *Engine) MediaEnded(end state.MediaEnd) { //nolint:gocritic // the observer interface passes it by value
	e.post(&event{kind: evMediaEnded, end: &end})
}

// PlaylistChanged reports that the playlist in slot was replaced, cleared or
// updated.
func (e *Engine) PlaylistChanged(slot string) {
	e.post(&event{kind: evPlaylistChanged, slot: slot})
}

// ItemLaunching reports that the launch of the item pls is on is starting.
// Media published until ItemLaunched is bound to the item.
func (e *Engine) ItemLaunching(pls *playlists.Playlist) {
	if e == nil || pls == nil {
		return
	}
	e.post(&event{kind: evLaunching, slot: slotOf(pls), plsID: pls.ID, index: pls.Index, engine: pls.EngineStarted})
}

// ItemLaunched reports that the launch started by ItemLaunching has returned.
//
// publishesLater says the item's launcher publishes its media itself after the
// launch call returns, as external lifecycle launchers do. For those the first
// primary media to appear in the next 30 seconds is bound to the item; for any
// other launcher media that did not appear during the launch is not the item's.
func (e *Engine) ItemLaunched(pls *playlists.Playlist, err error, publishesLater bool) {
	if e == nil || pls == nil {
		return
	}
	e.post(&event{
		kind: evLaunched, slot: slotOf(pls), plsID: pls.ID, index: pls.Index,
		async: publishesLater, failed: err != nil,
	})
}

// ItemRefused reports that the queue handler refused the engine's update pls
// because another launch was still under way. The engine tries the move again
// shortly, if it is still wanted.
func (e *Engine) ItemRefused(pls *playlists.Playlist) {
	if e == nil || pls == nil {
		return
	}
	e.post(&event{kind: evRefused, pls: pls})
}

// Sync waits until every event posted before the call has been handled and
// the engine has acted on it. It exists so tests can assert that nothing
// happened without sleeping.
func (e *Engine) Sync() {
	if e == nil || e.ctx == nil {
		return
	}
	done := make(chan struct{})
	e.post(&event{kind: evBarrier, done: done})
	select {
	case <-done:
	case <-e.ctx.Done():
	}
}

// NoteUserActivity reports a token scan or removal.
func (e *Engine) NoteUserActivity() {
	e.post(&event{kind: evUserActivity})
}

func slotOf(pls *playlists.Playlist) string {
	if pls.Slot == mediaslot.Background {
		return mediaslot.Background
	}
	return mediaslot.Primary
}

func (e *Engine) post(ev *event) {
	if e == nil {
		return
	}
	if ev.at.IsZero() {
		ev.at = e.clock.Now()
	}
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return
	}
	e.queue = append(e.queue, *ev)
	e.mu.Unlock()
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

func (e *Engine) take() []event {
	e.mu.Lock()
	defer e.mu.Unlock()
	batch := e.queue
	e.queue = nil
	return batch
}

func (e *Engine) run() {
	defer e.wg.Done()
	timer := e.clock.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()
	for {
		batch := e.take()
		for i := range batch {
			e.handle(&batch[i])
		}
		now := e.clock.Now()
		e.settle(now)
		e.rearm(timer, now)
		for i := range batch {
			if batch[i].done != nil {
				close(batch[i].done)
			}
		}
		select {
		case <-e.ctx.Done():
			e.shutdown()
			return
		case <-e.wake:
		case <-timer.Chan():
		}
	}
}

func (e *Engine) shutdown() {
	e.mu.Lock()
	e.closed = true
	batch := e.queue
	e.queue = nil
	e.mu.Unlock()
	for i := range batch {
		if batch[i].done != nil {
			close(batch[i].done)
		}
	}
	e.cancelCountdown()
	e.stopWatch()
}

func (e *Engine) handle(ev *event) {
	switch ev.kind {
	case evMediaStarted:
		e.onMediaStarted(ev)
	case evMediaEnded:
		e.onMediaEnded(ev)
	case evPlaylistChanged:
		e.onPlaylistChanged(ev.slot, ev.at)
	case evLaunching:
		e.onLaunching(ev)
	case evLaunched:
		e.onLaunched(ev)
	case evRefused:
		e.onRefused(ev)
	case evUserActivity:
		e.touch(ev.at)
		if c := e.cd; c != nil && c.kind != cdSwitchAdvance {
			log.Debug().Msg("auto-advance: countdown cancelled by a scan")
			e.cancelCountdown()
		}
	case evInput:
		e.onInput(ev)
	case evWatchEnded:
		if e.watch != nil && e.watch.gen == ev.gen {
			e.watch.cancel()
			e.watch = nil
			e.watchRetry = ev.at.Add(watchRetryDelay)
			log.Warn().Msg("auto-advance: input watch ended unexpectedly")
		}
	case evUIResult:
		e.onUIResult(ev)
	case evBarrier:
		if ev.fn != nil {
			ev.fn()
		}
	}
}

func (e *Engine) slot(name string) *slotState {
	if name == mediaslot.Background {
		return &e.background
	}
	return &e.primary
}

func (e *Engine) playlistFor(slot string) *playlists.Playlist {
	if slot == mediaslot.Background {
		return e.host.BackgroundPlaylist()
	}
	return e.host.ActivePlaylist()
}

// touch records that a person did something at t.
func (e *Engine) touch(t time.Time) {
	if t.After(e.lastActivity) {
		e.lastActivity = t
	}
}

// guard reports whether a move away from item b is still wanted: the playlist
// is the open one, on the same item, and playing.
func guard(pls *playlists.Playlist, b *bound) bool {
	return pls != nil && b != nil && pls.ID == b.plsID && pls.Index == b.index &&
		pls.Playing && !pls.PausedByFailure
}

func (e *Engine) onLaunching(ev *event) {
	s := e.slot(ev.slot)
	s.launching = &launchWindow{plsID: ev.plsID, index: ev.index, engineStarted: ev.engine}
	if ev.slot == mediaslot.Primary {
		// Any other launch ends the wait for an earlier item's media.
		s.pending = nil
		e.handoff = nil
	}
	if r := e.retry; r != nil && ev.engine && slotOf(r.upd) == ev.slot {
		// The move launched, so there is nothing to retry.
		e.retry = nil
	}
}

func (e *Engine) onLaunched(ev *event) {
	s := e.slot(ev.slot)
	w := s.launching
	s.launching = nil
	if w == nil || w.bound || !ev.async || ev.failed || ev.slot != mediaslot.Primary {
		return
	}
	s.pending = &pendingBind{
		plsID: w.plsID, index: w.index, engineStarted: w.engineStarted,
		deadline: ev.at.Add(pendingBindWindow),
	}
}

func (e *Engine) onRefused(ev *event) {
	r := e.retry
	if r == nil || r.upd != ev.pls {
		return
	}
	if r.tries >= moveMaxRetries {
		log.Warn().Str("playlist", ev.pls.ID).Msg("auto-advance: giving up on a move the queue kept refusing")
		e.retry = nil
		return
	}
	r.tries++
	r.due = ev.at.Add(moveRetryDelay)
}

func (e *Engine) onMediaStarted(ev *event) {
	if ev.start.Slot == mediaslot.Background {
		e.bind(&e.background, 0, &ev.start.Media, ev.at)
		return
	}
	e.primaryGen = ev.start.Gen
	e.primaryLive = true
	e.cancelCountdown()
	if !e.bind(&e.primary, ev.start.Gen, &ev.start.Media, ev.at) {
		// Media the engine did not start is someone using the device.
		e.touch(ev.at)
	}
}

// bind ties media that just started to the item being launched, or to an item
// waiting for its launcher's own publication. It reports whether the engine
// started that item itself.
func (e *Engine) bind(s *slotState, gen uint64, media *models.ActiveMedia, at time.Time) bool {
	w := s.launching
	started := media.Started
	if started.IsZero() {
		started = at
	}
	external := false
	switch {
	case w != nil:
		w.bound = true
		s.pending = nil
	case s != &e.background && s.pending != nil && !at.After(s.pending.deadline) &&
		e.mayPublishLater(media.LauncherID):
		p := s.pending
		s.pending = nil
		w = &launchWindow{plsID: p.plsID, index: p.index, engineStarted: p.engineStarted}
		external = true
	case s != &e.background && e.handoff != nil && !at.After(e.handoff.deadline) &&
		e.mayPublishLater(media.LauncherID):
		// The media that replaces one that handed the game over is the
		// item's. It keeps the first one's start, so a game that is then
		// quit right away still counts as quick.
		h := e.handoff
		e.handoff = nil
		w = &launchWindow{plsID: h.ended.plsID, index: h.ended.index, engineStarted: h.ended.engineStarted}
		started = h.ended.started
		external = true
	default:
		if s.bound != nil && s.bound.gen != gen {
			s.bound = nil
		}
		return false
	}
	s.bound = &bound{
		plsID:         w.plsID,
		index:         w.index,
		gen:           gen,
		media:         *media,
		started:       started,
		initAt:        at,
		engineStarted: w.engineStarted,
		external:      external,
	}
	if f := e.frozen; f != nil && (f.plsID != w.plsID || f.index != w.index) {
		e.frozen = nil
	}
	return w.engineStarted
}

// mayPublishLater reports whether media from the launcher with this ID is the
// kind an item waits for after its launch returned.
func (e *Engine) mayPublishLater(launcherID string) bool {
	return e.publishes == nil || e.publishes(launcherID)
}

func (e *Engine) onMediaEnded(ev *event) {
	slot := ev.end.Slot
	s := e.slot(slot)
	if slot != mediaslot.Background && ev.end.Gen == e.primaryGen {
		e.primaryLive = false
	}
	if slot != mediaslot.Background && (ev.end.Reason == models.MediaStopReasonStopped ||
		ev.end.Reason == models.MediaStopReasonReplaced) {
		// Core stopped or replaced it on someone's request, which is someone
		// using the device.
		e.touch(ev.at)
	}
	b := s.bound
	if b == nil {
		return
	}
	if slot == mediaslot.Background {
		if !ev.end.Media.Equal(&b.media) {
			return
		}
	} else if b.gen != ev.end.Gen {
		return
	}
	s.bound = nil
	if c := e.cd; c != nil && c.item == b {
		e.cancelCountdown()
	}
	if slot != mediaslot.Background {
		e.noteFrozen(b, ev)
	}

	switch ev.end.Reason {
	case models.MediaStopReasonFinished:
		e.onFinished(slot, b)
	case models.MediaStopReasonExited:
		if slot == mediaslot.Background {
			return
		}
		if b.external && ev.at.Sub(b.started) < handoffMaxRun {
			// Gone almost as soon as it appeared: a launcher handing the game
			// to another one. Wait for what takes over, and treat it as the
			// game quitting only if nothing does.
			e.handoff = &handoff{ended: b, endedAt: ev.at, deadline: ev.at.Add(handoffWindow)}
			return
		}
		e.onExited(b, ev.at)
	}
}

// onFinished advances a playlist whose item reached its natural end.
func (e *Engine) onFinished(slot string, b *bound) {
	pls := e.playlistFor(slot)
	if !guard(pls, b) {
		return
	}
	next, ok := nextAfterCompletion(pls)
	if !ok {
		e.finishPlaylist(slot, pls)
		return
	}
	e.send(pls, next)
}

// onExited handles a game that ended without Core's involvement. A playlist
// that is out of items closes before anything else: a countdown or a pause
// would only leave it open with nowhere to go.
func (e *Engine) onExited(b *bound, at time.Time) {
	pls := e.host.ActivePlaylist()
	if !guard(pls, b) || pls.Idle > 0 {
		return
	}
	next, ok := nextAfterCompletion(pls)
	if !ok {
		e.finishPlaylist(mediaslot.Primary, pls)
		return
	}
	if b.engineStarted && at.Sub(b.started) < quickQuitWindow {
		if e.host.PausePlaylistIfCurrent(mediaslot.Primary, pls) {
			log.Info().Str("playlist", pls.ID).Int("index", pls.Index).
				Msg("auto-advance: item quit right after starting, playlist paused")
		}
		return
	}
	wait := pls.EffectiveCountdown()
	if wait <= 0 {
		e.send(pls, next)
		return
	}
	e.startCountdown(&countdown{
		kind:             cdExit,
		slot:             mediaslot.Primary,
		plsID:            pls.ID,
		index:            pls.Index,
		inputCancels:     e.input != nil,
		ignoreInputUntil: at.Add(inputIgnoreWindow),
	}, "Up next: "+itemLabel(next.Current()), wait, at)
}

func (e *Engine) onPlaylistChanged(slot string, at time.Time) {
	if slot == mediaslot.Primary {
		// Someone, or the engine itself, just did something with the playlist.
		e.touch(at)
		e.primary.pending = nil
		e.handoff = nil
	}
	if c := e.cd; c != nil && c.slot == slot {
		log.Debug().Msg("auto-advance: countdown cancelled, playlist changed")
		e.cancelCountdown()
	}
	if slot != mediaslot.Primary {
		return
	}
	if f := e.frozen; f != nil {
		pls := e.host.ActivePlaylist()
		switch {
		case pls == nil || pls.ID != f.plsID || pls.Index != f.index:
			e.frozen = nil
		case !pls.Playing:
			f.sawPaused = true
		}
	}
}

func (e *Engine) onInput(ev *event) {
	if e.watch == nil || e.watch.gen != ev.gen {
		return
	}
	e.touch(ev.at)
	c := e.cd
	if c == nil || !c.inputCancels || ev.at.Before(c.ignoreInputUntil) {
		return
	}
	log.Debug().Msg("auto-advance: countdown cancelled by input")
	e.cancelCountdown()
}

// noteFrozen keeps the time left on an advance timer when media was stopped:
// if the playlist turns out to be paused, resuming picks the timer up again.
func (e *Engine) noteFrozen(b *bound, ev *event) {
	e.frozen = nil
	if ev.end.Reason != models.MediaStopReasonStopped || b.advTotal <= 0 || b.advFired {
		return
	}
	left := b.advLeft
	if !b.advSince.IsZero() {
		left -= ev.at.Sub(b.advSince)
	}
	if left <= 0 {
		return
	}
	f := &frozenAdvance{plsID: b.plsID, index: b.index, total: b.advTotal, left: left}
	if pls := e.host.ActivePlaylist(); pls != nil && pls.ID == b.plsID && pls.Index == b.index && !pls.Playing {
		f.sawPaused = true
	}
	e.frozen = f
}
