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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCoordinatorAllowsReadersAndRejectsConflictingWriter(t *testing.T) {
	t.Parallel()
	coordinator := New()
	first, err := coordinator.Begin(context.Background(), OperationLocalInspect, OperationRead)
	require.NoError(t, err)
	defer first.Release()
	second, err := coordinator.Begin(context.Background(), OperationLocalInspect, OperationRead)
	require.NoError(t, err)
	defer second.Release()

	_, err = coordinator.Begin(context.Background(), OperationLocalDelete, OperationWrite)
	var busy *BusyError
	require.ErrorAs(t, err, &busy)
	assert.Equal(t, OperationLocalInspect, busy.Kind)

	first.Release()
	second.Release()
	writer, err := coordinator.Begin(context.Background(), OperationLocalDelete, OperationWrite)
	require.NoError(t, err)
	writer.Release()
}

func TestCoordinatorShutdownCancelsAndWaitsForOperation(t *testing.T) {
	t.Parallel()
	coordinator := New()
	lease, err := coordinator.Begin(context.Background(), OperationRemoteUpload, OperationWrite)
	require.NoError(t, err)

	released := make(chan struct{})
	go func() {
		<-lease.Context().Done()
		lease.Release()
		close(released)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, coordinator.Shutdown(ctx))
	<-released
	require.ErrorIs(t, lease.Context().Err(), context.Canceled)

	_, err = coordinator.Begin(context.Background(), OperationLocalCreate, OperationWrite)
	assert.ErrorIs(t, err, ErrStopped)
}

func TestCoordinatorReportsWriteFinished(t *testing.T) {
	t.Parallel()
	coordinator := New()
	finished := make(chan OperationKind, 4)
	coordinator.SetOnWriteFinished(func(kind OperationKind) { finished <- kind })

	// Read operations end silently.
	readLease, err := coordinator.Begin(context.Background(), OperationLocalInspect, OperationRead)
	require.NoError(t, err)
	readLease.Release()
	select {
	case kind := <-finished:
		t.Fatalf("read operation must not report finished, got %s", kind)
	default:
	}

	// A write operation reports its kind exactly once on release.
	writeLease, err := coordinator.Begin(context.Background(), OperationRemoteUpload, OperationWrite)
	require.NoError(t, err)
	writeLease.Release()
	select {
	case kind := <-finished:
		assert.Equal(t, OperationRemoteUpload, kind)
	default:
		t.Fatal("write operation release must report finished")
	}
	writeLease.Release()
	select {
	case kind := <-finished:
		t.Fatalf("double release must not report finished again, got %s", kind)
	default:
	}
}

func TestCoordinatorCancelRestoreLeavesUnrelatedLeaseRunning(t *testing.T) {
	t.Parallel()
	coordinator := New()
	restoreKinds := []OperationKind{OperationLocalRestore, OperationRemoteRestore, OperationRecovery}
	restoreLeases := make([]*Lease, 0, len(restoreKinds))
	for _, kind := range restoreKinds {
		lease, err := coordinator.Begin(context.Background(), kind, OperationRead)
		require.NoError(t, err)
		restoreLeases = append(restoreLeases, lease)
	}
	unrelated, err := coordinator.Begin(context.Background(), OperationLocalInspect, OperationRead)
	require.NoError(t, err)
	defer unrelated.Release()

	assert.True(t, coordinator.CancelRestore())
	for _, lease := range restoreLeases {
		require.ErrorIs(t, lease.Context().Err(), context.Canceled)
		lease.Release()
	}
	require.NoError(t, unrelated.Context().Err())
	kind, _, active := coordinator.Active()
	assert.True(t, active)
	assert.Equal(t, OperationLocalInspect, kind)
}

func TestCoordinatorRemoteUnlinkedState(t *testing.T) {
	t.Parallel()
	coordinator := New()

	assert.False(t, coordinator.RemoteUnlinked())
	coordinator.SetRemoteUnlinked(true)
	assert.True(t, coordinator.RemoteUnlinked())
	coordinator.SetRemoteUnlinked(false)
	assert.False(t, coordinator.RemoteUnlinked())
}

func TestCoordinatorActiveTracksReadAndWriteLeases(t *testing.T) {
	t.Parallel()
	coordinator := New()

	kind, startedAt, active := coordinator.Active()
	assert.False(t, active)
	assert.Empty(t, kind)
	assert.True(t, startedAt.IsZero())

	readLease, err := coordinator.Begin(context.Background(), OperationLocalInspect, OperationRead)
	require.NoError(t, err)
	kind, startedAt, active = coordinator.Active()
	assert.True(t, active)
	assert.Equal(t, OperationLocalInspect, kind)
	assert.False(t, startedAt.IsZero())
	readLease.Release()
	_, _, active = coordinator.Active()
	assert.False(t, active)

	writeLease, err := coordinator.Begin(context.Background(), OperationRemoteUpload, OperationWrite)
	require.NoError(t, err)
	kind, startedAt, active = coordinator.Active()
	assert.True(t, active)
	assert.Equal(t, OperationRemoteUpload, kind)
	assert.False(t, startedAt.IsZero())
	writeLease.Release()
	_, _, active = coordinator.Active()
	assert.False(t, active)
}
