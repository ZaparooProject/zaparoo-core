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

package hostfiles

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStalledRootDoesNotStarveOtherRoots(t *testing.T) {
	t.Parallel()
	stalled, healthy := "/test-stalled-"+t.Name(), "/test-healthy-"+t.Name()
	short := &Browser{timeout: 50 * time.Millisecond}
	block := make(chan struct{})
	stall := func() (int, error) {
		<-block
		return 0, nil
	}

	// Reads that never answer time out but keep their slots.
	for range maxConcurrentRootReads {
		_, err := bounded(context.Background(), short, stalled, stall)
		require.ErrorIs(t, err, ErrTimeout)
	}
	_, err := bounded(context.Background(), short, stalled, stall)
	require.ErrorIs(t, err, ErrBusy, "the stalled root has no slot left")

	got, err := bounded(context.Background(), short, healthy, func() (int, error) { return 7, nil })
	require.NoError(t, err, "another root still gets a slot")
	assert.Equal(t, 7, got)

	// Once the filesystem answers, the stalled root's slots come back.
	close(block)
	long := &Browser{timeout: 5 * time.Second}
	got, err = bounded(context.Background(), long, stalled, func() (int, error) { return 1, nil })
	require.NoError(t, err)
	assert.Equal(t, 1, got)
}

func TestBoundedReportsCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	b := &Browser{timeout: 5 * time.Second}
	started := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	go func() {
		<-started
		cancel()
	}()
	_, err := bounded(ctx, b, "/test-cancel-"+t.Name(), func() (int, error) {
		close(started)
		<-release
		return 0, nil
	})
	require.ErrorIs(t, err, context.Canceled)
}
