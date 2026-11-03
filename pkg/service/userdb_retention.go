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
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	backupsvc "github.com/ZaparooProject/zaparoo-core/v2/pkg/service/backup"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/idle"
	"github.com/jonboulle/clockwork"
)

const (
	userDBRetentionInterval      = 24 * time.Hour
	userDBMaintenanceQuietWindow = 5 * time.Second
	userDBMaintenanceMaxWait     = 300 * time.Second
)

// runUserDBRetention prunes everything in the user database that is kept for
// a limited time. Whether unsynced play history is protected is decided on
// each run, because the device can be linked or unlinked in between.
func runUserDBRetention(ctx context.Context, cfg *config.Instance, pl platforms.Platform, db *database.Database) {
	runInSequence(ctx,
		func(context.Context) {
			pruneExpiredZapLinkHosts(db)
		},
		func(ctx context.Context) {
			remoteLinked := backupsvc.NewManager(cfg, pl, db).Status().Remote.Linked
			cleanupHistoryRetention(ctx, cfg, db, cfg.PlaytimeSyncEnabled() && remoteLinked)
		},
		func(context.Context) {
			pruneInboxMessages(db)
		},
	)
}

// startUserDBRetentionScheduler repeats retention every
// userDBRetentionInterval after the boot run, each time waiting for an idle
// window like the boot run does.
func startUserDBRetentionScheduler(
	ctx context.Context,
	cfg *config.Instance,
	pl platforms.Platform,
	db *database.Database,
	idleSched *idle.Scheduler,
	wg *sync.WaitGroup,
) {
	var running atomic.Bool
	schedule := func(ctx context.Context) {
		if !running.CompareAndSwap(false, true) {
			return
		}
		idleSched.Schedule(ctx, "userdb-retention",
			userDBMaintenanceQuietWindow, userDBMaintenanceMaxWait,
			func(ctx context.Context) {
				defer running.Store(false)
				runUserDBRetention(ctx, cfg, pl, db)
			},
		)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		repeatUserDBRetention(ctx, clockwork.NewRealClock(), schedule)
	}()
}

func repeatUserDBRetention(ctx context.Context, clock clockwork.Clock, schedule func(context.Context)) {
	ticker := clock.NewTicker(userDBRetentionInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.Chan():
			schedule(ctx)
		case <-ctx.Done():
			return
		}
	}
}
