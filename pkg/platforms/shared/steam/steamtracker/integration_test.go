//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package steamtracker

import (
	"errors"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxbase"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlatformIntegrationOnGameStopPreservesOtherActiveMedia(t *testing.T) {
	t.Parallel()

	active := &models.ActiveMedia{Path: "steam://123"}
	expectedActive := active
	cleared := false
	integration := &PlatformIntegration{
		activeMedia: func() *models.ActiveMedia { return active },
		setActiveMedia: func(media *models.ActiveMedia) {
			cleared = media == nil
			active = media
		},
		activeGames: map[int]int{456: 10},
	}

	integration.onGameStop(456, 10)

	assert.False(t, cleared)
	assert.Same(t, expectedActive, active)
	assert.Empty(t, integration.activeGames)
}

func TestPlatformIntegrationOnGameStopClearsOwnedActiveMedia(t *testing.T) {
	t.Parallel()

	active := &models.ActiveMedia{Path: "steam://123"}
	integration := &PlatformIntegration{
		activeMedia: func() *models.ActiveMedia { return active },
		setActiveMedia: func(media *models.ActiveMedia) {
			active = media
		},
		activeGames: map[int]int{123: 10},
	}

	integration.onGameStop(123, 10)

	assert.Nil(t, active)
	assert.Empty(t, integration.activeGames)
}

func TestPlatformIntegrationOnGameStopIgnoresStalePID(t *testing.T) {
	t.Parallel()

	active := &models.ActiveMedia{Path: "steam://123"}
	integration := &PlatformIntegration{
		activeMedia:    func() *models.ActiveMedia { return active },
		setActiveMedia: func(media *models.ActiveMedia) { active = media },
		activeGames:    map[int]int{123: 20},
	}

	integration.onGameStop(123, 10)

	assert.Equal(t, 20, integration.activeGames[123])
	assert.NotNil(t, active)
}

func TestPlatformIntegrationPublishActiveMediaIfActive(t *testing.T) {
	t.Parallel()

	var active *models.ActiveMedia
	integration := &PlatformIntegration{
		setActiveMedia: func(media *models.ActiveMedia) { active = media },
		activeGames:    map[int]int{123: 456},
	}
	expected := &models.ActiveMedia{Path: "steam://123"}

	assert.False(t, integration.publishActiveMediaIfActive(123, 457, expected))
	assert.Nil(t, active)
	assert.True(t, integration.publishActiveMediaIfActive(123, 456, expected))
	assert.Same(t, expected, active)
}

func TestPlatformIntegrationGameIsActive(t *testing.T) {
	t.Parallel()

	integration := &PlatformIntegration{
		activeGames: map[int]int{123: 456},
	}

	assert.True(t, integration.gameIsActive(123, 456))
	assert.False(t, integration.gameIsActive(123, 457))
	assert.False(t, integration.gameIsActive(456, 123))
}

func TestPlatformIntegrationTracksSteamReaperForStop(t *testing.T) {
	cmd := exec.CommandContext(t.Context(), "sleep", "30")
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	base := linuxbase.NewBase(platformids.SteamOS)
	integration := &PlatformIntegration{
		base:        base,
		activeMedia: func() *models.ActiveMedia { return &models.ActiveMedia{Path: "steam://123"} },
		activeGames: make(map[int]int),
	}

	integration.onGameStart(123, cmd.Process.Pid, "/game")
	require.NoError(t, base.StopActiveLauncher(platforms.StopForPreemption))

	require.Eventually(t, func() bool {
		return errors.Is(syscall.Kill(cmd.Process.Pid, 0), syscall.ESRCH)
	}, time.Second, 10*time.Millisecond)
}

func TestPlatformIntegrationForgetsReaperAfterNormalExit(t *testing.T) {
	cmd := exec.CommandContext(t.Context(), "sleep", "30")
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	base := linuxbase.NewBase(platformids.SteamOS)
	integration := &PlatformIntegration{
		base:           base,
		activeMedia:    func() *models.ActiveMedia { return &models.ActiveMedia{Path: "steam://123"} },
		setActiveMedia: func(*models.ActiveMedia) {},
		activeGames:    make(map[int]int),
	}

	integration.onGameStart(123, cmd.Process.Pid, "/game")
	integration.onGameStop(123, cmd.Process.Pid)
	require.NoError(t, base.StopActiveLauncher(platforms.StopForPreemption))

	assert.NoError(t, syscall.Kill(cmd.Process.Pid, 0))
}
