//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package gamescope

import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/command"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testExecutor struct {
	runs atomic.Int32
}

func (e *testExecutor) Run(context.Context, string, ...string) error {
	e.runs.Add(1)
	return nil
}

func (*testExecutor) Output(context.Context, string, ...string) ([]byte, error) { return nil, nil }
func (*testExecutor) Start(context.Context, string, ...string) error            { return nil }
func (*testExecutor) StartWithOptions(context.Context, command.StartOptions, string, ...string) error {
	return nil
}

func TestManagerOptIn(t *testing.T) {
	t.Parallel()

	assert.False(t, NewManager(SessionOptions{}).Enabled())
	assert.True(t, NewManager(SessionOptions{Enabled: true}).Enabled())
	assert.False(t, (*Manager)(nil).Enabled())
}

func TestManagerWrapLaunchers(t *testing.T) {
	t.Parallel()

	launched := false
	launchErr := errors.New("launch stopped for test")
	launch := func(*config.Instance, string, *platforms.LaunchOptions) (*os.Process, error) {
		launched = true
		return nil, launchErr
	}
	launchers := []platforms.Launcher{
		{ID: "Generic", Launch: launch},
		{ID: "Steam", Launch: launch},
	}

	NewManager(SessionOptions{Enabled: true}).WrapLaunchers(launchers)

	_, err := launchers[0].Launch(nil, "game", nil)
	require.ErrorIs(t, err, launchErr)
	assert.True(t, launched)
	assert.NotNil(t, launchers[0].Kill)
	assert.Nil(t, launchers[1].Kill)
}

func TestManagersKeepIndependentFocusState(t *testing.T) {
	t.Parallel()

	firstExecutor := &testExecutor{}
	secondExecutor := &testExecutor{}
	first := newManagerWithExecutor(SessionOptions{Enabled: true}, firstExecutor)
	second := newManagerWithExecutor(SessionOptions{Enabled: true}, secondExecutor)
	first.activeFocusManager = &FocusManager{
		executor: firstExecutor, display: ":0", originalLayer: "10",
	}
	second.activeFocusManager = &FocusManager{
		executor: secondExecutor, display: ":1", originalLayer: "20",
	}

	first.RevertFocus()

	assert.Equal(t, int32(1), firstExecutor.runs.Load())
	assert.Zero(t, secondExecutor.runs.Load())
	assert.Nil(t, first.activeFocusManager)
	assert.NotNil(t, second.activeFocusManager)
}

func TestManagerFocusStateConcurrentAccess(t *testing.T) {
	t.Parallel()

	manager := newManagerWithExecutor(SessionOptions{Enabled: true}, &testExecutor{})
	var workers sync.WaitGroup
	for range 20 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for range 50 {
				_, cancel, id := manager.beginFocusAttempt()
				cancel()
				manager.endFocusAttempt(id)
				manager.RevertFocus()
			}
		}()
	}
	workers.Wait()
	manager.RevertFocus()
}

func TestDisabledManagerDoesNotWrapLauncher(t *testing.T) {
	t.Parallel()

	launcher := platforms.Launcher{
		ID: "Generic",
		Launch: func(*config.Instance, string, *platforms.LaunchOptions) (*os.Process, error) {
			return os.FindProcess(os.Getpid())
		},
	}

	NewManager(SessionOptions{}).WrapLauncher(&launcher)

	assert.Nil(t, launcher.Kill)
}

func TestParseWindowCandidates(t *testing.T) {
	t.Parallel()

	output := `
     0x1234 "tiny": ("tiny") 80x80+0+0
     0x2345 "Steam": ("steam" "Steam") 1280x720+0+0
     0x5678 "Game": ("game" "Game") 1280x720+0+0
`
	assert.Equal(t, []windowCandidate{{ID: "0x5678"}}, parseWindowCandidates(output))
}

func TestParseWindowPIDOutput(t *testing.T) {
	t.Parallel()

	pid, ok := ParseWindowPIDOutput("_NET_WM_PID(CARDINAL) = 1234")
	assert.True(t, ok)
	assert.Equal(t, 1234, pid)

	_, ok = ParseWindowPIDOutput("_NET_WM_PID: not found")
	assert.False(t, ok)
}

func TestBaselayerValues(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "769, 0", ParseBaselayerOutput("GAMESCOPECTRL_BASELAYER_APPID(CARDINAL) = 769, 0"))
	assert.Empty(t, ParseBaselayerOutput("not found"))
	assert.Equal(t, "1, 769, 0", BuildBaselayerValue("1", "769, 0"))
	assert.Equal(t, "1", BuildBaselayerValue("1", ""))
}
