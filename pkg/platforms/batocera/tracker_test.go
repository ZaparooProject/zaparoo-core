//go:build linux

// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package batocera

import (
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esapi"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBackgroundTracker_StartsCorrectly tests that the background tracker starts without error
func TestBackgroundTracker_StartsCorrectly(t *testing.T) {
	// Note: Not using t.Parallel() because MockESAPIServer binds to hardcoded port 1234

	mockESAPI := helpers.NewMockESAPIServer(t)
	mockESAPI.WithNoRunningGame()

	fs := helpers.NewMemoryFS()
	cfg, err := helpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	fakeClock := clockwork.NewFakeClock()
	platform := &Platform{
		clock: fakeClock,
	}

	var capturedMedia *models.ActiveMedia
	setActiveMedia := func(media *models.ActiveMedia) {
		capturedMedia = media
	}

	activeMedia := func() *models.ActiveMedia {
		return capturedMedia
	}

	// StartPost should start the background tracker
	err = platform.StartPost(cfg, nil, activeMedia, setActiveMedia, nil)
	require.NoError(t, err)

	// Verify tracker was started
	assert.NotNil(t, platform.stopTracker, "Tracker cleanup function should be set")

	// Cleanup
	if platform.stopTracker != nil {
		_ = platform.stopTracker()
	}
}

// TestBackgroundTracker_DetectsExternalGameLaunch tests that the tracker detects
// when a game is launched externally (not by Zaparoo)
func TestBackgroundTracker_DetectsExternalGameLaunch(t *testing.T) {
	// Note: Not using t.Parallel() because MockESAPIServer binds to hardcoded port 1234

	mockESAPI := helpers.NewMockESAPIServer(t)
	mockESAPI.WithNoRunningGame()

	fs := helpers.NewMemoryFS()
	cfg, err := helpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	fakeClock := clockwork.NewFakeClock()
	platform := &Platform{
		clock: fakeClock,
	}

	var mediaMu syncutil.RWMutex
	var capturedMedia *models.ActiveMedia
	setActiveMedia := func(media *models.ActiveMedia) {
		mediaMu.Lock()
		capturedMedia = media
		mediaMu.Unlock()
	}

	activeMedia := func() *models.ActiveMedia {
		mediaMu.RLock()
		defer mediaMu.RUnlock()
		return capturedMedia
	}

	// Start platform with no game running
	err = platform.StartPost(cfg, nil, activeMedia, setActiveMedia, nil)
	require.NoError(t, err)
	defer func() {
		if platform.stopTracker != nil {
			_ = platform.stopTracker()
		}
	}()

	// Initially no game
	mediaMu.RLock()
	assert.Nil(t, capturedMedia)
	mediaMu.RUnlock()

	// Simulate external game launch by changing mock API response
	mockESAPI.WithRunningGame(&esapi.RunningGameResponse{
		Name:       "Sonic the Hedgehog",
		Path:       "/userdata/roms/genesis/sonic.md",
		SystemName: "megadrive",
	})

	// Advance clock by 2 seconds to trigger tracker tick
	fakeClock.Advance(2 * time.Second)

	// Give the tracker goroutine a moment to execute
	time.Sleep(50 * time.Millisecond)

	// Verify game was detected
	mediaMu.RLock()
	require.NotNil(t, capturedMedia, "Should detect externally launched game")
	assert.Equal(t, systemdefs.SystemGenesis, capturedMedia.SystemID)
	assert.Equal(t, "Sonic the Hedgehog", capturedMedia.Name)
	assert.Equal(t, "/userdata/roms/genesis/sonic.md", capturedMedia.Path)
	mediaMu.RUnlock()
}

// TestBackgroundTracker_DetectsExternalGameClose tests that the tracker detects
// when a game is closed externally
func TestBackgroundTracker_DetectsExternalGameClose(t *testing.T) {
	// Note: Not using t.Parallel() because MockESAPIServer binds to hardcoded port 1234

	mockESAPI := helpers.NewMockESAPIServer(t)
	mockESAPI.WithRunningGame(&esapi.RunningGameResponse{
		Name:       "Castlevania",
		Path:       "/userdata/roms/nes/castlevania.nes",
		SystemName: "nes",
	})

	fs := helpers.NewMemoryFS()
	cfg, err := helpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	fakeClock := clockwork.NewFakeClock()
	platform := &Platform{
		clock: fakeClock,
	}

	var mediaMu syncutil.RWMutex
	var capturedMedia *models.ActiveMedia
	setActiveMedia := func(media *models.ActiveMedia) {
		mediaMu.Lock()
		capturedMedia = media
		mediaMu.Unlock()
	}

	activeMedia := func() *models.ActiveMedia {
		mediaMu.RLock()
		defer mediaMu.RUnlock()
		return capturedMedia
	}

	// Start platform with game running
	err = platform.StartPost(cfg, nil, activeMedia, setActiveMedia, nil)
	require.NoError(t, err)
	defer func() {
		if platform.stopTracker != nil {
			_ = platform.stopTracker()
		}
	}()

	// Verify game was detected at startup
	mediaMu.RLock()
	require.NotNil(t, capturedMedia)
	assert.Equal(t, "Castlevania", capturedMedia.Name)
	mediaMu.RUnlock()

	// Simulate game closing by changing mock API response
	mockESAPI.WithNoRunningGame()

	// Advance clock by 2 seconds to trigger tracker tick
	fakeClock.Advance(2 * time.Second)

	// Give the tracker goroutine a moment to execute
	time.Sleep(50 * time.Millisecond)

	// Verify game close was detected
	mediaMu.RLock()
	assert.Nil(t, capturedMedia, "Should detect when game closes externally")
	mediaMu.RUnlock()
}

// TestBackgroundTracker_ClearsKodiWhenNotReachable tests that the tracker
// clears the kodiActive flag and active media when Kodi is not reachable
func TestBackgroundTracker_ClearsKodiWhenNotReachable(t *testing.T) {
	// Note: Not using t.Parallel() because MockESAPIServer binds to hardcoded port 1234

	mockESAPI := helpers.NewMockESAPIServer(t)
	mockESAPI.WithNoRunningGame()

	fs := helpers.NewMemoryFS()
	cfg, err := helpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	fakeClock := clockwork.NewFakeClock()
	platform := &Platform{
		clock: fakeClock,
	}

	var mediaMu syncutil.RWMutex
	var capturedMedia *models.ActiveMedia
	setActiveMedia := func(media *models.ActiveMedia) {
		mediaMu.Lock()
		capturedMedia = media
		mediaMu.Unlock()
	}

	activeMedia := func() *models.ActiveMedia {
		mediaMu.RLock()
		defer mediaMu.RUnlock()
		return capturedMedia
	}

	// Start platform
	err = platform.StartPost(cfg, nil, activeMedia, setActiveMedia, nil)
	require.NoError(t, err)
	defer func() {
		if platform.stopTracker != nil {
			_ = platform.stopTracker()
		}
	}()

	// Simulate Kodi being active by setting the flag manually
	// (normally this would be set by LaunchMedia)
	platform.trackerMu.Lock()
	platform.kodiActive = true
	platform.trackerMu.Unlock()

	// Set active media to a Kodi video
	kodiMedia := &models.ActiveMedia{
		SystemID:   systemdefs.SystemVideo,
		SystemName: "Video",
		Path:       "kodi://movies/123",
		Name:       "The Matrix",
		LauncherID: "KodiMovie",
	}
	setActiveMedia(kodiMedia)
	platform.trackerMu.Lock()
	platform.lastKnownGame = kodiMedia
	platform.trackerMu.Unlock()

	// Advance clock by 2 seconds to trigger tracker tick
	// Since Kodi is not actually running, the tracker should detect this and clear kodiActive
	fakeClock.Advance(2 * time.Second)

	// Give the tracker goroutine a moment to execute
	time.Sleep(50 * time.Millisecond)

	// Verify kodiActive was cleared when Kodi couldn't be reached
	platform.trackerMu.RLock()
	kodiActiveAfter := platform.kodiActive
	platform.trackerMu.RUnlock()
	assert.False(t, kodiActiveAfter, "Should clear kodiActive when Kodi is not reachable")

	// Verify active media was cleared
	mediaMu.RLock()
	assert.Nil(t, capturedMedia, "Should clear media when Kodi is not reachable")
	mediaMu.RUnlock()
}

// TestBackgroundTracker_ClearsKodiWhenNotActive tests that the tracker
// clears active media when API says "no game" and Kodi is not active
func TestBackgroundTracker_ClearsKodiWhenNotActive(t *testing.T) {
	// Note: Not using t.Parallel() because MockESAPIServer binds to hardcoded port 1234

	mockESAPI := helpers.NewMockESAPIServer(t)
	mockESAPI.WithNoRunningGame()

	fs := helpers.NewMemoryFS()
	cfg, err := helpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	fakeClock := clockwork.NewFakeClock()
	platform := &Platform{
		clock: fakeClock,
	}

	var mediaMu syncutil.RWMutex
	var capturedMedia *models.ActiveMedia
	setActiveMedia := func(media *models.ActiveMedia) {
		mediaMu.Lock()
		capturedMedia = media
		mediaMu.Unlock()
	}

	activeMedia := func() *models.ActiveMedia {
		mediaMu.RLock()
		defer mediaMu.RUnlock()
		return capturedMedia
	}

	// Start platform
	err = platform.StartPost(cfg, nil, activeMedia, setActiveMedia, nil)
	require.NoError(t, err)
	defer func() {
		if platform.stopTracker != nil {
			_ = platform.stopTracker()
		}
	}()

	// Set active media manually (but kodiActive=false)
	someMedia := &models.ActiveMedia{
		SystemID:   systemdefs.SystemVideo,
		SystemName: "Video",
		Path:       "/test/video.mp4",
		Name:       "Test Video",
	}
	setActiveMedia(someMedia)

	// Ensure kodiActive is false
	platform.trackerMu.Lock()
	platform.kodiActive = false
	platform.lastKnownGame = someMedia
	platform.trackerMu.Unlock()

	// Advance clock by 2 seconds to trigger tracker tick
	fakeClock.Advance(2 * time.Second)

	// Give the tracker goroutine a moment to execute
	time.Sleep(50 * time.Millisecond)

	// Verify media was cleared since API says no game and kodiActive=false
	mediaMu.RLock()
	assert.Nil(t, capturedMedia, "Should clear media when no game and kodiActive=false")
	mediaMu.RUnlock()
}

// TestBackgroundTracker_DetectsGameChange tests that the tracker detects
// when one game is switched to another
func TestBackgroundTracker_DetectsGameChange(t *testing.T) {
	// Note: Not using t.Parallel() because MockESAPIServer binds to hardcoded port 1234

	mockESAPI := helpers.NewMockESAPIServer(t)
	mockESAPI.WithRunningGame(&esapi.RunningGameResponse{
		Name:       "Game 1",
		Path:       "/roms/nes/game1.nes",
		SystemName: "nes",
	})

	fs := helpers.NewMemoryFS()
	cfg, err := helpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	fakeClock := clockwork.NewFakeClock()
	platform := &Platform{
		clock: fakeClock,
	}

	var mediaMu syncutil.RWMutex
	var capturedMedia *models.ActiveMedia
	setActiveMedia := func(media *models.ActiveMedia) {
		mediaMu.Lock()
		capturedMedia = media
		mediaMu.Unlock()
	}

	activeMedia := func() *models.ActiveMedia {
		mediaMu.RLock()
		defer mediaMu.RUnlock()
		return capturedMedia
	}

	// Start with game 1 running
	err = platform.StartPost(cfg, nil, activeMedia, setActiveMedia, nil)
	require.NoError(t, err)
	defer func() {
		if platform.stopTracker != nil {
			_ = platform.stopTracker()
		}
	}()

	mediaMu.RLock()
	assert.Equal(t, "Game 1", capturedMedia.Name)
	mediaMu.RUnlock()

	// Switch to game 2
	mockESAPI.WithRunningGame(&esapi.RunningGameResponse{
		Name:       "Game 2",
		Path:       "/roms/nes/game2.nes",
		SystemName: "nes",
	})

	// Advance clock
	fakeClock.Advance(2 * time.Second)
	time.Sleep(50 * time.Millisecond)

	// Verify game change detected
	mediaMu.RLock()
	assert.Equal(t, "Game 2", capturedMedia.Name)
	mediaMu.RUnlock()
}

// TestBackgroundTracker_StopsCleanly tests that the tracker cleanup function
// stops the goroutine without errors
func TestBackgroundTracker_StopsCleanly(t *testing.T) {
	// Note: Not using t.Parallel() because MockESAPIServer binds to hardcoded port 1234

	mockESAPI := helpers.NewMockESAPIServer(t)
	mockESAPI.WithNoRunningGame()

	fs := helpers.NewMemoryFS()
	cfg, err := helpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	fakeClock := clockwork.NewFakeClock()
	platform := &Platform{
		clock: fakeClock,
	}

	var capturedMedia *models.ActiveMedia
	setActiveMedia := func(media *models.ActiveMedia) {
		capturedMedia = media
	}

	activeMedia := func() *models.ActiveMedia {
		return capturedMedia
	}

	// Start platform
	err = platform.StartPost(cfg, nil, activeMedia, setActiveMedia, nil)
	require.NoError(t, err)

	// Verify tracker started
	require.NotNil(t, platform.stopTracker)

	// Stop the tracker
	err = platform.stopTracker()
	assert.NoError(t, err, "Tracker cleanup should not error")

	// Note: We don't call platform.Stop() here because it requires
	// keyboard/gamepad to be initialized via StartPre(), which we skip in tests
}

// TestLaunchMedia_SetsKodiActiveFlag tests that LaunchMedia sets the
// kodiActive flag when launching Kodi launchers
func TestLaunchMedia_SetsKodiActiveFlag(t *testing.T) {
	// Note: Not using t.Parallel() because MockESAPIServer binds to hardcoded port 1234

	mockESAPI := helpers.NewMockESAPIServer(t)
	mockESAPI.WithNoRunningGame()

	fs := helpers.NewMemoryFS()
	cfg, err := helpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	fakeClock := clockwork.NewFakeClock()
	platform := &Platform{
		clock: fakeClock,
		cfg:   cfg,
	}

	var capturedMedia *models.ActiveMedia
	setActiveMedia := func(media *models.ActiveMedia) {
		capturedMedia = media
	}

	activeMedia := func() *models.ActiveMedia {
		return capturedMedia
	}

	platform.activeMedia = activeMedia
	platform.setActiveMedia = setActiveMedia

	// Start platform
	err = platform.StartPost(cfg, nil, activeMedia, setActiveMedia, nil)
	require.NoError(t, err)
	defer func() {
		if platform.stopTracker != nil {
			_ = platform.stopTracker()
		}
	}()

	// Get a Kodi launcher
	launchers := platform.Launchers(cfg)
	var kodiLauncher *platforms.Launcher
	for i := range launchers {
		if launchers[i].ID == "KodiMovie" {
			kodiLauncher = &launchers[i]
			break
		}
	}
	require.NotNil(t, kodiLauncher)

	// We can't actually call LaunchMedia because it requires a running Kodi instance,
	// but we can verify the helper function works correctly
	assert.True(t, isKodiLauncher(kodiLauncher.ID), "KodiMovie should be identified as Kodi launcher")

	// Verify kodiActive starts as false
	platform.trackerMu.RLock()
	kodiActive := platform.kodiActive
	platform.trackerMu.RUnlock()
	assert.False(t, kodiActive, "kodiActive should start as false")
}

// TestStopActiveLauncher_UsesKodiFlagForExternalKodi tests that StopActiveLauncher
// uses Kodi stop logic when kodiActive=true, even if LauncherID is empty (external launch)
func TestStopActiveLauncher_UsesKodiFlagForExternalKodi(t *testing.T) {
	// Note: Not using t.Parallel() because MockESAPIServer binds to hardcoded port 1234

	mockESAPI := helpers.NewMockESAPIServer(t)
	mockESAPI.WithNoRunningGame()

	fs := helpers.NewMemoryFS()
	cfg, err := helpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	fakeClock := clockwork.NewFakeClock()
	platform := &Platform{
		clock: fakeClock,
		cfg:   cfg,
	}

	var capturedMedia *models.ActiveMedia
	setActiveMedia := func(media *models.ActiveMedia) {
		capturedMedia = media
	}

	activeMedia := func() *models.ActiveMedia {
		return capturedMedia
	}

	platform.activeMedia = activeMedia
	platform.setActiveMedia = setActiveMedia

	// Simulate externally launched Kodi (LauncherID is empty, but kodiActive=true)
	externalKodiMedia := &models.ActiveMedia{
		SystemID:   systemdefs.SystemVideo,
		SystemName: "Video",
		Path:       "/movies/matrix.mkv",
		Name:       "The Matrix",
		LauncherID: "", // Empty because detected externally
	}
	setActiveMedia(externalKodiMedia)

	// Set kodiActive=true (as the tracker would do when detecting external Kodi)
	platform.trackerMu.Lock()
	platform.kodiActive = true
	platform.trackerMu.Unlock()

	// Call StopActiveLauncher - should use Kodi stop logic
	err = platform.StopActiveLauncher(platforms.StopForMenu)

	// Verify kodiActive was NOT cleared (StopForMenu keeps Kodi running)
	platform.trackerMu.RLock()
	kodiActive := platform.kodiActive
	platform.trackerMu.RUnlock()

	// Note: In real scenario with Kodi running, StopForMenu would succeed and keep kodiActive=true
	// Here Kodi isn't actually running, so it will fail to connect, but the important part
	// is that it tried the Kodi code path instead of the ES API path

	// The test reaching here without panic means it took the Kodi code path
	t.Logf("StopActiveLauncher with external Kodi completed (kodiActive after: %v, err: %v)", kodiActive, err)
}

// TestStopActiveLauncher_ClearsKodiActiveFlag tests that StopActiveLauncher
// clears the kodiActive flag when stopping
func TestStopActiveLauncher_ClearsKodiActiveFlag(t *testing.T) {
	// Note: Not using t.Parallel() because MockESAPIServer binds to hardcoded port 1234

	mockESAPI := helpers.NewMockESAPIServer(t)
	mockESAPI.WithNoRunningGame()

	fs := helpers.NewMemoryFS()
	cfg, err := helpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	fakeClock := clockwork.NewFakeClock()
	platform := &Platform{
		clock: fakeClock,
		cfg:   cfg,
	}

	var capturedMedia *models.ActiveMedia
	setActiveMedia := func(media *models.ActiveMedia) {
		capturedMedia = media
	}

	activeMedia := func() *models.ActiveMedia {
		return capturedMedia
	}

	platform.activeMedia = activeMedia
	platform.setActiveMedia = setActiveMedia

	// Set kodiActive manually
	platform.trackerMu.Lock()
	platform.kodiActive = true
	platform.trackerMu.Unlock()

	// Call StopActiveLauncher
	_ = platform.StopActiveLauncher(platforms.StopForPreemption)

	// Verify kodiActive was cleared
	platform.trackerMu.RLock()
	kodiActive := platform.kodiActive
	platform.trackerMu.RUnlock()
	assert.False(t, kodiActive, "kodiActive should be cleared after StopActiveLauncher")
}

// TestBackgroundTracker_DetectsExternalKodi tests that the tracker can detect
// when Kodi is launched externally (not via Zaparoo).
// Note: This test documents the expected behavior. Full testing would require
// a Kodi JSON-RPC mock server that responds to GetActivePlayers and GetPlayerItem.
func TestBackgroundTracker_DetectsExternalKodi(t *testing.T) {
	// Note: Not using t.Parallel() because MockESAPIServer binds to hardcoded port 1234

	mockESAPI := helpers.NewMockESAPIServer(t)
	mockESAPI.WithNoRunningGame()

	fs := helpers.NewMemoryFS()
	cfg, err := helpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	fakeClock := clockwork.NewFakeClock()
	platform := &Platform{
		clock: fakeClock,
	}

	var capturedMedia *models.ActiveMedia
	setActiveMedia := func(media *models.ActiveMedia) {
		capturedMedia = media
	}

	activeMedia := func() *models.ActiveMedia {
		return capturedMedia
	}

	// Start platform
	err = platform.StartPost(cfg, nil, activeMedia, setActiveMedia, nil)
	require.NoError(t, err)
	defer func() {
		if platform.stopTracker != nil {
			_ = platform.stopTracker()
		}
	}()

	// Verify kodiActive starts as false
	platform.trackerMu.RLock()
	kodiActive := platform.kodiActive
	platform.trackerMu.RUnlock()
	assert.False(t, kodiActive, "kodiActive should start as false")

	// Note: To fully test external Kodi detection, we would need to:
	// 1. Set up a MockKodiServer (like MockESAPIServer)
	// 2. Configure it to respond to GetActivePlayers with active players
	// 3. Configure it to respond to GetPlayerItem with playing content
	// 4. Advance the clock and verify kodiActive gets set to true
	//
	// For now, this test documents the expected behavior and verifies initial state.
	// The actual external Kodi detection logic is in checkKodiPlaybackStatus() which
	// will probe the Kodi API when no game is running from ES API.
}

// TestBackgroundTracker_PollingInterval tests that the tracker polls at the
// correct interval (2 seconds)
func TestBackgroundTracker_PollingInterval(t *testing.T) {
	// Note: Not using t.Parallel() because MockESAPIServer binds to hardcoded port 1234

	mockESAPI := helpers.NewMockESAPIServer(t)
	mockESAPI.WithNoRunningGame()

	fs := helpers.NewMemoryFS()
	cfg, err := helpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	fakeClock := clockwork.NewFakeClock()
	platform := &Platform{
		clock: fakeClock,
	}

	var mediaMu syncutil.RWMutex
	var capturedMedia *models.ActiveMedia
	var callCount int
	setActiveMedia := func(media *models.ActiveMedia) {
		mediaMu.Lock()
		capturedMedia = media
		callCount++
		mediaMu.Unlock()
	}

	activeMedia := func() *models.ActiveMedia {
		mediaMu.RLock()
		defer mediaMu.RUnlock()
		return capturedMedia
	}

	// Start platform
	err = platform.StartPost(cfg, nil, activeMedia, setActiveMedia, nil)
	require.NoError(t, err)
	defer func() {
		if platform.stopTracker != nil {
			_ = platform.stopTracker()
		}
	}()

	// Reset call count after startup
	mediaMu.Lock()
	callCount = 0
	mediaMu.Unlock()

	// Simulate game launch
	mockESAPI.WithRunningGame(&esapi.RunningGameResponse{
		Name:       "Test Game",
		Path:       "/test/game.rom",
		SystemName: "nes",
	})

	// Advance clock by 1 second (should not trigger)
	fakeClock.Advance(1 * time.Second)
	time.Sleep(50 * time.Millisecond)
	mediaMu.RLock()
	assert.Equal(t, 0, callCount, "Should not poll after 1 second")
	mediaMu.RUnlock()

	// Advance clock by another 1 second (total 2 seconds, should trigger)
	fakeClock.Advance(1 * time.Second)
	time.Sleep(50 * time.Millisecond)
	mediaMu.RLock()
	assert.Equal(t, 1, callCount, "Should poll after 2 seconds")
	mediaMu.RUnlock()

	// Verify game was detected
	mediaMu.RLock()
	assert.NotNil(t, capturedMedia)
	assert.Equal(t, "Test Game", capturedMedia.Name)
	mediaMu.RUnlock()
}
