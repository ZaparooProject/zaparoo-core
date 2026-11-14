//go:build windows

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

package windows

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/Microsoft/go-winio"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/command"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/playnite"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/steam"
	"github.com/rs/zerolog/log"
	"github.com/shirou/gopsutil/v4/process"
	"github.com/spf13/afero"
)

// playniteIntegration returns the Playnite integration, creating it on first
// use. Launchers() can run before StartPost has supplied the ActiveMedia
// hooks, so the integration reads them through closures at call time rather
// than capturing them when it is built.
func (p *Platform) playniteIntegration() *playnite.Integration {
	p.playniteMu.Lock()
	defer p.playniteMu.Unlock()
	if p.playnite == nil {
		steamClient := steam.NewClient(steam.DefaultWindowsOptions())
		p.playnite = playnite.NewIntegration(&playnite.Deps{
			ActiveMedia: func() *models.ActiveMedia {
				if p.activeMedia == nil {
					return nil
				}
				return p.activeMedia()
			},
			SetActiveMedia: func(media *models.ActiveMedia) {
				if p.setActiveMedia != nil {
					p.setActiveMedia(media)
				}
			},
			TrackProcess: p.trackPlayniteProcess,
			UntrackProcess: func(pid int) {
				p.ClearTrackedProcessPID(pid)
			},
			WriteTag: writePlayniteTag,
			SteamIndexed: func(cfg *config.Instance) bool {
				if cfg == nil {
					return false
				}
				info, err := os.Stat(steamClient.FindSteamDir(cfg))
				return err == nil && info.IsDir()
			},
			Frontend: playnite.NewFrontend(&command.RealExecutor{}),
			Locator: playnite.Locator{
				FS:         afero.NewOsFs(),
				Registry:   playnite.LocateFromRegistry,
				Candidates: playnite.DefaultInstallDirs(os.Getenv("LOCALAPPDATA")),
			},
		})
	}
	return p.playnite
}

// initPlaynitePipe starts serving the pipe the Playnite extension connects
// to. The pipe is always served: a portable Playnite has no install Core can
// detect, and Playnite may be installed after Core starts.
func (p *Platform) initPlaynitePipe(cfg *config.Instance) {
	listener, err := winio.ListenPipe(playnite.PipeName, nil)
	if err != nil {
		log.Warn().Err(err).Msg("failed to create Playnite named pipe")
		return
	}
	p.playniteIntegration().Serve(cfg, listener)
	log.Debug().Msgf("Playnite named pipe server listening on %s", playnite.PipeName)
}

// trackPlayniteProcess adopts the process Playnite started for the running
// game, so a stop can end its tree if the extension cannot.
//
// The PID arrives over the pipe, so by the time Core opens it the process may
// have exited and Windows may have reused the number. Adopting the wrong PID
// would mean force-killing an unrelated process tree on the next stop, so the
// image is checked against the one the extension saw before it is trusted.
func (p *Platform) trackPlayniteProcess(pid int, exe string) {
	if pid <= 0 || !playniteProcessMatches(pid, exe) {
		log.Debug().Int("pid", pid).Str("exe", exe).Msg("not tracking Playnite game process")
		return
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		log.Debug().Err(err).Int("pid", pid).Msg("could not open Playnite game process")
		return
	}
	p.SetTrackedProcess(proc)
}

// playniteProcessMatches reports whether pid still runs the image the
// extension reported. Without an image to compare, the PID is not trusted.
func playniteProcessMatches(pid int, exe string) bool {
	if exe == "" {
		return false
	}
	proc, err := process.NewProcess(int32(pid)) //nolint:gosec // Windows PIDs fit in int32.
	if err != nil {
		return false
	}
	running, err := proc.Exe()
	if err != nil || running == "" {
		return false
	}
	return strings.EqualFold(filepath.Clean(running), filepath.Clean(exe))
}

// writePlayniteTag sends the extension's "write to tag" request to the API.
func writePlayniteTag(cfg *config.Instance, path string) {
	if cfg == nil {
		return
	}
	params, err := json.Marshal(&models.ReaderWriteParams{Text: path})
	if err != nil {
		log.Error().Err(err).Msg("failed to marshal Playnite write params")
		return
	}
	if _, err := client.LocalClient(context.Background(), cfg, models.MethodReadersWrite, string(params)); err != nil {
		log.Error().Err(err).Msg("failed to send Playnite write request to API")
		return
	}
	log.Info().Msgf("Playnite write request sent to API: %s", path)
}
