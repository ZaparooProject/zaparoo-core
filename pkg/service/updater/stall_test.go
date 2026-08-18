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

package updater

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// trickleReader stands in for a slow but healthy transfer: one byte at a time,
// with a pause between, for far longer in total than the stall timeout.
type trickleReader struct {
	interval time.Duration
	left     int
}

func (r *trickleReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if r.left == 0 {
		return 0, io.EOF
	}
	time.Sleep(r.interval)
	r.left--
	p[0] = 'x'
	return 1, nil
}

// emptyReader returns without producing bytes, which must not count as progress.
type emptyReader struct{ calls int }

func (r *emptyReader) Read([]byte) (int, error) {
	r.calls++
	if r.calls > 1 {
		return 0, io.EOF
	}
	return 0, nil
}

// TestStallGuard_ProgressKeepsASlowTransferAlive is the property the guard
// exists for, and the one nothing else in the package covers: it bounds silence,
// not duration. This transfer runs four times longer than the timeout while
// never going quiet for it, and has to survive.
//
// It is also the regression test for the tracking itself. The guard records
// progress once when it is constructed, so if reads stopped reporting it this
// would fail while a stall test would not notice.
func TestStallGuard_ProgressKeepsASlowTransferAlive(t *testing.T) {
	t.Parallel()

	const (
		timeout  = 500 * time.Millisecond
		interval = 50 * time.Millisecond
		total    = 40
	)

	ctx, guard := newStallGuard(context.Background(), timeout)
	defer guard.stop()

	n, err := io.Copy(io.Discard, guard.reader(&trickleReader{interval: interval, left: total}))
	require.NoError(t, err)
	assert.EqualValues(t, total, n)
	assert.False(t, guard.tripped(), "a transfer that never went quiet was reported as stalled")
	assert.NoError(t, ctx.Err())
}

func TestStallGuard_SilenceTrips(t *testing.T) {
	t.Parallel()

	ctx, guard := newStallGuard(context.Background(), 200*time.Millisecond)
	defer guard.stop()

	select {
	case <-ctx.Done():
	case <-time.After(30 * time.Second):
		t.Fatal("the guard never fired on a transfer that made no progress")
	}
	assert.True(t, guard.tripped(), "the guard cancelled without recording that it was the one that did")
}

// TestStallGuard_CallerCancelIsNotAStall keeps the two reasons a transfer stops
// distinguishable. Both arrive as a cancelled context, and only the guard's own
// record of firing tells them apart.
func TestStallGuard_CallerCancelIsNotAStall(t *testing.T) {
	t.Parallel()

	parent, cancel := context.WithCancel(context.Background())
	ctx, guard := newStallGuard(parent, time.Hour)
	defer guard.stop()

	cancel()
	<-ctx.Done()
	assert.False(t, guard.tripped(), "a caller giving up was recorded as a stall")
}

func TestStallGuard_StopIsIdempotent(t *testing.T) {
	t.Parallel()

	_, guard := newStallGuard(context.Background(), time.Hour)
	guard.stop()
	guard.stop()
}

// TestStallGuard_TinyTimeoutDoesNotPanic covers the interval floor. Dividing the
// timeout by the check count can reach zero, and time.NewTicker panics on that.
func TestStallGuard_TinyTimeoutDoesNotPanic(t *testing.T) {
	t.Parallel()

	ctx, guard := newStallGuard(context.Background(), time.Nanosecond)
	defer guard.stop()

	select {
	case <-ctx.Done():
	case <-time.After(30 * time.Second):
		t.Fatal("the guard never fired")
	}
	assert.True(t, guard.tripped())
}

func TestProgressReader_CountsBytesAndPassesThrough(t *testing.T) {
	t.Parallel()

	_, guard := newStallGuard(context.Background(), time.Hour)
	defer guard.stop()

	before := guard.progress.Load()
	time.Sleep(time.Millisecond)

	r := guard.reader(strings.NewReader("zaparoo"))
	buf := make([]byte, 4)
	n, err := r.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, 4, n)
	assert.Equal(t, "zapa", string(buf))
	assert.Greater(t, guard.progress.Load(), before,
		"a read that produced bytes did not count as progress")

	rest, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Equal(t, "roo", string(rest))
}

func TestProgressReader_EmptyReadIsNotProgress(t *testing.T) {
	t.Parallel()

	_, guard := newStallGuard(context.Background(), time.Hour)
	defer guard.stop()

	time.Sleep(time.Millisecond)
	before := guard.progress.Load()

	r := guard.reader(&emptyReader{})
	n, err := r.Read(make([]byte, 4))
	require.NoError(t, err)
	assert.Zero(t, n)
	assert.Equal(t, before, guard.progress.Load(),
		"a read that produced nothing was counted as progress")
}
