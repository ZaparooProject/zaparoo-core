//go:build linux

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

package inputactivity

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
	"golang.org/x/sys/unix"
)

const (
	devDir         = "/dev"
	sysClassHidraw = "/sys/class/hidraw"
	procHandlers   = "/proc/bus/input/handlers"
	miceNodeName   = "mice"
	joydevHandler  = "joydev"
)

// samJoystickInterval is how often joystick state is sampled outside a
// countdown. Only the device's current state is readable while MiSTer Main
// holds its grab, so a press that starts and ends between two samples is
// invisible. Measured on a MiSTer with samples every 250ms, a 50ms tap was
// seen 2 times in 6 and a 120ms tap 2 in 6, while holds of 300ms or more were
// always seen. At 100ms any press of 100ms or longer is caught, and a shorter
// tap only delays detection until the next press, since real play is
// continuous input. The cost is about 0.4 percentage points of one Cortex-A9
// core over the 250ms rate (estimated from 1.9% idle, 2.3% at 250ms, 5.7%
// at 20ms).
const samJoystickInterval = 100 * time.Millisecond

// samSampleInterval returns the joystick sampling gap: the short responsive
// rate while a countdown is on screen, samJoystickInterval otherwise.
func samSampleInterval(responsive bool) time.Duration {
	if responsive {
		return ResponsivePollInterval
	}
	return samJoystickInterval
}

// SAM watches input the way MiSTer_SAM does, for devices where MiSTer Main
// holds an exclusive grab on every /dev/input/event* node. The legacy nodes
// keep reporting while grabbed: joystick state through /dev/input/jsN,
// keyboards through their /dev/hidrawN twin and every mouse through
// /dev/input/mice.
type SAM struct {
	fs          afero.Fs
	clock       clockwork.Clock
	open        func(path string) (int, error)
	readJS      func(path string) ([]byte, error)
	procDevices string
	procHandler string
	devInput    string
	devDir      string
	sysHidraw   string
}

// NewSAM returns a source for the MiSTer input layout.
func NewSAM() *SAM {
	return &SAM{
		fs:          afero.NewOsFs(),
		clock:       clockwork.NewRealClock(),
		open:        openNonblock,
		readJS:      readNonblocking,
		procDevices: ProcDevicesPath,
		procHandler: procHandlers,
		devInput:    devInputDir,
		devDir:      devDir,
		sysHidraw:   sysClassHidraw,
	}
}

func (s *SAM) hasJoydev(devices []InputDevice) bool {
	for i := range devices {
		if _, ok := devices[i].JoystickNode(); ok {
			return true
		}
	}
	data, err := afero.ReadFile(s.fs, s.procHandler)
	return err == nil && strings.Contains(string(data), joydevHandler)
}

// Coverage reports what the legacy nodes can see right now. Keyboards count
// only when every keyboard that matters has a readable hidraw node.
func (s *SAM) Coverage() Class {
	devices, err := ReadDevices(s.fs, s.procDevices)
	if err != nil {
		return 0
	}
	var out Class
	if s.hasJoydev(devices) {
		out |= Controller
	}
	if s.keyboardsCovered(planSAM(devices)) {
		out |= Keyboard
	}
	if fd, err := s.open(filepath.Join(s.devInput, miceNodeName)); err == nil {
		_ = unix.Close(fd)
		out |= Mouse
	}
	return out
}

func (s *SAM) keyboardsCovered(plan samPlan) bool {
	if ok, _ := afero.DirExists(s.fs, s.sysHidraw); !ok {
		return false
	}
	for i := range plan.keyboards {
		node, ok := hidrawForPhys(s.fs, s.sysHidraw, plan.keyboards[i].Phys)
		if !ok {
			return false
		}
		fd, err := s.open(filepath.Join(s.devDir, node))
		if err != nil {
			return false
		}
		_ = unix.Close(fd)
	}
	return true
}

// Watch blocks until ctx is cancelled.
func (s *SAM) Watch(ctx context.Context, responsive bool, onActivity func()) error {
	set, err := newPollSet()
	if err != nil {
		return err
	}
	throttle := NewThrottle(s.clock, NotifyInterval, onActivity)
	w := &samWatch{sam: s, set: set, throttle: throttle, joy: map[string]*Snapshot{}}
	m := &managed{
		set:         set,
		watchDirs:   []string{s.devInput, s.devDir},
		match:       hasInputPrefix,
		resync:      w.resync,
		sample:      w.sample,
		sampleEvery: samSampleInterval(responsive),
	}
	return m.run(ctx)
}

// samWatch holds the state of one Watch call. resync and sample both run on
// the managed loop's goroutine, so it needs no locking.
type samWatch struct {
	sam      *SAM
	set      *pollSet
	throttle *Throttle
	joy      map[string]*Snapshot
	joyPaths []string
}

func (w *samWatch) resync() {
	s := w.sam
	devices, err := ReadDevices(s.fs, s.procDevices)
	if err != nil {
		return
	}
	plan := planSAM(devices)

	miceNode := filepath.Join(s.devInput, miceNodeName)
	if !w.set.has(miceNode) {
		if fd, err := s.open(miceNode); err == nil {
			if !w.set.add(miceNode, fd, func([]byte) { w.throttle.Trigger() }) {
				_ = unix.Close(fd)
			}
		}
	}

	wantKeys := map[string]struct{}{}
	for i := range plan.keyboards {
		node, ok := hidrawForPhys(s.fs, s.sysHidraw, plan.keyboards[i].Phys)
		if !ok {
			continue
		}
		path := filepath.Join(s.devDir, node)
		wantKeys[path] = struct{}{}
		if w.set.has(path) {
			continue
		}
		fd, err := s.open(path)
		if err != nil {
			log.Debug().Err(err).Str("path", path).Msg("input activity: cannot open keyboard")
			continue
		}
		if !w.set.add(path, fd, func([]byte) { w.throttle.Trigger() }) {
			_ = unix.Close(fd)
		}
	}
	for _, p := range w.set.paths() {
		if p == miceNode {
			continue
		}
		if _, ok := wantKeys[p]; !ok {
			w.set.remove(p)
		}
	}

	w.joyPaths = w.joyPaths[:0]
	live := map[string]struct{}{}
	for _, node := range plan.joysticks {
		path := filepath.Join(s.devInput, node)
		live[path] = struct{}{}
		w.joyPaths = append(w.joyPaths, path)
	}
	for path := range w.joy {
		if _, ok := live[path]; !ok {
			delete(w.joy, path)
		}
	}
}

// sample polls every joystick once. Each check re-opens the node: the state
// the kernel replays on open is compared with the last one seen, so a press
// that came and went between checks still shows as a change.
func (w *samWatch) sample() {
	for _, path := range w.joyPaths {
		data, err := w.sam.readJS(path)
		if err != nil {
			continue
		}
		snap := w.joy[path]
		if snap == nil {
			snap = NewSnapshot()
			w.joy[path] = snap
		}
		if applyJSEvents(snap, data) {
			w.throttle.Trigger()
		}
	}
}
