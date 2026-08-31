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
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playtime"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/profiles"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const (
	behaviorTimeout = 10 * time.Second
	noEventWait     = 200 * time.Millisecond
	testReaderID    = "test-reader-removable"
	testReaderSrc   = "test-reader-src"
)

type scanBehaviorEnv struct {
	st          *state.State
	cfg         *config.Instance
	userDB      *testhelpers.MockUserDBI
	svc         *ServiceContext
	launchHook  *launchHook
	historyHook *historyHook
	scanQueue   chan readers.Scan
	itq         chan tokens.Token
	clock       *clockwork.FakeClock
	launchCh    chan string
	stopCh      chan struct{}
	keyboardCh  chan string
	historyCh   chan database.HistoryEntry
	romsDir     string
}

// launchHook lets a test intercept the mock platform's LaunchMedia for one
// path before it publishes active media: it can block, panic, or record.
// The mock's LaunchMedia expectation is registered once with Maybe(), which
// never exhausts, so a per-test On() would never be consulted.
type launchHook struct {
	fn func(path string)
	mu syncutil.Mutex
}

func (h *launchHook) set(fn func(path string)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.fn = fn
}

func (h *launchHook) call(path string) {
	h.mu.Lock()
	fn := h.fn
	h.mu.Unlock()
	if fn != nil {
		fn(path)
	}
}

// historyHook intercepts a history write while it is in progress, so a test
// can prove what has and has not happened by the time it lands. Registered
// once with the AddHistory expectation for the same reason as launchHook.
type historyHook struct {
	fn func(he *database.HistoryEntry)
	mu syncutil.Mutex
}

func (h *historyHook) set(fn func(he *database.HistoryEntry)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.fn = fn
}

func (h *historyHook) call(he *database.HistoryEntry) {
	h.mu.Lock()
	fn := h.fn
	h.mu.Unlock()
	if fn != nil {
		fn(he)
	}
}

func setupScanBehavior(
	t *testing.T,
	scanMode string,
	exitDelay float32,
) *scanBehaviorEnv {
	t.Helper()

	tmpDir := t.TempDir()
	romsDir := filepath.Join(tmpDir, "roms")

	fs := testhelpers.NewMemoryFS()
	cfg, err := testhelpers.NewTestConfig(fs, tmpDir)
	require.NoError(t, err)

	cfg.SetScanMode(scanMode)
	cfg.SetScanExitDelay(exitDelay)
	require.NoError(t, cfg.LoadTOML(`[zapscript.input]
mode = "unrestricted"`))

	mockPlayer := mocks.NewMockPlayer()
	mockPlayer.SetupNoOpMock()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.SetupBasicMock()

	st, notifCh := state.NewState(mockPlatform, "test-boot-uuid")

	// CapabilityRemovable required for timedExit to arm.
	mockReader := mocks.NewMockReader()
	mockReader.On("Metadata").Return(readers.DriverMetadata{ID: "mock-reader"}).Maybe()
	mockReader.On("IDs").Return([]string{"mock:"}).Maybe()
	mockReader.On("Connected").Return(true).Maybe()
	mockReader.On("Path").Return("/dev/mock-device").Maybe()
	mockReader.On("Info").Return("Mock Removable Reader").Maybe()
	mockReader.On("Capabilities").Return([]readers.Capability{
		readers.CapabilityRemovable,
	}).Maybe()
	mockReader.On("ReaderID").Return(testReaderID).Maybe()
	mockReader.On("OnMediaChange", mock.Anything).Return(nil).Maybe()
	st.SetReader(mockReader)

	// historyCh mirrors every history write so tests can assert on outcomes
	// without reaching into the mock's call log. Sends never block: a test
	// that writes more entries than the buffer simply loses the oldest.
	historyCh := make(chan database.HistoryEntry, 64)

	mockUserDB := testhelpers.NewMockUserDBI()
	mockUserDB.On("GetEnabledMappings").Return([]database.Mapping{}, nil).Maybe()
	histHook := &historyHook{}
	mockUserDB.On("AddHistory", mock.Anything).Return(nil).Run(func(args mock.Arguments) {
		if he, ok := args.Get(0).(*database.HistoryEntry); ok && he != nil {
			histHook.call(he)
			select {
			case historyCh <- *he:
			default:
			}
		}
	}).Maybe()
	mockUserDB.On("GetSupportedZapLinkHosts").Return([]string{}, nil).Maybe()

	mockMediaDB := testhelpers.NewMockMediaDBI()

	db := &database.Database{
		UserDB:  mockUserDB,
		MediaDB: mockMediaDB,
	}

	launchCh := make(chan string, 10)
	stopCh := make(chan struct{}, 10)
	keyboardCh := make(chan string, 10)
	hook := &launchHook{}

	// LaunchMedia sets active media in state (simulating real platform behavior)
	// and signals launchCh so tests can observe launches.
	mockPlatform.On("LaunchMedia",
		mock.AnythingOfType("*config.Instance"),
		mock.AnythingOfType("string"),
		mock.Anything,
		mock.Anything,
		mock.Anything,
	).Return(nil).Run(func(args mock.Arguments) {
		path := args.String(1)
		hook.call(path)
		media := &models.ActiveMedia{
			SystemID: "mock",
			Path:     path,
			Name:     path,
		}
		opts, ok := args.Get(4).(*platforms.LaunchOptions)
		if ok && opts != nil && opts.ActiveMediaPublisher != nil {
			opts.ActiveMediaPublisher(media)
		} else {
			st.SetActiveMedia(media)
		}
		launchCh <- path
	}).Maybe()

	mockPlatform.On("StopActiveLauncher",
		mock.AnythingOfType("platforms.StopIntent"),
	).Return(nil).Run(func(_ mock.Arguments) {
		st.SetActiveMedia(nil)
		stopCh <- struct{}{}
	}).Maybe()

	mockPlatform.On("KeyboardPress",
		mock.AnythingOfType("string"),
	).Return(nil).Run(func(args mock.Arguments) {
		keyboardCh <- args.String(0)
	}).Maybe()

	mockPlatform.On("ScanHook", mock.Anything).Return(nil).Maybe()
	mockPlatform.On("LookupMapping", mock.Anything).Return("", false).Maybe()
	mockPlatform.On("ConsoleManager").Return(platforms.NoOpConsoleManager{}).Maybe()

	fakeClock := clockwork.NewFakeClock()

	// lsq is buffered so goroutines spawned by processTokenQueue and timedExit
	// can complete their sends after context cancellation.
	scanQueue := make(chan readers.Scan)
	itq := make(chan tokens.Token)
	lsq := make(chan *tokens.Token, 10)
	plq := make(chan *playlists.Playlist, 10)

	limitsManager := playtime.NewLimitsManager(db, mockPlatform, cfg, nil, mockPlayer)

	svc := &ServiceContext{
		Platform:            mockPlatform,
		Config:              cfg,
		State:               st,
		DB:                  db,
		Profiles:            profiles.NewService(db, st),
		LaunchSoftwareQueue: lsq,
		PlaylistQueue:       plq,
		BackgroundWG:        &sync.WaitGroup{},
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		readerManager(svc, itq, scanQueue, mockPlayer, fakeClock)
	}()
	go func() {
		defer wg.Done()
		processTokenQueue(svc, itq, limitsManager, mockPlayer)
	}()

	t.Cleanup(func() {
		st.StopService()
		wg.Wait()
		// Launch goroutines outlive the worker; join them before goleak looks.
		svc.BackgroundWG.Wait()
		for {
			select {
			case <-notifCh:
			case <-lsq:
			default:
				return
			}
		}
	})

	return &scanBehaviorEnv{
		st:          st,
		cfg:         cfg,
		userDB:      mockUserDB,
		svc:         svc,
		launchHook:  hook,
		historyHook: histHook,
		scanQueue:   scanQueue,
		itq:         itq,
		clock:       fakeClock,
		romsDir:     romsDir,
		launchCh:    launchCh,
		stopCh:      stopCh,
		keyboardCh:  keyboardCh,
		historyCh:   historyCh,
	}
}

// --- Scan helpers ---

// gamePath returns a platform-appropriate absolute path for a game file.
func (env *scanBehaviorEnv) gamePath(name string) string {
	return filepath.Join(env.romsDir, name)
}

func (env *scanBehaviorEnv) sendGameScan(uid, path string) {
	env.scanQueue <- readers.Scan{
		Source:   testReaderSrc,
		ReaderID: testReaderID,
		Token: &tokens.Token{
			UID:      uid,
			Text:     path,
			ScanTime: time.Now(),
			Source:   tokens.SourceReader,
			ReaderID: testReaderID,
		},
	}
}

func (env *scanBehaviorEnv) sendCommandScan(uid, cmd string) {
	env.scanQueue <- readers.Scan{
		Source:   testReaderSrc,
		ReaderID: testReaderID,
		Token: &tokens.Token{
			UID:      uid,
			Text:     cmd,
			ScanTime: time.Now(),
			Source:   tokens.SourceReader,
			ReaderID: testReaderID,
		},
	}
}

// sendTraitScan scans a card whose ZapScript carries scan-mode traits ahead of
// the rest of the script, e.g. "#tap||/roms/game.rom".
func (env *scanBehaviorEnv) sendTraitScan(uid, traits, script string) {
	env.sendCommandScan(uid, traits+"||"+script)
}

// sendRemoval reports that the reader no longer sees a token. Every real
// driver labels its scans, removals included, so the harness does too.
func (env *scanBehaviorEnv) sendRemoval() {
	env.scanQueue <- readers.Scan{
		Source:   testReaderSrc,
		ReaderID: testReaderID,
		Token:    nil,
	}
}

// --- Second reader ---
//
// A device can have more than one reader, which is the whole point of a
// per-reader scan mode. These drive a second, independently addressed reader.

const (
	testReader2ID   = "test-reader-removable-2"
	testReader2Path = "/dev/mock-device-2"
)

// addSecondReader registers another removable reader, on its own path so
// per-reader configuration can tell the two apart.
func (env *scanBehaviorEnv) addSecondReader(t *testing.T) {
	t.Helper()
	r := mocks.NewMockReader()
	r.On("Metadata").Return(readers.DriverMetadata{ID: "mock-reader"}).Maybe()
	r.On("IDs").Return([]string{"mock:"}).Maybe()
	r.On("Connected").Return(true).Maybe()
	r.On("Path").Return(testReader2Path).Maybe()
	r.On("Info").Return("Mock Removable Reader 2").Maybe()
	r.On("Capabilities").Return([]readers.Capability{
		readers.CapabilityRemovable,
	}).Maybe()
	r.On("ReaderID").Return(testReader2ID).Maybe()
	r.On("OnMediaChange", mock.Anything).Return(nil).Maybe()
	env.st.SetReader(r)
}

func (env *scanBehaviorEnv) sendScanOn(readerID, uid, text string) {
	env.scanQueue <- readers.Scan{
		Source:   testReaderSrc,
		ReaderID: readerID,
		Token: &tokens.Token{
			UID:      uid,
			Text:     text,
			ScanTime: time.Now(),
			Source:   tokens.SourceReader,
			ReaderID: readerID,
		},
	}
}

func (env *scanBehaviorEnv) sendRemovalOn(readerID string) {
	env.scanQueue <- readers.Scan{
		Source:   testReaderSrc,
		ReaderID: readerID,
		Token:    nil,
	}
}

// --- Observation helpers ---

func (env *scanBehaviorEnv) waitForLaunch(t *testing.T) string {
	t.Helper()
	select {
	case path := <-env.launchCh:
		return path
	case <-time.After(behaviorTimeout):
		t.Fatal("timed out waiting for LaunchMedia")
		return ""
	}
}

func (env *scanBehaviorEnv) expectNoLaunch(t *testing.T) {
	t.Helper()
	select {
	case path := <-env.launchCh:
		t.Fatalf("unexpected LaunchMedia call with path: %s", path)
	case <-time.After(noEventWait):
	}
}

func (env *scanBehaviorEnv) waitForStop(t *testing.T) {
	t.Helper()
	select {
	case <-env.stopCh:
	case <-time.After(behaviorTimeout):
		t.Fatal("timed out waiting for StopActiveLauncher")
	}
}

func (env *scanBehaviorEnv) expectNoStop(t *testing.T) {
	t.Helper()
	select {
	case <-env.stopCh:
		t.Fatal("unexpected StopActiveLauncher call")
	case <-time.After(noEventWait):
	}
}

func (env *scanBehaviorEnv) waitForKeyboard(t *testing.T) string {
	t.Helper()
	select {
	case key := <-env.keyboardCh:
		return key
	case <-time.After(behaviorTimeout):
		t.Fatal("timed out waiting for KeyboardPress")
		return ""
	}
}

// waitForSoftwareToken polls until processTokenQueue has sent the software
// token back through lsq and readerManager has set it in state.
func (env *scanBehaviorEnv) waitForSoftwareToken(t *testing.T) {
	t.Helper()
	env.waitForSoftwareTokenUID(t, "")
}

func (env *scanBehaviorEnv) waitForSoftwareTokenUID(t *testing.T, uid string) {
	t.Helper()
	deadline := time.After(behaviorTimeout)
	for {
		softwareToken := env.st.GetSoftwareToken()
		if softwareToken != nil && (uid == "" || softwareToken.UID == uid) {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for software token UID=%q", uid)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// waitForActiveCard polls until readerManager has processed a scan and set
// the active card to the expected UID. Note: SetActiveCard executes before
// exitTimer.Stop() in the same goroutine, so use waitForTimerStopped after
// this if you need to guarantee the timer has been cancelled.
func (env *scanBehaviorEnv) waitForActiveCard(t *testing.T, uid string) {
	t.Helper()
	deadline := time.After(behaviorTimeout)
	for {
		if env.st.GetActiveCard().UID == uid {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for active card UID=%q", uid)
		case <-time.After(time.Millisecond):
		}
	}
}

// waitForTimerStopped polls until the exit timer has been stopped, verified by
// the fake clock having no remaining waiters.
func (env *scanBehaviorEnv) waitForTimerStopped(t *testing.T) {
	t.Helper()
	deadline := time.After(behaviorTimeout)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
		err := env.clock.BlockUntilContext(ctx, 1)
		cancel()
		if err != nil {
			return
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for exit timer to be stopped")
		case <-time.After(time.Millisecond):
		}
	}
}

func (env *scanBehaviorEnv) waitForPlaylistCleared(t *testing.T) {
	t.Helper()
	deadline := time.After(behaviorTimeout)
	for {
		if env.st.GetActivePlaylist() == nil {
			return
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for active playlist to clear")
		case <-time.After(time.Millisecond):
		}
	}
}

// simulateManualExit mimics a user quitting a game through the game's own UI.
// Platforms detect this and call setActiveMedia(nil). The software token is NOT
// cleared — only the service layer clears it via the lsq channel.
func (env *scanBehaviorEnv) simulateManualExit() {
	env.st.SetActiveMedia(nil)
}

// ============================================================================
// Tap mode tests
// ============================================================================

func TestScanBehavior_Tap_RemovalDoesNotCloseGame(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeTap, 0)

	env.sendGameScan("game1", env.gamePath("game.rom"))
	env.waitForLaunch(t)

	env.sendRemoval()
	env.expectNoStop(t)
}

func TestScanBehavior_Tap_DuplicateSuppression(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeTap, 0)

	env.sendGameScan("game1", env.gamePath("game.rom"))
	env.waitForLaunch(t)

	// Same card again — should be suppressed.
	env.sendGameScan("game1", env.gamePath("game.rom"))
	env.expectNoLaunch(t)
}

func TestScanBehavior_Tap_DifferentCardLaunchesDirectly(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeTap, 0)

	env.sendGameScan("gameA", env.gamePath("gameA.rom"))
	require.Equal(t, env.gamePath("gameA.rom"), env.waitForLaunch(t))
	env.waitForSoftwareToken(t)

	env.sendGameScan("gameB", env.gamePath("gameB.rom"))
	require.Equal(t, env.gamePath("gameB.rom"), env.waitForLaunch(t))

	select {
	case <-env.stopCh:
		t.Fatal("StopActiveLauncher should not have been called between launches")
	default:
	}
}

func TestScanBehavior_Tap_SameCardAfterRemoveReloads(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeTap, 0)

	env.sendGameScan("game1", env.gamePath("game.rom"))
	env.waitForLaunch(t)

	env.sendRemoval()

	// Re-tap same card — should launch again (prevToken cleared by removal).
	env.sendGameScan("game1", env.gamePath("game.rom"))
	env.waitForLaunch(t)
}

func TestScanBehavior_Tap_CommandDoesNotInterruptGame(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeTap, 0)

	env.sendGameScan("game1", env.gamePath("game.rom"))
	env.waitForLaunch(t)

	env.sendCommandScan("cmd1", "**input.keyboard:coin")
	env.waitForKeyboard(t)

	env.expectNoStop(t)
}

func TestScanBehavior_Tap_ManualExitResetsState(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeTap, 0)

	env.sendGameScan("gameA", env.gamePath("gameA.rom"))
	env.waitForLaunch(t)
	// LaunchMedia returns before processTokenQueue completes the software-token
	// roundtrip. Wait for it so the manual exit cannot race that prior launch.
	env.waitForSoftwareToken(t)

	env.simulateManualExit()

	env.sendGameScan("gameB", env.gamePath("gameB.rom"))
	require.Equal(t, env.gamePath("gameB.rom"), env.waitForLaunch(t))
}

func TestScanBehavior_Tap_ManualExitWithCardNoRelaunch(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeTap, 0)

	env.sendGameScan("game1", env.gamePath("game.rom"))
	env.waitForLaunch(t)

	// User manually exits — card still on reader (no removal sent).
	env.simulateManualExit()
	env.expectNoLaunch(t)
}

// ============================================================================
// Hold mode immediate (exit_delay=0) tests
// ============================================================================

func TestScanBehavior_HoldImmediate_RemovalClosesGame(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeHold, 0)

	env.sendGameScan("game1", env.gamePath("game.rom"))
	env.waitForLaunch(t)
	env.waitForSoftwareToken(t)

	env.sendRemoval()
	env.waitForStop(t)
}

// TestScanBehavior_HoldImmediate_OnRemoveDisabledZapScriptStillClosesGame pins
// that a disabled "run ZapScript" setting is treated as the on_remove hook's
// prior silent no-op, not a hook failure: without hookErrorBlocks excluding
// the sentinel, readers.go's on_remove call site would treat it as a real
// hook failure and leave the game running on removal.
func TestScanBehavior_HoldImmediate_OnRemoveDisabledZapScriptStillClosesGame(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeHold, 0)
	require.NoError(t, env.cfg.LoadTOML(`[readers.scan]
mode = "hold"
on_remove = "**echo:should not run"`))

	env.sendGameScan("game1", env.gamePath("game.rom"))
	env.waitForLaunch(t)
	env.waitForSoftwareToken(t)

	// Disable only after the game is running, since this also gates the
	// launch command itself, not just the on_remove hook under test.
	env.st.SetRunZapScript(false)

	env.sendRemoval()
	env.waitForStop(t)
}

func TestScanBehavior_HoldImmediate_FastRemovalClosesAfterLaunchOwnershipArrives(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeHold, 0)

	env.sendGameScan("game1", env.gamePath("game.rom"))
	env.waitForLaunch(t)
	env.sendRemoval()

	env.waitForStop(t)
}

func TestScanBehavior_HoldImmediate_UtilityRemovalDoesNotCloseGame(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeHold, 0)

	env.sendGameScan("game1", env.gamePath("game.rom"))
	env.waitForLaunch(t)
	env.waitForSoftwareTokenUID(t, "game1")

	env.sendCommandScan("keyboard", "**input.keyboard:{f2}")
	env.waitForKeyboard(t)
	env.sendRemoval()

	env.expectNoStop(t)
	env.waitForSoftwareTokenUID(t, "game1")
}

func TestScanBehavior_HoldImmediate_OwnerRemovalClearsPrimaryPlaylist(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeHold, 0)

	env.sendGameScan("game1", env.gamePath("game.rom"))
	env.waitForLaunch(t)
	env.waitForSoftwareTokenUID(t, "game1")
	playlist := playlists.NewPlaylist("playlist", "Playlist", []playlists.PlaylistItem{{ZapScript: "game"}})
	playlist.Playing = true
	env.st.SetActivePlaylist(playlist)

	env.sendRemoval()
	env.waitForStop(t)
	env.waitForPlaylistCleared(t)
}

func TestScanBehavior_HoldImmediate_ManualExitNoRelaunch(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeHold, 0)

	env.sendGameScan("game1", env.gamePath("game.rom"))
	env.waitForLaunch(t)

	// User manually exits while card is still on reader.
	env.simulateManualExit()
	env.expectNoLaunch(t)
}

func TestScanBehavior_HoldImmediate_ManualExitThenRemoveNoReload(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeHold, 0)

	env.sendGameScan("game1", env.gamePath("game.rom"))
	env.waitForLaunch(t)

	env.simulateManualExit()

	// Remove card after manual exit — should NOT trigger stop (already stopped).
	env.sendRemoval()
	env.expectNoStop(t)
}

// ============================================================================
// Hold mode delayed tests
// ============================================================================

func TestScanBehavior_HoldDelayedOnRemove_ReinsertCancelsHookWithoutRelaunch(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeHold, 0)
	require.NoError(t, env.cfg.LoadTOML(`[readers.scan]
mode = "hold"
on_remove = '**delay:10s||**input.keyboard:{escape}'`))

	env.sendGameScan("game1", env.gamePath("game.rom"))
	env.waitForLaunch(t)
	env.waitForSoftwareToken(t)

	env.sendRemoval()
	env.sendGameScan("game1", env.gamePath("game.rom"))
	env.waitForActiveCard(t, "game1")

	env.expectNoLaunch(t)
	env.expectNoStop(t)
	select {
	case key := <-env.keyboardCh:
		t.Fatalf("unexpected delayed on_remove keyboard command: %s", key)
	case <-time.After(noEventWait):
	}
}

func TestScanBehavior_HoldDelayedOnRemove_AbsentTokenCompletesHookAndExit(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeHold, 0)
	require.NoError(t, env.cfg.LoadTOML(`[readers.scan]
mode = "hold"
on_remove = '**delay:10||**input.keyboard:{escape}'`))

	env.sendGameScan("game1", env.gamePath("game.rom"))
	env.waitForLaunch(t)
	env.waitForSoftwareToken(t)

	env.sendRemoval()
	require.Equal(t, "{escape}", env.waitForKeyboard(t))
	env.waitForStop(t)
}

// TestScanBehavior_HoldDelayedOnRemove_DisabledZapScriptStillClosesGame pins
// the same fix as the immediate on_remove variant, for the delayed hook path
// consumed via removalHookResults: a disabled "run ZapScript" setting must
// not block the exit, and since the whole hook script (including its delay
// command) never runs when disabled, the keyboard command it would have sent
// must not fire either.
func TestScanBehavior_HoldDelayedOnRemove_DisabledZapScriptStillClosesGame(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeHold, 0)
	require.NoError(t, env.cfg.LoadTOML(`[readers.scan]
mode = "hold"
on_remove = '**delay:10||**input.keyboard:{escape}'`))

	env.sendGameScan("game1", env.gamePath("game.rom"))
	env.waitForLaunch(t)
	env.waitForSoftwareToken(t)

	env.st.SetRunZapScript(false)

	env.sendRemoval()
	env.waitForStop(t)
	select {
	case key := <-env.keyboardCh:
		t.Fatalf("unexpected keyboard command from a hook that should not have run: %s", key)
	case <-time.After(noEventWait):
	}
}

func TestScanBehavior_HoldDelayed_RemovalClosesAfterDelay(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeHold, 5)

	env.sendGameScan("game1", env.gamePath("game.rom"))
	env.waitForLaunch(t)
	env.waitForSoftwareToken(t)

	env.sendRemoval()
	env.expectNoStop(t)

	env.clock.Advance(5 * time.Second)
	env.waitForStop(t)
}

func TestScanBehavior_HoldDelayed_ReinsertCancelsExit(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeHold, 5)

	env.sendGameScan("game1", env.gamePath("game.rom"))
	env.waitForLaunch(t)
	env.waitForSoftwareToken(t)

	env.sendRemoval()
	env.sendGameScan("game1", env.gamePath("game.rom"))
	env.waitForActiveCard(t, "game1")
	env.waitForTimerStopped(t)

	env.clock.Advance(10 * time.Second)
	env.expectNoStop(t)
	env.expectNoLaunch(t)
}

func TestScanBehavior_HoldDelayed_DifferentCardLaunchesImmediately(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeHold, 5)

	env.sendGameScan("gameA", env.gamePath("gameA.rom"))
	require.Equal(t, env.gamePath("gameA.rom"), env.waitForLaunch(t))
	env.waitForSoftwareTokenUID(t, "gameA")

	env.sendRemoval()
	env.sendGameScan("gameB", env.gamePath("gameB.rom"))
	require.Equal(t, env.gamePath("gameB.rom"), env.waitForLaunch(t))
	env.waitForSoftwareTokenUID(t, "gameB")

	env.clock.Advance(10 * time.Second)
	env.expectNoStop(t)
}

func TestScanBehavior_HoldDelayed_CommandDoesNotResetCountdown(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeHold, 5)

	env.sendGameScan("game1", env.gamePath("game.rom"))
	env.waitForLaunch(t)
	env.waitForSoftwareToken(t)

	env.sendRemoval()

	env.sendCommandScan("cmd1", "**input.keyboard:{f2}")
	env.waitForKeyboard(t)
	env.clock.Advance(4 * time.Second)
	env.expectNoStop(t)

	env.sendCommandScan("cmd2", "**input.keyboard:{f3}")
	env.waitForKeyboard(t)
	env.clock.Advance(time.Second)
	env.waitForStop(t)
}

func TestScanBehavior_HoldDelayed_ManualExitNoRelaunch(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeHold, 5)

	env.sendGameScan("game1", env.gamePath("game.rom"))
	env.waitForLaunch(t)

	env.simulateManualExit()
	env.expectNoLaunch(t)
}

func TestScanBehavior_HoldDelayed_ManualExitThenRemoveNoReload(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeHold, 5)

	env.sendGameScan("game1", env.gamePath("game.rom"))
	env.waitForLaunch(t)

	env.simulateManualExit()

	env.sendRemoval()
	env.expectNoStop(t)
}

func TestScanBehavior_HoldDelayed_ManualExitDuringCountdownCancels(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeHold, 5)

	env.sendGameScan("game1", env.gamePath("game.rom"))
	env.waitForLaunch(t)
	env.waitForSoftwareToken(t)

	env.sendRemoval()

	// Timer goroutine will see no active media and bail out.
	env.simulateManualExit()
	env.clock.Advance(10 * time.Second)
	env.expectNoStop(t)
}

// ============================================================================
// Per-token scan mode override (#tap / #hold traits)
// ============================================================================

// A #tap card opts out of hold mode: removing it leaves the game running even
// though the device is globally in hold mode.
func TestScanBehavior_Hold_TapTraitRemovalDoesNotCloseGame(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeHold, 0)

	env.sendTraitScan("game1", "#tap", env.gamePath("game.rom"))
	env.waitForLaunch(t)
	env.waitForSoftwareTokenUID(t, "game1")

	env.sendRemoval()
	env.expectNoStop(t)
}

// The override rides on the token that owns hold-mode exit, so the launch is
// still tracked; it simply never exits.
func TestScanBehavior_Hold_TapTraitRecordsTapPolicyOnOwner(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeHold, 0)

	env.sendTraitScan("game1", "#tap", env.gamePath("game.rom"))
	env.waitForLaunch(t)
	env.waitForSoftwareTokenUID(t, "game1")

	owner := env.st.GetSoftwareToken()
	require.NotNil(t, owner)
	assert.Equal(t, config.ScanModeTap, owner.Traits.ScanMode())
}

// Regression: a #tap launch must take ownership away from the card that owned
// the previous game, or removing that older card would stop media it never
// launched.
func TestScanBehavior_Hold_TapTraitLaunchClearsPreviousOwner(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeHold, 0)

	env.sendGameScan("holdCard", env.gamePath("gameA.rom"))
	require.Equal(t, env.gamePath("gameA.rom"), env.waitForLaunch(t))
	env.waitForSoftwareTokenUID(t, "holdCard")

	env.sendRemoval()
	env.waitForStop(t)

	env.sendTraitScan("tapCard", "#tap", env.gamePath("gameB.rom"))
	require.Equal(t, env.gamePath("gameB.rom"), env.waitForLaunch(t))
	env.waitForSoftwareTokenUID(t, "tapCard")

	// Removing the tap card must not stop the game it launched.
	env.sendRemoval()
	env.expectNoStop(t)
}

// Regression: swapping a live hold owner for a #tap card must move ownership
// to the new card. Leaving the older card as the owner would let its later
// removal stop media it never launched.
func TestScanBehavior_Hold_TapTraitLaunchReplacesLiveOwner(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeHold, 0)

	env.sendGameScan("holdCard", env.gamePath("gameA.rom"))
	require.Equal(t, env.gamePath("gameA.rom"), env.waitForLaunch(t))
	env.waitForSoftwareTokenUID(t, "holdCard")

	// No removal in between, so the replacement launch takes ownership from a
	// hold owner that is still live.
	env.sendTraitScan("tapCard", "#tap", env.gamePath("gameB.rom"))
	require.Equal(t, env.gamePath("gameB.rom"), env.waitForLaunch(t))
	env.waitForSoftwareTokenUID(t, "tapCard")

	owner := env.st.GetSoftwareToken()
	require.NotNil(t, owner)
	assert.Equal(t, config.ScanModeTap, owner.Traits.ScanMode())

	env.sendRemoval()
	env.expectNoStop(t)
}

// A #hold card opts in to hold mode on a device that is globally tap.
func TestScanBehavior_Tap_HoldTraitRemovalClosesGame(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeTap, 0)

	env.sendTraitScan("game1", "#hold", env.gamePath("game.rom"))
	env.waitForLaunch(t)
	env.waitForSoftwareTokenUID(t, "game1")

	env.sendRemoval()
	env.waitForStop(t)
}

func TestScanBehavior_Tap_HoldTraitRemovalClosesGameAfterDelay(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeTap, 5)

	env.sendTraitScan("game1", "#hold", env.gamePath("game.rom"))
	env.waitForLaunch(t)
	env.waitForSoftwareTokenUID(t, "game1")

	env.sendRemoval()
	env.expectNoStop(t)

	env.clock.Advance(5 * time.Second)
	env.waitForStop(t)
}

// Traits that contradict each other are ignored, so the card behaves exactly
// like one carrying no override at all.
func TestScanBehavior_Hold_ConflictingTraitsInheritGlobalMode(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeHold, 0)

	env.sendTraitScan("game1", "#tap #hold", env.gamePath("game.rom"))
	env.waitForLaunch(t)
	env.waitForSoftwareTokenUID(t, "game1")

	owner := env.st.GetSoftwareToken()
	require.NotNil(t, owner)
	assert.Empty(t, owner.Traits.ScanMode())

	env.sendRemoval()
	env.waitForStop(t)
}

// A #tap card that only runs a control command changes nothing: the card that
// launched the running game keeps hold ownership, and its own removal still
// exits.
func TestScanBehavior_Hold_TapTraitControlCardPreservesHoldOwner(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeHold, 0)

	env.sendGameScan("game1", env.gamePath("game.rom"))
	env.waitForLaunch(t)
	env.waitForSoftwareTokenUID(t, "game1")

	env.sendTraitScan("keyboard", "#tap", "**input.keyboard:{f2}")
	env.waitForKeyboard(t)
	env.sendRemoval()

	env.expectNoStop(t)
	env.waitForSoftwareTokenUID(t, "game1")

	env.sendGameScan("game1", env.gamePath("game.rom"))
	env.waitForActiveCard(t, "game1")
	env.sendRemoval()
	env.waitForStop(t)
}

// The on_remove hook belongs to hold-mode removal, so a card that opted out of
// hold mode must not fire it.
func TestScanBehavior_Hold_TapTraitSkipsOnRemoveHook(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeHold, 0)
	require.NoError(t, env.cfg.LoadTOML(`[readers.scan]
mode = "hold"
on_remove = "**input.keyboard:{f9}"`))

	env.sendTraitScan("game1", "#tap", env.gamePath("game.rom"))
	env.waitForLaunch(t)
	env.waitForSoftwareTokenUID(t, "game1")

	env.sendRemoval()
	env.expectNoStop(t)

	select {
	case key := <-env.keyboardCh:
		t.Fatalf("on_remove hook ran for a tap-overridden token, pressed %q", key)
	case <-time.After(noEventWait):
	}
}

// Rescanning the same #tap card repeatedly must not accumulate state that
// later resurrects it as a hold owner.
func TestScanBehavior_Hold_TapTraitRescanNeverExits(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeHold, 0)

	for range 3 {
		env.sendTraitScan("game1", "#tap", env.gamePath("game.rom"))
		env.waitForLaunch(t)
		env.waitForActiveCard(t, "game1")
		env.sendRemoval()
		env.expectNoStop(t)
	}

	owner := env.st.GetSoftwareToken()
	require.NotNil(t, owner)
	assert.Equal(t, config.ScanModeTap, owner.Traits.ScanMode())
}

// ============================================================================
// Per-reader scan mode (readers.drivers / readers.connect)
// ============================================================================

// The mock reader in this harness reports driver "mock-reader" on path
// "/dev/mock-device", which is what the per-reader config below keys off.
func TestScanBehavior_PerReader_TapDriverInHoldGlobal(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeHold, 0)
	require.NoError(t, env.cfg.LoadTOML(`[readers.drivers.mock-reader]
scan_mode = "tap"`))

	env.sendGameScan("game1", env.gamePath("game.rom"))
	env.waitForLaunch(t)
	env.waitForSoftwareTokenUID(t, "game1")

	env.sendRemoval()
	env.expectNoStop(t)
}

func TestScanBehavior_PerReader_HoldDriverInTapGlobal(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeTap, 0)
	require.NoError(t, env.cfg.LoadTOML(`[readers.drivers.mock-reader]
scan_mode = "hold"`))

	env.sendGameScan("game1", env.gamePath("game.rom"))
	env.waitForLaunch(t)
	env.waitForSoftwareTokenUID(t, "game1")

	env.sendRemoval()
	env.waitForStop(t)
}

func TestScanBehavior_PerReader_ConnectEntryOverridesDriver(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeTap, 0)
	require.NoError(t, env.cfg.LoadTOML(`[readers.drivers.mock-reader]
scan_mode = "tap"

[[readers.connect]]
driver = "mock-reader"
path = "/dev/mock-device"
scan_mode = "hold"`))

	env.sendGameScan("game1", env.gamePath("game.rom"))
	env.waitForLaunch(t)
	env.waitForSoftwareTokenUID(t, "game1")

	env.sendRemoval()
	env.waitForStop(t)
}

// A token trait outranks the reader's configured mode.
func TestScanBehavior_PerReader_TokenTraitBeatsReaderConfig(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeTap, 0)
	require.NoError(t, env.cfg.LoadTOML(`[readers.drivers.mock-reader]
scan_mode = "hold"`))

	env.sendTraitScan("game1", "#tap", env.gamePath("game.rom"))
	env.waitForLaunch(t)
	env.waitForSoftwareTokenUID(t, "game1")

	env.sendRemoval()
	env.expectNoStop(t)
}

// ============================================================================
// Two readers at once
//
// Scans were tracked in a single slot shared by every reader, so one reader's
// activity decided what another reader's removal meant. These pin the two ways
// that went wrong, both of which left hold-mode media running forever.
// ============================================================================

// A removal is the removal of what was on the reader that reported it, not of
// whatever was scanned last anywhere.
func TestScanBehavior_TwoReaders_RemovalAttributedToItsOwnReader(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeHold, 0)
	env.addSecondReader(t)

	// The game card owns hold-mode exit, scanned on reader one.
	env.sendScanOn(testReaderID, "game1", env.gamePath("game.rom"))
	env.waitForLaunch(t)
	env.waitForSoftwareTokenUID(t, "game1")

	// A command card is then put on reader two and left there.
	env.sendScanOn(testReader2ID, "cmd1", "**input.keyboard:a")
	env.waitForKeyboard(t)

	// Lifting the game card from reader one must exit: it is that card being
	// removed, not the command card that happened to be scanned more recently.
	env.sendRemovalOn(testReaderID)
	env.waitForStop(t)
}

// One reader's removal must not make another reader's removal look like a
// repeat of it.
func TestScanBehavior_TwoReaders_SecondRemovalIsNotADuplicate(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeHold, 0)
	env.addSecondReader(t)

	env.sendScanOn(testReaderID, "game1", env.gamePath("game.rom"))
	env.waitForLaunch(t)
	env.waitForSoftwareTokenUID(t, "game1")

	// A command card is tapped on reader two and taken away again.
	env.sendScanOn(testReader2ID, "cmd1", "**input.keyboard:a")
	env.waitForKeyboard(t)
	env.sendRemovalOn(testReader2ID)

	// Reader one's own removal still has to register.
	env.sendRemovalOn(testReaderID)
	env.waitForStop(t)
}

// A per-reader mode only applies to its own reader, and each reader's removal
// is judged by that reader's mode.
func TestScanBehavior_TwoReaders_HoldAndTapSideBySide(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeTap, 0)
	env.addSecondReader(t)
	require.NoError(t, env.cfg.LoadTOML(`[[readers.connect]]
driver = "mock-reader"
path = "/dev/mock-device"
scan_mode = "hold"

[[readers.connect]]
driver = "mock-reader"
path = "/dev/mock-device-2"
scan_mode = "tap"`))

	// The tap reader launches and keeps running when its card is lifted.
	env.sendScanOn(testReader2ID, "game2", env.gamePath("tap.rom"))
	env.waitForLaunch(t)
	env.waitForSoftwareTokenUID(t, "game2")
	env.sendRemovalOn(testReader2ID)
	env.expectNoStop(t)

	// The hold reader launches and exits when its card is lifted.
	env.sendScanOn(testReaderID, "game1", env.gamePath("hold.rom"))
	env.waitForLaunch(t)
	env.waitForSoftwareTokenUID(t, "game1")
	env.sendRemovalOn(testReaderID)
	env.waitForStop(t)
}

// ============================================================================
// A dead exit timer is not a pending exit
//
// The hold owner stays recorded long after its countdown is over, so treating
// "a timer object exists" as "an exit is pending" swallowed later scans of
// that card with nothing logged.
// ============================================================================

// The timer fired but decided against exiting, because the media had already
// stopped. Scanning the same card again must launch it.
func TestScanBehavior_Hold_RescanAfterTimerFoundNoMedia(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeHold, 0)

	env.sendGameScan("game1", env.gamePath("game.rom"))
	env.waitForLaunch(t)
	env.waitForSoftwareTokenUID(t, "game1")

	// The game exits by itself, then the card is lifted: the countdown runs,
	// finds nothing to close, and leaves the card recorded as the hold owner.
	env.simulateManualExit()
	env.sendRemoval()
	env.waitForActiveCard(t, "")

	// Putting the same card back must start the game again.
	env.sendGameScan("game1", env.gamePath("game.rom"))
	env.waitForLaunch(t)
}

// Reinsertion during a real countdown still cancels the exit rather than
// relaunching, which is what the dead-timer check must not break.
func TestScanBehavior_HoldDelayed_ReinsertionStillCancelsExit(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeHold, 5)

	env.sendGameScan("game1", env.gamePath("game.rom"))
	env.waitForLaunch(t)
	env.waitForSoftwareTokenUID(t, "game1")

	env.sendRemoval()
	env.waitForActiveCard(t, "")

	// Back on the reader before the countdown ends: no exit, no relaunch.
	env.sendGameScan("game1", env.gamePath("game.rom"))
	env.waitForActiveCard(t, "game1")
	env.expectNoLaunch(t)

	env.clock.Advance(10 * time.Second)
	env.expectNoStop(t)
}

// A reader in tap mode never arms a timer, so a repeat tap of the same card
// reloads the game every time even after another reader has run an exit.
func TestScanBehavior_Tap_RepeatTapsAfterAHoldReaderArmedATimer(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeTap, 0)
	env.addSecondReader(t)
	require.NoError(t, env.cfg.LoadTOML(`[[readers.connect]]
driver = "mock-reader"
path = "/dev/mock-device"
scan_mode = "hold"`))

	// The hold reader launches and its removal arms and runs an exit.
	env.sendScanOn(testReaderID, "game1", env.gamePath("hold.rom"))
	env.waitForLaunch(t)
	env.waitForSoftwareTokenUID(t, "game1")
	env.sendRemovalOn(testReaderID)
	env.waitForStop(t)

	// From then on the tap reader must keep reloading on every tap.
	for range 2 {
		env.sendScanOn(testReader2ID, "game2", env.gamePath("tap.rom"))
		env.waitForLaunch(t)
		env.waitForSoftwareTokenUID(t, "game2")
		env.sendRemovalOn(testReader2ID)
		env.expectNoStop(t)
	}
}
