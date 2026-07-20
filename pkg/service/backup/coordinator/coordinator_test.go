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
