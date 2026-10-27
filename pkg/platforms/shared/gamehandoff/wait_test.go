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

package gamehandoff

import (
	"context"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testSettle = 500 * time.Millisecond
	testWindow = 20 * time.Second
)

var untilClose = Options{Settle: testSettle, OpenWindow: testWindow, UntilClose: true}

// waitHarness stands in for the emulator: the test sends the file events a
// launch produces. The channel is unbuffered, so a completed send means the
// wait has finished handling the previous event.
type waitHarness struct {
	clock  *clockwork.FakeClock
	events chan Kind
}

func startWait(ctx context.Context, opts Options) (harness *waitHarness, out <-chan error) {
	h := &waitHarness{clock: clockwork.NewFakeClock(), events: make(chan Kind)}
	result := make(chan error, 1)
	go func() { result <- Wait(ctx, h.clock, h.events, opts) }()
	return h, result
}

func (h *waitHarness) settle(t *testing.T) {
	t.Helper()
	require.NoError(t, h.clock.BlockUntilContext(t.Context(), 1))
	h.clock.Advance(testSettle)
}

func TestWait_ReadyAfterCloseAndSettle(t *testing.T) {
	t.Parallel()
	h, done := startWait(t.Context(), untilClose)

	h.events <- Open
	h.events <- Close
	h.settle(t)

	require.NoError(t, <-done)
}

func TestWait_NotReadyWhileOpen(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	h, done := startWait(ctx, untilClose)

	h.events <- Open
	require.NoError(t, h.clock.BlockUntilContext(t.Context(), 0))
	h.clock.Advance(time.Hour)
	cancel()

	require.ErrorIs(t, <-done, context.Canceled)
}

func TestWait_ReopenDuringSettleRestartsTheWait(t *testing.T) {
	t.Parallel()
	h, done := startWait(t.Context(), untilClose)

	h.events <- Open
	h.events <- Close
	require.NoError(t, h.clock.BlockUntilContext(t.Context(), 1))
	h.events <- Open
	require.NoError(t, h.clock.BlockUntilContext(t.Context(), 0))
	h.clock.Advance(testSettle)
	select {
	case err := <-done:
		t.Fatalf("ready while the file was open again: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	h.events <- Close
	h.settle(t)

	require.NoError(t, <-done)
}

func TestWait_EveryOpenMustClose(t *testing.T) {
	t.Parallel()
	h, done := startWait(t.Context(), untilClose)

	h.events <- Open
	h.events <- Open
	h.events <- Close
	require.NoError(t, h.clock.BlockUntilContext(t.Context(), 0))
	h.clock.Advance(time.Hour)
	select {
	case err := <-done:
		t.Fatalf("ready with one open still outstanding: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	h.events <- Close
	h.settle(t)

	require.NoError(t, <-done)
}

func TestWait_MountedImageReadyAfterOpen(t *testing.T) {
	t.Parallel()
	h, done := startWait(t.Context(), Options{Settle: testSettle, OpenWindow: testWindow})

	h.events <- Open
	// The open swaps the wait for an open for the settle, so the timers are
	// not countable; keep advancing until the settle fires.
	for {
		select {
		case err := <-done:
			require.NoError(t, err)
			return
		case <-time.After(time.Millisecond):
			h.clock.Advance(testSettle)
		}
	}
}

func TestWait_NeverOpenedIsReadyAfterWindow(t *testing.T) {
	t.Parallel()
	h, done := startWait(t.Context(), untilClose)

	require.NoError(t, h.clock.BlockUntilContext(t.Context(), 1))
	h.clock.Advance(testWindow)

	require.NoError(t, <-done)
}

func TestWait_WindowDoesNotCutOffALongTransfer(t *testing.T) {
	t.Parallel()
	h, done := startWait(t.Context(), untilClose)

	h.events <- Open
	require.NoError(t, h.clock.BlockUntilContext(t.Context(), 0))
	h.clock.Advance(testWindow + time.Minute)
	select {
	case err := <-done:
		t.Fatalf("ready while the transfer was still running: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	h.events <- Close
	h.settle(t)

	require.NoError(t, <-done)
}

func TestWait_CancellationIsReported(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	_, done := startWait(ctx, untilClose)

	cancel()

	require.ErrorIs(t, <-done, context.Canceled)
}

func TestWait_EndedWatchIsUnavailable(t *testing.T) {
	t.Parallel()
	h, done := startWait(t.Context(), untilClose)

	close(h.events)

	err := <-done
	require.ErrorIs(t, err, platforms.ErrReadinessUnavailable)
	assert.NotContains(t, err.Error(), "%!w")
}

func TestLaunchLog(t *testing.T) {
	t.Parallel()
	clock := clockwork.NewFakeClock()
	log := NewLaunchLog(clock)

	assert.False(t, log.Recent("/games/a.nes", time.Minute), "nothing launched yet")

	log.Note("/games/a.nes")
	assert.True(t, log.Recent("/games/a.nes", time.Minute))
	assert.False(t, log.Recent("/games/b.nes", time.Minute), "another game")

	clock.Advance(2 * time.Minute)
	assert.False(t, log.Recent("/games/a.nes", time.Minute), "a stale launch")

	log.Note("/games/b.nes")
	assert.False(t, log.Recent("/games/a.nes", time.Minute), "a newer launch replaces it")
	assert.True(t, log.Recent("/games/b.nes", time.Minute))
}

func TestWait_IdleOpenFileCountsAsHandedOver(t *testing.T) {
	t.Parallel()
	opts := Options{Settle: testSettle, OpenWindow: testWindow, UntilClose: true, IdleHandover: 2 * time.Second}
	h, done := startWait(t.Context(), opts)

	// A floppy image: opened, read once, then held open for the session. The
	// timers swap places as events arrive, so keep advancing until it ends.
	h.events <- Open
	h.events <- Access
	h.advanceUntilDone(t, done)
}

func TestWait_ReadsKeepAnOpenFileFromGoingIdle(t *testing.T) {
	t.Parallel()
	opts := Options{Settle: testSettle, OpenWindow: testWindow, UntilClose: true, IdleHandover: 2 * time.Second}
	h, done := startWait(t.Context(), opts)

	h.events <- Open
	for range 5 {
		require.NoError(t, h.clock.BlockUntilContext(t.Context(), 1))
		h.clock.Advance(opts.IdleHandover - time.Millisecond)
		h.events <- Access
	}
	select {
	case err := <-done:
		t.Fatalf("handed over while the file was still being read: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	h.events <- Close
	h.advanceUntilDone(t, done)
}

func TestWait_ClosedFileHandsOverOnceWithoutIdleHelp(t *testing.T) {
	t.Parallel()
	opts := Options{Settle: testSettle, OpenWindow: testWindow, UntilClose: true, IdleHandover: 2 * time.Second}
	h, done := startWait(t.Context(), opts)

	h.events <- Open
	h.events <- Close
	h.advanceUntilDone(t, done)
}

// advanceUntilDone keeps advancing the fake clock until the wait returns, for
// tests where timers are replaced by events and cannot be counted.
func (h *waitHarness) advanceUntilDone(t *testing.T, done <-chan error) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case err := <-done:
			require.NoError(t, err)
			return
		case <-deadline:
			t.Fatal("wait did not finish")
		case <-time.After(time.Millisecond):
			h.clock.Advance(testSettle)
		}
	}
}

func TestLaunchLog_MatchesRespelledPath(t *testing.T) {
	t.Parallel()
	clock := clockwork.NewFakeClock()
	l := NewLaunchLog(clock)
	assert.False(t, l.Recent("/roms/a.nes", time.Minute), "nothing launched yet")

	l.Note("/userdata/roms/nes/../nes/a.nes")

	assert.True(t, l.Recent("/userdata/roms/nes/a.nes", time.Minute), "cleaned path")
	assert.True(t, l.Recent("/media/share/roms/nes/a.nes", time.Minute), "same file name, other mount")
	assert.False(t, l.Recent("/userdata/roms/nes/b.nes", time.Minute))
}

// MiSTer, 2026-10-26: Main took up to 14 s to load a core while the card was
// busy, so a window counted from the launch ran out before the game was even
// opened. A held window only starts once the emulator is up.
func TestWait_HeldWindowStartsWhenTheEmulatorIsUp(t *testing.T) {
	t.Parallel()
	start := make(chan struct{})
	opts := untilClose
	opts.OpenWindowStart = start
	h, done := startWait(t.Context(), opts)

	h.clock.Advance(testWindow * 2)
	select {
	case err := <-done:
		t.Fatalf("window ran before the emulator was up: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	close(start)
	require.NoError(t, h.clock.BlockUntilContext(t.Context(), 1))
	h.clock.Advance(testWindow)

	require.NoError(t, <-done, "never opened once up, treated as handed over")
}

// An open before the emulator reports itself up still counts as the handover.
func TestWait_OpenBeforeTheWindowStartsCounts(t *testing.T) {
	t.Parallel()
	start := make(chan struct{})
	opts := untilClose
	opts.OpenWindowStart = start
	h, done := startWait(t.Context(), opts)

	h.events <- Open
	h.events <- Close
	h.settle(t)

	require.NoError(t, <-done)
}
