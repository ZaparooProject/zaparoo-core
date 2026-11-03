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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/idle"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
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

// Each daily tick goes through the idle scheduler and runs retention once.
func TestScheduleUserDBRetention_RunsThroughIdleScheduler(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	clock := clockwork.NewFakeClock()
	idleSched := idle.NewWithClock(clock)
	var wg sync.WaitGroup
	runs := make(chan struct{}, 4)
	scheduleUserDBRetention(ctx, clock, idleSched, &wg, func(context.Context) {
		runs <- struct{}{}
	})

	for day := 1; day <= 2; day++ {
		require.NoError(t, clock.BlockUntilContext(ctx, 1))
		clock.Advance(userDBRetentionInterval)
		select {
		case <-runs:
		case <-time.After(5 * time.Second):
			require.FailNow(t, "retention did not run", "day %d", day)
		}
		idleSched.Wait()
	}

	cancel()
	wg.Wait()
	assert.Empty(t, runs)
}

// A tick that lands while the previous run is still waiting or working is
// dropped rather than queueing a second run behind it.
func TestUserDBRetentionTrigger_SkipsWhileRunning(t *testing.T) {
	t.Parallel()

	clock := clockwork.NewFakeClock()
	idleSched := idle.NewWithClock(clock)
	clock.Advance(userDBMaintenanceQuietWindow)
	release := make(chan struct{})
	var runs atomic.Int32
	trigger := &userDBRetentionTrigger{idle: idleSched, run: func(context.Context) {
		runs.Add(1)
		<-release
	}}

	trigger.fire(context.Background())
	trigger.fire(context.Background())
	close(release)
	idleSched.Wait()
	assert.Equal(t, int32(1), runs.Load())

	trigger.fire(context.Background())
	idleSched.Wait()
	assert.Equal(t, int32(2), runs.Load(), "a run is allowed again once the last one finished")
}

func TestStartUserDBRetentionScheduler_StopsWithContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var wg sync.WaitGroup
	startUserDBRetentionScheduler(ctx, &config.Instance{}, mocks.NewMockPlatform(),
		&database.Database{}, idle.New(), &wg)
	wg.Wait()
}

// One retention pass prunes ZapLink hosts, both histories and the inbox.
func TestRunUserDBRetention_RunsEveryPrune(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	pl.On("Settings").Return(platforms.Settings{DataDir: t.TempDir()}).Maybe()
	pl.On("ID").Return("test-platform").Maybe()
	cfg := &config.Instance{}
	cfg.SetPlaytimeRetention(14)
	mockUserDB := &testhelpers.MockUserDBI{}
	db := &database.Database{UserDB: mockUserDB}
	mockUserDB.On("PruneExpiredZapLinkHosts", zapLinkHostExpiration).Return(int64(0), nil).Once()
	mockUserDB.On("CleanupHistory", 30).Return(int64(0), nil).Once()
	mockUserDB.On("CleanupMediaHistory", 14, false).Return(int64(0), nil).Once()
	mockUserDB.On("PruneInboxMessages", inboxMessageMaxAge, database.InboxMessageLimit).
		Return(int64(0), nil).Once()

	runUserDBRetention(context.Background(), cfg, pl, db)

	mockUserDB.AssertExpectations(t)
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
