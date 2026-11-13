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

package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/bluetooth/ble"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseScript(t *testing.T) {
	t.Parallel()
	steps, err := parseScript("abc123:5s, -:250ms ,04a1b2:1m")
	require.NoError(t, err)
	assert.Equal(t, []readerStep{
		{uid: "abc123", d: 5 * time.Second},
		{uid: "", d: 250 * time.Millisecond},
		{uid: "04a1b2", d: time.Minute},
	}, steps)

	_, err = parseScript("abc123")
	require.EqualError(t, err, `script step "abc123" is not uid:duration`)
	_, err = parseScript("abc123:soon")
	require.ErrorContains(t, err, `script step "abc123:soon"`)
	// A step that takes no time would let a looping script spin.
	_, err = parseScript("abc123:0s")
	require.EqualError(t, err, `script step "abc123:0s" needs a duration above zero`)
	_, err = parseScript("abc123:1s,-:-1s")
	require.EqualError(t, err, `script step "-:-1s" needs a duration above zero`)
}

type readerRig struct {
	adapter *mocks.FakeAdapter
	clock   *clockwork.FakeClock
	done    chan error
	env     env
}

func newReaderRig(t *testing.T) *readerRig {
	t.Helper()
	r := &readerRig{
		adapter: mocks.NewFakeAdapter(),
		clock:   clockwork.NewFakeClock(),
		done:    make(chan error, 1),
	}
	r.env = env{clock: r.clock, open: func(context.Context) (ble.Adapter, error) { return r.adapter, nil }}
	return r
}

func (r *readerRig) start(ctx context.Context, args ...string) {
	go func() { r.done <- run(ctx, r.env, append([]string{"reader"}, args...)) }()
}

// waitForStep blocks until playStep has armed its deadline and its ticker.
func (r *readerRig) waitForStep(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	require.NoError(t, r.clock.BlockUntilContext(ctx, 2))
}

func (r *readerRig) nextScan(t *testing.T) string {
	t.Helper()
	select {
	case n := <-r.adapter.Periph.Notifications:
		assert.Equal(t, nusTXUUID, n.CharUUID)
		return string(n.Value)
	case <-time.After(5 * time.Second):
		require.FailNow(t, "no scan notified")
		return ""
	}
}

func (r *readerRig) result(t *testing.T) error {
	t.Helper()
	select {
	case err := <-r.done:
		return err
	case <-time.After(5 * time.Second):
		require.FailNow(t, "reader did not stop")
		return nil
	}
}

func TestReaderPlaysTheScriptOnce(t *testing.T) {
	t.Parallel()
	r := newReaderRig(t)
	r.start(t.Context(), "-name", "pad", "-script", "abc123:1s", "-loop=false")

	assert.Equal(t, "SCAN\tuid=abc123\n", r.nextScan(t))
	r.waitForStep(t)
	r.clock.Advance(scanRepeat)
	assert.Equal(t, "SCAN\tuid=abc123\n", r.nextScan(t), "a present token repeats")
	r.clock.Advance(time.Second)
	require.NoError(t, r.result(t))

	// Serve runs on its own goroutine, which a one-step script can outrun.
	require.Eventually(t, func() bool { return r.adapter.Periph.Handler() != nil }, 5*time.Second, time.Millisecond)
	app := r.adapter.Periph.Application()
	require.Len(t, app.Services, 1)
	assert.Equal(t, nusServiceUUID, app.Services[0].UUID)
	assert.Equal(t, ble.Advertisement{LocalName: "pad", ServiceUUIDs: []string{nusServiceUUID}},
		r.adapter.Periph.Advertisement())
	assert.True(t, r.adapter.Closed())
}

func TestReaderLoopsUntilInterrupted(t *testing.T) {
	t.Parallel()
	r := newReaderRig(t)
	ctx, cancel := context.WithCancel(t.Context())
	r.start(ctx, "-script", "abc123:1s")

	r.nextScan(t)
	r.waitForStep(t)
	r.clock.Advance(time.Second)
	// The first step's deadline has fired and left only its ticker, so two
	// waiters again means the script has started over.
	r.waitForStep(t)
	cancel()
	require.NoError(t, r.result(t), "an interrupt is a clean stop")
}

func TestReaderWithoutATokenSendsNothing(t *testing.T) {
	t.Parallel()
	r := newReaderRig(t)
	r.start(t.Context(), "-script", "-:1s", "-loop=false")

	r.waitForStep(t)
	r.clock.Advance(time.Second)
	require.NoError(t, r.result(t))
	assert.Empty(t, r.adapter.Periph.Notifications)
}

func TestReaderIgnoresNotifyWithoutASubscriber(t *testing.T) {
	t.Parallel()
	r := newReaderRig(t)
	r.adapter.Periph.NotifyErr = ble.ErrNotFound
	r.start(t.Context(), "-script", "abc:1s", "-loop=false")

	r.waitForStep(t)
	r.clock.Advance(time.Second)
	require.NoError(t, r.result(t))
}

func TestReaderStopsOnNotifyFailure(t *testing.T) {
	t.Parallel()
	r := newReaderRig(t)
	r.adapter.Periph.NotifyErr = errors.New("adapter gone")
	r.start(t.Context(), "-script", "abc:1s")

	require.EqualError(t, r.result(t), "notify: adapter gone")
}

func TestReaderStopsWhenServingFails(t *testing.T) {
	t.Parallel()
	r := newReaderRig(t)
	r.adapter.Periph.ServeErr = errors.New("advertisement refused")
	r.start(t.Context(), "-script", "-:1s")

	for {
		select {
		case err := <-r.done:
			require.EqualError(t, err, "serve: advertisement refused")
			return
		default:
		}
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
		if r.clock.BlockUntilContext(ctx, 2) == nil {
			r.clock.Advance(time.Second)
		}
		cancel()
	}
}

func TestReaderStopsMidStepWhenServingFails(t *testing.T) {
	t.Parallel()
	r := newReaderRig(t)
	r.adapter.Periph.ServeErr = errors.New("advertisement refused")
	r.start(t.Context(), "-script", "-:1h", "-loop=false")

	// The clock never moves, so only the serve failure can end the step.
	require.EqualError(t, r.result(t), "serve: advertisement refused")
}

func TestReaderSetupFailures(t *testing.T) {
	t.Parallel()

	err := run(t.Context(), env{}, []string{"reader", "-script", "nonsense"})
	require.ErrorContains(t, err, "not uid:duration")

	opened := errors.New("no adapter")
	err = run(t.Context(), env{open: func(context.Context) (ble.Adapter, error) { return nil, opened }},
		[]string{"reader"})
	require.ErrorIs(t, err, opened)

	centralOnly := mocks.NewFakeAdapter(ble.RoleCentral)
	err = run(t.Context(), env{open: func(context.Context) (ble.Adapter, error) { return centralOnly, nil }},
		[]string{"reader"})
	require.ErrorIs(t, err, ble.ErrRoleUnsupported)
	assert.True(t, centralOnly.Closed())
}

func TestReaderHandler(t *testing.T) {
	t.Parallel()
	var h ble.PeripheralHandler = readerHandler{}
	peer := ble.Peer{Address: fakeCoreAddr}
	h.OnWrite(peer, nusRXUUID, []byte("hello"), 23)
	h.OnSubscribe(peer, nusTXUUID, true)
	h.OnDisconnect(peer)
	_, err := h.OnRead(peer, nusTXUUID)
	require.ErrorIs(t, err, ble.ErrNotFound)
}

func TestRunNeedsAMode(t *testing.T) {
	t.Parallel()
	require.ErrorContains(t, run(t.Context(), env{}, nil), "usage")
	require.EqualError(t, run(t.Context(), env{}, []string{"server"}),
		`unknown mode "server"; want client or reader`)
}

func TestStringList(t *testing.T) {
	t.Parallel()
	var s stringList
	require.NoError(t, s.Set("a"))
	require.NoError(t, s.Set("b"))
	assert.Equal(t, stringList{"a", "b"}, s)
	assert.Empty(t, s.String())
}
