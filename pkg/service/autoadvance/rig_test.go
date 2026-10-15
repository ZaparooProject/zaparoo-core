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
	"errors"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	uievents "github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/events"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

const time10s = 10 * time.Second

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

type fakeHost struct {
	active     *playlists.Playlist
	background *playlists.Playlist
	updates    []*playlists.Playlist
	paused     []*playlists.Playlist
	stopSeq    uint64
	stopping   bool
	mu         syncutil.Mutex
}

func (h *fakeHost) ActivePlaylist() *playlists.Playlist {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.active
}

func (h *fakeHost) BackgroundPlaylist() *playlists.Playlist {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.background
}

func (h *fakeHost) StopSeq() uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.stopSeq
}

func (h *fakeHost) StopInProgress() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.stopping
}

// beginStop and endStop are a stop that takes a while: counted when it starts
// and again when it completes.
func (h *fakeHost) beginStop() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.stopping = true
	h.stopSeq++
}

func (h *fakeHost) endStop() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.stopping = false
	h.stopSeq++
}

func (h *fakeHost) userStop() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.stopSeq++
}

func (*fakeHost) ActiveMediaGeneration() (uint64, bool) { return 0, false }

func (h *fakeHost) PausePlaylistIfCurrent(slot string, pls *playlists.Playlist) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.paused = append(h.paused, pls)
	if slot == mediaslot.Primary && h.active == pls {
		paused := playlists.Pause(*pls)
		paused.PausedByFailure = true
		h.active = paused
		return true
	}
	return false
}

func (h *fakeHost) Enqueue(_ context.Context, pls *playlists.Playlist) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.updates = append(h.updates, pls)
	return true
}

func (h *fakeHost) set(slot string, pls *playlists.Playlist) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if slot == mediaslot.Background {
		h.background = pls
	} else {
		h.active = pls
	}
}

func (h *fakeHost) queued() []*playlists.Playlist {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]*playlists.Playlist(nil), h.updates...)
}

func (h *fakeHost) pausedCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.paused)
}

// last returns the newest queued update, failing when there is none.
func (h *fakeHost) last(t *testing.T) *playlists.Playlist {
	t.Helper()
	all := h.queued()
	require.NotEmpty(t, all, "expected a playlist update")
	return all[len(all)-1]
}

type fakeUI struct {
	others   []models.UIEvent
	requests []*uievents.Request
	handles  []chan uievents.Result
	ctxs     []context.Context
	mu       syncutil.Mutex
}

func (u *fakeUI) Open(ctx context.Context, req *uievents.Request) (*uievents.Handle, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	results := make(chan uievents.Result, 1)
	u.requests = append(u.requests, req)
	u.handles = append(u.handles, results)
	u.ctxs = append(u.ctxs, ctx)
	return &uievents.Handle{Results: results, ID: "ui"}, nil
}

// State reports the events open that are not the engine's own prompts.
func (u *fakeUI) State() models.UIStateResponse {
	u.mu.Lock()
	defer u.mu.Unlock()
	return models.UIStateResponse{Events: append([]models.UIEvent(nil), u.others...)}
}

func (u *fakeUI) setOthers(events ...models.UIEvent) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.others = events
}

func (u *fakeUI) opened() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return len(u.requests)
}

func (u *fakeUI) request(t *testing.T, i int) *uievents.Request {
	t.Helper()
	u.mu.Lock()
	defer u.mu.Unlock()
	require.Greater(t, len(u.requests), i)
	return u.requests[i]
}

// dismissed reports whether the prompt's context was cancelled, which is what
// closes a real UI event.
func (u *fakeUI) dismissed(i int) bool {
	u.mu.Lock()
	ctx := u.ctxs[i]
	u.mu.Unlock()
	return ctx.Err() != nil
}

func (u *fakeUI) respond(i int, result uievents.Result) {
	u.mu.Lock()
	ch := u.handles[i]
	u.mu.Unlock()
	ch <- result
	close(ch)
}

func selected(choice string) uievents.Result {
	return uievents.Result{
		Resolution: models.UIResolution{Outcome: models.UIOutcomeSelected},
		Value:      choice,
	}
}

type fakeWatch struct {
	end        chan struct{}
	ctx        context.Context
	activity   func()
	responsive bool
}

type fakeInput struct {
	started  chan *fakeWatch
	coverage platforms.InputClass
}

func newFakeInput() *fakeInput {
	return &fakeInput{started: make(chan *fakeWatch, 16), coverage: platforms.InputAll}
}

func (f *fakeInput) InputCoverage() platforms.InputClass { return f.coverage }

func (f *fakeInput) WatchInput(ctx context.Context, responsive bool, onActivity func()) error {
	w := &fakeWatch{ctx: ctx, responsive: responsive, activity: onActivity, end: make(chan struct{})}
	f.started <- w
	select {
	case <-ctx.Done():
		return nil
	case <-w.end:
		return errors.New("watch failed")
	}
}

func (f *fakeInput) nextWatch(t *testing.T) *fakeWatch {
	t.Helper()
	select {
	case w := <-f.started:
		return w
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for an input watch")
		return nil
	}
}

type rig struct {
	t          *testing.T
	eng        *Engine
	host       *fakeHost
	clock      *clockwork.FakeClock
	ui         *fakeUI
	input      *fakeInput
	launcherID string
	nextID     uint64
}

type rigOption func(*Config, *rig)

func withUI(renderer bool) rigOption {
	return func(cfg *Config, r *rig) {
		r.ui = &fakeUI{}
		cfg.UI = r.ui
		cfg.HasRenderer = renderer
	}
}

func withPublishes(fn func(launcherID string) bool) rigOption {
	return func(cfg *Config, _ *rig) { cfg.PublishesLater = fn }
}

func withInput() rigOption {
	return func(cfg *Config, r *rig) {
		r.input = newFakeInput()
		cfg.Input = r.input
	}
}

func newRig(t *testing.T, opts ...rigOption) *rig {
	t.Helper()
	r := &rig{
		t:     t,
		host:  &fakeHost{},
		clock: clockwork.NewFakeClock(),
	}
	cfg := &Config{Clock: r.clock, Host: r.host}
	for _, opt := range opts {
		opt(cfg, r)
	}
	r.eng = New(cfg)
	r.eng.Start(context.Background())
	t.Cleanup(r.eng.Stop)
	return r
}

func testPlaylist(items, index int, mutate func(*playlists.Playlist)) *playlists.Playlist {
	list := make([]playlists.PlaylistItem, items)
	for i := range list {
		list[i] = playlists.PlaylistItem{Name: "Game" + string(rune('A'+i)), ZapScript: "game" + string(rune('a'+i))}
	}
	pls := &playlists.Playlist{
		ID:      "p",
		Name:    "list",
		Slot:    mediaslot.Primary,
		Items:   list,
		Index:   index,
		Playing: true,
	}
	if mutate != nil {
		mutate(pls)
	}
	return pls
}

func (r *rig) media(name string) models.ActiveMedia {
	return models.ActiveMedia{
		Name: name, Path: name, Started: r.clock.Now(), SystemID: "snes", Slot: mediaslot.Primary,
		LauncherID: r.launcherID,
	}
}

// play makes pls the open playlist and publishes its current item's media, the
// way a launch of that item does, returning the media's generation.
func (r *rig) play(pls *playlists.Playlist) uint64 {
	r.t.Helper()
	r.host.set(pls.Slot, pls)
	r.eng.PlaylistChanged(pls.Slot)
	return r.launch(pls)
}

// launch publishes media for pls's current item without touching the host.
func (r *rig) launch(pls *playlists.Playlist) uint64 {
	r.t.Helper()
	r.nextID++
	gen := r.nextID
	r.eng.ItemLaunching(pls)
	r.eng.MediaStarted(state.MediaStart{
		Slot: pls.Slot, Gen: gen, Media: r.media(pls.Current().ZapScript),
	})
	r.eng.ItemLaunched(pls, nil, false)
	r.eng.Sync()
	return gen
}

// foreign publishes media that no playlist launched.
func (r *rig) foreign(name string) uint64 {
	r.t.Helper()
	r.nextID++
	gen := r.nextID
	r.eng.MediaStarted(state.MediaStart{Slot: mediaslot.Primary, Gen: gen, Media: r.media(name)})
	r.eng.Sync()
	return gen
}

func (r *rig) end(gen uint64, reason string) {
	r.t.Helper()
	r.eng.MediaEnded(state.MediaEnd{Slot: mediaslot.Primary, Gen: gen, Reason: reason})
	r.eng.Sync()
}

func (r *rig) tick(d time.Duration) {
	r.t.Helper()
	r.clock.Advance(d)
	r.eng.Sync()
}

// changed reports that the open playlist changed and waits for it to be seen.
func (r *rig) changed() {
	r.t.Helper()
	r.eng.PlaylistChanged(mediaslot.Primary)
	r.eng.Sync()
}

// probe runs fn on the engine's own goroutine, so a test can read its state
// without racing it.
func (r *rig) probe(fn func(e *Engine)) {
	r.t.Helper()
	done := make(chan struct{})
	r.eng.post(&event{kind: evBarrier, done: done, fn: func() { fn(r.eng) }})
	<-done
}

func (r *rig) quiet() {
	r.t.Helper()
	r.eng.Sync()
	assert.Empty(r.t, r.host.queued(), "no playlist update expected")
}

func engineStarted(pls *playlists.Playlist) *playlists.Playlist {
	pls.EngineStarted = true
	return pls
}

// plainPlatform is a platform with no input source.
type plainPlatform struct{ platforms.Platform }

// inputPlatform is a platform with an input source of the given coverage.
type inputPlatform struct {
	plainPlatform
	coverage platforms.InputClass
}

func (p inputPlatform) InputCoverage() platforms.InputClass { return p.coverage }

func (inputPlatform) WatchInput(context.Context, bool, func()) error { return nil }
