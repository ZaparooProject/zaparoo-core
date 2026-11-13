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

// Package rocknix is ROCKNIX, whose EmulationStation is Batocera's fork but
// whose /emukill does nothing: games are stopped the way its own hotkey
// daemon stops them.
package rocknix

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/command"
	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esbase"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const (
	// HomeDir is ROCKNIX's HOME, hardcoded because the service may start
	// before it is set.
	HomeDir   = "/storage"
	DataDir   = HomeDir + "/.local/share/" + config.AppName
	ConfigDir = HomeDir + "/.config/" + config.AppName
	LogDir    = DataDir + "/" + config.LogsDir
	// SystemESConfigDir holds the stock es_systems.cfg.
	SystemESConfigDir = "/usr/config/emulationstation"
	// UserESConfigDir holds the user's es_systems_*.cfg overrides.
	UserESConfigDir = HomeDir + "/.config/emulationstation"
	// ROMRoot is ROCKNIX's ROM directory.
	ROMRoot = HomeDir + "/roms"
	// KillDataFile names the processes the running game is made of. Its
	// launcher rewrites it for every game and sets it to emulationstation
	// when the game ends.
	KillDataFile = "/tmp/.process-kill-data"
	// idleKillTarget in KillDataFile means no game is running.
	idleKillTarget = "emulationstation"
	// gameScopes matches the systemd scopes ROCKNIX runs every game in.
	gameScopes = "game-*.scope"

	killGameTimeout = 10 * time.Second
	killRetryDelay  = time.Second
)

// errIdle means the kill data says no game is running.
var errIdle = errors.New("no game is running")

// Platform is ROCKNIX.
type Platform struct {
	*esbase.Platform
}

// NewPlatform returns the ROCKNIX platform.
func NewPlatform() *Platform {
	return &Platform{Platform: esbase.New(Options())}
}

// Options describes ROCKNIX to the shared EmulationStation platform.
func Options() *esbase.Options {
	return newOptions(&stopper{
		fs:     afero.NewOsFs(),
		killer: esbase.NewProcessKiller(),
		cmd:    &command.RealExecutor{},
	})
}

// nonGameSystems are ROCKNIX's EmulationStation systems for tools, music,
// screenshots and settings, kept out of the ROM roots.
var nonGameSystems = []string{"idtech", "imageviewer", "music", "tools"}

func newOptions(s *stopper) *esbase.Options {
	return &esbase.Options{
		ID:             platformids.ROCKNIX,
		DataDir:        DataDir,
		ConfigDir:      ConfigDir,
		LogDir:         LogDir,
		DefaultROMRoot: ROMRoot,
		ESConfigDirs:   []string{SystemESConfigDir, UserESConfigDir},
		ExitGame:       s.exitGame,
		KillGame:       s.killGame,
		KillRetryDelay: killRetryDelay,
		SystemAliases:  esbase.ELECSystemAliases,
		NonGameSystems: nonGameSystems,
	}
}

type stopper struct {
	fs     afero.Fs
	killer *esbase.ProcessKiller
	cmd    command.Executor
}

// exitGame ends the processes the kill data names: SIGTERM, then SIGKILL
// for any still running when ctx ends. Kill data marked -9 asks for SIGKILL
// straight away, as ROCKNIX's own hotkey does.
func (s *stopper) exitGame(ctx context.Context) error {
	names, force, err := readKillData(s.fs)
	if err != nil {
		return err
	}
	var n int
	if force {
		n, err = s.killer.KillNow(names)
	} else {
		n, err = s.killer.Kill(ctx, names)
	}
	if err != nil {
		return fmt.Errorf("kill %v: %w", names, err)
	}
	log.Debug().Strs("names", names).Bool("force", force).Int("count", n).Msg("signalled game processes")
	return nil
}

// killGame stops every game scope, which systemd ends with SIGTERM and then
// SIGKILL.
func (s *stopper) killGame() error {
	ctx, cancel := context.WithTimeout(context.Background(), killGameTimeout)
	defer cancel()
	if err := s.cmd.Run(ctx, "systemctl", "stop", gameScopes); err != nil {
		return fmt.Errorf("systemctl stop %s: %w", gameScopes, err)
	}
	return nil
}

// readKillData returns the process names in KillDataFile, never
// EmulationStation itself, and whether the launcher asked for SIGKILL with
// -9. Other options are dropped.
func readKillData(fs afero.Fs) (names []string, force bool, err error) {
	data, err := afero.ReadFile(fs, KillDataFile)
	if err != nil {
		return nil, false, fmt.Errorf("read kill data: %w", err)
	}
	for _, field := range strings.Fields(string(data)) {
		switch {
		case field == idleKillTarget:
			return nil, false, errIdle
		case field == "-9" || field == "-KILL" || field == "-SIGKILL":
			force = true
		case strings.HasPrefix(field, "-"):
			continue
		case !esbase.ValidProcessName(field):
			log.Warn().Str("name", field).Msg("ignoring invalid process name in kill data")
		default:
			names = append(names, field)
		}
	}
	if len(names) == 0 {
		return nil, false, esbase.ErrNoProcessNames
	}
	return names, force, nil
}
