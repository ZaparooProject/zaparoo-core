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
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/shared/nfctag/tagsim"
	"github.com/clausecker/nfc/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// lockedDevice lets a test change the presented card while the poll
// goroutine is using the device.
type lockedDevice struct {
	*fakeDevice
	closeErr error
	closed   atomic.Bool
	polls    atomic.Int32
	mu       syncutil.Mutex
}

func (d *lockedDevice) set(change func(*fakeDevice)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	change(d.fakeDevice)
}

func (d *lockedDevice) InitiatorPollTarget(m []nfc.Modulation, n int, p time.Duration) (int, nfc.Target, error) {
	d.polls.Add(1)
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.fakeDevice.InitiatorPollTarget(m, n, p)
}

func (d *lockedDevice) InitiatorSelectPassiveTarget(m nfc.Modulation, uid []byte) (nfc.Target, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.fakeDevice.InitiatorSelectPassiveTarget(m, uid)
}

func (d *lockedDevice) InitiatorTransceiveBytes(tx, rx []byte, timeout int) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.fakeDevice.InitiatorTransceiveBytes(tx, rx, timeout)
}

func (d *lockedDevice) Close() error {
	d.closed.Store(true)
	return d.closeErr
}

func nextScan(t *testing.T, iq <-chan readers.Scan) readers.Scan {
	t.Helper()
	select {
	case scan := <-iq:
		return scan
	case <-time.After(5 * time.Second):
		t.Fatal("no scan was sent")
		return readers.Scan{}
	}
}

func TestPollLoopReportsTagThenRemoval(t *testing.T) {
	t.Parallel()

	tag := tagsim.NewType2(tagsim.PagesNTAG215)
	dev := &lockedDevice{fakeDevice: &fakeDevice{type2: tag}}
	iq := make(chan readers.Scan, 4)
	r := NewACR122Reader(&config.Instance{})
	r.startPolling(dev, iq)
	t.Cleanup(func() { _ = r.Close() })

	scan := nextScan(t, iq)
	require.NotNil(t, scan.Token)
	assert.False(t, scan.ReaderError)

	dev.set((*fakeDevice).clear)
	removed := nextScan(t, iq)
	assert.Nil(t, removed.Token)
	assert.False(t, removed.ReaderError)
}

// A poll that fails without the device going away is retried, not reported.
func TestPollLoopContinuesAfterTransientError(t *testing.T) {
	t.Parallel()

	dev := &lockedDevice{fakeDevice: &fakeDevice{pollErr: errors.New("transient poll failure")}}
	iq := make(chan readers.Scan, 4)
	r := NewACR122Reader(&config.Instance{})
	r.startPolling(dev, iq)
	require.Eventually(t, func() bool { return dev.polls.Load() >= 3 }, 5*time.Second, 10*time.Millisecond)

	assert.True(t, r.Connected())
	assert.Empty(t, iq)
	require.NoError(t, r.Close())
	assert.True(t, dev.closed.Load())
}

func TestWriteTargetUnsupportedTagFails(t *testing.T) {
	t.Parallel()

	dev := &lockedDevice{fakeDevice: &fakeDevice{desfire: true}}
	r := NewACR122Reader(&config.Instance{})
	r.startPolling(dev, make(chan readers.Scan, 4))
	t.Cleanup(func() { _ = r.Close() })

	_, err := r.WriteTarget(t.Context(), "**launch.system:nes", readers.WriteOptions{})
	require.ErrorIs(t, err, readers.ErrUnsupportedTagType)
}

// Close has to cancel a write still waiting for a tag rather than wait out
// the write's whole timeout.
func TestCloseCancelsWriteWaitingForTag(t *testing.T) {
	t.Parallel()

	dev := &lockedDevice{fakeDevice: &fakeDevice{}}
	r := NewACR122Reader(&config.Instance{})
	r.startPolling(dev, make(chan readers.Scan, 4))

	writeDone := make(chan error, 1)
	go func() {
		_, err := r.WriteTarget(t.Context(), "**launch.system:nes", readers.WriteOptions{})
		writeDone <- err
	}()
	require.Eventually(t, func() bool {
		r.activeWriteMu.RLock()
		defer r.activeWriteMu.RUnlock()
		return r.activeWrite != nil
	}, 5*time.Second, 10*time.Millisecond)

	require.NoError(t, waitClosed(t, closeAsync(r)))
	select {
	case err := <-writeDone:
		require.ErrorIs(t, err, ErrWriteCancelled)
	case <-time.After(5 * time.Second):
		t.Fatal("the waiting write was not cancelled")
	}
}

func TestCloseReportsDeviceCloseError(t *testing.T) {
	t.Parallel()

	t.Run("polling", func(t *testing.T) {
		t.Parallel()
		dev := &lockedDevice{fakeDevice: &fakeDevice{}, closeErr: errors.New("usb gone")}
		r := NewACR122Reader(&config.Instance{})
		r.startPolling(dev, make(chan readers.Scan, 4))
		require.ErrorContains(t, r.Close(), "usb gone")
	})

	t.Run("not polling", func(t *testing.T) {
		t.Parallel()
		dev := &lockedDevice{fakeDevice: &fakeDevice{}, closeErr: errors.New("usb gone")}
		r := NewACR122Reader(&config.Instance{})
		r.pnd = dev
		require.ErrorContains(t, r.Close(), "usb gone")
		assert.True(t, dev.closed.Load())
	})
}
