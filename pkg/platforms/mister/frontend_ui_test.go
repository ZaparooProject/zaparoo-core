//go:build linux && !android

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
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	uievents "github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/events"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func handoffPlatform(t *testing.T, status string, mediaRunning bool) (p *Platform, stops *int) {
	t.Helper()
	fs := afero.NewMemMapFs()
	// The Main that wrote the status files below is alive.
	require.NoError(t, fs.MkdirAll("/proc/4242", 0o755))
	if status != "" {
		require.NoError(t, afero.WriteFile(fs, frontendStatusPath, []byte(status), 0o600))
	}
	count := 0
	p = &Platform{fs: fs}
	noWait := time.Duration(0)
	p.uiReloadWait = &noWait
	p.uiFrontendRunning = func() bool { return true }
	p.uiReturnToMenu = func() error {
		count++
		return nil
	}
	p.activeMedia = func() *models.ActiveMedia {
		if mediaRunning {
			return &models.ActiveMedia{SystemID: "SNES", Path: "/games/a.sfc"}
		}
		return nil
	}
	return p, &count
}

// Main reports the frontend active as soon as it is enabled, before the
// frontend has started. At the menu, an event only goes to the frontend once
// it is running; until then the widget shows it.
func TestHandOffUIHoldsEventUntilFrontendRuns(t *testing.T) {
	t.Parallel()

	p, stops := handoffPlatform(t, `{"active":true,"pid":4242}`, false)
	p.uiFrontendRunning = func() bool { return false }
	handled, minOpen := p.HandOffUI(t.Context(), &models.UIEvent{ID: "e", Kind: models.UIEventKindPicker})
	assert.True(t, handled, "the widget never gets an event the starting frontend will draw")
	assert.Equal(t, frontendColdStartHold, minOpen, "the event stays open while the frontend starts")
	assert.Zero(t, *stops, "nothing is running, so nothing is stopped")
}

func TestHandOffUILeavesEventToWidgetWhenFrontendInactive(t *testing.T) {
	t.Parallel()

	p, stops := handoffPlatform(t, `{"active":false,"pid":4242}`, false)
	p.uiFrontendRunning = func() bool { return false }
	handled, minOpen := p.HandOffUI(t.Context(), &models.UIEvent{ID: "e", Kind: models.UIEventKindPicker})
	assert.False(t, handled)
	assert.Zero(t, minOpen)
	assert.Zero(t, *stops)
}

func TestPlatformImplementsHandoffRenderer(t *testing.T) {
	t.Parallel()
	var _ uievents.HandoffRenderer = &Platform{}
}

func TestHandOffUI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		status       string
		mediaRunning bool
		handled      bool
		minOpen      time.Duration
		stops        int
	}{
		{name: "status file missing", mediaRunning: true},
		{name: "frontend inactive", status: `{"active":false,"pid":4242}`, mediaRunning: true},
		{name: "status file unparseable", status: `{"active":`, mediaRunning: true},
		{
			name: "trailing data after the status", status: `{"active":true,"pid":4242} {"active":false}`,
			mediaRunning: true,
		},
		{name: "writer no longer running", status: `{"active":true,"pid":999}`, mediaRunning: true},
		{name: "writer not named", status: `{"active":true}`, mediaRunning: true},
		{name: "active without media", status: `{"active":true,"pid":4242}`, handled: true},
		{
			name: "active over running media", status: `{"active":true,"pid":4242}`, mediaRunning: true,
			handled: true, minOpen: 60 * time.Second, stops: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p, stops := handoffPlatform(t, tt.status, tt.mediaRunning)
			handled, minOpen := p.HandOffUI(t.Context(), &models.UIEvent{ID: "e", Kind: models.UIEventKindPicker})
			assert.Equal(t, tt.handled, handled)
			assert.Equal(t, tt.minOpen, minOpen)
			assert.Equal(t, tt.stops, *stops)
		})
	}
}

// A menu return that fails leaves no frontend to draw the event, so the
// widget gets to try it instead.
func TestHandOffUIFallsBackToWidgetWhenMenuReturnFails(t *testing.T) {
	t.Parallel()

	p, _ := handoffPlatform(t, `{"active":true,"pid":4242}`, true)
	p.uiReturnToMenu = func() error { return errors.New("menu core failed to load") }
	handled, minOpen := p.HandOffUI(t.Context(), &models.UIEvent{ID: "e", Kind: models.UIEventKindPicker})
	assert.False(t, handled)
	assert.Zero(t, minOpen)
}

// A game's exit raises its countdown while Main is between processes: the
// status file still names the old one. The hand-off waits for Main to name
// the new one instead of giving the event to the widget.
func TestHandOffUIWaitsForMainToReload(t *testing.T) {
	t.Parallel()

	p, _ := handoffPlatform(t, `{"active":true,"pid":999}`, false)
	p.uiFrontendRunning = func() bool { return false }
	wait := 5 * time.Second
	p.uiReloadWait = &wait
	go func() {
		time.Sleep(3 * frontendReloadPoll)
		_ = afero.WriteFile(p.fs, frontendStatusPath, []byte(`{"active":true,"pid":4242}`), 0o600)
	}()
	start := time.Now()
	handled, minOpen := p.HandOffUI(t.Context(), &models.UIEvent{ID: "e", Kind: models.UIEventKindPicker})
	assert.True(t, handled)
	assert.Equal(t, frontendColdStartHold, minOpen)
	assert.Less(t, time.Since(start), wait, "it stops waiting once Main is back")
}

// A status file left by a fork that a stock Main replaced is never
// rewritten; the wait ends and the widget shows the event.
func TestHandOffUIGivesUpOnAnOrphanedStatus(t *testing.T) {
	t.Parallel()

	p, _ := handoffPlatform(t, `{"active":true,"pid":999}`, false)
	wait := 2 * frontendReloadPoll
	p.uiReloadWait = &wait
	handled, minOpen := p.HandOffUI(t.Context(), &models.UIEvent{ID: "e", Kind: models.UIEventKindPicker})
	assert.False(t, handled)
	assert.Zero(t, minOpen)
}

func TestFrontendActiveRejectsOversizedStatus(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/proc/4242", 0o755))
	status := `{"active":true,"pid":4242}` + strings.Repeat(" ", frontendStatusMaxBytes)
	require.NoError(t, afero.WriteFile(fs, frontendStatusPath, []byte(status), 0o600))
	assert.False(t, frontendActive(fs))
}

// TestHandOffUIKeepsPickerOpenForFrontend drives the UI event service with the
// MiSTer platform as its renderer: a picker opened over a running game
// returns to the menu once and stays open for the frontend's cold start.
func TestHandOffUIKeepsPickerOpenForFrontend(t *testing.T) {
	t.Parallel()

	p, stops := handoffPlatform(t, `{"active":true,"pid":4242}`, true)
	states := make(chan models.UIStateResponse, 4)
	service := uievents.New(nil, p, func(state models.UIStateResponse) { states <- state })
	start := time.Now()
	_, err := service.Open(t.Context(), &uievents.Request{
		Kind:    models.UIEventKindPicker,
		Choices: []uievents.Choice{{Label: "One"}},
		Timeout: 30 * time.Second,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, *stops)
	state := service.State()
	require.Len(t, state.Events, 1)
	require.NotNil(t, state.Events[0].ExpiresAt)
	assert.False(t, state.Events[0].ExpiresAt.Before(start.Add(60*time.Second)))
}
