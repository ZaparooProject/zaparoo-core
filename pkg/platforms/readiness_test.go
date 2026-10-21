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

package platforms

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testPollInterval = 250 * time.Millisecond

// pollResult runs PollReady in the background so the test can drive the fake
// clock between checks.
func pollResult(
	ctx context.Context, clock clockwork.Clock, check ReadyCheck,
) <-chan error {
	done := make(chan error, 1)
	go func() { done <- PollReady(ctx, clock, testPollInterval, check) }()
	return done
}

func TestPollReady_ReadyOnFirstCheck(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	err := PollReady(t.Context(), clockwork.NewFakeClock(), testPollInterval,
		func(context.Context) (bool, error) {
			calls.Add(1)
			return true, nil
		})

	require.NoError(t, err)
	assert.Equal(t, int32(1), calls.Load())
}

func TestPollReady_RetriesUntilReady(t *testing.T) {
	t.Parallel()

	clock := clockwork.NewFakeClock()
	var calls atomic.Int32
	done := pollResult(t.Context(), clock, func(context.Context) (bool, error) {
		return calls.Add(1) == 3, nil
	})

	for range 2 {
		require.NoError(t, clock.BlockUntilContext(t.Context(), 1))
		clock.Advance(testPollInterval)
	}

	require.NoError(t, <-done)
	assert.Equal(t, int32(3), calls.Load())
}

func TestPollReady_UnavailableIsRetried(t *testing.T) {
	t.Parallel()

	clock := clockwork.NewFakeClock()
	var calls atomic.Int32
	done := pollResult(t.Context(), clock, func(context.Context) (bool, error) {
		if calls.Add(1) == 1 {
			return false, ErrReadinessUnavailable
		}
		return true, nil
	})

	require.NoError(t, clock.BlockUntilContext(t.Context(), 1))
	clock.Advance(testPollInterval)

	require.NoError(t, <-done)
	assert.Equal(t, int32(2), calls.Load())
}

func TestPollReady_PermanentErrorEndsWait(t *testing.T) {
	t.Parallel()

	boom := errors.New("bad credentials")
	err := PollReady(t.Context(), clockwork.NewFakeClock(), testPollInterval,
		func(context.Context) (bool, error) { return false, boom })

	require.ErrorIs(t, err, boom)
	assert.NotErrorIs(t, err, ErrReadinessUnavailable)
}

func TestPollReady_TimeoutWhileNotReady(t *testing.T) {
	t.Parallel()

	clock := clockwork.NewFakeClock()
	ctx, cancel := context.WithTimeout(t.Context(), time.Hour)
	defer cancel()
	done := pollResult(ctx, clock, func(context.Context) (bool, error) { return false, nil })

	require.NoError(t, clock.BlockUntilContext(t.Context(), 1))
	cancel()

	err := <-done
	require.ErrorIs(t, err, context.Canceled)
	assert.NotErrorIs(t, err, ErrReadinessUnavailable, "slow media is not a missing integration")
}

func TestPollReady_TimeoutWhileUnavailable(t *testing.T) {
	t.Parallel()

	clock := clockwork.NewFakeClock()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := pollResult(ctx, clock, func(context.Context) (bool, error) {
		return false, ErrReadinessUnavailable
	})

	require.NoError(t, clock.BlockUntilContext(t.Context(), 1))
	cancel()

	err := <-done
	require.ErrorIs(t, err, ErrReadinessUnavailable)
	require.ErrorIs(t, err, context.Canceled)
}

func TestPollReady_UnavailableThenNotReadyIsNotUnavailable(t *testing.T) {
	t.Parallel()

	clock := clockwork.NewFakeClock()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var calls atomic.Int32
	done := pollResult(ctx, clock, func(context.Context) (bool, error) {
		if calls.Add(1) == 1 {
			return false, ErrReadinessUnavailable
		}
		return false, nil
	})

	require.NoError(t, clock.BlockUntilContext(t.Context(), 1))
	clock.Advance(testPollInterval)
	require.NoError(t, clock.BlockUntilContext(t.Context(), 1))
	cancel()

	err := <-done
	require.ErrorIs(t, err, context.Canceled)
	assert.NotErrorIs(t, err, ErrReadinessUnavailable, "the integration answered on the second check")
}

func TestPollReady_CheckFailingBecauseWaitEnded(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	err := PollReady(ctx, clockwork.NewFakeClock(), testPollInterval,
		func(c context.Context) (bool, error) {
			cancel()
			return false, c.Err()
		})

	require.ErrorIs(t, err, context.Canceled)
}
