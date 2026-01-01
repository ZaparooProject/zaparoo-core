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

package state

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLauncherManager_NewContext(t *testing.T) {
	t.Parallel()

	lm := NewLauncherManager()
	ctx1 := lm.GetContext()
	ctx2 := lm.NewContext()

	assert.NotEqual(t, ctx1, ctx2)
	require.Error(t, ctx1.Err())
	assert.Equal(t, context.Canceled, ctx1.Err())
	assert.NoError(t, ctx2.Err())
}

func TestLauncherManager_NewContext_CancelsMultipleTimes(t *testing.T) {
	t.Parallel()

	lm := NewLauncherManager()
	ctx1 := lm.GetContext()
	ctx2 := lm.NewContext()
	ctx3 := lm.NewContext()

	require.Error(t, ctx1.Err())
	require.Error(t, ctx2.Err())
	assert.NoError(t, ctx3.Err())
}

func TestLauncherManager_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	lm := NewLauncherManager()
	done := make(chan bool)

	for range 10 {
		go func() {
			defer func() { done <- true }()
			for range 100 {
				_ = lm.GetContext()
			}
		}()
	}

	for range 5 {
		go func() {
			defer func() { done <- true }()
			for range 50 {
				_ = lm.NewContext()
				time.Sleep(time.Millisecond)
			}
		}()
	}

	for range 15 {
		<-done
	}
}

func TestLauncherManager_ContextCancellationSignaling(t *testing.T) {
	t.Parallel()

	lm := NewLauncherManager()
	ctx := lm.GetContext()

	cancelled := make(chan bool)
	go func() {
		<-ctx.Done()
		cancelled <- true
	}()

	_ = lm.NewContext()

	select {
	case <-cancelled:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("context was not cancelled")
	}
}

func TestLauncherManager_LauncherSupersession(t *testing.T) {
	t.Parallel()

	lm := NewLauncherManager()

	launcher1Ctx := lm.GetContext()
	require.NoError(t, launcher1Ctx.Err())

	time.Sleep(10 * time.Millisecond)

	// New launcher supersedes previous one
	launcher2Ctx := lm.NewContext()

	// Previous context is cancelled so cleanup routines can detect staleness
	require.Error(t, launcher1Ctx.Err())
	assert.NoError(t, launcher2Ctx.Err())
}

func TestLauncherManager_TryStartLaunch(t *testing.T) {
	t.Parallel()

	lm := NewLauncherManager()

	err := lm.TryStartLaunch()
	require.NoError(t, err)

	err = lm.TryStartLaunch()
	require.ErrorIs(t, err, ErrLaunchInProgress)

	lm.EndLaunch()
	err = lm.TryStartLaunch()
	require.NoError(t, err)
}

func TestLauncherManager_LaunchGuardConcurrent(t *testing.T) {
	t.Parallel()

	lm := NewLauncherManager()
	var successCount atomic.Int32
	done := make(chan bool)

	for range 10 {
		go func() {
			defer func() { done <- true }()
			if err := lm.TryStartLaunch(); err == nil {
				successCount.Add(1)
				time.Sleep(10 * time.Millisecond)
				lm.EndLaunch()
			}
		}()
	}

	for range 10 {
		<-done
	}

	assert.GreaterOrEqual(t, successCount.Load(), int32(1))
}
