//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package arkos

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/command"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const (
	esService = "emulationstation.service"
	// esStoppedMarker records that Core stopped ES, so the unit's
	// ExecStopPost and the next Core start can bring ES back if Core dies
	// while a game runs. Keep in sync with cmd/arkos/conf/zaparoo.service.
	esStoppedMarker  = "/run/zaparoo-arkos-es-stopped"
	perfnormPath     = "/usr/local/bin/perfnorm"
	systemctlTimeout = 30 * time.Second
)

// frontend owns the EmulationStation service while Core runs games. ES holds
// the display, so it is stopped before a game starts and started again once
// nothing Core launched is running and no launch is in progress.
type frontend struct {
	cmd       command.Executor
	fs        afero.Fs
	launching int
	gamePID   int
	mu        syncutil.Mutex
	stopped   bool
}

func newFrontend(cmd command.Executor, fs afero.Fs) *frontend {
	return &frontend{cmd: cmd, fs: fs}
}

func (f *frontend) systemctl(args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), systemctlTimeout)
	defer cancel()
	if err := f.cmd.Run(ctx, "systemctl", args...); err != nil {
		return fmt.Errorf("systemctl %v: %w", args, err)
	}
	return nil
}

// stopES stops ES before a game takes the display.
func (f *frontend) stopES() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.stopped {
		return nil
	}
	if err := f.writeMarker(); err != nil {
		log.Warn().Err(err).Msg("failed to record that EmulationStation was stopped")
	}
	if err := f.systemctl("stop", esService); err != nil {
		f.removeMarker()
		return err
	}
	f.stopped = true
	log.Debug().Msg("stopped EmulationStation for launch")
	return nil
}

func (f *frontend) beginLaunch() {
	f.mu.Lock()
	f.launching++
	f.mu.Unlock()
}

func (f *frontend) endLaunch() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.launching > 0 {
		f.launching--
	}
	f.restoreIfIdleLocked()
}

func (f *frontend) gameStarted(pid int) {
	f.mu.Lock()
	f.gamePID = pid
	f.mu.Unlock()
}

// gameExited is called once per game Core started, however it ended.
func (f *frontend) gameExited(pid int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.gamePID == pid {
		f.gamePID = 0
	}
	f.restoreIfIdleLocked()
}

// launchFailed restores ES after a launch that stopped it but started nothing.
func (f *frontend) launchFailed() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.restoreIfIdleLocked()
}

func (f *frontend) runningGame() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.gamePID
}

// restore starts ES regardless of launch state; used when Core shuts down.
func (f *frontend) restore() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.launching = 0
	f.gamePID = 0
	f.restoreLocked()
}

// recoverStale starts ES if a previous Core run stopped it and died before
// starting it again.
func (f *frontend) recoverStale() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, err := f.fs.Stat(esStoppedMarker); err != nil {
		return
	}
	log.Info().Msg("EmulationStation was left stopped by a previous run, starting it")
	f.stopped = true
	f.restoreLocked()
}

func (f *frontend) restoreIfIdleLocked() {
	if f.launching > 0 || f.gamePID != 0 {
		return
	}
	f.restoreLocked()
}

func (f *frontend) restoreLocked() {
	if !f.stopped {
		return
	}
	// A game killed by Core never reaches the "sudo perfnorm" at the end of
	// its ES command, so reset the CPU governor here.
	if _, err := f.fs.Stat(perfnormPath); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), systemctlTimeout)
		if err := f.cmd.Run(ctx, perfnormPath); err != nil {
			log.Debug().Err(err).Msg("perfnorm failed")
		}
		cancel()
	}
	if err := f.systemctl("start", esService); err != nil {
		log.Error().Err(err).Msg("failed to start EmulationStation")
		return
	}
	f.stopped = false
	f.removeMarker()
	log.Debug().Msg("started EmulationStation")
}

func (f *frontend) writeMarker() error {
	if err := afero.WriteFile(f.fs, esStoppedMarker, nil, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", esStoppedMarker, err)
	}
	return nil
}

func (f *frontend) removeMarker() {
	if err := f.fs.Remove(esStoppedMarker); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Warn().Err(err).Msg("failed to remove EmulationStation stop marker")
	}
}
