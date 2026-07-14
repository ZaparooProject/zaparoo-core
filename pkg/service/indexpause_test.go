/*
Zaparoo Core
Copyright (c) 2026 The Zaparoo Project Contributors.
SPDX-License-Identifier: GPL-3.0-or-later

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func alwaysActive() bool { return true }
func neverActive() bool  { return false }

func pauseMode() config.MediaPausePolicy {
	return config.MediaPausePolicy{Mode: config.IndexDuringMediaPause, Level: syncutil.ThrottleLight}
}

func throttleMode() config.MediaPausePolicy {
	return config.MediaPausePolicy{Mode: config.IndexDuringMediaThrottle, Level: syncutil.ThrottleLight}
}

func mediaLifecycleNotification(t *testing.T, method, slot string) models.Notification {
	t.Helper()
	params, err := json.Marshal(struct {
		Slot string `json:"slot"`
	}{Slot: slot})
	require.NoError(t, err)
	return models.Notification{Method: method, Params: params}
}

// drainNotification reads a single notification from ns, failing if none
// arrives within the timeout.
func drainNotification(t *testing.T, ns <-chan models.Notification) models.Notification {
	t.Helper()
	select {
	case n := <-ns:
		return n
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected notification but none received")
		return models.Notification{}
	}
}

func drainState(t *testing.T, st *state.State, ns <-chan models.Notification) {
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

func TestActiveSystemID_NoActiveMedia(t *testing.T) {
	t.Parallel()

	st, ns := state.NewState(mocks.NewMockPlatform(), "test-boot-uuid")
	drainState(t, st, ns)

	assert.Empty(t, activeSystemID(st))
}

func TestActiveSystemID_ReturnsActiveMediaSystemID(t *testing.T) {
	t.Parallel()

	st, ns := state.NewState(mocks.NewMockPlatform(), "test-boot-uuid")
	drainState(t, st, ns)

	st.SetActiveMedia(models.NewActiveMedia(
		systemdefs.SystemSaturn, systemdefs.SystemSaturn, "game.chd", "Game", "Saturn",
	))

	assert.Equal(t, systemdefs.SystemSaturn, activeSystemID(st))
}

func TestActiveMediaPausesMediaWork_BackgroundSlot(t *testing.T) {
	media := models.NewActiveMedia("Audio", "Audio", "song.mp3", "Song", "native-audio")
	media.Slot = "background"

	assert.False(t, activeMediaPausesMediaWork(media))
}

func TestActiveMediaPausesMediaWork_PrimarySlot(t *testing.T) {
	media := models.NewActiveMedia("NES", "NES", "game.nes", "Game", "NES")

	assert.True(t, activeMediaPausesMediaWork(media))
}

func TestHandleIndexPauseNotifications_PausesOnStarted(t *testing.T) {
	ch := make(chan models.Notification, 1)
	ns := make(chan models.Notification, 10)
	pauser := syncutil.NewPauser()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go handleIndexPauseNotifications(ctx, ch, ns, pauser, false, alwaysActive, pauseMode)

	require.False(t, pauser.IsPaused())

	ch <- models.Notification{Method: models.NotificationStarted}
	require.Eventually(t, pauser.IsPaused,
		500*time.Millisecond, 10*time.Millisecond)

	notif := drainNotification(t, ns)
	assert.Equal(t, models.NotificationMediaIndexing, notif.Method)
	var resp models.IndexingStatusResponse
	require.NoError(t, json.Unmarshal(notif.Params, &resp))
	assert.True(t, resp.Paused)
}

func TestHandleIndexPauseNotifications_PausesOnInvalidSlot(t *testing.T) {
	ch := make(chan models.Notification, 1)
	ns := make(chan models.Notification, 10)
	pauser := syncutil.NewPauser()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go handleIndexPauseNotifications(ctx, ch, ns, pauser, false, alwaysActive, pauseMode)

	ch <- mediaLifecycleNotification(t, models.NotificationStarted, "tertiary")

	require.Eventually(t, pauser.IsPaused,
		500*time.Millisecond, 10*time.Millisecond)
	notif := drainNotification(t, ns)
	assert.Equal(t, models.NotificationMediaIndexing, notif.Method)
}

func TestHandleIndexPauseNotifications_PausesOnMalformedParams(t *testing.T) {
	ch := make(chan models.Notification, 1)
	ns := make(chan models.Notification, 10)
	pauser := syncutil.NewPauser()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go handleIndexPauseNotifications(ctx, ch, ns, pauser, false, alwaysActive, pauseMode)

	ch <- models.Notification{Method: models.NotificationStarted, Params: []byte("{")}

	require.Eventually(t, pauser.IsPaused,
		500*time.Millisecond, 10*time.Millisecond)
	notif := drainNotification(t, ns)
	assert.Equal(t, models.NotificationMediaIndexing, notif.Method)
}

func TestHandleIndexPauseNotifications_IgnoresStartedWhenNoPrimaryActive(t *testing.T) {
	ch := make(chan models.Notification, 1)
	ns := make(chan models.Notification, 10)
	pauser := syncutil.NewPauser()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go handleIndexPauseNotifications(ctx, ch, ns, pauser, false, alwaysActive, pauseMode, neverActive)

	ch <- models.Notification{Method: models.NotificationStarted}

	assert.Never(t, pauser.IsPaused, 100*time.Millisecond, 10*time.Millisecond)
	select {
	case notif := <-ns:
		t.Fatalf("unexpected notification sent without primary media: %+v", notif)
	default:
	}
}

func TestHandleIndexPauseNotifications_PausesStartedWhenPrimaryActive(t *testing.T) {
	ch := make(chan models.Notification, 1)
	ns := make(chan models.Notification, 10)
	pauser := syncutil.NewPauser()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go handleIndexPauseNotifications(ctx, ch, ns, pauser, false, alwaysActive, pauseMode, alwaysActive)

	ch <- mediaLifecycleNotification(t, models.NotificationStarted, "background")

	require.Eventually(t, pauser.IsPaused,
		500*time.Millisecond, 10*time.Millisecond)
	notif := drainNotification(t, ns)
	assert.Equal(t, models.NotificationMediaIndexing, notif.Method)
}

func TestHandleIndexPauseNotifications_IgnoresStoppedWhenAlreadyUnpaused(t *testing.T) {
	ch := make(chan models.Notification, 1)
	ns := make(chan models.Notification, 10)
	pauser := syncutil.NewPauser()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go handleIndexPauseNotifications(ctx, ch, ns, pauser, false, alwaysActive, pauseMode, neverActive)

	ch <- mediaLifecycleNotification(t, models.NotificationStopped, "background")

	assert.Never(t, pauser.IsPaused, 100*time.Millisecond, 10*time.Millisecond)
	select {
	case notif := <-ns:
		t.Fatalf("unexpected notification sent for no-op stopped event: %+v", notif)
	default:
	}
}

func TestHandleScrapePauseNotifications_IgnoresStoppedWhenAlreadyUnpaused(t *testing.T) {
	ch := make(chan models.Notification, 1)
	ns := make(chan models.Notification, 10)
	pauser := syncutil.NewPauser()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go handleScrapePauseNotifications(ctx, ch, ns, pauser, false, alwaysActive, pauseMode, neverActive)

	ch <- mediaLifecycleNotification(t, models.NotificationStopped, "background")

	assert.Never(t, pauser.IsPaused, 100*time.Millisecond, 10*time.Millisecond)
	select {
	case notif := <-ns:
		t.Fatalf("unexpected notification sent for no-op stopped event: %+v", notif)
	default:
	}
}

func TestHandleIndexPauseNotifications_DoesNotResumeStoppedWhilePrimaryActive(t *testing.T) {
	ch := make(chan models.Notification, 1)
	ns := make(chan models.Notification, 10)
	pauser := syncutil.NewPauser()
	pauser.Pause()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go handleIndexPauseNotifications(ctx, ch, ns, pauser, false, alwaysActive, pauseMode, alwaysActive)

	ch <- mediaLifecycleNotification(t, models.NotificationStopped, "background")

	assert.Never(t, func() bool { return !pauser.IsPaused() }, 100*time.Millisecond, 10*time.Millisecond)
	select {
	case notif := <-ns:
		t.Fatalf("unexpected notification sent while primary remains active: %+v", notif)
	default:
	}
}

func TestHandleIndexPauseNotifications_IgnoresBackgroundStarted(t *testing.T) {
	ch := make(chan models.Notification, 1)
	ns := make(chan models.Notification, 10)
	pauser := syncutil.NewPauser()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go handleIndexPauseNotifications(ctx, ch, ns, pauser, false, alwaysActive, pauseMode)

	ch <- mediaLifecycleNotification(t, models.NotificationStarted, "background")

	assert.Never(t, pauser.IsPaused, 100*time.Millisecond, 10*time.Millisecond)
	select {
	case notif := <-ns:
		t.Fatalf("unexpected notification sent for background media: %+v", notif)
	default:
	}
}

func TestHandleIndexPauseNotifications_IgnoresBackgroundStoppedWhilePaused(t *testing.T) {
	ch := make(chan models.Notification, 2)
	ns := make(chan models.Notification, 10)
	pauser := syncutil.NewPauser()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go handleIndexPauseNotifications(ctx, ch, ns, pauser, false, alwaysActive, pauseMode)

	ch <- models.Notification{Method: models.NotificationStarted}
	require.Eventually(t, pauser.IsPaused,
		500*time.Millisecond, 10*time.Millisecond)
	drainNotification(t, ns)

	ch <- mediaLifecycleNotification(t, models.NotificationStopped, "background")

	assert.Never(t, func() bool { return !pauser.IsPaused() }, 100*time.Millisecond, 10*time.Millisecond)
	select {
	case notif := <-ns:
		t.Fatalf("unexpected notification sent for background media: %+v", notif)
	default:
	}
}

func TestHandleIndexPauseNotifications_ResumesOnStopped(t *testing.T) {
	ch := make(chan models.Notification, 2)
	ns := make(chan models.Notification, 10)
	pauser := syncutil.NewPauser()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go handleIndexPauseNotifications(ctx, ch, ns, pauser, false, alwaysActive, pauseMode)

	ch <- models.Notification{Method: models.NotificationStarted}
	require.Eventually(t, pauser.IsPaused,
		500*time.Millisecond, 10*time.Millisecond)
	drainNotification(t, ns) // consume pause notification

	ch <- models.Notification{Method: models.NotificationStopped}
	require.Eventually(t, func() bool { return !pauser.IsPaused() },
		500*time.Millisecond, 10*time.Millisecond)

	notif := drainNotification(t, ns)
	assert.Equal(t, models.NotificationMediaIndexing, notif.Method)
	var resp models.IndexingStatusResponse
	require.NoError(t, json.Unmarshal(notif.Params, &resp))
	assert.False(t, resp.Paused)
}

func TestHandleIndexPauseNotifications_PausesWhenGameAlreadyActive(t *testing.T) {
	ch := make(chan models.Notification)
	ns := make(chan models.Notification, 10)
	pauser := syncutil.NewPauser()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go handleIndexPauseNotifications(ctx, ch, ns, pauser, true, alwaysActive, pauseMode)

	require.Eventually(t, pauser.IsPaused,
		500*time.Millisecond, 10*time.Millisecond)

	notif := drainNotification(t, ns)
	assert.Equal(t, models.NotificationMediaIndexing, notif.Method)
	var resp models.IndexingStatusResponse
	require.NoError(t, json.Unmarshal(notif.Params, &resp))
	assert.True(t, resp.Paused)
}

func TestHandleIndexPauseNotifications_ResumesOnContextCancel(t *testing.T) {
	ch := make(chan models.Notification, 1)
	ns := make(chan models.Notification, 10)
	pauser := syncutil.NewPauser()

	ctx, cancel := context.WithCancel(context.Background())

	go handleIndexPauseNotifications(ctx, ch, ns, pauser, false, alwaysActive, pauseMode)

	ch <- models.Notification{Method: models.NotificationStarted}
	require.Eventually(t, pauser.IsPaused,
		500*time.Millisecond, 10*time.Millisecond)

	cancel()

	require.Eventually(t, func() bool { return !pauser.IsPaused() },
		500*time.Millisecond, 10*time.Millisecond)
}

func TestHandleIndexPauseNotifications_ExitsOnChannelClose(t *testing.T) {
	ch := make(chan models.Notification)
	ns := make(chan models.Notification, 10)
	pauser := syncutil.NewPauser()

	done := make(chan struct{})
	go func() {
		defer close(done)
		handleIndexPauseNotifications(
			context.Background(), ch, ns, pauser, false, alwaysActive, pauseMode,
		)
	}()

	close(ch)

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("handleIndexPauseNotifications did not exit on channel close")
	}
}

func TestHandleIndexPauseNotifications_ResumesWhenPausedAndChannelCloses(t *testing.T) {
	ch := make(chan models.Notification)
	ns := make(chan models.Notification, 10)
	pauser := syncutil.NewPauser()

	done := make(chan struct{})
	go func() {
		defer close(done)
		handleIndexPauseNotifications(
			context.Background(), ch, ns, pauser, true, alwaysActive, pauseMode,
		)
	}()

	require.Eventually(t, pauser.IsPaused,
		500*time.Millisecond, 10*time.Millisecond)
	drainNotification(t, ns)

	close(ch)

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("handleIndexPauseNotifications did not exit on channel close")
	}
	assert.False(t, pauser.IsPaused())
}

func TestHandleIndexPauseNotifications_NoNotificationWhenNotIndexing(t *testing.T) {
	ch := make(chan models.Notification, 1)
	ns := make(chan models.Notification, 10)
	pauser := syncutil.NewPauser()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go handleIndexPauseNotifications(ctx, ch, ns, pauser, false, neverActive, pauseMode)

	ch <- models.Notification{Method: models.NotificationStarted}
	require.Eventually(t, pauser.IsPaused,
		500*time.Millisecond, 10*time.Millisecond)

	// Pauser should be paused but no notification should have been sent
	select {
	case notif := <-ns:
		t.Fatalf("unexpected notification sent when not indexing: %+v", notif)
	case <-time.After(100 * time.Millisecond):
		// expected — no notification
	}
}

func TestHandleScrapePauseNotifications_IgnoresStartedWhenNoPrimaryActive(t *testing.T) {
	ch := make(chan models.Notification, 1)
	ns := make(chan models.Notification, 10)
	pauser := syncutil.NewPauser()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go handleScrapePauseNotifications(ctx, ch, ns, pauser, false, alwaysActive, pauseMode, neverActive)

	ch <- models.Notification{Method: models.NotificationStarted}

	assert.Never(t, pauser.IsPaused, 100*time.Millisecond, 10*time.Millisecond)
	select {
	case notif := <-ns:
		t.Fatalf("unexpected notification sent without primary media: %+v", notif)
	default:
	}
}

func TestHandleScrapePauseNotifications_IgnoresBackgroundStarted(t *testing.T) {
	ch := make(chan models.Notification, 1)
	ns := make(chan models.Notification, 10)
	pauser := syncutil.NewPauser()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go handleScrapePauseNotifications(ctx, ch, ns, pauser, false, alwaysActive, pauseMode)

	ch <- mediaLifecycleNotification(t, models.NotificationStarted, "background")

	assert.Never(t, pauser.IsPaused, 100*time.Millisecond, 10*time.Millisecond)
	select {
	case notif := <-ns:
		t.Fatalf("unexpected notification sent for background media: %+v", notif)
	default:
	}
}

func TestHandleScrapePauseNotifications_PausesOnStarted(t *testing.T) {
	ch := make(chan models.Notification, 1)
	ns := make(chan models.Notification, 10)
	pauser := syncutil.NewPauser()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go handleScrapePauseNotifications(ctx, ch, ns, pauser, false, alwaysActive, pauseMode)

	ch <- models.Notification{Method: models.NotificationStarted}
	require.Eventually(t, pauser.IsPaused,
		500*time.Millisecond, 10*time.Millisecond)

	notif := drainNotification(t, ns)
	assert.Equal(t, models.NotificationMediaScraping, notif.Method)
	var resp models.ScrapingStatusResponse
	require.NoError(t, json.Unmarshal(notif.Params, &resp))
	assert.True(t, resp.Scraping)
	assert.True(t, resp.Paused)
}

func TestHandleScrapePauseNotifications_ResumesOnStopped(t *testing.T) {
	ch := make(chan models.Notification, 2)
	ns := make(chan models.Notification, 10)
	pauser := syncutil.NewPauser()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go handleScrapePauseNotifications(ctx, ch, ns, pauser, false, alwaysActive, pauseMode)

	ch <- models.Notification{Method: models.NotificationStarted}
	require.Eventually(t, pauser.IsPaused,
		500*time.Millisecond, 10*time.Millisecond)
	drainNotification(t, ns)

	ch <- models.Notification{Method: models.NotificationStopped}
	require.Eventually(t, func() bool { return !pauser.IsPaused() },
		500*time.Millisecond, 10*time.Millisecond)

	notif := drainNotification(t, ns)
	assert.Equal(t, models.NotificationMediaScraping, notif.Method)
	var resp models.ScrapingStatusResponse
	require.NoError(t, json.Unmarshal(notif.Params, &resp))
	assert.True(t, resp.Scraping)
	assert.False(t, resp.Paused)
}

func TestHandleIndexPauseNotifications_ThrottlesOnStartedInThrottleMode(t *testing.T) {
	ch := make(chan models.Notification, 1)
	ns := make(chan models.Notification, 10)
	pauser := syncutil.NewPauser()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go handleIndexPauseNotifications(ctx, ch, ns, pauser, false, alwaysActive, throttleMode)

	ch <- models.Notification{Method: models.NotificationStarted}
	require.Eventually(t, pauser.IsThrottled,
		500*time.Millisecond, 10*time.Millisecond)
	assert.False(t, pauser.IsPaused())

	notif := drainNotification(t, ns)
	assert.Equal(t, models.NotificationMediaIndexing, notif.Method)
	var resp models.IndexingStatusResponse
	require.NoError(t, json.Unmarshal(notif.Params, &resp))
	assert.False(t, resp.Paused)
	assert.True(t, resp.Throttled)
}

func TestHandleIndexPauseNotifications_ResumesFromThrottleOnStopped(t *testing.T) {
	ch := make(chan models.Notification, 2)
	ns := make(chan models.Notification, 10)
	pauser := syncutil.NewPauser()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go handleIndexPauseNotifications(ctx, ch, ns, pauser, false, alwaysActive, throttleMode)

	ch <- models.Notification{Method: models.NotificationStarted}
	require.Eventually(t, pauser.IsThrottled,
		500*time.Millisecond, 10*time.Millisecond)
	drainNotification(t, ns)

	ch <- models.Notification{Method: models.NotificationStopped}
	require.Eventually(t, func() bool { return !pauser.IsThrottled() && !pauser.IsPaused() },
		500*time.Millisecond, 10*time.Millisecond)

	notif := drainNotification(t, ns)
	var resp models.IndexingStatusResponse
	require.NoError(t, json.Unmarshal(notif.Params, &resp))
	assert.False(t, resp.Paused)
	assert.False(t, resp.Throttled)
}

func TestHandleIndexPauseNotifications_NilRestrictModeDefaultsToThrottle(t *testing.T) {
	ch := make(chan models.Notification, 1)
	ns := make(chan models.Notification, 10)
	pauser := syncutil.NewPauser()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go handleIndexPauseNotifications(ctx, ch, ns, pauser, false, alwaysActive, nil)

	ch <- models.Notification{Method: models.NotificationStarted}
	require.Eventually(t, pauser.IsThrottled,
		500*time.Millisecond, 10*time.Millisecond)
	assert.False(t, pauser.IsPaused())
	drainNotification(t, ns)
}

func TestHandleScrapePauseNotifications_ThrottlesOnStartedInThrottleMode(t *testing.T) {
	ch := make(chan models.Notification, 1)
	ns := make(chan models.Notification, 10)
	pauser := syncutil.NewPauser()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go handleScrapePauseNotifications(ctx, ch, ns, pauser, false, alwaysActive, throttleMode)

	ch <- models.Notification{Method: models.NotificationStarted}
	require.Eventually(t, pauser.IsThrottled,
		500*time.Millisecond, 10*time.Millisecond)

	notif := drainNotification(t, ns)
	assert.Equal(t, models.NotificationMediaScraping, notif.Method)
	var resp models.ScrapingStatusResponse
	require.NoError(t, json.Unmarshal(notif.Params, &resp))
	assert.True(t, resp.Scraping)
	assert.False(t, resp.Paused)
	assert.True(t, resp.Throttled)
}
