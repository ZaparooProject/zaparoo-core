//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package steamtracker

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/stretchr/testify/assert"
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

	integration.onGameStop(456)

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

	integration.onGameStop(123)

	assert.Nil(t, active)
	assert.Empty(t, integration.activeGames)
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

func TestPlatformIntegrationCanOwnActiveProcess(t *testing.T) {
	t.Parallel()

	current := &models.ActiveMedia{Path: "steam://123"}
	integration := &PlatformIntegration{
		activeMedia: func() *models.ActiveMedia { return current },
	}

	assert.True(t, integration.canOwnActiveProcess(123))
	assert.False(t, integration.canOwnActiveProcess(456))

	current.Path = "/home/user/roms/game.sfc"
	assert.False(t, integration.canOwnActiveProcess(123))

	current = nil
	assert.True(t, integration.canOwnActiveProcess(123))
}
