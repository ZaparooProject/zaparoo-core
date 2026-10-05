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

package playnite

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func game(t *testing.T, id string) *Game {
	t.Helper()
	games := fixtureGames()
	for i := range games {
		if games[i].ID == id {
			return &games[i]
		}
	}
	require.Fail(t, "no fixture game", id)
	return nil
}

func TestAvailable(t *testing.T) {
	t.Parallel()

	h := newHarness(t, harnessOptions{})
	require.ErrorIs(t, h.integration.Available(nil), ErrNotInstalled)

	h.install()
	require.NoError(t, h.integration.Available(nil))
}

func TestAvailableWhenOnlyTheExtensionIsKnown(t *testing.T) {
	t.Parallel()

	// A portable Playnite has no install Core can find; the connection is
	// what proves it is there.
	h := newHarness(t, harnessOptions{})
	h.connect(fixtureGames())
	require.NoError(t, h.integration.Available(nil))
}

func TestScanWithoutPlaynite(t *testing.T) {
	t.Parallel()

	h := newHarness(t, harnessOptions{})
	results, err := h.integration.Scan(context.Background(), nil, systemdefs.SystemPC)
	require.NoError(t, err, "an absent install contributes nothing")
	assert.Empty(t, results)
}

func TestScanWithPlayniteClosedKeepsIndexedGames(t *testing.T) {
	t.Parallel()

	h := newHarness(t, harnessOptions{})
	h.install()
	_, err := h.integration.Scan(context.Background(), nil, systemdefs.SystemPC)
	// The media scanner compares by identity so a joined I/O failure is not
	// mistaken for an absent source.
	//nolint:errorlint,testifylint // Identity is the contract.
	assert.True(t, err == platforms.ErrScannerUnavailable, "got %v", err)
}

func TestScanReadsLibraryOncePerIndex(t *testing.T) {
	t.Parallel()

	clock := clockwork.NewFakeClock()
	h := newHarness(t, harnessOptions{clock: clock, steam: true})
	ext := h.connect(fixtureGames())
	ctx := context.Background()

	pc, err := h.integration.Scan(ctx, nil, systemdefs.SystemPC)
	require.NoError(t, err)
	require.Len(t, pc, 2)
	assert.Equal(t, GamePath(idPC, "PC Game"), pc[0].Path)
	assert.Equal(t, GamePath(idBare, "Bare Program"), pc[1].Path)

	nes, err := h.integration.Scan(ctx, nil, systemdefs.SystemNES)
	require.NoError(t, err)
	require.Len(t, nes, 1)
	assert.Equal(t, "NES Game", nes[0].Name)

	other, err := h.integration.Scan(ctx, nil, systemdefs.SystemSNES)
	require.NoError(t, err)
	assert.Empty(t, other)
	assert.Equal(t, 1, ext.count(CommandGetGames), "every system of one index shares a library read")

	clock.Advance(DefaultTimeouts().LibraryCache)
	_, err = h.integration.Scan(ctx, nil, systemdefs.SystemPC)
	require.NoError(t, err)
	assert.Equal(t, 2, ext.count(CommandGetGames), "a later index reads the library again")
}

func TestScanDropsLibrarySnapshotOnReconnect(t *testing.T) {
	t.Parallel()

	h := newHarness(t, harnessOptions{clock: clockwork.NewFakeClock()})
	ctx := context.Background()
	first := h.connect(fixtureGames())
	results, err := h.integration.Scan(ctx, nil, systemdefs.SystemNES)
	require.NoError(t, err)
	require.Len(t, results, 1)

	first.close()
	require.Eventually(t, func() bool { return !h.integration.Connected() }, waitFor, tick)

	// Playnite came back with a different library.
	h.connect([]Game{{ID: idPC, Name: "PC Game", IsInstalled: true}})
	results, err = h.integration.Scan(ctx, nil, systemdefs.SystemNES)
	require.NoError(t, err)
	assert.Empty(t, results, "the previous session's library must not be served")
}

func TestScanReportsLibraryFailure(t *testing.T) {
	t.Parallel()

	h := newHarness(t, harnessOptions{})
	ext := h.connect(fixtureGames())
	ext.set(func(f *fakeExtension) {
		f.onGames = func(cmd Command) []Event {
			return []Event{{Event: EventGames, RequestID: cmd.RequestID, Error: "database is closed"}}
		}
	})
	_, err := h.integration.Scan(context.Background(), nil, systemdefs.SystemPC)
	require.ErrorContains(t, err, "database is closed")
	assert.False(t, err == platforms.ErrScannerUnavailable) //nolint:errorlint,testifylint // Identity is the contract.
}

func TestScanTimesOutWhenExtensionIsSilent(t *testing.T) {
	t.Parallel()

	timeouts := DefaultTimeouts()
	timeouts.Library = 50 * time.Millisecond
	h := newHarness(t, harnessOptions{timeouts: timeouts})
	ext := h.connect(fixtureGames())
	ext.set(func(f *fakeExtension) {
		f.onGames = func(Command) []Event { return nil }
	})
	_, err := h.integration.Scan(context.Background(), nil, systemdefs.SystemPC)
	require.ErrorContains(t, err, "timed out")
}

func TestScanHonoursCancellation(t *testing.T) {
	t.Parallel()

	h := newHarness(t, harnessOptions{})
	ext := h.connect(fixtureGames())
	ext.set(func(f *fakeExtension) {
		f.onGames = func(Command) []Event { return nil }
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := h.integration.Scan(ctx, nil, systemdefs.SystemPC)
	require.ErrorIs(t, err, context.Canceled)
}

func TestLibraryDetailsIsNotCached(t *testing.T) {
	t.Parallel()

	h := newHarness(t, harnessOptions{clock: clockwork.NewFakeClock()})
	ext := h.connect(fixtureGames())
	for range 2 {
		games, err := h.integration.LibraryDetails(context.Background())
		require.NoError(t, err)
		assert.Len(t, games, len(fixtureGames()))
	}
	assert.Equal(t, 2, ext.count(CommandGetGames))

	ext.close()
	require.Eventually(t, func() bool { return !h.integration.Connected() }, waitFor, tick)
	_, err := h.integration.LibraryDetails(context.Background())
	require.ErrorIs(t, err, ErrNotConnected)
}

func TestLaunchPublishesMediaOnlyWhenTheGameStarts(t *testing.T) {
	t.Parallel()

	h := newHarness(t, harnessOptions{})
	ext := h.connect(fixtureGames())
	path := GamePath(idNES, "NES Game")

	require.NoError(t, h.integration.Launch(nil, path))
	assert.Equal(t, 1, ext.count(CommandLaunch))
	assert.Nil(t, h.activeMedia(), "Playnite accepting a launch is not the game running")
	assert.False(t, h.integration.GameRunning())

	ext.write(started(game(t, idNES), 1, 4242))
	h.requireMediaPath(path)
	media := h.activeMedia()
	assert.Equal(t, systemdefs.SystemNES, media.SystemID)
	assert.Equal(t, "NES Game", media.Name)
	assert.Equal(t, LauncherID, media.LauncherID)
	assert.True(t, h.integration.GameRunning())
	require.Eventually(t, func() bool { return len(h.trackedCalls()) == 1 }, waitFor, tick)
	assert.Equal(t, trackCall{pid: 4242, exe: `C:\Games\game.exe`}, h.trackedCalls()[0])

	ext.write(stopped(idNES, 1))
	h.requireMediaPath("")
	assert.False(t, h.integration.GameRunning())
	require.Eventually(t, func() bool { return len(h.untrackedPIDs()) == 1 }, waitFor, tick)
	assert.Equal(t, []int{4242}, h.untrackedPIDs())
}

func TestLaunchRejectsBadPathBeforeTalkingToPlaynite(t *testing.T) {
	t.Parallel()

	h := newHarness(t, harnessOptions{})
	ext := h.connect(fixtureGames())
	require.Error(t, h.integration.Launch(nil, "playnite://not-a-guid/Game"))
	assert.Zero(t, ext.count(CommandLaunch))
}

func TestLaunchRefusedByPlaynite(t *testing.T) {
	t.Parallel()

	tests := []struct {
		reply *Event
		name  string
		want  string
	}{
		{
			name:  "error event",
			reply: &Event{Event: EventError, Command: CommandLaunch, ID: idMissing, Error: "game is not installed"},
			want:  "game is not installed",
		},
		{
			name:  "failed result",
			reply: &Event{Event: EventLaunchResult, ID: idMissing, Status: StatusFailed, Error: "no play action"},
			want:  "no play action",
		},
		{
			name:  "unknown status",
			reply: &Event{Event: EventLaunchResult, ID: idMissing, Status: "perhaps"},
			want:  "perhaps",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t, harnessOptions{})
			ext := h.connect(fixtureGames())
			ext.set(func(f *fakeExtension) {
				f.onLaunch = func(Command) *Event { return tt.reply }
			})
			err := h.integration.Launch(nil, GamePath(idMissing, "Uninstalled Game"))
			require.ErrorContains(t, err, tt.want)
			assert.Nil(t, h.activeMedia())
		})
	}
}

func TestLaunchTimesOutWhenPlayniteDoesNotAnswer(t *testing.T) {
	t.Parallel()

	timeouts := DefaultTimeouts()
	timeouts.LaunchAccept = 50 * time.Millisecond
	h := newHarness(t, harnessOptions{timeouts: timeouts})
	ext := h.connect(fixtureGames())
	ext.set(func(f *fakeExtension) {
		f.onLaunch = func(Command) *Event { return nil }
	})
	err := h.integration.Launch(nil, GamePath(idPC, "PC Game"))
	require.ErrorContains(t, err, "timed out")

	// The request is no longer pending, so the next one is not refused.
	ext.set(func(f *fakeExtension) { f.onLaunch = nil })
	require.NoError(t, h.integration.Launch(nil, GamePath(idPC, "PC Game")))
}

func TestLaunchStartsPlayniteWhenExtensionIsNotConnected(t *testing.T) {
	t.Parallel()

	h := newHarness(t, harnessOptions{})
	path := GamePath(idPC, "PC Game")

	err := h.integration.Launch(nil, path)
	require.ErrorIs(t, err, ErrNotInstalled)
	assert.Empty(t, h.frontend.launches())

	h.install()
	require.NoError(t, h.integration.Launch(nil, path))
	assert.Equal(t, []string{idPC}, h.frontend.launches())
	assert.Nil(t, h.activeMedia())

	// Playnite loads, the extension connects and reports the game.
	ext := h.connect(fixtureGames())
	ext.write(started(game(t, idPC), 1, 77))
	h.requireMediaPath(path)
}

func TestLaunchReportsPlayniteFailingToStart(t *testing.T) {
	t.Parallel()

	h := newHarness(t, harnessOptions{})
	h.install()
	h.frontend.err = errors.New("access denied")
	err := h.integration.Launch(nil, GamePath(idPC, "PC Game"))
	require.ErrorContains(t, err, "access denied")
}

func TestGameStartedInPlayniteIsTracked(t *testing.T) {
	t.Parallel()

	h := newHarness(t, harnessOptions{})
	ext := h.connect(fixtureGames())

	ext.write(started(game(t, idBare), 3, 0))
	h.requireMediaPath(GamePath(idBare, "Bare Program"))
	assert.Equal(t, systemdefs.SystemPC, h.activeMedia().SystemID)
	assert.Empty(t, h.trackedCalls(), "no process was reported, so none is tracked")

	ext.write(stopped(idBare, 3))
	h.requireMediaPath("")
	assert.Empty(t, h.untrackedPIDs())
}

func TestStartsCoreDoesNotIndexAreIgnored(t *testing.T) {
	t.Parallel()

	h := newHarness(t, harnessOptions{steam: true})
	ext := h.connect(fixtureGames())

	ext.write(started(game(t, idOdd), 1, 10))
	ext.write(started(game(t, idSteam), 2, 11))
	ext.write(&Event{Event: EventMediaStarted, ID: idPC})
	ext.write(started(&Game{ID: "not-a-guid", Name: "Bad"}, 3, 12))
	// A marker the reader reaches only after the events above.
	ext.write(started(game(t, idPC), 4, 13))
	h.requireMediaPath(GamePath(idPC, "PC Game"))
	assert.Equal(t, []trackCall{{pid: 13, exe: `C:\Games\game.exe`}}, h.trackedCalls())
}

func TestHiddenGameStartedInPlayniteIsTracked(t *testing.T) {
	t.Parallel()

	// Hidden only keeps a game out of the index; it is still what is running.
	h := newHarness(t, harnessOptions{})
	ext := h.connect(fixtureGames())
	ext.write(started(game(t, idHidden), 1, 10))
	h.requireMediaPath(GamePath(idHidden, "Hidden Game"))
}

func TestStaleStopDoesNotClearNewerRun(t *testing.T) {
	t.Parallel()

	h := newHarness(t, harnessOptions{})
	ext := h.connect(fixtureGames())
	path := GamePath(idPC, "PC Game")

	require.NoError(t, h.integration.Launch(nil, path))
	ext.write(started(game(t, idPC), 1, 100))
	h.requireMediaPath(path)

	// Core stops the game and launches it again before Playnite's own stop
	// event for the first run arrives.
	require.NoError(t, h.integration.StopGame())
	h.setMedia(nil)
	require.NoError(t, h.integration.Launch(nil, path))
	ext.write(stopped(idPC, 1))
	ext.write(started(game(t, idPC), 2, 200))
	h.requireMediaPath(path)

	// A repeat of the first run's stop is still stale.
	ext.write(stopped(idPC, 1))
	ext.write(stopped(idNES, 2))
	ext.write(&Event{Event: EventMediaStopped, ID: "not-a-guid", Session: 2})
	ext.write(&Event{Event: EventWrite, ID: idNES, Name: "marker"})
	require.Eventually(t, func() bool { return len(h.tagWrites()) == 1 }, waitFor, tick)
	require.NotNil(t, h.activeMedia(), "only the running session's stop clears media")
	assert.True(t, h.integration.GameRunning())

	ext.write(stopped(idPC, 2))
	h.requireMediaPath("")
}

func TestStopDoesNotClearMediaItDoesNotOwn(t *testing.T) {
	t.Parallel()

	h := newHarness(t, harnessOptions{})
	ext := h.connect(fixtureGames())
	ext.write(started(game(t, idPC), 1, 100))
	h.requireMediaPath(GamePath(idPC, "PC Game"))

	other := models.NewActiveMedia(systemdefs.SystemPC, "PC", "steam://1/Other", "Other", "Steam")
	h.setMedia(other)
	ext.write(stopped(idPC, 1))
	require.Eventually(t, func() bool { return !h.integration.GameRunning() }, waitFor, tick)
	assert.Same(t, other, h.activeMedia())
}

func TestSecondGameReplacesTheFirst(t *testing.T) {
	t.Parallel()

	h := newHarness(t, harnessOptions{})
	ext := h.connect(fixtureGames())
	ext.write(started(game(t, idPC), 1, 100))
	h.requireMediaPath(GamePath(idPC, "PC Game"))

	ext.write(started(game(t, idNES), 2, 200))
	h.requireMediaPath(GamePath(idNES, "NES Game"))

	ext.write(stopped(idPC, 1))
	ext.write(&Event{Event: EventWrite, ID: idNES, Name: "marker"})
	require.Eventually(t, func() bool { return len(h.tagWrites()) == 1 }, waitFor, tick)
	require.NotNil(t, h.activeMedia(), "the first game's exit must not clear the second")
	assert.Equal(t, GamePath(idNES, "NES Game"), h.activeMedia().Path)
}

func TestStopGame(t *testing.T) {
	t.Parallel()

	tests := []struct {
		reply   *Event
		wantIs  error
		name    string
		wantMsg string
		running bool
	}{
		{name: "completed", reply: &Event{Event: EventMediaStopResult, ID: idPC, Status: StatusCompleted}},
		{
			name:   "no process to stop",
			reply:  &Event{Event: EventMediaStopResult, ID: idPC, Status: StatusUnsupported},
			wantIs: ErrStopUnsupported, running: true,
		},
		{
			name:    "playnite still reports it running",
			reply:   &Event{Event: EventMediaStopResult, ID: idPC, Status: StatusFailed, Error: "still running"},
			wantMsg: "still running", running: true,
		},
		{
			name:    "command rejected",
			reply:   &Event{Event: EventError, Command: CommandStop, ID: idPC, Error: "invalid game ID"},
			wantMsg: "invalid game ID", running: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t, harnessOptions{})
			ext := h.connect(fixtureGames())
			ext.set(func(f *fakeExtension) {
				f.onStop = func(Command) *Event { return tt.reply }
			})
			ext.write(started(game(t, idPC), 1, 100))
			h.requireMediaPath(GamePath(idPC, "PC Game"))

			err := h.integration.StopGame()
			switch {
			case tt.wantIs != nil:
				require.ErrorIs(t, err, tt.wantIs)
			case tt.wantMsg != "":
				require.ErrorContains(t, err, tt.wantMsg)
			default:
				require.NoError(t, err)
			}
			assert.Equal(t, 1, ext.count(CommandStop))
			assert.Equal(t, tt.running, h.integration.GameRunning(),
				"a failed stop keeps the game so a retry has something to act on")
			assert.NotNil(t, h.activeMedia(), "clearing media is the platform's job once a stop is confirmed")
		})
	}
}

func TestStopGameWithNothingRunning(t *testing.T) {
	t.Parallel()

	h := newHarness(t, harnessOptions{})
	ext := h.connect(fixtureGames())
	require.ErrorIs(t, h.integration.StopGame(), ErrNoActiveGame)

	// A launch Playnite has accepted but not yet started is not running.
	require.NoError(t, h.integration.Launch(nil, GamePath(idPC, "PC Game")))
	require.ErrorIs(t, h.integration.StopGame(), ErrNoActiveGame)
	assert.Zero(t, ext.count(CommandStop))
}

func TestStopGameTimesOut(t *testing.T) {
	t.Parallel()

	timeouts := DefaultTimeouts()
	timeouts.Stop = 50 * time.Millisecond
	h := newHarness(t, harnessOptions{timeouts: timeouts})
	ext := h.connect(fixtureGames())
	ext.set(func(f *fakeExtension) {
		f.onStop = func(Command) *Event { return nil }
	})
	ext.write(started(game(t, idPC), 1, 100))
	h.requireMediaPath(GamePath(idPC, "PC Game"))

	require.ErrorContains(t, h.integration.StopGame(), "timed out")
	assert.True(t, h.integration.GameRunning())
}

func TestDuplicateStopResultsDoNotWedgeTheReader(t *testing.T) {
	t.Parallel()

	h := newHarness(t, harnessOptions{})
	ext := h.connect(fixtureGames())
	ext.set(func(f *fakeExtension) {
		f.onStop = func(cmd Command) *Event {
			for range 3 {
				f.write(&Event{Event: EventMediaStopResult, ID: cmd.ID, Status: StatusCompleted})
			}
			return nil
		}
	})
	ext.write(started(game(t, idPC), 1, 100))
	h.requireMediaPath(GamePath(idPC, "PC Game"))
	require.NoError(t, h.integration.StopGame())

	// The reader is still processing events.
	ext.write(started(game(t, idNES), 2, 200))
	h.requireMediaPath(GamePath(idNES, "NES Game"))
}

func TestDisconnectStopsClaimingTheGameIsRunning(t *testing.T) {
	t.Parallel()

	h := newHarness(t, harnessOptions{})
	ext := h.connect(fixtureGames())
	path := GamePath(idPC, "PC Game")
	ext.write(started(game(t, idPC), 1, 100))
	h.requireMediaPath(path)

	ext.close()
	h.requireMediaPath("")
	assert.False(t, h.integration.GameRunning())
	require.Eventually(t, func() bool { return len(h.untrackedPIDs()) == 1 }, waitFor, tick)

	// Playnite is still there: the extension reconnects and says so.
	again := h.connect(fixtureGames())
	again.write(started(game(t, idPC), 1, 100))
	h.requireMediaPath(path)
}

func TestDisconnectFailsWaitingRequests(t *testing.T) {
	t.Parallel()

	h := newHarness(t, harnessOptions{})
	ext := h.connect(fixtureGames())
	ext.set(func(f *fakeExtension) {
		f.onLaunch = func(Command) *Event {
			_ = f.conn.Close()
			return nil
		}
	})
	started := time.Now()
	err := h.integration.Launch(nil, GamePath(idPC, "PC Game"))
	require.Error(t, err)
	assert.Less(t, time.Since(started), DefaultTimeouts().LaunchAccept, "a lost pipe must not wait out the timeout")
}

func TestNewConnectionReplacesTheOld(t *testing.T) {
	t.Parallel()

	h := newHarness(t, harnessOptions{})
	first := h.connect(fixtureGames())
	second := h.connect(fixtureGames())

	select {
	case <-first.done:
	case <-time.After(waitFor):
		require.Fail(t, "the replaced connection was left open")
	}
	assert.True(t, h.integration.Connected())
	require.NoError(t, h.integration.Launch(nil, GamePath(idPC, "PC Game")))
	assert.Equal(t, 1, second.count(CommandLaunch))
	assert.Zero(t, first.count(CommandLaunch))
}

func TestWriteRequest(t *testing.T) {
	t.Parallel()

	h := newHarness(t, harnessOptions{})
	ext := h.connect(fixtureGames())
	ext.write(&Event{Event: EventWrite, ID: "not-a-guid", Name: "Bad"})
	ext.write(&Event{Event: EventWrite, ID: idNES, Name: "  "})
	ext.write(&Event{Event: EventWrite, ID: strings.ToUpper(idNES), Name: " NES Game "})
	require.Eventually(t, func() bool { return len(h.tagWrites()) == 1 }, waitFor, tick)
	assert.Equal(t, []string{GamePath(idNES, "NES Game")}, h.tagWrites())
}

func TestMalformedLinesAreSkipped(t *testing.T) {
	t.Parallel()

	h := newHarness(t, harnessOptions{})
	ext := h.connect(fixtureGames())
	ext.writeRaw([]byte("\n"))
	ext.writeRaw([]byte("not json\n"))
	ext.writeRaw([]byte("[1,2,3]\n"))
	ext.writeRaw([]byte(`{"Id":"` + idPC + `"}` + "\n"))
	ext.writeRaw([]byte(`{"Event":"SomethingNew"}` + "\n"))
	ext.writeRaw([]byte(`{"Event":"MediaStarted","Game":"wrong type"}` + "\n"))
	ext.write(started(game(t, idPC), 1, 100))
	h.requireMediaPath(GamePath(idPC, "PC Game"))
}

func TestOversizedLineDropsTheConnection(t *testing.T) {
	t.Parallel()

	h := newHarness(t, harnessOptions{})
	ext := h.connect(fixtureGames())
	go ext.writeRaw([]byte(strings.Repeat("x", MaxLineSize+1) + "\n"))
	require.Eventually(t, func() bool { return !h.integration.Connected() }, waitFor, tick)
}

func TestLibraryLargerThanTheLimitIsRefused(t *testing.T) {
	t.Parallel()

	h := newHarness(t, harnessOptions{})
	ext := h.connect(fixtureGames())
	chunk := make([]Game, 1000)
	ext.set(func(f *fakeExtension) {
		f.onGames = func(cmd Command) []Event {
			events := make([]Event, 0, maxLibraryGames/len(chunk)+1)
			for range maxLibraryGames/len(chunk) + 1 {
				events = append(events, Event{Event: EventGames, RequestID: cmd.RequestID, Games: chunk})
			}
			return events
		}
	})
	_, err := h.integration.LibraryDetails(context.Background())
	require.ErrorContains(t, err, "exceeds")
}

func TestStopWithRequestInFlight(t *testing.T) {
	t.Parallel()

	h := newHarness(t, harnessOptions{})
	ext := h.connect(fixtureGames())
	ext.set(func(f *fakeExtension) {
		f.onGames = func(Command) []Event { return nil }
	})
	errs := make(chan error, 1)
	go func() {
		_, err := h.integration.LibraryDetails(context.Background())
		errs <- err
	}()
	require.Eventually(t, func() bool { return ext.count(CommandGetGames) == 1 }, waitFor, tick)

	h.integration.Stop()
	select {
	case err := <-errs:
		require.Error(t, err)
	case <-time.After(waitFor):
		require.Fail(t, "request outlived the integration")
	}
	require.ErrorIs(t, h.integration.Launch(nil, GamePath(idPC, "PC Game")), context.Canceled)
}
