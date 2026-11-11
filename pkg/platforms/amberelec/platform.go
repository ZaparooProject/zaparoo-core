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

// Package amberelec is AmberELEC, whose EmulationStation is Batocera's fork
// but whose /emukill does nothing: games are stopped the way its jslisten
// hotkey stops them.
package amberelec

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esbase"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const (
	// HomeDir is AmberELEC's HOME, hardcoded because the service may start
	// before it is set.
	HomeDir   = "/storage"
	DataDir   = HomeDir + "/.local/share/" + config.AppName
	ConfigDir = HomeDir + "/.config/" + config.AppName
	LogDir    = DataDir + "/" + config.LogsDir
	// SystemESConfigDir holds the stock es_systems.cfg.
	SystemESConfigDir = "/usr/config/emulationstation"
	// UserESConfigDir holds the user's es_systems_*.cfg overrides.
	UserESConfigDir = HomeDir + "/.config/emulationstation"
	// ROMRoot is AmberELEC's ROM directory.
	ROMRoot = HomeDir + "/roms"
	// JSListenConfig holds the command AmberELEC's exit hotkey runs. Its
	// launcher rewrites it for every game; the first program, in [Generic],
	// is always "/usr/bin/killall <names>".
	JSListenConfig = HomeDir + "/.config/distribution/configs/jslisten.cfg"

	killGameGrace  = time.Second
	killRetryDelay = time.Second
)

var errUnsupportedProgram = errors.New("jslisten program is not a game kill")

// Platform is AmberELEC.
type Platform struct {
	*esbase.Platform
}

// NewPlatform returns the AmberELEC platform.
func NewPlatform() *Platform {
	return &Platform{Platform: esbase.New(Options())}
}

// Options describes AmberELEC to the shared EmulationStation platform.
func Options() *esbase.Options {
	return newOptions(&stopper{
		fs:     afero.NewOsFs(),
		killer: esbase.NewProcessKiller(),
	})
}

func newOptions(s *stopper) *esbase.Options {
	return &esbase.Options{
		ID:             platformids.AmberELEC,
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

// nonGameSystems are AmberELEC's EmulationStation systems for tools and
// screenshots, kept out of the ROM roots.
var nonGameSystems = []string{"imageviewer", "tools"}

type stopper struct {
	fs     afero.Fs
	killer *esbase.ProcessKiller
}

// exitGame ends the game the way the exit hotkey would: SIGTERM to the
// processes jslisten.cfg names, then SIGKILL for any still running when ctx
// ends.
func (s *stopper) exitGame(ctx context.Context) error {
	return s.stop(ctx)
}

// killGame repeats the stop with a short grace period.
func (s *stopper) killGame() error {
	ctx, cancel := context.WithTimeout(context.Background(), killGameGrace)
	defer cancel()
	return s.stop(ctx)
}

func (s *stopper) stop(ctx context.Context) error {
	program, err := readProgram(s.fs)
	if err != nil {
		return err
	}
	names, err := killallNames(program)
	if err != nil {
		return err
	}
	n, err := s.killer.Kill(ctx, names)
	if err != nil {
		return fmt.Errorf("kill %v: %w", names, err)
	}
	log.Debug().Strs("names", names).Int("count", n).Msg("signalled game processes")
	return nil
}

// readProgram returns the program value from jslisten.cfg.
func readProgram(fs afero.Fs) (string, error) {
	data, err := afero.ReadFile(fs, JSListenConfig)
	if err != nil {
		return "", fmt.Errorf("read jslisten config: %w", err)
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		key, value, ok := strings.Cut(strings.TrimSpace(scanner.Text()), "=")
		if !ok || strings.TrimSpace(key) != "program" {
			continue
		}
		value = strings.TrimSpace(value)
		if unquoted, err := strconv.Unquote(value); err == nil {
			value = unquoted
		}
		return strings.TrimSpace(value), nil
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read jslisten config: %w", err)
	}
	return "", errors.New("jslisten config has no program")
}

// killallNames returns the process names a plain killall command line
// ends, refusing any other command and any killall option. AmberELEC's
// modules and ports pass "killall <name>" to a helper that adds its own
// killall, so repeated leading killall words are dropped.
func killallNames(program string) ([]string, error) {
	fields := strings.Fields(program)
	if len(fields) == 0 || filepath.Base(fields[0]) != "killall" {
		return nil, fmt.Errorf("%w: %q", errUnsupportedProgram, program)
	}
	fields = fields[1:]
	for len(fields) > 0 && filepath.Base(fields[0]) == "killall" {
		fields = fields[1:]
	}
	var names []string
	for _, field := range fields {
		if strings.HasPrefix(field, "-") {
			// An option may take the next field as its value, so the names
			// cannot be told apart from it.
			return nil, fmt.Errorf("%w: %q", errUnsupportedProgram, program)
		}
		if !esbase.ValidProcessName(field) || field == "emulationstation" {
			log.Warn().Str("name", field).Msg("ignoring process name in jslisten config")
			continue
		}
		names = append(names, field)
	}
	if len(names) == 0 {
		return nil, esbase.ErrNoProcessNames
	}
	return names, nil
}
