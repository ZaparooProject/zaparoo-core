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

// Package apidiag records bounded, content-free API timeout diagnostics.
package apidiag

import (
	"context"
	"errors"
	"net"
	"os"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/jonboulle/clockwork"
)

// Stage identifies code-owned phases, never request-supplied operation names.
type Stage uint8

const (
	Dispatch Stage = iota
	DatabaseLock
	DatabasePool
	ConcurrencySlot
	Handler
	Database
	ResponseBuild
	ResponseQueue
	ResponseWrite
	AfterWrite
	stageCount
)

func (s Stage) String() string {
	names := [...]string{
		"dispatch", "database_lock", "database_pool", "concurrency_slot", "handler", "database",
		"response_build", "response_queue", "response_write", "after_write",
	}
	if int(s) >= len(names) {
		return "unknown"
	}
	return names[s]
}

type Transport uint8

const (
	HTTP Transport = iota
	WebSocket
)

func (t Transport) String() string {
	switch t {
	case HTTP:
		return "http"
	case WebSocket:
		return "websocket"
	default:
		return "unknown"
	}
}

type DeadlineKind uint8

const (
	RequestDeadline DeadlineKind = iota
	OperationDeadline
)

func (k DeadlineKind) String() string {
	if k == RequestDeadline {
		return "request"
	}
	if k == OperationDeadline {
		return "operation"
	}
	return "unknown"
}

// State distinguishes an unavailable snapshot from an observed false value.
type State uint8

const (
	Unknown State = iota
	Inactive
	Active
)

func Observed(active bool) State {
	if active {
		return Active
	}
	return Inactive
}

func (s State) String() string {
	switch s {
	case Inactive:
		return "inactive"
	case Active:
		return "active"
	default:
		return "unknown"
	}
}

// Pool contains process-wide counters, not attribution to an individual request.
type Pool struct {
	WaitDuration time.Duration
	WaitCount    int64
	Max          int
	Open         int
	InUse        int
	Idle         int
	Available    bool
}

type DatabaseSnapshot struct {
	Pool        Pool
	Transaction State
	Indexing    State
	Optimizing  State
	Recovery    State
}

// DatabaseProvider is optional; existing database interfaces remain unchanged.
// Implementations must not query SQL, inspect files, or wait for application locks.
type DatabaseProvider interface {
	APIDiagnostics() DatabaseSnapshot
}

type Snapshot struct {
	MediaDB      DatabaseSnapshot
	UserDB       DatabaseSnapshot
	Scraping     State
	MediaPlaying State
	Recovery     State
}

type Report struct {
	Started       time.Time
	Method        string
	StartSnapshot Snapshot
	EndSnapshot   Snapshot
	Durations     [stageCount]time.Duration
	Elapsed       time.Duration
	Budget        time.Duration
	QueueWait     time.Duration
	ActiveStages  [stageCount]bool
	Stage         Stage
	Transport     Transport
	Kind          DeadlineKind
}

type span struct {
	started time.Time
	id      uint64
	stage   Stage
}

// Recorder is scoped to one request. Timings of nested stages are inclusive,
// so their sum is not the request's elapsed time.
type Recorder struct {
	ctx      context.Context
	clock    clockwork.Clock
	snapshot func() Snapshot
	emit     func(Report)
	stop     func() bool
	spans    [16]span
	report   Report
	nextID   uint64
	mu       syncutil.Mutex
	finished bool
	reported bool
}

type contextKey struct{}

// New starts diagnostics without changing the context's deadline or cancellation.
// Snapshot and emit run outside the recorder lock and must be safe concurrently.
func New(ctx context.Context, method string, transport Transport, queueWait time.Duration,
	snapshot func() Snapshot, emit func(Report), clock clockwork.Clock,
) (context.Context, *Recorder) {
	if clock == nil {
		clock = clockwork.NewRealClock()
	}
	now := clock.Now()
	r := &Recorder{
		ctx: ctx, clock: clock, snapshot: snapshot, emit: emit,
		report: Report{Started: now, Method: MethodName(method), Transport: transport, QueueWait: queueWait},
	}
	if deadline, ok := ctx.Deadline(); ok {
		r.report.Budget = max(time.Duration(0), deadline.Sub(now))
	}
	if snapshot != nil {
		r.report.StartSnapshot = snapshot()
	}
	r.stop = context.AfterFunc(ctx, func() {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			r.Report(RequestDeadline)
		}
	})
	return context.WithValue(ctx, contextKey{}, r), r
}

// Check Done first so success paths never wait for cancellation, including
// with clockwork's fake contexts.
func contextError(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func FromContext(ctx context.Context) *Recorder {
	if ctx == nil {
		return nil
	}
	r, ok := ctx.Value(contextKey{}).(*Recorder)
	if !ok {
		return nil
	}
	return r
}

// Begin returns an idempotent stage completion function. Excess concurrent
// stages are ignored rather than allowing unbounded diagnostic state.
func Begin(ctx context.Context, stage Stage) func() {
	r := FromContext(ctx)
	if r == nil || stage >= stageCount {
		return func() {}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.finished || r.reported {
		return func() {}
	}
	for i := range r.spans {
		if r.spans[i].id != 0 {
			continue
		}
		r.nextID++
		id := r.nextID
		r.spans[i] = span{started: r.clock.Now(), id: id, stage: stage}
		return func() {
			if errors.Is(contextError(r.ctx), context.DeadlineExceeded) {
				r.Report(RequestDeadline)
			}
			r.mu.Lock()
			defer r.mu.Unlock()
			if r.spans[i].id != id {
				return
			}
			if !r.finished && !r.reported {
				r.report.Durations[stage] += r.clock.Since(r.spans[i].started)
			}
			r.spans[i] = span{}
		}
	}
	return func() {}
}

// RecordError reports nested operation deadlines as well as request deadlines.
// It never copies the error text, which may contain private request contents.
func RecordError(ctx context.Context, err error) {
	r := FromContext(ctx)
	if r == nil || errors.Is(contextError(ctx), context.Canceled) {
		return
	}
	if errors.Is(contextError(ctx), context.DeadlineExceeded) {
		r.Report(RequestDeadline)
	} else if IsTimeout(err) {
		r.Report(OperationDeadline)
	}
}

// IsContextFailure identifies errors that belong in request diagnostics, not
// a second unstructured error event. Cancellation remains distinct from timeout.
func IsContextFailure(ctx context.Context, err error) bool {
	return errors.Is(err, context.Canceled) || IsTimeout(err) ||
		(ctx != nil && contextError(ctx) != nil)
}

func IsTimeout(err error) bool {
	var networkErr net.Error
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) ||
		(errors.As(err, &networkErr) && networkErr.Timeout())
}

func (r *Recorder) Report(kind DeadlineKind) {
	if r == nil {
		return
	}
	r.mu.Lock()
	if r.finished || r.reported || errors.Is(contextError(r.ctx), context.Canceled) {
		r.mu.Unlock()
		return
	}
	r.reported = true
	report := r.report
	report.Kind = kind
	report.Elapsed = r.clock.Since(report.Started)
	var newest uint64
	for _, active := range &r.spans {
		if active.id == 0 {
			continue
		}
		report.Durations[active.stage] += r.clock.Since(active.started)
		report.ActiveStages[active.stage] = true
		if active.id > newest {
			newest = active.id
			report.Stage = active.stage
		}
	}
	r.mu.Unlock()
	if r.snapshot != nil {
		report.EndSnapshot = r.snapshot()
	}
	if r.emit != nil {
		r.emit(report)
	}
}

// Finish detaches the deadline callback after the complete response lifecycle.
// A deadline racing completion is reported once, never lost or duplicated.
func (r *Recorder) Finish() {
	if r == nil {
		return
	}
	if errors.Is(contextError(r.ctx), context.DeadlineExceeded) {
		r.Report(RequestDeadline)
	}
	r.mu.Lock()
	r.finished = true
	r.mu.Unlock()
	r.stop()
}
