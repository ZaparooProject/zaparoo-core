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
	"sync"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
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
	tokenTimeout = 2 * time.Second
	noTokenWait  = 100 * time.Millisecond
)

type readerManagerEnv struct {
	st           *state.State
	scanQueue    chan readers.Scan
	itq          chan tokens.Token
	confirmQueue chan chan error
	notifCh      <-chan models.Notification
	clock        clockwork.Clock
}

func setupReaderManager(t *testing.T, opts ...func(*config.Instance)) *readerManagerEnv {
	return setupReaderManagerWithClock(t, nil, opts...)
}

func setupReaderManagerWithClock(t *testing.T, clk clockwork.Clock, opts ...func(*config.Instance)) *readerManagerEnv {
	t.Helper()

	fs := testhelpers.NewMemoryFS()
	cfg, err := testhelpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	for _, opt := range opts {
		opt(cfg)
	}

	mockPlayer := mocks.NewMockPlayer()
	mockPlayer.SetupNoOpMock()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.SetupBasicMock()
	mockPlatform.On("LookupMapping", mock.Anything).Return("", false)

	st, notifCh := state.NewState(mockPlatform, "test-boot-uuid")

	mockUserDB := testhelpers.NewMockUserDBI()
	mockUserDB.On("GetEnabledMappings").Return([]database.Mapping{}, nil)
	db := &database.Database{
		UserDB:  mockUserDB,
		MediaDB: testhelpers.NewMockMediaDBI(),
	}

	scanQueue := make(chan readers.Scan)
	itq := make(chan tokens.Token, 10)
	lsq := make(chan *tokens.Token, 10)
	plq := make(chan *playlists.Playlist, 10)
	cfq := make(chan chan error, 10)

	svc := &ServiceContext{
		Platform:            mockPlatform,
		Config:              cfg,
		State:               st,
		DB:                  db,
		LaunchSoftwareQueue: lsq,
		PlaylistQueue:       plq,
		ConfirmQueue:        cfq,
	}

	go readerManager(svc, itq, scanQueue, mockPlayer, clk)

	t.Cleanup(func() {
		st.StopService()
		for {
			select {
			case <-notifCh:
			default:
				return
			}
		}
	})

	return &readerManagerEnv{
		st:           st,
		scanQueue:    scanQueue,
		itq:          itq,
		confirmQueue: cfq,
		notifCh:      notifCh,
		clock:        clk,
	}
}

func (env *readerManagerEnv) expectToken(t *testing.T) tokens.Token {
	t.Helper()
	select {
	case tok := <-env.itq:
		return tok
	case <-time.After(tokenTimeout):
		t.Fatal("timed out waiting for token on itq")
		return tokens.Token{}
	}
}

func (env *readerManagerEnv) expectNoToken(t *testing.T) {
	t.Helper()
	select {
	case tok := <-env.itq:
		t.Fatalf("unexpected token on itq: %+v", tok)
	case <-time.After(noTokenWait):
	}
}

func (env *readerManagerEnv) sendScan(scan readers.Scan) {
	env.scanQueue <- scan
}

func (env *readerManagerEnv) expectNotification(t *testing.T, method string) {
	t.Helper()
	timeout := time.After(tokenTimeout)
	for {
		select {
		case notif := <-env.notifCh:
			if notif.Method == method {
				return
			}
		case <-timeout:
			t.Fatalf("timed out waiting for %s notification", method)
		}
	}
}

func TestReaderManager_NormalScanFlow(t *testing.T) {
	t.Parallel()
	env := setupReaderManager(t)

	env.sendScan(readers.Scan{
		Source: "test-reader",
		Token: &tokens.Token{
			UID:      "abc123",
			Text:     "**launch.system:nes",
			ScanTime: time.Now(),
		},
	})

	tok := env.expectToken(t)
	assert.Equal(t, "abc123", tok.UID)
	assert.Equal(t, "**launch.system:nes", tok.Text)
}

func TestReaderManager_DuplicateScanSuppression(t *testing.T) {
	t.Parallel()
	env := setupReaderManager(t)

	scan := readers.Scan{
		Source: "test-reader",
		Token: &tokens.Token{
			UID:      "abc123",
			Text:     "**launch.system:nes",
			ScanTime: time.Now(),
		},
	}

	env.sendScan(scan)
	env.expectToken(t)

	// Same token again should be suppressed as duplicate
	env.sendScan(scan)
	env.expectNoToken(t)
}

func TestReaderManager_DifferentTokens(t *testing.T) {
	t.Parallel()
	env := setupReaderManager(t)

	env.sendScan(readers.Scan{
		Source: "test-reader",
		Token: &tokens.Token{
			UID:      "token-a",
			Text:     "game-a",
			ScanTime: time.Now(),
		},
	})
	tokA := env.expectToken(t)
	assert.Equal(t, "token-a", tokA.UID)

	// Remove token (nil)
	env.sendScan(readers.Scan{Source: "test-reader", Token: nil})
	env.expectNoToken(t)

	env.sendScan(readers.Scan{
		Source: "test-reader",
		Token: &tokens.Token{
			UID:      "token-b",
			Text:     "game-b",
			ScanTime: time.Now(),
		},
	})
	tokB := env.expectToken(t)
	assert.Equal(t, "token-b", tokB.UID)
}

// TestReaderManager_ReaderErrorPreservesToken is the #497 regression test:
// card scanned -> reader error (USB hotplug) -> reader reconnects -> same card
// detected. The re-detection must be caught as duplicate because prevToken was
// preserved through the reader error.
func TestReaderManager_ReaderErrorPreservesToken(t *testing.T) {
	t.Parallel()
	env := setupReaderManager(t)

	tokenA := &tokens.Token{
		UID:      "nfc-tag-001",
		Text:     "**launch.system:snes",
		ScanTime: time.Now(),
	}

	env.sendScan(readers.Scan{Source: "test-reader", Token: tokenA})
	env.expectToken(t)

	// Reader error (USB hotplug) — nil token with ReaderError=true
	env.sendScan(readers.Scan{
		Source:      "test-reader",
		Token:       nil,
		ReaderError: true,
	})
	env.expectNoToken(t)

	// Reader reconnects, same card detected — must be duplicate
	env.sendScan(readers.Scan{Source: "test-reader", Token: tokenA})
	env.expectNoToken(t)
}

func TestReaderManager_ReaderErrorThenDifferentToken(t *testing.T) {
	t.Parallel()
	env := setupReaderManager(t)

	env.sendScan(readers.Scan{
		Source: "test-reader",
		Token: &tokens.Token{
			UID:      "token-a",
			Text:     "game-a",
			ScanTime: time.Now(),
		},
	})
	env.expectToken(t)

	env.sendScan(readers.Scan{
		Source:      "test-reader",
		Token:       nil,
		ReaderError: true,
	})
	env.expectNoToken(t)

	// Different token should pass through
	env.sendScan(readers.Scan{
		Source: "test-reader",
		Token: &tokens.Token{
			UID:      "token-b",
			Text:     "game-b",
			ScanTime: time.Now(),
		},
	})
	tok := env.expectToken(t)
	assert.Equal(t, "token-b", tok.UID)
}

func TestReaderManager_NormalRemovalClearsPrevToken(t *testing.T) {
	t.Parallel()
	env := setupReaderManager(t)

	tokenA := &tokens.Token{
		UID:      "nfc-tag-001",
		Text:     "**launch.system:nes",
		ScanTime: time.Now(),
	}

	env.sendScan(readers.Scan{Source: "test-reader", Token: tokenA})
	env.expectToken(t)

	// Normal removal clears prevToken
	env.sendScan(readers.Scan{Source: "test-reader", Token: nil})
	env.expectNoToken(t)

	// Same token again should pass through (prevToken was cleared by removal)
	env.sendScan(readers.Scan{Source: "test-reader", Token: tokenA})
	tok := env.expectToken(t)
	assert.Equal(t, "nfc-tag-001", tok.UID)
}

func TestReaderManager_ScanError(t *testing.T) {
	t.Parallel()
	env := setupReaderManager(t)

	env.sendScan(readers.Scan{
		Source: "test-reader",
		Error:  errors.New("read failed"),
	})

	env.expectNoToken(t)
}

func TestReaderManager_WroteTokenSuppression(t *testing.T) {
	t.Parallel()
	env := setupReaderManager(t)

	tokenA := &tokens.Token{
		UID:      "nfc-write-001",
		Text:     "**launch.system:gb",
		ScanTime: time.Now(),
	}

	// Simulate a write operation
	env.st.SetWroteToken(tokenA)

	// Scan the just-written token — should be suppressed
	env.sendScan(readers.Scan{Source: "test-reader", Token: tokenA})
	env.expectNoToken(t)

	// A different token should pass through (wroteToken was cleared)
	env.sendScan(readers.Scan{
		Source: "test-reader",
		Token: &tokens.Token{
			UID:      "nfc-other-002",
			Text:     "**launch.system:snes",
			ScanTime: time.Now(),
		},
	})
	tok := env.expectToken(t)
	assert.Equal(t, "nfc-other-002", tok.UID)
}

func withIgnoreOnConnect(cfg *config.Instance) {
	cfg.SetScanIgnoreOnConnect(true)
}

func TestReaderManager_IgnoreOnConnect_SuppressesFirstScan(t *testing.T) {
	t.Parallel()
	env := setupReaderManager(t, withIgnoreOnConnect)

	// First scan from a reader should be suppressed
	env.sendScan(readers.Scan{
		Source: "test-reader",
		Token: &tokens.Token{
			UID:      "startup-card",
			Text:     "**launch.system:nes",
			ScanTime: time.Now(),
			ReaderID: "reader-1",
		},
	})
	env.expectNoToken(t)
}

func TestReaderManager_IgnoreOnConnect_SecondScanProceeds(t *testing.T) {
	t.Parallel()
	env := setupReaderManager(t, withIgnoreOnConnect)

	// First scan suppressed
	env.sendScan(readers.Scan{
		Source: "test-reader",
		Token: &tokens.Token{
			UID:      "startup-card",
			Text:     "**launch.system:nes",
			ScanTime: time.Now(),
			ReaderID: "reader-1",
		},
	})
	env.expectNoToken(t)

	// Remove the card
	env.sendScan(readers.Scan{Source: "test-reader", Token: nil})
	env.expectNoToken(t)

	// Second scan from the same reader should proceed
	env.sendScan(readers.Scan{
		Source: "test-reader",
		Token: &tokens.Token{
			UID:      "real-card",
			Text:     "**launch.system:snes",
			ScanTime: time.Now(),
			ReaderID: "reader-1",
		},
	})
	tok := env.expectToken(t)
	assert.Equal(t, "real-card", tok.UID)
}

func TestReaderManager_IgnoreOnConnect_Disabled(t *testing.T) {
	t.Parallel()
	env := setupReaderManager(t)

	// With ignore_on_connect disabled (default), first scan should proceed
	env.sendScan(readers.Scan{
		Source: "test-reader",
		Token: &tokens.Token{
			UID:      "first-card",
			Text:     "**launch.system:nes",
			ScanTime: time.Now(),
			ReaderID: "reader-1",
		},
	})
	tok := env.expectToken(t)
	assert.Equal(t, "first-card", tok.UID)
}

func TestReaderManager_IgnoreOnConnect_MultipleReaders(t *testing.T) {
	t.Parallel()
	env := setupReaderManager(t, withIgnoreOnConnect)

	// First scan from reader-1: suppressed
	env.sendScan(readers.Scan{
		Source: "test-reader-1",
		Token: &tokens.Token{
			UID:      "card-a",
			Text:     "**launch.system:nes",
			ScanTime: time.Now(),
			ReaderID: "reader-1",
		},
	})
	env.expectNoToken(t)

	// First scan from reader-2: also suppressed (independent tracking)
	env.sendScan(readers.Scan{
		Source: "test-reader-2",
		Token: &tokens.Token{
			UID:      "card-b",
			Text:     "**launch.system:snes",
			ScanTime: time.Now(),
			ReaderID: "reader-2",
		},
	})
	env.expectNoToken(t)

	// Remove from reader-1
	env.sendScan(readers.Scan{Source: "test-reader-1", Token: nil})
	env.expectNoToken(t)

	// Second scan from reader-1: proceeds
	env.sendScan(readers.Scan{
		Source: "test-reader-1",
		Token: &tokens.Token{
			UID:      "card-c",
			Text:     "**launch.system:gb",
			ScanTime: time.Now(),
			ReaderID: "reader-1",
		},
	})
	tok := env.expectToken(t)
	assert.Equal(t, "card-c", tok.UID)

	// Remove from reader-2
	env.sendScan(readers.Scan{Source: "test-reader-2", Token: nil})
	env.expectNoToken(t)

	// Second scan from reader-2: also proceeds
	env.sendScan(readers.Scan{
		Source: "test-reader-2",
		Token: &tokens.Token{
			UID:      "card-d",
			Text:     "**launch.system:gba",
			ScanTime: time.Now(),
			ReaderID: "reader-2",
		},
	})
	tok2 := env.expectToken(t)
	assert.Equal(t, "card-d", tok2.UID)
}

func TestReaderManager_IgnoreOnConnect_ReaderReconnection(t *testing.T) {
	t.Parallel()
	env := setupReaderManager(t, withIgnoreOnConnect)

	// First scan from reader: suppressed
	env.sendScan(readers.Scan{
		Source: "test-reader",
		Token: &tokens.Token{
			UID:      "card-a",
			Text:     "**launch.system:nes",
			ScanTime: time.Now(),
			ReaderID: "reader-1",
		},
	})
	env.expectNoToken(t)

	// Reader error (USB disconnect) — clears acknowledged state
	env.sendScan(readers.Scan{
		Source:      "test-reader",
		Token:       nil,
		ReaderError: true,
	})
	env.expectNoToken(t)

	// Reader reconnects with a different card — first scan suppressed again
	env.sendScan(readers.Scan{
		Source: "test-reader",
		Token: &tokens.Token{
			UID:      "card-b",
			Text:     "**launch.system:snes",
			ScanTime: time.Now(),
			ReaderID: "reader-1",
		},
	})
	env.expectNoToken(t)

	// Remove card
	env.sendScan(readers.Scan{Source: "test-reader", Token: nil})
	env.expectNoToken(t)

	// Second scan after reconnection: proceeds
	env.sendScan(readers.Scan{
		Source: "test-reader",
		Token: &tokens.Token{
			UID:      "card-c",
			Text:     "**launch.system:gb",
			ScanTime: time.Now(),
			ReaderID: "reader-1",
		},
	})
	tok2 := env.expectToken(t)
	assert.Equal(t, "card-c", tok2.UID)
}

func withLaunchGuard(cfg *config.Instance) {
	cfg.SetLaunchGuard(true)
}

func withLaunchGuardRequireConfirm(cfg *config.Instance) {
	cfg.SetLaunchGuard(true)
	cfg.SetLaunchGuardRequireConfirm(true)
}

func TestReaderManager_LaunchGuard_NoMediaPlaying(t *testing.T) {
	t.Parallel()
	env := setupReaderManager(t, withLaunchGuard)

	// No media playing — token should pass through immediately
	env.sendScan(readers.Scan{
		Source: "test-reader",
		Token: &tokens.Token{
			UID:      "card-a",
			Text:     "**launch.system:nes",
			ScanTime: time.Now(),
		},
	})
	tok := env.expectToken(t)
	assert.Equal(t, "card-a", tok.UID)
}

func TestReaderManager_LaunchGuard_StagesWhenMediaPlaying(t *testing.T) {
	t.Parallel()
	env := setupReaderManager(t, withLaunchGuard)

	// Set active media to simulate a game running
	env.st.SetActiveMedia(&models.ActiveMedia{
		LauncherID: "test",
		SystemID:   "nes",
		Name:       "Current Game",
	})

	// Scan a new card — should be staged, not launched
	env.sendScan(readers.Scan{
		Source: "test-reader",
		Token: &tokens.Token{
			UID:      "card-b",
			Text:     "**launch.system:snes",
			ScanTime: time.Now(),
		},
	})
	env.expectNoToken(t)
}

func TestReaderManager_LaunchGuard_DoubleTapConfirms(t *testing.T) {
	t.Parallel()
	env := setupReaderManager(t, withLaunchGuard)

	// Set active media
	env.st.SetActiveMedia(&models.ActiveMedia{
		LauncherID: "test",
		SystemID:   "nes",
		Name:       "Current Game",
	})

	cardB := &tokens.Token{
		UID:      "card-b",
		Text:     "**launch.system:snes",
		ScanTime: time.Now(),
	}

	// First tap — staged
	env.sendScan(readers.Scan{Source: "test-reader", Token: cardB})
	env.expectNoToken(t)

	// Remove card (clears preprocessor prevToken via normal removal path)
	env.sendScan(readers.Scan{Source: "test-reader", Token: nil})
	env.expectNoToken(t)

	// Second tap — should confirm and launch
	env.sendScan(readers.Scan{Source: "test-reader", Token: cardB})
	tok := env.expectToken(t)
	assert.Equal(t, "card-b", tok.UID)
}

func TestReaderManager_LaunchGuard_DifferentCardReplacesStaged(t *testing.T) {
	t.Parallel()
	env := setupReaderManager(t, withLaunchGuard)

	env.st.SetActiveMedia(&models.ActiveMedia{
		LauncherID: "test",
		SystemID:   "nes",
		Name:       "Current Game",
	})

	// Stage card B
	env.sendScan(readers.Scan{
		Source: "test-reader",
		Token: &tokens.Token{
			UID:      "card-b",
			Text:     "game-b",
			ScanTime: time.Now(),
		},
	})
	env.expectNoToken(t)

	// Remove card
	env.sendScan(readers.Scan{Source: "test-reader", Token: nil})
	env.expectNoToken(t)

	// Stage card C (replaces B) — verify by double-tapping C succeeds
	cardC := &tokens.Token{
		UID:      "card-c",
		Text:     "game-c",
		ScanTime: time.Now(),
	}
	env.sendScan(readers.Scan{Source: "test-reader", Token: cardC})
	env.expectNoToken(t)

	// Remove and re-tap card C to confirm
	env.sendScan(readers.Scan{Source: "test-reader", Token: nil})
	env.expectNoToken(t)

	env.sendScan(readers.Scan{Source: "test-reader", Token: cardC})
	tok := env.expectToken(t)
	assert.Equal(t, "card-c", tok.UID)
}

func TestReaderManager_LaunchGuard_RequireConfirm_BlocksDoubleTap(t *testing.T) {
	t.Parallel()
	env := setupReaderManager(t, withLaunchGuardRequireConfirm)

	env.st.SetActiveMedia(&models.ActiveMedia{
		LauncherID: "test",
		SystemID:   "nes",
		Name:       "Current Game",
	})

	cardB := &tokens.Token{
		UID:      "card-b",
		Text:     "**launch.system:snes",
		ScanTime: time.Now(),
	}

	// First tap — staged
	env.sendScan(readers.Scan{Source: "test-reader", Token: cardB})
	env.expectNoToken(t)

	// Remove
	env.sendScan(readers.Scan{Source: "test-reader", Token: nil})
	env.expectNoToken(t)

	// Re-tap — should NOT confirm because require_confirm is true
	env.sendScan(readers.Scan{Source: "test-reader", Token: cardB})
	env.expectNoToken(t)
}

func TestReaderManager_LaunchGuard_APIConfirm(t *testing.T) {
	t.Parallel()
	env := setupReaderManager(t, withLaunchGuardRequireConfirm)

	env.st.SetActiveMedia(&models.ActiveMedia{
		LauncherID: "test",
		SystemID:   "nes",
		Name:       "Current Game",
	})

	// Stage a token
	env.sendScan(readers.Scan{
		Source: "test-reader",
		Token: &tokens.Token{
			UID:      "card-b",
			Text:     "**launch.system:snes",
			ScanTime: time.Now(),
		},
	})
	env.expectNoToken(t)

	// Send confirm via channel
	result := make(chan error, 1)
	env.confirmQueue <- result
	err := <-result
	require.NoError(t, err)

	// Token should now be on itq
	tok := env.expectToken(t)
	assert.Equal(t, "card-b", tok.UID)
}

func TestReaderManager_LaunchGuard_APIConfirmNoStaged(t *testing.T) {
	t.Parallel()
	env := setupReaderManager(t, withLaunchGuard)

	// Confirm with nothing staged — should return error
	result := make(chan error, 1)
	env.confirmQueue <- result
	err := <-result
	assert.Error(t, err)
}

func withLaunchGuardNoTimeout(cfg *config.Instance) {
	cfg.SetLaunchGuard(true)
	cfg.SetLaunchGuardTimeout(-1) // negative = no timeout (0 returns default)
}

// Timeout expiry — staged token should be discarded after timeout fires
func TestReaderManager_LaunchGuard_TimeoutExpiry(t *testing.T) {
	t.Parallel()
	fakeClock := clockwork.NewFakeClock()
	env := setupReaderManagerWithClock(t, fakeClock, withLaunchGuard)

	env.st.SetActiveMedia(&models.ActiveMedia{
		LauncherID: "test",
		SystemID:   "nes",
		Name:       "Current Game",
	})

	cardB := &tokens.Token{
		UID:      "card-b",
		Text:     "**launch.system:snes",
		ScanTime: time.Now(),
	}

	// Stage token
	env.sendScan(readers.Scan{Source: "test-reader", Token: cardB})
	// Barrier: goroutine must finish processing the stage (including
	// clock.After registration) before it can receive this removal.
	env.sendScan(readers.Scan{Source: "test-reader", Token: nil})

	// Safe to advance — timer is definitely registered
	fakeClock.Advance(16 * time.Second)

	// Two barriers: after Advance, both guardTimeout and scanQueue are ready
	// in the select. Go picks randomly, so the first barrier may consume
	// either one. The second barrier guarantees both have been processed.
	env.sendScan(readers.Scan{Source: "test-reader", Token: nil})
	env.sendScan(readers.Scan{Source: "test-reader", Token: nil})

	// Re-tap — should stage again, not confirm (old staged was cleared by timeout)
	env.sendScan(readers.Scan{Source: "test-reader", Token: cardB})
	env.expectNoToken(t)
}

// Zero/negative timeout — staged token persists until re-tap or confirm
func TestReaderManager_LaunchGuard_NoTimeout(t *testing.T) {
	t.Parallel()
	fakeClock := clockwork.NewFakeClock()
	env := setupReaderManagerWithClock(t, fakeClock, withLaunchGuardNoTimeout)

	env.st.SetActiveMedia(&models.ActiveMedia{
		LauncherID: "test",
		SystemID:   "nes",
		Name:       "Current Game",
	})

	cardB := &tokens.Token{
		UID:      "card-b",
		Text:     "**launch.system:snes",
		ScanTime: time.Now(),
	}

	// Stage token
	env.sendScan(readers.Scan{Source: "test-reader", Token: cardB})
	env.expectNoToken(t)

	// Advance time significantly — token should still be staged (no timeout)
	fakeClock.Advance(10 * time.Minute)

	// Remove and re-tap — should confirm since token is still staged
	env.sendScan(readers.Scan{Source: "test-reader", Token: nil})
	env.expectNoToken(t)

	env.sendScan(readers.Scan{Source: "test-reader", Token: cardB})
	tok := env.expectToken(t)
	assert.Equal(t, "card-b", tok.UID)
}

// Barcode scanner double-tap — same card scanned twice with no removal
func TestReaderManager_LaunchGuard_BarcodeScannerDoubleTap(t *testing.T) {
	t.Parallel()
	env := setupReaderManager(t, withLaunchGuard)

	env.st.SetActiveMedia(&models.ActiveMedia{
		LauncherID: "test",
		SystemID:   "nes",
		Name:       "Current Game",
	})

	cardB := &tokens.Token{
		UID:      "card-b",
		Text:     "**launch.system:snes",
		ScanTime: time.Now(),
	}

	// First scan — staged
	env.sendScan(readers.Scan{Source: "barcode-reader", Token: cardB})
	env.expectNoToken(t)

	// Second scan of same card WITHOUT removal in between
	// This is the barcode scanner case — no nil scan between reads
	env.sendScan(readers.Scan{Source: "barcode-reader", Token: cardB})
	tok := env.expectToken(t)
	assert.Equal(t, "card-b", tok.UID)
}

// API confirm after card replacement — should launch the latest staged card
func TestReaderManager_LaunchGuard_APIConfirmAfterReplacement(t *testing.T) {
	t.Parallel()
	env := setupReaderManager(t, withLaunchGuardRequireConfirm)

	env.st.SetActiveMedia(&models.ActiveMedia{
		LauncherID: "test",
		SystemID:   "nes",
		Name:       "Current Game",
	})

	// Stage card B
	env.sendScan(readers.Scan{
		Source: "test-reader",
		Token: &tokens.Token{
			UID:      "card-b",
			Text:     "game-b",
			ScanTime: time.Now(),
		},
	})
	env.expectNoToken(t)

	// Remove
	env.sendScan(readers.Scan{Source: "test-reader", Token: nil})
	env.expectNoToken(t)

	// Stage card C (replaces B)
	env.sendScan(readers.Scan{
		Source: "test-reader",
		Token: &tokens.Token{
			UID:      "card-c",
			Text:     "game-c",
			ScanTime: time.Now(),
		},
	})
	env.expectNoToken(t)

	// API confirm — should launch C, not B
	result := make(chan error, 1)
	env.confirmQueue <- result
	require.NoError(t, <-result)

	tok := env.expectToken(t)
	assert.Equal(t, "card-c", tok.UID)
}

// Media stops while token is staged — next scan should launch directly
func TestReaderManager_LaunchGuard_MediaStopsClearsGuard(t *testing.T) {
	t.Parallel()
	env := setupReaderManager(t, withLaunchGuard)

	env.st.SetActiveMedia(&models.ActiveMedia{
		LauncherID: "test",
		SystemID:   "nes",
		Name:       "Current Game",
	})

	// Stage a token
	env.sendScan(readers.Scan{
		Source: "test-reader",
		Token: &tokens.Token{
			UID:      "card-b",
			Text:     "game-b",
			ScanTime: time.Now(),
		},
	})
	env.expectNoToken(t)

	// Media stops
	env.st.SetActiveMedia(nil)

	// Remove old card
	env.sendScan(readers.Scan{Source: "test-reader", Token: nil})
	env.expectNoToken(t)

	// New scan — media is no longer playing, should launch directly
	env.sendScan(readers.Scan{
		Source: "test-reader",
		Token: &tokens.Token{
			UID:      "card-c",
			Text:     "game-c",
			ScanTime: time.Now(),
		},
	})
	tok := env.expectToken(t)
	assert.Equal(t, "card-c", tok.UID)
}

// tokens.staged notification emitted when staging
func TestReaderManager_LaunchGuard_EmitsStagedNotification(t *testing.T) {
	t.Parallel()
	env := setupReaderManager(t, withLaunchGuard)

	env.st.SetActiveMedia(&models.ActiveMedia{
		LauncherID: "test",
		SystemID:   "nes",
		Name:       "Current Game",
	})

	env.sendScan(readers.Scan{
		Source: "test-reader",
		Token: &tokens.Token{
			UID:      "card-b",
			Text:     "game-b",
			ScanTime: time.Now(),
		},
	})
	env.expectNoToken(t)

	// Drain notifications until we find tokens.staged
	found := false
	timeout := time.After(2 * time.Second)
	for !found {
		select {
		case notif := <-env.notifCh:
			if notif.Method == models.NotificationTokensStaged {
				found = true
			}
		case <-timeout:
			t.Fatal("expected tokens.staged notification")
		}
	}
}

// Utility commands pass through launch guard without staging
func TestReaderManager_LaunchGuard_UtilityCommandPassesThrough(t *testing.T) {
	t.Parallel()
	env := setupReaderManager(t, withLaunchGuard)

	env.st.SetActiveMedia(&models.ActiveMedia{
		LauncherID: "test",
		SystemID:   "nes",
		Name:       "Current Game",
	})

	// Coin insert should pass through immediately
	env.sendScan(readers.Scan{
		Source: "test-reader",
		Token: &tokens.Token{
			UID:      "coin-card",
			Text:     "**coin.p1",
			ScanTime: time.Now(),
		},
	})
	tok := env.expectToken(t)
	assert.Equal(t, "coin-card", tok.UID)
}

// Stop command gets staged by launch guard
func TestReaderManager_LaunchGuard_StopCommandStaged(t *testing.T) {
	t.Parallel()
	env := setupReaderManager(t, withLaunchGuard)

	env.st.SetActiveMedia(&models.ActiveMedia{
		LauncherID: "test",
		SystemID:   "nes",
		Name:       "Current Game",
	})

	env.sendScan(readers.Scan{
		Source: "test-reader",
		Token: &tokens.Token{
			UID:      "stop-card",
			Text:     "**stop",
			ScanTime: time.Now(),
		},
	})
	env.expectNoToken(t)
}

// Playlist command gets staged by launch guard
func TestReaderManager_LaunchGuard_PlaylistCommandStaged(t *testing.T) {
	t.Parallel()
	env := setupReaderManager(t, withLaunchGuard)

	env.st.SetActiveMedia(&models.ActiveMedia{
		LauncherID: "test",
		SystemID:   "nes",
		Name:       "Current Game",
	})

	env.sendScan(readers.Scan{
		Source: "test-reader",
		Token: &tokens.Token{
			UID:      "playlist-card",
			Text:     "**playlist.next",
			ScanTime: time.Now(),
		},
	})
	env.expectNoToken(t)
}

// Mapping-resolved token gets staged when mapped script is disrupting
func TestReaderManager_LaunchGuard_MappedTokenStaged(t *testing.T) {
	t.Parallel()

	// Custom setup: platform maps this specific token to a launch command
	fs := testhelpers.NewMemoryFS()
	cfg, err := testhelpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)
	cfg.SetLaunchGuard(true)

	mockPlayer := mocks.NewMockPlayer()
	mockPlayer.SetupNoOpMock()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.SetupBasicMock()
	// Default: no mapping
	// This specific token maps to a launch command; all others return no mapping
	mockPlatform.On("LookupMapping", mock.MatchedBy(func(t *tokens.Token) bool {
		return t.UID == "mapped-card"
	})).Return("**launch.system:snes", true)
	mockPlatform.On("LookupMapping", mock.Anything).Return("", false)

	st, notifCh := state.NewState(mockPlatform, "test-boot-uuid")

	mockUserDB := testhelpers.NewMockUserDBI()
	mockUserDB.On("GetEnabledMappings").Return([]database.Mapping{}, nil)
	db := &database.Database{
		UserDB:  mockUserDB,
		MediaDB: testhelpers.NewMockMediaDBI(),
	}

	scanQueue := make(chan readers.Scan)
	itq := make(chan tokens.Token, 10)
	lsq := make(chan *tokens.Token, 10)
	plq := make(chan *playlists.Playlist, 10)
	cfq := make(chan chan error, 10)

	svc := &ServiceContext{
		Platform:            mockPlatform,
		Config:              cfg,
		State:               st,
		DB:                  db,
		LaunchSoftwareQueue: lsq,
		PlaylistQueue:       plq,
		ConfirmQueue:        cfq,
	}

	go readerManager(svc, itq, scanQueue, mockPlayer, nil)

	t.Cleanup(func() {
		st.StopService()
		for {
			select {
			case <-notifCh:
			default:
				return
			}
		}
	})

	env := &readerManagerEnv{
		st:           st,
		scanQueue:    scanQueue,
		itq:          itq,
		confirmQueue: cfq,
		notifCh:      notifCh,
	}

	st.SetActiveMedia(&models.ActiveMedia{
		LauncherID: "test",
		SystemID:   "nes",
		Name:       "Current Game",
	})

	// Token text looks like a utility command, but mapping resolves to a launch
	env.sendScan(readers.Scan{
		Source: "test-reader",
		Token: &tokens.Token{
			UID:      "mapped-card",
			Text:     "**coin.p1",
			ScanTime: time.Now(),
		},
	})
	env.expectNoToken(t) // staged because mapping resolved to launch command
}

// Unparseable ZapScript gets staged conservatively
func TestReaderManager_LaunchGuard_UnparseableScriptStaged(t *testing.T) {
	t.Parallel()
	env := setupReaderManager(t, withLaunchGuard)

	env.st.SetActiveMedia(&models.ActiveMedia{
		LauncherID: "test",
		SystemID:   "nes",
		Name:       "Current Game",
	})

	// Plain path (no ** prefix) — not valid ZapScript but commonly used for game launches
	env.sendScan(readers.Scan{
		Source: "test-reader",
		Token: &tokens.Token{
			UID:      "plain-path-card",
			Text:     "/roms/snes/game.sfc",
			ScanTime: time.Now(),
		},
	})
	env.expectNoToken(t)
}

func TestReaderManager_LaunchGuard_Disabled(t *testing.T) {
	t.Parallel()
	env := setupReaderManager(t) // no launch guard

	env.st.SetActiveMedia(&models.ActiveMedia{
		LauncherID: "test",
		SystemID:   "nes",
		Name:       "Current Game",
	})

	// Even with media playing, token should pass through immediately
	env.sendScan(readers.Scan{
		Source: "test-reader",
		Token: &tokens.Token{
			UID:      "card-a",
			Text:     "**launch.system:snes",
			ScanTime: time.Now(),
		},
	})
	tok := env.expectToken(t)
	assert.Equal(t, "card-a", tok.UID)
}

func withLaunchGuardDelay(cfg *config.Instance) {
	cfg.SetLaunchGuard(true)
	cfg.SetLaunchGuardTimeout(15)
	cfg.SetLaunchGuardDelay(5)
}

// Re-tap during delay resets both timers and does not confirm
func TestReaderManager_LaunchGuard_Delay_RetapDuringDelayResets(t *testing.T) {
	t.Parallel()
	fakeClock := clockwork.NewFakeClock()
	env := setupReaderManagerWithClock(t, fakeClock, withLaunchGuardDelay)

	env.st.SetActiveMedia(&models.ActiveMedia{
		LauncherID: "test",
		SystemID:   "nes",
		Name:       "Current Game",
	})

	card := &tokens.Token{
		UID:      "card-a",
		Text:     "**launch.system:snes",
		ScanTime: time.Now(),
	}

	// Stage token
	env.sendScan(readers.Scan{Source: "test-reader", Token: card})
	// Barrier: ensure staging (including clock.After) completes
	env.sendScan(readers.Scan{Source: "test-reader", Token: nil})

	// Advance 3s (within 5s delay)
	fakeClock.Advance(3 * time.Second)
	// Barrier
	env.sendScan(readers.Scan{Source: "test-reader", Token: nil})

	// Re-tap during delay — should NOT confirm, should reset timers
	env.sendScan(readers.Scan{Source: "test-reader", Token: card})
	env.expectNoToken(t)

	// Remove and barrier
	env.sendScan(readers.Scan{Source: "test-reader", Token: nil})

	// Advance another 3s — delay was reset so still within new delay period
	fakeClock.Advance(3 * time.Second)
	env.sendScan(readers.Scan{Source: "test-reader", Token: nil})

	// Re-tap again — still in delay, should NOT confirm
	env.sendScan(readers.Scan{Source: "test-reader", Token: card})
	env.expectNoToken(t)
}

// Re-tap after delay expires should confirm and launch
func TestReaderManager_LaunchGuard_Delay_RetapAfterDelayConfirms(t *testing.T) {
	t.Parallel()
	fakeClock := clockwork.NewFakeClock()
	env := setupReaderManagerWithClock(t, fakeClock, withLaunchGuardDelay)

	env.st.SetActiveMedia(&models.ActiveMedia{
		LauncherID: "test",
		SystemID:   "nes",
		Name:       "Current Game",
	})

	card := &tokens.Token{
		UID:      "card-a",
		Text:     "**launch.system:snes",
		ScanTime: time.Now(),
	}

	// Stage token
	env.sendScan(readers.Scan{Source: "test-reader", Token: card})
	// Barrier
	env.sendScan(readers.Scan{Source: "test-reader", Token: nil})

	// Advance past delay (5s) but within timeout (15s)
	fakeClock.Advance(6 * time.Second)
	// Wait for the delay timer to be consumed — the goroutine emits
	// tokens.staged.ready when delayExpired is set. Without this,
	// the re-tap may race with the timer in the select and be
	// misinterpreted as a re-tap during the delay period.
	env.expectNotification(t, models.NotificationTokensStagedReady)

	// Re-tap after delay — should confirm
	env.sendScan(readers.Scan{Source: "test-reader", Token: card})
	tok := env.expectToken(t)
	assert.Equal(t, "card-a", tok.UID)
}

// API confirm bypasses delay entirely
func TestReaderManager_LaunchGuard_Delay_APIConfirmBypassesDelay(t *testing.T) {
	t.Parallel()
	fakeClock := clockwork.NewFakeClock()
	env := setupReaderManagerWithClock(t, fakeClock, withLaunchGuardDelay)

	env.st.SetActiveMedia(&models.ActiveMedia{
		LauncherID: "test",
		SystemID:   "nes",
		Name:       "Current Game",
	})

	// Stage token
	env.sendScan(readers.Scan{
		Source: "test-reader",
		Token: &tokens.Token{
			UID:      "card-a",
			Text:     "**launch.system:snes",
			ScanTime: time.Now(),
		},
	})
	env.expectNoToken(t)

	// API confirm immediately — should bypass delay
	result := make(chan error, 1)
	env.confirmQueue <- result
	err := <-result
	require.NoError(t, err)

	tok := env.expectToken(t)
	assert.Equal(t, "card-a", tok.UID)
}

// Delay=0 (default) — same behavior as before, re-tap confirms immediately
func TestReaderManager_LaunchGuard_Delay_ZeroDelayRetapConfirmsImmediately(t *testing.T) {
	t.Parallel()
	env := setupReaderManager(t, withLaunchGuard) // no delay set

	env.st.SetActiveMedia(&models.ActiveMedia{
		LauncherID: "test",
		SystemID:   "nes",
		Name:       "Current Game",
	})

	card := &tokens.Token{
		UID:      "card-a",
		Text:     "**launch.system:snes",
		ScanTime: time.Now(),
	}

	// Stage token
	env.sendScan(readers.Scan{Source: "test-reader", Token: card})
	env.expectNoToken(t)

	// Remove and immediate re-tap — should confirm (no delay)
	env.sendScan(readers.Scan{Source: "test-reader", Token: nil})
	env.sendScan(readers.Scan{Source: "test-reader", Token: card})
	tok := env.expectToken(t)
	assert.Equal(t, "card-a", tok.UID)
}

// Delay emits tokens.staged.ready notification when delay expires
func TestReaderManager_LaunchGuard_Delay_EmitsReadyNotification(t *testing.T) {
	t.Parallel()
	fakeClock := clockwork.NewFakeClock()
	env := setupReaderManagerWithClock(t, fakeClock, withLaunchGuardDelay)

	env.st.SetActiveMedia(&models.ActiveMedia{
		LauncherID: "test",
		SystemID:   "nes",
		Name:       "Current Game",
	})

	// Stage token
	env.sendScan(readers.Scan{
		Source: "test-reader",
		Token: &tokens.Token{
			UID:      "card-a",
			Text:     "**launch.system:snes",
			ScanTime: time.Now(),
		},
	})
	// Barrier
	env.sendScan(readers.Scan{Source: "test-reader", Token: nil})

	// Advance past delay
	fakeClock.Advance(6 * time.Second)

	// Verify tokens.staged.ready notification is emitted
	env.expectNotification(t, models.NotificationTokensStagedReady)
}

// API confirm returns error when media has stopped (stale staged token)
func TestReaderManager_LaunchGuard_APIConfirmAfterMediaStops(t *testing.T) {
	t.Parallel()
	env := setupReaderManager(t, withLaunchGuardRequireConfirm)

	env.st.SetActiveMedia(&models.ActiveMedia{
		LauncherID: "test",
		SystemID:   "nes",
		Name:       "Current Game",
	})

	// Stage a token
	env.sendScan(readers.Scan{
		Source: "test-reader",
		Token: &tokens.Token{
			UID:      "card-a",
			Text:     "**launch.system:snes",
			ScanTime: time.Now(),
		},
	})
	env.expectNoToken(t)

	// Media stops
	env.st.SetActiveMedia(nil)

	// Send a scan to trigger the stale-stage check
	env.sendScan(readers.Scan{Source: "test-reader", Token: nil})

	// API confirm — should fail because staged token was cleared
	result := make(chan error, 1)
	env.confirmQueue <- result
	err := <-result
	assert.Error(t, err)
}

// TestReaderManager_ContextCancellation_ItqSend verifies that readerManager
// exits cleanly when context is canceled while blocked on an itq send.
// This is a regression test for a deadlock where bare itq sends had no
// context cancellation support.
func TestReaderManager_ContextCancellation_ItqSend(t *testing.T) {
	t.Parallel()

	fs := testhelpers.NewMemoryFS()
	cfg, err := testhelpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	mockPlayer := mocks.NewMockPlayer()
	mockPlayer.SetupNoOpMock()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.SetupBasicMock()
	mockPlatform.On("LookupMapping", mock.Anything).Return("", false)

	st, notifCh := state.NewState(mockPlatform, "test-boot-uuid")

	mockUserDB := testhelpers.NewMockUserDBI()
	mockUserDB.On("GetEnabledMappings").Return([]database.Mapping{}, nil)
	db := &database.Database{
		UserDB:  mockUserDB,
		MediaDB: testhelpers.NewMockMediaDBI(),
	}

	scanQueue := make(chan readers.Scan)
	itq := make(chan tokens.Token) // unbuffered, no consumer
	lsq := make(chan *tokens.Token, 10)
	plq := make(chan *playlists.Playlist, 10)
	cfq := make(chan chan error, 10)

	svc := &ServiceContext{
		Platform:            mockPlatform,
		Config:              cfg,
		State:               st,
		DB:                  db,
		LaunchSoftwareQueue: lsq,
		PlaylistQueue:       plq,
		ConfirmQueue:        cfq,
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		readerManager(svc, itq, scanQueue, mockPlayer, nil)
	}()

	// Send a scan — readerManager will block on itq <- because nothing reads itq.
	// scanQueue is unbuffered so this returns once readerManager reads it.
	scanQueue <- readers.Scan{
		Source: "test-reader",
		Token: &tokens.Token{
			UID:      "card-1",
			Text:     "/rom/game.rom",
			ScanTime: time.Now(),
			Source:   tokens.SourceReader,
		},
	}

	// Wait for TokensAdded notification — SetActiveCard fires this immediately
	// before the itq send, so readerManager is at the blocked send when we
	// receive it.
	deadline := time.After(2 * time.Second)
	for {
		select {
		case n := <-notifCh:
			if n.Method == models.NotificationTokensAdded {
				goto ready
			}
		case <-deadline:
			t.Fatal("timed out waiting for TokensAdded notification")
		}
	}
ready:

	// Cancel context — readerManager must exit despite blocked itq send
	st.StopService()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// readerManager exited cleanly
	case <-time.After(5 * time.Second):
		t.Fatal("readerManager did not exit after context cancellation (deadlock)")
	}

	// Drain notifications
	for {
		select {
		case <-notifCh:
		default:
			return
		}
	}
}

// TestReaderManager_ContextCancellation_ConfirmItqSend verifies that when
// context is canceled while the confirm path is blocked sending to itq, the
// result channel receives a context error (not ErrNoStagedToken).
func TestReaderManager_ContextCancellation_ConfirmItqSend(t *testing.T) {
	t.Parallel()

	fs := testhelpers.NewMemoryFS()
	cfg, err := testhelpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)
	cfg.SetLaunchGuard(true)
	cfg.SetLaunchGuardRequireConfirm(true)

	mockPlayer := mocks.NewMockPlayer()
	mockPlayer.SetupNoOpMock()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.SetupBasicMock()
	mockPlatform.On("LookupMapping", mock.Anything).Return("", false)

	st, notifCh := state.NewState(mockPlatform, "test-boot-uuid")

	mockUserDB := testhelpers.NewMockUserDBI()
	mockUserDB.On("GetEnabledMappings").Return([]database.Mapping{}, nil)
	db := &database.Database{
		UserDB:  mockUserDB,
		MediaDB: testhelpers.NewMockMediaDBI(),
	}

	scanQueue := make(chan readers.Scan)
	itq := make(chan tokens.Token) // unbuffered, no consumer
	lsq := make(chan *tokens.Token, 10)
	plq := make(chan *playlists.Playlist, 10)
	cfq := make(chan chan error, 10)

	svc := &ServiceContext{
		Platform:            mockPlatform,
		Config:              cfg,
		State:               st,
		DB:                  db,
		LaunchSoftwareQueue: lsq,
		PlaylistQueue:       plq,
		ConfirmQueue:        cfq,
	}

	// Set active media so launch guard stages instead of sending directly
	st.SetActiveMedia(&models.ActiveMedia{
		LauncherID: "test",
		SystemID:   "nes",
		Name:       "Current Game",
	})

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		readerManager(svc, itq, scanQueue, mockPlayer, nil)
	}()

	// Send a media-launching scan — gets staged, not sent to itq
	scanQueue <- readers.Scan{
		Source: "test-reader",
		Token: &tokens.Token{
			UID:      "card-1",
			Text:     "**launch.system:snes",
			ScanTime: time.Now(),
			Source:   tokens.SourceReader,
		},
	}

	// Wait for the staged notification
	timeout := time.After(2 * time.Second)
	for {
		select {
		case n := <-notifCh:
			if n.Method == models.NotificationTokensStaged {
				goto staged
			}
		case <-timeout:
			t.Fatal("timed out waiting for staged notification")
		}
	}
staged:

	// Send a removal to clear the active card. This doesn't clear the staged
	// token (media is still active). scanQueue is unbuffered so this returns
	// once readerManager has processed it and is back at its select.
	scanQueue <- readers.Scan{Source: "test-reader", Token: nil}

	// Send confirm — readerManager reads from cfq, calls SetActiveCard
	// (fires TokensAdded since we cleared the card above), then blocks on
	// itq <- confirmed.
	resultCh := make(chan error, 1)
	cfq <- resultCh

	deadline := time.After(2 * time.Second)
	for {
		select {
		case n := <-notifCh:
			if n.Method == models.NotificationTokensAdded {
				goto confirmed
			}
		case <-deadline:
			t.Fatal("timed out waiting for TokensAdded notification from confirm path")
		}
	}
confirmed:

	// Cancel context — confirm path should return context error, not ErrNoStagedToken
	st.StopService()

	select {
	case err := <-resultCh:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("confirm result never received (deadlock)")
	}

	wg.Wait()

	// Drain notifications
	for {
		select {
		case <-notifCh:
		default:
			return
		}
	}
}
