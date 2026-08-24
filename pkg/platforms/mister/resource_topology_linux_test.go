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

package mister

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
)

func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	originalLogger := log.Logger
	log.Logger = zerolog.New(&buf).Level(zerolog.DebugLevel)
	t.Cleanup(func() { log.Logger = originalLogger })
	return &buf
}

func TestLoggedFailureLogsFirstOccurrenceAtWarnAndRepeatsAtDebug(t *testing.T) {
	buf := captureLogs(t)

	var failure loggedFailure
	err := errors.New("boom")
	failure.log(err, "widget failed")
	failure.log(err, "widget failed")
	failure.log(err, "widget failed")

	assert.Equal(t, 1, strings.Count(buf.String(), `"level":"warn"`))
	assert.Equal(t, 2, strings.Count(buf.String(), `"level":"debug"`))
}

func TestLoggedFailureReLogsAtWarnAfterClear(t *testing.T) {
	buf := captureLogs(t)

	var failure loggedFailure
	err := errors.New("boom")
	failure.log(err, "widget failed")
	failure.clear()
	failure.log(err, "widget failed")

	assert.Equal(t, 2, strings.Count(buf.String(), `"level":"warn"`))
	assert.Zero(t, strings.Count(buf.String(), `"level":"debug"`))
}

func TestLoggedFailureReLogsAtWarnWhenErrorTextChanges(t *testing.T) {
	buf := captureLogs(t)

	var failure loggedFailure
	failure.log(errors.New("boom"), "widget failed")
	failure.log(errors.New("bang"), "widget failed")

	assert.Equal(t, 2, strings.Count(buf.String(), `"level":"warn"`))
}

// TestRunResourceTopologyManagerSuppressesRepeatWarnLogsForPersistentFailure
// pins the fix for unbounded per-tick Warn log spam: a hook that fails with
// the same error on every tick (e.g. the frontend lease directory missing,
// or the MMC IRQ never found) must only Warn-log the first occurrence, not
// once per tick, while the hook is still retried every tick regardless (no
// backoff: instant recovery matters more than a saved retry here).
func TestRunResourceTopologyManagerSuppressesRepeatWarnLogsForPersistentFailure(t *testing.T) {
	buf := captureLogs(t)

	leaseErr := errors.New("frontend lease directory missing")
	var leaseCalls int
	hooks := resourceTopologyHooks{
		leaseActive: func() (bool, error) {
			leaseCalls++
			return false, leaseErr
		},
		setCoreAffinity: func(bool) error { return nil },
		setMMCAffinity:  func(bool) error { return nil },
	}

	ctx, cancel := context.WithCancel(context.Background())
	ticks := make(chan time.Time)
	done := make(chan struct{})
	go func() {
		runResourceTopologyManager(ctx, ticks, hooks)
		close(done)
	}()

	const tickCount = 5
	for range tickCount {
		ticks <- time.Now()
	}
	cancel()
	<-done

	// One call happens immediately on entry, plus one per consumed tick.
	assert.Equal(t, tickCount+1, leaseCalls)
	assert.Equal(t, 1, strings.Count(buf.String(), `"level":"warn"`))
	assert.Equal(t, tickCount, strings.Count(buf.String(), `"level":"debug"`))
}

// TestRunResourceTopologyManagerReWarnsAfterRecoveryAndFailureAgain pins that
// suppression is per persistent-failure streak, not permanent: a transient
// failure that clears on success and then recurs must Warn-log again rather
// than staying silently demoted to Debug forever.
func TestRunResourceTopologyManagerReWarnsAfterRecoveryAndFailureAgain(t *testing.T) {
	buf := captureLogs(t)

	leaseErr := errors.New("frontend lease directory missing")
	sequence := []error{leaseErr, leaseErr, nil, leaseErr, leaseErr}
	call := 0
	hooks := resourceTopologyHooks{
		leaseActive: func() (bool, error) {
			err := sequence[call]
			call++
			return err == nil, err
		},
		setCoreAffinity: func(bool) error { return nil },
		setMMCAffinity:  func(bool) error { return nil },
	}

	ctx, cancel := context.WithCancel(context.Background())
	ticks := make(chan time.Time)
	done := make(chan struct{})
	go func() {
		runResourceTopologyManager(ctx, ticks, hooks)
		close(done)
	}()

	for range len(sequence) - 1 {
		ticks <- time.Now()
	}
	cancel()
	<-done

	assert.Equal(t, len(sequence), call)
	// First failure streak (calls 1-2): one Warn, one Debug repeat.
	// Success (call 3) clears it. Second failure streak (calls 4-5): a
	// fresh Warn, one more Debug repeat.
	assert.Equal(t, 2, strings.Count(buf.String(), `"level":"warn"`))
	assert.Equal(t, 2, strings.Count(buf.String(), `"level":"debug"`))
}
