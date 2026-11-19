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

package serialport

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/shared/usbserial"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog/log"
	"golang.org/x/sys/unix"
)

const (
	devDir    = "/dev"
	procDir   = "/proc"
	sysTTYDir = "/sys/class/tty"
)

// USBPorts lists the USB serial device nodes present, which are the ports
// reader auto-detection probes.
func USBPorts() []USBPort {
	return defaultPortLister.list()
}

// portLister lists USB serial device nodes. Detection asks once a second, so
// the list is kept until the device directory changes, which it only does
// when a node is added or removed.
type portLister struct {
	dirModTime time.Time
	dev        string
	sysTTY     string
	ports      []USBPort
	mu         syncutil.Mutex
	listed     bool
}

var defaultPortLister = &portLister{dev: devDir, sysTTY: sysTTYDir}

func (l *portLister) list() []USBPort {
	l.mu.Lock()
	defer l.mu.Unlock()

	info, err := os.Stat(l.dev)
	if err != nil {
		l.listed = false
		return nil
	}
	if l.listed && info.ModTime().Equal(l.dirModTime) {
		return slices.Clone(l.ports)
	}
	entries, err := os.ReadDir(l.dev)
	if err != nil {
		l.listed = false
		return nil
	}
	var ports []USBPort
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, "ttyUSB") || strings.HasPrefix(name, "ttyACM") {
			ports = append(ports, USBPort{
				Path:   filepath.Join(l.dev, name),
				VIDPID: usbVIDPID(filepath.Join(l.sysTTY, name, "device")),
			})
		}
	}
	l.ports, l.dirModTime, l.listed = ports, info.ModTime(), true
	return slices.Clone(ports)
}

// usbVIDPID reads the vendor and product of the USB device a tty belongs to
// from sysfs. The tty's device link points at a USB interface, or at a port
// below one, so the IDs are a level or two further up.
func usbVIDPID(sysDevice string) string {
	dir, err := filepath.EvalSymlinks(sysDevice)
	if err != nil {
		return ""
	}
	for range 4 {
		vid, vidErr := os.ReadFile(filepath.Join(dir, "idVendor"))
		pid, pidErr := os.ReadFile(filepath.Join(dir, "idProduct"))
		if vidErr == nil && pidErr == nil {
			return strings.ToUpper(strings.TrimSpace(string(vid)) + ":" + strings.TrimSpace(string(pid)))
		}
		dir = filepath.Dir(dir)
	}
	return ""
}

// GuardProbes prepares the given serial ports for probing. Ports that
// another process has open are marked busy. Each remaining port is opened
// and held until Release, so its settings can be put back and so the probe
// closing the port is not the last close.
func GuardProbes(paths []string) *ProbeGuard {
	return guardProbesWith(defaultPortScanner, paths)
}

func guardProbesWith(scanner *portScanner, paths []string) *ProbeGuard {
	guard := &ProbeGuard{}
	devices := make([]string, 0, len(paths))
	for _, path := range paths {
		if !usbserial.IsPath(path) {
			devices = append(devices, path)
		}
	}
	if len(devices) == 0 {
		return guard
	}

	guard.busy = scanner.busy(devices)
	guard.saved = make(map[string]*lineState, len(devices))
	for _, path := range devices {
		if guard.Busy(path) {
			continue
		}
		state, err := saveLineState(path)
		if err != nil {
			log.Debug().Err(err).Str("path", path).Msg("failed to save serial port settings before probe")
			continue
		}
		guard.saved[path] = state
	}
	return guard
}

// PortsInUse returns which of the given serial ports another process has
// open.
func PortsInUse(paths []string) []string {
	return (&ProbeGuard{busy: defaultPortScanner.busy(paths)}).BusyPaths()
}

// busyRecheck is how long a port found open in another process is taken to
// still be so. Detection asks once a second, and searching every process's
// open files that often is too slow on a MiSTer for a port that stays held.
const busyRecheck = 30 * time.Second

// portScanner finds serial ports other processes have open and remembers the
// answer for busyRecheck.
type portScanner struct {
	clock     clockwork.Clock
	busyUntil map[string]time.Time
	proc      string
	mu        syncutil.Mutex
	self      int
}

func newPortScanner(proc string, self int, clock clockwork.Clock) *portScanner {
	return &portScanner{proc: proc, self: self, clock: clock, busyUntil: make(map[string]time.Time)}
}

var defaultPortScanner = newPortScanner(procDir, os.Getpid(), clockwork.NewRealClock())

func (s *portScanner) busy(paths []string) map[string]struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.clock.Now()
	busy := make(map[string]struct{})
	unknown := make([]string, 0, len(paths))
	for _, path := range paths {
		if until, ok := s.busyUntil[path]; ok && now.Before(until) {
			busy[path] = struct{}{}
			continue
		}
		unknown = append(unknown, path)
	}
	if len(unknown) == 0 {
		return busy
	}

	found := scanOpenPorts(s.proc, s.self, unknown)
	for _, path := range unknown {
		_, known := s.busyUntil[path]
		if _, isBusy := found[path]; !isBusy {
			delete(s.busyUntil, path)
			continue
		}
		busy[path] = struct{}{}
		s.busyUntil[path] = now.Add(busyRecheck)
		if !known {
			log.Info().Str("path", path).
				Msg("serial port is open in another program, leaving it out of reader auto-detection")
		}
	}
	return busy
}

// scanOpenPorts searches the open files of every other process for the given
// device nodes. A lock on the port cannot stand in for this: Core runs as
// root on some platforms and root passes TIOCEXCL. Processes whose files
// cannot be read, as for an unprivileged Core, are skipped.
func scanOpenPorts(proc string, self int, paths []string) map[string]struct{} {
	byTarget := make(map[string][]string, len(paths))
	for _, path := range paths {
		target, err := filepath.EvalSymlinks(path)
		if err != nil {
			target = path
		}
		byTarget[target] = append(byTarget[target], path)
	}

	busy := make(map[string]struct{})
	if len(byTarget) == 0 {
		return busy
	}
	procs, err := os.ReadDir(proc)
	if err != nil {
		return busy
	}
	for _, entry := range procs {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid == self {
			continue
		}
		fdDir := filepath.Join(proc, entry.Name(), "fd")
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}
		for _, fd := range fds {
			target, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if err != nil {
				continue
			}
			for _, path := range byTarget[target] {
				busy[path] = struct{}{}
			}
		}
		if len(busy) == len(paths) {
			break
		}
	}
	return busy
}

// lineState is a serial port held open with the settings it had when it was
// opened.
type lineState struct {
	termios unix.Termios
	fd      int
}

// saveLineState reads the port's settings as termios2, the only form that
// carries a non-standard rate such as MIDI's 31250.
func saveLineState(path string) (*lineState, error) {
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_NOCTTY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	termios, err := unix.IoctlGetTermios(fd, unix.TCGETS2)
	if err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("read settings of %s: %w", path, err)
	}
	return &lineState{fd: fd, termios: *termios}, nil
}

func (s *lineState) restore() error {
	setErr := unix.IoctlSetTermios(s.fd, unix.TCSETS2, &s.termios)
	closeErr := unix.Close(s.fd)
	if setErr != nil {
		return fmt.Errorf("write settings: %w", setErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close: %w", closeErr)
	}
	return nil
}

func (s *lineState) discard() {
	_ = unix.Close(s.fd)
}

// NodeID identifies the device node at path until it is removed and created
// again, as happens when a device is unplugged and another takes its name.
// The modification time is no use for this: the kernel moves it whenever
// data is written to a serial port.
func NodeID(path string) (string, error) {
	var stat unix.Stat_t
	if err := unix.Stat(path, &stat); err != nil {
		return "", fmt.Errorf("stat %s: %w", path, err)
	}
	//nolint:unconvert // field widths differ between architectures
	return fmt.Sprintf("%d:%d:%d.%d",
		uint64(stat.Ino), uint64(stat.Rdev), int64(stat.Ctim.Sec), int64(stat.Ctim.Nsec)), nil
}
