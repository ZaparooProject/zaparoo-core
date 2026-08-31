//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package steamtracker

import (
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/stretchr/testify/assert"
)

func TestPlatformIntegrationIgnoresExactRuntimeExecutable(t *testing.T) {
	t.Parallel()

	var active *models.ActiveMedia
	integration := &PlatformIntegration{
		activeMedia:    func() *models.ActiveMedia { return active },
		setActiveMedia: func(media *models.ActiveMedia) { active = media },
		activeGames:    make(map[int]int),
		ignoredPaths:   make(map[string]struct{}),
	}
	runtimePath := filepath.Join(t.TempDir(), "zaparoo-steam-runtime")
	integration.IgnoreExecutable(runtimePath)

	integration.onGameStart(42, 100, `"`+runtimePath+`"`)

	assert.Nil(t, active)
	assert.Empty(t, integration.activeGames)
}

func TestPlatformIntegrationDoesNotIgnoreSimilarRuntimeName(t *testing.T) {
	t.Parallel()

	var active *models.ActiveMedia
	integration := &PlatformIntegration{
		activeMedia:    func() *models.ActiveMedia { return active },
		setActiveMedia: func(media *models.ActiveMedia) { active = media },
		activeGames:    make(map[int]int),
		ignoredPaths:   make(map[string]struct{}),
	}
	dir := t.TempDir()
	integration.IgnoreExecutable(filepath.Join(dir, "zaparoo-steam-runtime"))

	integration.onGameStart(42, 100, filepath.Join(dir, "zaparoo-steam-runtime-copy"))

	assert.NotNil(t, active)
	assert.Equal(t, "steam://42", active.Path)
	assert.Equal(t, 100, integration.activeGames[42])
}
