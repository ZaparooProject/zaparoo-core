//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package esbase

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esapi"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/kodi"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type nopKeyboard struct{}

func (nopKeyboard) KeyDown(int) error { return nil }
func (nopKeyboard) KeyUp(int) error   { return nil }
func (nopKeyboard) Close() error      { return nil }

// startSleeper runs a child process that stays up until it is killed. The
// channel closes once it has exited.
func startSleeper(t *testing.T) (proc *os.Process, exited <-chan struct{}) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "sleep", "30")
	require.NoError(t, cmd.Start())
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		<-done
	})
	return cmd.Process, done
}

func kodiConfig(t *testing.T) (*config.Instance, *helpers.MockKodiServer) {
	t.Helper()
	srv := helpers.NewMockKodiServer(t)
	t.Cleanup(srv.Close)
	cfg, err := config.NewConfig(t.TempDir(), config.Values{
		Launchers: config.Launchers{
			Default: []config.LaunchersDefault{{Launcher: "Kodi", ServerURL: srv.URL()}},
		},
	})
	require.NoError(t, err)
	return cfg, srv
}

func sentKodiMethods(srv *helpers.MockKodiServer) string {
	var sent strings.Builder
	for _, r := range srv.Requests() {
		sent.Write(r)
	}
	return sent.String()
}

func TestPlatform_TrivialMethods(t *testing.T) {
	t.Parallel()

	p := New(&Options{ID: "rocknix", API: &fakeAPI{}})
	require.NoError(t, p.ScanHook(nil))
	_, err := p.Screenshot()
	require.ErrorIs(t, err, platforms.ErrNotSupported)
	_, err = p.ForwardCmd(&platforms.CmdEnv{Cmd: zapscript.Command{Name: "mister.ini"}})
	require.ErrorContains(t, err, "command not supported on rocknix")
	mapping, ok := p.LookupMapping(nil)
	assert.False(t, ok)
	assert.Empty(t, mapping)
	assert.NotNil(t, p.ConsoleManager())
	assert.NotEmpty(t, p.SupportedReaders(mustConfig(t)))
}

func TestStartPre_InitialisesInputDevices(t *testing.T) {
	t.Parallel()

	p := New(&Options{ID: "rocknix"})
	p.NewKeyboard = func() (shared.KeyboardDevice, error) { return nopKeyboard{}, nil }
	require.NoError(t, p.StartPre(mustConfig(t)))
	assert.NotNil(t, p.clock)
	require.NoError(t, p.Stop())

	failing := New(&Options{ID: "rocknix"})
	failing.NewKeyboard = func() (shared.KeyboardDevice, error) { return nil, errors.New("no uinput") }
	require.ErrorContains(t, failing.StartPre(mustConfig(t)), "failed to initialize input devices")
}

func TestStop_StopsTheTracker(t *testing.T) {
	t.Parallel()

	stopped := 0
	p := New(&Options{ID: "rocknix"})
	p.stopTracker = func() error {
		stopped++
		return errors.New("already stopped")
	}
	require.NoError(t, p.Stop(), "a tracker that fails to stop does not fail the platform stop")
	assert.Equal(t, 1, stopped)
}

func TestESGameRunning_ReportsTheAPI(t *testing.T) {
	t.Parallel()

	api := &fakeAPI{game: &esapi.RunningGameResponse{Path: "/roms/nes/a.nes"}}
	p := New(&Options{ID: "rocknix", API: api})
	running, err := p.esGameRunning(context.Background(), nil, nil)
	require.NoError(t, err)
	assert.True(t, running)

	api.err = errors.New("refused")
	_, err = p.esGameRunning(context.Background(), nil, nil)
	require.ErrorContains(t, err, "ask EmulationStation for its running game")
}

func TestStopActiveLauncher_KillsTheTrackedProcess(t *testing.T) {
	t.Parallel()

	api := &fakeAPI{game: &esapi.RunningGameResponse{SystemName: "nes", Path: "/roms/nes/a.nes"}}
	opts := Options{ID: "rocknix", API: api, KillGame: func() error {
		api.game = nil
		return nil
	}}
	p, _ := runningPlatform(t, &opts)
	proc, exited := startSleeper(t)
	p.SetTrackedProcess(proc)

	require.NoError(t, p.StopActiveLauncher(platforms.StopForMenu))
	assert.Nil(t, p.trackedProcess)
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("the tracked process was not killed")
	}
}

func kodiPlatform(t *testing.T) (*Platform, *helpers.MockKodiServer, *[]*models.ActiveMedia) {
	t.Helper()
	cfg, srv := kodiConfig(t)
	opts := Options{ID: "batocera", API: &fakeAPI{}}
	p, published := runningPlatform(t, &opts)
	p.cfg = cfg
	media := models.NewActiveMedia(systemdefs.SystemVideo, "Video", "kodi-movie://7/Heat", "Heat", "KodiMovie")
	p.activeMedia = func() *models.ActiveMedia { return media }
	p.kodiActive = true
	return p, srv, published
}

func TestStopKodi_ForMenuStopsPlayback(t *testing.T) {
	t.Parallel()

	p, srv, published := kodiPlatform(t)
	srv.WithActivePlayers()
	require.NoError(t, p.StopActiveLauncher(platforms.StopForMenu))
	assert.Contains(t, sentKodiMethods(srv), string(kodi.APIMethodPlayerStop))
	assert.Empty(t, *published, "Kodi stays running, so its media stays published")
	assert.True(t, p.kodiActive)

	srv.FailMethod(kodi.APIMethodPlayerGetActivePlayers)
	require.ErrorContains(t, p.StopActiveLauncher(platforms.StopForMenu), "failed to stop Kodi playback")
}

func TestStopKodi_PreemptionQuitsKodi(t *testing.T) {
	t.Parallel()

	p, srv, published := kodiPlatform(t)
	require.NoError(t, p.StopActiveLauncher(platforms.StopForPreemption))
	assert.Contains(t, sentKodiMethods(srv), string(kodi.APIMethodApplicationQuit))
	assert.False(t, p.kodiActive)
	require.Len(t, *published, 1)
	assert.Nil(t, (*published)[0])
}

func TestStopKodi_KillsKodiWhenQuitFails(t *testing.T) {
	t.Parallel()

	p, srv, published := kodiPlatform(t)
	srv.FailMethod(kodi.APIMethodApplicationQuit)
	proc, exited := startSleeper(t)
	p.SetTrackedProcess(proc)

	require.NoError(t, p.StopActiveLauncher(platforms.StopForPreemption))
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("Kodi was not killed")
	}
	assert.Nil(t, p.trackedProcess)
	assert.False(t, p.kodiActive)
	require.Len(t, *published, 1)
	assert.Nil(t, (*published)[0])
}

type cancelledLaunches struct{ ctx context.Context }

func (c cancelledLaunches) GetContext() context.Context { return c.ctx }
func (c cancelledLaunches) NewContext() context.Context { return c.ctx }

func noLaunch(t *testing.T) func(*config.Instance, string, *platforms.LaunchOptions) (*os.Process, error) {
	t.Helper()
	return func(*config.Instance, string, *platforms.LaunchOptions) (*os.Process, error) {
		t.Error("the launcher must not run")
		return nil, nil //nolint:nilnil // a test launcher with no process
	}
}

func TestLaunchMedia_NoLauncherForThePath(t *testing.T) {
	t.Parallel()

	p := New(&Options{ID: "rocknix", API: &fakeAPI{}})
	err := p.LaunchMedia(mustConfig(t), "/nowhere/file.unknownext", nil, nil, nil)
	require.ErrorContains(t, err, "error finding launcher")
}

func TestLaunchMedia_FailsWhenTheRunningGameWillNotStop(t *testing.T) {
	t.Parallel()

	api := &fakeAPI{game: &esapi.RunningGameResponse{SystemName: "nes", Path: "/roms/nes/a.nes"}}
	opts := Options{
		ID: "rocknix", API: api, KillRetryDelay: time.Millisecond,
		KillGame: func() error { return errors.New("still there") },
	}
	p, _ := runningPlatform(t, &opts)
	cancelled := 0
	launchOpts := &platforms.LaunchOptions{RecordMediaStop: func(string) func() {
		return func() { cancelled++ }
	}}
	launcher := &platforms.Launcher{ID: "NES", Launch: noLaunch(t)}

	err := p.LaunchMedia(mustConfig(t), "/roms/nes/b.nes", launcher, nil, launchOpts)
	require.ErrorContains(t, err, "failed to stop active launcher")
	assert.Equal(t, 1, cancelled, "the recorded stop is withdrawn")
}

func TestLaunchMedia_StoppedBeforeLaunch(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := New(&Options{ID: "rocknix", API: &fakeAPI{}})
	p.launcherManager = cancelledLaunches{ctx: ctx}
	launcher := &platforms.Launcher{ID: "NES", Launch: noLaunch(t)}

	err := p.LaunchMedia(mustConfig(t), "/roms/nes/b.nes", launcher, nil, nil)
	require.ErrorContains(t, err, "stopped before launch")
}

func TestLaunchMedia_StoppedWhileReplacing(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	api := &fakeAPI{game: &esapi.RunningGameResponse{SystemName: "nes", Path: "/roms/nes/a.nes"}}
	opts := Options{ID: "rocknix", API: api, KillGame: func() error {
		api.game = nil
		return nil
	}}
	p, _ := runningPlatform(t, &opts)
	p.launcherManager = cancelledLaunches{ctx: ctx}
	launcher := &platforms.Launcher{ID: "NES", Launch: noLaunch(t)}

	err := p.LaunchMedia(mustConfig(t), "/roms/nes/b.nes", launcher, nil, nil)
	require.ErrorContains(t, err, "stopped while replacing running media")
}

func TestLaunchMedia_KeepsKodiRunningBetweenKodiItems(t *testing.T) {
	t.Parallel()

	cfg, _ := kodiConfig(t)
	api := &fakeAPI{game: &esapi.RunningGameResponse{SystemName: "kodi", Path: "kodi"}}
	opts := Options{ID: "batocera", API: api}
	p, published := runningPlatform(t, &opts)
	media := models.NewActiveMedia(systemdefs.SystemVideo, "Video", "kodi-movie://7/Heat", "Heat", "KodiMovie")
	p.activeMedia = func() *models.ActiveMedia { return media }
	db, cleanup := helpers.NewTestDatabase(t)
	t.Cleanup(cleanup)

	launched := false
	launcher := kodi.NewKodiTVLauncher()
	launcher.Launch = func(*config.Instance, string, *platforms.LaunchOptions) (*os.Process, error) {
		launched = true
		return nil, nil //nolint:nilnil // a test launcher with no process
	}
	launcher.WaitForReady = nil

	require.NoError(t, p.LaunchMedia(cfg, "kodi-episode://3/Pilot", &launcher, db, nil))
	assert.True(t, launched)
	require.NotEmpty(t, *published)
	assert.Nil(t, (*published)[0], "the old item is cleared before the new one starts")
	assert.Zero(t, api.emuKills, "Kodi itself is not stopped")
	assert.True(t, p.kodiActive)
}

func TestLaunchMedia_LaunchErrorIsReturned(t *testing.T) {
	t.Parallel()

	p := New(&Options{ID: "rocknix", API: &fakeAPI{}})
	p.clock = clockwork.NewFakeClock()
	p.activeMedia = func() *models.ActiveMedia { return nil }
	p.setActiveMedia = func(*models.ActiveMedia) {}
	db, cleanup := helpers.NewTestDatabase(t)
	t.Cleanup(cleanup)
	launcher := &platforms.Launcher{
		ID: "NES",
		Launch: func(*config.Instance, string, *platforms.LaunchOptions) (*os.Process, error) {
			return nil, errors.New("no emulator")
		},
	}

	err := p.LaunchMedia(mustConfig(t), "/roms/nes/b.nes", launcher, db, nil)
	require.ErrorContains(t, err, "error launching")
}

func TestStopUnpublishedGame_KillsAGameThatStarts(t *testing.T) {
	t.Parallel()

	api := &fakeAPI{game: &esapi.RunningGameResponse{SystemName: "nes", Path: "/roms/nes/a.nes"}}
	kills := 0
	p := New(&Options{ID: "rocknix", API: api, KillGame: func() error {
		kills++
		return errors.New("kill failed")
	}})
	p.stopUnpublishedGame()
	assert.Equal(t, 1, kills, "a failed kill is logged, not retried")
}

func TestStopUnpublishedGame_GivesUpAfterTheWait(t *testing.T) {
	t.Parallel()

	clock := clockwork.NewFakeClock()
	p := New(&Options{ID: "rocknix", API: &fakeAPI{}, KillGame: func() error {
		t.Error("nothing started, so nothing is killed")
		return nil
	}})
	p.clock = clock
	done := make(chan struct{})
	go func() {
		p.stopUnpublishedGame()
		close(done)
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		select {
		case <-done:
			return
		default:
		}
		require.True(t, time.Now().Before(deadline), "the wait never ended")
		// The poller either sleeps on the clock or has just returned, so a
		// short wait for it to block is retried rather than trusted.
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		if clock.BlockUntilContext(ctx, 1) == nil {
			clock.Advance(unpublishedGamePoll)
		}
		cancel()
	}
}

// Without /runningGame the game starting cannot be seen, so the stop waits out
// the window and then kills once instead of never killing.
func TestStopUnpublishedGame_KillsAfterTheWaitWithoutRunningGameRoute(t *testing.T) {
	t.Parallel()

	clock := clockwork.NewFakeClock()
	kills := 0
	p := New(&Options{
		ID:  "amberelec",
		API: &fakeAPI{err: esapi.ErrRouteUnsupported},
		KillGame: func() error {
			kills++
			return nil
		},
	})
	p.clock = clock
	done := make(chan struct{})
	go func() {
		p.stopUnpublishedGame()
		close(done)
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	require.NoError(t, clock.BlockUntilContext(ctx, 1))
	assert.Zero(t, kills, "nothing is killed before the window ends")
	clock.Advance(unpublishedGameWait)
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("the stop never finished")
	}
	assert.Equal(t, 1, kills)
}

func TestSettle_DefaultsToTheRealClock(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := New(&Options{ID: "rocknix"})
	require.ErrorIs(t, p.settle(ctx, time.Hour), context.Canceled)
	require.NoError(t, p.settle(context.Background(), 0))
}

func systemLauncher(t *testing.T, p *Platform, cfg *config.Instance, systemID string) platforms.Launcher {
	t.Helper()
	launchers := p.Launchers(cfg)
	for i := range launchers {
		if launchers[i].SystemID == systemID && launchers[i].Scanner != nil {
			return launchers[i]
		}
	}
	t.Fatalf("no %s launcher", systemID)
	return platforms.Launcher{}
}

func TestLaunchersScanner_NamesGamesFromGamelist(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	nesDir := filepath.Join(root, "nes")
	require.NoError(t, os.MkdirAll(nesDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(nesDir, "gamelist.xml"), []byte(
		`<gameList><game><path>./mario.nes</path><name>Super Mario Bros.</name></game>`+
			`<game><path>./absent.nes</path><name>Absent</name></game></gameList>`), 0o600))

	opts := Options{ID: "rocknix", API: &fakeAPI{}, DefaultROMRoot: root}
	p := New(&opts)
	cfg := mustConfig(t)
	nes := systemLauncher(t, p, cfg, systemdefs.SystemNES)

	results := []platforms.ScanResult{
		{Path: filepath.Join(nesDir, "mario.nes"), Name: "mario"},
		{Path: filepath.Join(nesDir, "zelda.nes"), Name: "zelda"},
		{Path: filepath.Join(nesDir, "gamelist.xml"), Name: "gamelist"},
		{Path: filepath.Join(nesDir, "images", "mario.nes"), Name: "image"},
	}
	got, err := nes.Scanner(context.Background(), cfg, systemdefs.SystemNES, results)
	require.NoError(t, err)
	require.Len(t, got, 2, "gamelist.xml and media folders are not games")
	assert.Equal(t, "Super Mario Bros.", got[0].Name)
	assert.Equal(t, "zelda", got[1].Name, "a game the gamelist does not name keeps its file name")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = nes.Scanner(ctx, cfg, systemdefs.SystemNES, results[:1])
	require.ErrorIs(t, err, context.Canceled)
}

func TestGenericLauncher_RunsTheScript(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	marker := filepath.Join(dir, "ran")
	script := filepath.Join(dir, "run.sh")
	//nolint:gosec // the test script has to be executable
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\ntouch '"+marker+"'\n"), 0o700))

	p := New(&Options{ID: "rocknix", API: &fakeAPI{}})
	var generic *platforms.Launcher
	launchers := p.Launchers(mustConfig(t))
	for i := range launchers {
		if launchers[i].ID == "Generic" {
			generic = &launchers[i]
		}
	}
	require.NotNil(t, generic)

	proc, err := generic.Launch(nil, script, nil)
	require.NoError(t, err)
	assert.Nil(t, proc)
	assert.Eventually(t, func() bool {
		_, statErr := os.Stat(marker)
		return statErr == nil
	}, 5*time.Second, 10*time.Millisecond)

	_, err = generic.Launch(nil, filepath.Join(dir, "missing.sh"), nil)
	require.ErrorContains(t, err, "failed to start command")
}
