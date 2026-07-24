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

package windowfocus

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeAPI struct {
	findAfter     int
	activateAfter int
	findCalls     int
	activateCalls int
}

func (*fakeAPI) allowProcessForeground(_ uint32) {}

func (f *fakeAPI) findProcessWindow(_ uint32) (uintptr, bool) {
	f.findCalls++
	return 42, f.findCalls >= f.findAfter
}

func (f *fakeAPI) activateWindow(_ uintptr) bool {
	f.activateCalls++
	return f.activateCalls >= f.activateAfter
}

func TestProcessTreeIncludesNestedChildren(t *testing.T) {
	t.Parallel()

	pids := processTree(10, []processRelation{
		{pid: 30, parentPID: 20},
		{pid: 20, parentPID: 10},
		{pid: 40, parentPID: 99},
	})

	assert.Equal(t, map[uint32]struct{}{10: {}, 20: {}, 30: {}}, pids)
}

func TestManagerFocusWaitsForWindowAndActivation(t *testing.T) {
	t.Parallel()

	api := &fakeAPI{findAfter: 2, activateAfter: 2}
	manager := newManager(api, time.Millisecond, 100*time.Millisecond)

	require.NoError(t, manager.Focus(context.Background(), 123))
	assert.GreaterOrEqual(t, api.findCalls, 3)
	assert.Equal(t, 2, api.activateCalls)
}

func TestManagerFocusStopsWhenCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	api := &fakeAPI{findAfter: 100, activateAfter: 1}
	manager := newManager(api, time.Millisecond, time.Second)

	err := manager.Focus(ctx, 123)
	require.ErrorIs(t, err, context.Canceled)
}

func TestManagerFocusTimesOut(t *testing.T) {
	t.Parallel()

	api := &fakeAPI{findAfter: 100, activateAfter: 1}
	manager := newManager(api, time.Millisecond, 10*time.Millisecond)

	err := manager.Focus(context.Background(), 123)
	require.ErrorIs(t, err, errFocusTimeout)
}

func TestManagerFocusIgnoresInvalidRequests(t *testing.T) {
	t.Parallel()

	assert.NoError(t, (*Manager)(nil).Focus(context.Background(), 123))
	assert.NoError(t, newManager(nil, time.Millisecond, time.Second).Focus(context.Background(), 123))
	assert.NoError(t, newManager(&fakeAPI{}, time.Millisecond, time.Second).Focus(context.Background(), 0))
}
