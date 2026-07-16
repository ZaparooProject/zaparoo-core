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

// Package events manages global, transient UI requests. It owns presentation
// lifecycle and response arbitration; callers retain ownership of domain work.
package events

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/google/uuid"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog/log"
)

const (
	MaxTitleBytes   = 512
	MaxMessageBytes = 16 * 1024
	MaxChoices      = 1000
	MaxLabelBytes   = 512
)

var (
	ErrClosed         = errors.New("UI event service is closed")
	ErrNoActiveEvent  = errors.New("no active UI event")
	ErrEventNotActive = errors.New("UI event is not active")
	ErrInvalidKind    = errors.New("invalid UI event kind")
	ErrInvalidAction  = errors.New("invalid UI response action")
	ErrNotDismissible = errors.New("UI event is not dismissible")
	ErrChoiceRequired = errors.New("choice ID is required")
	ErrChoiceNotFound = errors.New("UI choice was not found")
	ErrInvalidRequest = errors.New("invalid UI event request")
	ErrInvalidOutcome = errors.New("invalid UI event outcome")
	ErrEventExpired   = errors.New("UI event has expired")
)

// Renderer presents UI events on the host platform. Renderer failure never
// cancels an event because remote clients remain valid fallback renderers.
type Renderer interface {
	PresentUI(context.Context, *models.UIEvent) (closeFn func() error, err error)
}

// UpdatingRenderer optionally applies producer updates to an existing host UI.
type UpdatingRenderer interface {
	UpdateUI(context.Context, *models.UIEvent) error
}

// TimedRenderer reports minimum time a producer should keep a newly presented
// event open before completing it. This accommodates host UI startup latency.
type TimedRenderer interface {
	MinimumUIDisplay(models.UIEventKind) time.Duration
}

// Publisher broadcasts an authoritative UI state snapshot.
type Publisher func(models.UIStateResponse)

// Choice combines public display text with a private caller-owned value. Value
// is returned only to producer and is never serialized into public event.
type Choice struct {
	Value any
	Label string
}

// Request describes one transient UI interaction. A positive Timeout creates
// authoritative expiry; zero or negative values leave event open until resolved.
type Request struct {
	Kind           models.UIEventKind
	Title          string
	Message        string
	Choices        []Choice
	SelectedChoice int
	Timeout        time.Duration
	Dismissible    bool
	// SkipHostRenderer keeps the request available to API clients without
	// presenting it through the platform renderer.
	SkipHostRenderer bool
}

// Update changes presentation of active event while preserving its ID. Nil
// fields remain unchanged. A non-positive Timeout removes authoritative expiry.
type Update struct {
	Title       *string
	Message     *string
	Timeout     *time.Duration
	Dismissible *bool
}

// Result is delivered once when an external response, timeout, cancellation,
// or supersession resolves request. Value contains private selected choice value.
type Result struct {
	Value      any
	Resolution models.UIResolution
}

// Handle lets producer update or complete request it opened.
type Handle struct {
	service        *Service
	Results        <-chan Result
	ID             string
	MinimumDisplay time.Duration
}

// Complete resolves event from producer side. Completing stale/superseded
// handle is harmless so deferred loader cleanup remains safe.
func (h *Handle) Complete(outcome models.UIOutcome) error {
	if h == nil || h.service == nil {
		return nil
	}
	err := h.service.complete(h.ID, outcome)
	if outcome != models.UIOutcomeConfirmed &&
		(errors.Is(err, ErrNoActiveEvent) || errors.Is(err, ErrEventNotActive)) {
		return nil
	}
	return err
}

// Update changes active event. Updating stale handle is reported to caller.
func (h *Handle) Update(update Update) error {
	if h == nil || h.service == nil {
		return ErrEventNotActive
	}
	return h.service.Update(h.ID, update)
}

type activeEvent struct {
	stopContext      func() bool
	closeRenderer    func() error
	timer            clockwork.Timer
	choices          map[string]Choice
	result           chan Result
	event            models.UIEvent
	timerGeneration  uint64
	skipHostRenderer bool
}

// Service owns current global UI event and arbitrates all terminal outcomes.
type Service struct {
	clock    clockwork.Clock
	renderer Renderer
	publish  Publisher
	active   *activeEvent
	revision uint64
	closed   bool
	mu       syncutil.Mutex
}

// New creates UI event service. Nil clock uses real time. Renderer and
// publisher may be nil for headless use and tests.
func New(clock clockwork.Clock, renderer Renderer, publish Publisher) *Service {
	if clock == nil {
		clock = clockwork.NewRealClock()
	}
	return &Service{
		clock:    clock,
		renderer: renderer,
		publish:  publish,
	}
}

// Open replaces current request, if any, and returns producer handle.
func (s *Service) Open(ctx context.Context, request *Request) (*Handle, error) {
	if err := validateRequest(request); err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}

	now := s.clock.Now()
	entry := newActiveEvent(request, now)

	var replaced *activeEvent
	var replacedResolution *models.UIResolution

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, ErrClosed
	}
	if s.active != nil {
		replaced = s.active
		stopActiveTimers(replaced)
		resolution := models.UIResolution{
			ID:      replaced.event.ID,
			Outcome: models.UIOutcomeSuperseded,
		}
		replacedResolution = &resolution
	}
	s.active = entry
	s.revision++
	state := s.stateLocked(resolutionSlice(replacedResolution))
	s.mu.Unlock()

	if replaced != nil {
		closeRenderer(replaced)
		s.deliverResult(replaced, Result{Resolution: *replacedResolution})
	}
	s.publishState(state)

	s.attachCancellation(ctx, entry.event.ID)
	s.attachTimer(entry.event.ID, entry.timerGeneration, request.Timeout)

	minimumDisplay := time.Duration(0)
	if !entry.skipHostRenderer {
		event := cloneEvent(&entry.event)
		s.present(ctx, entry.event.ID, &event)
		if timedRenderer, ok := s.renderer.(TimedRenderer); ok {
			minimumDisplay = timedRenderer.MinimumUIDisplay(entry.event.Kind)
		}
	}
	return &Handle{
		service:        s,
		Results:        entry.result,
		ID:             entry.event.ID,
		MinimumDisplay: minimumDisplay,
	}, nil
}

// State returns immutable current snapshot. Resolved is always empty for query.
func (s *Service) State() models.UIStateResponse {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stateLocked(nil)
}

// Respond atomically resolves current event from API or host renderer input.
func (s *Service) Respond(id string, action models.UIResponseAction, choiceID string) error {
	s.mu.Lock()
	entry, err := s.activeForIDLocked(id)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	if s.eventExpired(entry) {
		state, resolution := s.expireLocked(entry)
		s.mu.Unlock()
		closeRenderer(entry)
		s.publishState(state)
		s.deliverResult(entry, Result{Resolution: resolution})
		return ErrEventExpired
	}

	resolution, value, err := validateResponse(entry, action, choiceID)
	if err != nil {
		s.mu.Unlock()
		return err
	}

	s.active = nil
	stopActiveTimers(entry)
	s.revision++
	state := s.stateLocked([]models.UIResolution{resolution})
	s.mu.Unlock()

	closeRenderer(entry)
	s.publishState(state)
	s.deliverResult(entry, Result{Resolution: resolution, Value: value})
	return nil
}

// Update changes one active event and publishes newer revision.
func (s *Service) Update(id string, update Update) error {
	if err := validateUpdate(update); err != nil {
		return err
	}

	var timeout time.Duration
	var resetTimer bool

	s.mu.Lock()
	entry, err := s.activeForIDLocked(id)
	if err != nil {
		s.mu.Unlock()
		return err
	}

	if update.Title != nil {
		entry.event.Title = *update.Title
	}
	if update.Message != nil {
		entry.event.Message = *update.Message
	}
	if update.Dismissible != nil {
		entry.event.Dismissible = *update.Dismissible
	}
	if update.Timeout != nil {
		resetTimer = true
		timeout = *update.Timeout
		entry.timerGeneration++
		if entry.timer != nil {
			entry.timer.Stop()
			entry.timer = nil
		}
		if timeout > 0 {
			expiresAt := s.clock.Now().Add(timeout)
			entry.event.ExpiresAt = &expiresAt
		} else {
			entry.event.ExpiresAt = nil
		}
	}

	s.revision++
	event := cloneEvent(&entry.event)
	generation := entry.timerGeneration
	skipHostRenderer := entry.skipHostRenderer
	state := s.stateLocked(nil)
	s.mu.Unlock()

	s.publishState(state)
	if updatingRenderer, ok := s.renderer.(UpdatingRenderer); ok && !skipHostRenderer {
		if err = updatingRenderer.UpdateUI(context.Background(), &event); err != nil {
			log.Warn().Err(err).Str("event_id", id).Msg("host UI renderer update failed")
		}
	}
	if resetTimer {
		s.attachTimer(id, generation, timeout)
	}
	return nil
}

// Cancel resolves active event and notifies producer.
func (s *Service) Cancel(id string) error {
	return s.resolve(id, models.UIOutcomeCancelled, true)
}

// Shutdown prevents new events and cancels active request, if present.
func (s *Service) Shutdown() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	entry := s.active
	if entry == nil {
		s.mu.Unlock()
		return
	}
	s.active = nil
	stopActiveTimers(entry)
	s.revision++
	resolution := models.UIResolution{ID: entry.event.ID, Outcome: models.UIOutcomeCancelled}
	state := s.stateLocked([]models.UIResolution{resolution})
	s.mu.Unlock()

	closeRenderer(entry)
	s.publishState(state)
	s.deliverResult(entry, Result{Resolution: resolution})
}

func (s *Service) complete(id string, outcome models.UIOutcome) error {
	if !producerOutcomeAllowed(outcome) {
		return ErrInvalidOutcome
	}

	s.mu.Lock()
	entry, err := s.activeForIDLocked(id)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	if s.eventExpired(entry) {
		state, resolution := s.expireLocked(entry)
		s.mu.Unlock()
		closeRenderer(entry)
		s.publishState(state)
		s.deliverResult(entry, Result{Resolution: resolution})
		return ErrEventExpired
	}

	s.active = nil
	stopActiveTimers(entry)
	s.revision++
	resolution := models.UIResolution{ID: entry.event.ID, Outcome: outcome}
	state := s.stateLocked([]models.UIResolution{resolution})
	s.mu.Unlock()

	closeRenderer(entry)
	s.publishState(state)
	return nil
}

func (s *Service) resolve(id string, outcome models.UIOutcome, notifyProducer bool) error {
	s.mu.Lock()
	entry, err := s.activeForIDLocked(id)
	if err != nil {
		s.mu.Unlock()
		return err
	}

	s.active = nil
	stopActiveTimers(entry)
	s.revision++
	resolution := models.UIResolution{ID: entry.event.ID, Outcome: outcome}
	state := s.stateLocked([]models.UIResolution{resolution})
	s.mu.Unlock()

	closeRenderer(entry)
	s.publishState(state)
	if notifyProducer {
		s.deliverResult(entry, Result{Resolution: resolution})
	}
	return nil
}

func (s *Service) eventExpired(entry *activeEvent) bool {
	return entry.event.ExpiresAt != nil && !s.clock.Now().Before(*entry.event.ExpiresAt)
}

func (s *Service) expireLocked(entry *activeEvent) (models.UIStateResponse, models.UIResolution) {
	s.active = nil
	stopActiveTimers(entry)
	s.revision++
	resolution := models.UIResolution{ID: entry.event.ID, Outcome: models.UIOutcomeTimedOut}
	return s.stateLocked([]models.UIResolution{resolution}), resolution
}

func (s *Service) activeForIDLocked(id string) (*activeEvent, error) {
	if s.active == nil {
		return nil, ErrNoActiveEvent
	}
	if s.active.event.ID != id {
		return nil, ErrEventNotActive
	}
	return s.active, nil
}

func (s *Service) stateLocked(resolved []models.UIResolution) models.UIStateResponse {
	events := make([]models.UIEvent, 0, 1)
	if s.active != nil {
		events = append(events, cloneEvent(&s.active.event))
	}
	if resolved == nil {
		resolved = make([]models.UIResolution, 0)
	} else {
		resolved = append([]models.UIResolution(nil), resolved...)
	}
	return models.UIStateResponse{
		Events:   events,
		Resolved: resolved,
		Revision: s.revision,
	}
}

func (s *Service) attachCancellation(ctx context.Context, id string) {
	stop := context.AfterFunc(ctx, func() {
		if err := s.resolve(id, models.UIOutcomeCancelled, true); err != nil &&
			!errors.Is(err, ErrNoActiveEvent) && !errors.Is(err, ErrEventNotActive) {
			log.Warn().Err(err).Str("event_id", id).Msg("failed to cancel UI event from context")
		}
	})

	s.mu.Lock()
	if s.active != nil && s.active.event.ID == id {
		s.active.stopContext = stop
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	stop()
}

func (s *Service) attachTimer(id string, generation uint64, timeout time.Duration) {
	if timeout <= 0 {
		return
	}

	timer := s.clock.AfterFunc(timeout, func() {
		s.resolveTimeout(id, generation)
	})

	s.mu.Lock()
	if s.active != nil && s.active.event.ID == id && s.active.timerGeneration == generation {
		s.active.timer = timer
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	timer.Stop()
}

func (s *Service) resolveTimeout(id string, generation uint64) {
	s.mu.Lock()
	if s.active == nil || s.active.event.ID != id || s.active.timerGeneration != generation {
		s.mu.Unlock()
		return
	}
	entry := s.active
	s.active = nil
	stopActiveTimers(entry)
	s.revision++
	resolution := models.UIResolution{ID: id, Outcome: models.UIOutcomeTimedOut}
	state := s.stateLocked([]models.UIResolution{resolution})
	s.mu.Unlock()

	closeRenderer(entry)
	s.publishState(state)
	s.deliverResult(entry, Result{Resolution: resolution})
}

func (s *Service) present(ctx context.Context, id string, event *models.UIEvent) {
	if s.renderer == nil {
		return
	}
	closeFn, err := s.renderer.PresentUI(ctx, event)
	if err != nil {
		log.Warn().Err(err).Str("event_id", id).Msg("host UI renderer failed")
	}
	if closeFn == nil {
		return
	}

	s.mu.Lock()
	if s.active != nil && s.active.event.ID == id {
		s.active.closeRenderer = closeFn
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	if err = closeFn(); err != nil {
		log.Warn().Err(err).Str("event_id", id).Msg("failed to close stale host UI renderer")
	}
}

func closeRenderer(entry *activeEvent) {
	if entry == nil || entry.closeRenderer == nil {
		return
	}
	if err := entry.closeRenderer(); err != nil {
		log.Warn().Err(err).Str("event_id", entry.event.ID).Msg("failed to close host UI renderer")
	}
}

func (s *Service) publishState(state models.UIStateResponse) {
	if s.publish != nil {
		s.publish(state)
	}
}

func (*Service) deliverResult(entry *activeEvent, result Result) {
	if entry == nil {
		return
	}
	entry.result <- result
	close(entry.result)
}

func newActiveEvent(request *Request, now time.Time) *activeEvent {
	id := uuid.NewString()
	publicChoices := make([]models.UIChoice, 0, len(request.Choices))
	privateChoices := make(map[string]Choice, len(request.Choices))
	selectedChoiceID := ""

	for i, choice := range request.Choices {
		choiceID := uuid.NewString()
		publicChoices = append(publicChoices, models.UIChoice{ID: choiceID, Label: choice.Label})
		privateChoices[choiceID] = choice
		if i == request.SelectedChoice {
			selectedChoiceID = choiceID
		}
	}

	var expiresAt *time.Time
	if request.Timeout > 0 {
		expires := now.Add(request.Timeout)
		expiresAt = &expires
	}

	return &activeEvent{
		choices:          privateChoices,
		result:           make(chan Result, 1),
		skipHostRenderer: request.SkipHostRenderer,
		event: models.UIEvent{
			ExpiresAt:        expiresAt,
			ID:               id,
			Kind:             request.Kind,
			Title:            request.Title,
			Message:          request.Message,
			Choices:          publicChoices,
			SelectedChoiceID: selectedChoiceID,
			CreatedAt:        now,
			Dismissible:      request.Dismissible,
		},
		timerGeneration: 1,
	}
}

func validateRequest(request *Request) error {
	if request == nil {
		return fmt.Errorf("%w: request is required", ErrInvalidRequest)
	}
	switch request.Kind {
	case models.UIEventKindNotice, models.UIEventKindLoader, models.UIEventKindPicker, models.UIEventKindConfirm:
	default:
		return fmt.Errorf("%w: %q", ErrInvalidKind, request.Kind)
	}
	if len(request.Title) > MaxTitleBytes {
		return fmt.Errorf("%w: title exceeds %d bytes", ErrInvalidRequest, MaxTitleBytes)
	}
	if len(request.Message) > MaxMessageBytes {
		return fmt.Errorf("%w: message exceeds %d bytes", ErrInvalidRequest, MaxMessageBytes)
	}
	if len(request.Choices) > MaxChoices {
		return fmt.Errorf("%w: choices exceed %d items", ErrInvalidRequest, MaxChoices)
	}
	if request.Kind == models.UIEventKindPicker && len(request.Choices) == 0 {
		return fmt.Errorf("%w: picker requires at least one choice", ErrInvalidRequest)
	}
	if request.Kind != models.UIEventKindPicker && len(request.Choices) != 0 {
		return fmt.Errorf("%w: choices require picker kind", ErrInvalidRequest)
	}
	for _, choice := range request.Choices {
		if choice.Label == "" {
			return fmt.Errorf("%w: choice label is required", ErrInvalidRequest)
		}
		if len(choice.Label) > MaxLabelBytes {
			return fmt.Errorf("%w: choice label exceeds %d bytes", ErrInvalidRequest, MaxLabelBytes)
		}
	}
	if len(request.Choices) > 0 &&
		(request.SelectedChoice < -1 || request.SelectedChoice >= len(request.Choices)) {
		return fmt.Errorf("%w: selected choice is out of range", ErrInvalidRequest)
	}
	return nil
}

func validateUpdate(update Update) error {
	if update.Title != nil && len(*update.Title) > MaxTitleBytes {
		return fmt.Errorf("%w: title exceeds %d bytes", ErrInvalidRequest, MaxTitleBytes)
	}
	if update.Message != nil && len(*update.Message) > MaxMessageBytes {
		return fmt.Errorf("%w: message exceeds %d bytes", ErrInvalidRequest, MaxMessageBytes)
	}
	return nil
}

func validateResponse(
	entry *activeEvent,
	action models.UIResponseAction,
	choiceID string,
) (models.UIResolution, any, error) {
	resolution := models.UIResolution{ID: entry.event.ID}

	switch action {
	case models.UIResponseActionDismiss:
		if !entry.event.Dismissible {
			return models.UIResolution{}, nil, ErrNotDismissible
		}
		resolution.Outcome = models.UIOutcomeDismissed
		return resolution, nil, nil
	case models.UIResponseActionConfirm:
		if entry.event.Kind != models.UIEventKindConfirm {
			return models.UIResolution{}, nil, ErrInvalidAction
		}
		resolution.Outcome = models.UIOutcomeConfirmed
		return resolution, nil, nil
	case models.UIResponseActionSelect:
		if entry.event.Kind != models.UIEventKindPicker {
			return models.UIResolution{}, nil, ErrInvalidAction
		}
		if choiceID == "" {
			return models.UIResolution{}, nil, ErrChoiceRequired
		}
		choice, ok := entry.choices[choiceID]
		if !ok {
			return models.UIResolution{}, nil, ErrChoiceNotFound
		}
		resolution.Outcome = models.UIOutcomeSelected
		resolution.ChoiceID = choiceID
		return resolution, choice.Value, nil
	default:
		return models.UIResolution{}, nil, ErrInvalidAction
	}
}

func producerOutcomeAllowed(outcome models.UIOutcome) bool {
	switch outcome {
	case models.UIOutcomeConfirmed,
		models.UIOutcomeDismissed,
		models.UIOutcomeCompleted,
		models.UIOutcomeCancelled:
		return true
	default:
		return false
	}
}

func stopActiveTimers(entry *activeEvent) {
	if entry == nil {
		return
	}
	entry.timerGeneration++
	if entry.timer != nil {
		entry.timer.Stop()
		entry.timer = nil
	}
	if entry.stopContext != nil {
		entry.stopContext()
		entry.stopContext = nil
	}
}

func cloneEvent(event *models.UIEvent) models.UIEvent {
	cloned := *event
	if event.ExpiresAt != nil {
		expiresAt := *event.ExpiresAt
		cloned.ExpiresAt = &expiresAt
	}
	cloned.Choices = append([]models.UIChoice(nil), event.Choices...)
	return cloned
}

func resolutionSlice(resolution *models.UIResolution) []models.UIResolution {
	if resolution == nil {
		return nil
	}
	return []models.UIResolution{*resolution}
}
