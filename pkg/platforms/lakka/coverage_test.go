//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package lakka

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/jonboulle/clockwork"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// faultFs fails chosen operations on top of an in-memory filesystem.
type faultFs struct {
	afero.Fs
	openErr    map[string]error
	readErr    map[string]error
	failCreate bool
}

func newFaultFs() *faultFs {
	return &faultFs{Fs: afero.NewMemMapFs(), openErr: map[string]error{}, readErr: map[string]error{}}
}

func (f *faultFs) Open(name string) (afero.File, error) {
	if err := f.openErr[name]; err != nil {
		return nil, err
	}
	file, err := f.Fs.Open(name)
	if err != nil {
		return nil, err //nolint:wrapcheck // Passes the wrapped filesystem's error through.
	}
	return faultFile{File: file, err: f.readErr[name]}, nil
}

func (f *faultFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if f.failCreate && flag&os.O_CREATE != 0 {
		return nil, syscall.EIO
	}
	return f.Fs.OpenFile(name, flag, perm) //nolint:wrapcheck // Passes the wrapped filesystem's error through.
}

type faultFile struct {
	afero.File
	err error
}

// Read returns what it read along with the injected error, as a failing disk
// can.
func (f faultFile) Read(b []byte) (int, error) {
	n, err := f.File.Read(b)
	if f.err != nil {
		return n, f.err
	}
	return n, err //nolint:wrapcheck // Passes the wrapped file's error through.
}

func TestProbeLoadTreatsLongSilenceAsALongLoad(t *testing.T) {
	t.Parallel()
	state := &retroArchState{status: contentless, onCommand: func(s *retroArchState, msg string) {
		if strings.HasPrefix(msg, commandLoadContent) {
			s.silent = 2
		}
	}}
	ra := newFakeRetroArch(t, state.handle)
	f, _, clock := testFrontend(t, ra.addr())

	var loaded bool
	var err error
	drive(t, clock, loadProbeTimeout, func() {
		loaded, err = f.loadOverNetwork(context.Background(), testCore, testContent)
	})
	require.NoError(t, err)
	assert.True(t, loaded)
	assert.Equal(t, loadSupported, f.capability)
}

func TestProbeLoadAcceptsDifferentlyNamedContent(t *testing.T) {
	t.Parallel()
	state := &retroArchState{status: contentless, onCommand: func(s *retroArchState, msg string) {
		if strings.HasPrefix(msg, commandLoadContent) {
			s.status = "GET_STATUS PLAYING super_nes,Database Title,crc32=1234abcd"
		}
	}}
	ra := newFakeRetroArch(t, state.handle)
	f, _, clock := testFrontend(t, ra.addr())

	var loaded bool
	var err error
	drive(t, clock, pollInterval, func() {
		loaded, err = f.loadOverNetwork(context.Background(), testCore, testContent)
	})
	require.NoError(t, err)
	assert.True(t, loaded)
	assert.Equal(t, loadSupported, f.capability)
}

func TestProbeLoadStopsWhenContextEnds(t *testing.T) {
	t.Parallel()
	state := &retroArchState{status: contentless}
	ra := newFakeRetroArch(t, state.handle)
	f, _, clock := testFrontend(t, ra.addr())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := f.loadOverNetwork(ctx, testCore, testContent)
		done <- err
	}()
	require.NoError(t, clock.BlockUntilContext(context.Background(), 1))
	cancel()
	err := <-done
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, loadUnknown, f.capability)
}

func TestLaunchTransientStopFailures(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")

	t.Run("previous game", func(t *testing.T) {
		t.Parallel()
		f, exec, _ := testFrontend(t, closedAddr(t))
		f.capability = loadUnsupported
		expectSystemctl(exec, nil, "is-active", "--quiet", transientUnit)
		expectSystemctl(exec, boom, "stop", transientUnit)

		err := f.launch(context.Background(), testArgv, testContent)
		require.ErrorIs(t, err, boom)
		require.ErrorContains(t, err, "stop previous game")
		assert.Empty(t, f.currentLaunch())
	})

	t.Run("retroarch service", func(t *testing.T) {
		t.Parallel()
		f, exec, _ := testFrontend(t, closedAddr(t))
		f.capability = loadUnsupported
		expectSystemctl(exec, errInactive, "is-active", "--quiet", transientUnit)
		expectSystemctl(exec, boom, "stop", retroArchService)
		expectSystemctl(exec, errors.New("start failed"), "--no-block", "start", retroArchService)

		err := f.launch(context.Background(), testArgv, testContent)
		require.ErrorIs(t, err, boom)
		require.ErrorContains(t, err, "stop retroarch service")
		assert.Empty(t, f.currentLaunch())
	})
}

func TestStopEndsWhenContextEnds(t *testing.T) {
	t.Parallel()
	state := &retroArchState{status: playingGame}
	ra := newFakeRetroArch(t, state.handle)
	f, exec, clock := testFrontend(t, ra.addr())
	expectSystemctl(exec, errInactive, "is-active", "--quiet", transientUnit)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- f.stop(ctx) }()
	require.NoError(t, clock.BlockUntilContext(context.Background(), 1))
	cancel()
	err := <-done
	require.ErrorIs(t, err, platforms.ErrStopFailed)
	require.ErrorIs(t, err, context.Canceled)
	// The command is fire and forget, so it may land after stop returns.
	require.Eventually(t, func() bool { return ra.count(commandCloseContent) == 1 }, time.Second, time.Millisecond)
}

func TestGameRunningWithUnusableAddress(t *testing.T) {
	t.Parallel()
	f, exec, _ := testFrontend(t, "127.0.0.1:notaport")
	expectSystemctl(exec, errInactive, "is-active", "--quiet", transientUnit)

	running, err := f.gameRunning(context.Background())
	require.ErrorContains(t, err, "dial retroarch network command interface")
	assert.False(t, running)
}

func TestStartPostWithNetworkCommandsOffAndReturnToMenu(t *testing.T) {
	t.Parallel()
	state := &retroArchState{status: contentless}
	ra := newFakeRetroArch(t, state.handle)
	p, exec, _ := testPlatform(t, ra.addr())
	require.NoError(t, afero.WriteFile(p.fs, RetroArchConfigPath, []byte("network_cmd_enable = \"false\"\n"), 0o644))
	expectSystemctl(exec, errInactive, "is-active", "--quiet", transientUnit).Twice()
	expectSystemctl(exec, nil, "is-active", "--quiet", retroArchService)

	holder := &mediaHolder{}
	holder.set(&models.ActiveMedia{Path: testContent})
	require.NoError(t, p.StartPost(context.Background(), testConfig(t), nil, holder.get, holder.set, nil, nil))
	t.Cleanup(func() { _ = p.Stop() })

	p.frontend.mu.Lock()
	p.frontend.launchedPath = testContent
	p.frontend.mu.Unlock()
	require.NoError(t, p.ReturnToMenu())
	assert.Empty(t, p.frontend.currentLaunch())
	assert.Nil(t, holder.get())
	assert.Zero(t, ra.count(commandCloseContent), "nothing loaded, nothing to close")
}

func TestSupportedReaders(t *testing.T) {
	t.Parallel()
	p, _, _ := testPlatform(t, closedAddr(t))
	assert.NotEmpty(t, p.SupportedReaders(testConfig(t)))
}

func TestLaunchMediaWithoutMatchingLauncher(t *testing.T) {
	t.Parallel()
	p, _, _ := testPlatform(t, closedAddr(t))
	err := p.LaunchMedia(testConfig(t), "/nowhere/file.unknown-extension", nil, nil, nil)
	require.ErrorContains(t, err, "error finding launcher")
}

func TestLauncherBuildFailureAndGameRunning(t *testing.T) {
	t.Parallel()
	state := &retroArchState{status: contentless}
	ra := newFakeRetroArch(t, state.handle)
	p, _, _ := testPlatform(t, ra.addr())
	cfg := testConfig(t)
	launcher := snesLauncher(t, p.Launchers(cfg))

	proc, err := launcher.Launch(cfg, testContent, &platforms.LaunchOptions{})
	require.ErrorContains(t, err, "retroarch is not installed")
	assert.Nil(t, proc)
	assert.Empty(t, ra.sent(), "a launch that cannot be built never reaches RetroArch")
	assert.Empty(t, p.frontend.currentLaunch())

	running, err := launcher.GameRunning(context.Background(), cfg, nil)
	require.NoError(t, err)
	assert.False(t, running)

	state2 := &retroArchState{status: playingGame}
	ra2 := newFakeRetroArch(t, state2.handle)
	p2, _, _ := testPlatform(t, ra2.addr())
	launcher2 := snesLauncher(t, p2.Launchers(cfg))
	running, err = launcher2.GameRunning(context.Background(), cfg, nil)
	require.NoError(t, err)
	assert.True(t, running)
}

func TestInstallFailures(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	const exe = "/storage/zaparoo/zaparoo"
	withConfig := func(t *testing.T, fs afero.Fs) {
		t.Helper()
		require.NoError(t, afero.WriteFile(fs, RetroArchConfigPath, []byte("network_cmd_enable = \"true\"\n"), 0o644))
	}

	t.Run("unit directory", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewReadOnlyFs(afero.NewMemMapFs())
		inst := &Installer{FS: fs, Exec: newExec(t), Unit: testUnit, Executable: exe}
		require.ErrorContains(t, inst.Install(context.Background()), "create systemd unit directory")
	})

	t.Run("unit file", func(t *testing.T) {
		t.Parallel()
		fs := newFaultFs()
		fs.failCreate = true
		inst := &Installer{FS: fs, Exec: newExec(t), Unit: testUnit, Executable: exe}
		require.ErrorContains(t, inst.Install(context.Background()), "write service file")
	})

	t.Run("daemon reload", func(t *testing.T) {
		t.Parallel()
		exec := newExec(t)
		expectSystemctl(exec, boom, "daemon-reload")
		inst := &Installer{FS: afero.NewMemMapFs(), Exec: exec, Unit: testUnit, Executable: exe}
		err := inst.Install(context.Background())
		require.ErrorIs(t, err, boom)
		require.ErrorContains(t, err, "systemctl daemon-reload")
	})

	t.Run("enable", func(t *testing.T) {
		t.Parallel()
		exec := newExec(t)
		expectSystemctl(exec, nil, "daemon-reload")
		expectSystemctl(exec, boom, "enable", serviceName)
		inst := &Installer{FS: afero.NewMemMapFs(), Exec: exec, Unit: testUnit, Executable: exe}
		require.ErrorContains(t, inst.Install(context.Background()), "systemctl enable "+serviceName)
	})

	t.Run("network commands", func(t *testing.T) {
		t.Parallel()
		exec := newExec(t)
		expectSystemctl(exec, nil, "daemon-reload")
		expectSystemctl(exec, nil, "enable", serviceName)
		inst := &Installer{FS: afero.NewMemMapFs(), Exec: exec, Unit: testUnit, Executable: exe}
		require.ErrorContains(t, inst.Install(context.Background()), "read retroarch default config")
	})

	t.Run("uninstall remove", func(t *testing.T) {
		t.Parallel()
		base := afero.NewMemMapFs()
		withConfig(t, base)
		exec := newExec(t)
		expectSystemctl(exec, boom, "stop", serviceName)
		expectSystemctl(exec, boom, "disable", serviceName)
		inst := &Installer{FS: afero.NewReadOnlyFs(base), Exec: exec, Unit: testUnit, Executable: exe}
		require.ErrorContains(t, inst.Uninstall(context.Background()), "remove service file")
	})
}

func TestEnableNetworkCommandsFailures(t *testing.T) {
	t.Parallel()
	const off = "network_cmd_enable = \"false\"\n"

	t.Run("start after edit", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(fs, RetroArchConfigPath, []byte(off), 0o644))
		exec := newExec(t)
		expectSystemctl(exec, nil, "stop", retroArchService)
		expectSystemctl(exec, errors.New("boom"), "start", retroArchService)

		require.ErrorContains(t, EnableNetworkCommands(context.Background(), fs, exec), "start retroarch")
		data, err := afero.ReadFile(fs, RetroArchConfigPath)
		require.NoError(t, err)
		assert.True(t, NetworkCommandsEnabled(string(data)), "the edit still landed")
	})

	t.Run("config gone after stop", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(fs, RetroArchConfigPath, []byte(off), 0o644))
		exec := newExec(t)
		exec.On("Run", mock.Anything, "systemctl", []string{"stop", retroArchService}).
			Run(func(mock.Arguments) { _ = fs.Remove(RetroArchConfigPath) }).Return(nil).Once()
		expectSystemctl(exec, nil, "start", retroArchService)

		require.ErrorContains(t, EnableNetworkCommands(context.Background(), fs, exec), "read retroarch default config")
		exists, err := afero.Exists(fs, RetroArchConfigPath)
		require.NoError(t, err)
		assert.False(t, exists)
	})

	t.Run("write", func(t *testing.T) {
		t.Parallel()
		fs := newFaultFs()
		require.NoError(t, afero.WriteFile(fs.Fs, RetroArchConfigPath, []byte(off), 0o644))
		fs.failCreate = true
		exec := newExec(t)
		expectSystemctl(exec, nil, "stop", retroArchService)
		expectSystemctl(exec, nil, "start", retroArchService)

		require.ErrorContains(t, EnableNetworkCommands(context.Background(), fs, exec), "write retroarch config")
	})

	t.Run("unreadable config", func(t *testing.T) {
		t.Parallel()
		fs := newFaultFs()
		fs.openErr[RetroArchConfigPath] = syscall.EACCES
		exec := newExec(t)

		err := EnableNetworkCommands(context.Background(), fs, exec)
		require.ErrorIs(t, err, syscall.EACCES)
		require.ErrorContains(t, err, "read retroarch config")
	})
}

func TestParseConfigLineSkipsLinesWithoutEquals(t *testing.T) {
	t.Parallel()
	_, _, ok := parseConfigLine("network_cmd_enable true")
	assert.False(t, ok)
	assert.Equal(t, map[string]string{networkCmdPortKey: networkCmdPort},
		configValues("network_cmd_enable true\nnetwork_cmd_port = \"55355\"\n"))
}

func TestParsePlaylistEntryCap(t *testing.T) {
	t.Parallel()
	var js strings.Builder
	js.WriteString(`{"items": [`)
	for i := range maxPlaylistEntries + 1 {
		if i > 0 {
			js.WriteString(",")
		}
		fmt.Fprintf(&js, `{"path": "/g/%d.sfc"}`, i)
	}
	js.WriteString("]}")
	pl, err := parsePlaylist([]byte(js.String()))
	require.NoError(t, err)
	assert.Len(t, pl.Entries, maxPlaylistEntries)

	var legacy strings.Builder
	for i := range maxPlaylistEntries + 1 {
		fmt.Fprintf(&legacy, "/g/%d.sfc\nGame\nDETECT\nDETECT\nDETECT\n%s\n", i, snesPlaylistName)
	}
	pl, err = parsePlaylist([]byte(legacy.String()))
	require.NoError(t, err)
	assert.Len(t, pl.Entries, maxPlaylistEntries)
}

func TestPlaylistStoreCapsFileCount(t *testing.T) {
	t.Parallel()
	p, _, _ := testPlatform(t, closedAddr(t))
	for i := range maxPlaylistFiles + 1 {
		writePlaylist(t, p.fs, fmt.Sprintf("p%04d.lpl", i), `{"items": []}`)
	}
	p.playlists.load()
	assert.Len(t, p.playlists.files, maxPlaylistFiles)
	assert.NotContains(t, p.playlists.files, fmt.Sprintf("p%04d.lpl", maxPlaylistFiles))
}

func TestPlaylistStoreSkipsUnreadablePlaylists(t *testing.T) {
	t.Parallel()
	fs := newFaultFs()
	p := newTestablePlatform(fs, newExec(t), clockwork.NewFakeClock(), closedAddr(t))
	game := filepath.Join(romsDir, "snes", "Game.sfc")
	body := `{"items": [{"path": "` + game + `"}, {"path": "relative/Game.sfc"}]}`
	writePlaylist(t, fs, snesPlaylistName, body)
	writePlaylist(t, fs, "Sony - PlayStation.lpl", `{"items": [{"path": "/storage/roms/psx/A.cue"}]}`)
	writePlaylist(t, fs, "Sega - Mega Drive - Genesis.lpl", `{"items": [{"path": "/storage/roms/md/A.md"}]}`)
	fs.openErr[filepath.Join(playlistsDir, "Sony - PlayStation.lpl")] = syscall.EACCES
	fs.readErr[filepath.Join(playlistsDir, "Sega - Mega Drive - Genesis.lpl")] = syscall.EIO

	index := p.playlists.load()
	require.Len(t, index.byPath, 1)
	snes := index.bySystem[systemdefs.SystemSNES]
	require.Len(t, snes, 1)
	assert.Equal(t, game, snes[0].Path)
	assert.Empty(t, index.bySystem[systemdefs.SystemPSX])
	assert.Empty(t, index.bySystem[systemdefs.SystemGenesis])
}

func TestTrackerKeepsStateWhenStatusFails(t *testing.T) {
	t.Parallel()
	f, _, _ := testFrontend(t, closedAddr(t))
	holder := &mediaHolder{}
	tr := &tracker{frontend: f, activeMedia: holder.get, setActiveMedia: holder.set}
	f.launchedPath = testContent
	media := &models.ActiveMedia{Path: testContent, Started: time.Unix(1, 0)}
	holder.set(media)
	tr.mediaKey = testContent + "\x00" + media.Started.String()
	tr.confirmed = true

	tr.poll(context.Background())
	assert.True(t, tr.confirmed)
	assert.Same(t, media, holder.get())
	assert.Equal(t, testContent, f.currentLaunch())
}

func TestTrackerYieldsToLaunchDuringStatusQuery(t *testing.T) {
	t.Parallel()
	holder := &mediaHolder{}
	replacement := &models.ActiveMedia{Path: testContent, Started: time.Unix(2, 0)}
	status := &lockedStatus{status: playingGame}
	swap := false
	ra := newFakeRetroArch(t, func(msg string) (string, bool) {
		reply, ok := status.handle(msg)
		if swap {
			holder.set(replacement)
		}
		return reply, ok
	})
	f, _, _ := testFrontend(t, ra.addr())
	tr := &tracker{frontend: f, activeMedia: holder.get, setActiveMedia: holder.set}
	f.launchedPath = testContent
	holder.set(&models.ActiveMedia{Path: testContent, Started: time.Unix(1, 0)})
	ctx := context.Background()

	tr.poll(ctx)
	require.True(t, tr.confirmed)

	// The next query sees RetroArch idle, but a new launch of the same file
	// replaced the media while it was in flight.
	status.set(contentless)
	ra.mu.Lock()
	swap = true
	ra.mu.Unlock()
	tr.poll(ctx)
	assert.Same(t, replacement, holder.get())
	assert.Equal(t, testContent, f.currentLaunch())
}
