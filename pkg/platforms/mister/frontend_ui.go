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
	// frontendReloadWait is how long HandOffUI waits for Main to rewrite a
	// status file whose writer has gone. Main replaces its process on every
	// core load and names the new one within about two seconds; a game's
	// exit raises its countdown inside that gap.
	frontendReloadWait = 5 * time.Second
	// frontendReloadPoll is how often the status file is reread meanwhile.
	frontendReloadPoll = 100 * time.Millisecond
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
	return readFrontendStatus(fs) == statusLive
}

type statusState int

const (
	statusOff statusState = iota
	// statusOrphaned is a status file saying active whose writer has gone:
	// Main is between processes, or a stock Main replaced the fork.
	statusOrphaned
	statusLive
)

func readFrontendStatus(fs afero.Fs) statusState {
	file, err := fs.Open(frontendStatusPath)
	if err != nil {
		return statusOff
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, frontendStatusMaxBytes+1))
	if err != nil || len(data) > frontendStatusMaxBytes {
		log.Debug().Err(err).Msg("frontend status file is not readable")
		return statusOff
	}
	var status frontendStatus
	if err := json.Unmarshal(data, &status); err != nil {
		log.Debug().Err(err).Msg("frontend status file is not readable")
		return statusOff
	}
	if !status.Active || status.PID <= 0 {
		return statusOff
	}
	if _, err := fs.Stat(filepath.Join(string(filepath.Separator), "proc", strconv.Itoa(status.PID))); err != nil {
		return statusOrphaned
	}
	return statusLive
}

// frontendActiveSettled is frontendActive that waits out a core load: a
// status file whose writer has just gone is reread until Main names its new
// process, or until wait passes, when a stock Main is taken to have replaced
// the fork.
func frontendActiveSettled(ctx context.Context, fs afero.Fs, wait time.Duration) bool {
	deadline := time.Now().Add(wait)
	for {
		switch readFrontendStatus(fs) {
		case statusLive:
			return true
		case statusOff:
			return false
		case statusOrphaned:
		}
		if !time.Now().Before(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(frontendReloadPoll):
		}
	}
}

// HandOffUI leaves UI events to API clients while the frontend is active, so
// it draws them instead of the menu widgets. The widgets end running media to
// show any event that reaches them, so an event raised while media runs
// returns to the menu, which makes Main start the frontend, and is kept open
// long enough for the frontend to start. An event raised at the menu before
// the frontend has started, as when a game has just exited and Main is still
// loading the menu, is kept open the same way: the widget cannot switch
// consoles under a starting frontend. Main names a new process in the status
// file after each core load, so a status whose writer has just gone is
// waited on briefly before the widget gets the event. Events the widgets never show over
// media (the auto-advance switch countdown, the launch guard) are decided
// before they reach the renderer and are unaffected.
func (p *Platform) HandOffUI(ctx context.Context, event *models.UIEvent) (handled bool, minOpen time.Duration) {
	wait := frontendReloadWait
	if p.uiReloadWait != nil {
		wait = *p.uiReloadWait
	}
	if !frontendActiveSettled(ctx, p.filesystem(), wait) {
		return false, 0
	}
	if p.activeMedia == nil || p.activeMedia() == nil {
		// Nothing running means the menu is up, but Main can report the
		// frontend active before it has started (a game just exited and the
		// menu is loading, or it was just enabled). It draws the event once it
		// is up, so the event is held open for it to start.
		if !p.frontendRunning() {
			log.Debug().Str("event_id", event.ID).
				Msg("frontend active but not running yet; holding the UI event for it")
			return true, frontendColdStartHold
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
		// No menu, no frontend: let the widget try to show it instead.
		log.Warn().Err(err).Msg("failed to return to the menu for a UI event")
		return false, 0
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
