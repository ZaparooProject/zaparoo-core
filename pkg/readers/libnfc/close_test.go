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

package libnfc

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/clausecker/nfc/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// blockingDevice holds every poll in "C" until the test releases it, and
// records whether Close freed the device while a poll was still running.
type blockingDevice struct {
	pollResult error
	entered    chan struct{}
	release    chan struct{}
	fakeDevice
	inPoll           atomic.Bool
	closed           atomic.Bool
	closedDuringPoll atomic.Bool
}

func newBlockingDevice() *blockingDevice {
	return &blockingDevice{
		entered: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
}

func (d *blockingDevice) InitiatorPollTarget([]nfc.Modulation, int, time.Duration) (int, nfc.Target, error) {
	d.inPoll.Store(true)
	defer d.inPoll.Store(false)
	select {
	case d.entered <- struct{}{}:
	default:
	}
	<-d.release
	return 0, nil, d.pollResult
}

func (d *blockingDevice) Close() error {
	if d.inPoll.Load() {
		d.closedDuringPoll.Store(true)
	}
	d.closed.Store(true)
	return nil
}

func waitClosed(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return")
		return nil
	}
}

// Close used to free the device (nfc_close) while the poll goroutine was
// still inside InitiatorPollTarget, a use-after-free that crashed the service
// on shutdown, sleep, prune and reader replacement.
func TestCloseWaitsForPollInFlight(t *testing.T) {
	t.Parallel()

	dev := newBlockingDevice()
	r := NewACR122Reader(&config.Instance{})
	r.startPolling(dev, make(chan readers.Scan, 1))

	select {
	case <-dev.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("poll never started")
	}

	closeDone := make(chan error, 1)
	go func() { closeDone <- r.Close() }()

	select {
	case <-closeDone:
		t.Fatal("Close returned while a poll was still in flight")
	case <-time.After(50 * time.Millisecond):
	}
	assert.False(t, dev.closed.Load(), "the device must not be closed under a running poll")

	close(dev.release)
	require.NoError(t, waitClosed(t, closeDone))
	assert.True(t, dev.closed.Load())
	assert.False(t, dev.closedDuringPoll.Load())
	assert.False(t, r.Connected())

	require.NoError(t, r.Close(), "a second Close is a no-op")
}

// An unplugged reader fails its poll with EIO and the goroutine closes the
// device itself. A later Close from the reader manager must not wait on it.
func TestCloseAfterPollIOError(t *testing.T) {
	t.Parallel()

	dev := newBlockingDevice()
	dev.pollResult = nfc.Error(nfc.EIO)
	close(dev.release)
	iq := make(chan readers.Scan, 1)
	r := NewACR122Reader(&config.Instance{})
	r.startPolling(dev, iq)

	select {
	case scan := <-iq:
		assert.True(t, scan.ReaderError)
	case <-time.After(5 * time.Second):
		t.Fatal("no reader error was sent")
	}

	closeDone := make(chan error, 1)
	go func() { closeDone <- r.Close() }()
	require.NoError(t, waitClosed(t, closeDone))
	assert.True(t, dev.closed.Load())
	assert.False(t, dev.closedDuringPoll.Load())
}

// The reader manager stops reading scans before it closes readers, so a poll
// goroutine blocked on sending one must still let Close finish.
func TestCloseWithUnreadScan(t *testing.T) {
	t.Parallel()

	dev := newBlockingDevice()
	dev.pollResult = nfc.Error(nfc.EIO)
	close(dev.release)
	r := NewACR122Reader(&config.Instance{})
	r.startPolling(dev, make(chan readers.Scan))

	<-dev.entered
	closeDone := make(chan error, 1)
	go func() { closeDone <- r.Close() }()
	require.NoError(t, waitClosed(t, closeDone))
	assert.True(t, dev.closed.Load())
}

// A reader whose poll goroutine never started still closes its device.
func TestCloseWithoutPolling(t *testing.T) {
	t.Parallel()

	dev := &fakeDevice{}
	r := testReader(dev)

	require.NoError(t, r.Close())
	assert.True(t, dev.closed)
	require.NoError(t, r.Close())
}
