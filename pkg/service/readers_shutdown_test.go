//go:build !deadlock

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

// These tests run in a synctest bubble, which cannot share a process with the
// deadlock build: its lock watcher keeps one global timer, and a bubble that
// creates it leaves every later lock outside the bubble panicking.

import (
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
)

// Shutdown closes the launch queue once the background group is done, so an
// exit still sending on it must hold the group open until the send finishes.
func TestTimedExit_HoldsBackgroundGroupUntilSent(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		fx := newTimedExitStopFixture(t, nil)
		fx.svc.BackgroundWG = &sync.WaitGroup{}
		fx.svc.LaunchSoftwareQueue = make(chan softwareTokenUpdate)
		clock := clockwork.NewFakeClock()
		var exitGeneration atomic.Uint64
		timedExit(fx.svc, clock, nil, &exitGeneration, &fx.owner, 7)
		clock.Advance(time.Millisecond)

		waitDone := make(chan struct{})
		go func() {
			fx.svc.BackgroundWG.Wait()
			close(waitDone)
		}()
		synctest.Wait()
		select {
		case <-waitDone:
			t.Fatal("background group finished while the exit was still sending")
		default:
		}

		update := <-fx.svc.LaunchSoftwareQueue
		assert.Nil(t, update.token)
		synctest.Wait()
		select {
		case <-waitDone:
		default:
			t.Fatal("background group still open after the exit sent")
		}
	})
}
