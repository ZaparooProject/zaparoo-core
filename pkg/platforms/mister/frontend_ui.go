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

package mister

import (
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strconv"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const (
	// frontendStatusPath is where Main_MiSTer records whether it runs the
	// Zaparoo frontend in place of the stock menu.
	frontendStatusPath = "/tmp/zaparoo/frontend_status.json"
	// frontendStatusMaxBytes bounds the status file read; it holds one flag.
	frontendStatusMaxBytes = 4096
	// frontendColdStartHold is how long an event stays open after Core
	// leaves running media for it, so the frontend has time to start and
	// draw it.
	frontendColdStartHold = 60 * time.Second
)

type frontendStatus struct {
	Active bool `json:"active"`
	PID    int  `json:"pid"`
}

// frontendActive reports whether Main_MiSTer runs the Zaparoo frontend. A
// missing, oversized or unreadable status file means it does not, which
// keeps the stock widget behavior. The file names the Main process that
// wrote it, like /tmp/zaparoo_main: Main starts a new process on every core
// load and the fork rewrites the file from it, so a dead pid means a stock
// Main took over and the file is stale.
func frontendActive(fs afero.Fs) bool {
	file, err := fs.Open(frontendStatusPath)
	if err != nil {
		return false
	}
	defer func() { _ = file.Close() }()
	var status frontendStatus
	decoder := json.NewDecoder(io.LimitReader(file, frontendStatusMaxBytes))
	if err := decoder.Decode(&status); err != nil {
		log.Debug().Err(err).Msg("frontend status file is not readable")
		return false
	}
	if !status.Active || status.PID <= 0 {
		return false
	}
	if _, err := fs.Stat(filepath.Join(string(filepath.Separator), "proc", strconv.Itoa(status.PID))); err != nil {
		return false
	}
	return true
}

// HandOffUI leaves UI events to API clients while the frontend runs, so it
// draws them instead of the menu widgets. The widgets end running media to
// show any event that reaches them, so an event raised while media runs
// returns to the menu, which makes Main start the frontend, and is kept open
// long enough for the frontend to start. Events the widgets never show over
// media (the auto-advance switch countdown, the launch guard) are decided
// before they reach the renderer and are unaffected.
func (p *Platform) HandOffUI(_ context.Context, event *models.UIEvent) (handled bool, minOpen time.Duration) {
	if !frontendActive(p.filesystem()) {
		return false, 0
	}
	if p.activeMedia == nil || p.activeMedia() == nil {
		// Nothing running means the menu is up, but Main can report the
		// frontend active before it has started (just enabled, or Main
		// restarted without a menu load). Only a running frontend can draw
		// the event; otherwise the widget shows it, as before.
		if !p.frontendRunning() {
			log.Debug().Str("event_id", event.ID).
				Msg("frontend active but not running; leaving the UI event to the widget")
			return false, 0
		}
		return true, 0
	}
	log.Info().Str("event_id", event.ID).Str("kind", string(event.Kind)).
		Msg("returning to the menu so the frontend can show a UI event")
	returnToMenu := p.uiReturnToMenu
	if returnToMenu == nil {
		returnToMenu = func() error { return p.StopActiveLauncher(platforms.StopForMenu) }
	}
	if err := returnToMenu(); err != nil {
		log.Warn().Err(err).Msg("failed to return to the menu for a UI event")
	}
	return true, frontendColdStartHold
}

// frontendRunning reports whether the frontend holds its activity lease,
// which it does from start to exit.
func (p *Platform) frontendRunning() bool {
	if p.uiFrontendRunning != nil {
		return p.uiFrontendRunning()
	}
	running, err := frontendResourceLeaseActive()
	if err != nil {
		log.Debug().Err(err).Msg("could not probe the frontend lease")
		return false
	}
	return running
}
