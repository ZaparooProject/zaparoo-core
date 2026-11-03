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

package service

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A device that stays up for weeks still has to prune, not just at boot.
func TestRepeatUserDBRetention_RunsDaily(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	clock := clockwork.NewFakeClock()
	var runs atomic.Int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		repeatUserDBRetention(ctx, clock, func(context.Context) { runs.Add(1) })
	}()

	require.NoError(t, clock.BlockUntilContext(ctx, 1))
	clock.Advance(userDBRetentionInterval - time.Second)
	assert.Zero(t, runs.Load(), "the boot run covers the first day")

	clock.Advance(time.Second)
	require.Eventually(t, func() bool { return runs.Load() == 1 }, time.Second, time.Millisecond)
	clock.Advance(userDBRetentionInterval)
	require.Eventually(t, func() bool { return runs.Load() == 2 }, time.Second, time.Millisecond)

	cancel()
	<-done
}

func TestPruneInboxMessages_UsesListLimit(t *testing.T) {
	t.Parallel()

	mockUserDB := &testhelpers.MockUserDBI{}
	db := &database.Database{UserDB: mockUserDB}
	mockUserDB.On("PruneInboxMessages", inboxMessageMaxAge, database.InboxMessageLimit).
		Return(int64(3), nil).Once()

	pruneInboxMessages(db)

	mockUserDB.AssertExpectations(t)
}

func TestPruneInboxMessages_DatabaseError(t *testing.T) {
	t.Parallel()

	mockUserDB := &testhelpers.MockUserDBI{}
	db := &database.Database{UserDB: mockUserDB}
	mockUserDB.On("PruneInboxMessages", inboxMessageMaxAge, database.InboxMessageLimit).
		Return(int64(0), assert.AnError).Once()

	pruneInboxMessages(db)

	mockUserDB.AssertExpectations(t)
}
