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
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testRenderer struct {
	presentErr error
	presented  chan models.UIEvent
	minDisplay time.Duration
	closed     atomic.Int32
	updated    atomic.Int32
}

func (r *testRenderer) PresentUI(_ context.Context, event *models.UIEvent) (func() error, error) {
	if r.presented != nil {
		r.presented <- *event
	}
	if r.presentErr != nil {
		return nil, r.presentErr
	}
	return func() error {
		r.closed.Add(1)
		return nil
	}, nil
}

func (r *testRenderer) UpdateUI(_ context.Context, _ *models.UIEvent) error {
	r.updated.Add(1)
	return nil
}

func (r *testRenderer) MinimumUIDisplay(_ models.UIEventKind) time.Duration {
	return r.minDisplay
}

func newTestService(
	clock clockwork.Clock,
	renderer Renderer,
) (service *Service, states <-chan models.UIStateResponse) {
	published := make(chan models.UIStateResponse, 20)
	service = New(clock, renderer, func(state models.UIStateResponse) {
		published <- state
	})
	return service, published
}

func receiveResult(t *testing.T, results <-chan Result) Result {
	t.Helper()
	select {
	case result := <-results:
		return result
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for UI result")
		return Result{}
	}
}

func receiveState(t *testing.T, states <-chan models.UIStateResponse) models.UIStateResponse {
	t.Helper()
	select {
	case state := <-states:
		return state
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for UI state")
		return models.UIStateResponse{}
	}
}

func TestServiceOpenAndSelect(t *testing.T) {
	t.Parallel()

	clock := clockwork.NewFakeClockAt(time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC))
	service, published := newTestService(clock, nil)

	handle, err := service.Open(t.Context(), &Request{
		Kind:    models.UIEventKindPicker,
		Title:   "Choose",
		Message: "Pick one",
		Choices: []Choice{
			{Label: "First", Value: "**launch:first"},
			{Label: "Second", Value: "**launch:second"},
		},
		SelectedChoice: 1,
		Dismissible:    true,
		Timeout:        30 * time.Second,
	})
	require.NoError(t, err)

	opened := receiveState(t, published)
	assert.Equal(t, uint64(1), opened.Revision)
	require.Len(t, opened.Events, 1)
	assert.Empty(t, opened.Resolved)
	event := opened.Events[0]
	assert.Equal(t, handle.ID, event.ID)
	assert.Equal(t, event.Choices[1].ID, event.SelectedChoiceID)
	require.NotNil(t, event.ExpiresAt)
	assert.Equal(t, clock.Now().Add(30*time.Second), *event.ExpiresAt)

	encoded, err := json.Marshal(event)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "**launch")

	err = service.Respond(handle.ID, models.UIResponseActionSelect, event.Choices[1].ID)
	require.NoError(t, err)

	result := receiveResult(t, handle.Results)
	assert.Equal(t, models.UIOutcomeSelected, result.Resolution.Outcome)
	assert.Equal(t, event.Choices[1].ID, result.Resolution.ChoiceID)
	assert.Equal(t, "**launch:second", result.Value)

	resolved := receiveState(t, published)
	assert.Equal(t, uint64(2), resolved.Revision)
	assert.Empty(t, resolved.Events)
	require.Len(t, resolved.Resolved, 1)
	assert.Equal(t, models.UIOutcomeSelected, resolved.Resolved[0].Outcome)

	query := service.State()
	assert.Equal(t, uint64(2), query.Revision)
	assert.NotNil(t, query.Events)
	assert.NotNil(t, query.Resolved)
	assert.Empty(t, query.Resolved)
}

func TestServiceStateIsImmutable(t *testing.T) {
	t.Parallel()

	service, _ := newTestService(clockwork.NewFakeClock(), nil)
	_, err := service.Open(t.Context(), &Request{
		Kind:    models.UIEventKindPicker,
		Choices: []Choice{{Label: "Original"}},
	})
	require.NoError(t, err)

	state := service.State()
	state.Events[0].Choices[0].Label = "Mutated"

	fresh := service.State()
	assert.Equal(t, "Original", fresh.Events[0].Choices[0].Label)
}

func TestServiceSupersedesActiveEvent(t *testing.T) {
	t.Parallel()

	renderer := &testRenderer{presented: make(chan models.UIEvent, 2)}
	service, published := newTestService(clockwork.NewFakeClock(), renderer)

	first, err := service.Open(t.Context(), &Request{Kind: models.UIEventKindLoader})
	require.NoError(t, err)
	_ = receiveState(t, published)

	second, err := service.Open(t.Context(), &Request{Kind: models.UIEventKindNotice, Dismissible: true})
	require.NoError(t, err)

	result := receiveResult(t, first.Results)
	assert.Equal(t, models.UIOutcomeSuperseded, result.Resolution.Outcome)

	state := receiveState(t, published)
	assert.Equal(t, uint64(2), state.Revision)
	require.Len(t, state.Events, 1)
	assert.Equal(t, second.ID, state.Events[0].ID)
	require.Len(t, state.Resolved, 1)
	assert.Equal(t, first.ID, state.Resolved[0].ID)
	assert.Equal(t, models.UIOutcomeSuperseded, state.Resolved[0].Outcome)
	assert.Equal(t, int32(1), renderer.closed.Load())

	assert.NoError(t, first.Complete(models.UIOutcomeCompleted))
}

func TestServiceTimeoutAndReset(t *testing.T) {
	t.Parallel()

	clock := clockwork.NewFakeClock()
	service, published := newTestService(clock, nil)
	handle, err := service.Open(t.Context(), &Request{
		Kind:    models.UIEventKindConfirm,
		Timeout: 10 * time.Second,
	})
	require.NoError(t, err)
	_ = receiveState(t, published)
	require.NoError(t, clock.BlockUntilContext(t.Context(), 1))

	newTimeout := 20 * time.Second
	require.NoError(t, handle.Update(Update{Timeout: &newTimeout}))
	updated := receiveState(t, published)
	assert.Equal(t, uint64(2), updated.Revision)
	require.NotNil(t, updated.Events[0].ExpiresAt)
	assert.Equal(t, clock.Now().Add(newTimeout), *updated.Events[0].ExpiresAt)
	require.NoError(t, clock.BlockUntilContext(t.Context(), 1))

	clock.Advance(10 * time.Second)
	select {
	case result := <-handle.Results:
		t.Fatalf("event resolved before reset timeout: %+v", result)
	default:
	}

	clock.Advance(10 * time.Second)
	result := receiveResult(t, handle.Results)
	assert.Equal(t, models.UIOutcomeTimedOut, result.Resolution.Outcome)

	resolved := receiveState(t, published)
	assert.Equal(t, uint64(3), resolved.Revision)
	assert.Equal(t, models.UIOutcomeTimedOut, resolved.Resolved[0].Outcome)
}

func TestServiceFirstResponseWins(t *testing.T) {
	t.Parallel()

	service, _ := newTestService(clockwork.NewRealClock(), nil)
	handle, err := service.Open(t.Context(), &Request{
		Kind:        models.UIEventKindConfirm,
		Dismissible: true,
	})
	require.NoError(t, err)

	var successes atomic.Int32
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if service.Respond(handle.ID, models.UIResponseActionConfirm, "") == nil {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()

	assert.Equal(t, int32(1), successes.Load())
	assert.Equal(t, models.UIOutcomeConfirmed, receiveResult(t, handle.Results).Resolution.Outcome)
}

func TestServiceValidatesResponses(t *testing.T) {
	t.Parallel()

	service, _ := newTestService(clockwork.NewFakeClock(), nil)
	handle, err := service.Open(t.Context(), &Request{Kind: models.UIEventKindLoader})
	require.NoError(t, err)

	require.ErrorIs(t, service.Respond(handle.ID, models.UIResponseActionDismiss, ""), ErrNotDismissible)
	require.ErrorIs(t, service.Respond(handle.ID, models.UIResponseActionConfirm, ""), ErrInvalidAction)
	require.ErrorIs(t, service.Respond("stale", models.UIResponseActionDismiss, ""), ErrEventNotActive)

	require.NoError(t, handle.Complete(models.UIOutcomeCompleted))
	require.ErrorIs(t, service.Respond(handle.ID, models.UIResponseActionDismiss, ""), ErrNoActiveEvent)
}

func TestServiceRendererFailureKeepsEventActive(t *testing.T) {
	t.Parallel()

	renderer := &testRenderer{presentErr: errors.New("renderer unavailable")}
	service, published := newTestService(clockwork.NewFakeClock(), renderer)
	handle, err := service.Open(t.Context(), &Request{
		Kind:        models.UIEventKindNotice,
		Dismissible: true,
	})
	require.NoError(t, err)
	_ = receiveState(t, published)

	state := service.State()
	require.Len(t, state.Events, 1)
	assert.Equal(t, handle.ID, state.Events[0].ID)
	require.NoError(t, service.Respond(handle.ID, models.UIResponseActionDismiss, ""))
}

func TestServiceSkipsHostRenderer(t *testing.T) {
	t.Parallel()

	renderer := &testRenderer{
		presented:  make(chan models.UIEvent, 1),
		minDisplay: time.Second,
	}
	service, published := newTestService(clockwork.NewFakeClock(), renderer)
	handle, err := service.Open(t.Context(), &Request{
		Kind:             models.UIEventKindConfirm,
		SkipHostRenderer: true,
	})
	require.NoError(t, err)
	assert.Zero(t, handle.MinimumDisplay)

	opened := receiveState(t, published)
	require.Len(t, opened.Events, 1)
	assert.Equal(t, handle.ID, opened.Events[0].ID)
	select {
	case event := <-renderer.presented:
		t.Fatalf("host rendered suppressed event: %+v", event)
	default:
	}

	message := "Updated"
	require.NoError(t, handle.Update(Update{Message: &message}))
	updated := receiveState(t, published)
	assert.Equal(t, message, updated.Events[0].Message)
	assert.Equal(t, int32(0), renderer.updated.Load())

	require.NoError(t, handle.Complete(models.UIOutcomeCompleted))
	assert.Equal(t, int32(0), renderer.closed.Load())
}

func TestServiceContextCancellationAndShutdown(t *testing.T) {
	t.Parallel()

	service, published := newTestService(clockwork.NewFakeClock(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	handle, err := service.Open(ctx, &Request{Kind: models.UIEventKindConfirm})
	require.NoError(t, err)
	_ = receiveState(t, published)

	cancel()
	assert.Equal(t, models.UIOutcomeCancelled, receiveResult(t, handle.Results).Resolution.Outcome)
	_ = receiveState(t, published)

	second, err := service.Open(t.Context(), &Request{Kind: models.UIEventKindLoader})
	require.NoError(t, err)
	_ = receiveState(t, published)
	service.Shutdown()
	assert.Equal(t, models.UIOutcomeCancelled, receiveResult(t, second.Results).Resolution.Outcome)
	_ = receiveState(t, published)

	_, err = service.Open(t.Context(), &Request{Kind: models.UIEventKindNotice})
	require.ErrorIs(t, err, ErrClosed)
}

func TestServiceUpdateNotifiesRenderer(t *testing.T) {
	t.Parallel()

	renderer := &testRenderer{}
	service, published := newTestService(clockwork.NewFakeClock(), renderer)
	handle, err := service.Open(t.Context(), &Request{Kind: models.UIEventKindLoader})
	require.NoError(t, err)
	_ = receiveState(t, published)

	message := "Halfway"
	require.NoError(t, handle.Update(Update{Message: &message}))
	state := receiveState(t, published)
	assert.Equal(t, "Halfway", state.Events[0].Message)
	assert.Equal(t, int32(1), renderer.updated.Load())
}

func TestValidateRequestLimitsAndKinds(t *testing.T) {
	t.Parallel()

	service, _ := newTestService(clockwork.NewFakeClock(), nil)
	_, err := service.Open(t.Context(), &Request{Kind: "unknown"})
	require.ErrorIs(t, err, ErrInvalidKind)

	_, err = service.Open(t.Context(), &Request{
		Kind:  models.UIEventKindNotice,
		Title: strings.Repeat("x", MaxTitleBytes+1),
	})
	require.ErrorIs(t, err, ErrInvalidRequest)

	_, err = service.Open(t.Context(), &Request{Kind: models.UIEventKindPicker})
	require.ErrorIs(t, err, ErrInvalidRequest)

	_, err = service.Open(t.Context(), &Request{
		Kind:    models.UIEventKindPicker,
		Choices: []Choice{{Label: ""}},
	})
	require.ErrorIs(t, err, ErrInvalidRequest)
}
