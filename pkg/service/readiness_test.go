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

		require.NoError(t, waitForMediaReady(context.Background(), svc, gen))
	})

	t.Run("no active media returns wrapped error", func(t *testing.T) {
		svc := newReadinessService(&readinessPlatform{MockPlatform: mocks.NewMockPlatform()})

		err := waitForMediaReady(context.Background(), svc, 1)
		require.Error(t, err)
		require.ErrorIs(t, err, statepkg.ErrNoActiveMedia)
	})

	t.Run("active media changed returns wrapped error", func(t *testing.T) {
		svc := newReadinessService(&readinessPlatform{MockPlatform: mocks.NewMockPlatform()})
		svc.State.SetActiveMedia(models.NewActiveMedia("nes", "NES", "game-a.nes", "Game A", "retroarch"))
		gen, ok := svc.State.ActiveMediaReadyGeneration()
		require.True(t, ok)
		svc.State.SetActiveMedia(models.NewActiveMedia("snes", "SNES", "game-b.sfc", "Game B", "retroarch"))

		err := waitForMediaReady(context.Background(), svc, gen)
		require.Error(t, err)
		require.ErrorIs(t, err, statepkg.ErrActiveMediaChanged)
	})

	t.Run("ready returns nil", func(t *testing.T) {
		svc := newReadinessService(&readinessPlatform{MockPlatform: mocks.NewMockPlatform()})
		svc.State.SetActiveMedia(models.NewActiveMedia("nes", "NES", "game.nes", "Game", "retroarch"))
		gen, ok := svc.State.ActiveMediaReadyGeneration()
		require.True(t, ok)
		svc.State.MarkActiveMediaReady(gen)

		require.NoError(t, waitForMediaReady(context.Background(), svc, gen))
	})
}
