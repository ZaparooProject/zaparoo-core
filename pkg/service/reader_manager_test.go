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
	"errors"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	tokenTimeout = 2 * time.Second
	noTokenWait  = 100 * time.Millisecond
)

type readerManagerEnv struct {
	st        *state.State
	scanQueue chan readers.Scan
	itq       chan tokens.Token
}

func setupReaderManager(t *testing.T) *readerManagerEnv {
	t.Helper()

	fs := testhelpers.NewMemoryFS()
	cfg, err := testhelpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	// Disable all audio to prevent malgo goroutine leaks on macOS CI
	cfg.DisableAllSoundsForTesting()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.SetupBasicMock()

	st, notifCh := state.NewState(mockPlatform, "test-boot-uuid")

	db := &database.Database{
		UserDB:  testhelpers.NewMockUserDBI(),
		MediaDB: testhelpers.NewMockMediaDBI(),
	}

	scanQueue := make(chan readers.Scan)
	itq := make(chan tokens.Token, 10)
	lsq := make(chan *tokens.Token, 10)
	plq := make(chan *playlists.Playlist, 10)

	go readerManager(mockPlatform, cfg, st, db, itq, lsq, plq, scanQueue)

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
		st:        st,
		scanQueue: scanQueue,
		itq:       itq,
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
