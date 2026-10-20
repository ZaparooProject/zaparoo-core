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

package windows

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog/log"
	"golang.org/x/sys/windows/registry"
)

const (
	// flashpointReadyPoll is how often the probe looks at the process list.
	flashpointReadyPoll = 250 * time.Millisecond
	// flashpointReadySettle is how long after the game's window appears it is
	// given to start running.
	flashpointReadySettle = 500 * time.Millisecond
	// flashpointReadyTimeout bounds the wait for the game's window. Flashpoint
	// Infinity downloads a game's files the first time it is played: a Flash
	// game's window opened 7.6 s after the launch, which is already past the
	// service's default timeout.
	flashpointReadyTimeout = 30 * time.Second
)

var (
	errFlashpointNotFound  = errors.New("flashpoint is not registered as the flashpoint:// handler")
	errFlashpointBadLayout = errors.New("flashpoint handler is not inside a Launcher folder")
)

// flashpointReadyTimeoutFor extends the readiness timeout for Flashpoint games.
func flashpointReadyTimeoutFor(*models.ActiveMedia) time.Duration {
	return flashpointReadyTimeout
}

// findFlashpointDir returns the Flashpoint install directory from the
// program registered for flashpoint:// links, which is
// <install>\Launcher\Flashpoint.exe.
func findFlashpointDir() (string, error) {
	key, err := registry.OpenKey(registry.CLASSES_ROOT, `flashpoint\shell\open\command`, registry.QUERY_VALUE)
	if err != nil {
		return "", fmt.Errorf("%w: %w", errFlashpointNotFound, err)
	}
	defer func() { _ = key.Close() }()
	command, _, err := key.GetStringValue("")
	if err != nil {
		return "", fmt.Errorf("%w: %w", errFlashpointNotFound, err)
	}
	return flashpointDirFromCommand(command)
}

// flashpointDirFromCommand extracts the install directory from a registered
// shell command such as `"C:\Flashpoint\Launcher\Flashpoint.exe" "%1"`.
func flashpointDirFromCommand(command string) (string, error) {
	command = strings.TrimSpace(command)
	exe := command
	if rest, ok := strings.CutPrefix(command, `"`); ok {
		exe, _, _ = strings.Cut(rest, `"`)
	} else if i := strings.IndexByte(command, ' '); i >= 0 {
		exe = command[:i]
	}
	launcherDir := filepath.Dir(exe)
	if !strings.EqualFold(filepath.Base(launcherDir), "Launcher") {
		return "", fmt.Errorf("%w: %s", errFlashpointBadLayout, exe)
	}
	return filepath.Dir(launcherDir), nil
}

// waitForFlashpointGame is the readiness probe for Flashpoint games. The
// launcher starts the game's player from its FPSoftware folder some time after
// the flashpoint:// link is handed over, and Core holds no handle to it, so the
// game is ready once a player that was not running when the probe started has
// a window. A player left over from the previous game does not count.
func (p *Platform) waitForFlashpointGame(ctx context.Context, _ *config.Instance, _ *models.ActiveMedia) error {
	return waitForFlashpointPlayer(
		ctx, p.clockForReadiness(), findFlashpointDir, listWindowsProcesses, p.processHasWindow,
	)
}

func waitForFlashpointPlayer(
	ctx context.Context,
	clock clockwork.Clock,
	findDir func() (string, error),
	listProcesses func() ([]windowsProcessInfo, error),
	hasWindow func(pid int) bool,
) error {
	dir, err := findDir()
	if err != nil {
		return fmt.Errorf("%w: %w", platforms.ErrReadinessUnavailable, err)
	}
	playersDir := filepath.Join(dir, "FPSoftware")

	var before map[uint32]struct{}
	err = platforms.PollReady(ctx, clock, flashpointReadyPoll, func(context.Context) (bool, error) {
		procs, listErr := listProcesses()
		if listErr != nil {
			return false, fmt.Errorf("%w: %w", platforms.ErrReadinessUnavailable, listErr)
		}
		if before == nil {
			before = make(map[uint32]struct{})
			for i := range procs {
				if helpers.PathHasPrefix(procs[i].ExePath, playersDir) {
					before[procs[i].PID] = struct{}{}
				}
			}
			return false, nil
		}
		for i := range procs {
			if _, old := before[procs[i].PID]; old || !helpers.PathHasPrefix(procs[i].ExePath, playersDir) {
				continue
			}
			if hasWindow(int(procs[i].PID)) {
				log.Debug().Str("player", procs[i].ExePath).Msg("Flashpoint game window is up")
				return true, nil
			}
		}
		return false, nil
	})
	if err != nil {
		return fmt.Errorf("wait for Flashpoint game: %w", err)
	}

	select {
	case <-ctx.Done():
		return fmt.Errorf("settle after the Flashpoint game window: %w", ctx.Err())
	case <-clock.After(flashpointReadySettle):
		return nil
	}
}
