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

package database

import (
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
)

// MediaWriteOperation identifies one process-local MediaDB write owner.
type MediaWriteOperation string

const (
	MediaWriteOperationNone         MediaWriteOperation = ""
	MediaWriteOperationIndexing     MediaWriteOperation = "indexing"
	MediaWriteOperationScraping     MediaWriteOperation = "scraping"
	MediaWriteOperationOptimization MediaWriteOperation = "optimization"
	MediaWriteOperationMaintenance  MediaWriteOperation = "maintenance"
	MediaWriteOperationRecovery     MediaWriteOperation = "recovery"
)

var (
	ErrMediaWriteConflict               = errors.New("media database write operation conflict")
	ErrMediaWriteLease                  = errors.New("invalid media database write lease")
	ErrMediaWriteCoordinatorUnavailable = errors.New("media database write coordinator unavailable")
)

// MediaDBWriteCoordinator is optional process-local write arbitration layered
// over MediaDBI without expanding that compatibility-sensitive interface.
type MediaDBWriteCoordinator interface {
	AcquireMediaWrite(operation MediaWriteOperation) (*MediaWriteLease, error)
	ActiveMediaWriteOperation() MediaWriteOperation
	RunBackgroundOptimizationWithLease(
		statusCallback func(optimizing bool), pauser *syncutil.Pauser, lease *MediaWriteLease,
	) error
}

// GetMediaDBWriteCoordinator returns write arbitration supported by current MediaDB implementations.
func GetMediaDBWriteCoordinator(mediaDB MediaDBI) (MediaDBWriteCoordinator, error) {
	coordinator, ok := mediaDB.(MediaDBWriteCoordinator)
	if !ok {
		return nil, ErrMediaWriteCoordinatorUnavailable
	}
	return coordinator, nil
}

// MediaWriteConflictError reports which process-local owner blocked a request.
type MediaWriteConflictError struct {
	Requested MediaWriteOperation
	Active    MediaWriteOperation
}

func (e *MediaWriteConflictError) Error() string {
	return fmt.Sprintf("media database %s is in progress", e.Active)
}

func (*MediaWriteConflictError) Unwrap() error {
	return ErrMediaWriteConflict
}

// MediaWriteArbiter serializes process-local MediaDB mutations. Its zero value is ready to use.
type MediaWriteArbiter struct {
	active   MediaWriteOperation
	mu       syncutil.Mutex
	nextID   uint64
	activeID uint64
}

// MediaWriteLease is exclusive ownership of one MediaDB write-operation slot.
// Release is idempotent. Handoff changes operation type without an unowned gap.
type MediaWriteLease struct {
	arbiter  *MediaWriteArbiter
	id       uint64
	released atomic.Bool
}

// TryAcquire atomically claims the write slot.
func (a *MediaWriteArbiter) TryAcquire(operation MediaWriteOperation) (*MediaWriteLease, error) {
	if operation == MediaWriteOperationNone {
		return nil, fmt.Errorf("%w: operation is empty", ErrMediaWriteLease)
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.active != MediaWriteOperationNone {
		return nil, &MediaWriteConflictError{Requested: operation, Active: a.active}
	}

	a.nextID++
	if a.nextID == 0 {
		a.nextID++
	}
	a.activeID = a.nextID
	a.active = operation
	return &MediaWriteLease{arbiter: a, id: a.activeID}, nil
}

// Active reports current process-local owner, or MediaWriteOperationNone.
func (a *MediaWriteArbiter) Active() MediaWriteOperation {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.active
}

// Operation reports operation currently owned by this lease.
func (l *MediaWriteLease) Operation() MediaWriteOperation {
	if l == nil || l.arbiter == nil || l.released.Load() {
		return MediaWriteOperationNone
	}

	l.arbiter.mu.Lock()
	defer l.arbiter.mu.Unlock()
	if l.arbiter.activeID != l.id {
		return MediaWriteOperationNone
	}
	return l.arbiter.active
}

// ValidFor reports whether lease still owns operation.
func (l *MediaWriteLease) ValidFor(operation MediaWriteOperation) bool {
	return l.Operation() == operation
}

// Handoff atomically transfers ownership to another operation type.
func (l *MediaWriteLease) Handoff(operation MediaWriteOperation) error {
	if l == nil || l.arbiter == nil || operation == MediaWriteOperationNone || l.released.Load() {
		return ErrMediaWriteLease
	}

	l.arbiter.mu.Lock()
	defer l.arbiter.mu.Unlock()
	if l.released.Load() || l.arbiter.activeID != l.id || l.arbiter.active == MediaWriteOperationNone {
		return ErrMediaWriteLease
	}
	l.arbiter.active = operation
	return nil
}

// Release relinquishes ownership exactly once. Duplicate calls are no-ops.
func (l *MediaWriteLease) Release() {
	if l == nil || l.arbiter == nil || !l.released.CompareAndSwap(false, true) {
		return
	}

	l.arbiter.mu.Lock()
	defer l.arbiter.mu.Unlock()
	if l.arbiter.activeID != l.id {
		return
	}
	l.arbiter.activeID = 0
	l.arbiter.active = MediaWriteOperationNone
}
