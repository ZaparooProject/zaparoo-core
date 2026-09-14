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

package librarysync_test

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/backup"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/decks"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/inbox"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/librarysync"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/scantest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	putInventory    = "PUT /v1/device/library/inventory/{sha256}"
	getInventory    = "GET /v1/device/library/inventory"
	deleteInventory = "DELETE /v1/device/library/inventory"
	postResolve     = "POST /v1/device/library/resolve"
)

type syncFixture struct {
	ctx        context.Context
	newClient  librarysync.ClientFactory
	cfg        *config.Instance
	db         *database.Database
	online     *fakeOnline
	svc        *librarysync.Service
	now        atomic.Int64
	heartbeats atomic.Int32
}

func nesPath(name string) string {
	return filepath.ToSlash(filepath.Join("roms", "NES", name))
}

// newSyncFixture indexes the given NES files, links the device to a fake
// account and turns Library sync on. Tests using it cannot run in parallel:
// device credentials are process-wide.
func newSyncFixture(t *testing.T, paths ...string) *syncFixture {
	t.Helper()
	return newSyncFixtureWithPace(t, time.Millisecond, paths...)
}

// newSyncFixtureWithPace is newSyncFixture with an explicit resolve pace, for
// tests that need a page boundary to be observable.
func newSyncFixtureWithPace(t *testing.T, pace time.Duration, paths ...string) *syncFixture {
	t.Helper()
	db, cleanup := testhelpers.NewTestDatabase(t)
	t.Cleanup(cleanup)
	if len(paths) > 0 {
		scantest.IndexMediaPaths(t, db.MediaDB, "NES", paths...)
	}
	_, err := db.MediaDB.BumpIndexGeneration()
	require.NoError(t, err)

	cfg, err := testhelpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)
	f := &syncFixture{ctx: context.Background(), cfg: cfg, db: db, online: newFakeOnline(t)}
	f.now.Store(time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC).Unix())
	require.NoError(t, cfg.SetOnlineBaseURL(f.online.server.URL))
	cfg.SetLibrarySync(true)
	config.SetAuthCfgForTesting(map[string]config.CredentialEntry{
		config.RemoteAuthLookupURL(f.online.server.URL): {Bearer: "library-token"},
	})
	t.Cleanup(config.ClearAuthCfgForTesting)

	platform := mocks.NewMockPlatform()
	platform.On("ID").Return("test-platform")
	manager := backup.NewManager(cfg, platform, db).
		WithRateLimitWaits(time.Millisecond, time.Millisecond, 5*time.Millisecond)
	f.newClient = manager.NewOnlineClient
	db.DeckTags = &syncDeckTags{
		db: db,
		deps: &decks.ResolveDeps{
			MediaDB: db.MediaDB, UserDB: db.UserDB, Cfg: cfg,
			LaunchersForSystem: func(string) []platforms.Launcher { return nil },
		},
	}
	f.svc = librarysync.New(&librarysync.Options{
		Config:      cfg,
		DB:          db,
		NewClient:   manager.NewOnlineClient,
		Now:         func() time.Time { return time.Unix(f.now.Load(), 0).UTC() },
		ResolvePace: pace,
		SendHeartbeat: func(context.Context) error {
			f.heartbeats.Add(1)
			return nil
		},
	})
	return f
}

func (f *syncFixture) advance(d time.Duration) {
	f.now.Add(int64(d / time.Second))
}

func (f *syncFixture) bumpGeneration(t *testing.T) int64 {
	t.Helper()
	generation, err := f.db.MediaDB.BumpIndexGeneration()
	require.NoError(t, err)
	return generation
}

func (f *syncFixture) heldOrdinals() []uint32 {
	f.online.mu.Lock()
	defer f.online.mu.Unlock()
	if f.online.held == nil {
		return nil
	}
	held := append([]uint32(nil), f.online.held.ordinals...)
	sort.Slice(held, func(i, j int) bool { return held[i] < held[j] })
	return held
}

func TestSyncInventory_FirstSyncUploadsEveryGame(t *testing.T) {
	f := newSyncFixture(t,
		nesPath("Metroid (USA).nes"),
		nesPath(filepath.Join("copies", "Metroid (USA).nes")),
		nesPath("Zelda (USA).nes"),
	)

	result, err := f.svc.SyncInventory(f.ctx, false)
	require.NoError(t, err)
	assert.Equal(t, librarysync.InventoryUploaded, result.Outcome)
	assert.Equal(t, 2, result.ItemCount, "two copies of one game are one entry")
	assert.Equal(t, 2, result.Resolved)
	assert.Equal(t, 1, f.online.count(postResolve), "one page resolves in one request")
	assert.Len(t, f.heldOrdinals(), 2)

	sent := f.online.resolvedBatches()[0]
	for i := range sent {
		assert.Equal(t, "NES", sent[i].CanonicalSystemID)
		assert.NotEmpty(t, sent[i].DisplayName, "the display name travels as title evidence")
	}

	state, err := f.db.MediaDB.GetLibraryInventoryState(f.ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), state.Generation)
	assert.Equal(t, 2, state.ItemCount)
	assert.Len(t, state.SHA256, 64)
}

func TestSyncInventory_UnchangedGenerationSkipsThenConfirms(t *testing.T) {
	f := newSyncFixture(t, nesPath("Metroid (USA).nes"))
	_, err := f.svc.SyncInventory(f.ctx, false)
	require.NoError(t, err)
	f.online.resetCalls()

	result, err := f.svc.SyncInventory(f.ctx, false)
	require.NoError(t, err)
	assert.Equal(t, librarysync.InventorySkipped, result.Outcome)
	assert.Zero(t, f.online.count(getInventory), "a committed generation is trusted for an hour")

	f.advance(time.Hour)
	result, err = f.svc.SyncInventory(f.ctx, false)
	require.NoError(t, err)
	assert.Equal(t, librarysync.InventoryConfirmed, result.Outcome)
	assert.Equal(t, 1, f.online.count(getInventory))
	assert.Zero(t, f.online.count(putInventory))
	assert.Zero(t, f.online.count(postResolve))
}

func TestSyncInventory_NewGenerationReusesCachedOrdinals(t *testing.T) {
	f := newSyncFixture(t, nesPath("Metroid (USA).nes"), nesPath("Zelda (USA).nes"))
	_, err := f.svc.SyncInventory(f.ctx, false)
	require.NoError(t, err)
	before := f.heldOrdinals()
	f.online.resetCalls()

	scantest.IndexMediaPaths(t, f.db.MediaDB, "NES",
		nesPath("Metroid (USA).nes"), nesPath("Zelda (USA).nes"), nesPath("Kid Icarus (USA).nes"))
	f.bumpGeneration(t)

	result, err := f.svc.SyncInventory(f.ctx, false)
	require.NoError(t, err)
	assert.Equal(t, librarysync.InventoryUploaded, result.Outcome)
	assert.Equal(t, 3, result.ItemCount)
	assert.Equal(t, 1, result.Resolved, "only the new game is resolved")
	batches := f.online.resolvedBatches()
	require.Len(t, batches, 1)
	assert.Len(t, batches[0], 1)
	assert.Subset(t, f.heldOrdinals(), before)
}

func TestSyncInventory_RemovedGameLeavesInventoryAndCache(t *testing.T) {
	f := newSyncFixture(t, nesPath("Metroid (USA).nes"), nesPath("Zelda (USA).nes"))
	_, err := f.svc.SyncInventory(f.ctx, false)
	require.NoError(t, err)

	scantest.IndexMediaPaths(t, f.db.MediaDB, "NES", nesPath("Metroid (USA).nes"))
	f.bumpGeneration(t)
	result, err := f.svc.SyncInventory(f.ctx, false)
	require.NoError(t, err)
	assert.Equal(t, 1, result.ItemCount)
	assert.Len(t, f.heldOrdinals(), 1)

	// The removed game's cached answer was not met by this walk, so it was
	// pruned: bringing it back resolves it again.
	f.online.resetCalls()
	scantest.IndexMediaPaths(t, f.db.MediaDB, "NES", nesPath("Metroid (USA).nes"), nesPath("Zelda (USA).nes"))
	f.bumpGeneration(t)
	result, err = f.svc.SyncInventory(f.ctx, false)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Resolved)
}

func TestSyncInventory_AccountLostInventoryIsUploadedAgain(t *testing.T) {
	f := newSyncFixture(t, nesPath("Metroid (USA).nes"))
	_, err := f.svc.SyncInventory(f.ctx, false)
	require.NoError(t, err)

	f.online.mu.Lock()
	f.online.held = nil
	f.online.mu.Unlock()
	f.online.resetCalls()

	result, err := f.svc.SyncInventory(f.ctx, true)
	require.NoError(t, err)
	assert.Equal(t, librarysync.InventoryUploaded, result.Outcome)
	assert.Equal(t, 1, f.online.count(getInventory))
	assert.Equal(t, 1, f.online.count(putInventory))
	assert.Zero(t, f.online.count(postResolve), "cached ordinals are still valid")
}

func TestSyncInventory_UnknownOrdinalResolvesAgain(t *testing.T) {
	f := newSyncFixture(t, nesPath("Metroid (USA).nes"))
	_, err := f.svc.SyncInventory(f.ctx, false)
	require.NoError(t, err)

	// The server no longer recognizes anything it issued before.
	f.online.mu.Lock()
	f.online.ordinals = make(map[string]uint32)
	f.online.issued = make(map[uint32]bool)
	f.online.mu.Unlock()
	f.online.resetCalls()
	f.bumpGeneration(t)

	result, err := f.svc.SyncInventory(f.ctx, false)
	require.NoError(t, err)
	assert.Equal(t, librarysync.InventoryUploaded, result.Outcome)
	assert.Equal(t, 2, f.online.count(putInventory))
	assert.Equal(t, 1, f.online.count(postResolve))
}

func TestSyncInventory_SupersededReplacesHeldInventory(t *testing.T) {
	f := newSyncFixture(t, nesPath("Metroid (USA).nes"))
	f.online.mu.Lock()
	f.online.held = &fakeInventory{sha: "old", generation: 500}
	f.online.mu.Unlock()

	result, err := f.svc.SyncInventory(f.ctx, false)
	require.NoError(t, err)
	assert.Equal(t, librarysync.InventoryUploaded, result.Outcome)
	assert.Equal(t, 2, f.online.count(putInventory))
	assert.Equal(t, 1, f.online.count(deleteInventory))
	assert.Len(t, f.heldOrdinals(), 1)
}

func TestSyncInventory_TooLargeIsRecordedOncePerGeneration(t *testing.T) {
	f := newSyncFixture(t, nesPath("Metroid (USA).nes"))
	f.online.setPutError("inventory_too_large")

	result, err := f.svc.SyncInventory(f.ctx, false)
	require.NoError(t, err)
	assert.Equal(t, librarysync.InventoryTooLarge, result.Outcome)
	f.online.resetCalls()

	result, err = f.svc.SyncInventory(f.ctx, true)
	require.NoError(t, err)
	assert.Equal(t, librarysync.InventoryTooLarge, result.Outcome)
	assert.Zero(t, f.online.count(putInventory), "not built again until the index changes")

	f.online.setPutError("")
	f.bumpGeneration(t)
	result, err = f.svc.SyncInventory(f.ctx, false)
	require.NoError(t, err)
	assert.Equal(t, librarysync.InventoryUploaded, result.Outcome)
}

func TestSyncInventory_RejectedIdentityRetriedAfterAWeek(t *testing.T) {
	f := newSyncFixture(t, nesPath("Metroid (USA).nes"))
	identity, found, err := database.LookupMediaIdentity(f.ctx, f.db.MediaDB, "NES", nesPath("Metroid (USA).nes"))
	require.NoError(t, err)
	require.True(t, found)
	f.online.setRejected(identity.ObservationFingerprint, "incompatible_identity")

	result, err := f.svc.SyncInventory(f.ctx, false)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Rejected)
	assert.Zero(t, result.ItemCount)

	f.online.resetCalls()
	f.bumpGeneration(t)
	result, err = f.svc.SyncInventory(f.ctx, false)
	require.NoError(t, err)
	assert.Zero(t, f.online.count(postResolve), "a fresh rejection is trusted")
	assert.Zero(t, result.ItemCount)

	f.online.setRejected(identity.ObservationFingerprint, "")
	f.advance(8 * 24 * time.Hour)
	f.bumpGeneration(t)
	result, err = f.svc.SyncInventory(f.ctx, false)
	require.NoError(t, err)
	assert.Equal(t, 1, f.online.count(postResolve))
	assert.Equal(t, 1, result.ItemCount)
}

func TestSyncInventory_WaitsOutRateLimit(t *testing.T) {
	f := newSyncFixture(t, nesPath("Metroid (USA).nes"))
	f.online.setLimitFirst(2)

	result, err := f.svc.SyncInventory(f.ctx, false)
	require.NoError(t, err)
	assert.Equal(t, librarysync.InventoryUploaded, result.Outcome)
	assert.Equal(t, 3, f.online.count(postResolve))
}

func TestSyncInventory_IdleStates(t *testing.T) {
	f := newSyncFixture(t, nesPath("Metroid (USA).nes"))

	f.cfg.SetLibrarySync(false)
	_, err := f.svc.SyncInventory(f.ctx, false)
	require.ErrorIs(t, err, librarysync.ErrDisabled)
	assert.True(t, librarysync.IsIdleError(err))

	f.cfg.SetLibrarySync(true)
	config.SetAuthCfgForTesting(map[string]config.CredentialEntry{})
	_, err = f.svc.SyncInventory(f.ctx, false)
	require.Error(t, err)
	assert.True(t, librarysync.IsIdleError(err), "an unlinked device has nothing to sync")

	config.SetAuthCfgForTesting(map[string]config.CredentialEntry{
		config.RemoteAuthLookupURL(f.online.server.URL): {Bearer: "library-token"},
	})
	require.NoError(t, f.db.MediaDB.SetIndexingStatus("running"))
	_, err = f.svc.SyncInventory(f.ctx, false)
	require.ErrorIs(t, err, librarysync.ErrNotSettled)
	assert.Zero(t, f.online.count(postResolve))
}

// TestSyncInventory_BusyDatabaseDefers covers a batch transaction held open
// without the indexing status being set, which the settled check cannot see.
// The ordinal cache write refuses to join it, and that refusal must read as
// "come back later" rather than a failure that backs the scheduler off for an
// hour.
func TestSyncInventory_BusyDatabaseDefers(t *testing.T) {
	f := newSyncFixture(t, nesPath("Metroid (USA).nes"))

	require.NoError(t, f.db.MediaDB.BeginTransaction(true))
	t.Cleanup(func() { _ = f.db.MediaDB.CommitTransaction() })

	_, err := f.svc.SyncInventory(f.ctx, false)
	require.Error(t, err)
	assert.True(t, librarysync.IsIdleError(err),
		"a busy media database defers a pass instead of failing it: %v", err)
}

func TestSyncInventory_RelinkChecksAccountAgain(t *testing.T) {
	f := newSyncFixture(t, nesPath("Metroid (USA).nes"))
	_, err := f.svc.SyncInventory(f.ctx, false)
	require.NoError(t, err)

	// A new link is a new device on the account, holding nothing yet.
	f.online.mu.Lock()
	f.online.held = nil
	f.online.mu.Unlock()
	f.online.resetCalls()
	config.SetAuthCfgForTesting(map[string]config.CredentialEntry{
		config.RemoteAuthLookupURL(f.online.server.URL): {Bearer: "library-token-2"},
	})
	f.online.mu.Lock()
	f.online.token = "library-token-2"
	f.online.mu.Unlock()

	result, err := f.svc.SyncInventory(f.ctx, false)
	require.NoError(t, err)
	assert.Equal(t, librarysync.InventoryUploaded, result.Outcome, "the unchanged index is not trusted across links")
	assert.Equal(t, 1, f.online.count(getInventory))
	assert.Zero(t, f.online.count(postResolve))
}

func TestSyncInventory_EndpointChangeClearsOrdinalCache(t *testing.T) {
	f := newSyncFixture(t, nesPath("Metroid (USA).nes"))
	_, err := f.svc.SyncInventory(f.ctx, false)
	require.NoError(t, err)

	other := newFakeOnline(t)
	require.NoError(t, f.cfg.SetOnlineBaseURL(other.server.URL))
	config.SetAuthCfgForTesting(map[string]config.CredentialEntry{
		config.RemoteAuthLookupURL(other.server.URL): {Bearer: "library-token"},
	})

	result, err := f.svc.SyncInventory(f.ctx, false)
	require.NoError(t, err)
	assert.Equal(t, librarysync.InventoryUploaded, result.Outcome)
	assert.Equal(t, 1, other.count(postResolve), "ordinals from another server are never reused")
	assert.Equal(t, 1, other.count(putInventory))
}

func TestApplySettingAndDeleteInventory(t *testing.T) {
	f := newSyncFixture(t, nesPath("Metroid (USA).nes"))

	changed, err := f.svc.ApplySetting(f.ctx)
	require.NoError(t, err)
	assert.True(t, changed, "the first pass records sync as on")
	assert.Equal(t, int32(1), f.heartbeats.Load())
	changed, err = f.svc.ApplySetting(f.ctx)
	require.NoError(t, err)
	assert.False(t, changed)

	_, err = f.svc.SyncInventory(f.ctx, false)
	require.NoError(t, err)
	require.NotEmpty(t, f.heldOrdinals())

	deleted, err := f.svc.DeleteInventory(f.ctx)
	require.NoError(t, err)
	assert.False(t, deleted, "nothing is deleted while sync is on")

	f.cfg.SetLibrarySync(false)
	changed, err = f.svc.ApplySetting(f.ctx)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, int32(2), f.heartbeats.Load(), "the account hears that sync is off")

	deleted, err = f.svc.DeleteInventory(f.ctx)
	require.NoError(t, err)
	assert.True(t, deleted)
	assert.Nil(t, f.heldOrdinals())
	assert.Equal(t, 1, f.online.count(deleteInventory))

	deleted, err = f.svc.DeleteInventory(f.ctx)
	require.NoError(t, err)
	assert.False(t, deleted, "the delete runs once")

	// Turning sync back on uploads again without re-resolving.
	f.online.resetCalls()
	f.cfg.SetLibrarySync(true)
	_, err = f.svc.ApplySetting(f.ctx)
	require.NoError(t, err)
	result, err := f.svc.SyncInventory(f.ctx, false)
	require.NoError(t, err)
	assert.Equal(t, librarysync.InventoryUploaded, result.Outcome)
	assert.Zero(t, f.online.count(postResolve))
}

func TestDeleteInventory_UnlinkedDeviceClearsMarker(t *testing.T) {
	f := newSyncFixture(t, nesPath("Metroid (USA).nes"))
	_, err := f.svc.ApplySetting(f.ctx)
	require.NoError(t, err)
	f.cfg.SetLibrarySync(false)
	_, err = f.svc.ApplySetting(f.ctx)
	require.NoError(t, err)

	config.SetAuthCfgForTesting(map[string]config.CredentialEntry{})
	deleted, err := f.svc.DeleteInventory(f.ctx)
	require.NoError(t, err)
	assert.True(t, deleted, "an unlinked device's inventory is already gone")
	assert.Zero(t, f.online.count(deleteInventory))
}

// TestBuildMediaIdentityMatchesPathLookup pins that the paged walk names a
// file exactly as a path lookup does, so the inventory, play history and
// state all agree on what a file is.
func TestBuildMediaIdentityMatchesPathLookup(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)
	ctx := context.Background()
	path := nesPath("Legend of Zelda, The (USA) (Rev 1).nes")
	scantest.IndexMediaPaths(t, mediaDB, "NES", path)

	page, err := mediaDB.LibraryMediaPage(ctx, 0, 10)
	require.NoError(t, err)
	require.Len(t, page, 1)
	tags, err := mediaDB.GetMediaTagsByMediaDBIDs(ctx, []int64{page[0].MediaDBID})
	require.NoError(t, err)
	built, err := database.BuildMediaIdentity("Game", page[0].SystemID, page[0].Name, page[0].Slug,
		tags[page[0].MediaDBID])
	require.NoError(t, err)

	looked, found, err := database.LookupMediaIdentity(ctx, mediaDB, "NES", path)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, looked, built)
}

// TestSyncInventory_TurningOffMidWalkStopsAsking covers the first sync of a
// library too large for one page. Turning Library sync off part way through
// must stop the walk where it is: at the account's pace a large library takes
// tens of minutes, and a user who withdraws consent should not have the rest
// of their library offered up before the pass notices.
func TestSyncInventory_TurningOffMidWalkStopsAsking(t *testing.T) {
	paths := make([]string, 0, 501)
	for i := range 501 {
		paths = append(paths, nesPath(fmt.Sprintf("Game %04d (USA).nes", i)))
	}
	f := newSyncFixtureWithPace(t, 3*time.Second, paths...)

	type outcome struct {
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		_, err := f.svc.SyncInventory(f.ctx, false)
		done <- outcome{err: err}
	}()

	require.Eventually(t, func() bool { return f.online.count(postResolve) == 1 }, 20*time.Second, 10*time.Millisecond,
		"the first page should be resolved before the setting changes")
	f.cfg.SetLibrarySync(false)

	select {
	case got := <-done:
		require.ErrorIs(t, got.err, librarysync.ErrDisabled)
	case <-time.After(20 * time.Second):
		t.Fatal("the walk kept going after Library sync was turned off")
	}
	assert.Equal(t, 1, f.online.count(postResolve), "no further page is offered to the account")
	assert.Zero(t, f.online.count(putInventory), "nothing is uploaded for a user who turned sync off")
}

// TestSyncInventory_EdgeRefusedTitleDoesNotStopTheWalk covers a title the
// filter in front of the account refuses to pass on, answering the whole
// request with its own 403 page. One such title in a library must not stop the
// inventory: the batch is split until that title is alone, it is left out, and
// everything else is uploaded.
func TestSyncInventory_EdgeRefusedTitleDoesNotStopTheWalk(t *testing.T) {
	f := newSyncFixture(t,
		nesPath("Metroid (USA).nes"),
		nesPath("Zelda (USA).nes"),
		nesPath("Kid Icarus (USA).nes"),
		nesPath("Punch-Out (USA).nes"),
	)
	f.online.setEdgeRefused("Zelda")

	result, err := f.svc.SyncInventory(f.ctx, false)
	require.NoError(t, err, "one refused title is not a failed pass")
	assert.Equal(t, librarysync.InventoryUploaded, result.Outcome)
	assert.Equal(t, 3, result.ItemCount, "the other three games still reach the account")
	assert.Equal(t, 1, result.Rejected, "the refused title counts as rejected")
	assert.Len(t, f.heldOrdinals(), 3)
	assert.Greater(t, f.online.count(postResolve), 1, "the batch was split to find it")

	// The refused title is remembered, so the next pass does not pay to find
	// it again, and the inventory stays the same.
	f.online.resetCalls()
	f.bumpGeneration(t)
	result, err = f.svc.SyncInventory(f.ctx, false)
	require.NoError(t, err)
	assert.Equal(t, 3, result.ItemCount)
	assert.Zero(t, f.online.count(postResolve), "the refusal is cached like any other rejection")

	// A week later it is offered again, in case the filter changed its mind.
	f.advance(8 * 24 * time.Hour)
	f.bumpGeneration(t)
	_, err = f.svc.SyncInventory(f.ctx, false)
	require.NoError(t, err)
	assert.Positive(t, f.online.count(postResolve), "a refused title is retried after a week")
}

// TestSyncInventory_UnansweredIdentityKeepsTheGenerationOpen covers a resolve
// response that leaves an identity out. The games that did resolve are still
// worth uploading, but the generation must not be recorded as committed, or the
// hourly confirmation would trust an inventory that is missing titles until the
// index changes.
func TestSyncInventory_UnansweredIdentityKeepsTheGenerationOpen(t *testing.T) {
	f := newSyncFixture(t,
		nesPath("Metroid (USA).nes"),
		nesPath("Zelda (USA).nes"),
		nesPath("Kid Icarus (USA).nes"),
	)
	f.online.setOmitAnswer("Zelda", true)

	result, err := f.svc.SyncInventory(f.ctx, false)
	require.NoError(t, err)
	assert.Equal(t, librarysync.InventoryUploaded, result.Outcome)
	assert.Equal(t, 2, result.ItemCount, "the two answered games are uploaded")
	assert.Equal(t, 1, result.Unanswered)

	state, err := f.db.MediaDB.GetLibraryInventoryState(f.ctx)
	require.NoError(t, err)
	assert.Zero(t, state.Generation, "an incomplete inventory does not close the generation")

	// The next pass asks again for the title that went unanswered, without the
	// index having changed, and closes the generation once it is answered.
	f.online.setOmitAnswer("Zelda", false)
	f.online.resetCalls()
	result, err = f.svc.SyncInventory(f.ctx, false)
	require.NoError(t, err)
	assert.Positive(t, f.online.count(postResolve), "the unanswered title is asked for again")
	assert.Equal(t, 3, result.ItemCount)
	assert.Zero(t, result.Unanswered)

	state, err = f.db.MediaDB.GetLibraryInventoryState(f.ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), state.Generation, "a complete inventory closes the generation")
}

func TestSyncInventory_TooLargeTellsTheUser(t *testing.T) {
	f := newSyncFixture(t, nesPath("Metroid (USA).nes"))
	notifications := make(chan models.Notification, 8)
	t.Cleanup(func() { close(notifications) })
	inboxSvc := inbox.NewService(f.db.UserDB, notifications)
	svc := librarysync.New(&librarysync.Options{
		Config: f.cfg, DB: f.db, NewClient: f.newClient, Inbox: inboxSvc,
		Now: func() time.Time { return time.Unix(f.now.Load(), 0).UTC() }, ResolvePace: time.Millisecond,
	})
	f.online.setPutError("inventory_too_large")

	result, err := svc.SyncInventory(f.ctx, false)
	require.NoError(t, err)
	assert.Equal(t, librarysync.InventoryTooLarge, result.Outcome)

	messages, err := f.db.UserDB.GetInboxMessages()
	require.NoError(t, err)
	require.NotEmpty(t, messages, "the user is told their library is too large to sync")
	found := false
	for i := range messages {
		if messages[i].Category == inbox.CategoryLibraryInventoryTooLarge {
			found = true
			assert.Contains(t, messages[i].Title, "too large")
		}
	}
	assert.True(t, found, "a message carries the library-too-large category")
}

func TestSyncInventory_TwoRefusedTitlesInOnePage(t *testing.T) {
	f := newSyncFixture(t,
		nesPath("Metroid (USA).nes"),
		nesPath("Zelda (USA).nes"),
		nesPath("Kid Icarus (USA).nes"),
		nesPath("Punch-Out (USA).nes"),
		nesPath("Excitebike (USA).nes"),
	)
	f.online.setEdgeRefused("Zelda")
	f.online.setEdgeRefused("Excitebike")

	result, err := f.svc.SyncInventory(f.ctx, false)
	require.NoError(t, err)
	assert.Equal(t, 3, result.ItemCount, "both refused titles are isolated and the rest upload")
	assert.Equal(t, 2, result.Rejected)
	assert.Zero(t, result.Unanswered, "an isolated refusal is an answer, not a gap")
}

func TestServiceEnabledFollowsTheSetting(t *testing.T) {
	f := newSyncFixture(t)
	assert.True(t, f.svc.Enabled())
	f.cfg.SetLibrarySync(false)
	assert.False(t, f.svc.Enabled())
}

// TestSyncInventory_ServerErrorIsNotTreatedAsARefusal pins that only the edge's
// own refusal is narrowed down. An error from the account itself fails the pass,
// so the scheduler backs off instead of splitting the batch against a server
// that is already struggling.
func TestSyncInventory_ServerErrorIsNotTreatedAsARefusal(t *testing.T) {
	f := newSyncFixture(t, nesPath("Metroid (USA).nes"), nesPath("Zelda (USA).nes"))
	f.online.setResolveStatus(http.StatusInternalServerError, "internal_error")

	_, err := f.svc.SyncInventory(f.ctx, false)
	require.Error(t, err)
	assert.False(t, librarysync.IsIdleError(err), "a failing account is a failure, not an idle state")
	assert.Equal(t, 1, f.online.count(postResolve), "the batch is not split against a server error")
	assert.Zero(t, f.online.count(putInventory), "nothing is committed")
}
