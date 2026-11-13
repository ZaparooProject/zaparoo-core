//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package lakka

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var testArgv = []string{retroArchExecutable, "-L", testCore, testContent}

func loadMessage(content string) string {
	return commandLoadContent + " " + testCore + "|" + content
}

func expectTransientLaunch(exec *mocks.MockCommandExecutor, runErr error) {
	expectSystemctl(exec, errInactive, "is-active", "--quiet", transientUnit)
	expectSystemctl(exec, nil, "stop", retroArchService)
	want := make([]string, 0, 12+len(testArgv))
	want = append(want,
		"--unit=zaparoo-retroarch.service",
		"--collect",
		"--quiet",
		"--expand-environment=no",
		"--property=Conflicts=retroarch.service",
		"--property=After=retroarch.service",
		"--property=TimeoutStopSec=10",
		"--property=ExecStopPost=/usr/bin/systemctl --no-block start retroarch.service",
		"--property=EnvironmentFile=/usr/lib/retroarch/retroarch-env.conf",
		"--property=EnvironmentFile=-/run/libreelec/retroarch.conf",
		"--property=EnvironmentFile=-/storage/.cache/services/retroarch.conf",
		"--",
	)
	want = append(want, testArgv...)
	exec.On("Run", mock.Anything, "systemd-run", want).Return(runErr).Once()
}

func TestLaunchLoadsContentInRunningRetroArch(t *testing.T) {
	t.Parallel()
	state := &retroArchState{status: contentless, onCommand: func(s *retroArchState, msg string) {
		if strings.HasPrefix(msg, commandLoadContent) {
			s.silent = 2
			s.status = playingGame
		}
	}}
	ra := newFakeRetroArch(t, state.handle)
	f, _, clock := testFrontend(t, ra.addr())

	var err error
	drive(t, clock, pollInterval, func() { err = f.launch(context.Background(), testArgv, testContent) })
	require.NoError(t, err)
	assert.Equal(t, 1, ra.count(loadMessage(testContent)))
	assert.Equal(t, testContent, f.currentLaunch())

	// Known support skips the probe: one status check, then the command.
	before := ra.count(commandGetStatus)
	other := "/storage/roms/snes/Other.sfc"
	require.NoError(t, f.launch(context.Background(), []string{retroArchExecutable, "-L", testCore, other}, other))
	assert.Equal(t, before+1, ra.count(commandGetStatus))
	// The command is fire and forget, so it may land after launch returns.
	require.Eventually(t, func() bool { return ra.count(loadMessage(other)) == 1 }, time.Second, time.Millisecond)
	assert.Equal(t, other, f.currentLaunch())
}

func TestLaunchFallsBackWhenLoadContentIsIgnored(t *testing.T) {
	t.Parallel()
	state := &retroArchState{status: contentless}
	ra := newFakeRetroArch(t, state.handle)
	f, exec, clock := testFrontend(t, ra.addr())
	expectTransientLaunch(exec, nil)

	var err error
	drive(t, clock, pollInterval, func() { err = f.launch(context.Background(), testArgv, testContent) })
	require.NoError(t, err)
	assert.Equal(t, loadUnsupported, f.capability)
	assert.Equal(t, testContent, f.currentLaunch())

	// Once known to be unsupported, LOAD_CONTENT is not tried again.
	expectTransientLaunch(exec, nil)
	require.NoError(t, f.launch(context.Background(), testArgv, testContent))
	assert.Equal(t, 1, ra.count(loadMessage(testContent)))
}

func TestLaunchFallsBackWhenOnlyPauseStateChanges(t *testing.T) {
	t.Parallel()
	const other = "super_nes,Other Game (USA),crc32=1"
	state := &retroArchState{status: statusPausedPrefix + other}
	state.onCommand = func(s *retroArchState, msg string) {
		if msg == loadMessage(testContent) {
			// A build without LOAD_CONTENT ignores it while the user resumes.
			s.status = statusPlayingPrefix + other
		}
	}
	ra := newFakeRetroArch(t, state.handle)
	f, exec, clock := testFrontend(t, ra.addr())
	expectTransientLaunch(exec, nil)

	var err error
	drive(t, clock, pollInterval, func() { err = f.launch(context.Background(), testArgv, testContent) })
	require.NoError(t, err)
	assert.Equal(t, loadUnsupported, f.capability)
}

func TestLaunchReportsFailedLoad(t *testing.T) {
	t.Parallel()
	state := &retroArchState{status: contentless, onCommand: func(s *retroArchState, msg string) {
		if strings.HasPrefix(msg, commandLoadContent) {
			s.silent = 2
		}
	}}
	ra := newFakeRetroArch(t, state.handle)
	f, _, clock := testFrontend(t, ra.addr())

	var err error
	drive(t, clock, pollInterval, func() { err = f.launch(context.Background(), testArgv, testContent) })
	require.ErrorContains(t, err, "could not load")
	assert.Equal(t, loadSupported, f.capability)
	assert.Empty(t, f.currentLaunch())
}

func TestLaunchUnreachableRetroArchStartsGameUnit(t *testing.T) {
	t.Parallel()
	f, exec, _ := testFrontend(t, closedAddr(t))
	expectTransientLaunch(exec, nil)

	require.NoError(t, f.launch(context.Background(), testArgv, testContent))
	// Unreachable says nothing about the build, so nothing is cached.
	assert.Equal(t, loadUnknown, f.capability)
}

func TestLaunchGameUnitFailureRestoresMenu(t *testing.T) {
	t.Parallel()
	f, exec, _ := testFrontend(t, closedAddr(t))
	expectTransientLaunch(exec, errors.New("boom"))
	expectSystemctl(exec, nil, "--no-block", "start", retroArchService)

	require.Error(t, f.launch(context.Background(), testArgv, testContent))
	assert.Empty(t, f.currentLaunch())
}

func TestLaunchReplacesRunningGameUnit(t *testing.T) {
	t.Parallel()
	f, exec, _ := testFrontend(t, closedAddr(t))
	expectSystemctl(exec, nil, "is-active", "--quiet", transientUnit)
	expectSystemctl(exec, nil, "stop", transientUnit)
	expectSystemctl(exec, nil, "stop", retroArchService)
	exec.On("Run", mock.Anything, "systemd-run", mock.Anything).Return(nil).Once()

	require.NoError(t, f.launch(context.Background(), testArgv, testContent))
}

func TestLaunchRejectsUnsafePaths(t *testing.T) {
	t.Parallel()
	f, _, _ := testFrontend(t, closedAddr(t))

	require.Error(t, f.launch(context.Background(), testArgv, "/storage/roms/a\nQUIT"))
	require.Error(t, f.launch(context.Background(), []string{retroArchExecutable, testContent}, testContent))
}

func TestLaunchTooLongForNetworkCommandUsesGameUnit(t *testing.T) {
	t.Parallel()
	state := &retroArchState{status: contentless}
	ra := newFakeRetroArch(t, state.handle)
	f, exec, _ := testFrontend(t, ra.addr())
	long := "/storage/roms/" + strings.Repeat("a", maxCommandLen)
	argv := []string{retroArchExecutable, "-L", testCore, long}
	expectSystemctl(exec, errInactive, "is-active", "--quiet", transientUnit)
	expectSystemctl(exec, nil, "stop", retroArchService)
	exec.On("Run", mock.Anything, "systemd-run", mock.Anything).Return(nil).Once()

	require.NoError(t, f.launch(context.Background(), argv, long))
	assert.Empty(t, ra.sent())
}

func TestStopClosesContentWithDoublePress(t *testing.T) {
	t.Parallel()
	state := &retroArchState{status: playingGame}
	presses := 0
	state.onCommand = func(s *retroArchState, msg string) {
		if msg == commandCloseContent {
			presses++
			if presses == 2 {
				s.status = contentless
			}
		}
	}
	ra := newFakeRetroArch(t, state.handle)
	f, exec, clock := testFrontend(t, ra.addr())
	expectSystemctl(exec, errInactive, "is-active", "--quiet", transientUnit)
	f.launchedPath = testContent

	var err error
	drive(t, clock, pollInterval, func() { err = f.stop(context.Background()) })
	require.NoError(t, err)
	assert.Equal(t, 2, ra.count(commandCloseContent))
	assert.Empty(t, f.currentLaunch())
}

func TestStopFailsWhenContentStaysLoaded(t *testing.T) {
	t.Parallel()
	state := &retroArchState{status: playingGame}
	ra := newFakeRetroArch(t, state.handle)
	f, exec, clock := testFrontend(t, ra.addr())
	expectSystemctl(exec, errInactive, "is-active", "--quiet", transientUnit)
	f.launchedPath = testContent

	var err error
	drive(t, clock, pollInterval, func() { err = f.stop(context.Background()) })
	require.ErrorIs(t, err, platforms.ErrStopFailed)
	assert.Equal(t, testContent, f.currentLaunch(), "a failed stop keeps the launch so the tracker still owns it")
}

func TestStopWaitsForLoadingLaunch(t *testing.T) {
	t.Parallel()
	state := &retroArchState{status: playingGame, silent: 2}
	state.onCommand = func(s *retroArchState, msg string) {
		if msg == commandCloseContent {
			s.status = contentless
		}
	}
	ra := newFakeRetroArch(t, state.handle)
	f, exec, clock := testFrontend(t, ra.addr())
	expectSystemctl(exec, errInactive, "is-active", "--quiet", transientUnit)
	expectSystemctl(exec, nil, "is-active", "--quiet", retroArchService).Twice()
	f.launchedPath = testContent

	var err error
	drive(t, clock, pollInterval, func() { err = f.stop(context.Background()) })
	require.NoError(t, err)
	assert.Equal(t, 1, ra.count(commandCloseContent), "the game that finished loading is closed")
	assert.Empty(t, f.currentLaunch())
}

func TestStopFailsWhenLoadingLaunchNeverAnswers(t *testing.T) {
	t.Parallel()
	f, exec, clock := testFrontend(t, closedAddr(t))
	expectSystemctl(exec, errInactive, "is-active", "--quiet", transientUnit)
	exec.On("Run", mock.Anything, "systemctl", []string{"is-active", "--quiet", retroArchService}).Return(nil)
	f.launchedPath = testContent

	var err error
	drive(t, clock, pollInterval, func() { err = f.stop(context.Background()) })
	require.ErrorIs(t, err, platforms.ErrStopFailed)
	require.ErrorContains(t, err, "not answering")
	assert.Equal(t, testContent, f.currentLaunch())
}

func TestStopLoadingLaunchEndsWithContext(t *testing.T) {
	t.Parallel()
	f, exec, clock := testFrontend(t, closedAddr(t))
	expectSystemctl(exec, errInactive, "is-active", "--quiet", transientUnit)
	expectSystemctl(exec, nil, "is-active", "--quiet", retroArchService)
	f.launchedPath = testContent

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- f.stop(ctx) }()
	require.NoError(t, clock.BlockUntilContext(context.Background(), 1))
	cancel()
	err := <-done
	require.ErrorIs(t, err, platforms.ErrStopFailed)
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, testContent, f.currentLaunch())
}

func TestStopUnreachableRetroArch(t *testing.T) {
	t.Parallel()
	// Nothing of Core's is loading: there is nothing to wait for.
	f, exec, _ := testFrontend(t, closedAddr(t))
	expectSystemctl(exec, errInactive, "is-active", "--quiet", transientUnit)
	require.NoError(t, f.stop(context.Background()))

	// Core launched something but RetroArch is not running at all.
	expectSystemctl(exec, errInactive, "is-active", "--quiet", transientUnit)
	expectSystemctl(exec, errInactive, "is-active", "--quiet", retroArchService)
	f.launchedPath = testContent
	require.NoError(t, f.stop(context.Background()))
	assert.Empty(t, f.currentLaunch())
}

func TestStopWithNothingLoadedSendsNothing(t *testing.T) {
	t.Parallel()
	state := &retroArchState{status: contentless}
	ra := newFakeRetroArch(t, state.handle)
	f, exec, _ := testFrontend(t, ra.addr())
	expectSystemctl(exec, errInactive, "is-active", "--quiet", transientUnit)

	require.NoError(t, f.stop(context.Background()))
	assert.Zero(t, ra.count(commandCloseContent))
}

func TestStopEndsGameUnit(t *testing.T) {
	t.Parallel()
	f, exec, _ := testFrontend(t, closedAddr(t))
	expectSystemctl(exec, nil, "is-active", "--quiet", transientUnit)
	expectSystemctl(exec, nil, "stop", transientUnit)
	require.NoError(t, f.stop(context.Background()))

	expectSystemctl(exec, nil, "is-active", "--quiet", transientUnit)
	expectSystemctl(exec, errors.New("boom"), "stop", transientUnit)
	require.ErrorIs(t, f.stop(context.Background()), platforms.ErrStopFailed)
}

func TestGameRunning(t *testing.T) {
	t.Parallel()
	state := &retroArchState{status: playingGame}
	ra := newFakeRetroArch(t, state.handle)
	f, _, _ := testFrontend(t, ra.addr())
	running, err := f.gameRunning(context.Background())
	require.NoError(t, err)
	assert.True(t, running)

	unreachable, exec, _ := testFrontend(t, closedAddr(t))
	expectSystemctl(exec, nil, "is-active", "--quiet", transientUnit)
	running, err = unreachable.gameRunning(context.Background())
	require.NoError(t, err)
	assert.True(t, running)

	expectSystemctl(exec, errInactive, "is-active", "--quiet", transientUnit)
	_, err = unreachable.gameRunning(context.Background())
	require.Error(t, err)
}

func TestEnsureFrontend(t *testing.T) {
	t.Parallel()
	f, exec, _ := testFrontend(t, closedAddr(t))
	expectSystemctl(exec, errInactive, "is-active", "--quiet", transientUnit)
	expectSystemctl(exec, errInactive, "is-active", "--quiet", retroArchService)
	expectSystemctl(exec, nil, "--no-block", "start", retroArchService)
	f.ensureFrontend(context.Background())

	expectSystemctl(exec, errInactive, "is-active", "--quiet", transientUnit)
	expectSystemctl(exec, nil, "is-active", "--quiet", retroArchService)
	f.ensureFrontend(context.Background())
}

func TestStatusNamesContent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		status string
		path   string
		want   bool
	}{
		{name: "match", status: playingGame, path: testContent, want: true},
		{name: "case", status: "GET_STATUS PAUSED snes,super game (usa),crc32=0", path: testContent, want: true},
		{name: "other", status: playingGame, path: "/storage/roms/snes/Other.sfc"},
		{name: "contentless", status: contentless, path: testContent},
		{name: "empty path", status: playingGame},
		{
			name:   "comma in name",
			status: "GET_STATUS PLAYING snes,Game, The,crc32=1234",
			path:   "/storage/roms/snes/Game, The.sfc",
			want:   true,
		},
		{
			name:   "no crc",
			status: "GET_STATUS PLAYING snes,Game, The",
			path:   "/storage/roms/snes/Game, The.sfc",
			want:   true,
		},
		{
			name:   "comma prefix only",
			status: "GET_STATUS PLAYING snes,Game, The,crc32=1",
			path:   "/storage/roms/snes/Game.sfc",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, statusNamesContent(tt.status, tt.path))
		})
	}
}
