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
	"fmt"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	statepkg "github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type readinessPlatform struct {
	*mocks.MockPlatform
	platformWait func(context.Context, *config.Instance, *models.ActiveMedia) error
	launchers    []platforms.Launcher
}

func (p *readinessPlatform) Launchers(*config.Instance) []platforms.Launcher {
	return p.launchers
}

func (p *readinessPlatform) WaitForMediaReady(
	ctx context.Context,
	cfg *config.Instance,
	media *models.ActiveMedia,
) error {
	return p.platformWait(ctx, cfg, media)
}

func withTestMediaReadyTimeout(t *testing.T, timeout time.Duration) {
	t.Helper()

	oldTimeout := mediaReadyTimeout
	mediaReadyTimeout = timeout
	t.Cleanup(func() { mediaReadyTimeout = oldTimeout })
}

func newReadinessService(pl *readinessPlatform) *ServiceContext {
	st, _ := statepkg.NewState(pl, "test-boot")
	return &ServiceContext{
		Platform: pl,
		Config:   &config.Instance{},
		State:    st,
	}
}

func TestMediaReadyFuncUsesMatchingLauncherWaitForReady(t *testing.T) {
	launcherCalled := make(chan struct{}, 1)
	platformCalled := make(chan struct{}, 1)
	pl := &readinessPlatform{
		MockPlatform: mocks.NewMockPlatform(),
		launchers: []platforms.Launcher{
			{
				ID: "retroarch",
				WaitForReady: func(context.Context, *config.Instance, *models.ActiveMedia) error {
					launcherCalled <- struct{}{}
					return nil
				},
			},
		},
		platformWait: func(context.Context, *config.Instance, *models.ActiveMedia) error {
			platformCalled <- struct{}{}
			return nil
		},
	}
	svc := newReadinessService(pl)
	media := models.NewActiveMedia("nes", "NES", "game.nes", "Game", "retroarch")

	readyFunc := mediaReadyFunc(svc, media)
	require.NotNil(t, readyFunc)
	require.NoError(t, readyFunc(context.Background(), svc.Config, media))

	select {
	case <-launcherCalled:
	case <-time.After(time.Second):
		t.Fatal("launcher WaitForReady was not called")
	}
	select {
	case <-platformCalled:
		t.Fatal("platform WaitForMediaReady was called")
	default:
	}
}

func TestMediaReadyFuncFallsBackToPlatformWaitForMediaReady(t *testing.T) {
	platformCalled := make(chan struct{}, 1)
	pl := &readinessPlatform{
		MockPlatform: mocks.NewMockPlatform(),
		launchers: []platforms.Launcher{
			{
				ID: "retroarch",
				WaitForReady: func(context.Context, *config.Instance, *models.ActiveMedia) error {
					t.Fatal("launcher WaitForReady was called")
					return nil
				},
			},
		},
		platformWait: func(context.Context, *config.Instance, *models.ActiveMedia) error {
			platformCalled <- struct{}{}
			return nil
		},
	}
	svc := newReadinessService(pl)
	media := models.NewActiveMedia("nes", "NES", "game.nes", "Game", "different")

	readyFunc := mediaReadyFunc(svc, media)
	require.NotNil(t, readyFunc)
	require.NoError(t, readyFunc(context.Background(), svc.Config, media))

	select {
	case <-platformCalled:
	case <-time.After(time.Second):
		t.Fatal("platform WaitForMediaReady was not called")
	}
}

func TestStartMediaReadyProbeMarksReadyAfterWaitCompletes(t *testing.T) {
	withTestMediaReadyTimeout(t, 50*time.Millisecond)

	tests := map[string]struct {
		waitForReady func(context.Context, *config.Instance, *models.ActiveMedia) error
	}{
		"success": {
			waitForReady: func(context.Context, *config.Instance, *models.ActiveMedia) error {
				return nil
			},
		},
		"error": {
			waitForReady: func(context.Context, *config.Instance, *models.ActiveMedia) error {
				return errors.New("failed")
			},
		},
		"timeout": {
			waitForReady: func(ctx context.Context, _ *config.Instance, _ *models.ActiveMedia) error {
				<-ctx.Done()
				return ctx.Err()
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			called := make(chan struct{}, 1)
			pl := &readinessPlatform{
				MockPlatform: mocks.NewMockPlatform(),
				launchers: []platforms.Launcher{
					{
						ID: "retroarch",
						WaitForReady: func(ctx context.Context, cfg *config.Instance, media *models.ActiveMedia) error {
							called <- struct{}{}
							return tt.waitForReady(ctx, cfg, media)
						},
					},
				},
			}
			svc := newReadinessService(pl)
			media := models.NewActiveMedia("nes", "NES", "game.nes", "Game", "retroarch")
			svc.State.SetActiveMedia(media)
			gen, ok := svc.State.ActiveMediaReadyGeneration()
			require.True(t, ok)

			startMediaReadyProbe(svc, media, gen)

			select {
			case <-called:
			case <-time.After(time.Second):
				t.Fatal("WaitForReady was not called")
			}

			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			require.NoError(t, svc.State.WaitForActiveMediaReady(ctx, gen))
			assert.True(t, svc.State.ActiveMediaReady())
		})
	}
}

func TestWaitForMediaReadyErrorBehavior(t *testing.T) {
	withTestMediaReadyTimeout(t, 20*time.Millisecond)

	t.Run("deadline exceeded continues", func(t *testing.T) {
		svc := newReadinessService(&readinessPlatform{MockPlatform: mocks.NewMockPlatform()})
		media := models.NewActiveMedia("nes", "NES", "game.nes", "Game", "retroarch")
		svc.State.SetActiveMedia(media)
		gen, ok := svc.State.ActiveMediaReadyGeneration()
		require.True(t, ok)

		require.NoError(t, waitForMediaReady(context.Background(), svc, gen, false))
	})

	t.Run("no active media returns wrapped error", func(t *testing.T) {
		svc := newReadinessService(&readinessPlatform{MockPlatform: mocks.NewMockPlatform()})

		err := waitForMediaReady(context.Background(), svc, 1, false)
		require.Error(t, err)
		require.ErrorIs(t, err, statepkg.ErrNoActiveMedia)
	})

	t.Run("active media changed returns wrapped error", func(t *testing.T) {
		svc := newReadinessService(&readinessPlatform{MockPlatform: mocks.NewMockPlatform()})
		svc.State.SetActiveMedia(models.NewActiveMedia("nes", "NES", "game-a.nes", "Game A", "retroarch"))
		gen, ok := svc.State.ActiveMediaReadyGeneration()
		require.True(t, ok)
		svc.State.SetActiveMedia(models.NewActiveMedia("snes", "SNES", "game-b.sfc", "Game B", "retroarch"))

		err := waitForMediaReady(context.Background(), svc, gen, false)
		require.Error(t, err)
		require.ErrorIs(t, err, statepkg.ErrActiveMediaChanged)
	})

	t.Run("ready returns nil", func(t *testing.T) {
		svc := newReadinessService(&readinessPlatform{MockPlatform: mocks.NewMockPlatform()})
		svc.State.SetActiveMedia(models.NewActiveMedia("nes", "NES", "game.nes", "Game", "retroarch"))
		gen, ok := svc.State.ActiveMediaReadyGeneration()
		require.True(t, ok)
		svc.State.MarkActiveMediaReady(gen)

		require.NoError(t, waitForMediaReady(context.Background(), svc, gen, false))
	})
}

// blockingProbe starts a probe whose launcher wait blocks until its context
// ends and reports that error on the returned channel.
func blockingProbe(t *testing.T) (svc *ServiceContext, gen uint64, waited <-chan error) {
	t.Helper()
	withTestMediaReadyTimeout(t, time.Minute)

	waitedCh := make(chan error, 1)
	started := make(chan struct{})
	pl := &readinessPlatform{
		MockPlatform: mocks.NewMockPlatform(),
		launchers: []platforms.Launcher{{
			ID: "retroarch",
			WaitForReady: func(ctx context.Context, _ *config.Instance, _ *models.ActiveMedia) error {
				close(started)
				<-ctx.Done()
				waitedCh <- ctx.Err()
				return ctx.Err()
			},
		}},
	}
	svc = newReadinessService(pl)
	media := models.NewActiveMedia("nes", "NES", "game.nes", "Game", "retroarch")
	svc.State.SetActiveMedia(media)
	gen, ok := svc.State.ActiveMediaReadyGeneration()
	require.True(t, ok)

	startMediaReadyProbe(svc, media, gen)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("WaitForReady was not called")
	}
	return svc, gen, waitedCh
}

func requireProbeCancelled(t *testing.T, waited <-chan error) {
	t.Helper()
	select {
	case err := <-waited:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("probe was not cancelled")
	}
}

func TestStartMediaReadyProbeCancelledWhenMediaReplaced(t *testing.T) {
	svc, _, waited := blockingProbe(t)

	svc.State.SetActiveMedia(models.NewActiveMedia("snes", "SNES", "other.sfc", "Other", "retroarch"))

	requireProbeCancelled(t, waited)
	assert.False(t, svc.State.ActiveMediaReady(), "the old probe must not mark the new media ready")
}

func TestStartMediaReadyProbeCancelledWhenMediaStops(t *testing.T) {
	svc, gen, waited := blockingProbe(t)

	svc.State.SetActiveMedia(nil)

	requireProbeCancelled(t, waited)
	_, ok := svc.State.ActiveMediaReadyGeneration()
	assert.False(t, ok)
	svc.State.MarkActiveMediaReady(gen)
	assert.False(t, svc.State.ActiveMediaReady())
}

func TestStartMediaReadyProbeCancelledWhenServiceStops(t *testing.T) {
	svc, _, waited := blockingProbe(t)

	svc.State.StopService()

	requireProbeCancelled(t, waited)
}

func TestStartMediaReadyProbeUnavailableIntegrationStillMarksReady(t *testing.T) {
	withTestMediaReadyTimeout(t, time.Minute)

	pl := &readinessPlatform{
		MockPlatform: mocks.NewMockPlatform(),
		launchers: []platforms.Launcher{{
			ID: "retroarch",
			WaitForReady: func(context.Context, *config.Instance, *models.ActiveMedia) error {
				return fmt.Errorf("status: %w", platforms.ErrReadinessUnavailable)
			},
		}},
	}
	svc := newReadinessService(pl)
	media := models.NewActiveMedia("nes", "NES", "game.nes", "Game", "retroarch")
	svc.State.SetActiveMedia(media)
	gen, ok := svc.State.ActiveMediaReadyGeneration()
	require.True(t, ok)

	startMediaReadyProbe(svc, media, gen)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, svc.State.WaitForActiveMediaReady(ctx, gen))
}

func TestWaitForMediaReady_AwaitsMediaTheLaunchHasNotPublishedYet(t *testing.T) {
	withTestMediaReadyTimeout(t, 2*time.Second)
	svc := newReadinessService(&readinessPlatform{MockPlatform: mocks.NewMockPlatform()})
	done := make(chan error, 1)
	go func() { done <- waitForMediaReady(context.Background(), svc, 0, true) }()

	// A tracker publishes the media some time after the launch returned.
	time.Sleep(3 * mediaAppearPoll)
	svc.State.SetActiveMedia(models.NewActiveMedia("pc", "PC", "game.exe", "Game", "Steam"))
	gen, ok := svc.State.ActiveMediaReadyGeneration()
	require.True(t, ok)
	select {
	case err := <-done:
		t.Fatalf("returned before the media was ready: %v", err)
	case <-time.After(150 * time.Millisecond):
	}

	svc.State.MarkActiveMediaReady(gen)

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("wait did not return once the media was ready")
	}
}

func TestWaitForMediaReady_NoMediaAppearingIsTheSameErrorAsBefore(t *testing.T) {
	withTestMediaReadyTimeout(t, 250*time.Millisecond)
	svc := newReadinessService(&readinessPlatform{MockPlatform: mocks.NewMockPlatform()})

	err := waitForMediaReady(context.Background(), svc, 0, true)

	require.ErrorIs(t, err, statepkg.ErrNoActiveMedia)
}

func TestWaitForMediaReady_NoMediaAndNoEarlierLaunchFailsAtOnce(t *testing.T) {
	withTestMediaReadyTimeout(t, time.Minute)
	svc := newReadinessService(&readinessPlatform{MockPlatform: mocks.NewMockPlatform()})
	start := time.Now()

	err := waitForMediaReady(context.Background(), svc, 0, false)

	require.ErrorIs(t, err, statepkg.ErrNoActiveMedia)
	assert.Less(t, time.Since(start), time.Second, "a script with no launch must not wait for media")
}

func TestWaitForMediaReady_AwaitingMediaEndsWithTheContext(t *testing.T) {
	withTestMediaReadyTimeout(t, time.Minute)
	svc := newReadinessService(&readinessPlatform{MockPlatform: mocks.NewMockPlatform()})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- waitForMediaReady(ctx, svc, 0, true) }()

	cancel()

	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("wait ignored the cancelled context")
	}
}

func readyTimeoutPlatform(readyTimeout time.Duration) *readinessPlatform {
	return &readinessPlatform{
		MockPlatform: mocks.NewMockPlatform(),
		launchers: []platforms.Launcher{{
			ID: "arcade",
			WaitForReady: func(ctx context.Context, _ *config.Instance, _ *models.ActiveMedia) error {
				<-ctx.Done()
				return ctx.Err()
			},
			ReadyTimeout: func(*models.ActiveMedia) time.Duration { return readyTimeout },
		}},
	}
}

func TestMediaReadyTimeoutFor(t *testing.T) {
	withTestMediaReadyTimeout(t, 20*time.Millisecond)
	media := models.NewActiveMedia("arcade", "Arcade", "game.mra", "Game", "arcade")

	tests := map[string]struct {
		svc   *ServiceContext
		media *models.ActiveMedia
		want  time.Duration
	}{
		"a longer launcher bound wins": {
			svc: newReadinessService(readyTimeoutPlatform(time.Hour)), media: media, want: time.Hour,
		},
		"a shorter launcher bound is ignored": {
			svc: newReadinessService(readyTimeoutPlatform(time.Millisecond)), media: media, want: 20 * time.Millisecond,
		},
		"no launcher bound uses the default": {
			svc: newReadinessService(&readinessPlatform{
				MockPlatform: mocks.NewMockPlatform(),
				launchers:    []platforms.Launcher{{ID: "arcade"}},
			}),
			media: media, want: 20 * time.Millisecond,
		},
		"an unknown launcher uses the default": {
			svc:   newReadinessService(readyTimeoutPlatform(time.Hour)),
			media: models.NewActiveMedia("arcade", "Arcade", "game.mra", "Game", "other"),
			want:  20 * time.Millisecond,
		},
		"no media uses the default": {
			svc: newReadinessService(readyTimeoutPlatform(time.Hour)), media: nil, want: 20 * time.Millisecond,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.want, mediaReadyTimeoutFor(tt.svc, tt.media))
		})
	}
}

func TestStartMediaReadyProbeUsesTheLauncherTimeout(t *testing.T) {
	withTestMediaReadyTimeout(t, 20*time.Millisecond)
	svc := newReadinessService(readyTimeoutPlatform(time.Hour))
	media := models.NewActiveMedia("arcade", "Arcade", "game.mra", "Game", "arcade")
	svc.State.SetActiveMedia(media)
	gen, ok := svc.State.ActiveMediaReadyGeneration()
	require.True(t, ok)

	startMediaReadyProbe(svc, media, gen)

	time.Sleep(150 * time.Millisecond)
	assert.False(t, svc.State.ActiveMediaReady(), "the probe was ended at the default bound")

	svc.State.SetActiveMedia(nil)
}

func TestWaitForMediaReadyUsesTheLauncherTimeout(t *testing.T) {
	withTestMediaReadyTimeout(t, 20*time.Millisecond)
	svc := newReadinessService(readyTimeoutPlatform(time.Hour))
	media := models.NewActiveMedia("arcade", "Arcade", "game.mra", "Game", "arcade")
	svc.State.SetActiveMedia(media)
	gen, ok := svc.State.ActiveMediaReadyGeneration()
	require.True(t, ok)
	done := make(chan error, 1)
	go func() { done <- waitForMediaReady(context.Background(), svc, gen, false) }()

	select {
	case err := <-done:
		t.Fatalf("gave up at the default bound: %v", err)
	case <-time.After(150 * time.Millisecond):
	}

	svc.State.MarkActiveMediaReady(gen)
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("wait did not return once the media was ready")
	}
}

type launcherResolverStub struct {
	launcher *platforms.Launcher
}

func (s launcherResolverStub) GetLauncherByID(string) *platforms.Launcher { return s.launcher }

func TestReadyLauncherPrefersTheLauncherCache(t *testing.T) {
	t.Parallel()

	cached := &platforms.Launcher{ID: "native"}
	svc := newReadinessService(&readinessPlatform{MockPlatform: mocks.NewMockPlatform()})
	svc.LauncherCache = launcherResolverStub{launcher: cached}
	media := models.NewActiveMedia("audio", "Audio", "song.mp3", "Song", "native")

	assert.Same(t, cached, readyLauncher(svc, media), "a launcher the platform cannot build is found")
}
