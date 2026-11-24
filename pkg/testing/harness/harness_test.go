//go:build linux && !android

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

package harness_test

import (
	"context"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/romm"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/fakes/fakeset"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/harness"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Without fakes, every outbound service points at an unreachable loopback
// address, so an instance can never reach the real ones.
func TestOfflineByDefault(t *testing.T) {
	in := harness.New(t, &harness.Options{Readers: []string{}})

	assert.Equal(t, fakeset.Unreachable, in.Config.OnlineBaseURL())
	assert.Equal(t, fakeset.Unreachable, in.Config.ScraperLibretroThumbnailsBaseURL())

	_, err := helpers.UploadLog(in.Platform)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "127.0.0.1:9")
}

// settings.update and settings.reload read config.toml back in, so the fake
// URLs must be saved there or a reload would point Core at the real services.
func TestFakeURLsSurviveSettingsReload(t *testing.T) {
	in := harness.New(t, &harness.Options{
		Readers: []string{},
		Fakes:   []string{fakeset.Online, fakeset.Libretro, fakeset.RomM},
	})
	ctx := context.Background()
	_, err := in.API.Call(ctx, "settings.update", map[string]any{"readersScanMode": "hold"})
	require.NoError(t, err)
	_, err = in.API.Call(ctx, "settings.reload", nil)
	require.NoError(t, err)

	assert.Equal(t, in.Fakes.URLs[fakeset.Online], in.Config.OnlineBaseURL())
	assert.Equal(t, in.Fakes.URLs[fakeset.Libretro], in.Config.ScraperLibretroThumbnailsBaseURL())
	defaults := in.Config.LookupLauncherDefaults(romm.LauncherID, nil)
	assert.Equal(t, in.Fakes.URLs[fakeset.RomM], defaults.ServerURL)
}
