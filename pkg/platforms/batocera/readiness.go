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

package batocera

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/gamehandoff"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog/log"
)

const (
	// EmulationStation reports a game as running about 2 s before its emulator
	// has loaded it, and RetroArch has no network command interface on
	// Batocera unless the user enables it. RetroArch reads the game file once
	// as it starts the content and draws the first frame about a second
	// later: measured on a Pi 4, the file closed 4.4 s after the launch and
	// the first frame was on screen at 5.5 s.
	gameSettle = 1500 * time.Millisecond
	// gameOpenWindow is how long to wait for the game file to be opened at
	// all. Systems whose game is a folder or is read some other way never
	// open it, and waiting on them would only stall scripts. It must stay
	// under the service's readiness timeout or the wait ends there first;
	// measured opens come 3 to 8 s after a launch.
	gameOpenWindow = 8 * time.Second
	// launchFreshness is how long a launch stays the one a probe belongs to.
	launchFreshness = 30 * time.Second
)

// launches returns the log of games Core launched, creating it on first use
// because the platform is built as a zero value.
func (p *Platform) launches() *gamehandoff.LaunchLog {
	p.launchLogMu.Lock()
	defer p.launchLogMu.Unlock()
	if p.launchLog == nil {
		p.launchLog = gamehandoff.NewLaunchLog(p.readyClock())
	}
	return p.launchLog
}

func (p *Platform) readyClock() clockwork.Clock {
	if p.clock == nil {
		return clockwork.NewRealClock()
	}
	return p.clock
}

// noteLaunches wraps launch so the probe can tell a game Core launched from
// one that was already running when the tracker noticed it, which was opened
// before anything could watch.
func (p *Platform) noteLaunches(
	launch func(*config.Instance, string, *platforms.LaunchOptions) (*os.Process, error),
) func(*config.Instance, string, *platforms.LaunchOptions) (*os.Process, error) {
	return func(cfg *config.Instance, path string, opts *platforms.LaunchOptions) (*os.Process, error) {
		p.launches().Note(path)
		return launch(cfg, path, opts)
	}
}

// waitForGame is the readiness probe for EmulationStation systems: the game
// is ready a settle after its emulator has read the game file.
func (p *Platform) waitForGame(ctx context.Context, _ *config.Instance, media *models.ActiveMedia) error {
	if !p.launches().Recent(media.Path, launchFreshness) {
		log.Info().Str("path", media.Path).Msg("media was not launched by Core; treating the game as ready")
		return nil
	}
	info, err := os.Stat(media.Path)
	if err != nil || !info.Mode().IsRegular() {
		log.Debug().Str("path", media.Path).Msg("game is not a file to watch; treating it as ready")
		return nil //nolint:nilerr // a game that is not a file has nothing to wait for
	}

	watch := p.watchGame
	if watch == nil {
		watch = gamehandoff.Watch
	}
	events, err := watch(ctx, media.Path)
	if err != nil {
		return fmt.Errorf("%w: watch game file: %w", platforms.ErrReadinessUnavailable, err)
	}
	if err := gamehandoff.Wait(ctx, p.readyClock(), events, gamehandoff.Options{
		Settle:     gameSettle,
		OpenWindow: gameOpenWindow,
		UntilClose: true,
	}); err != nil {
		return fmt.Errorf("wait for the emulator to load %s: %w", media.Path, err)
	}
	return nil
}
