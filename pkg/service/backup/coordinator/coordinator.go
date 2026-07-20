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

package coordinator

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
)

type OperationKind string

type OperationMode uint8

const (
	OperationLocalCreate   OperationKind = "local-create"
	OperationLocalInspect  OperationKind = "local-inspect"
	OperationLocalDelete   OperationKind = "local-delete"
	OperationLocalRestore  OperationKind = "local-restore"
	OperationRemoteUpload  OperationKind = "remote-upload"
	OperationRemoteRestore OperationKind = "remote-restore"
	OperationRecovery      OperationKind = "restore-recovery"
)

const (
	OperationRead OperationMode = iota
	OperationWrite
)

var ErrStopped = errors.New("backup coordinator is shutting down")

type BusyError struct {
	StartedAt time.Time
	Kind      OperationKind
}

func (e *BusyError) Error() string {
	return fmt.Sprintf("backup operation %s has been running since %s", e.Kind, e.StartedAt.Format(time.RFC3339))
}

type activeOperation struct {
	cancel    context.CancelFunc
	startedAt time.Time
	kind      OperationKind
	mode      OperationMode
}

type Coordinator struct {
	operations      map[uint64]activeOperation
	done            chan struct{}
	onWriteFinished func(OperationKind)
	nextID          uint64
	mu              syncutil.Mutex
	shuttingDown    bool
	remoteUnlinked  bool
}

type Lease struct {
	coordinator *Coordinator
	ctx         context.Context
	id          uint64
}

func New() *Coordinator {
	return &Coordinator{operations: make(map[uint64]activeOperation)}
}

func (c *Coordinator) Begin(ctx context.Context, kind OperationKind, mode OperationMode) (*Lease, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.shuttingDown {
		return nil, ErrStopped
	}
	if conflict := c.conflictingOperation(mode); conflict != nil {
		return nil, &BusyError{Kind: conflict.kind, StartedAt: conflict.startedAt}
	}

	//nolint:gosec // Lease.Release or Coordinator.Shutdown owns cancellation.
	opCtx, cancel := context.WithCancel(ctx)
	c.nextID++
	id := c.nextID
	if len(c.operations) == 0 {
		c.done = make(chan struct{})
	}
	c.operations[id] = activeOperation{
		cancel:    cancel,
		startedAt: time.Now().UTC(),
		kind:      kind,
		mode:      mode,
	}
	return &Lease{coordinator: c, ctx: opCtx, id: id}, nil
}

func (c *Coordinator) conflictingOperation(mode OperationMode) *activeOperation {
	for _, operation := range c.operations {
		if mode == OperationWrite || operation.mode == OperationWrite {
			operationCopy := operation
			return &operationCopy
		}
	}
	return nil
}

func (l *Lease) Context() context.Context {
	if l == nil || l.ctx == nil {
		return context.Background()
	}
	return l.ctx
}

func (l *Lease) Release() {
	if l == nil || l.coordinator == nil {
		return
	}
	l.coordinator.release(l.id)
}

// SetOnWriteFinished registers a callback invoked, outside the coordinator
// lock, whenever a write operation's lease is released. It reports that a
// backup, upload, restore, or recovery operation ended, whatever its outcome.
func (c *Coordinator) SetOnWriteFinished(fn func(OperationKind)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onWriteFinished = fn
}

func (c *Coordinator) release(id uint64) {
	c.mu.Lock()
	operation, ok := c.operations[id]
	if !ok {
		c.mu.Unlock()
		return
	}
	delete(c.operations, id)
	operation.cancel()
	if len(c.operations) == 0 && c.done != nil {
		close(c.done)
		c.done = nil
	}
	onWriteFinished := c.onWriteFinished
	c.mu.Unlock()
	if operation.mode == OperationWrite && onWriteFinished != nil {
		onWriteFinished(operation.kind)
	}
}

func (c *Coordinator) SetRemoteUnlinked(unlinked bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.remoteUnlinked = unlinked
}

func (c *Coordinator) RemoteUnlinked() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.remoteUnlinked
}

func (c *Coordinator) Active() (OperationKind, time.Time, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, operation := range c.operations {
		return operation.kind, operation.startedAt, true
	}
	return "", time.Time{}, false
}

func (c *Coordinator) CancelRestore() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	cancelled := false
	for _, operation := range c.operations {
		if operation.kind != OperationLocalRestore && operation.kind != OperationRemoteRestore &&
			operation.kind != OperationRecovery {
			continue
		}
		operation.cancel()
		cancelled = true
	}
	return cancelled
}

func (c *Coordinator) Shutdown(ctx context.Context) error {
	c.mu.Lock()
	c.shuttingDown = true
	for _, operation := range c.operations {
		operation.cancel()
	}
	done := c.done
	c.mu.Unlock()

	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("waiting for backup operation shutdown: %w", ctx.Err())
	}
}
