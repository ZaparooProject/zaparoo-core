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
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// fakeProc builds a /proc tree in which each process has the given files
// open.
func fakeProc(t *testing.T, open map[int][]string) string {
	t.Helper()
	proc := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(proc, "uptime"), nil, 0o600))
	for pid, targets := range open {
		fdDir := filepath.Join(proc, strconv.Itoa(pid), "fd")
		require.NoError(t, os.MkdirAll(fdDir, 0o750))
		for i, target := range targets {
			require.NoError(t, os.Symlink(target, filepath.Join(fdDir, strconv.Itoa(i))))
		}
	}
	return proc
}

func testScanner(proc string) *portScanner {
	return newPortScanner(proc, os.Getpid(), clockwork.NewFakeClock())
}

// realDir is a temp dir with symlinks resolved, as the kernel reports paths.
func realDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	return dir
}

func touch(t *testing.T, path string) string {
	t.Helper()
	require.NoError(t, os.WriteFile(path, nil, 0o600))
	return path
}

func TestPortsInUseFindsOtherProcessesOnly(t *testing.T) {
	t.Parallel()

	dev := realDir(t)
	held := touch(t, filepath.Join(dev, "ttyUSB0"))
	ours := touch(t, filepath.Join(dev, "ttyUSB1"))
	free := touch(t, filepath.Join(dev, "ttyUSB2"))
	alias := filepath.Join(dev, "midi")
	require.NoError(t, os.Symlink(held, alias))

	const self = 200
	proc := fakeProc(t, map[int][]string{
		100:  {"/dev/null", held},
		self: {ours},
		300:  {"socket:[1234]"},
	})

	busy := scanOpenPorts(proc, self, []string{held, ours, free, alias})
	assert.Contains(t, busy, held)
	assert.Contains(t, busy, alias, "a symlink to a held port is held too")
	assert.NotContains(t, busy, ours, "Core's own open ports are not another program's")
	assert.NotContains(t, busy, free)
}

func TestPortScannerRechecksABusyPortOnlyAfterAWhile(t *testing.T) {
	t.Parallel()

	dev := realDir(t)
	port := touch(t, filepath.Join(dev, "ttyUSB0"))
	proc := fakeProc(t, map[int][]string{100: {port}})
	clock := clockwork.NewFakeClock()
	scanner := newPortScanner(proc, 1, clock)
	assert.Contains(t, scanner.busy([]string{port}), port)

	// The other program lets go. The port still reads as busy until the
	// recheck comes round, then as free.
	require.NoError(t, os.RemoveAll(filepath.Join(proc, "100")))
	clock.Advance(busyRecheck - time.Second)
	assert.Contains(t, scanner.busy([]string{port}), port)
	clock.Advance(time.Second)
	assert.Empty(t, scanner.busy([]string{port}))

	// A free port is looked at afresh every time.
	other := fakeProc(t, map[int][]string{100: {port}})
	require.NoError(t, os.Rename(filepath.Join(other, "100"), filepath.Join(proc, "100")))
	assert.Contains(t, scanner.busy([]string{port}), port)
}

func TestPortsInUseWithoutProc(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "proc")
	assert.Empty(t, scanOpenPorts(missing, 1, []string{"/dev/ttyUSB0"}))
	assert.Empty(t, scanOpenPorts(missing, 1, nil))
}

func TestUSBPortsInReadsVendorAndProduct(t *testing.T) {
	t.Parallel()

	dev := t.TempDir()
	for _, name := range []string{"ttyUSB0", "ttyACM3", "ttyUSB1", "ttyS0", "null"} {
		touch(t, filepath.Join(dev, name))
	}

	// ttyUSB0 hangs off a port below the interface, ttyACM3 off the interface
	// itself, and ttyUSB1 has no sysfs entry.
	sys := t.TempDir()
	usbDevice := filepath.Join(sys, "devices", "usb1", "1-2")
	iface := filepath.Join(usbDevice, "1-2:1.0")
	require.NoError(t, os.MkdirAll(filepath.Join(iface, "ttyUSB0"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(usbDevice, "idVendor"), []byte("16d0\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(usbDevice, "idProduct"), []byte("109a\n"), 0o600))
	sysTTY := filepath.Join(sys, "class", "tty")
	require.NoError(t, os.MkdirAll(filepath.Join(sysTTY, "ttyUSB0"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(sysTTY, "ttyACM3"), 0o750))
	require.NoError(t, os.Symlink(filepath.Join(iface, "ttyUSB0"), filepath.Join(sysTTY, "ttyUSB0", "device")))
	require.NoError(t, os.Symlink(iface, filepath.Join(sysTTY, "ttyACM3", "device")))

	lister := &portLister{dev: dev, sysTTY: sysTTY}
	want := []USBPort{
		{Path: filepath.Join(dev, "ttyUSB0"), VIDPID: "16D0:109A"},
		{Path: filepath.Join(dev, "ttyACM3"), VIDPID: "16D0:109A"},
		{Path: filepath.Join(dev, "ttyUSB1")},
	}
	assert.ElementsMatch(t, want, lister.list())

	// The list is reused until the directory changes: sysfs is not read
	// again, so a change there alone goes unseen.
	require.NoError(t, os.WriteFile(filepath.Join(usbDevice, "idProduct"), []byte("ffff\n"), 0o600))
	assert.ElementsMatch(t, want, lister.list())

	// A node appearing changes the directory and the list with it.
	touch(t, filepath.Join(dev, "ttyUSB7"))
	later := time.Now().Add(time.Hour)
	require.NoError(t, os.Chtimes(dev, later, later))
	got := lister.list()
	assert.Len(t, got, 4)
	assert.Contains(t, got, USBPort{Path: filepath.Join(dev, "ttyUSB0"), VIDPID: "16D0:FFFF"})

	assert.Empty(t, (&portLister{dev: filepath.Join(dev, "missing"), sysTTY: sysTTY}).list())
}

// openPTY returns a pseudo terminal's controlling side and the path of its
// line side, which takes the same settings calls as a serial port.
func openPTY(t *testing.T) (ctl int, line string) {
	t.Helper()
	ctl, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Skipf("no pseudo terminals here: %v", err)
	}
	t.Cleanup(func() { _ = unix.Close(ctl) })
	require.NoError(t, unix.IoctlSetPointerInt(ctl, unix.TIOCSPTLCK, 0))
	num, err := unix.IoctlGetInt(ctl, unix.TIOCGPTN)
	require.NoError(t, err)
	return ctl, "/dev/pts/" + strconv.Itoa(num)
}

func setSpeed(t *testing.T, path string, cflagSpeed, speed uint32) {
	t.Helper()
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	require.NoError(t, err)
	defer func() { _ = unix.Close(fd) }()
	termios, err := unix.IoctlGetTermios(fd, unix.TCGETS2)
	require.NoError(t, err)
	termios.Cflag = termios.Cflag&^unix.CBAUD | cflagSpeed
	termios.Ispeed, termios.Ospeed = speed, speed
	require.NoError(t, unix.IoctlSetTermios(fd, unix.TCSETS2, termios))
}

func readSpeed(t *testing.T, path string) (cflagSpeed, speed uint32) {
	t.Helper()
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	require.NoError(t, err)
	defer func() { _ = unix.Close(fd) }()
	termios, err := unix.IoctlGetTermios(fd, unix.TCGETS2)
	require.NoError(t, err)
	return termios.Cflag & unix.CBAUD, termios.Ospeed
}

func TestGuardRestoresANonStandardRateAfterAProbe(t *testing.T) {
	t.Parallel()

	_, line := openPTY(t)
	setSpeed(t, line, unix.BOTHER, 31250)

	guard := guardProbesWith(testScanner(fakeProc(t, nil)), []string{line, "usbserial:1-2"})
	assert.False(t, guard.Busy(line))
	setSpeed(t, line, unix.B115200, 115200) // what a probe leaves behind
	guard.Release()

	flag, speed := readSpeed(t, line)
	assert.Equal(t, uint32(unix.BOTHER), flag)
	assert.Equal(t, uint32(31250), speed)
}

func TestGuardKeepsAFoundReadersSettings(t *testing.T) {
	t.Parallel()

	_, line := openPTY(t)
	setSpeed(t, line, unix.B9600, 9600)

	guard := guardProbesWith(testScanner(fakeProc(t, nil)), []string{line})
	setSpeed(t, line, unix.B115200, 115200)
	guard.Release(line)

	flag, speed := readSpeed(t, line)
	assert.Equal(t, uint32(unix.B115200), flag)
	assert.Equal(t, uint32(115200), speed)
}

func TestGuardLeavesABusyPortUnopened(t *testing.T) {
	t.Parallel()

	_, line := openPTY(t)
	setSpeed(t, line, unix.BOTHER, 31250)

	guard := guardProbesWith(testScanner(fakeProc(t, map[int][]string{100: {line}})), []string{line})
	assert.True(t, guard.Busy(line))
	assert.Equal(t, []string{line}, guard.BusyPaths())
	assert.Empty(t, guard.saved, "a busy port must not be opened to save its settings")
	guard.Release()
}

func TestGuardSkipsPortsItCannotOpen(t *testing.T) {
	t.Parallel()

	gone := filepath.Join(t.TempDir(), "ttyUSB0")
	notATerminal := touch(t, filepath.Join(t.TempDir(), "ttyUSB1"))
	guard := guardProbesWith(testScanner(fakeProc(t, nil)), []string{gone, notATerminal})
	assert.False(t, guard.Busy(gone))
	assert.Empty(t, guard.saved)
	guard.Release()
}

func TestNodeIDFollowsTheNodeNotItsTraffic(t *testing.T) {
	t.Parallel()

	ctl, line := openPTY(t)
	before, err := NodeID(line)
	require.NoError(t, err)
	_, err = unix.Write(ctl, []byte("x"))
	require.NoError(t, err)
	after, err := NodeID(line)
	require.NoError(t, err)
	assert.Equal(t, before, after, "data on the line must not look like a replug")

	_, other := openPTY(t)
	otherID, err := NodeID(other)
	require.NoError(t, err)
	assert.NotEqual(t, before, otherID)

	_, err = NodeID(filepath.Join(t.TempDir(), "gone"))
	require.Error(t, err)
}

func TestPortsInUseOnTheRealProcTree(t *testing.T) {
	t.Parallel()

	// Nothing but this process has the temp file open, and this process does
	// not count.
	path := touch(t, filepath.Join(realDir(t), "ttyUSB0"))
	file, err := os.Open(path) //nolint:gosec // G304: test-created temp file
	require.NoError(t, err)
	defer func() { _ = file.Close() }()
	assert.Empty(t, PortsInUse([]string{path}))
	assert.False(t, GuardProbes([]string{path}).Busy(path))
}
