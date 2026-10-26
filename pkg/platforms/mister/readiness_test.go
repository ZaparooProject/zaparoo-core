//go:build linux && !android

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

package mister

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	misterconfig "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/cores"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/gamehandoff"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeFileInfo struct {
	os.FileInfo
	size int64
	mode fs.FileMode
}

func (f fakeFileInfo) Size() int64       { return f.size }
func (f fakeFileInfo) Mode() fs.FileMode { return f.mode }

// readyHarness stands in for Main: the test sends the file events a launch
// produces and watches what the probe does.
type readyHarness struct {
	r         *coreReadiness
	clock     *clockwork.FakeClock
	events    chan gamehandoff.Kind
	files     map[string]int64
	written   time.Time // when the core name file was last written
	names     []string
	writtenMu syncutil.Mutex
}

// setCore has Main report core, written at at.
func (h *readyHarness) setCore(core string, at time.Time) {
	h.writtenMu.Lock()
	defer h.writtenMu.Unlock()
	h.names = []string{core}
	h.written = at
}

func (h *readyHarness) setWritten(at time.Time) {
	h.writtenMu.Lock()
	defer h.writtenMu.Unlock()
	h.written = at
}

func newReadyHarness(t *testing.T) *readyHarness {
	t.Helper()
	h := &readyHarness{
		clock:  clockwork.NewFakeClock(),
		events: make(chan gamehandoff.Kind),
		files:  map[string]int64{},
	}
	h.r = &coreReadiness{
		clock:    h.clock,
		launches: gamehandoff.NewLaunchLog(h.clock),
		watch:    func(context.Context, string) (<-chan gamehandoff.Kind, error) { return h.events, nil },
		stat: func(path string) (os.FileInfo, error) {
			size, ok := h.files[path]
			if !ok {
				return nil, os.ErrNotExist
			}
			return fakeFileInfo{size: size, mode: 0}, nil
		},
		// The core is up at once unless a test says otherwise.
		coreUp: func(context.Context, time.Time) <-chan struct{} {
			up := make(chan struct{})
			close(up)
			return up
		},
		coreName: func() (string, time.Time, error) {
			h.writtenMu.Lock()
			defer h.writtenMu.Unlock()
			name := h.names[0]
			if len(h.names) > 1 {
				h.names = h.names[1:]
			}
			return name, h.written, nil
		},
	}
	return h
}

func mustCore(t *testing.T, id string) *cores.Core {
	t.Helper()
	core, err := cores.GetCore(id)
	require.NoError(t, err)
	return core
}

func probeMedia(path string) *models.ActiveMedia {
	return models.NewActiveMedia("Genesis", "Genesis", path, "Game", "Genesis")
}

// launched records that Core launched path, as the launcher wrapper does.
func (h *readyHarness) launched(path string) {
	h.r.launches.Note(path)
}

func (h *readyHarness) start(
	ctx context.Context, core *cores.Core, path string,
) <-chan error {
	h.launched(path)
	done := make(chan error, 1)
	go func() { done <- h.r.waitForReady(core, "")(ctx, nil, probeMedia(path)) }()
	return done
}

// advanceUntilDone keeps advancing the fake clock until the probe returns, for
// waits whose timers are replaced by events and so cannot be counted.
func (h *readyHarness) advanceUntilDone(t *testing.T, done <-chan error) error {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case err := <-done:
			return err
		case <-deadline:
			t.Fatal("probe did not finish")
		case <-time.After(time.Millisecond):
			h.clock.Advance(readySettle)
		}
	}
}

func (h *readyHarness) settle(t *testing.T) {
	t.Helper()
	require.NoError(t, h.clock.BlockUntilContext(t.Context(), 1))
	h.clock.Advance(readySettle)
}

const genesisGame = "/media/fat/games/Genesis/game.md"

func TestCoreReadiness_StreamedGameReadyAfterCloseAndSettle(t *testing.T) {
	t.Parallel()
	h := newReadyHarness(t)
	h.files[genesisGame] = 4 << 20
	done := h.start(t.Context(), mustCore(t, "Genesis"), genesisGame)

	h.events <- gamehandoff.Open
	h.events <- gamehandoff.Close
	require.NoError(t, h.advanceUntilDone(t, done))
}

func TestCoreReadiness_StreamedGameNotReadyWhileOpen(t *testing.T) {
	t.Parallel()
	h := newReadyHarness(t)
	h.files[genesisGame] = 4 << 20
	ctx, cancel := context.WithCancel(t.Context())
	done := h.start(ctx, mustCore(t, "Genesis"), genesisGame)

	h.events <- gamehandoff.Open
	require.NoError(t, h.clock.BlockUntilContext(t.Context(), 0))
	h.clock.Advance(time.Hour)
	cancel()

	err := <-done
	require.ErrorIs(t, err, context.Canceled)
	assert.NotErrorIs(t, err, platforms.ErrReadinessUnavailable)
}

func TestCoreReadiness_MountedImageReadyAfterOpen(t *testing.T) {
	t.Parallel()
	h := newReadyHarness(t)
	game := "/media/fat/games/PSX/game.chd"
	h.files[game] = 500 << 20
	done := h.start(t.Context(), mustCore(t, "PSX"), game)

	h.events <- gamehandoff.Open
	// The open swaps the wait for an open for the mount settle, so the timers
	// are not countable; keep advancing until the settle fires.
	for {
		select {
		case err := <-done:
			require.NoError(t, err)
			return
		case <-time.After(time.Millisecond):
			h.clock.Advance(readySettle)
		}
	}
}

func TestCoreReadiness_GameInsideArchiveWatchesTheArchive(t *testing.T) {
	t.Parallel()
	h := newReadyHarness(t)
	archive := "/media/fat/games/Genesis/pack.zip"
	h.files[archive] = 2 << 20
	var watched string
	h.r.watch = func(_ context.Context, path string) (<-chan gamehandoff.Kind, error) {
		watched = path
		return h.events, nil
	}
	done := h.start(t.Context(), mustCore(t, "Genesis"), archive+"/game.md")

	h.events <- gamehandoff.Open
	h.events <- gamehandoff.Close
	require.NoError(t, h.advanceUntilDone(t, done))
	assert.Equal(t, archive, watched)
}

func TestCoreReadiness_NoGameFileIsReadyImmediately(t *testing.T) {
	t.Parallel()
	h := newReadyHarness(t)

	h.launched("/media/fat/games/Genesis/gone.md")

	gone := "/media/fat/games/Genesis/gone.md"
	err := h.r.waitForReady(mustCore(t, "Genesis"), "")(t.Context(), nil, probeMedia(gone))

	require.NoError(t, err)
}

func TestCoreReadiness_UnclaimedExtensionStillWaitsForTheCoreToTakeTheFile(t *testing.T) {
	t.Parallel()
	h := newReadyHarness(t)
	game := "/media/fat/games/Genesis/game.unclaimed"
	h.files[game] = 10
	done := h.start(t.Context(), mustCore(t, "Genesis"), game)

	h.events <- gamehandoff.Open
	h.events <- gamehandoff.Close
	require.NoError(t, h.advanceUntilDone(t, done))
}

func TestCoreReadiness_WatchFailureIsUnavailable(t *testing.T) {
	t.Parallel()
	h := newReadyHarness(t)
	h.files[genesisGame] = 4 << 20
	h.r.watch = func(context.Context, string) (<-chan gamehandoff.Kind, error) {
		return nil, errors.New("inotify limit reached")
	}
	h.launched(genesisGame)

	err := h.r.waitForReady(mustCore(t, "Genesis"), "")(t.Context(), nil, probeMedia(genesisGame))

	require.ErrorIs(t, err, platforms.ErrReadinessUnavailable)
}

func writeMRA(t *testing.T, setname string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "game.mra")
	body := `<misterromdescription><name>Game</name><setname>` + setname + `</setname></misterromdescription>`
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

func TestCoreReadiness_ArcadeReadyWhenSetnameRuns(t *testing.T) {
	t.Parallel()
	h := newReadyHarness(t)
	h.names = []string{"MENU", "MENU", "dkong"}
	mra := writeMRA(t, "dkong")
	h.setWritten(h.clock.Now().Add(time.Second))
	done := h.start(t.Context(), mustCore(t, "Arcade"), mra)

	for range 2 {
		require.NoError(t, h.clock.BlockUntilContext(t.Context(), 1))
		h.clock.Advance(readyCorePoll)
	}
	require.NoError(t, h.advanceUntilDone(t, done))
}

func TestCoreReadiness_ArcadeUnreadableCoreNameIsUnavailable(t *testing.T) {
	t.Parallel()
	h := newReadyHarness(t)
	h.r.coreName = func() (string, time.Time, error) { return "", time.Time{}, os.ErrNotExist }
	ctx, cancel := context.WithCancel(t.Context())
	done := h.start(ctx, mustCore(t, "Arcade"), writeMRA(t, "dkong"))

	require.NoError(t, h.clock.BlockUntilContext(t.Context(), 1))
	cancel()

	require.ErrorIs(t, <-done, platforms.ErrReadinessUnavailable)
}

func TestCoreReadiness_ArcadeMissingMRAIsUnavailable(t *testing.T) {
	t.Parallel()
	h := newReadyHarness(t)
	missing := filepath.Join(t.TempDir(), "absent.mra")
	h.launched(missing)

	err := h.r.waitForReady(mustCore(t, "Arcade"), "")(t.Context(), nil, probeMedia(missing))

	require.ErrorIs(t, err, platforms.ErrReadinessUnavailable)
}

func TestWithCoreReadiness(t *testing.T) {
	t.Parallel()

	launch := func(*config.Instance, string, *platforms.LaunchOptions) (*os.Process, error) {
		return &os.Process{}, nil
	}
	ls := withCoreReadiness(nil, []platforms.Launcher{
		{ID: "Genesis", SystemID: "Genesis", Launch: launch},
		{ID: "RAMegaDrive", SystemID: "Genesis", Launch: launch},
		{ID: "Generic", Launch: launch},
		{ID: "NoLaunch", SystemID: "Genesis"},
		{ID: "Own", SystemID: "Genesis", Launch: launch, WaitForReady: func(
			context.Context, *config.Instance, *models.ActiveMedia,
		) error {
			return nil
		}},
	})

	assert.NotNil(t, ls[0].WaitForReady)
	assert.NotNil(t, ls[1].WaitForReady, "alternate cores load the same media")
	assert.Nil(t, ls[2].WaitForReady, "a launcher with no core has nothing to wait for")
	assert.Nil(t, ls[3].WaitForReady, "a launcher that launches nothing")
	require.NotNil(t, ls[4].WaitForReady, "an existing probe is kept")
	assert.Nil(t, ls[4].ReadyTimeout, "an existing probe keeps its own timeout")
}

func TestCoreReadiness_GameCoreDidNotLaunchIsReadyImmediately(t *testing.T) {
	t.Parallel()
	h := newReadyHarness(t)
	h.files[genesisGame] = 4 << 20
	h.r.watch = func(context.Context, string) (<-chan gamehandoff.Kind, error) {
		t.Fatal("watched a game Core did not launch")
		return nil, errors.New("unreachable")
	}

	require.NoError(t, h.r.waitForReady(mustCore(t, "Genesis"), "")(t.Context(), nil, probeMedia(genesisGame)))
}

func TestCoreReadiness_StaleLaunchIsReadyImmediately(t *testing.T) {
	t.Parallel()
	h := newReadyHarness(t)
	h.files[genesisGame] = 4 << 20
	h.launched(genesisGame)
	h.clock.Advance(readyLaunchFreshness + time.Second)
	h.r.watch = func(context.Context, string) (<-chan gamehandoff.Kind, error) {
		t.Fatal("watched a game from a stale launch")
		return nil, errors.New("unreachable")
	}

	require.NoError(t, h.r.waitForReady(mustCore(t, "Genesis"), "")(t.Context(), nil, probeMedia(genesisGame)))
}

func TestCoreReadiness_OtherGameThanLaunchedIsReadyImmediately(t *testing.T) {
	t.Parallel()
	h := newReadyHarness(t)
	h.files[genesisGame] = 4 << 20
	h.launched("/media/fat/games/Genesis/other.md")
	h.r.watch = func(context.Context, string) (<-chan gamehandoff.Kind, error) {
		t.Fatal("watched a game Core did not launch")
		return nil, errors.New("unreachable")
	}

	require.NoError(t, h.r.waitForReady(mustCore(t, "Genesis"), "")(t.Context(), nil, probeMedia(genesisGame)))
}

func TestCoreReadiness_NoteLaunchesRecordsAndPassesThrough(t *testing.T) {
	t.Parallel()
	h := newReadyHarness(t)
	boom := errors.New("launch failed")
	var got string
	launch := h.r.noteLaunches(func(_ *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
		got = path
		return nil, boom
	})

	_, err := launch(nil, genesisGame, nil)

	require.ErrorIs(t, err, boom)
	assert.Equal(t, genesisGame, got)
	assert.True(t, h.r.launches.Recent(genesisGame, readyLaunchFreshness))
	assert.False(t, h.r.launches.Recent("/other.md", readyLaunchFreshness))
}

// Launchers are rebuilt on every Launchers call, so the list that launches a
// game and the list the service later asks for its probe are different lists.
// It must not run in parallel with other tests: it swaps the shared tracker.
func TestWithCoreReadiness_LaunchInOneListIsSeenByAnother(t *testing.T) {
	h := newReadyHarness(t)
	h.files[genesisGame] = 4 << 20
	saved := launchReadiness
	launchReadiness = h.r
	t.Cleanup(func() { launchReadiness = saved })

	launch := func(*config.Instance, string, *platforms.LaunchOptions) (*os.Process, error) {
		return &os.Process{}, nil
	}
	build := func() platforms.Launcher {
		return withCoreReadiness(nil, []platforms.Launcher{{ID: "Genesis", SystemID: "Genesis", Launch: launch}})[0]
	}
	launching, probing := build(), build()

	_, err := launching.Launch(nil, genesisGame, nil)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- probing.WaitForReady(t.Context(), nil, probeMedia(genesisGame)) }()

	h.events <- gamehandoff.Open
	h.events <- gamehandoff.Close
	require.NoError(t, h.advanceUntilDone(t, done))
}

func TestCoreReadiness_OpenWindowIsShorterThanTheServiceTimeout(t *testing.T) {
	t.Parallel()

	// The service ends every probe after 10 s (pkg/service mediaReadyTimeout);
	// a longer window could never fire.
	assert.Less(t, readyOpenWindow, 10*time.Second)
}

func TestCoreReadiness_ArcadeRelaunchOfTheRunningSetWaitsForTheReload(t *testing.T) {
	t.Parallel()
	h := newReadyHarness(t)
	h.names = []string{"dkong"}
	mra := writeMRA(t, "dkong")
	h.setWritten(h.clock.Now().Add(-time.Hour))
	ctx, cancel := context.WithCancel(t.Context())
	done := h.start(ctx, mustCore(t, "Arcade"), mra)

	// The name already matches, but the file was written before the launch.
	require.NoError(t, h.clock.BlockUntilContext(t.Context(), 1))
	h.clock.Advance(readyCorePoll)
	select {
	case err := <-done:
		t.Fatalf("ready before the core was reloaded: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	h.setWritten(h.clock.Now().Add(time.Second))
	require.NoError(t, h.clock.BlockUntilContext(t.Context(), 1))
	h.clock.Advance(readyCorePoll)
	h.settle(t)
	cancel()

	require.NoError(t, <-done)
}

func TestCoreReadiness_ArcadeWithNoSetnameWaitsForAnyCoreStart(t *testing.T) {
	t.Parallel()
	h := newReadyHarness(t)
	h.names = []string{"MENU", "SomeRBFName"}
	mra := writeMRA(t, "")
	h.setWritten(h.clock.Now().Add(time.Second))
	done := h.start(t.Context(), mustCore(t, "Arcade"), mra)

	for range 2 {
		require.NoError(t, h.clock.BlockUntilContext(t.Context(), 1))
		h.clock.Advance(readyCorePoll)
	}
	require.NoError(t, h.advanceUntilDone(t, done))
}

func TestCoreReadiness_TargetUsesTheCatalogMethodForTheExtension(t *testing.T) {
	t.Parallel()
	h := newReadyHarness(t)
	h.files[genesisGame] = 1

	file, streamed, err := h.r.target(mustCore(t, "Genesis"), "Genesis", genesisGame)

	require.NoError(t, err)
	assert.Equal(t, genesisGame, file)
	assert.True(t, streamed)
}

func TestCoreReadiness_TargetFallsBackToTheCoresFirstSlot(t *testing.T) {
	t.Parallel()
	h := newReadyHarness(t)
	unknown := "/media/fat/games/NES/game.unclaimed"
	h.files[unknown] = 1

	_, streamed, err := h.r.target(mustCore(t, "NES"), "NES", unknown)

	require.NoError(t, err)
	assert.True(t, streamed, "the NES slot streams its file")
}

func TestCoreReadiness_TargetTreatsACoreWithNoSlotsAsMounting(t *testing.T) {
	t.Parallel()
	h := newReadyHarness(t)
	disc := "/media/fat/games/CDTV/game.iso"
	h.files[disc] = 1
	core := *mustCore(t, "Genesis")
	core.Slots = nil

	_, streamed, err := h.r.target(&core, "X", disc)

	require.NoError(t, err)
	assert.False(t, streamed)
}

func TestCoreReadiness_TargetHonoursAStreamingOverride(t *testing.T) {
	t.Parallel()
	h := newReadyHarness(t)
	iso := "/media/fat/games/DVD/movie.iso"
	h.files[iso] = 1

	_, streamed, err := h.r.target(mustCore(t, "DVDPlayer"), "HybridDVDPlayer", iso)

	require.NoError(t, err)
	assert.True(t, streamed, "the hybrid DVD core streams although the catalog entry mounts")
}

func writeMGL(t *testing.T, fileTag string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "game.mgl")
	body := `<mistergamedescription><rbf>_Console/NES</rbf>` + fileTag + `</mistergamedescription>`
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

func TestCoreReadiness_TargetWatchesTheGameAnMGLLoads(t *testing.T) {
	t.Parallel()
	h := newReadyHarness(t)
	game := "/media/fat/games/NES/game.nes"
	h.files[game] = 1
	mgl := writeMGL(t, `<file delay="1" type="f" index="1" path="`+game+`"/>`)

	file, streamed, err := h.r.target(mustCore(t, "NES"), "NES", mgl)

	require.NoError(t, err)
	assert.Equal(t, game, file, "the MGL itself is not the file Main streams")
	assert.True(t, streamed)
}

func TestCoreReadiness_TargetResolvesARelativeMGLPathAgainstTheGamesFolder(t *testing.T) {
	t.Parallel()
	h := newReadyHarness(t)
	game := "/media/fat/games/NES/sub/game.nes"
	h.files[game] = 1
	mgl := writeMGL(t, `<file delay="1" type="s" index="0" path="sub/game.nes"/>`)

	file, streamed, err := h.r.target(mustCore(t, "NES"), "NES", mgl)

	require.NoError(t, err)
	assert.Equal(t, game, file)
	assert.False(t, streamed, "the MGL says the file is mounted")
}

func TestCoreReadiness_TargetOfAnMGLWithNoGameHasNothingToWatch(t *testing.T) {
	t.Parallel()
	h := newReadyHarness(t)

	_, _, err := h.r.target(mustCore(t, "NES"), "NES", writeMGL(t, ""))

	require.ErrorIs(t, err, errNoGameFile)
}

func TestWithCoreReadiness_CoversArcadeOnlyAndAliasedLaunchers(t *testing.T) {
	t.Parallel()

	launch := func(*config.Instance, string, *platforms.LaunchOptions) (*os.Process, error) {
		return &os.Process{}, nil
	}
	ls := withCoreReadiness(nil, []platforms.Launcher{
		{ID: "CPS1", SystemID: "CPS1", Extensions: []string{".mra"}, Launch: launch},
		{ID: "NeoGeoMVS", SystemID: "NeoGeoMVS", Extensions: []string{".neo"}, Launch: launch},
		{ID: "Unknown", SystemID: "NoSuchSystem", Extensions: []string{".bin"}, Launch: launch},
	})

	assert.NotNil(t, ls[0].WaitForReady, "an MRA-only system has no catalog core but still launches MRAs")
	assert.NotNil(t, ls[1].WaitForReady, "MVS loads through the NeoGeo core")
	assert.Nil(t, ls[2].WaitForReady, "no core and no MRA: nothing to wait for")
}

func TestCoreReadiness_StreamedGameHeldOpenAndIdleIsHandedOver(t *testing.T) {
	t.Parallel()
	h := newReadyHarness(t)
	disk := "/media/fat/games/Genesis/disk.md"
	h.files[disk] = 900 << 10
	done := h.start(t.Context(), mustCore(t, "Genesis"), disk)

	// The core opens the image, reads it once, and never closes it.
	h.events <- gamehandoff.Open
	h.events <- gamehandoff.Access
	deadline := time.After(10 * time.Second)
	for {
		select {
		case err := <-done:
			require.NoError(t, err)
			return
		case <-deadline:
			t.Fatal("a game held open and idle was never handed over")
		case <-time.After(time.Millisecond):
			h.clock.Advance(readySettle)
		}
	}
}

func writeMRAWithRoms(t *testing.T, setname string, zips ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "game.mra")
	var roms strings.Builder
	for _, zip := range zips {
		roms.WriteString(`<rom index="0" zip="` + zip + `"><part>00</part></rom>`)
	}
	body := `<misterromdescription><name>Game</name><setname>` + setname + `</setname>` +
		roms.String() + `</misterromdescription>`
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

func TestCoreReadiness_ArcadeReadyTimeoutScalesWithTheRomArchives(t *testing.T) {
	t.Parallel()
	h := newReadyHarness(t)
	h.files["/media/fat/games/mame/big.zip"] = 22 << 20
	h.files["/media/fat/games/hbmame/hack.zip"] = 2 << 20
	h.files["/media/fat/games/mame/huge.zip"] = 4 << 30

	tests := []struct {
		name string
		zips []string
		want time.Duration
	}{
		{"one archive", []string{"big.zip"}, readyArcadeBase + 23*readyArcadePerMB},
		{"archives in both folders add up", []string{"big.zip", "hack.zip"}, readyArcadeBase + 25*readyArcadePerMB},
		{"a repeated archive counts once", []string{"big.zip", "big.zip"}, readyArcadeBase + 23*readyArcadePerMB},
		{"alternatives count the one found", []string{"missing.zip|big.zip"}, readyArcadeBase + 23*readyArcadePerMB},
		{"nothing found", []string{"missing.zip"}, readyArcadeBase + readyArcadePerMB},
		{"capped", []string{"huge.zip"}, readyArcadeMax},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mra := writeMRAWithRoms(t, "set", tt.zips...)
			assert.Equal(t, tt.want, h.r.readyTimeout(probeMedia(mra)))
		})
	}
}

// A game file's wait has room for Main to load the core before the open window
// starts, so it outlasts the service default.
func TestCoreReadiness_ReadyTimeoutCoversTheCoreLoad(t *testing.T) {
	t.Parallel()
	h := newReadyHarness(t)
	h.files[genesisGame] = 64 << 20

	assert.Greater(t, h.r.readyTimeout(probeMedia(genesisGame)), readyCoreWait+readyOpenWindow)
	absent := probeMedia(filepath.Join(t.TempDir(), "absent.mra"))
	assert.Zero(t, h.r.readyTimeout(absent), "an unreadable MRA keeps the default")
}

// keyRecorder stands in for the platform keyboard.
type keyRecorder struct {
	err  error
	keys []string
	mu   syncutil.Mutex
}

func (k *keyRecorder) press(key string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.keys = append(k.keys, key)
	return k.err
}

func (k *keyRecorder) pressed() []string {
	k.mu.Lock()
	defer k.mu.Unlock()
	return append([]string(nil), k.keys...)
}

func TestCoreReadiness_ProbeSendsNoKeys(t *testing.T) {
	t.Parallel()
	h := newReadyHarness(t)
	h.files[genesisGame] = 4 << 20
	keys := &keyRecorder{}
	h.r.pressKey = keys.press
	done := h.start(t.Context(), mustCore(t, "Genesis"), genesisGame)

	h.events <- gamehandoff.Open
	h.events <- gamehandoff.Close

	require.NoError(t, h.advanceUntilDone(t, done))
	assert.Empty(t, keys.pressed(), "a launch that types nothing gets no stray key")
}

func TestCoreReadiness_PrimesOncePerCoreLoad(t *testing.T) {
	t.Parallel()
	h := newReadyHarness(t)
	h.names = []string{"Genesis"}
	keys := &keyRecorder{}
	h.r.pressKey = keys.press
	h.setWritten(h.clock.Now())

	prime := func() <-chan error {
		done := make(chan error, 1)
		go func() { done <- h.r.primeForInput(t.Context()) }()
		return done
	}

	done := prime()
	require.NoError(t, h.clock.BlockUntilContext(t.Context(), 1))
	h.clock.Advance(readyPrimeSettle)
	require.NoError(t, <-done)
	assert.Equal(t, []string{readyPrimeKey}, keys.pressed(), "one inert tap")

	require.NoError(t, h.r.primeForInput(t.Context()))
	assert.Len(t, keys.pressed(), 1, "the same core load is primed once")

	h.setWritten(h.clock.Now().Add(time.Minute))
	done = prime()
	require.NoError(t, h.clock.BlockUntilContext(t.Context(), 1))
	h.clock.Advance(readyPrimeSettle)
	require.NoError(t, <-done)
	assert.Len(t, keys.pressed(), 2, "a newly loaded core is primed again")
}

func TestCoreReadiness_NoPrimeInTheMenu(t *testing.T) {
	t.Parallel()
	h := newReadyHarness(t)
	h.names = []string{misterconfig.MenuCore}
	keys := &keyRecorder{}
	h.r.pressKey = keys.press
	h.setWritten(h.clock.Now())

	require.NoError(t, h.r.primeForInput(t.Context()))
	assert.Empty(t, keys.pressed())
}

func TestCoreReadiness_FailedPrimeDoesNotHoldInput(t *testing.T) {
	t.Parallel()
	h := newReadyHarness(t)
	h.names = []string{"Genesis"}
	keys := &keyRecorder{err: errors.New("no keyboard")}
	h.r.pressKey = keys.press
	h.setWritten(h.clock.Now())

	require.NoError(t, h.r.primeForInput(t.Context()))
	assert.Len(t, keys.pressed(), 1)
}

func TestUsePlatform_SetsTheKeyboardToPrime(t *testing.T) {
	t.Parallel()
	r := newCoreReadiness()

	r.usePlatform(nil)
	r.keyMu.Lock()
	assert.Nil(t, r.pressKey, "a nil platform changes nothing")
	r.keyMu.Unlock()
}

// MiSTer, 2026-10-26: with the card busy, Main took 7 to 14 s to load a core
// and only then opened the game; the open window, counted from the launch,
// ran out first and released media_ready before the game had loaded.
func TestCoreReadiness_WaitCoreUpFollowsTheCoreName(t *testing.T) {
	t.Parallel()
	h := newReadyHarness(t)
	launchedAt := h.clock.Now()
	h.names = []string{misterconfig.MenuCore}
	h.setWritten(launchedAt.Add(-time.Minute))

	up := h.r.waitCoreUp(t.Context(), launchedAt)
	for range 3 {
		require.NoError(t, h.clock.BlockUntilContext(t.Context(), 1))
		h.clock.Advance(readyCorePoll)
	}
	// Waiting again means the last check has run and found the menu.
	require.NoError(t, h.clock.BlockUntilContext(t.Context(), 1))
	select {
	case <-up:
		t.Fatal("up before Main loaded a core")
	default:
	}

	h.setCore("NES", h.clock.Now())
	h.clock.Advance(readyCorePoll)
	select {
	case <-up:
	case <-time.After(time.Second):
		t.Fatal("not up after Main reported the core")
	}
}

// A core that never reports in releases the window after readyCoreWait, so the
// wait stays bounded.
func TestCoreReadiness_WaitCoreUpGivesUpAfterTheBound(t *testing.T) {
	t.Parallel()
	h := newReadyHarness(t)
	launchedAt := h.clock.Now()
	h.names = []string{misterconfig.MenuCore}
	h.setWritten(launchedAt.Add(-time.Minute))

	up := h.r.waitCoreUp(t.Context(), launchedAt)
	require.NoError(t, h.clock.BlockUntilContext(t.Context(), 1))
	h.clock.Advance(readyCoreWait)

	select {
	case <-up:
	case <-time.After(time.Second):
		t.Fatal("still held after readyCoreWait")
	}
}

// The game is not given up on as never opened while Main is still loading the
// core: the open window only starts once the core is up.
func TestCoreReadiness_OpenWindowWaitsForTheCore(t *testing.T) {
	t.Parallel()
	h := newReadyHarness(t)
	h.files[genesisGame] = 4 << 20
	coreUp := make(chan struct{})
	h.r.coreUp = func(context.Context, time.Time) <-chan struct{} { return coreUp }
	done := h.start(t.Context(), mustCore(t, "Genesis"), genesisGame)

	// A window counted from the launch would be waiting on the clock now; let
	// it run out if it is.
	waitCtx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	if h.clock.BlockUntilContext(waitCtx, 1) == nil {
		h.clock.Advance(3 * readyOpenWindow)
	}
	select {
	case err := <-done:
		t.Fatalf("probe gave up while the core was loading: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(coreUp)
	h.events <- gamehandoff.Open
	h.events <- gamehandoff.Close
	require.NoError(t, h.advanceUntilDone(t, done))
}
