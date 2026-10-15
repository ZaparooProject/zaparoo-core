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

package batocera

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// kodiPollRig runs one poll of the tracker against a Kodi that reports no
// active players, as it does in the moment after a natural end.
type kodiPollRig struct {
	platform *Platform
	media    *models.ActiveMedia
	asked    []string
	cleared  int
}

func newKodiPollRig(t *testing.T, tracking bool) *kodiPollRig {
	t.Helper()
	srv := helpers.NewMockKodiServer(t)
	t.Cleanup(srv.Close)
	cfg, err := config.NewConfig(t.TempDir(), config.Values{
		Launchers: config.Launchers{
			Default: []config.LaunchersDefault{{Launcher: "Kodi", ServerURL: srv.URL()}},
		},
	})
	require.NoError(t, err)

	rig := &kodiPollRig{media: &models.ActiveMedia{
		SystemID: systemdefs.SystemVideo,
		Path:     "kodi-movie://7/Heat",
		Name:     "Heat",
	}}
	rig.platform = &Platform{cfg: cfg, kodiActive: true, lastKnownGame: rig.media}

	prev := kodiTracking
	kodiTracking = func(url string) bool {
		rig.asked = append(rig.asked, url)
		return tracking
	}
	t.Cleanup(func() { kodiTracking = prev })
	return rig
}

func (r *kodiPollRig) poll() {
	r.platform.checkAndUpdateRunningGame(func(m *models.ActiveMedia) {
		if m == nil {
			r.cleared++
		}
	})
}

// Not parallel: kodiTracking is a package variable.

func TestKodiPoll_DefersToActiveWatcher(t *testing.T) {
	r := newKodiPollRig(t, true)

	r.poll()

	assert.Zero(t, r.cleared, "an armed watcher reports the end itself")
	assert.Same(t, r.media, r.platform.lastKnownGame)
	require.Len(t, r.asked, 1)
	assert.Contains(t, r.asked[0], "/jsonrpc")
}

func TestKodiPoll_ClearsWhenNoWatcher(t *testing.T) {
	r := newKodiPollRig(t, false)

	r.poll()

	assert.Equal(t, 1, r.cleared)
	assert.Nil(t, r.platform.lastKnownGame)
}
