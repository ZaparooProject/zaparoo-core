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
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMediaWriteArbiterPairwiseConflicts(t *testing.T) {
	operations := []MediaWriteOperation{
		MediaWriteOperationIndexing,
		MediaWriteOperationScraping,
		MediaWriteOperationOptimization,
	}

	for _, active := range operations {
		for _, requested := range operations {
			t.Run(string(active)+"_blocks_"+string(requested), func(t *testing.T) {
				var arbiter MediaWriteArbiter
				lease, err := arbiter.TryAcquire(active)
				require.NoError(t, err)

				blockedLease, err := arbiter.TryAcquire(requested)
				require.ErrorIs(t, err, ErrMediaWriteConflict)
				assert.Nil(t, blockedLease)
				var conflict *MediaWriteConflictError
				require.ErrorAs(t, err, &conflict)
				assert.Equal(t, active, conflict.Active)
				assert.Equal(t, requested, conflict.Requested)

				lease.Release()
				next, err := arbiter.TryAcquire(requested)
				require.NoError(t, err)
				next.Release()
			})
		}
	}
}

func TestMediaWriteArbiterSimultaneousStartsChooseOneOwner(t *testing.T) {
	var arbiter MediaWriteArbiter
	const attempts = 64
	start := make(chan struct{})
	release := make(chan struct{})
	var attempted sync.WaitGroup
	var finished sync.WaitGroup
	var acquired atomic.Int32
	attempted.Add(attempts)
	finished.Add(attempts)

	for range attempts {
		go func() {
			defer finished.Done()
			<-start
			lease, err := arbiter.TryAcquire(MediaWriteOperationIndexing)
			if err != nil {
				attempted.Done()
				return
			}
			acquired.Add(1)
			attempted.Done()
			<-release
			lease.Release()
		}()
	}

	close(start)
	attempted.Wait()
	assert.Equal(t, int32(1), acquired.Load())
	close(release)
	finished.Wait()
	assert.Equal(t, MediaWriteOperationNone, arbiter.Active())
}

func TestMediaWriteLeaseHandoffHasNoUnownedGap(t *testing.T) {
	var arbiter MediaWriteArbiter
	lease, err := arbiter.TryAcquire(MediaWriteOperationIndexing)
	require.NoError(t, err)

	require.NoError(t, lease.Handoff(MediaWriteOperationOptimization))
	assert.True(t, lease.ValidFor(MediaWriteOperationOptimization))
	assert.Equal(t, MediaWriteOperationOptimization, arbiter.Active())

	blocked, err := arbiter.TryAcquire(MediaWriteOperationScraping)
	require.ErrorIs(t, err, ErrMediaWriteConflict)
	assert.Nil(t, blocked)

	lease.Release()
	scrapeLease, err := arbiter.TryAcquire(MediaWriteOperationScraping)
	require.NoError(t, err)
	scrapeLease.Release()
}

func TestMediaWriteLeaseDuplicateAndConcurrentRelease(t *testing.T) {
	var arbiter MediaWriteArbiter
	lease, err := arbiter.TryAcquire(MediaWriteOperationScraping)
	require.NoError(t, err)

	const releases = 32
	var wg sync.WaitGroup
	wg.Add(releases)
	for range releases {
		go func() {
			defer wg.Done()
			lease.Release()
		}()
	}
	wg.Wait()
	lease.Release()

	assert.Equal(t, MediaWriteOperationNone, arbiter.Active())
	assert.Equal(t, MediaWriteOperationNone, lease.Operation())
	require.ErrorIs(t, lease.Handoff(MediaWriteOperationOptimization), ErrMediaWriteLease)
}

func TestMediaWriteArbiterRejectsEmptyOperation(t *testing.T) {
	var arbiter MediaWriteArbiter
	lease, err := arbiter.TryAcquire(MediaWriteOperationNone)
	assert.Nil(t, lease)
	require.ErrorIs(t, err, ErrMediaWriteLease)
	assert.NotErrorIs(t, err, ErrMediaWriteConflict)
}

type mediaDBWithWriteCoordinator struct {
	MediaDBI
	MediaDBWriteCoordinator
}

type mediaDBWithoutWriteCoordinator struct {
	MediaDBI
}

func TestGetMediaDBWriteCoordinator(t *testing.T) {
	var arbiter MediaWriteArbiter
	coordinatedDB := &mediaDBWithWriteCoordinator{
		MediaDBWriteCoordinator: &testMediaWriteCoordinator{arbiter: &arbiter},
	}

	coordinator, err := GetMediaDBWriteCoordinator(coordinatedDB)
	require.NoError(t, err)
	assert.Same(t, coordinatedDB, coordinator)

	coordinator, err = GetMediaDBWriteCoordinator(&mediaDBWithoutWriteCoordinator{})
	assert.Nil(t, coordinator)
	require.ErrorIs(t, err, ErrMediaWriteCoordinatorUnavailable)
}

func TestMediaWriteConflictErrorDescribesAndWrapsConflict(t *testing.T) {
	var arbiter MediaWriteArbiter
	lease, err := arbiter.TryAcquire(MediaWriteOperationIndexing)
	require.NoError(t, err)
	defer lease.Release()

	_, err = arbiter.TryAcquire(MediaWriteOperationScraping)
	require.Error(t, err)
	assert.Equal(t, "media database indexing is in progress", err.Error())
	require.ErrorIs(t, err, ErrMediaWriteConflict)
}

type testMediaWriteCoordinator struct {
	arbiter *MediaWriteArbiter
}

func (c *testMediaWriteCoordinator) AcquireMediaWrite(
	operation MediaWriteOperation,
) (*MediaWriteLease, error) {
	return c.arbiter.TryAcquire(operation)
}

func (c *testMediaWriteCoordinator) ActiveMediaWriteOperation() MediaWriteOperation {
	return c.arbiter.Active()
}

func (*testMediaWriteCoordinator) RunBackgroundOptimizationWithLease(
	func(bool), *syncutil.Pauser, *MediaWriteLease,
) error {
	return nil
}
