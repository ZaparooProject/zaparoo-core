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
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playtime"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const (
	behaviorTimeout = 3 * time.Second
	noEventWait     = 200 * time.Millisecond
	testReaderID    = "test-reader-removable"
	testReaderSrc   = "test-reader-src"
)

type scanBehaviorEnv struct {
	st        *state.State
	cfg       *config.Instance
	scanQueue chan readers.Scan
	clock     *clockwork.FakeClock

	launchCh   chan string
	stopCh     chan struct{}
	keyboardCh chan string
}

func setupScanBehavior(
	t *testing.T,
	scanMode string,
	exitDelay float32,
) *scanBehaviorEnv {
	t.Helper()

	fs := testhelpers.NewMemoryFS()
	cfg, err := testhelpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	cfg.SetScanMode(scanMode)
	cfg.SetScanExitDelay(exitDelay)

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

	mockUserDB := testhelpers.NewMockUserDBI()
	mockUserDB.On("GetEnabledMappings").Return([]database.Mapping{}, nil).Maybe()
	mockUserDB.On("AddHistory", mock.Anything).Return(nil).Maybe()
	mockUserDB.On("GetSupportedZapLinkHosts").Return([]string{}, nil).Maybe()

	mockMediaDB := testhelpers.NewMockMediaDBI()

	db := &database.Database{
		UserDB:  mockUserDB,
		MediaDB: mockMediaDB,
	}

	launchCh := make(chan string, 10)
	stopCh := make(chan struct{}, 10)
	keyboardCh := make(chan string, 10)

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
		st.SetActiveMedia(&models.ActiveMedia{
			SystemID: "mock",
			Path:     path,
			Name:     path,
		})
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

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		readerManager(mockPlatform, cfg, st, db, itq, lsq, plq, scanQueue, mockPlayer, fakeClock)
	}()
	go func() {
		defer wg.Done()
		processTokenQueue(mockPlatform, cfg, st, itq, db, lsq, plq, limitsManager, mockPlayer)
	}()

	t.Cleanup(func() {
		st.StopService()
		wg.Wait()
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
		st:         st,
		cfg:        cfg,
		scanQueue:  scanQueue,
		clock:      fakeClock,
		launchCh:   launchCh,
		stopCh:     stopCh,
		keyboardCh: keyboardCh,
	}
}

// --- Scan helpers ---

func (env *scanBehaviorEnv) sendGameScan(uid, path string) {
	env.scanQueue <- readers.Scan{
		Source: testReaderSrc,
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
		Source: testReaderSrc,
		Token: &tokens.Token{
			UID:      uid,
			Text:     cmd,
			ScanTime: time.Now(),
			Source:   tokens.SourceReader,
			ReaderID: testReaderID,
		},
	}
}

func (env *scanBehaviorEnv) sendRemoval() {
	env.scanQueue <- readers.Scan{
		Source: testReaderSrc,
		Token:  nil,
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
	deadline := time.After(behaviorTimeout)
	for {
		if env.st.GetSoftwareToken() != nil {
			return
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for software token to be set")
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

	env.sendGameScan("game1", "/mock/roms/game.rom")
	env.waitForLaunch(t)

	env.sendRemoval()
	env.expectNoStop(t)
}

func TestScanBehavior_Tap_DuplicateSuppression(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeTap, 0)

	env.sendGameScan("game1", "/mock/roms/game.rom")
	env.waitForLaunch(t)

	// Same card again — should be suppressed.
	env.sendGameScan("game1", "/mock/roms/game.rom")
	env.expectNoLaunch(t)
}

func TestScanBehavior_Tap_DifferentCardLaunchesDirectly(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeTap, 0)

	env.sendGameScan("gameA", "/mock/roms/gameA.rom")
	require.Equal(t, "/mock/roms/gameA.rom", env.waitForLaunch(t))

	env.sendGameScan("gameB", "/mock/roms/gameB.rom")
	require.Equal(t, "/mock/roms/gameB.rom", env.waitForLaunch(t))

	select {
	case <-env.stopCh:
		t.Fatal("StopActiveLauncher should not have been called between launches")
	default:
	}
}

func TestScanBehavior_Tap_SameCardAfterRemoveReloads(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeTap, 0)

	env.sendGameScan("game1", "/mock/roms/game.rom")
	env.waitForLaunch(t)

	env.sendRemoval()

	// Re-tap same card — should launch again (prevToken cleared by removal).
	env.sendGameScan("game1", "/mock/roms/game.rom")
	env.waitForLaunch(t)
}

func TestScanBehavior_Tap_CommandDoesNotInterruptGame(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeTap, 0)

	env.sendGameScan("game1", "/mock/roms/game.rom")
	env.waitForLaunch(t)

	env.sendCommandScan("cmd1", "**input.keyboard:coin")
	env.waitForKeyboard(t)

	env.expectNoStop(t)
}

func TestScanBehavior_Tap_ManualExitResetsState(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeTap, 0)

	env.sendGameScan("gameA", "/mock/roms/gameA.rom")
	env.waitForLaunch(t)

	env.simulateManualExit()

	env.sendGameScan("gameB", "/mock/roms/gameB.rom")
	require.Equal(t, "/mock/roms/gameB.rom", env.waitForLaunch(t))
}

func TestScanBehavior_Tap_ManualExitWithCardNoRelaunch(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeTap, 0)

	env.sendGameScan("game1", "/mock/roms/game.rom")
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

	env.sendGameScan("game1", "/mock/roms/game.rom")
	env.waitForLaunch(t)
	// Wait for the software token roundtrip through lsq before removal,
	// otherwise the 0s timer fires before software token is set.
	env.waitForSoftwareToken(t)

	env.sendRemoval()
	env.waitForStop(t)
}

func TestScanBehavior_HoldImmediate_ManualExitNoRelaunch(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeHold, 0)

	env.sendGameScan("game1", "/mock/roms/game.rom")
	env.waitForLaunch(t)

	// User manually exits while card is still on reader.
	env.simulateManualExit()
	env.expectNoLaunch(t)
}

func TestScanBehavior_HoldImmediate_ManualExitThenRemoveNoReload(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeHold, 0)

	env.sendGameScan("game1", "/mock/roms/game.rom")
	env.waitForLaunch(t)

	env.simulateManualExit()

	// Remove card after manual exit — should NOT trigger stop (already stopped).
	env.sendRemoval()
	env.expectNoStop(t)
}

// ============================================================================
// Hold mode delayed tests
// ============================================================================

func TestScanBehavior_HoldDelayed_RemovalClosesAfterDelay(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeHold, 5)

	env.sendGameScan("game1", "/mock/roms/game.rom")
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

	env.sendGameScan("game1", "/mock/roms/game.rom")
	env.waitForLaunch(t)
	env.waitForSoftwareToken(t)

	env.sendRemoval()
	env.sendGameScan("game1", "/mock/roms/game.rom")
	env.waitForActiveCard(t, "game1")
	env.waitForTimerStopped(t)

	env.clock.Advance(10 * time.Second)
	env.expectNoStop(t)
	env.expectNoLaunch(t)
}

func TestScanBehavior_HoldDelayed_DifferentCardLaunchesImmediately(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeHold, 5)

	env.sendGameScan("gameA", "/mock/roms/gameA.rom")
	require.Equal(t, "/mock/roms/gameA.rom", env.waitForLaunch(t))
	env.waitForSoftwareToken(t)

	env.sendRemoval()
	env.sendGameScan("gameB", "/mock/roms/gameB.rom")
	require.Equal(t, "/mock/roms/gameB.rom", env.waitForLaunch(t))
}

func TestScanBehavior_HoldDelayed_CommandResetsCountdown(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeHold, 5)

	env.sendGameScan("game1", "/mock/roms/game.rom")
	env.waitForLaunch(t)
	env.waitForSoftwareToken(t)

	env.sendRemoval()

	// First command card resets the 5s countdown.
	env.sendCommandScan("cmd1", "**input.keyboard:coin")
	env.waitForKeyboard(t)

	// Advance 4s (< 5s exit_delay). If the timer was reset by the command,
	// there's 1s remaining. If it wasn't, 4s > original timer and it would fire.
	env.clock.Advance(4 * time.Second)

	// Second command card resets the countdown again.
	env.sendCommandScan("cmd2", "**input.keyboard:start")
	env.waitForKeyboard(t)

	// Advance another 4s (total 8s > 5s). Only passes if the second command
	// truly reset the timer — otherwise the first command's timer (1s remaining)
	// would have fired.
	env.clock.Advance(4 * time.Second)
	env.expectNoStop(t)

	// Reinsert original game card — cancels timer, session continues.
	env.sendGameScan("game1", "/mock/roms/game.rom")
	env.waitForActiveCard(t, "game1")
	env.waitForTimerStopped(t)

	env.clock.Advance(10 * time.Second)
	env.expectNoStop(t)
}

func TestScanBehavior_HoldDelayed_ManualExitNoRelaunch(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeHold, 5)

	env.sendGameScan("game1", "/mock/roms/game.rom")
	env.waitForLaunch(t)

	env.simulateManualExit()
	env.expectNoLaunch(t)
}

func TestScanBehavior_HoldDelayed_ManualExitThenRemoveNoReload(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeHold, 5)

	env.sendGameScan("game1", "/mock/roms/game.rom")
	env.waitForLaunch(t)

	env.simulateManualExit()

	env.sendRemoval()
	env.expectNoStop(t)
}

func TestScanBehavior_HoldDelayed_ManualExitDuringCountdownCancels(t *testing.T) {
	t.Parallel()
	env := setupScanBehavior(t, config.ScanModeHold, 5)

	env.sendGameScan("game1", "/mock/roms/game.rom")
	env.waitForLaunch(t)
	env.waitForSoftwareToken(t)

	env.sendRemoval()

	// Timer goroutine will see no active media and bail out.
	env.simulateManualExit()
	env.clock.Advance(10 * time.Second)
	env.expectNoStop(t)
}
