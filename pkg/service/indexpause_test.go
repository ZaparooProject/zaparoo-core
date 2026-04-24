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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func alwaysActive() bool { return true }
func neverActive() bool  { return false }

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

func TestHandleIndexPauseNotifications_PausesOnStarted(t *testing.T) {
	ch := make(chan models.Notification, 1)
	ns := make(chan models.Notification, 10)
	pauser := syncutil.NewPauser()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go handleIndexPauseNotifications(ctx, ch, ns, pauser, false, alwaysActive)

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

func TestHandleIndexPauseNotifications_ResumesOnStopped(t *testing.T) {
	ch := make(chan models.Notification, 2)
	ns := make(chan models.Notification, 10)
	pauser := syncutil.NewPauser()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go handleIndexPauseNotifications(ctx, ch, ns, pauser, false, alwaysActive)

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

	go handleIndexPauseNotifications(ctx, ch, ns, pauser, true, alwaysActive)

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

	go handleIndexPauseNotifications(ctx, ch, ns, pauser, false, alwaysActive)

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
			context.Background(), ch, ns, pauser, false, alwaysActive,
		)
	}()

	close(ch)

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("handleIndexPauseNotifications did not exit on channel close")
	}
}

func TestHandleIndexPauseNotifications_NoNotificationWhenNotIndexing(t *testing.T) {
	ch := make(chan models.Notification, 1)
	ns := make(chan models.Notification, 10)
	pauser := syncutil.NewPauser()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go handleIndexPauseNotifications(ctx, ch, ns, pauser, false, neverActive)

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
