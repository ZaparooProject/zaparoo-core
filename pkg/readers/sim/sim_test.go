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
	"context"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestReader(t *testing.T, hub *Hub, name string) (r *Reader, iq chan readers.Scan) {
	t.Helper()
	cfg, err := helpers.NewTestConfig(helpers.NewMemoryFS(), t.TempDir())
	require.NoError(t, err)
	r = NewReaderWithHub(cfg, hub)
	iq = make(chan readers.Scan, 8)
	require.NoError(t, r.Open(config.ReadersConnect{Driver: DriverID, Path: name}, iq, readers.OpenOpts{}))
	t.Cleanup(func() { _ = r.Close() })
	return r, iq
}

func nextScan(t *testing.T, iq <-chan readers.Scan) readers.Scan {
	t.Helper()
	select {
	case s := <-iq:
		return s
	case <-time.After(2 * time.Second):
		t.Fatal("no scan")
		return readers.Scan{}
	}
}

func noScan(t *testing.T, iq <-chan readers.Scan) {
	t.Helper()
	select {
	case s := <-iq:
		t.Fatalf("unexpected scan: %+v", s)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestHubAddValidatesNames(t *testing.T) {
	t.Parallel()
	hub := NewHub()
	for _, name := range []string{"", " ", "a:b", "a/b", "a\\b"} {
		_, err := hub.Add(name, DeviceOptions{})
		require.Error(t, err, "name %q", name)
	}
	_, err := hub.Add("r1", DeviceOptions{})
	require.NoError(t, err)
	_, err = hub.Add("r1", DeviceOptions{})
	require.ErrorIs(t, err, ErrDeviceExists)
}

func TestDetectFindsPluggedUnopenedDevices(t *testing.T) {
	t.Parallel()
	hub := NewHub()
	_, err := hub.Add("b", DeviceOptions{})
	require.NoError(t, err)
	_, err = hub.Add("a", DeviceOptions{})
	require.NoError(t, err)
	_, err = hub.Add("hidden", DeviceOptions{NoAutoDetect: true})
	require.NoError(t, err)

	r := NewReaderWithHub(nil, hub)
	assert.Equal(t, "sim:a", r.Detect(nil))
	assert.Equal(t, "sim:b", r.Detect([]string{"sim:a"}))
	assert.Empty(t, r.Detect([]string{"a", "b"}))

	newTestReader(t, hub, "a")
	assert.Equal(t, "sim:b", r.Detect(nil), "an open device is not detected again")
}

func TestTapAndRemove(t *testing.T) {
	t.Parallel()
	hub := NewHub()
	d, err := hub.Add("r", DeviceOptions{})
	require.NoError(t, err)
	r, iq := newTestReader(t, hub, "r")

	tag, err := d.Tap(Tag{Text: "**launch.random:snes"}, 0)
	require.NoError(t, err)
	assert.NotEmpty(t, tag.UID, "a UID is generated")
	assert.NotEmpty(t, tag.Type)

	s := nextScan(t, iq)
	require.NotNil(t, s.Token)
	assert.Equal(t, "**launch.random:snes", s.Token.Text)
	assert.Equal(t, tag.UID, s.Token.UID)
	assert.Equal(t, r.ReaderID(), s.ReaderID)
	assert.Equal(t, tag.UID, d.Status().Present.UID)

	require.NoError(t, d.RemoveTag())
	s = nextScan(t, iq)
	assert.Nil(t, s.Token)
	assert.False(t, s.WrittenTagRemoved)
	assert.Nil(t, d.Status().Present)
	require.ErrorIs(t, d.RemoveTag(), ErrNoTag)
}

func TestTapHoldRemovesAfterDelay(t *testing.T) {
	t.Parallel()
	hub := NewHub()
	d, err := hub.Add("r", DeviceOptions{})
	require.NoError(t, err)
	_, iq := newTestReader(t, hub, "r")

	_, err = d.Tap(Tag{UID: "04aa", Text: "x"}, 20*time.Millisecond)
	require.NoError(t, err)
	require.NotNil(t, nextScan(t, iq).Token)
	assert.Nil(t, nextScan(t, iq).Token, "auto-removed")
}

func TestTapNeedsOpenReader(t *testing.T) {
	t.Parallel()
	hub := NewHub()
	d, err := hub.Add("r", DeviceOptions{})
	require.NoError(t, err)
	_, err = d.Tap(Tag{Text: "x"}, 0)
	require.ErrorIs(t, err, ErrNotOpen)
}

func TestNotRemovableReader(t *testing.T) {
	t.Parallel()
	hub := NewHub()
	d, err := hub.Add("r", DeviceOptions{NotRemovable: true})
	require.NoError(t, err)
	r, iq := newTestReader(t, hub, "r")
	assert.NotContains(t, r.Capabilities(), readers.CapabilityRemovable)
	_, err = d.Tap(Tag{Text: "x"}, 0)
	require.NoError(t, err)
	nextScan(t, iq)
	require.ErrorIs(t, d.RemoveTag(), ErrNotRemovable)
}

func TestUnplugReportsErrorAndPlugRestores(t *testing.T) {
	t.Parallel()
	hub := NewHub()
	d, err := hub.Add("r", DeviceOptions{})
	require.NoError(t, err)
	r, iq := newTestReader(t, hub, "r")
	_, err = d.Tap(Tag{Text: "x"}, 0)
	require.NoError(t, err)
	nextScan(t, iq)

	d.Unplug()
	s := nextScan(t, iq)
	assert.True(t, s.ReaderError)
	assert.False(t, r.Connected())
	assert.Empty(t, r.Detect(nil), "an unplugged device is not detected")
	assert.False(t, d.Status().Plugged)

	d.Plug()
	assert.True(t, d.Status().Plugged)
}

func TestScanErrorAndReaderError(t *testing.T) {
	t.Parallel()
	hub := NewHub()
	d, err := hub.Add("r", DeviceOptions{})
	require.NoError(t, err)
	_, iq := newTestReader(t, hub, "r")

	require.NoError(t, d.ScanError("bad read"))
	s := nextScan(t, iq)
	require.Error(t, s.Error)
	assert.Contains(t, s.Error.Error(), "bad read")

	require.NoError(t, d.ReaderError())
	assert.True(t, nextScan(t, iq).ReaderError)
}

func TestWriteToPresentTag(t *testing.T) {
	t.Parallel()
	hub := NewHub()
	d, err := hub.Add("r", DeviceOptions{})
	require.NoError(t, err)
	r, iq := newTestReader(t, hub, "r")
	_, err = d.Tap(Tag{UID: "04bb"}, 0)
	require.NoError(t, err)
	nextScan(t, iq)

	tok, err := r.Write("**launch:snes/game.sfc")
	require.NoError(t, err)
	assert.Equal(t, "04bb", tok.UID)
	assert.Equal(t, "**launch:snes/game.sfc", tok.Text)

	st := d.Status()
	require.Len(t, st.Writes, 1)
	assert.Equal(t, "04bb", st.Writes[0].UID)
	assert.Empty(t, st.Writes[0].Error)
	assert.Equal(t, "**launch:snes/game.sfc", st.Present.Text)

	require.NoError(t, d.RemoveTag())
	assert.True(t, nextScan(t, iq).WrittenTagRemoved, "removing a just-written tag is flagged")
}

func TestWriteWaitsForNextTag(t *testing.T) {
	t.Parallel()
	hub := NewHub()
	d, err := hub.Add("r", DeviceOptions{})
	require.NoError(t, err)
	r, iq := newTestReader(t, hub, "r")

	type result struct {
		err error
		uid string
	}
	done := make(chan result, 1)
	go func() {
		tok, writeErr := r.Write("hello")
		if writeErr != nil {
			done <- result{err: writeErr}
			return
		}
		done <- result{uid: tok.UID}
	}()
	require.Eventually(t, func() bool { return d.Status().Writing }, 2*time.Second, 5*time.Millisecond)

	tag, err := d.CompleteWrite("04cc")
	require.NoError(t, err)
	assert.Equal(t, "04cc", tag.UID)
	got := <-done
	require.NoError(t, got.err)
	assert.Equal(t, "04cc", got.uid)
	noScan(t, iq)
}

func TestWriteFailures(t *testing.T) {
	t.Parallel()
	tests := []struct {
		want error
		mode WriteFailure
	}{
		{mode: WriteFailError},
		{mode: WriteFailRemoved, want: readers.ErrTagRemoved},
		{mode: WriteFailNoTag, want: readers.ErrTagNotDetected},
		{mode: WriteFailUnsupported, want: readers.ErrUnsupportedTagType},
	}
	for _, tt := range tests {
		t.Run(string(tt.mode), func(t *testing.T) {
			t.Parallel()
			hub := NewHub()
			d, err := hub.Add("r", DeviceOptions{})
			require.NoError(t, err)
			r, iq := newTestReader(t, hub, "r")
			_, err = d.Tap(Tag{UID: "04dd"}, 0)
			require.NoError(t, err)
			nextScan(t, iq)

			d.FailNextWrite(tt.mode)
			assert.Equal(t, string(tt.mode), d.Status().PendingFault)
			_, err = r.Write("x")
			require.Error(t, err)
			if tt.want != nil {
				require.ErrorIs(t, err, tt.want)
			}
			assert.NotEmpty(t, d.Status().Writes[0].Error)
			if tt.mode == WriteFailRemoved {
				assert.Nil(t, d.Status().Present, "the tag left mid-write")
				_, err = d.Tap(Tag{UID: "04dd"}, 0)
				require.NoError(t, err)
				nextScan(t, iq)
			}

			_, err = r.Write("y")
			require.NoError(t, err, "a fault applies to one write only")
		})
	}
}

func TestWriteRefusesExcludedAndWrongTags(t *testing.T) {
	t.Parallel()
	hub := NewHub()
	d, err := hub.Add("r", DeviceOptions{})
	require.NoError(t, err)
	r, iq := newTestReader(t, hub, "r")
	_, err = d.Tap(Tag{UID: "04ee"}, 0)
	require.NoError(t, err)
	nextScan(t, iq)

	_, err = r.WriteTarget(context.Background(), "x", readers.WriteOptions{ExcludeUID: "04EE"})
	require.Error(t, err)
	_, err = r.WriteTarget(context.Background(), "x", readers.WriteOptions{TargetUID: "0411"})
	require.Error(t, err)
	_, err = r.WriteTarget(context.Background(), "x", readers.WriteOptions{TargetUID: "04ee"})
	require.NoError(t, err)
}

func TestWriteTimesOutAndCancels(t *testing.T) {
	t.Parallel()
	hub := NewHub()
	_, err := hub.Add("r", DeviceOptions{WriteWait: 20 * time.Millisecond})
	require.NoError(t, err)
	r, _ := newTestReader(t, hub, "r")
	_, err = r.Write("x")
	require.ErrorIs(t, err, readers.ErrTagNotDetected)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = r.WriteTarget(ctx, "x", readers.WriteOptions{})
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestNoWriteReader(t *testing.T) {
	t.Parallel()
	hub := NewHub()
	_, err := hub.Add("r", DeviceOptions{NoWrite: true})
	require.NoError(t, err)
	r, _ := newTestReader(t, hub, "r")
	assert.NotContains(t, r.Capabilities(), readers.CapabilityWrite)
	_, err = r.Write("x")
	require.Error(t, err)
}

func TestCancelWrite(t *testing.T) {
	t.Parallel()
	hub := NewHub()
	d, err := hub.Add("r", DeviceOptions{})
	require.NoError(t, err)
	r, _ := newTestReader(t, hub, "r")
	done := make(chan error, 1)
	go func() {
		_, err := r.Write("x")
		done <- err
	}()
	require.Eventually(t, func() bool { return d.Status().Writing }, 2*time.Second, 5*time.Millisecond)
	r.CancelWrite()
	select {
	case err := <-done:
		require.ErrorIs(t, err, readers.ErrReaderDisconnected)
	case <-time.After(2 * time.Second):
		t.Fatal("write not cancelled")
	}
}

func TestHubListAndRemove(t *testing.T) {
	t.Parallel()
	hub := NewHub()
	b, err := hub.Add("b", DeviceOptions{})
	require.NoError(t, err)
	_, err = hub.Add("a", DeviceOptions{})
	require.NoError(t, err)
	assert.Equal(t, "b", b.Name())

	list := hub.List()
	require.Len(t, list, 2)
	assert.Equal(t, "a", list[0].Name)
	assert.Equal(t, "b", list[1].Name)

	r, _ := newTestReader(t, hub, "b")
	assert.Equal(t, "Sim reader b", r.Info())
	require.NoError(t, r.OnMediaChange(nil))

	require.NoError(t, hub.Remove("b"))
	require.Error(t, hub.Remove("b"))
	assert.Len(t, hub.List(), 1)
	assert.False(t, r.Connected())
}

func TestNewReaderUsesDefaultHub(t *testing.T) {
	t.Parallel()
	r := NewReader(nil)
	assert.Same(t, DefaultHub(), r.hub)
	meta := r.Metadata()
	assert.Equal(t, DriverID, meta.ID)
	assert.True(t, meta.DefaultAutoDetect)
}
