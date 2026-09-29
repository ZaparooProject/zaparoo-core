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
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
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

// Launchers runs in the background beside API startup, which writes the
// config testify would reflect over while matching; answer without the mock.
func (*embeddedTestPlatform) Launchers(*config.Instance) []platforms.Launcher {
	return nil
}

func embeddedFixture(t *testing.T, dir string) (*embeddedTestPlatform, *config.Instance, EmbeddedOptions) {
	t.Helper()
	restoreGlobalLogging(t)
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
		// Only the first start has migrations to apply; the second reuses the
		// upgraded databases, so it must not report a migration.
		want := []string{"starting", "migrating", "ready", "stopped"}
		if cycle > 0 {
			want = []string{"starting", "ready", "stopped"}
		}
		for _, want := range want {
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

// TestEmbeddedCancelDuringStartup cancels the host's context while the
// platform is still starting. Startup must not hang or leave a runtime behind:
// it either fails, or returns a runtime that stops on its own, and in both
// cases the platform it started is stopped and the listener is closed.
func TestEmbeddedCancelDuringStartup(t *testing.T) {
	previousCache := helpers.GlobalLauncherCache
	t.Cleanup(func() { helpers.GlobalLauncherCache = previousCache })
	helpers.GlobalLauncherCache = &helpers.LauncherCache{}
	platform, cfg, opts := embeddedFixture(t, t.TempDir())
	hostCtx, cancelHost := context.WithCancel(context.Background())
	defer cancelHost()
	opts.Context = hostCtx
	platform.On("StartPre", mock.Anything).Run(func(mock.Arguments) { cancelHost() }).Return(nil)
	stopped := make(chan struct{})
	platform.On("Stop").Run(func(mock.Arguments) { close(stopped) }).Return(nil).Once()

	started := make(chan struct{})
	var result *StartResult
	var err error
	go func() {
		defer close(started)
		result, err = StartEmbedded(platform, cfg, opts)
	}()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("startup hung after the host cancelled its context")
	}
	if err == nil {
		select {
		case <-result.Done:
		case <-time.After(10 * time.Second):
			t.Fatal("a runtime started under a cancelled context never stopped")
		}
	}
	select {
	case <-stopped:
	case <-time.After(10 * time.Second):
		t.Fatal("the started platform was not stopped")
	}
	_, acceptErr := opts.Listener.Accept()
	assert.ErrorIs(t, acceptErr, net.ErrClosed)
}

// TestEmbeddedHostKeysGuardTheListener checks that StartEmbedded hands the
// host's key provider to the supplied listener: only its key is accepted.
func TestEmbeddedHostKeysGuardTheListener(t *testing.T) {
	previousCache := helpers.GlobalLauncherCache
	t.Cleanup(func() { helpers.GlobalLauncherCache = previousCache })
	helpers.GlobalLauncherCache = &helpers.LauncherCache{}
	platform, cfg, opts := embeddedFixture(t, t.TempDir())
	platform.On("StartPre", mock.Anything).Return(nil)
	platform.On("Stop").Return(nil)
	opts.APIKeys = func() []string { return []string{"host-key"} }
	socket := opts.Listener.Addr().String()
	result, err := StartEmbedded(platform, cfg, opts)
	require.NoError(t, err)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, result.StopContext(ctx))
	}()

	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	for key, want := range map[string]int{
		"":         http.StatusUnauthorized,
		"wrong":    http.StatusUnauthorized,
		"host-key": http.StatusOK,
	} {
		request, reqErr := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://core.invalid/api/v0.1",
			strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"version"}`))
		require.NoError(t, reqErr)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+key)
		response, doErr := client.Do(request) //nolint:gosec // DialContext confines requests to the test Unix socket.
		require.NoError(t, doErr)
		assert.Equal(t, want, response.StatusCode, "key %q", key)
		require.NoError(t, response.Body.Close())
	}
}

type testRenderer struct{}

func (testRenderer) PresentUI(context.Context, *models.UIEvent) (func() error, error) {
	return func() error { return nil }, nil
}

type rendererPlatform struct {
	testRenderer
	*mocks.MockPlatform
}

// TestEmbeddedUsesOnlyTheHostRenderer: the host owns the screen, so an
// embedded start never falls back to a renderer the platform implements.
func TestEmbeddedUsesOnlyTheHostRenderer(t *testing.T) {
	platform := &rendererPlatform{MockPlatform: mocks.NewMockPlatform()}
	host := testRenderer{}
	assert.Equal(t, host, uiRendererFor(platform, &EmbeddedOptions{Renderer: host}))
	assert.Nil(t, uiRendererFor(platform, &EmbeddedOptions{}))
	assert.NotNil(t, uiRendererFor(platform, nil), "standalone keeps the platform's renderer")
	assert.Nil(t, uiRendererFor(mocks.NewMockPlatform(), nil))
}

// restoreGlobalLogging puts back the level an embedded start sets and closes
// its log file before the test directory goes; closing sends later lines to
// stderr.
func restoreGlobalLogging(t *testing.T) {
	t.Helper()
	previousLevel := zerolog.GlobalLevel()
	t.Cleanup(func() {
		zerolog.SetGlobalLevel(previousLevel)
		_ = helpers.CloseLogging()
	})
}

func TestSetupEmbeddedLogging(t *testing.T) {
	restoreGlobalLogging(t)
	for _, debug := range []bool{false, true} {
		dir := t.TempDir()
		platform := mocks.NewMockPlatform()
		platform.On("Settings").Return(platforms.Settings{LogDir: dir, HostManagedPaths: true})
		cfg, err := testhelpers.NewTestConfig(testhelpers.NewMemoryFS(), dir)
		require.NoError(t, err)
		cfg.SetDebugLogging(debug)
		zerolog.SetGlobalLevel(zerolog.TraceLevel)

		require.NoError(t, setupEmbeddedLogging(platform, cfg))
		want := zerolog.InfoLevel
		if debug {
			want = zerolog.DebugLevel
		}
		if os.Getenv("ZAPAROO_TRACE") == "" {
			assert.Equal(t, want, zerolog.GlobalLevel(), "debug_logging=%t", debug)
		}
		log.Info().Msg("embedded logging reaches the host log directory")
		data, err := os.ReadFile(filepath.Join(dir, config.LogFile)) //nolint:gosec // test temp dir
		require.NoError(t, err)
		assert.Contains(t, string(data), "embedded logging reaches the host log directory")

		// A runtime settings change still moves the level.
		cfg.SetDebugLogging(!debug)
		if os.Getenv("ZAPAROO_TRACE") == "" {
			assert.NotEqual(t, want, zerolog.GlobalLevel())
		}
		require.NoError(t, helpers.CloseLogging())
	}
}

// blockingLaunchersPlatform holds launcher probing until released, standing in
// for a host whose launcher discovery is slow.
type blockingLaunchersPlatform struct {
	*embeddedTestPlatform
	entered chan struct{}
	release chan struct{}
}

func (p *blockingLaunchersPlatform) Launchers(cfg *config.Instance) []platforms.Launcher {
	select {
	case p.entered <- struct{}{}:
	default:
	}
	<-p.release
	return p.embeddedTestPlatform.Launchers(cfg)
}

// Launcher probing must not hold up the API or the ready phase, and cache
// readers wait for it instead of seeing no launchers.
func TestEmbeddedStartsBeforeLauncherProbingFinishes(t *testing.T) {
	previousCache := helpers.GlobalLauncherCache
	t.Cleanup(func() { helpers.GlobalLauncherCache = previousCache })
	cache := &helpers.LauncherCache{}
	helpers.GlobalLauncherCache = cache
	base, cfg, opts := embeddedFixture(t, t.TempDir())
	base.On("StartPre", mock.Anything).Return(nil)
	base.On("Stop").Return(nil)
	platform := &blockingLaunchersPlatform{
		embeddedTestPlatform: base,
		entered:              make(chan struct{}, 1),
		release:              make(chan struct{}),
	}
	released := false
	releaseProbe := func() {
		if !released {
			released = true
			close(platform.release)
		}
	}
	t.Cleanup(releaseProbe)

	started := make(chan *StartResult, 1)
	go func() {
		result, err := StartEmbedded(platform, cfg, opts)
		assert.NoError(t, err)
		started <- result
	}()
	var result *StartResult
	select {
	case result = <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("startup waited for launcher probing")
	}
	require.NotNil(t, result)
	select {
	case <-platform.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("launcher probing never started")
	}

	conn, err := (&net.Dialer{}).DialContext(t.Context(), "unix", opts.Listener.Addr().String())
	require.NoError(t, err, "the API accepts connections while launchers are still probed")
	require.NoError(t, conn.Close())

	readerDone := make(chan int, 1)
	go func() { readerDone <- len(cache.GetAllLaunchers()) }()
	select {
	case <-readerDone:
		t.Fatal("a cache reader returned before probing finished")
	case <-time.After(50 * time.Millisecond):
	}
	releaseProbe()
	select {
	case <-readerDone:
	case <-time.After(10 * time.Second):
		t.Fatal("cache reader never resumed")
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, result.StopContext(stopCtx))
}

func TestBackgroundThrottled(t *testing.T) {
	t.Parallel()
	assert.False(t, backgroundThrottled(platforms.Settings{}))
	assert.True(t, backgroundThrottled(platforms.Settings{ResourceConstrained: true}))
	assert.True(t, backgroundThrottled(platforms.Settings{ThrottleBackground: true}),
		"a host can ask for the throttle baseline without being resource constrained")
}
