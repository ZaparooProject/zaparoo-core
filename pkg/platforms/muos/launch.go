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

package muos

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const (
	pickupPolls       = 150
	overlayGonePolls  = 100
	readyPollInterval = 250 * time.Millisecond
	readyTimeout      = 30 * time.Second
)

var errNotPickedUp = errors.New("muOS frontend did not pick up the content request")

func (p *Platform) setLaunching(v bool) {
	p.trackerMu.Lock()
	p.launching = v
	p.trackerMu.Unlock()
}

// launchContent hands a ROM to the frontend's content loader. The loader
// only reads its request between frontend modules, so whatever is in the
// foreground is closed to let it run.
func (p *Platform) launchContent(_ *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
	p.launchMu.Lock()
	defer p.launchMu.Unlock()

	fs := p.filesystem()
	req, err := resolveContent(fs, p.coreDB(), path)
	if err != nil {
		return nil, err
	}
	log.Info().
		Str("path", req.path).
		Str("system", req.system).
		Str("core", req.core).
		Str("tag", req.tag).
		Msg("launching through muOS content loader")

	p.setLaunching(true)
	defer p.setLaunching(false)

	if err := p.endRunningContent(); err != nil {
		return nil, err
	}

	if err := writeAssets(fs, req); err != nil {
		return nil, err
	}
	if err := writeContentFile(fs, req); err != nil {
		return nil, err
	}
	p.trackerMu.Lock()
	p.pending = req.name
	p.trackerMu.Unlock()

	if err := p.awaitPickup(req); err != nil {
		if rmErr := removeIfExists(fs, contentFile); rmErr != nil {
			log.Warn().Err(rmErr).Msg("failed to withdraw muOS content request")
		}
		p.trackerMu.Lock()
		p.pending = ""
		p.trackerMu.Unlock()
		return nil, err
	}
	return nil, nil //nolint:nilnil // The frontend owns the content process.
}

// endRunningContent closes running content and waits for the frontend to
// finish its cleanup, which deletes the per-launch settings files.
func (p *Platform) endRunningContent() error {
	fs := p.filesystem()
	if _, running := readOverlay(fs); !running {
		return nil
	}
	fg := p.foreground()
	if fg == "" || isMenuModule(fg) {
		return nil
	}
	if err := p.closeProcess(fg); err != nil {
		return fmt.Errorf("closing running content: %w", err)
	}
	if p.setActiveMedia != nil {
		p.setActiveMedia(nil)
	}

	gone := false
	for range overlayGonePolls {
		if !fileExists(fs, overlayFile) {
			gone = true
			break
		}
		p.pause(closePollStep)
	}
	if !gone {
		log.Warn().Msg("muOS did not finish cleaning up after the previous content")
	}

	p.trackerMu.Lock()
	p.lastOverlay = ""
	p.trackerMu.Unlock()
	return nil
}

// awaitPickup waits for the loader to consume the request, closing the
// foreground menu module so the frontend loop reaches the loader.
func (p *Platform) awaitPickup(req *contentRequest) error {
	fs := p.filesystem()
	closed := make(map[string]bool)
	for range pickupPolls {
		if !fileExists(fs, contentFile) {
			return nil
		}
		fg := p.foreground()
		if fg != "" && !closed[fg] && len(p.foregroundPIDs(fg)) > 0 {
			closed[fg] = true
			if isMenuModule(fg) {
				// Leaving a menu clears the per-launch settings, and
				// safe_quit tells the frontend the exit was intended.
				if err := writeAssets(fs, req); err != nil {
					return err
				}
				if err := afero.WriteFile(fs, safeQuitFile, nil, 0o644); err != nil {
					log.Warn().Err(err).Msg("failed to write muOS safe quit marker")
				}
			}
			if err := p.closeProcess(fg); err != nil {
				log.Warn().Err(err).Str("process", fg).Msg("failed to close muOS foreground process")
			}
			continue
		}
		p.pause(closePollStep)
	}
	if !fileExists(fs, contentFile) {
		return nil
	}
	return errNotPickedUp
}

// waitForReady waits until the frontend reports the launched content as
// running.
func (p *Platform) waitForReady(ctx context.Context, _ *config.Instance, media *models.ActiveMedia) error {
	if media == nil {
		return nil
	}
	want := contentName(media.Path)
	clock := p.getClock()
	for {
		if state, ok := readOverlay(p.filesystem()); ok && state.name == want {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for muOS content: %w", ctx.Err())
		case <-clock.After(readyPollInterval):
		}
	}
}

func (p *Platform) gameRunning(_ context.Context, _ *config.Instance, media *models.ActiveMedia) (bool, error) {
	if media == nil {
		return false, nil
	}
	want := contentName(media.Path)
	if state, ok := readOverlay(p.filesystem()); ok && state.name == want {
		return true, nil
	}
	p.trackerMu.Lock()
	if p.pending == want {
		p.pending = ""
	}
	p.trackerMu.Unlock()
	return false, nil
}

func readyTimeoutFor(_ *models.ActiveMedia) time.Duration {
	return readyTimeout
}
