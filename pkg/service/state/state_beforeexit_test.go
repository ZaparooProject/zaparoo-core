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
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunBeforeExitHook_NilHookIsNoOp(t *testing.T) {
	t.Parallel()
	state, _ := NewState(mocks.NewMockPlatform(), "test-boot-uuid")

	assert.NotPanics(t, state.RunBeforeExitHook)
}

// A before_exit script that itself stops or launches media re-enters this path.
// The guard must let the outer run finish and drop the nested one, rather than
// recursing until the stack blows.
func TestRunBeforeExitHook_SuppressesNestedRun(t *testing.T) {
	t.Parallel()
	state, _ := NewState(mocks.NewMockPlatform(), "test-boot-uuid")

	calls := 0
	state.SetBeforeExitHook(func() {
		calls++
		state.RunBeforeExitHook()
	})

	state.RunBeforeExitHook()

	assert.Equal(t, 1, calls, "nested before_exit run should be dropped")
}

// The hold-mode exit timer, a playtime limit and an API stop all run on
// different goroutines and can race. Only one before_exit script may run.
func TestRunBeforeExitHook_SuppressesConcurrentRun(t *testing.T) {
	t.Parallel()
	state, _ := NewState(mocks.NewMockPlatform(), "test-boot-uuid")

	entered := make(chan struct{})
	release := make(chan struct{})
	calls := make(chan struct{}, 2)

	state.SetBeforeExitHook(func() {
		calls <- struct{}{}
		close(entered)
		<-release
	})

	go state.RunBeforeExitHook()
	<-entered

	// Second caller must return immediately rather than block on the first.
	done := make(chan struct{})
	go func() {
		state.RunBeforeExitHook()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("concurrent RunBeforeExitHook blocked instead of returning")
	}

	close(release)
	assert.Len(t, calls, 1, "only one before_exit script should have run")
}

func TestRunBeforeExitHook_ReleasesGuardAfterPanic(t *testing.T) {
	t.Parallel()
	state, _ := NewState(mocks.NewMockPlatform(), "test-boot-uuid")

	calls := 0
	state.SetBeforeExitHook(func() {
		calls++
		if calls == 1 {
			panic("boom")
		}
	})

	assert.Panics(t, state.RunBeforeExitHook)

	// The guard must not stay set, or before_exit is dead for the rest of the
	// process lifetime.
	require.NotPanics(t, state.RunBeforeExitHook)
	assert.Equal(t, 2, calls)
}

// The hook runs real ZapScript, which reads and writes state. Holding s.mu
// across the callback would deadlock every such script.
func TestRunBeforeExitHook_DoesNotHoldStateLock(t *testing.T) {
	t.Parallel()
	state, _ := NewState(mocks.NewMockPlatform(), "test-boot-uuid")
	state.SetActiveMedia(models.NewActiveMedia("NES", "NES", "game.nes", "Game", "test-launcher"))

	done := make(chan struct{})
	state.SetBeforeExitHook(func() {
		_ = state.ActiveMedia()
		state.SetActiveMedia(nil)
		close(done)
	})

	go state.RunBeforeExitHook()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("before_exit hook deadlocked against the state lock")
	}
}
