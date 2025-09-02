//go:build linux

// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package libreelec

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms/shared/kodi"
	"github.com/ZaparooProject/zaparoo-core/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLibreELECHasKodiLaunchers tests that LibreELEC includes all Kodi launchers
func TestLibreELECHasKodiLaunchers(t *testing.T) {
	t.Parallel()

	fs := helpers.NewMemoryFS()
	cfg, err := helpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	platform := &Platform{}
	launchers := platform.Launchers(cfg)

	// Check for KodiLocal launcher
	var kodiLocal, kodiMovie, kodiTV *string
	for _, launcher := range launchers {
		switch launcher.ID {
		case "KodiLocal":
			kodiLocal = &launcher.ID
			assert.Equal(t, systemdefs.SystemVideo, launcher.SystemID)
		case "KodiMovie":
			kodiMovie = &launcher.ID
			assert.Equal(t, systemdefs.SystemMovie, launcher.SystemID)
			assert.Contains(t, launcher.Schemes, kodi.SchemeKodiMovie)
		case "KodiTV":
			kodiTV = &launcher.ID
			assert.Equal(t, systemdefs.SystemTV, launcher.SystemID)
			assert.Contains(t, launcher.Schemes, kodi.SchemeKodiEpisode)
		}
	}

	require.NotNil(t, kodiLocal, "KodiLocal launcher should exist")
	require.NotNil(t, kodiMovie, "KodiMovie launcher should exist")
	require.NotNil(t, kodiTV, "KodiTV launcher should exist")
}
