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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// The unbuffered ownership send followed by a confirmation round trip fences
// the reader loop: even a rejected clear has finished before assertions run.
func applySoftwareUpdate(
	t *testing.T,
	queue chan softwareTokenUpdate,
	confirmQueue chan chan error,
	update softwareTokenUpdate,
) {
	t.Helper()
	select {
	case queue <- update:
	case <-time.After(behaviorTimeout):
		t.Fatal("reader manager did not receive ownership update")
	}
	result := make(chan error, 1)
	select {
	case confirmQueue <- result:
	case <-time.After(behaviorTimeout):
		t.Fatal("reader manager did not receive synchronization request")
	}
	select {
	case err := <-result:
		require.ErrorIs(t, err, ErrNoStagedToken)
	case <-time.After(behaviorTimeout):
		t.Fatal("reader manager did not finish ownership update")
	}
}

func TestReaderManager_SoftwareTokenClearGenerations(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name           string
		replacementUID string
		exitGeneration uint64
		wantClear      bool
	}{
		{name: "current_exit", exitGeneration: 1, wantClear: true},
		{name: "different_owner", replacementUID: "new-card", exitGeneration: 1},
		{name: "same_card_new_launch", replacementUID: "card", exitGeneration: 1},
		{name: "stale_exit_generation", exitGeneration: 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := setupReaderManager(t)
			owner := &tokens.Token{UID: "card", Text: "game.nes", ReaderID: "reader"}
			applySoftwareUpdate(t, env.softwareQueue, env.confirmQueue, softwareTokenUpdate{token: owner})
			current := owner
			if tt.replacementUID != "" {
				replacement := *owner
				replacement.UID = tt.replacementUID
				current = &replacement
				applySoftwareUpdate(t, env.softwareQueue, env.confirmQueue, softwareTokenUpdate{token: current})
			}

			// Deliver the old completion only after the replacement was applied.
			// Equal token contents still represent a new launch ownership epoch.
			applySoftwareUpdate(t, env.softwareQueue, env.confirmQueue, softwareTokenUpdate{
				ownerGeneration: 1,
				exitGeneration:  tt.exitGeneration,
			})
			if tt.wantClear {
				assert.Nil(t, env.st.GetSoftwareToken())
			} else {
				assert.Same(t, current, env.st.GetSoftwareToken())
			}
		})
	}
}

func TestScanBehavior_StaleClearPreservesRemoval(t *testing.T) {
	t.Parallel()

	for _, delayedHook := range []bool{false, true} {
		name := "exit_timer"
		if delayedHook {
			name = "delayed_on_remove"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			env := setupScanBehaviorWithSoftwareQueue(t, config.ScanModeHold, 1, make(chan softwareTokenUpdate))
			queue := env.svc.LaunchSoftwareQueue
			confirmQueue := env.svc.ConfirmQueue
			applySoftwareUpdate(t, queue, confirmQueue, softwareTokenUpdate{
				token: &tokens.Token{UID: "old-card", Text: "old-game", ReaderID: testReader2ID},
			})

			hookStarted := make(chan struct{})
			releaseHook := make(chan struct{})
			var releaseOnce sync.Once
			unblockHook := func() { releaseOnce.Do(func() { close(releaseHook) }) }
			t.Cleanup(unblockHook)
			if delayedHook {
				require.NoError(t, env.cfg.LoadTOML(`[readers.scan]
mode = "hold"
on_remove = '**delay:1||**input.keyboard:{f2}||**input.keyboard:{escape}'`))
				platform, ok := env.svc.Platform.(*mocks.MockPlatform)
				require.True(t, ok)
				platform.On("KeyboardPress", "{f2}").Unset()
				platform.On("KeyboardPress", "{f2}").Run(func(_ mock.Arguments) {
					close(hookStarted)
					<-releaseHook
				}).Return(nil).Once()
				platform.On("KeyboardPress", "{escape}").Run(func(_ mock.Arguments) {
					env.keyboardCh <- "{escape}"
				}).Return(nil).Once()
			}

			env.sendGameScan("new-card", env.gamePath("new.rom"))
			env.waitForLaunch(t)
			env.waitForSoftwareTokenUID(t, "new-card")
			owner := env.st.GetSoftwareToken()
			env.sendRemoval()
			ctx, cancel := context.WithTimeout(t.Context(), behaviorTimeout)
			defer cancel()
			if delayedHook {
				select {
				case <-hookStarted:
				case <-ctx.Done():
					t.Fatal("delayed removal hook did not start")
				}
			} else {
				require.NoError(t, env.clock.BlockUntilContext(ctx, 1))
			}

			// The old exit completes only after the new owner's removal is
			// pending. Rejection must precede hook and timer cancellation.
			applySoftwareUpdate(t, queue, confirmQueue, softwareTokenUpdate{
				ownerGeneration: 1,
				exitGeneration:  3,
			})
			require.Same(t, owner, env.st.GetSoftwareToken())
			if delayedHook {
				unblockHook()
				require.Equal(t, "{escape}", env.waitForKeyboard(t))
			}
			require.NoError(t, env.clock.BlockUntilContext(ctx, 1))
			env.clock.Advance(time.Second)
			env.waitForStop(t)
			require.Eventually(t, func() bool {
				return env.st.GetSoftwareToken() == nil
			}, behaviorTimeout, time.Millisecond, "current exit did not clear ownership")
		})
	}
}

func TestTimedExit_ClearKeepsCapturedGenerationsWhilePlaylistBlocked(t *testing.T) {
	t.Parallel()
	fx := newTimedExitStopFixture(t, nil)
	t.Cleanup(fx.st.StopService)
	fx.svc.PlaylistQueue = make(chan *playlists.Playlist)
	clock := clockwork.NewFakeClock()
	var exitGeneration atomic.Uint64
	timedExit(fx.svc, clock, nil, &exitGeneration, &fx.owner, 7)
	capturedGeneration := exitGeneration.Load()
	clock.Advance(time.Millisecond)

	select {
	case <-fx.stopCalled:
	case <-time.After(behaviorTimeout):
		t.Fatal("exit did not stop the launcher")
	}
	// Playlist cleanup cannot finish until the receive below. Change ownership
	// and invalidate the timer while its completion is still blocked.
	replacement := fx.owner
	replacement.UID = "new-card"
	fx.st.SetSoftwareToken(&replacement)
	cancelTimedExit(nil, &exitGeneration)
	select {
	case pls := <-fx.svc.PlaylistQueue:
		require.Nil(t, pls)
	case <-time.After(behaviorTimeout):
		t.Fatal("exit did not clear the playlist")
	}
	select {
	case update := <-fx.svc.LaunchSoftwareQueue:
		assert.Nil(t, update.token)
		assert.Equal(t, uint64(7), update.ownerGeneration)
		assert.Equal(t, capturedGeneration, update.exitGeneration)
		assert.NotEqual(t, exitGeneration.Load(), update.exitGeneration)
	case <-time.After(behaviorTimeout):
		t.Fatal("exit did not publish its conditional owner clear")
	}
}
