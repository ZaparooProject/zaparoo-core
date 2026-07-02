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
	"errors"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/methods"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	inboxservice "github.com/ZaparooProject/zaparoo-core/v2/pkg/service/inbox"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestStartReturnsErrorWhenAPIPortIsOccupied(t *testing.T) {
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { require.NoError(t, listener.Close()) }()

	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	require.True(t, ok)

	testRoot := t.TempDir()
	settings := platforms.Settings{
		ConfigDir: testRoot,
		DataDir:   testRoot,
		LogDir:    testRoot,
		TempDir:   testRoot,
	}

	cfg, err := testhelpers.NewTestConfigWithListenAndPort(nil, testRoot, "127.0.0.1", tcpAddr.Port)
	require.NoError(t, err)
	cfg.SetAutoUpdate(false)

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("mock-platform")
	mockPlatform.On("Settings").Return(settings)
	mockPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).Return([]string{testRoot})
	mockPlatform.On("SupportedReaders", mock.AnythingOfType("*config.Instance")).Return([]readers.Reader{})
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).Return([]platforms.Launcher{})
	mockPlatform.On("ManagedByPackageManager").Return(false)
	mockPlatform.On("StartPre", cfg).Return(nil)
	mockPlatform.On("Stop").Return(nil).Maybe()

	svcResult, err := Start(mockPlatform, cfg)
	require.Nil(t, svcResult)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "api startup failed")
	assert.True(t, strings.Contains(err.Error(), "bind") || strings.Contains(err.Error(), "address already in use"))
	mockPlatform.AssertNotCalled(
		t, "StartPost", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	)
	mockPlatform.AssertCalled(t, "Stop")
}

func TestSetupEnvironmentFS_CreatesPlatformDirectories(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	mockPlatform := mocks.NewMockPlatform()
	baseDir := "testroot"
	settings := platforms.Settings{
		ConfigDir: filepath.Join(baseDir, "config"),
		DataDir:   filepath.Join(baseDir, "data"),
		TempDir:   filepath.Join(baseDir, "tmp"),
	}
	mockPlatform.On("Settings").Return(settings)

	err := setupEnvironmentFS(fs, mockPlatform)
	require.NoError(t, err)

	for _, dir := range []string{
		settings.ConfigDir,
		settings.TempDir,
		settings.DataDir,
		filepath.Join(settings.DataDir, config.MappingsDir),
		filepath.Join(settings.DataDir, config.AssetsDir),
		filepath.Join(settings.DataDir, config.LaunchersDir),
		filepath.Join(settings.DataDir, config.MediaDir),
	} {
		exists, statErr := afero.DirExists(fs, dir)
		require.NoError(t, statErr)
		assert.True(t, exists, "expected %s to exist", dir)
	}
}

func TestCleanupHistoryRetention_CleansConfiguredHistory(t *testing.T) {
	t.Parallel()

	fs := testhelpers.NewMemoryFS()
	configDir := t.TempDir()
	configContent := `
config_schema = 1

[readers]
scan_history = 7
`
	err := fs.WriteFile(filepath.Join(configDir, "config.toml"), []byte(configContent), 0o644)
	require.NoError(t, err)
	cfg, err := testhelpers.NewTestConfigWithPort(fs, configDir, 7497)
	require.NoError(t, err)
	cfg.SetPlaytimeRetention(14)

	mockUserDB := &testhelpers.MockUserDBI{}
	db := &database.Database{UserDB: mockUserDB}
	mockUserDB.On("CleanupHistory", 7).Return(int64(2), nil).Once()
	mockUserDB.On("CleanupMediaHistory", 14).Return(int64(3), nil).Once()

	cleanupHistoryRetention(context.Background(), cfg, db)

	mockUserDB.AssertExpectations(t)
}

func TestCleanupHistoryRetention_CancelledBeforeMediaHistory(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	cfg.SetPlaytimeRetention(14)
	mockUserDB := &testhelpers.MockUserDBI{}
	db := &database.Database{UserDB: mockUserDB}
	ctx, cancel := context.WithCancel(context.Background())
	mockUserDB.On("CleanupHistory", 30).Run(func(_ mock.Arguments) {
		cancel()
	}).Return(int64(1), nil).Once()

	cleanupHistoryRetention(ctx, cfg, db)

	mockUserDB.AssertExpectations(t)
	mockUserDB.AssertNotCalled(t, "CleanupMediaHistory", mock.Anything)
}

type testDrainCallbackRegistrar struct {
	callbacks map[string]func(natural bool)
}

func (r *testDrainCallbackRegistrar) SetDrainCallback(slot string, fn func(natural bool)) {
	if r.callbacks == nil {
		r.callbacks = make(map[string]func(natural bool))
	}
	r.callbacks[slot] = fn
}

func TestWireNativeAudioDrainCallbacks_ClearsMediaOnNaturalDrain(t *testing.T) {
	t.Parallel()

	st, ns := state.NewState(mocks.NewMockPlatform(), "test-boot-uuid")
	t.Cleanup(func() {
		st.StopService()
		for {
			select {
			case <-ns:
			default:
				return
			}
		}
	})
	st.SetActiveMedia(models.NewActiveMedia(
		"Audio", "Audio", "primary.mp3", "Primary", platforms.NativeAudioLauncherID,
	))
	st.SetBackgroundMedia(models.NewActiveMedia(
		"Audio", "Audio", "background.mp3", "Background", platforms.NativeAudioLauncherID,
	))
	require.NotNil(t, st.ActiveMedia())
	require.NotNil(t, st.BackgroundMedia())

	plq := make(chan *playlists.Playlist, 1)
	svc := &ServiceContext{State: st, PlaylistQueue: plq}
	registrar := &testDrainCallbackRegistrar{}
	wireNativeAudioDrainCallbacks(registrar, svc)

	require.Contains(t, registrar.callbacks, mediaslot.Primary)
	require.Contains(t, registrar.callbacks, mediaslot.Background)
	// Primary callback clears ActiveMedia when native audio still owns it.
	registrar.callbacks[mediaslot.Primary](true)
	// Background callback with natural=true and no playlist clears BackgroundMedia.
	registrar.callbacks[mediaslot.Background](true)
	assert.Nil(t, st.ActiveMedia())
	assert.Nil(t, st.BackgroundMedia())
}

func TestWireNativeAudioDrainCallbacks_PrimaryDrainKeepsOtherLaunchersMedia(t *testing.T) {
	t.Parallel()

	st, ns := state.NewState(mocks.NewMockPlatform(), "test-boot-uuid")
	t.Cleanup(func() {
		st.StopService()
		for {
			select {
			case <-ns:
			default:
				return
			}
		}
	})
	// A game launched out-of-band owns active media while the audio track drains.
	st.SetActiveMedia(models.NewActiveMedia(
		"SNES", "SNES", "game.sfc", "Game", "mister-launcher",
	))

	svc := &ServiceContext{State: st, PlaylistQueue: make(chan *playlists.Playlist, 1)}
	registrar := &testDrainCallbackRegistrar{}
	wireNativeAudioDrainCallbacks(registrar, svc)

	registrar.callbacks[mediaslot.Primary](true)
	assert.NotNil(t, st.ActiveMedia(), "natural audio drain must not clear another launcher's media")
}

func TestWireNativeAudioDrainCallbacks_NonNaturalBackgroundNoOp(t *testing.T) {
	t.Parallel()

	st, ns := state.NewState(mocks.NewMockPlatform(), "test-boot-uuid")
	t.Cleanup(func() {
		st.StopService()
		for {
			select {
			case <-ns:
			default:
				return
			}
		}
	})
	st.SetBackgroundMedia(models.NewActiveMedia(
		"Audio", "Audio", "background.mp3", "Background", platforms.NativeAudioLauncherID,
	))
	require.NotNil(t, st.BackgroundMedia())

	plq := make(chan *playlists.Playlist, 1)
	svc := &ServiceContext{State: st, PlaylistQueue: plq}
	registrar := &testDrainCallbackRegistrar{}
	wireNativeAudioDrainCallbacks(registrar, svc)

	// natural=false (explicit stop/replace) must not clear or advance anything.
	registrar.callbacks[mediaslot.Background](false)
	assert.NotNil(t, st.BackgroundMedia())
}

// newAdvanceTestSvc creates a minimal ServiceContext for advanceBackgroundPlaylist tests.
func newAdvanceTestSvc(t *testing.T) (svc *ServiceContext, cleanup func()) {
	t.Helper()
	st, ns := state.NewState(mocks.NewMockPlatform(), "test-boot-uuid")
	cleanup = func() {
		st.StopService()
		for {
			select {
			case <-ns:
			default:
				return
			}
		}
	}
	svc = &ServiceContext{
		State:         st,
		PlaylistQueue: make(chan *playlists.Playlist, 2),
	}
	return svc, cleanup
}

type resumePlaybackStub struct {
	resumeErr error
	resumed   []string
}

func (*resumePlaybackStub) Play(_, _ string, _ audio.PlaybackOptions) error { return nil }
func (*resumePlaybackStub) Stop(_ string) error                             { return nil }
func (*resumePlaybackStub) Pause(_ string) error                            { return nil }
func (*resumePlaybackStub) TogglePause(_ string) error                      { return nil }
func (*resumePlaybackStub) Seek(_ string, _ time.Duration) error            { return nil }
func (*resumePlaybackStub) State(_ string) audio.PlaybackState              { return audio.PlaybackState{} }
func (s *resumePlaybackStub) Resume(slot string) error {
	s.resumed = append(s.resumed, slot)
	return s.resumeErr
}

func TestResumeBackgroundAfterMediaStop_ClearsAutoPauseOnSuccess(t *testing.T) {
	t.Parallel()

	svc, cleanup := newAdvanceTestSvc(t)
	defer cleanup()
	playback := &resumePlaybackStub{}
	svc.PlaybackManager = playback
	svc.State.SetBackgroundAutoPaused(true)

	resumeBackgroundAfterMediaStop(svc)

	assert.Equal(t, []string{mediaslot.Background}, playback.resumed)
	assert.False(t, svc.State.BackgroundAutoPaused())
}

func TestResumeBackgroundAfterMediaStop_KeepsAutoPauseOnFailure(t *testing.T) {
	t.Parallel()

	svc, cleanup := newAdvanceTestSvc(t)
	defer cleanup()
	playback := &resumePlaybackStub{resumeErr: errors.New("resume failed")}
	svc.PlaybackManager = playback
	svc.State.SetBackgroundAutoPaused(true)

	resumeBackgroundAfterMediaStop(svc)

	assert.Equal(t, []string{mediaslot.Background}, playback.resumed)
	assert.True(t, svc.State.BackgroundAutoPaused())
	select {
	case got := <-svc.PlaylistQueue:
		t.Fatalf("unexpected playlist enqueue: %+v", got)
	default:
	}
}

// makeMultiTrackPlaylist returns a playlist at the given index with 3 items.
func makeMultiTrackPlaylist(idx int, loop, loopOne bool) *playlists.Playlist {
	return &playlists.Playlist{
		ID:      "bg-list",
		Name:    "bg",
		Slot:    mediaslot.Background,
		Items:   []playlists.PlaylistItem{{ZapScript: "a"}, {ZapScript: "b"}, {ZapScript: "c"}},
		Index:   idx,
		Playing: true,
		Loop:    loop,
		LoopOne: loopOne,
	}
}

func TestAdvanceBackgroundPlaylist_RepeatOffAdvancesWithinPlaylist(t *testing.T) {
	t.Parallel()

	svc, cleanup := newAdvanceTestSvc(t)
	defer cleanup()

	pls := makeMultiTrackPlaylist(0, false, false) // idx=0, 2 more tracks remain
	svc.State.SetBackgroundPlaylist(pls)

	advanceBackgroundPlaylist(svc)

	require.NotNil(t, svc.State.GetBackgroundPlaylist())
	select {
	case got := <-svc.PlaylistQueue:
		assert.Equal(t, 1, got.Index, "should advance to index 1")
		assert.True(t, got.Playing)
		assert.False(t, got.ForceRelaunch)
	case <-time.After(time.Second):
		t.Fatal("timeout: expected playlist on queue")
	}
}

func TestAdvanceBackgroundPlaylist_RepeatOffStopsAtEnd(t *testing.T) {
	t.Parallel()

	svc, cleanup := newAdvanceTestSvc(t)
	defer cleanup()

	pls := makeMultiTrackPlaylist(2, false, false) // idx=2 is last
	svc.State.SetBackgroundPlaylist(pls)

	advanceBackgroundPlaylist(svc)

	assert.Nil(t, svc.State.GetBackgroundPlaylist(), "playlist must be cleared")
	assert.Nil(t, svc.State.BackgroundMedia(), "background media must be cleared")
	select {
	case got := <-svc.PlaylistQueue:
		t.Fatalf("unexpected enqueue: %+v", got)
	default:
	}
}

func TestAdvanceBackgroundPlaylist_RepeatAllWrapsToFirst(t *testing.T) {
	t.Parallel()

	svc, cleanup := newAdvanceTestSvc(t)
	defer cleanup()

	pls := makeMultiTrackPlaylist(2, true, false) // idx=2 is last, Loop=true
	svc.State.SetBackgroundPlaylist(pls)

	advanceBackgroundPlaylist(svc)

	select {
	case got := <-svc.PlaylistQueue:
		assert.Equal(t, 0, got.Index, "should wrap back to index 0")
		assert.True(t, got.Playing)
		assert.True(t, got.Loop)
		assert.False(t, got.ForceRelaunch)
	case <-time.After(time.Second):
		t.Fatal("timeout: expected playlist on queue")
	}
}

func TestAdvanceBackgroundPlaylist_RepeatOneSameTrack(t *testing.T) {
	t.Parallel()

	svc, cleanup := newAdvanceTestSvc(t)
	defer cleanup()

	pls := makeMultiTrackPlaylist(1, false, true) // idx=1, LoopOne=true
	svc.State.SetBackgroundPlaylist(pls)

	advanceBackgroundPlaylist(svc)

	select {
	case got := <-svc.PlaylistQueue:
		assert.Equal(t, 1, got.Index, "should stay on same index")
		assert.True(t, got.LoopOne)
		assert.True(t, got.ForceRelaunch, "ForceRelaunch must be set to defeat dedup")
	case <-time.After(time.Second):
		t.Fatal("timeout: expected playlist on queue")
	}
}

func TestAdvanceBackgroundPlaylist_RepeatAllSingleItemUsesForceRelaunch(t *testing.T) {
	t.Parallel()

	svc, cleanup := newAdvanceTestSvc(t)
	defer cleanup()

	pls := &playlists.Playlist{
		ID:      "single",
		Name:    "single",
		Slot:    mediaslot.Background,
		Items:   []playlists.PlaylistItem{{ZapScript: "only-track"}},
		Index:   0,
		Playing: true,
		Loop:    true,
	}
	svc.State.SetBackgroundPlaylist(pls)

	advanceBackgroundPlaylist(svc)

	select {
	case got := <-svc.PlaylistQueue:
		assert.Equal(t, 0, got.Index, "single-item loop stays at index 0")
		assert.True(t, got.Loop)
		assert.True(t, got.ForceRelaunch, "single-item loop needs ForceRelaunch")
	case <-time.After(time.Second):
		t.Fatal("timeout: expected playlist on queue")
	}
}

func TestAdvanceBackgroundPlaylist_NilPlaylistClearsBackgroundMedia(t *testing.T) {
	t.Parallel()

	svc, cleanup := newAdvanceTestSvc(t)
	defer cleanup()

	// No playlist set — single-track background.
	svc.State.SetBackgroundMedia(models.NewActiveMedia(
		"Audio", "Audio", "track.mp3", "Track", platforms.NativeAudioLauncherID,
	))
	require.NotNil(t, svc.State.BackgroundMedia())

	advanceBackgroundPlaylist(svc)

	assert.Nil(t, svc.State.BackgroundMedia(), "single-track end must clear background media")
	select {
	case got := <-svc.PlaylistQueue:
		t.Fatalf("unexpected enqueue: %+v", got)
	default:
	}
}

func TestRebuildStartupSlugSearchCache_SkipsWhenLoaded(t *testing.T) {
	t.Parallel()

	mockMediaDB := &testhelpers.MockMediaDBI{}

	rebuildStartupSlugSearchCache(mockMediaDB, true)

	mockMediaDB.AssertNotCalled(t, "GetIndexingStatus")
	mockMediaDB.AssertNotCalled(t, "RebuildSlugSearchCache")
}

func TestRebuildStartupSlugSearchCache_StatusErrorSkipsRebuild(t *testing.T) {
	t.Parallel()

	mockMediaDB := &testhelpers.MockMediaDBI{}
	mockMediaDB.On("GetIndexingStatus").Return("", assert.AnError).Once()

	rebuildStartupSlugSearchCache(mockMediaDB, false)

	mockMediaDB.AssertExpectations(t)
	mockMediaDB.AssertNotCalled(t, "TrackBackgroundOperation")
	mockMediaDB.AssertNotCalled(t, "RebuildSlugSearchCache")
}

func TestRebuildStartupSlugSearchCache_RebuildErrorReleasesBackgroundOperation(t *testing.T) {
	t.Parallel()

	mockMediaDB := &testhelpers.MockMediaDBI{}
	mockMediaDB.On("GetIndexingStatus").Return(mediadb.IndexingStatusCompleted, nil).Once()
	mockMediaDB.On("TrackBackgroundOperation").Return().Once()
	mockMediaDB.On("RebuildSlugSearchCache").Return(assert.AnError).Once()
	mockMediaDB.On("BackgroundOperationDone").Return().Once()

	rebuildStartupSlugSearchCache(mockMediaDB, false)

	mockMediaDB.AssertExpectations(t)
	mockMediaDB.AssertNotCalled(t, "PersistSlugSearchCache")
}

func TestRebuildStartupSlugSearchCache_SkipsDuringIndexing(t *testing.T) {
	t.Parallel()

	mockMediaDB := &testhelpers.MockMediaDBI{}
	mockMediaDB.On("GetIndexingStatus").Return(mediadb.IndexingStatusRunning, nil).Once()

	rebuildStartupSlugSearchCache(mockMediaDB, false)

	mockMediaDB.AssertExpectations(t)
	mockMediaDB.AssertNotCalled(t, "TrackBackgroundOperation")
	mockMediaDB.AssertNotCalled(t, "RebuildSlugSearchCache")
	mockMediaDB.AssertNotCalled(t, "PersistSlugSearchCache")
}

func TestRebuildStartupSlugSearchCache_RebuildsWhenIdle(t *testing.T) {
	t.Parallel()

	mockMediaDB := &testhelpers.MockMediaDBI{}
	mockMediaDB.On("GetIndexingStatus").Return(mediadb.IndexingStatusCompleted, nil).Once()
	mockMediaDB.On("TrackBackgroundOperation").Return().Once()
	mockMediaDB.On("RebuildSlugSearchCache").Return(nil).Once()
	mockMediaDB.On("PersistSlugSearchCache").Return(nil).Once()
	mockMediaDB.On("BackgroundOperationDone").Return().Once()

	rebuildStartupSlugSearchCache(mockMediaDB, false)

	mockMediaDB.AssertExpectations(t)
}

func TestCheckAndResumeIndexing_NoInterruption(t *testing.T) {
	// Note: Not using t.Parallel() due to global statusInstance usage in GenerateMediaDB
	methods.ClearIndexingStatus()

	// Create test dependencies
	fs := testhelpers.NewMemoryFS()
	cfg, err := testhelpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	mockPlatform := mocks.NewMockPlatform()
	mockUserDB := &testhelpers.MockUserDBI{}
	mockMediaDB := &testhelpers.MockMediaDBI{}

	db := &database.Database{
		UserDB:  mockUserDB,
		MediaDB: mockMediaDB,
	}

	// Create mock state
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")

	// Mock database to return "completed" status (no interruption)
	mockMediaDB.On("GetIndexingStatus").Return(mediadb.IndexingStatusCompleted, nil)
	// A clean state resets the resume-attempt counter.
	mockMediaDB.On("ResetIndexResumeAttempts").Return(nil)

	// Call the function
	checkAndResumeIndexing(mockPlatform, cfg, db, st, nil)

	mockMediaDB.AssertExpectations(t)

	// Verify that no indexing was triggered (only GetIndexingStatus should be called)
	mockMediaDB.AssertNotCalled(t, "Truncate")
	mockMediaDB.AssertNotCalled(t, "BeginTransaction")
}

func TestCheckAndResumeIndexing_WithRunningStatus(t *testing.T) {
	// Note: Not using t.Parallel() due to global statusInstance usage in GenerateMediaDB
	methods.ClearIndexingStatus()

	// Create test dependencies
	fs := testhelpers.NewMemoryFS()
	cfg, err := testhelpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform")
	mockPlatform.On("Settings").Return(platforms.Settings{})
	mockPlatform.On("Launchers", mock.Anything).Return([]platforms.Launcher{})
	mockPlatform.On("RootDirs", mock.Anything).Return([]string{"/test/roms"})

	// Use real database
	db, cleanup := testhelpers.NewTestDatabase(t)
	defer cleanup()

	// Create mock state
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")

	// Set up interrupted indexing state in real database
	// Use a minimal system list to make the test fast
	err = db.MediaDB.SetIndexingStatus(mediadb.IndexingStatusRunning)
	require.NoError(t, err)
	err = db.MediaDB.SetLastIndexedSystem("")
	require.NoError(t, err)
	err = db.MediaDB.SetIndexingSystems([]string{"NES"})
	require.NoError(t, err)

	// Call the function
	checkAndResumeIndexing(mockPlatform, cfg, db, st, nil)

	// Wait for async operation to start and complete
	// With minimal system list (just NES), this should complete quickly
	// Use longer timeout for slower CI environments (especially Windows)
	var status string
	maxWait := 10 * time.Second
	pollInterval := 100 * time.Millisecond
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		var err error
		status, err = db.MediaDB.GetIndexingStatus()
		require.NoError(t, err)
		if status != mediadb.IndexingStatusRunning {
			break
		}
		time.Sleep(pollInterval)
	}

	// If indexing is still running after timeout, cancel it to prevent
	// "database is closed" errors during cleanup
	if status == mediadb.IndexingStatusRunning {
		t.Logf("indexing did not complete within %v, cancelling to prevent cleanup race", maxWait)
		methods.CancelIndexing()
		// Wait for cancellation to complete
		cancelDeadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(cancelDeadline) {
			var err error
			status, err = db.MediaDB.GetIndexingStatus()
			require.NoError(t, err)
			if status != mediadb.IndexingStatusRunning {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
	}

	// Verify that indexing resume was triggered and completed
	// With proper tag seeding, indexing should now complete successfully
	assert.Contains(t, []string{mediadb.IndexingStatusCompleted, mediadb.IndexingStatusFailed}, status,
		"Indexing should complete or fail")
}

func TestCheckAndResumeIndexing_WithPendingStatus(t *testing.T) {
	// Note: Not using t.Parallel() due to global statusInstance usage in GenerateMediaDB
	methods.ClearIndexingStatus()

	// Create test dependencies
	fs := testhelpers.NewMemoryFS()
	cfg, err := testhelpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform")
	mockPlatform.On("Settings").Return(platforms.Settings{})
	mockPlatform.On("Launchers", mock.Anything).Return([]platforms.Launcher{})
	mockPlatform.On("RootDirs", mock.Anything).Return([]string{"/test/roms"})

	// Use real database
	db, cleanup := testhelpers.NewTestDatabase(t)
	defer cleanup()

	// Create mock state
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")

	// Set up interrupted indexing state in real database with "pending" status
	// Use a minimal system list to make the test fast
	err = db.MediaDB.SetIndexingStatus(mediadb.IndexingStatusPending)
	require.NoError(t, err)
	err = db.MediaDB.SetLastIndexedSystem("")
	require.NoError(t, err)
	err = db.MediaDB.SetIndexingSystems([]string{"NES"})
	require.NoError(t, err)

	// Call the function
	checkAndResumeIndexing(mockPlatform, cfg, db, st, nil)

	// Wait for status to change from "pending" - this confirms resume was triggered
	// We only need to see it start (change from pending), not complete
	var status string
	maxWait := 30 * time.Second
	pollInterval := 100 * time.Millisecond
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		var getStatusErr error
		status, getStatusErr = db.MediaDB.GetIndexingStatus()
		require.NoError(t, getStatusErr)
		if status != mediadb.IndexingStatusPending {
			break
		}
		time.Sleep(pollInterval)
	}

	// The key assertion: status should have changed from "pending"
	// This proves the resume logic was triggered
	assert.NotEqual(t, mediadb.IndexingStatusPending, status,
		"Status should have changed from pending (resume logic should have triggered)")

	// Cancel any running indexing to ensure clean test cleanup
	// This prevents "database is closed" errors during cleanup
	if status == mediadb.IndexingStatusRunning {
		methods.CancelIndexing()
		// Wait briefly for cancellation
		cancelDeadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(cancelDeadline) {
			status, _ = db.MediaDB.GetIndexingStatus()
			if status != mediadb.IndexingStatusRunning {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func TestCheckAndResumeIndexing_DatabaseError(t *testing.T) {
	// Note: Not using t.Parallel() due to global statusInstance usage in GenerateMediaDB
	methods.ClearIndexingStatus()

	// Create test dependencies
	fs := testhelpers.NewMemoryFS()
	cfg, err := testhelpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	mockPlatform := mocks.NewMockPlatform()
	mockUserDB := &testhelpers.MockUserDBI{}
	mockMediaDB := &testhelpers.MockMediaDBI{}

	db := &database.Database{
		UserDB:  mockUserDB,
		MediaDB: mockMediaDB,
	}

	// Create mock state
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")

	// Mock database to return error when checking indexing status
	mockMediaDB.On("GetIndexingStatus").Return("", assert.AnError)

	// Call the function
	checkAndResumeIndexing(mockPlatform, cfg, db, st, nil)

	mockMediaDB.AssertExpectations(t)

	// Verify that only GetIndexingStatus was called and no further operations
	mockMediaDB.AssertNotCalled(t, "GetOptimizationStatus")
}

func TestCheckAndResumeIndexing_FailedStatus(t *testing.T) {
	// Note: Not using t.Parallel() due to global statusInstance usage in GenerateMediaDB
	methods.ClearIndexingStatus()

	// Create test dependencies
	fs := testhelpers.NewMemoryFS()
	cfg, err := testhelpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	mockPlatform := mocks.NewMockPlatform()
	mockUserDB := &testhelpers.MockUserDBI{}
	mockMediaDB := &testhelpers.MockMediaDBI{}

	db := &database.Database{
		UserDB:  mockUserDB,
		MediaDB: mockMediaDB,
	}

	// Create mock state
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")

	// Mock database to return "failed" status (should not resume)
	mockMediaDB.On("GetIndexingStatus").Return(mediadb.IndexingStatusFailed, nil)
	// A non-interrupted state resets the resume-attempt counter.
	mockMediaDB.On("ResetIndexResumeAttempts").Return(nil)

	// Call the function
	checkAndResumeIndexing(mockPlatform, cfg, db, st, nil)

	mockMediaDB.AssertExpectations(t)

	// Verify that no indexing was triggered for failed status
	mockMediaDB.AssertNotCalled(t, "GetOptimizationStatus")
}

func TestCheckAndResumeIndexing_StopsAfterMaxResumeAttempts(t *testing.T) {
	// Note: Not using t.Parallel() due to global statusInstance usage in GenerateMediaDB
	methods.ClearIndexingStatus()

	fs := testhelpers.NewMemoryFS()
	cfg, err := testhelpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	mockPlatform := mocks.NewMockPlatform()
	db, cleanup := testhelpers.NewTestDatabase(t)
	defer cleanup()
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")

	// Wire an inbox so the resume-limit branch can surface a user-facing message.
	mockUserDB := testhelpers.NewMockUserDBI()
	mockUserDB.On("AddInboxMessage", mock.MatchedBy(func(m *database.InboxMessage) bool {
		return m.Category == inboxservice.CategoryMediaIndexResumeLimit &&
			m.Severity == inboxservice.SeverityWarning
	})).Return(&database.InboxMessage{}, nil)
	st.SetInbox(inboxservice.NewService(mockUserDB, st.Notifications))

	// Simulate a reindex that keeps getting interrupted: status wedged at
	// "running" with the resume-attempt counter already at the limit.
	require.NoError(t, db.MediaDB.SetIndexingStatus(mediadb.IndexingStatusRunning))
	for range maxIndexResumeAttempts {
		_, incErr := db.MediaDB.IncrementIndexResumeAttempts()
		require.NoError(t, incErr)
	}

	checkAndResumeIndexing(mockPlatform, cfg, db, st, nil)

	// The wedged index must be cancelled, not relaunched, so the library stays
	// browsable rather than looping the reindex on every boot.
	status, err := db.MediaDB.GetIndexingStatus()
	require.NoError(t, err)
	assert.Equal(t, mediadb.IndexingStatusCancelled, status)
	assert.False(t, methods.IsIndexing(), "indexing must not be relaunched past the resume limit")

	// The user must be told why indexing stopped, via a resume-limit inbox message.
	mockUserDB.AssertCalled(t, "AddInboxMessage", mock.Anything)
}

func TestCheckAndResumeIndexing_ResetsAttemptsOnCleanState(t *testing.T) {
	// Note: Not using t.Parallel() due to global statusInstance usage in GenerateMediaDB
	methods.ClearIndexingStatus()

	fs := testhelpers.NewMemoryFS()
	cfg, err := testhelpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	mockPlatform := mocks.NewMockPlatform()
	db, cleanup := testhelpers.NewTestDatabase(t)
	defer cleanup()
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")

	require.NoError(t, db.MediaDB.SetIndexingStatus(mediadb.IndexingStatusCompleted))
	_, incErr := db.MediaDB.IncrementIndexResumeAttempts()
	require.NoError(t, incErr)

	checkAndResumeIndexing(mockPlatform, cfg, db, st, nil)

	attempts, err := db.MediaDB.GetIndexResumeAttempts()
	require.NoError(t, err)
	assert.Equal(t, 0, attempts, "a clean indexing state must reset the resume-attempt counter")
}

func TestCheckAndResumeScraping_StatusErrorDoesNothing(t *testing.T) {
	// Not parallel — manipulates shared scrapingStatusInstance.
	methods.ClearScrapingStatus()
	mockMediaDB := testhelpers.NewMockMediaDBI()
	mockMediaDB.On("GetScrapingStatus").Return("", assert.AnError).Once()

	checkAndResumeScraping(
		mocks.NewMockPlatform(), nil, &database.Database{MediaDB: mockMediaDB}, nil, nil,
	)

	mockMediaDB.AssertExpectations(t)
	mockMediaDB.AssertNotCalled(t, "GetScrapingOperation")
	mockMediaDB.AssertNotCalled(t, "SetScrapingStatus", mock.Anything)
}

func TestCheckAndResumeScraping_OperationReadErrorDoesNotMarkFailed(t *testing.T) {
	// Not parallel — manipulates shared scrapingStatusInstance.
	methods.ClearScrapingStatus()
	mockMediaDB := testhelpers.NewMockMediaDBI()
	mockMediaDB.On("GetScrapingStatus").Return(mediadb.IndexingStatusRunning, nil).Once()
	mockMediaDB.On("GetScrapingOperation").Return(database.ScrapingOperation{}, false, assert.AnError).Once()

	checkAndResumeScraping(
		mocks.NewMockPlatform(), nil, &database.Database{MediaDB: mockMediaDB}, nil, nil,
	)

	mockMediaDB.AssertExpectations(t)
	mockMediaDB.AssertNotCalled(t, "SetScrapingStatus", mock.Anything)
}

func TestCheckAndResumeScraping_MissingOperationMarksFailed(t *testing.T) {
	// Not parallel — manipulates shared scrapingStatusInstance.
	methods.ClearScrapingStatus()
	mockMediaDB := testhelpers.NewMockMediaDBI()
	mockMediaDB.On("GetScrapingStatus").Return(mediadb.IndexingStatusRunning, nil).Once()
	mockMediaDB.On("GetScrapingOperation").Return(database.ScrapingOperation{}, false, nil).Once()
	mockMediaDB.On("SetScrapingStatus", mediadb.IndexingStatusFailed).Return(nil).Once()

	checkAndResumeScraping(
		mocks.NewMockPlatform(), nil, &database.Database{MediaDB: mockMediaDB}, nil, nil,
	)

	mockMediaDB.AssertExpectations(t)
}

func TestCheckAndResumeScraping_UnavailableScraperMarksFailed(t *testing.T) {
	// Not parallel — manipulates shared scrapingStatusInstance.
	methods.ClearScrapingStatus()
	operation := database.ScrapingOperation{ScraperID: "missing-scraper"}
	mockMediaDB := testhelpers.NewMockMediaDBI()
	mockMediaDB.On("GetScrapingStatus").Return(mediadb.IndexingStatusRunning, nil).Once()
	mockMediaDB.On("GetScrapingOperation").Return(operation, true, nil).Once()
	mockMediaDB.On("SetScrapingStatus", mediadb.IndexingStatusFailed).Return(nil).Once()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Scrapers", (*config.Instance)(nil)).Return(map[string]platforms.Scraper{}).Once()

	checkAndResumeScraping(mockPlatform, nil, &database.Database{MediaDB: mockMediaDB}, nil, nil)

	mockMediaDB.AssertExpectations(t)
	mockPlatform.AssertExpectations(t)
}

func TestCheckAndResumeScraping_StartFailurePersistsTerminalState(t *testing.T) {
	// Not parallel — manipulates shared scrapingStatusInstance.
	methods.ClearScrapingStatus()
	fs := testhelpers.NewMemoryFS()
	cfg, err := testhelpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	operation := database.ScrapingOperation{ScraperID: "test-scraper"}
	mockMediaDB := testhelpers.NewMockMediaDBI()
	mockMediaDB.On("GetScrapingStatus").Return(mediadb.IndexingStatusRunning, nil).Once()
	mockMediaDB.On("GetScrapingOperation").Return(operation, true, nil).Once()
	mockMediaDB.On("SetScrapingOperation", operation).Return(assert.AnError).Once()
	mockMediaDB.On("SetScrapingStatus", mediadb.IndexingStatusFailed).Return(nil).Once()
	mockMediaDB.On("ClearScrapingOperation").Return(nil).Once()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Scrapers", cfg).Return(map[string]platforms.Scraper{
		"test-scraper": {
			ID:   "test-scraper",
			Name: "Test Scraper",
			Scrape: func(
				context.Context, *config.Instance, platforms.Platform, afero.Fs,
				*database.Database, scraper.ScrapeOptions, platforms.ScraperCustomOptions,
				chan<- scraper.ScrapeUpdate,
			) error {
				return nil
			},
		},
	}).Twice()
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")
	t.Cleanup(st.StopService)

	checkAndResumeScraping(mockPlatform, cfg, &database.Database{MediaDB: mockMediaDB}, st, nil)

	mockMediaDB.AssertExpectations(t)
	mockPlatform.AssertExpectations(t)
}

func TestCheckAndResumeOptimization_RunningStatus(t *testing.T) {
	t.Parallel()

	mockMediaDB := &testhelpers.MockMediaDBI{}
	db := &database.Database{MediaDB: mockMediaDB}
	pauser := syncutil.NewPauser()
	notifChan := make(chan models.Notification, 1)
	mockMediaDB.On("GetOptimizationStatus").Return(mediadb.IndexingStatusRunning, nil).Once()
	mockMediaDB.On("RunBackgroundOptimization", mock.Anything, pauser).Run(func(args mock.Arguments) {
		callback, ok := args.Get(0).(func(bool))
		require.True(t, ok)
		callback(true)
	}).Once()

	checkAndResumeOptimization(db, notifChan, pauser)

	mockMediaDB.AssertExpectations(t)
	select {
	case notif := <-notifChan:
		assert.Equal(t, models.NotificationMediaIndexing, notif.Method)
	case <-time.After(time.Second):
		t.Fatal("expected optimization status notification")
	}
}

func TestCheckAndResumeOptimization_CompletedStatus(t *testing.T) {
	t.Parallel()

	mockMediaDB := &testhelpers.MockMediaDBI{}
	db := &database.Database{MediaDB: mockMediaDB}
	mockMediaDB.On("GetOptimizationStatus").Return(mediadb.IndexingStatusCompleted, nil).Once()

	checkAndResumeOptimization(db, make(chan models.Notification, 1), nil)

	mockMediaDB.AssertExpectations(t)
	mockMediaDB.AssertNotCalled(t, "RunBackgroundOptimization", mock.Anything, mock.Anything)
}

func TestStartPublishers_NoPublishers(t *testing.T) {
	t.Parallel()

	fs := testhelpers.NewMemoryFS()
	cfg, err := testhelpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	mockPlatform := mocks.NewMockPlatform()
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")
	notifChan := make(chan models.Notification)

	publishers, cancel, done := startPublishers(st, cfg, notifChan)
	defer func() {
		cancel()
		<-done
	}()

	assert.Empty(t, publishers, "should return empty slice when no publishers configured")
}

func TestStartPublishers_DrainsNotificationsWithoutPublishers(t *testing.T) {
	t.Parallel()

	fs := testhelpers.NewMemoryFS()
	cfg, err := testhelpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	mockPlatform := mocks.NewMockPlatform()
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")
	notifChan := make(chan models.Notification)

	publishers, cancel, done := startPublishers(st, cfg, notifChan)
	require.Empty(t, publishers)

	select {
	case notifChan <- models.Notification{Method: models.NotificationStarted}:
	case <-time.After(time.Second):
		t.Fatal("notification channel was not drained")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("publisher fan-out did not stop after cancellation")
	}
}

func TestRunMediaDBStartupMaintenance_CancelledSkipsTagCacheWarmup(t *testing.T) {
	t.Parallel()

	mockMediaDB := &testhelpers.MockMediaDBI{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	mockMediaDB.On("TrackBackgroundOperation").Once()
	mockMediaDB.On("BackgroundOperationDone").Once()

	runMediaDBStartupMaintenance(ctx, mockMediaDB, nil, false)

	mockMediaDB.AssertExpectations(t)
	mockMediaDB.AssertNotCalled(t, "RebuildTagCache")
}

func TestRunMediaDBStartupMaintenance_PassesPauserToTemporaryRepairOptimization(t *testing.T) {
	t.Parallel()

	mockMediaDB := &testhelpers.MockMediaDBI{}
	pauser := syncutil.NewPauser()
	ctx := context.Background()
	mockMediaDB.On("TrackBackgroundOperation").Once()
	mockMediaDB.On("RebuildTagCache").Return(nil).Once()
	mockMediaDB.On("PersistTagCache").Return(nil).Once()
	mockMediaDB.On("TemporaryRepairJobsPending", ctx).Return(true, nil).Once()
	mockMediaDB.On("GetIndexingStatus").Return(mediadb.IndexingStatusCompleted, nil).Once()
	mockMediaDB.On("GetOptimizationStatus").Return(mediadb.IndexingStatusCompleted, nil).Once()
	mockMediaDB.On("RunBackgroundOptimization", mock.Anything, pauser).Once()
	mockMediaDB.On("BackgroundOperationDone").Once()

	runMediaDBStartupMaintenance(ctx, mockMediaDB, pauser, false)

	mockMediaDB.AssertExpectations(t)
}

func TestRunMediaDBStartupMaintenance_SkipsRebuildWhenCachePersisted(t *testing.T) {
	t.Parallel()

	mockMediaDB := &testhelpers.MockMediaDBI{}
	ctx := context.Background()
	mockMediaDB.On("TrackBackgroundOperation").Once()
	mockMediaDB.On("TemporaryRepairJobsPending", ctx).Return(false, nil).Once()
	mockMediaDB.On("BackgroundOperationDone").Once()

	runMediaDBStartupMaintenance(ctx, mockMediaDB, nil, true)

	mockMediaDB.AssertExpectations(t)
	mockMediaDB.AssertNotCalled(t, "RebuildTagCache")
}

func TestStartPublishers_DisabledPublisher(t *testing.T) {
	t.Parallel()

	fs := testhelpers.NewMemoryFS()
	configDir := t.TempDir()

	// Create config with explicitly disabled publisher
	configContent := `
config_schema = 1

[service]
api_port = 7497

[[service.publishers.mqtt]]
enabled = false
broker = "localhost:1883"
topic = "zaparoo/events"
`
	err := fs.WriteFile(filepath.Join(configDir, "config.toml"), []byte(configContent), 0o644)
	require.NoError(t, err)

	cfg, err := testhelpers.NewTestConfigWithPort(fs, configDir, 7497)
	require.NoError(t, err)

	mockPlatform := mocks.NewMockPlatform()
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")
	notifChan := make(chan models.Notification)

	publishers, cancel, done := startPublishers(st, cfg, notifChan)
	defer func() {
		cancel()
		<-done
	}()

	assert.Empty(t, publishers, "should skip disabled publishers")
}

func TestStartPublishers_InvalidBroker(t *testing.T) {
	t.Parallel()

	fs := testhelpers.NewMemoryFS()
	configDir := t.TempDir()

	// Create config with unreachable broker
	configContent := `
config_schema = 1

[service]
api_port = 7497

[[service.publishers.mqtt]]
broker = "invalid-broker-does-not-exist:1883"
topic = "zaparoo/events"
`
	err := fs.WriteFile(filepath.Join(configDir, "config.toml"), []byte(configContent), 0o644)
	require.NoError(t, err)

	cfg, err := testhelpers.NewTestConfigWithPort(fs, configDir, 7497)
	require.NoError(t, err)

	mockPlatform := mocks.NewMockPlatform()
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")
	notifChan := make(chan models.Notification)

	publishers, cancel, done := startPublishers(st, cfg, notifChan)
	defer func() {
		cancel()
		<-done
	}()
	defer st.StopService() // cancel state context to stop publisher retry goroutines

	// Connection failures now retry in background instead of being skipped,
	// so the publisher is added to the active list
	assert.Len(t, publishers, 1, "unreachable broker should still be added (retries in background)")
}

func TestStartPublishers_EmptyBroker(t *testing.T) {
	t.Parallel()

	fs := testhelpers.NewMemoryFS()
	configDir := t.TempDir()

	// Create config with empty broker (config validation error)
	configContent := `
config_schema = 1

[service]
api_port = 7497

[[service.publishers.mqtt]]
broker = ""
topic = "zaparoo/events"
`
	err := fs.WriteFile(filepath.Join(configDir, "config.toml"), []byte(configContent), 0o644)
	require.NoError(t, err)

	cfg, err := testhelpers.NewTestConfigWithPort(fs, configDir, 7497)
	require.NoError(t, err)

	mockPlatform := mocks.NewMockPlatform()
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")
	notifChan := make(chan models.Notification)

	publishers, cancel, done := startPublishers(st, cfg, notifChan)
	defer func() {
		cancel()
		<-done
	}()
	defer st.StopService()

	// Config validation errors (empty broker) should still cause the publisher to be skipped
	assert.Empty(t, publishers, "empty broker should be skipped (config validation error)")
}

func TestRedactBroker(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "mqtt://broker.example:1883", redactBroker("mqtt://user:pass@broker.example:1883"))
	assert.Equal(t, "broker.example:1883", redactBroker("user:pass@broker.example:1883"))
	assert.Equal(t, "broker.example:1883", redactBroker("broker.example:1883"))
}

func TestPruneExpiredZapLinkHosts_Success(t *testing.T) {
	t.Parallel()

	mockUserDB := &testhelpers.MockUserDBI{}
	mockMediaDB := &testhelpers.MockMediaDBI{}

	db := &database.Database{
		UserDB:  mockUserDB,
		MediaDB: mockMediaDB,
	}

	mockUserDB.On("PruneExpiredZapLinkHosts", zapLinkHostExpiration).Return(int64(5), nil)

	pruneExpiredZapLinkHosts(db)

	mockUserDB.AssertExpectations(t)
}

func TestPruneExpiredZapLinkHosts_NoRowsDeleted(t *testing.T) {
	t.Parallel()

	mockUserDB := &testhelpers.MockUserDBI{}
	mockMediaDB := &testhelpers.MockMediaDBI{}

	db := &database.Database{
		UserDB:  mockUserDB,
		MediaDB: mockMediaDB,
	}

	mockUserDB.On("PruneExpiredZapLinkHosts", zapLinkHostExpiration).Return(int64(0), nil)

	pruneExpiredZapLinkHosts(db)

	mockUserDB.AssertExpectations(t)
}

func TestPruneExpiredZapLinkHosts_DatabaseError(t *testing.T) {
	t.Parallel()

	mockUserDB := &testhelpers.MockUserDBI{}
	mockMediaDB := &testhelpers.MockMediaDBI{}

	db := &database.Database{
		UserDB:  mockUserDB,
		MediaDB: mockMediaDB,
	}

	mockUserDB.On("PruneExpiredZapLinkHosts", zapLinkHostExpiration).Return(int64(0), assert.AnError)

	pruneExpiredZapLinkHosts(db)

	mockUserDB.AssertExpectations(t)
}

// TestCheckAndResumeIndexing_WaitGroupRace is a regression test for a race condition
// where WaitGroup.Add() was called after WaitGroup.Wait() had already returned.
// The bug occurred when optimization was started as a separate goroutine with its own
// Add(1) inside, creating a window where the indexing goroutine's Done() could cause
// Wait() to return before optimization's Add(1) ran.
//
// This test runs multiple iterations to increase the likelihood of triggering the race
// condition if the bug is reintroduced. Run with: go test -race -run TestCheckAndResumeIndexing_WaitGroupRace
func TestCheckAndResumeIndexing_WaitGroupRace(t *testing.T) {
	// Note: Not using t.Parallel() due to global statusInstance usage in GenerateMediaDB
	// Run multiple iterations to stress-test the race condition
	const iterations = 10

	for range iterations {
		t.Run("iteration", func(t *testing.T) {
			methods.ClearIndexingStatus()

			// Create test dependencies
			fs := testhelpers.NewMemoryFS()
			cfg, err := testhelpers.NewTestConfig(fs, t.TempDir())
			require.NoError(t, err)

			mockPlatform := mocks.NewMockPlatform()
			mockPlatform.On("ID").Return("test-platform")
			mockPlatform.On("Settings").Return(platforms.Settings{})
			mockPlatform.On("Launchers", mock.Anything).Return([]platforms.Launcher{})
			mockPlatform.On("RootDirs", mock.Anything).Return([]string{"/test/roms"})

			// Use real database
			db, cleanup := testhelpers.NewTestDatabase(t)

			// Create mock state
			st, _ := state.NewState(mockPlatform, "test-boot-uuid")

			// Set up interrupted indexing state
			err = db.MediaDB.SetIndexingStatus(mediadb.IndexingStatusRunning)
			require.NoError(t, err)
			err = db.MediaDB.SetLastIndexedSystem("")
			require.NoError(t, err)
			err = db.MediaDB.SetIndexingSystems([]string{"NES"})
			require.NoError(t, err)

			// Call the function - this starts async indexing + optimization
			checkAndResumeIndexing(mockPlatform, cfg, db, st, nil)

			// The critical test: WaitForBackgroundOperations should NOT panic
			// If the race condition exists, this could panic with:
			// "sync: WaitGroup is reused before previous Wait has returned"
			db.MediaDB.WaitForBackgroundOperations()

			// Clean up after waiting for all operations
			cleanup()
		})
	}
}
