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
	backupcoordinator "github.com/ZaparooProject/zaparoo-core/v2/pkg/service/backup/coordinator"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// drainState drains the notification channel in t.Cleanup so goroutines don't leak.
func drainState(t *testing.T, st *State, ns <-chan models.Notification) {
	t.Helper()
	t.Cleanup(func() {
		st.StopService()
		for {
			select {
			case <-ns:
			default:
				return
			}
		}
	})
}

func TestMediaRestoreGateMutualExclusion(t *testing.T) {
	t.Parallel()
	st, _ := NewState(nil, "test-boot")
	defer st.StopService()

	releaseLaunch, err := st.AcquireMediaLaunch()
	require.NoError(t, err)
	restoreErr := make(chan error, 1)
	go func() {
		finish, beginErr := st.BeginRestoreGate()
		if finish != nil {
			finish(false)
		}
		restoreErr <- beginErr
	}()
	require.ErrorIs(t, <-restoreErr, ErrMediaLaunchInProgress)
	releaseLaunch()

	finishRestore, err := st.BeginRestoreGate()
	require.NoError(t, err)
	launchErr := make(chan error, 1)
	go func() {
		release, acquireErr := st.AcquireMediaLaunch()
		if release != nil {
			release()
		}
		launchErr <- acquireErr
	}()
	require.ErrorIs(t, <-launchErr, ErrRestoreInProgress)
	finishRestore(false)

	releaseLaunch, err = st.AcquireMediaLaunch()
	require.NoError(t, err)
	releaseLaunch()
}

func TestExternalActiveMediaCancelsRestoreBeforeUpdatingState(t *testing.T) {
	t.Parallel()
	st, _ := NewState(nil, "test-boot")
	defer st.StopService()
	lease, err := st.BackupCoordinator().Begin(
		context.Background(), backupcoordinator.OperationLocalRestore, backupcoordinator.OperationWrite,
	)
	require.NoError(t, err)
	defer lease.Release()
	finishRestore, err := st.BeginRestoreGate()
	require.NoError(t, err)
	updated := make(chan struct{})
	go func() {
		st.SetActiveMedia(&models.ActiveMedia{SystemID: "SNES", Path: "game.sfc", Name: "Game"})
		close(updated)
	}()

	select {
	case <-lease.Context().Done():
	case <-time.After(time.Second):
		t.Fatal("external active media did not cancel restore")
	}
	select {
	case <-updated:
		t.Fatal("active media changed before restore gate released")
	case <-time.After(50 * time.Millisecond):
	}
	finishRestore(false)
	select {
	case <-updated:
	case <-time.After(time.Second):
		t.Fatal("active media update did not resume after rollback gate released")
	}
	assert.NotNil(t, st.ActiveMedia())
}

func TestBlockingRestoreAccessWaitsForRollback(t *testing.T) {
	t.Parallel()
	st, _ := NewState(nil, "test-boot")
	defer st.StopService()
	finishRestore, err := st.BeginRestoreGate()
	require.NoError(t, err)
	acquired := make(chan error, 1)
	go func() {
		release, accessErr := st.AcquireRestoreAccess()
		if release != nil {
			release()
		}
		acquired <- accessErr
	}()
	select {
	case <-acquired:
		t.Fatal("restore-sensitive operation did not wait for restore")
	case <-time.After(50 * time.Millisecond):
	}
	finishRestore(false)
	require.NoError(t, <-acquired)
}

func TestSuccessfulRestoreGateBlocksLaunchUntilRestart(t *testing.T) {
	t.Parallel()
	st, _ := NewState(nil, "test-boot")
	defer st.StopService()

	finishRestore, err := st.BeginRestoreGate()
	require.NoError(t, err)
	finishRestore(true)

	_, err = st.AcquireMediaLaunch()
	require.ErrorIs(t, err, ErrRestoreRestartRequired)
	_, err = st.BeginRestoreGate()
	require.ErrorIs(t, err, ErrRestoreRestartRequired)
}

func TestSetRunZapScript(t *testing.T) {
	t.Parallel()
	st, _ := NewState(nil, "boot")
	defer st.StopService()

	assert.True(t, st.RunZapScriptEnabled(), "default should be enabled")

	st.SetRunZapScript(false)
	assert.False(t, st.RunZapScriptEnabled())

	st.SetRunZapScript(true)
	assert.True(t, st.RunZapScriptEnabled())
}

func TestActivePlaylist(t *testing.T) {
	t.Parallel()
	st, _ := NewState(nil, "boot")
	defer st.StopService()

	assert.Nil(t, st.GetActivePlaylist())

	pls := &playlists.Playlist{ID: "test"}
	st.SetActivePlaylist(pls)
	assert.Equal(t, pls, st.GetActivePlaylist())

	st.SetActivePlaylist(nil)
	assert.Nil(t, st.GetActivePlaylist())
}

func TestBackgroundPlaylist(t *testing.T) {
	t.Parallel()
	st, _ := NewState(nil, "boot")
	defer st.StopService()

	assert.Nil(t, st.GetBackgroundPlaylist())

	pls := &playlists.Playlist{ID: "bg"}
	st.SetBackgroundPlaylist(pls)
	assert.Equal(t, pls, st.GetBackgroundPlaylist())

	st.SetBackgroundPlaylist(nil)
	assert.Nil(t, st.GetBackgroundPlaylist())
}

func TestSetBackgroundMedia_Changed(t *testing.T) {
	t.Parallel()

	st, ns := NewState(nil, "boot")
	drainState(t, st, ns)

	first := models.NewActiveMedia("Audio", "Audio", "song1.mp3", "Song 1", "native-audio")
	st.SetBackgroundMedia(first)
	select {
	case n := <-ns:
		assert.Equal(t, models.NotificationStarted, n.Method)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for MediaStarted")
	}

	second := models.NewActiveMedia("Audio", "Audio", "song2.mp3", "Song 2", "native-audio")
	st.SetBackgroundMedia(second)

	select {
	case n := <-ns:
		assert.Equal(t, models.NotificationStopped, n.Method)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for MediaStopped on change")
	}
	select {
	case n := <-ns:
		assert.Equal(t, models.NotificationStarted, n.Method)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for MediaStarted on change")
	}

	assert.Equal(t, second, st.BackgroundMedia())
}

func TestSetBackgroundMedia_SameMediaIsNoOp(t *testing.T) {
	t.Parallel()

	st, ns := NewState(nil, "boot")
	drainState(t, st, ns)

	media := models.NewActiveMedia("Audio", "Audio", "song.mp3", "Song", "native-audio")
	st.SetBackgroundMedia(media)
	select {
	case <-ns:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for initial MediaStarted")
	}

	// Setting the identical media again must not produce notifications.
	st.SetBackgroundMedia(media)
	select {
	case n := <-ns:
		t.Fatalf("unexpected notification on same-media set: %s", n.Method)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestActiveMediaReady_LifecycleWithMarkAndWait(t *testing.T) {
	t.Parallel()

	st, ns := NewState(nil, "boot")
	drainState(t, st, ns)

	// No active media → not ready, no generation.
	assert.False(t, st.ActiveMediaReady())
	_, ok := st.ActiveMediaReadyGeneration()
	assert.False(t, ok)

	// Set media — ready is false until marked.
	media := models.NewActiveMedia("NES", "NES", "game.nes", "Game", "retroarch")
	st.SetActiveMedia(media)
	select {
	case <-ns:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for MediaStarted")
	}

	assert.False(t, st.ActiveMediaReady())
	gen, ok := st.ActiveMediaReadyGeneration()
	require.True(t, ok)

	// After marking, both predicates flip and WaitForActiveMediaReady returns immediately.
	st.MarkActiveMediaReady(gen)
	assert.True(t, st.ActiveMediaReady())

	require.NoError(t, st.WaitForActiveMediaReady(context.Background(), gen))
}

func TestWaitForActiveMediaReady_NoMedia(t *testing.T) {
	t.Parallel()

	st, _ := NewState(nil, "boot")
	defer st.StopService()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := st.WaitForActiveMediaReady(ctx, 1)
	require.ErrorIs(t, err, ErrNoActiveMedia)
}

func TestWaitForActiveMediaReady_WrongGeneration(t *testing.T) {
	t.Parallel()

	st, ns := NewState(nil, "boot")
	drainState(t, st, ns)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	media := models.NewActiveMedia("NES", "NES", "game.nes", "Game", "retroarch")
	st.SetActiveMedia(media)
	select {
	case <-ns:
	case <-ctx.Done():
		t.Fatal("timeout waiting for MediaStarted")
	}

	gen, _ := st.ActiveMediaReadyGeneration()

	// Change active media — gen is invalidated.
	media2 := models.NewActiveMedia("SNES", "SNES", "game.sfc", "Game2", "retroarch")
	st.SetActiveMedia(media2)
	select {
	case <-ns:
	case <-ctx.Done():
		t.Fatal("timeout waiting for MediaStopped")
	}
	select {
	case <-ns:
	case <-ctx.Done():
		t.Fatal("timeout waiting for MediaStarted")
	}

	err := st.WaitForActiveMediaReady(ctx, gen)
	require.ErrorIs(t, err, ErrActiveMediaChanged)
}

func TestWaitForActiveMediaReady_ContextCancelled(t *testing.T) {
	t.Parallel()

	st, ns := NewState(nil, "boot")
	drainState(t, st, ns)

	media := models.NewActiveMedia("NES", "NES", "game.nes", "Game", "retroarch")
	st.SetActiveMedia(media)
	select {
	case <-ns:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for MediaStarted")
	}

	gen, _ := st.ActiveMediaReadyGeneration()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := st.WaitForActiveMediaReady(ctx, gen)
	require.ErrorIs(t, err, context.Canceled)
}
