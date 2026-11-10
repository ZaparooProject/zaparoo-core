//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package lakka

import (
	"context"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mediaHolder struct {
	media *models.ActiveMedia
	mu    syncutil.Mutex
}

func (h *mediaHolder) get() *models.ActiveMedia {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.media
}

func (h *mediaHolder) set(m *models.ActiveMedia) {
	h.mu.Lock()
	h.media = m
	h.mu.Unlock()
}

type lockedStatus struct {
	status string
	mu     syncutil.Mutex
}

func (s *lockedStatus) handle(msg string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status, msg == commandGetStatus
}

func (s *lockedStatus) set(status string) {
	s.mu.Lock()
	s.status = status
	s.mu.Unlock()
}

func newTestTracker(t *testing.T, status *lockedStatus) (*tracker, *frontend, *mediaHolder) {
	t.Helper()
	ra := newFakeRetroArch(t, status.handle)
	f, _, _ := testFrontend(t, ra.addr())
	holder := &mediaHolder{}
	return &tracker{frontend: f, activeMedia: holder.get, setActiveMedia: holder.set}, f, holder
}

func TestTrackerClearsMediaAfterGameCloses(t *testing.T) {
	t.Parallel()
	status := &lockedStatus{status: contentless}
	tr, f, holder := newTestTracker(t, status)
	f.launchedPath = testContent
	holder.set(&models.ActiveMedia{Path: testContent, Started: time.Unix(1, 0)})
	ctx := context.Background()

	// Still loading: never seen playing, so not cleared.
	tr.poll(ctx)
	assert.NotNil(t, holder.get())

	status.set(playingGame)
	tr.poll(ctx)
	assert.True(t, tr.confirmed)

	status.set(contentless)
	tr.poll(ctx)
	assert.Nil(t, holder.get())
	assert.Empty(t, f.currentLaunch())
}

func TestTrackerIgnoresMediaItDidNotLaunch(t *testing.T) {
	t.Parallel()
	status := &lockedStatus{status: playingGame}
	tr, f, holder := newTestTracker(t, status)
	f.launchedPath = testContent
	holder.set(&models.ActiveMedia{Path: testContent, Started: time.Unix(1, 0)})
	ctx := context.Background()
	tr.poll(ctx)

	// A different launch replaced it before RetroArch went idle.
	replacement := &models.ActiveMedia{Path: "/storage/roms/other.zip", Started: time.Unix(2, 0)}
	holder.set(replacement)
	status.set(contentless)
	tr.poll(ctx)
	assert.Same(t, replacement, holder.get())
}

func TestTrackerRunPollsUntilCancelled(t *testing.T) {
	t.Parallel()
	status := &lockedStatus{status: contentless}
	ra := newFakeRetroArch(t, status.handle)
	f, _, clock := testFrontend(t, ra.addr())
	holder := &mediaHolder{}
	tr := &tracker{frontend: f, activeMedia: holder.get, setActiveMedia: holder.set}
	f.launchedPath = testContent
	holder.set(&models.ActiveMedia{Path: testContent, Started: time.Unix(1, 0)})
	tr.confirmed = true
	tr.mediaKey = testContent + "\x00" + time.Unix(1, 0).String()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		tr.run(ctx)
	}()
	require.NoError(t, clock.BlockUntilContext(ctx, 1))
	clock.Advance(trackInterval)
	require.NoError(t, clock.BlockUntilContext(ctx, 1))
	assert.Nil(t, holder.get())
	cancel()
	<-done
}
