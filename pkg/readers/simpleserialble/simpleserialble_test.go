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

package simpleserialble

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/bluetooth/bluez"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testAddress = "AA:BB:CC:DD:EE:01"
	testTimeout = 5 * time.Second
)

type testRig struct {
	reader  *Reader
	adapter *mocks.FakeAdapter
	device  *mocks.FakeDevice
	tx      *mocks.FakeCharacteristic
	clock   *clockwork.FakeClock
	scans   chan readers.Scan
}

// newTestRig builds a reader over a fake adapter. The device is only
// findable once addDevice is called.
func newTestRig(t *testing.T, roles ...bluez.Role) *testRig {
	t.Helper()
	cfg, err := config.NewConfig(t.TempDir(), config.BaseDefaults)
	require.NoError(t, err)

	if len(roles) == 0 {
		roles = []bluez.Role{bluez.RoleCentral}
	}
	adapter := mocks.NewFakeAdapter(roles...)
	clock := clockwork.NewFakeClock()
	reader := newReaderWith(cfg, func(context.Context) (bluez.Adapter, error) {
		return adapter, nil
	}, clock)
	reader.findTimeout = 50 * time.Millisecond

	rig := &testRig{
		reader:  reader,
		adapter: adapter,
		clock:   clock,
		scans:   make(chan readers.Scan, 16),
	}
	t.Cleanup(func() { _ = reader.Close() })
	return rig
}

// addDevice makes a fresh device with the test address findable.
func (r *testRig) addDevice() {
	r.device = mocks.NewFakeDevice(testAddress)
	r.tx = mocks.NewFakeCharacteristic()
	r.device.AddCharacteristic(nusServiceUUID, nusTXUUID, r.tx)
	r.adapter.Cent.AddDevice(r.device)
}

func (r *testRig) open(t *testing.T) {
	t.Helper()
	require.NoError(t, r.reader.Open(
		config.ReadersConnect{Driver: DriverID, Path: testAddress}, r.scans, readers.OpenOpts{},
	))
	require.True(t, r.reader.Connected())
}

// waitLinked drives the fake clock past retry pauses until the current
// device is connected.
func (r *testRig) waitLinked(t *testing.T) {
	t.Helper()
	require.Eventually(t, func() bool {
		r.clock.Advance(time.Second)
		return r.device.Connected() && strings.Contains(r.reader.Info(), "connected")
	}, testTimeout, 10*time.Millisecond)
}

func (r *testRig) scan(t *testing.T) readers.Scan {
	t.Helper()
	select {
	case s := <-r.scans:
		return s
	case <-time.After(testTimeout):
		t.Fatal("no scan emitted")
		return readers.Scan{}
	}
}

func (r *testRig) noScan(t *testing.T) {
	t.Helper()
	select {
	case s := <-r.scans:
		t.Fatalf("unexpected scan: %+v", s)
	case <-time.After(100 * time.Millisecond):
	}
}

// tick advances the fake clock past one removal poll once the read loop is
// waiting on its ticker.
func (r *testRig) tick(t *testing.T, d time.Duration) {
	t.Helper()
	require.NoError(t, r.clock.BlockUntilContext(t.Context(), 1))
	r.clock.Advance(d)
}

func TestMetadata(t *testing.T) {
	t.Parallel()

	r := NewReader(nil)
	assert.Equal(t, DriverID, r.Metadata().ID)
	assert.False(t, r.Metadata().DefaultAutoDetect)
	assert.True(t, r.Metadata().DefaultEnabled)
	assert.Equal(t, []string{DriverID}, r.IDs())
	assert.Empty(t, r.Detect(nil))
	assert.False(t, r.Connected())
	_, err := r.Write("x")
	require.Error(t, err)
}

func TestOpen_RejectsBadDriverAndAddress(t *testing.T) {
	t.Parallel()

	rig := newTestRig(t)
	err := rig.reader.Open(config.ReadersConnect{Driver: "pn532", Path: testAddress}, rig.scans, readers.OpenOpts{})
	require.Error(t, err)
	err = rig.reader.Open(config.ReadersConnect{Driver: DriverID, Path: "/dev/ttyUSB0"}, rig.scans, readers.OpenOpts{})
	require.Error(t, err)
	assert.False(t, rig.reader.Connected())
	assert.Empty(t, rig.adapter.Cent.Finds(), "nothing is scanned for an invalid address")
}

func TestOpen_RequiresCentralRole(t *testing.T) {
	t.Parallel()

	rig := newTestRig(t, bluez.RolePeripheral)
	err := rig.reader.Open(config.ReadersConnect{Driver: DriverID, Path: testAddress}, rig.scans, readers.OpenOpts{})
	require.ErrorIs(t, err, bluez.ErrRoleUnsupported)
	assert.True(t, rig.adapter.Closed(), "a failed open releases the adapter")
	assert.False(t, rig.reader.Connected())
}

func TestOpen_ReturnsAtOnceAndKeepsSearching(t *testing.T) {
	t.Parallel()

	rig := newTestRig(t)
	started := time.Now()
	rig.open(t)
	assert.Less(t, time.Since(started), rig.reader.findTimeout, "Open must not wait for the device")
	assert.Contains(t, rig.reader.Info(), "searching")

	// Attempts continue with growing pauses while the device is absent.
	require.Eventually(t, func() bool {
		rig.clock.Advance(maxRetryPause)
		return len(rig.adapter.Cent.Finds()) >= 3
	}, testTimeout, 10*time.Millisecond)
	for _, f := range rig.adapter.Cent.Finds() {
		assert.Equal(t, testAddress, f.Address)
		assert.Equal(t, []string{nusServiceUUID}, f.ServiceUUIDs)
	}
	rig.noScan(t)

	// Once the device shows up it is connected on the next attempt.
	rig.addDevice()
	rig.waitLinked(t)
	assert.Equal(t, testAddress, rig.reader.Path())
	assert.Equal(t, readers.GenerateReaderID(DriverID, testAddress), rig.reader.ReaderID())
}

func TestScansAndRemoval(t *testing.T) {
	t.Parallel()

	rig := newTestRig(t)
	rig.addDevice()
	rig.open(t)
	rig.waitLinked(t)

	// Lines may arrive split across notifications.
	rig.tx.Push([]byte("SCAN\tuid=abc123\ttext=**launch"))
	rig.tx.Push([]byte(".system:nes\n"))
	scan := rig.scan(t)
	require.NotNil(t, scan.Token)
	assert.Equal(t, "abc123", scan.Token.UID)
	assert.Equal(t, "**launch.system:nes", scan.Token.Text)
	assert.Equal(t, rig.reader.ReaderID(), scan.ReaderID)
	assert.False(t, scan.ReaderError)

	// The reader repeats the token while it is present: no duplicate scan.
	rig.tx.Push([]byte("SCAN\tuid=abc123\ttext=**launch.system:nes\n"))
	rig.noScan(t)

	// A different token is a new scan.
	rig.tx.Push([]byte("SCAN\tuid=def456\n"))
	scan = rig.scan(t)
	require.NotNil(t, scan.Token)
	assert.Equal(t, "def456", scan.Token.UID)

	// Silence for longer than the removal timeout reports it gone.
	rig.tick(t, removalPoll)
	rig.noScan(t)
	rig.tick(t, removalTimeout)
	scan = rig.scan(t)
	assert.Nil(t, scan.Token)
	assert.False(t, scan.ReaderError)
	assert.True(t, rig.reader.Connected())
}

func TestRemovableFlag(t *testing.T) {
	t.Parallel()

	rig := newTestRig(t)
	rig.addDevice()
	rig.open(t)
	rig.waitLinked(t)
	assert.True(t, readers.HasCapability(rig.reader, readers.CapabilityRemovable))

	rig.tx.Push([]byte("SCAN\tuid=1\tremovable=no\n"))
	rig.scan(t)
	assert.False(t, readers.HasCapability(rig.reader, readers.CapabilityRemovable))

	rig.tx.Push([]byte("SCAN\tuid=2\tremovable=yes\n"))
	rig.scan(t)
	assert.True(t, readers.HasCapability(rig.reader, readers.CapabilityRemovable))
}

func TestLinkLossReportsReaderErrorAndReconnects(t *testing.T) {
	t.Parallel()

	rig := newTestRig(t)
	rig.addDevice()
	rig.open(t)
	rig.waitLinked(t)

	rig.tx.Push([]byte("SCAN\tuid=abc123\n"))
	require.NotNil(t, rig.scan(t).Token)

	rig.device.Drop()
	scan := rig.scan(t)
	assert.Nil(t, scan.Token)
	assert.True(t, scan.ReaderError, "a lost link must not look like a removal")
	assert.True(t, rig.reader.Connected(), "the reader stays attached and reconnects itself")
	require.Eventually(t, func() bool { return strings.Contains(rig.reader.Info(), "searching") },
		testTimeout, 10*time.Millisecond)

	// The device comes back: scans resume on the new link.
	rig.addDevice()
	rig.waitLinked(t)
	rig.tx.Push([]byte("SCAN\tuid=abc123\n"))
	scan = rig.scan(t)
	require.NotNil(t, scan.Token)
	assert.Equal(t, "abc123", scan.Token.UID)
}

func TestLinkLossWithoutTokenIsQuiet(t *testing.T) {
	t.Parallel()

	rig := newTestRig(t)
	rig.addDevice()
	rig.open(t)
	rig.waitLinked(t)

	rig.device.Drop()
	require.Eventually(t, func() bool { return strings.Contains(rig.reader.Info(), "searching") },
		testTimeout, 10*time.Millisecond)
	rig.noScan(t)
}

func TestCloseReleasesEverything(t *testing.T) {
	t.Parallel()

	rig := newTestRig(t)
	rig.addDevice()
	rig.open(t)
	rig.waitLinked(t)

	require.NoError(t, rig.reader.Close())
	assert.False(t, rig.reader.Connected())
	assert.False(t, rig.device.Connected())
	assert.True(t, rig.adapter.Closed())
	require.NoError(t, rig.reader.Close(), "closing twice is harmless")
}

func TestCloseWhileSearching(t *testing.T) {
	t.Parallel()

	rig := newTestRig(t)
	rig.open(t)
	require.NoError(t, rig.reader.Close())
	assert.False(t, rig.reader.Connected())
	assert.True(t, rig.adapter.Closed())
}
