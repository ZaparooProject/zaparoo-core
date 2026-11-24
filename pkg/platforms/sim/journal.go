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

package sim

import (
	"encoding/json"
	"os"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/rs/zerolog/log"
)

// Journal event kinds.
const (
	EventLaunchRequest = "launch-request" // LaunchMedia called by Core
	EventLaunch        = "launch"         // a sim launcher's Launch ran
	EventLaunchError   = "launch-error"   // LaunchMedia failed
	EventProcess       = "process"        // a child process was started
	EventProcessExit   = "process-exit"   // a child process exited
	EventTracked       = "tracked"        // SetTrackedProcess
	EventStop          = "stop"           // StopActiveLauncher
	EventStopError     = "stop-error"     // StopActiveLauncher failed
	EventKill          = "kill"           // a launcher's custom Kill ran
	EventMenu          = "menu"           // ReturnToMenu
	EventLaunchSystem  = "launch-system"  // LaunchSystem
	EventKeyboard      = "keyboard"       // KeyboardPress
	EventGamepad       = "gamepad"        // GamepadPress
	EventScreenshot    = "screenshot"     // Screenshot
	EventControl       = "control"        // a launcher control ran
	EventReady         = "ready"          // a readiness wait ran
	EventRunning       = "running"        // a GameRunning check ran
	EventExternal      = "external"       // the sim tracker published external media
	EventFinished      = "finished"       // a launcher reported natural end
	EventFault         = "fault"          // an injected fault fired
	EventRefresh       = "refresh"        // launcher dependency refresh
	EventPower         = "power"          // a power action was committed
)

// Event is one thing the sim platform was asked to do or did.
type Event struct {
	Time     time.Time         `json:"time"`
	Args     map[string]string `json:"args,omitempty"`
	Kind     string            `json:"kind"`
	Launcher string            `json:"launcher,omitempty"`
	System   string            `json:"system,omitempty"`
	Path     string            `json:"path,omitempty"`
	Intent   string            `json:"intent,omitempty"`
	Detail   string            `json:"detail,omitempty"`
	Error    string            `json:"error,omitempty"`
	Seq      int64             `json:"seq"`
	PID      int               `json:"pid,omitempty"`
}

const journalRing = 5000

// Journal records events in memory and, optionally, as JSON lines in a file.
type Journal struct {
	file *os.File
	subs map[chan Event]struct{}
	ring []Event
	mu   syncutil.Mutex
	seq  int64
}

// NewJournal returns a journal that also appends to path, if path is set.
func NewJournal(path string) (*Journal, error) {
	j := &Journal{subs: map[chan Event]struct{}{}}
	if path != "" {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // sim-owned path
		if err != nil {
			return nil, err //nolint:wrapcheck // caller adds context
		}
		j.file = f
	}
	return j, nil
}

// Record stores a copy of ev, assigning its sequence number and time, and
// returns the stored event. ev itself is not modified.
func (j *Journal) Record(ev *Event) Event {
	e := *ev
	j.mu.Lock()
	j.seq++
	e.Seq = j.seq
	if e.Time.IsZero() {
		e.Time = time.Now().UTC()
	}
	j.ring = append(j.ring, e)
	if len(j.ring) > journalRing {
		j.ring = append([]Event(nil), j.ring[len(j.ring)-journalRing:]...)
	}
	if j.file != nil {
		if data, err := json.Marshal(e); err == nil {
			_, _ = j.file.Write(append(data, '\n'))
		}
	}
	for ch := range j.subs {
		select {
		case ch <- e:
		default:
		}
	}
	j.mu.Unlock()
	log.Debug().Str("kind", e.Kind).Str("launcher", e.Launcher).Str("path", e.Path).
		Int("pid", e.PID).Str("error", e.Error).Msg("sim journal")
	return e
}

// Since returns events after seq, optionally only of the given kinds.
func (j *Journal) Since(seq int64, kinds ...string) []Event {
	want := map[string]bool{}
	for _, k := range kinds {
		want[k] = true
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make([]Event, 0, len(j.ring))
	for i := range j.ring {
		if j.ring[i].Seq <= seq {
			continue
		}
		if len(want) > 0 && !want[j.ring[i].Kind] {
			continue
		}
		out = append(out, j.ring[i])
	}
	return out
}

// Last returns the sequence number of the newest event.
func (j *Journal) Last() int64 {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.seq
}

// Subscribe returns a channel of new events and a function that ends the
// subscription. Slow subscribers miss events rather than block the platform.
func (j *Journal) Subscribe() (events <-chan Event, unsubscribe func()) {
	ch := make(chan Event, 256)
	j.mu.Lock()
	j.subs[ch] = struct{}{}
	j.mu.Unlock()
	return ch, func() {
		j.mu.Lock()
		if _, ok := j.subs[ch]; ok {
			delete(j.subs, ch)
			close(ch)
		}
		j.mu.Unlock()
	}
}

// Close closes the journal file.
func (j *Journal) Close() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.file == nil {
		return nil
	}
	err := j.file.Close()
	j.file = nil
	return err //nolint:wrapcheck // close error is self-describing
}
