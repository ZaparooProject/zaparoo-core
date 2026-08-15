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

package api

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	apimiddleware "github.com/ZaparooProject/zaparoo-core/v2/pkg/api/middleware"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/olahol/melody"
	"github.com/rs/zerolog/log"
)

type mediaDBLockMode uint8

const (
	wsHighConcurrency         = 1
	wsNormalConcurrency       = 4
	wsLowConcurrency          = 2
	wsQueueSize               = 256
	wsLowQueueSize            = 16
	wsResponseQueueSize       = 256
	wsGlobalImageConcurrent   = 2
	wsInputWorkerDrainTimeout = 2 * time.Second
)

const (
	mediaDBLockNone mediaDBLockMode = iota
	mediaDBLockRead
	mediaDBLockWrite
)

var (
	errWSRequestQueueFull = errors.New("websocket request queue is full")
	wsGlobalImageSlots    = make(chan struct{}, wsGlobalImageConcurrent)
	wsMediaDBMu           syncutil.RWMutex
)

const wsDispatcherSessionKey = "api.ws.dispatcher"

type wsRequestQueueFullError struct {
	method    string
	requestID models.RPCID
	priority  apiRequestPriority
	depth     int
	capacity  int
}

func (e *wsRequestQueueFullError) Error() string {
	return fmt.Sprintf("%s: method %s", errWSRequestQueueFull, e.method)
}

func (*wsRequestQueueFullError) Unwrap() error {
	return errWSRequestQueueFull
}

func queueDuration(enqueuedAt time.Time) time.Duration {
	if enqueuedAt.IsZero() {
		return 0
	}
	return time.Since(enqueuedAt)
}

type wsRequestJob struct {
	tracker    RequestTracker
	methodMap  *MethodMap
	cs         *apimiddleware.ClientSession
	cancel     context.CancelFunc
	env        *requests.RequestEnv
	enqueuedAt time.Time
	requestID  models.RPCID
	method     string
	msg        []byte
	image      bool
}

type wsResponseJob struct {
	enqueuedAt time.Time
	tracker    RequestTracker
	cs         *apimiddleware.ClientSession
	cancel     context.CancelFunc
	method     string
	result     requestResult
	pong       bool
}

type wsSessionDispatcher struct {
	ctx          context.Context
	cancel       context.CancelFunc
	session      *melody.Session
	inputSession platforms.InputSession
	high         chan *wsRequestJob
	normal       chan *wsRequestJob
	input        chan *wsRequestJob
	low          chan *wsRequestJob
	responses    chan *wsResponseJob
	inputDone    chan struct{}
	closeOnce    sync.Once
}

func getOrCreateWSDispatcher(
	parent context.Context,
	session *melody.Session,
	platform platforms.Platform,
) *wsSessionDispatcher {
	if existing, ok := session.Get(wsDispatcherSessionKey); ok {
		if d, ok := existing.(*wsSessionDispatcher); ok {
			return d
		}
	}

	ctx, cancel := context.WithCancel(parent)
	var inputSession platforms.InputSession
	if provider, ok := platform.(platforms.InputSessionProvider); ok {
		inputSession = provider.NewInputSession()
	}
	d := &wsSessionDispatcher{
		ctx:          ctx,
		cancel:       cancel,
		session:      session,
		inputSession: inputSession,
		high:         make(chan *wsRequestJob, wsQueueSize),
		normal:       make(chan *wsRequestJob, wsQueueSize),
		input:        make(chan *wsRequestJob, wsQueueSize),
		low:          make(chan *wsRequestJob, wsLowQueueSize),
		responses:    make(chan *wsResponseJob, wsResponseQueueSize),
		inputDone:    make(chan struct{}),
	}
	session.Set(wsDispatcherSessionKey, d)
	d.start()
	return d
}

func closeWSDispatcher(session *melody.Session) {
	existing, ok := session.Get(wsDispatcherSessionKey)
	if !ok {
		return
	}
	d, ok := existing.(*wsSessionDispatcher)
	if !ok {
		return
	}
	d.close()
}

func (d *wsSessionDispatcher) close() {
	d.closeOnce.Do(func() {
		d.cancel()
		d.releaseInputSession()
		d.drainQueuedJobs(d.high)
		d.drainQueuedJobs(d.normal)
		d.drainQueuedJobs(d.input)
		d.drainQueuedJobs(d.low)
		d.waitForInputWorker()
		// Retry after the drain wait in case worker cleanup raced the first
		// release or a device release initially failed.
		d.releaseInputSession()
		d.drainQueuedResponses()
	})
}

func (d *wsSessionDispatcher) waitForInputWorker() {
	if d.inputDone == nil {
		return
	}

	timer := time.NewTimer(wsInputWorkerDrainTimeout)
	defer timer.Stop()
	select {
	case <-d.inputDone:
	case <-timer.C:
		log.Warn().Dur("timeout", wsInputWorkerDrainTimeout).
			Msg("timed out waiting for WebSocket input worker to stop")
	}
}

func (d *wsSessionDispatcher) releaseInputSession() {
	if d.inputSession == nil {
		return
	}
	if err := d.inputSession.ReleaseAll(); err != nil {
		log.Warn().Err(err).Msg("error releasing WebSocket input session")
	}
}

func (*wsSessionDispatcher) drainQueuedJobs(queue <-chan *wsRequestJob) {
	for {
		select {
		case job := <-queue:
			if job.cancel != nil {
				job.cancel()
			}
			if job.tracker != nil {
				job.tracker.RequestEnded()
			}
		default:
			return
		}
	}
}

func (d *wsSessionDispatcher) drainQueuedResponses() {
	for {
		select {
		case resp := <-d.responses:
			if resp.cancel != nil {
				resp.cancel()
			}
			if resp.tracker != nil {
				resp.tracker.RequestEnded()
			}
		default:
			return
		}
	}
}

func (d *wsSessionDispatcher) start() {
	if d.inputDone == nil {
		d.inputDone = make(chan struct{})
	}
	for range wsHighConcurrency {
		go d.worker(d.high)
	}
	for range wsNormalConcurrency {
		go d.worker(d.normal)
	}
	go func() {
		defer close(d.inputDone)
		d.worker(d.input)
	}()
	for range wsLowConcurrency {
		go d.worker(d.low)
	}
	go d.writer()
}

func (d *wsSessionDispatcher) queue(priority apiRequestPriority) chan *wsRequestJob {
	switch priority {
	case apiPriorityHigh:
		return d.high
	case apiPriorityInput:
		return d.input
	case apiPriorityLow:
		return d.low
	default:
		return d.normal
	}
}

func (d *wsSessionDispatcher) enqueue(job *wsRequestJob, priority apiRequestPriority) error {
	q := d.queue(priority)
	select {
	case <-d.ctx.Done():
		return d.ctx.Err()
	case q <- job:
		return nil
	default:
		return errWSRequestQueueFull
	}
}

func (d *wsSessionDispatcher) enqueuePong(cs *apimiddleware.ClientSession, tracker RequestTracker) error {
	select {
	case <-d.ctx.Done():
		return d.ctx.Err()
	case d.responses <- &wsResponseJob{
		cs: cs, tracker: tracker, enqueuedAt: time.Now(), method: "ping", pong: true,
	}:
		return nil
	default:
		return errors.New("websocket response queue is full")
	}
}

func (d *wsSessionDispatcher) worker(queue <-chan *wsRequestJob) {
	for {
		select {
		case <-d.ctx.Done():
			return
		case job := <-queue:
			d.runJob(job)
		}
	}
}

func (d *wsSessionDispatcher) runJob(job *wsRequestJob) {
	log.Debug().
		Str("method", job.method).
		Str("requestId", requestIDForLog(job.requestID)).
		Dur("queueWaitDuration", queueDuration(job.enqueuedAt)).
		Msg("websocket request dequeued")

	//nolint:gosec // Cancellation is transferred to job and invoked when response handling completes.
	ctx, cancel := requestContextForAPIMethod(d.ctx, job.method)
	job.env.Context = ctx
	job.cancel = cancel

	defer func() {
		if r := recover(); r != nil {
			log.Error().Interface("panic", r).Msg("panic in websocket request worker")
			d.enqueueResponse(&wsResponseJob{
				result: requestResult{ID: models.NullRPCID, Error: &JSONRPCErrorInternalError, ShouldReply: true},
				cs:     job.cs, tracker: job.tracker, cancel: job.cancel, method: job.method,
			})
		}
	}()

	if job.image {
		select {
		case <-job.env.Context.Done():
			d.finishWithoutReply(job)
			return
		case <-d.ctx.Done():
			d.finishWithoutReply(job)
			return
		case wsGlobalImageSlots <- struct{}{}:
			defer func() { <-wsGlobalImageSlots }()
		}
	}

	unlock := lockMediaDBForAPIMethod(job.method)
	defer unlock()

	result := processRequestObject(job.methodMap, *job.env, job.msg)
	d.enqueueResponse(&wsResponseJob{
		result: result, cs: job.cs, tracker: job.tracker, cancel: job.cancel, method: job.method,
	})
}

func mediaDBLockModeForAPIMethod(method string) mediaDBLockMode {
	// Instant control methods (run/launch, stop, media.control) never touch
	// MediaDB, so they must not wait behind a slow tag/meta write or an
	// in-flight indexing commit holding this lock.
	if isMediaDBFreeInstantMethod(method) {
		return mediaDBLockNone
	}
	if isMediaDBTransactionAPIMethod(method) {
		return mediaDBLockWrite
	}
	// media.image already has its own tiny concurrency gate; do not let slow
	// image reads/resizes hold the API DB read lane and starve tag/meta writes.
	if isImageAPIMethod(method) {
		return mediaDBLockNone
	}
	return mediaDBLockRead
}

func lockMediaDBForAPIMethod(method string) func() {
	switch mediaDBLockModeForAPIMethod(method) {
	case mediaDBLockWrite:
		wsMediaDBMu.Lock()
		return wsMediaDBMu.Unlock
	case mediaDBLockRead:
		wsMediaDBMu.RLock()
		return wsMediaDBMu.RUnlock
	default:
		return func() {}
	}
}

func (d *wsSessionDispatcher) finishWithoutReply(job *wsRequestJob) {
	d.enqueueResponse(&wsResponseJob{
		result:  requestResult{ShouldReply: false},
		cs:      job.cs,
		tracker: job.tracker,
		cancel:  job.cancel,
		method:  job.method,
	})
}

func (d *wsSessionDispatcher) enqueueResponse(resp *wsResponseJob) {
	resp.enqueuedAt = time.Now()
	select {
	case <-d.ctx.Done():
		if resp.cancel != nil {
			resp.cancel()
		}
		if resp.tracker != nil {
			resp.tracker.RequestEnded()
		}
	case d.responses <- resp:
	}
}

func (d *wsSessionDispatcher) writer() {
	for {
		select {
		case <-d.ctx.Done():
			return
		case resp := <-d.responses:
			d.writeResponse(resp)
		}
	}
}

func (d *wsSessionDispatcher) writeResponse(resp *wsResponseJob) {
	requestID := resp.result.ID
	if resp.pong {
		requestID = models.RPCID{}
	}
	log.Debug().
		Str("method", resp.method).
		Str("requestId", requestIDForLog(requestID)).
		Dur("responseQueueDuration", queueDuration(resp.enqueuedAt)).
		Msg("websocket response dequeued")

	defer func() {
		if resp.cancel != nil {
			resp.cancel()
		}
		if resp.tracker != nil {
			resp.tracker.RequestEnded()
		}
	}()

	if resp.pong {
		if err := writePong(d.session.Write, resp.cs); err != nil {
			logWSWriteError(err, "sending pong")
			closeMelodySession(d.session)
		}
		return
	}

	if !resp.result.ShouldReply {
		return
	}

	if resp.result.Error != nil {
		if err := sendWSEncryptedError(d.session, resp.cs, resp.result.ID, *resp.result.Error); err != nil {
			logWSWriteError(err, "error sending error response")
			closeMelodySession(d.session)
		}
	} else {
		if err := sendWSEncryptedResponse(d.session, resp.cs, resp.result.ID, resp.result.Result); err != nil {
			logWSWriteError(err, "error sending response")
			closeMelodySession(d.session)
		}
	}
	if resp.result.AfterWrite != nil {
		resp.result.AfterWrite()
	}
}

func enqueueWSRequest(
	d *wsSessionDispatcher,
	methodMap *MethodMap,
	env *requests.RequestEnv,
	msg []byte,
	cs *apimiddleware.ClientSession,
	tracker RequestTracker,
) error {
	method, requestID := requestMetadataFromAPIRequestPayload(msg)
	priority := classifyAPIMethod(method)
	env.Context = d.ctx
	job := &wsRequestJob{
		methodMap:  methodMap,
		env:        env,
		enqueuedAt: time.Now(),
		requestID:  requestID,
		method:     method,
		msg:        append([]byte(nil), msg...),
		cs:         cs,
		tracker:    tracker,
		image:      isImageAPIMethod(method),
	}
	if err := d.enqueue(job, priority); err != nil {
		if errors.Is(err, errWSRequestQueueFull) {
			q := d.queue(priority)
			return &wsRequestQueueFullError{
				requestID: requestID,
				method:    method,
				priority:  priority,
				depth:     len(q),
				capacity:  cap(q),
			}
		}
		return fmt.Errorf("enqueue websocket request: %w", err)
	}
	return nil
}
