// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

package kodi

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/pkg/database/systemdefs"
	"github.com/stretchr/testify/assert"
)

// TestNewKodiLocalLauncher tests the creation of standard KodiLocal launcher
func TestNewKodiLocalLauncher(t *testing.T) {
	t.Parallel()

	launcher := NewKodiLocalLauncher()

	assert.Equal(t, "KodiLocal", launcher.ID)
	assert.Equal(t, systemdefs.SystemVideo, launcher.SystemID)
	assert.Equal(t, []string{"videos", "tvshows"}, launcher.Folders)

	// Test all required extensions from LibreELEC
	expectedExtensions := []string{
		".avi", ".mp4", ".mkv", ".iso", ".bdmv", ".ifo", ".mpeg", ".mpg",
		".mov", ".wmv", ".flv", ".webm", ".m4v", ".3gp", ".ts", ".m2ts", ".mts",
	}
	assert.Equal(t, expectedExtensions, launcher.Extensions)
	assert.NotNil(t, launcher.Launch, "Launch function should be set")
}

// TestNewKodiMovieLauncher tests the creation of standard KodiMovie launcher
func TestNewKodiMovieLauncher(t *testing.T) {
	t.Parallel()

	launcher := NewKodiMovieLauncher()

	assert.Equal(t, "KodiMovie", launcher.ID)
	assert.Equal(t, systemdefs.SystemMovie, launcher.SystemID)
	assert.Equal(t, []string{SchemeKodiMovie}, launcher.Schemes)
	assert.NotNil(t, launcher.Launch, "Launch function should be set")
	assert.NotNil(t, launcher.Scanner, "Scanner function should be set")
}

// TestNewKodiTVLauncher tests the creation of standard KodiTV launcher
func TestNewKodiTVLauncher(t *testing.T) {
	t.Parallel()

	launcher := NewKodiTVLauncher()

	assert.Equal(t, "KodiTV", launcher.ID)
	assert.Equal(t, systemdefs.SystemTV, launcher.SystemID)
	assert.Equal(t, []string{SchemeKodiEpisode}, launcher.Schemes)
	assert.NotNil(t, launcher.Launch, "Launch function should be set")
	assert.NotNil(t, launcher.Scanner, "Scanner function should be set")
}
