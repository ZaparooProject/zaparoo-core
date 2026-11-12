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
	"unsafe"

	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
	"golang.org/x/sys/unix"
)

const (
	devInputDir = "/dev/input"

	absCount = 0x40
)

// Evdev watches /dev/input/event* nodes for keyboards, mice and controllers.
// It only works where nothing else grabs the devices exclusively.
type Evdev struct {
	fs          afero.Fs
	clock       clockwork.Clock
	open        func(path string) (int, error)
	absInfo     func(fd int, code uint16) (axisRange, bool)
	readable    func(path string) bool
	procDevices string
	devInput    string
	classes     Class
}

// NewEvdev returns a source limited to the given classes of input.
func NewEvdev(classes Class) *Evdev {
	return &Evdev{
		fs:          afero.NewOsFs(),
		clock:       clockwork.NewRealClock(),
		open:        openNonblock,
		absInfo:     evdevAbsInfo,
		readable:    func(path string) bool { return unix.Access(path, unix.R_OK) == nil },
		procDevices: ProcDevicesPath,
		devInput:    devInputDir,
		classes:     classes,
	}
}

type candidate struct {
	dev   *InputDevice
	path  string
	class Class
}

func (e *Evdev) candidates() []candidate {
	devices, err := ReadDevices(e.fs, e.procDevices)
	if err != nil {
		return nil
	}
	var out []candidate
	for i := range devices {
		dev := &devices[i]
		if dev.Skipped() {
			continue
		}
		class := dev.Classify()
		if class == 0 || class&e.classes == 0 {
			continue
		}
		node, ok := dev.EventNode()
		if !ok {
			continue
		}
		out = append(out, candidate{dev: dev, class: class, path: filepath.Join(e.devInput, node)})
	}
	return out
}

// probeResult is the outcome of trying to open one candidate device.
type probeResult struct {
	class Class
	ok    bool
}

// coverageFromProbes turns open attempts into the classes that can really be
// watched. A class with a device that would not open is dropped, because that
// device could be the one a person is using. A class with no devices at all is
// covered only when reading works generally and nothing failed to open: a
// keyboard plugged in later is then picked up (hotplug is watched with
// fsnotify and every device list is re-read every resyncInterval regardless),
// but if some device was already refused, a new one may be refused too.
func coverageFromProbes(want Class, probes []probeResult, canRead bool) Class {
	var opened, failed Class
	anyOpened := false
	for _, p := range probes {
		if p.ok {
			opened |= p.class
			anyOpened = true
		} else {
			failed |= p.class
		}
	}
	var out Class
	for _, c := range []Class{Keyboard, Mouse, Controller} {
		if want&c == 0 || failed&c != 0 {
			continue
		}
		if opened&c != 0 || ((anyOpened || canRead) && failed == 0) {
			out |= c
		}
	}
	return out
}

// Coverage reports the classes of input that can be read right now.
func (e *Evdev) Coverage() Class {
	cands := e.candidates()
	probes := make([]probeResult, 0, len(cands))
	for _, c := range cands {
		fd, err := e.open(c.path)
		if err == nil {
			_ = unix.Close(fd)
		}
		probes = append(probes, probeResult{class: c.class, ok: err == nil})
	}
	return coverageFromProbes(e.classes, probes, e.canRead())
}

// canRead reports whether any event node is readable, which shows the process
// has the rights to read input devices at all.
func (e *Evdev) canRead() bool {
	entries, err := afero.ReadDir(e.fs, e.devInput)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if isEventName(entry.Name()) && e.readable(filepath.Join(e.devInput, entry.Name())) {
			return true
		}
	}
	return false
}

func isEventName(name string) bool {
	return isNumbered("event")(name)
}

// Watch reads input devices until ctx is cancelled.
func (e *Evdev) Watch(ctx context.Context, _ bool, onActivity func()) error {
	set, err := newPollSet()
	if err != nil {
		return err
	}
	throttle := NewThrottle(e.clock, NotifyInterval, onActivity)
	m := &managed{
		set:       set,
		watchDirs: []string{e.devInput},
		match:     isEventName,
		resync:    func() { e.sync(set, throttle) },
	}
	return m.run(ctx)
}

func (e *Evdev) sync(set *pollSet, throttle *Throttle) {
	cands := e.candidates()
	present := make(map[string]struct{}, len(cands))
	for _, c := range cands {
		present[c.path] = struct{}{}
	}
	for _, p := range set.paths() {
		if _, ok := present[p]; !ok {
			set.remove(p)
		}
	}
	for i := range cands {
		c := &cands[i]
		if set.has(c.path) {
			continue
		}
		fd, err := e.open(c.path)
		if err != nil {
			log.Debug().Err(err).Str("path", c.path).Msg("input activity: cannot open input device")
			continue
		}
		if !set.add(c.path, fd, e.handler(fd, c, throttle)) {
			_ = unix.Close(fd)
		}
	}
}

func (e *Evdev) handler(fd int, c *candidate, throttle *Throttle) func([]byte) {
	var axes *axisTracker
	if c.class == Controller {
		axes = newAxisTracker(func(code uint16) (axisRange, bool) { return e.absInfo(fd, code) })
		for code := range absCount {
			if !c.dev.HasAbs(code) {
				continue
			}
			if r, ok := e.absInfo(fd, uint16(code)); ok { //nolint:gosec // code < 0x40
				axes.seed(uint16(code), r) //nolint:gosec // code < 0x40
			}
		}
	}
	var chatter ChatterFilter
	return func(p []byte) {
		active, sig := eventsActive(c.class, parseEvents(p), axes)
		if !active {
			return
		}
		if chatter.Allow(e.clock.Now(), sig) {
			throttle.Trigger()
		}
	}
}

// evdevAbsInfo reads an absolute axis range and current value with EVIOCGABS.
func evdevAbsInfo(fd int, code uint16) (axisRange, bool) {
	var info struct {
		value, minimum, maximum, fuzz, flat, resolution int32
	}
	const (
		iocRead  = 2
		iocShift = 30
		sizeBits = 16
		typeBits = 8
	)
	req := uintptr(iocRead)<<iocShift |
		unsafe.Sizeof(info)<<sizeBits |
		uintptr('E')<<typeBits |
		uintptr(0x40+code)
	//nolint:gosec // G103: pointer is to a local the kernel fills in
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), req, uintptr(unsafe.Pointer(&info)))
	if errno != 0 {
		return axisRange{}, false
	}
	return axisRange{min: info.minimum, max: info.maximum, value: info.value}, true
}

// hasInputPrefix reports whether name looks like an input node of any kind.
func hasInputPrefix(name string) bool {
	return strings.HasPrefix(name, "event") || strings.HasPrefix(name, "js") ||
		strings.HasPrefix(name, "mice") || strings.HasPrefix(name, "hidraw")
}

// NewEvdevAll returns a source watching keyboards, mice and controllers.
func NewEvdevAll() *Evdev {
	return NewEvdev(Keyboard | Mouse | Controller)
}
