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
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
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

const (
	// flashpointStartTimeout bounds the wait for a cold Flashpoint to be able
	// to take the game's link. Its first start of the day took 20 s while it
	// downloaded updates.
	flashpointStartTimeout = 60 * time.Second
	// flashpointStartSettle is how long after its game server appears
	// Flashpoint is given before the link is handed over.
	flashpointStartSettle = time.Second
)

// launchFlashpointGame starts game id. Flashpoint only acts on a
// flashpoint:// link handed to an instance that is already running: started
// cold with the link it ignores it, and started from another working
// directory it cannot load its data at all. So a cold Flashpoint is started
// from its Launcher folder first, and the link handed over once it is up.
func (p *Platform) launchFlashpointGame(id string) error {
	link := "flashpoint://run/" + id
	dir, dirErr := findFlashpointDir()
	launcherDir := ""
	if dirErr == nil {
		launcherDir = filepath.Join(dir, "Launcher")
		if err := p.ensureFlashpointRunning(dir); err != nil {
			return err
		}
	} else {
		log.Debug().Err(dirErr).Msg("Flashpoint install not found; handing the link to Windows")
	}

	//nolint:gosec // Safe: launches Flashpoint with game ID from internal database
	cmd := exec.CommandContext(context.Background(), helpers.ComSpec(), "/c", "start", link)
	cmd.Dir = launcherDir
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start flashpoint: %w", err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// ensureFlashpointRunning starts the Flashpoint installed in dir when it is not
// running and waits until it can take a game. A stop during the wait ends it.
func (p *Platform) ensureFlashpointRunning(dir string) error {
	launcherDir := filepath.Join(dir, "Launcher")
	exe := filepath.Join(launcherDir, "Flashpoint.exe")
	if pid, running := findProcessByPath(exe); running {
		log.Debug().Uint32("pid", pid).Msg("Flashpoint is running")
		return nil
	}

	log.Info().Str("exe", exe).Msg("starting Flashpoint before handing it the game")
	cmd := exec.CommandContext(context.Background(), exe) //nolint:gosec // registered Flashpoint install
	cmd.Dir = launcherDir
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start Flashpoint: %w", err)
	}
	go func() { _ = cmd.Wait() }()

	ctx := context.Background()
	if p.launcherManager != nil {
		ctx = p.launcherManager.GetContext()
	}
	if err := waitForFlashpointStart(
		ctx, p.clockForReadiness(), dir, flashpointStartTimeout, listWindowsProcesses, p.processHasWindow,
	); err != nil {
		return err
	}
	log.Info().Msg("Flashpoint is up")
	return nil
}

// waitForFlashpointStart waits for a cold Flashpoint in dir to be able to take
// a game's link. Its window opens seconds before its backend is up, and a link
// arriving in between is lost: the backend stops part way through starting and
// the whole launcher exits a minute later. The backend starts its game server
// once its services are up, so that process marks it ready. A Flashpoint
// without the game server is handed the link once the wait, bounded by
// timeout, runs out.
func waitForFlashpointStart(
	ctx context.Context,
	clock clockwork.Clock,
	dir string,
	timeout time.Duration,
	listProcesses func() ([]windowsProcessInfo, error),
	hasWindow func(pid int) bool,
) error {
	launcher := filepath.Join(dir, "Launcher", "Flashpoint.exe")
	server := filepath.Join(dir, "Server", "Flashpoint Game Server.exe")

	// The timeout bounds the poll only: Flashpoint ready just before it runs
	// out still gets its settle.
	pollCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	windowUp := false
	err := platforms.PollReady(pollCtx, clock, flashpointReadyPoll, func(context.Context) (bool, error) {
		procs, listErr := listProcesses()
		if listErr != nil {
			return false, fmt.Errorf("%w: %w", platforms.ErrReadinessUnavailable, listErr)
		}
		serverUp := false
		for i := range procs {
			switch {
			case sameExePath(procs[i].ExePath, launcher):
				windowUp = windowUp || hasWindow(int(procs[i].PID))
			case sameExePath(procs[i].ExePath, server):
				serverUp = true
			}
		}
		return windowUp && serverUp, nil
	})
	switch {
	case err == nil:
	case windowUp && errors.Is(err, context.DeadlineExceeded):
		log.Warn().Msg("Flashpoint game server did not start; handing over the game anyway")
		return nil
	default:
		return fmt.Errorf("wait for Flashpoint to start: %w", err)
	}

	select {
	case <-ctx.Done():
		return fmt.Errorf("wait for Flashpoint to start: %w", ctx.Err())
	case <-clock.After(flashpointStartSettle):
		return nil
	}
}

func sameExePath(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

// findProcessByPath returns a running process whose image is exe.
func findProcessByPath(exe string) (uint32, bool) {
	procs, err := listWindowsProcesses()
	if err != nil {
		return 0, false
	}
	for i := range procs {
		if sameExePath(procs[i].ExePath, exe) {
			return procs[i].PID, true
		}
	}
	return 0, false
}

// killFlashpointGame stops a Flashpoint game. Flashpoint starts the game's
// player from its FPSoftware folder and Core holds no handle to it, so a stop
// cleared the media and left the game playing. Every player under that folder
// belongs to Flashpoint, so all of them are ended.
func killFlashpointGame(*config.Instance) error {
	dir, err := findFlashpointDir()
	if err != nil {
		return err
	}
	killed, err := killFlashpointPlayers(
		filepath.Join(dir, "FPSoftware"), listWindowsProcesses, retroBatProcessPath, retroBatRunTaskKill,
	)
	if err != nil {
		return fmt.Errorf("stopped %d Flashpoint players, others are still running: %w", killed, err)
	}
	// No player means the game has already been closed, which is what a stop
	// asks for. Reporting it as a failure kept the media active and refused
	// every launch after it.
	if killed == 0 {
		log.Debug().Msg("no Flashpoint game was running")
		return nil
	}
	log.Info().Int("players", killed).Msg("stopped Flashpoint game")
	return nil
}

func killFlashpointPlayers(
	playersDir string,
	listProcesses func() ([]windowsProcessInfo, error),
	processPath func(pid uint32) (string, error),
	killTree func(ctx context.Context, pid uint32) error,
) (int, error) {
	procs, err := listProcesses()
	if err != nil {
		return 0, fmt.Errorf("list processes: %w", err)
	}
	killed := 0
	var errs []error
	for i := range procs {
		if procs[i].PID == 0 || !helpers.PathHasPrefix(procs[i].ExePath, playersDir) {
			continue
		}
		// The PID may have been reused since the listing.
		if current, pathErr := processPath(procs[i].PID); pathErr != nil ||
			!helpers.PathHasPrefix(current, playersDir) {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), retroBatProcessKillTimeout)
		killErr := killTree(ctx, procs[i].PID)
		cancel()
		if killErr != nil {
			errs = append(errs, fmt.Errorf("kill pid %d: %w", procs[i].PID, killErr))
			continue
		}
		killed++
	}
	// A player left running is a stop that did not happen, however many
	// others were ended.
	return killed, errors.Join(errs...)
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
