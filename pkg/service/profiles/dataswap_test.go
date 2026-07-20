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

package profiles

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSwapper records ApplyProfile calls. The device-owned item proves
// only profile-owned items are passed through.
type fakeSwapper struct {
	err     error
	applies []platforms.ProfileRef
	items   [][]string
	mu      syncutil.Mutex
}

func (*fakeSwapper) ProfileItems() []platforms.ProfileItem {
	return []platforms.ProfileItem{
		{ID: "saves", Label: "Save files", Owner: platforms.ProfileItemOwnerProfile},
		{ID: "savestates", Label: "Save states", Owner: platforms.ProfileItemOwnerProfile},
		{ID: "display", Label: "Display settings", Owner: platforms.ProfileItemOwnerDevice},
	}
}

func (f *fakeSwapper) ApplyProfile(ref platforms.ProfileRef, enabledItems []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.applies = append(f.applies, ref)
	f.items = append(f.items, enabledItems)
	return f.err
}

func (f *fakeSwapper) applyCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.applies)
}

func (f *fakeSwapper) lastApply() (ref platforms.ProfileRef, items []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.applies) == 0 {
		return platforms.ProfileRef{}, nil
	}
	return f.applies[len(f.applies)-1], f.items[len(f.items)-1]
}

// stubBroker returns a test-fed notification channel.
type stubBroker struct {
	ch      chan models.Notification
	methods []string
}

func (b *stubBroker) Subscribe(_ int, methods ...string) (notifChan <-chan models.Notification, id int) {
	b.methods = methods
	b.ch = make(chan models.Notification, 4)
	return b.ch, 1
}

func (*stubBroker) Unsubscribe(int) {}

type coordFixture struct {
	coord  *DataSwapCoordinator
	st     *state.State
	broker *stubBroker
	notifs chan models.Notification
}

func newTestCoordinator(t *testing.T, swapper platforms.ProfileDataSwapper) coordFixture {
	t.Helper()
	st, ns := state.NewState(nil, "boot")
	t.Cleanup(func() {
		for {
			select {
			case <-ns:
			default:
				return
			}
		}
	})

	coord := NewDataSwapCoordinator(&config.Instance{}, st, swapper)
	broker := &stubBroker{}
	notifs := make(chan models.Notification, 16)
	coord.Start(broker, notifs)
	t.Cleanup(coord.Stop)
	return coordFixture{coord: coord, st: st, broker: broker, notifs: notifs}
}

func waitForNotification(t *testing.T, notifs <-chan models.Notification, method string) models.Notification {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case n := <-notifs:
			if n.Method == method {
				return n
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s notification", method)
		}
	}
}

func TestDataSwap_SwitchApplies(t *testing.T) {
	t.Parallel()
	swapper := &fakeSwapper{}
	fix := newTestCoordinator(t, swapper)

	fix.coord.RequestSwitch(platforms.ProfileRef{ID: "profile-1", Name: "Kid A"})

	require.Eventually(t, func() bool { return swapper.applyCount() == 1 },
		2*time.Second, 5*time.Millisecond)
	ref, items := swapper.lastApply()
	assert.Equal(t, "profile-1", ref.ID)
	assert.Equal(t, "Kid A", ref.Name)
	// Only profile-owned items are applied; device-owned items never swap.
	assert.Equal(t, []string{"saves", "savestates"}, items)

	n := waitForNotification(t, fix.notifs, models.NotificationProfilesData)
	assert.Contains(t, string(n.Params), models.ProfilesDataApplied)
}

func TestDataSwap_WaitsForRestoreRollback(t *testing.T) {
	t.Parallel()
	swapper := &fakeSwapper{}
	fix := newTestCoordinator(t, swapper)
	finishRestore, err := fix.st.BeginRestoreGate()
	require.NoError(t, err)
	requested := make(chan struct{})
	go func() {
		fix.coord.RequestSwitch(platforms.ProfileRef{ID: "profile-1", Name: "Kid A"})
		close(requested)
	}()

	time.Sleep(50 * time.Millisecond)
	assert.Zero(t, swapper.applyCount())
	finishRestore(false)
	require.Eventually(t, func() bool { return swapper.applyCount() == 1 },
		2*time.Second, 5*time.Millisecond)
	select {
	case <-requested:
	case <-time.After(2 * time.Second):
		t.Fatal("profile switch request did not complete after restore rollback")
	}
}

func TestDataSwap_DeferredWhileMediaRunning(t *testing.T) {
	t.Parallel()
	swapper := &fakeSwapper{}
	fix := newTestCoordinator(t, swapper)

	fix.st.SetActiveMedia(&models.ActiveMedia{SystemID: "SNES", Path: "game.sfc", Name: "Game"})
	fix.coord.RequestSwitch(platforms.ProfileRef{ID: "profile-1", Name: "Kid A"})

	// Nothing applies while media is running.
	n := waitForNotification(t, fix.notifs, models.NotificationProfilesData)
	assert.Contains(t, string(n.Params), models.ProfilesDataDeferred)
	assert.Equal(t, 0, swapper.applyCount())

	// Media stops: the deferred swap applies.
	fix.st.SetActiveMedia(nil)
	fix.broker.ch <- models.Notification{Method: models.NotificationStopped}

	require.Eventually(t, func() bool { return swapper.applyCount() == 1 },
		2*time.Second, 5*time.Millisecond)
	ref, _ := swapper.lastApply()
	assert.Equal(t, "profile-1", ref.ID)
}

func TestDataSwap_DeferredSwitchesCoalesce(t *testing.T) {
	t.Parallel()
	swapper := &fakeSwapper{}
	fix := newTestCoordinator(t, swapper)

	fix.st.SetActiveMedia(&models.ActiveMedia{SystemID: "SNES", Path: "game.sfc", Name: "Game"})
	fix.coord.RequestSwitch(platforms.ProfileRef{ID: "profile-1", Name: "Kid A"})
	fix.coord.RequestSwitch(platforms.ProfileRef{ID: "profile-2", Name: "Kid B"})
	fix.coord.RequestSwitch(platforms.ProfileRef{})

	fix.st.SetActiveMedia(nil)
	fix.broker.ch <- models.Notification{Method: models.NotificationStopped}

	// Only the most recent target (shared) is applied.
	require.Eventually(t, func() bool { return swapper.applyCount() == 1 },
		2*time.Second, 5*time.Millisecond)
	ref, _ := swapper.lastApply()
	assert.Empty(t, ref.ID)

	// A later media stop with nothing pending applies nothing.
	fix.broker.ch <- models.Notification{Method: models.NotificationStopped}
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 1, swapper.applyCount())
}

func TestDataSwap_StaleDeferredTargetCannotDisplaceNewerSwitch(t *testing.T) {
	t.Parallel()
	swapper := &fakeSwapper{}
	fix := newTestCoordinator(t, swapper)

	fix.st.SetActiveMedia(&models.ActiveMedia{SystemID: "SNES", Path: "game.sfc", Name: "Game"})
	fix.coord.RequestSwitch(platforms.ProfileRef{ID: "profile-1", Name: "Kid A"})

	// Simulate media.stopped extracting A immediately before a concurrent
	// direct switch to B. Enqueueing extracted A afterwards must be rejected.
	stale := fix.coord.takePending()
	fix.st.SetActiveMedia(nil)
	fix.coord.RequestSwitch(platforms.ProfileRef{ID: "profile-2", Name: "Kid B"})
	fix.coord.enqueuePending(stale)

	require.Eventually(t, func() bool { return swapper.applyCount() == 1 },
		2*time.Second, 5*time.Millisecond)
	ref, _ := swapper.lastApply()
	assert.Equal(t, "profile-2", ref.ID)
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 1, swapper.applyCount())
}

func TestDataSwap_DisabledConvergesToShared(t *testing.T) {
	t.Parallel()
	swapper := &fakeSwapper{}
	st, ns := state.NewState(nil, "boot")
	t.Cleanup(func() {
		for {
			select {
			case <-ns:
			default:
				return
			}
		}
	})
	cfg := &config.Instance{}
	cfg.SetProfilesSwapData(false)

	coord := NewDataSwapCoordinator(cfg, st, swapper)
	coord.Start(&stubBroker{}, make(chan models.Notification, 16))
	t.Cleanup(coord.Stop)

	coord.RequestSwitch(platforms.ProfileRef{ID: "profile-1", Name: "Kid A"})

	require.Eventually(t, func() bool { return swapper.applyCount() == 1 },
		2*time.Second, 5*time.Millisecond)
	ref, _ := swapper.lastApply()
	assert.Empty(t, ref.ID, "swap_data off maps every target to the shared profile")
}

func TestDataSwap_FailureAndUnavailableNotify(t *testing.T) {
	t.Parallel()

	swapper := &fakeSwapper{err: errors.New("mount failed")}
	fix := newTestCoordinator(t, swapper)
	fix.coord.RequestSwitch(platforms.ProfileRef{ID: "profile-1"})
	n := waitForNotification(t, fix.notifs, models.NotificationProfilesData)
	assert.Contains(t, string(n.Params), models.ProfilesDataFailed)

	swapper2 := &fakeSwapper{
		err: fmt.Errorf("saves are network-mounted: %w", platforms.ErrProfileDataUnavailable),
	}
	fix2 := newTestCoordinator(t, swapper2)
	fix2.coord.RequestSwitch(platforms.ProfileRef{ID: "profile-1"})
	n2 := waitForNotification(t, fix2.notifs, models.NotificationProfilesData)
	assert.Contains(t, string(n2.Params), models.ProfilesDataUnavailable)
}

func TestDataSwap_ReconcileAppliesActiveProfileQuietly(t *testing.T) {
	t.Parallel()
	swapper := &fakeSwapper{}
	fix := newTestCoordinator(t, swapper)

	fix.st.SetActiveProfile(&models.ActiveProfile{ProfileID: "profile-1", Name: "Kid A"})
	fix.coord.Reconcile()

	require.Eventually(t, func() bool { return swapper.applyCount() == 1 },
		2*time.Second, 5*time.Millisecond)
	ref, _ := swapper.lastApply()
	assert.Equal(t, "profile-1", ref.ID)

	// Quiet on success: no profiles.data notification for reconciles.
	select {
	case n := <-fix.notifs:
		if n.Method == models.NotificationProfilesData {
			t.Fatal("unexpected profiles.data notification for quiet reconcile")
		}
	case <-time.After(50 * time.Millisecond):
	}
}

func TestDataSwap_NilSwapperInert(t *testing.T) {
	t.Parallel()
	st, ns := state.NewState(nil, "boot")
	t.Cleanup(func() {
		for {
			select {
			case <-ns:
			default:
				return
			}
		}
	})

	coord := NewDataSwapCoordinator(&config.Instance{}, st, nil)
	coord.Start(&stubBroker{}, nil)
	coord.RequestSwitch(platforms.ProfileRef{ID: "profile-1"})
	coord.Reconcile()
	coord.Stop()
}

func TestDataSwap_SubscribesToMediaStopped(t *testing.T) {
	t.Parallel()
	swapper := &fakeSwapper{}
	fix := newTestCoordinator(t, swapper)

	// The filter must include media.stopped or deferred swaps never apply.
	assert.Contains(t, fix.broker.methods, models.NotificationStopped)
}
