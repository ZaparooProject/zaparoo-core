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

package events

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// handoffRenderer leaves events to API clients when handled is set.
type handoffRenderer struct {
	testRenderer
	minOpen time.Duration
	offered atomic.Int32
	handled bool
}

func (r *handoffRenderer) HandOffUI(_ context.Context, _ *models.UIEvent) (bool, time.Duration) {
	r.offered.Add(1)
	return r.handled, r.minOpen
}

func TestServiceHandOffSkipsHostRenderer(t *testing.T) {
	t.Parallel()

	renderer := &handoffRenderer{handled: true}
	clock := clockwork.NewFakeClock()
	service, published := newTestService(clock, renderer)
	handle, err := service.Open(t.Context(), &Request{
		Kind:    models.UIEventKindPicker,
		Choices: []Choice{{Label: "One", Value: "one"}},
		Timeout: 30 * time.Second,
	})
	require.NoError(t, err)
	opened := receiveState(t, published)
	require.Len(t, opened.Events, 1)
	assert.Equal(t, clock.Now().Add(30*time.Second), *opened.Events[0].ExpiresAt)
	assert.Equal(t, int32(1), renderer.offered.Load())

	title := "Changed"
	require.NoError(t, handle.Update(Update{Title: &title}))
	_ = receiveState(t, published)
	assert.Zero(t, renderer.updated.Load(), "a handed-off event is never updated on the host")

	require.NoError(t, service.Respond(handle.ID, models.UIResponseActionSelect, opened.Events[0].Choices[0].ID))
	result := receiveResult(t, handle.Results)
	assert.Equal(t, "one", result.Value)
	assert.Zero(t, renderer.closed.Load(), "nothing was presented, so nothing is closed")
	select {
	case state := <-published:
		assert.Empty(t, state.Events)
	default:
		t.Fatal("resolution was not published")
	}
}

func TestServiceHandOffExtendsExpiry(t *testing.T) {
	t.Parallel()

	renderer := &handoffRenderer{handled: true, minOpen: time.Minute}
	clock := clockwork.NewFakeClock()
	service, published := newTestService(clock, renderer)
	handle, err := service.Open(t.Context(), &Request{
		Kind:    models.UIEventKindPicker,
		Choices: []Choice{{Label: "One"}},
		Timeout: 30 * time.Second,
	})
	require.NoError(t, err)
	_ = receiveState(t, published)
	extended := receiveState(t, published)
	require.Len(t, extended.Events, 1)
	assert.Equal(t, clock.Now().Add(time.Minute), *extended.Events[0].ExpiresAt)
	require.NoError(t, clock.BlockUntilContext(t.Context(), 1))

	clock.Advance(30 * time.Second)
	select {
	case result := <-handle.Results:
		t.Fatalf("event resolved at its own timeout: %+v", result)
	default:
	}
	clock.Advance(30 * time.Second)
	result := receiveResult(t, handle.Results)
	assert.Equal(t, models.UIOutcomeTimedOut, result.Resolution.Outcome)
}

func TestServiceHandOffKeepsLongerTimeout(t *testing.T) {
	t.Parallel()

	renderer := &handoffRenderer{handled: true, minOpen: time.Minute}
	clock := clockwork.NewFakeClock()
	service, published := newTestService(clock, renderer)
	_, err := service.Open(t.Context(), &Request{
		Kind:    models.UIEventKindNotice,
		Timeout: 2 * time.Minute,
	})
	require.NoError(t, err)
	opened := receiveState(t, published)
	assert.Equal(t, clock.Now().Add(2*time.Minute), *opened.Events[0].ExpiresAt)
	select {
	case state := <-published:
		t.Fatalf("unexpected state %+v", state)
	default:
	}
}

func TestServiceHandOffLeavesNoTimeoutOpen(t *testing.T) {
	t.Parallel()

	renderer := &handoffRenderer{handled: true, minOpen: time.Minute}
	clock := clockwork.NewFakeClock()
	service, published := newTestService(clock, renderer)
	_, err := service.Open(t.Context(), &Request{Kind: models.UIEventKindLoader})
	require.NoError(t, err)
	opened := receiveState(t, published)
	assert.Nil(t, opened.Events[0].ExpiresAt)
	select {
	case state := <-published:
		t.Fatalf("unexpected state %+v", state)
	default:
	}
}

func TestServiceHandOffDeclinedPresents(t *testing.T) {
	t.Parallel()

	renderer := &handoffRenderer{}
	renderer.presented = make(chan models.UIEvent, 1)
	service, published := newTestService(clockwork.NewFakeClock(), renderer)
	handle, err := service.Open(t.Context(), &Request{Kind: models.UIEventKindNotice, Message: "Hi"})
	require.NoError(t, err)
	_ = receiveState(t, published)
	presented := <-renderer.presented
	assert.Equal(t, "Hi", presented.Message)
	require.NoError(t, handle.Complete(models.UIOutcomeCompleted))
	assert.Equal(t, int32(1), renderer.closed.Load())
}

func TestServiceHandOffNotAskedForSkippedEvents(t *testing.T) {
	t.Parallel()

	renderer := &handoffRenderer{handled: true}
	service, _ := newTestService(clockwork.NewFakeClock(), renderer)
	_, err := service.Open(t.Context(), &Request{Kind: models.UIEventKindConfirm, SkipHostRenderer: true})
	require.NoError(t, err)
	assert.Zero(t, renderer.offered.Load())
}

func TestServicePublishesChoiceMedia(t *testing.T) {
	t.Parallel()

	service, published := newTestService(clockwork.NewFakeClock(), nil)
	media := &models.UIChoiceMedia{System: "SNES", Path: "/games/a.sfc", Name: "A"}
	_, err := service.Open(t.Context(), &Request{
		Kind:    models.UIEventKindPicker,
		Choices: []Choice{{Label: "A", Media: media}, {Label: "B"}},
	})
	require.NoError(t, err)
	state := receiveState(t, published)
	require.Len(t, state.Events[0].Choices, 2)
	assert.Equal(t, *media, *state.Events[0].Choices[0].Media)
	assert.NotSame(t, media, state.Events[0].Choices[0].Media)
	assert.Nil(t, state.Events[0].Choices[1].Media)
}
