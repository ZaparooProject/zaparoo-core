//go:build !windows

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
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/discovery"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/idle"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type embeddedTestPlatform struct {
	*mocks.MockPlatform
}

// Testify formats live arguments while matching them, which races with scheduler
// and database workers. Reuse the platform mock without reflecting over those owners.
func (*embeddedTestPlatform) StartPost(
	context.Context, *config.Instance, platforms.LauncherContextManager,
	func() *models.ActiveMedia, func(*models.ActiveMedia), *database.Database, *idle.Scheduler,
) error {
	return nil
}

func embeddedFixture(t *testing.T, dir string) (*embeddedTestPlatform, *config.Instance, EmbeddedOptions) {
	t.Helper()
	platform := &embeddedTestPlatform{MockPlatform: mocks.NewMockPlatform()}
	platform.On("Settings").Return(platforms.Settings{
		DataDir: dir, ConfigDir: dir, TempDir: dir, LogDir: dir,
		HostManagedPaths: true, DisableSelfUpdate: true,
	})
	platform.On("RootDirs", mock.Anything).Return([]string{})
	platform.SetupBasicMock()
	cfg, err := testhelpers.NewTestConfigWithPort(testhelpers.NewMemoryFS(), dir, 0)
	require.NoError(t, err)
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", testhelpers.TempSocketPath(t, "s"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	player := mocks.NewMockPlayer()
	player.SetupNoOpMock()
	return platform, cfg, EmbeddedOptions{Context: t.Context(), Listener: listener, Audio: player}
}

func TestEmbeddedLifecycle(t *testing.T) {
	// The service owns a process-wide launcher cache; never run runtimes in parallel.
	previousCache := helpers.GlobalLauncherCache
	t.Cleanup(func() { helpers.GlobalLauncherCache = previousCache })
	helpers.GlobalLauncherCache = &helpers.LauncherCache{}
	dir := t.TempDir()
	for cycle := range 2 {
		platform, cfg, opts := embeddedFixture(t, dir)
		platform.On("StartPre", mock.Anything).Return(nil)
		platform.On("Stop").Return(nil)
		phases := make(chan string, 8)
		opts.OnPhase = func(phase string) { phases <- phase }
		result, err := StartEmbedded(platform, cfg, opts)
		require.NoError(t, err, "cycle %d", cycle)
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		require.NoError(t, result.StopContext(stopCtx))
		cancel()
		require.NoError(t, result.Err())
		for _, want := range []string{"starting", "migrating", "ready", "stopped"} {
			select {
			case got := <-phases:
				assert.Equal(t, want, got)
			case <-time.After(10 * time.Second):
				t.Fatalf("missing phase %s", want)
			}
		}
		platform.AssertCalled(t, "Stop")
	}
}

// TestEmbeddedPreStartFailure also pins the rule the standalone path states at
// its own StartPre failure: a StartPre that failed partway is not stopped,
// because its counterpart has nothing well-defined to undo. The embedded
// cleanup must not reach a platform that never finished starting.
func TestEmbeddedPreStartFailure(t *testing.T) {
	platform, cfg, opts := embeddedFixture(t, t.TempDir())
	startupErr := errors.New("host unavailable")
	platform.On("StartPre", mock.Anything).Return(startupErr)
	platform.On("Stop").Return(nil).Maybe()
	var fatal error
	opts.OnFatal = func(err error) { fatal = err }
	result, err := StartEmbedded(platform, cfg, opts)
	require.ErrorIs(t, err, startupErr)
	assert.Nil(t, result)
	require.ErrorIs(t, fatal, startupErr)
	platform.AssertNotCalled(t, "Stop")
	_, err = opts.Listener.Accept()
	assert.ErrorIs(t, err, net.ErrClosed)
}

func TestEmbeddedRejectsMissingResources(t *testing.T) {
	_, err := StartEmbedded(nil, nil, EmbeddedOptions{})
	require.ErrorContains(t, err, "requires platform")
}

func TestEmbeddedStopDeadline(t *testing.T) {
	previousCache := helpers.GlobalLauncherCache
	t.Cleanup(func() { helpers.GlobalLauncherCache = previousCache })
	helpers.GlobalLauncherCache = &helpers.LauncherCache{}
	platform, cfg, opts := embeddedFixture(t, t.TempDir())
	platform.On("StartPre", mock.Anything).Return(nil)
	releaseStop := make(chan struct{})
	platform.On("Stop").Run(func(mock.Arguments) { <-releaseStop }).Return(nil)
	result, err := StartEmbedded(platform, cfg, opts)
	require.NoError(t, err)
	// Always release native cleanup, including assertion failures.
	defer func() {
		close(releaseStop)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, result.StopContext(ctx))
	}()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, result.StopContext(ctx), context.Canceled)
	select {
	case <-result.Done:
		t.Fatal("deadline must not pretend that blocked cleanup completed")
	default:
	}
}

func TestEmbeddedNetwork(t *testing.T) {
	previousCache := helpers.GlobalLauncherCache
	t.Cleanup(func() { helpers.GlobalLauncherCache = previousCache })
	helpers.GlobalLauncherCache = &helpers.LauncherCache{}
	type advert struct {
		name string
		port int
	}
	for _, network := range []bool{true, false} {
		platform, cfg, opts := embeddedFixture(t, t.TempDir())
		platform.On("StartPre", mock.Anything).Return(nil)
		platform.On("Stop").Return(nil)
		adverts := make(chan advert, 4)
		opts.Network = network
		opts.OnNetwork = func(port int, instanceName string) { adverts <- advert{instanceName, port} }
		result, err := StartEmbedded(platform, cfg, opts)
		require.NoError(t, err)
		if network {
			select {
			case got := <-adverts:
				assert.NotZero(t, got.port)
				assert.Equal(t, cfg.APIPort(), got.port, "the advertised port is the one actually bound")
				assert.Equal(t, discovery.ResolveInstanceName(cfg), got.name)
				assert.NotEmpty(t, got.name)
				conn, dialErr := (&net.Dialer{Timeout: 2 * time.Second}).DialContext(
					t.Context(), "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(got.port)))
				require.NoError(t, dialErr, "the advertised port must accept connections")
				require.NoError(t, conn.Close())
			case <-time.After(10 * time.Second):
				t.Fatal("OnNetwork was never called")
			}
		}
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		require.NoError(t, result.StopContext(stopCtx))
		cancel()
		require.NoError(t, result.Err())
		select {
		case got := <-adverts:
			t.Fatalf("unexpected OnNetwork call (network=%t): %+v", network, got)
		default:
		}
	}
}
