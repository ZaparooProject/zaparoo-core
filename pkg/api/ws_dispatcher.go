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

	apimiddleware "github.com/ZaparooProject/zaparoo-core/v2/pkg/api/middleware"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/olahol/melody"
	"github.com/rs/zerolog/log"
)

const (
	wsHighConcurrency       = 1
	wsNormalConcurrency     = 4
	wsLowConcurrency        = 2
	wsQueueSize             = 256
	wsResponseQueueSize     = 256
	wsGlobalImageConcurrent = 2
)

var (
	wsGlobalImageSlots = make(chan struct{}, wsGlobalImageConcurrent)
	wsMediaDBMu        syncutil.RWMutex
)

const wsDispatcherSessionKey = "api.ws.dispatcher"

type wsRequestJob struct {
	tracker   RequestTracker
	methodMap *MethodMap
	cs        *apimiddleware.ClientSession
	cancel    context.CancelFunc
	env       *requests.RequestEnv
	method    string
	msg       []byte
	image     bool
}

type wsResponseJob struct {
	tracker RequestTracker
	cs      *apimiddleware.ClientSession
	cancel  context.CancelFunc
	result  requestResult
	pong    bool
}

type wsSessionDispatcher struct {
	ctx       context.Context
	cancel    context.CancelFunc
	session   *melody.Session
	high      chan *wsRequestJob
	normal    chan *wsRequestJob
	low       chan *wsRequestJob
	responses chan *wsResponseJob
}

func getOrCreateWSDispatcher(parent context.Context, session *melody.Session) *wsSessionDispatcher {
	if existing, ok := session.Get(wsDispatcherSessionKey); ok {
		if d, ok := existing.(*wsSessionDispatcher); ok {
			return d
		}
	}

	ctx, cancel := context.WithCancel(parent)
	d := &wsSessionDispatcher{
		ctx:       ctx,
		cancel:    cancel,
		session:   session,
		high:      make(chan *wsRequestJob, wsQueueSize),
		normal:    make(chan *wsRequestJob, wsQueueSize),
		low:       make(chan *wsRequestJob, wsQueueSize),
		responses: make(chan *wsResponseJob, wsResponseQueueSize),
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
	d.cancel()
	d.drainQueuedJobs(d.high)
	d.drainQueuedJobs(d.normal)
	d.drainQueuedJobs(d.low)
	d.drainQueuedResponses()
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
	for range wsHighConcurrency {
		go d.worker(d.high)
	}
	for range wsNormalConcurrency {
		go d.worker(d.normal)
	}
	for range wsLowConcurrency {
		go d.worker(d.low)
	}
	go d.writer()
}

func (d *wsSessionDispatcher) enqueue(job *wsRequestJob, priority apiRequestPriority) error {
	var q chan *wsRequestJob
	switch priority {
	case apiPriorityHigh:
		q = d.high
	case apiPriorityLow:
		q = d.low
	default:
		q = d.normal
	}

	select {
	case <-d.ctx.Done():
		return d.ctx.Err()
	case q <- job:
		return nil
	default:
		return errors.New("websocket request queue is full")
	}
}

func (d *wsSessionDispatcher) enqueuePong(cs *apimiddleware.ClientSession, tracker RequestTracker) error {
	select {
	case <-d.ctx.Done():
		return d.ctx.Err()
	case d.responses <- &wsResponseJob{cs: cs, tracker: tracker, pong: true}:
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
	//nolint:gosec // Cancellation is transferred to job and invoked when response handling completes.
	ctx, cancel := context.WithTimeout(d.ctx, config.APIRequestTimeout)
	job.env.Context = ctx
	job.cancel = cancel

	defer func() {
		if r := recover(); r != nil {
			log.Error().Interface("panic", r).Msg("panic in websocket request worker")
			d.enqueueResponse(&wsResponseJob{
				result: requestResult{ID: models.NullRPCID, Error: &JSONRPCErrorInternalError, ShouldReply: true},
				cs:     job.cs, tracker: job.tracker, cancel: job.cancel,
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
	d.enqueueResponse(&wsResponseJob{result: result, cs: job.cs, tracker: job.tracker, cancel: job.cancel})
}

func lockMediaDBForAPIMethod(method string) func() {
	// Instant control methods (run/launch, stop, media.control) never touch
	// MediaDB, so they must not wait behind a slow tag/meta write or an
	// in-flight indexing commit holding this lock.
	if isMediaDBFreeInstantMethod(method) {
		return func() {}
	}
	if isMediaDBTransactionAPIMethod(method) {
		wsMediaDBMu.Lock()
		return wsMediaDBMu.Unlock
	}
	// media.image already has its own tiny concurrency gate; do not let slow
	// image reads/resizes hold the API DB read lane and starve tag/meta writes.
	if isImageAPIMethod(method) {
		return func() {}
	}
	wsMediaDBMu.RLock()
	return wsMediaDBMu.RUnlock
}

func (d *wsSessionDispatcher) finishWithoutReply(job *wsRequestJob) {
	d.enqueueResponse(&wsResponseJob{
		result:  requestResult{ShouldReply: false},
		cs:      job.cs,
		tracker: job.tracker,
		cancel:  job.cancel,
	})
}

func (d *wsSessionDispatcher) enqueueResponse(resp *wsResponseJob) {
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
	method := methodFromAPIRequestPayload(msg)
	priority := classifyAPIMethod(method)
	env.Context = d.ctx
	job := &wsRequestJob{
		methodMap: methodMap,
		env:       env,
		method:    method,
		msg:       append([]byte(nil), msg...),
		cs:        cs,
		tracker:   tracker,
		image:     isImageAPIMethod(method),
	}
	if err := d.enqueue(job, priority); err != nil {
		return fmt.Errorf("enqueue websocket request: %w", err)
	}
	return nil
}
