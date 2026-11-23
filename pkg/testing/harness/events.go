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

package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/assets"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/sim"
)

// Sound names the Recorder gives Core's built-in sounds.
const (
	SoundSuccess = "success"
	SoundFail    = "fail"
	SoundLimit   = "limit"
	SoundPending = "pending"
	SoundReady   = "ready"
	SoundOther   = "bytes"
)

// Recorder is an audio player that plays nothing and records what Core asked
// it to play: a built-in sound by name, a file as "file:<path>".
type Recorder struct {
	played []string
	mu     syncutil.Mutex
	volume float64
}

// NewRecorder returns an empty recorder.
func NewRecorder() *Recorder { return &Recorder{volume: 1} }

func soundName(data []byte) string {
	for name, s := range map[string][]byte{
		SoundSuccess: assets.SuccessSound, SoundFail: assets.FailSound, SoundLimit: assets.LimitSound,
		SoundPending: assets.PendingSound, SoundReady: assets.ReadySound,
	} {
		if bytes.Equal(data, s) {
			return name
		}
	}
	return SoundOther
}

// PlayBytes records a built-in sound.
func (r *Recorder) PlayBytes(data []byte) error {
	r.add(soundName(data))
	return nil
}

// PlayFile records a sound file.
func (r *Recorder) PlayFile(path string) error {
	r.add("file:" + path)
	return nil
}

// ClearFileCache does nothing.
func (*Recorder) ClearFileCache() {}

// SetVolume records the volume.
func (r *Recorder) SetVolume(v float64) {
	r.mu.Lock()
	r.volume = v
	r.mu.Unlock()
}

func (r *Recorder) add(s string) {
	r.mu.Lock()
	r.played = append(r.played, s)
	r.mu.Unlock()
}

// Played returns every sound played so far, oldest first.
func (r *Recorder) Played() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.played...)
}

// Reset forgets the sounds played so far.
func (r *Recorder) Reset() {
	r.mu.Lock()
	r.played = nil
	r.mu.Unlock()
}

// EventLog keeps every API notification an instance has sent since boot, so
// a test can wait for one without racing its subscription.
type EventLog struct {
	err   error
	cond  *sync.Cond
	done  chan struct{}
	items []sim.Notification
	mu    syncutil.Mutex
}

func newEventLog() *EventLog {
	l := &EventLog{done: make(chan struct{})}
	l.cond = sync.NewCond(&l.mu)
	return l
}

func (l *EventLog) start(ctx context.Context, api *sim.API) error {
	subscribed := make(chan struct{})
	go func() {
		defer close(l.done)
		_, err := api.WaitNotification(ctx, func(n sim.Notification) bool {
			l.mu.Lock()
			l.items = append(l.items, n)
			l.cond.Broadcast()
			l.mu.Unlock()
			return false
		}, func() error {
			close(subscribed)
			return nil
		})
		l.mu.Lock()
		l.err = err
		l.cond.Broadcast()
		l.mu.Unlock()
	}()
	select {
	case <-subscribed:
		return nil
	case <-l.done:
		return fmt.Errorf("subscribe to notifications: %w", l.err)
	case <-time.After(30 * time.Second):
		return errors.New("subscribe to notifications: timed out")
	}
}

func (l *EventLog) waitDone() {
	select {
	case <-l.done:
	case <-time.After(5 * time.Second):
	}
}

// Len returns how many notifications have arrived.
func (l *EventLog) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.items)
}

// All returns the notifications from index since on.
func (l *EventLog) All(since int) []sim.Notification {
	l.mu.Lock()
	defer l.mu.Unlock()
	if since >= len(l.items) {
		return nil
	}
	return append([]sim.Notification(nil), l.items[since:]...)
}

// Wait returns the first notification at index since or later named method
// whose params satisfy every KEY=VALUE in matches, and the index after it.
func (l *EventLog) Wait(
	ctx context.Context, since int, method string, matches ...string,
) (sim.Notification, int, error) {
	stop := context.AfterFunc(ctx, func() {
		l.mu.Lock()
		l.cond.Broadcast()
		l.mu.Unlock()
	})
	defer stop()
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := since; ; i++ {
		for i >= len(l.items) {
			if err := ctx.Err(); err != nil {
				return sim.Notification{}, i, fmt.Errorf("wait for %s %v: %w", method, matches, err)
			}
			if l.err != nil && len(l.items) <= i {
				return sim.Notification{}, i, fmt.Errorf("wait for %s: event stream ended: %w", method, l.err)
			}
			l.cond.Wait()
		}
		n := l.items[i]
		if n.Method == method && sim.MatchParams(n.Params, matches) {
			return n, i + 1, nil
		}
	}
}

// Decode unmarshals a notification's params.
func Decode[T any](n sim.Notification) (T, error) {
	var v T
	if err := json.Unmarshal(n.Params, &v); err != nil {
		return v, fmt.Errorf("decode %s params: %w", n.Method, err)
	}
	return v, nil
}
