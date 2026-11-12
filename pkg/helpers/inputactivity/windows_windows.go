//go:build windows

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
	"errors"
	"time"
	"unsafe"

	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"golang.org/x/sys/windows"
)

const (
	xinputSlots   = 4
	joyReturnAll  = 0xFF
	joyMaxDevices = 16
	// absentRecheck is how often an empty XInput slot is looked at again
	// without any device change; a change in the device set looks at once.
	absentRecheck = 10 * time.Second
	// joyDiscoveryInterval is the least time between rounds that probe
	// winmm ids for new devices; see deviceDiscovery for why.
	joyDiscoveryInterval = 30 * time.Second
	xinputSuccess        = 0
	joyErrNone           = 0
	rimTypeHID           = 2
)

var errNoWindowsInput = errors.New("no input source is available")

var (
	user32            = windows.NewLazySystemDLL("user32.dll")
	procLastInputInfo = user32.NewProc("GetLastInputInfo")
	procRawInputList  = user32.NewProc("GetRawInputDeviceList")
	kernel32          = windows.NewLazySystemDLL("kernel32.dll")
	procTickCount     = kernel32.NewProc("GetTickCount")
	xinput            = windows.NewLazySystemDLL("xinput1_4.dll")
	procXInputState   = xinput.NewProc("XInputGetState")
	winmm             = windows.NewLazySystemDLL("winmm.dll")
	procJoyGetPosEx   = winmm.NewProc("joyGetPosEx")

	// joyDevices is shared by every Watch so that restarting a watch does not
	// probe again.
	joyDevices = newDeviceDiscovery(joyDiscoveryInterval)
)

type lastInputInfo struct {
	size uint32
	time uint32
}

type xinputState struct {
	packetNumber uint32
	gamepad      [12]byte
}

type joyInfoEx struct {
	size, flags                uint32
	x, y, z, r, u, v           uint32
	buttons, buttonNumber, pov uint32
	reserved1, reserved2       uint32
}

type rawInputDevice struct {
	handle uintptr
	typ    uint32
}

// Injections tells the source when Core last injected input of its own.
type Injections struct {
	// Keyboard returns the time of Core's last synthetic key event.
	Keyboard func() time.Time
	// Gamepad returns the time of Core's last virtual gamepad change.
	Gamepad func() time.Time
}

// Windows reports keyboard and mouse from the session's last-input time and
// controllers from XInput packet numbers plus winmm for non-XInput pads.
// Input that Core caused itself is discounted; see lastInputInjected.
type Windows struct {
	clock clockwork.Clock
	// ExcludeSlots returns XInput slots owned by Zaparoo's own virtual pads.
	ExcludeSlots func() []int
	injections   Injections
}

// NewWindows returns a source. excludeSlots and the injection clocks may be
// nil.
func NewWindows(excludeSlots func() []int, injections Injections) *Windows {
	return &Windows{clock: clockwork.NewRealClock(), ExcludeSlots: excludeSlots, injections: injections}
}

func (*Windows) keyboardMouseAvailable() bool {
	return procLastInputInfo.Find() == nil && procTickCount.Find() == nil
}

// controllersAvailable needs the raw input device list as well: it is how new
// joysticks are noticed without probing winmm for them.
func (*Windows) controllersAvailable() bool {
	return procXInputState.Find() == nil && procJoyGetPosEx.Find() == nil && procRawInputList.Find() == nil
}

// Coverage reports each class only when its API is present on this machine.
func (w *Windows) Coverage() Class {
	var out Class
	if w.keyboardMouseAvailable() {
		out |= Keyboard | Mouse
	}
	if w.controllersAvailable() {
		out |= Controller
	}
	return out
}

// Watch blocks until ctx is cancelled.
func (w *Windows) Watch(ctx context.Context, responsive bool, onActivity func()) error {
	if w.Coverage() == 0 {
		return errNoWindowsInput
	}
	throttle := NewThrottle(w.clock, NotifyInterval, onActivity)
	s := &windowsSampler{
		w:      w,
		snap:   NewSnapshot(),
		absent: map[uint32]time.Time{},
		diag:   newDiagLimiter(w.clock),
	}
	watchPolled(ctx, pollEvery(responsive), s.sample, throttle.Trigger)
	return nil
}

type windowsSampler struct {
	w            *Windows
	snap         *Snapshot
	absent       map[uint32]time.Time
	diag         *diagLimiter
	lastSig      uint64
	xinputSig    uint64
	lastInput    uint32
	seen         bool
	xinputSigSet bool
}

func injectedAt(f func() time.Time) time.Time {
	if f == nil {
		return time.Time{}
	}
	return f()
}

// note writes one rate-limited debug line saying what the source saw. The
// lines are how a wrongly counted input is traced afterwards: grep the log for
// "input activity".
func (s *windowsSampler) note(key, msg string, fill func(e *zerolog.Event)) {
	e := log.Debug()
	if !e.Enabled() || !s.diag.allow(key) {
		return
	}
	fill(e)
	e.Msg("input activity: " + msg)
}

func (s *windowsSampler) sample() bool {
	kbInj := injectedAt(s.w.injections.Keyboard)
	padInj := injectedAt(s.w.injections.Gamepad)
	active := s.w.keyboardMouseAvailable() && s.userInput(kbInj, padInj)
	if s.w.controllersAvailable() {
		sig := s.deviceSignature()
		now := s.w.clock.Now()
		discounted := padChangeInjected(now, padInj)
		// Evaluate both so each updates its baseline, then discount what Core
		// itself caused.
		padActive := s.xinput(sig, discounted)
		if s.joysticks(sig, discounted) {
			padActive = true
		}
		if padActive {
			s.note("pad", "gamepad change", func(e *zerolog.Event) {
				e.Bool("discounted", discounted).Time("gamepad_injection", padInj).
					Dur("since_injection", now.Sub(padInj))
			})
			if !discounted {
				active = true
			}
		}
	}
	if active {
		s.note("reported", "reported to idle rotation", func(*zerolog.Event) {})
	}
	return active
}

// userInput reports whether the session's last keyboard or mouse input is new
// and was not caused by Core's own injections.
func (s *windowsSampler) userInput(kbInj, padInj time.Time) bool {
	info := lastInputInfo{size: uint32(unsafe.Sizeof(lastInputInfo{}))}
	//nolint:gosec // G103: pointer is to a local the call fills in
	ret, _, _ := procLastInputInfo.Call(uintptr(unsafe.Pointer(&info)))
	if ret == 0 {
		// Not knowing counts as activity so rotation never fires blind.
		return true
	}
	changed := s.seen && info.time != s.lastInput
	prev := s.lastInput
	s.lastInput = info.time
	s.seen = true
	if !changed {
		return false
	}
	tick, _, _ := procTickCount.Call()
	age := time.Duration(uint32(tick)-info.time) * time.Millisecond //nolint:gosec // wrapping tick arithmetic
	now := s.w.clock.Now()
	inputAt := now.Add(-age)
	discounted := lastInputFromCore(inputAt, kbInj, padInj)
	s.note("lastinput", "last input changed", func(e *zerolog.Event) {
		e.Uint32("tick", info.time).Uint32("prev_tick", prev).Dur("age", age).
			Time("input_at", inputAt).Time("keyboard_injection", kbInj).Time("gamepad_injection", padInj).
			Bool("discounted", discounted)
	})
	return !discounted
}

// deviceSignature fingerprints the machine's raw input devices. It creates no
// handles, unlike probing winmm ids.
func (s *windowsSampler) deviceSignature() uint64 {
	var count uint32
	size := uint32(unsafe.Sizeof(rawInputDevice{}))
	//nolint:gosec // G103: pointer is to a local the call fills in
	ret, _, _ := procRawInputList.Call(0, uintptr(unsafe.Pointer(&count)), uintptr(size))
	if ret == ^uintptr(0) || count == 0 {
		return s.lastSig
	}
	list := make([]rawInputDevice, count+8)
	n := count + 8
	//nolint:gosec // G103: the list is sized for n entries
	ret, _, _ = procRawInputList.Call(uintptr(unsafe.Pointer(&list[0])), uintptr(unsafe.Pointer(&n)), uintptr(size))
	if ret == ^uintptr(0) {
		return s.lastSig
	}
	handles := make([]uintptr, 0, n)
	for _, dev := range list[:min(int(ret), len(list))] {
		if dev.typ == rimTypeHID {
			handles = append(handles, dev.handle)
		}
	}
	s.lastSig = fingerprint(handles)
	return s.lastSig
}

func (s *windowsSampler) skip(key uint32) bool {
	until, ok := s.absent[key]
	return ok && s.w.clock.Now().Before(until)
}

func (s *windowsSampler) xinput(sig uint64, discounted bool) bool {
	if s.xinputSigSet && sig != s.xinputSig {
		// A device came or went: look at empty slots right away.
		clear(s.absent)
	}
	s.xinputSig = sig
	s.xinputSigSet = true
	excluded := map[int]struct{}{}
	if s.w.ExcludeSlots != nil {
		for _, slot := range s.w.ExcludeSlots() {
			excluded[slot] = struct{}{}
		}
	}
	active := false
	for slot := range xinputSlots {
		if _, skip := excluded[slot]; skip {
			continue
		}
		key := uint32(slot) //nolint:gosec // slot < 4
		if s.skip(key) {
			continue
		}
		var state xinputState
		//nolint:gosec // G103: pointer is to a local the call fills in
		ret, _, _ := procXInputState.Call(uintptr(slot), uintptr(unsafe.Pointer(&state)))
		if ret != xinputSuccess {
			// Polling an empty slot is slow, so look again only now and then.
			s.absent[key] = s.w.clock.Now().Add(absentRecheck)
			continue
		}
		delete(s.absent, key)
		if s.snap.Button(0x10000|key, int32(state.packetNumber)) { //nolint:gosec // compared, not used as a number
			active = true
			s.note("xinput", "xinput changed", func(e *zerolog.Event) {
				e.Uint32("slot", key).Uint32("packet", state.packetNumber).
					Uint16("buttons", uint16(state.gamepad[0])|uint16(state.gamepad[1])<<8).
					Bool("discounted", discounted)
			})
		}
	}
	return active
}

// joysticks polls only the winmm ids confirmed present. Probing an id with no
// device leaks a handle in winmm, so absent ids are looked for only through
// deviceDiscovery.
func (s *windowsSampler) joysticks(sig uint64, discounted bool) bool {
	now := s.w.clock.Now()
	if joyDevices.due(sig, now) {
		ids := probeJoysticks()
		joyDevices.record(sig, ids, now)
		// A rescan only refreshes which ids exist; it is never activity and
		// the first sight of a device only sets its baseline.
		s.note("rescan", "winmm rescan", func(e *zerolog.Event) {
			e.Uint64("signature", sig).Interface("present_ids", ids)
		})
	}
	active := false
	for _, id := range joyDevices.present() {
		info, ok := readJoy(id)
		if !ok {
			joyDevices.drop(id)
			continue
		}
		sample := joySample{
			axes:    [joyAxisCount]uint32{info.x, info.y, info.z, info.r, info.u, info.v},
			buttons: info.buttons,
			pov:     info.pov,
		}
		if applyJoySample(s.snap, id, sample) {
			active = true
			s.note("winmm", "winmm changed", func(e *zerolog.Event) {
				e.Uint32("id", id).Uint32("buttons", info.buttons).Uint32("pov", info.pov).
					Uint32("x", info.x).Uint32("y", info.y).Bool("discounted", discounted)
			})
		}
	}
	return active
}

func probeJoysticks() []uint32 {
	var ids []uint32
	for id := range joyMaxDevices {
		if _, ok := readJoy(uint32(id)); ok { //nolint:gosec // id < 16
			ids = append(ids, uint32(id)) //nolint:gosec // id < 16
		}
	}
	return ids
}

func readJoy(id uint32) (joyInfoEx, bool) {
	info := joyInfoEx{flags: joyReturnAll}
	info.size = uint32(unsafe.Sizeof(info))
	//nolint:gosec // G103: pointer is to a local the call fills in
	ret, _, _ := procJoyGetPosEx.Call(uintptr(id), uintptr(unsafe.Pointer(&info)))
	return info, ret == joyErrNone
}
