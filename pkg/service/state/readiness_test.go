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
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActiveMediaReadinessStartsPendingAndMarksReady(t *testing.T) {
	t.Parallel()

	st, _ := NewState(mocks.NewMockPlatform(), "test-boot")
	st.SetActiveMedia(models.NewActiveMedia("nes", "NES", "game.nes", "Game", "retroarch"))

	assert.False(t, st.ActiveMediaReady())
	gen, ok := st.ActiveMediaReadyGeneration()
	require.True(t, ok)

	waitDone := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		waitDone <- st.WaitForActiveMediaReady(ctx, gen)
	}()

	select {
	case err := <-waitDone:
		t.Fatalf("wait returned before media was ready: %v", err)
	case <-time.After(25 * time.Millisecond):
	}

	st.MarkActiveMediaReady(gen)

	select {
	case err := <-waitDone:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("wait did not return after media was marked ready")
	}
	assert.True(t, st.ActiveMediaReady())
}

func TestActiveMediaReadinessOldGenerationCannotMarkNewMediaReady(t *testing.T) {
	t.Parallel()

	st, _ := NewState(mocks.NewMockPlatform(), "test-boot")
	st.SetActiveMedia(models.NewActiveMedia("nes", "NES", "game-a.nes", "Game A", "retroarch"))
	oldGen, ok := st.ActiveMediaReadyGeneration()
	require.True(t, ok)

	st.SetActiveMedia(models.NewActiveMedia("snes", "SNES", "game-b.sfc", "Game B", "retroarch"))
	newGen, ok := st.ActiveMediaReadyGeneration()
	require.True(t, ok)
	require.NotEqual(t, oldGen, newGen)

	st.MarkActiveMediaReady(oldGen)
	assert.False(t, st.ActiveMediaReady())

	st.MarkActiveMediaReady(newGen)
	assert.True(t, st.ActiveMediaReady())
}

func TestActiveMediaReadinessChangeReleasesWaitersWithChangedError(t *testing.T) {
	t.Parallel()

	st, _ := NewState(mocks.NewMockPlatform(), "test-boot")
	st.SetActiveMedia(models.NewActiveMedia("nes", "NES", "game-a.nes", "Game A", "retroarch"))
	gen, ok := st.ActiveMediaReadyGeneration()
	require.True(t, ok)

	waitDone := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		waitDone <- st.WaitForActiveMediaReady(ctx, gen)
	}()

	st.SetActiveMedia(models.NewActiveMedia("snes", "SNES", "game-b.sfc", "Game B", "retroarch"))

	select {
	case err := <-waitDone:
		require.Error(t, err)
		require.ErrorIs(t, err, ErrActiveMediaChanged)
	case <-time.After(time.Second):
		t.Fatal("wait did not return after media changed")
	}
}

func TestActiveMediaReadinessStopReleasesWaitersWithNoActiveMedia(t *testing.T) {
	t.Parallel()

	st, _ := NewState(mocks.NewMockPlatform(), "test-boot")
	st.SetActiveMedia(models.NewActiveMedia("nes", "NES", "game.nes", "Game", "retroarch"))
	gen, ok := st.ActiveMediaReadyGeneration()
	require.True(t, ok)

	waitDone := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		waitDone <- st.WaitForActiveMediaReady(ctx, gen)
	}()

	st.SetActiveMedia(nil)

	select {
	case err := <-waitDone:
		require.Error(t, err)
		require.ErrorIs(t, err, ErrNoActiveMedia)
	case <-time.After(time.Second):
		t.Fatal("wait did not return after media stopped")
	}
	assert.False(t, st.ActiveMediaReady())
}
