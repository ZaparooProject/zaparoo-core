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

package esbase

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const (
	// commLen is how much of a process name the kernel keeps in comm.
	commLen = 15
	// maxProcessNameLen bounds a name read from a config file.
	maxProcessNameLen = 255
)

// ErrNoProcessNames means a kill was asked for nothing at all.
var ErrNoProcessNames = errors.New("no process names to kill")

// ValidProcessName reports whether name is a bare process name, as killall
// takes one: no path, no whitespace, no option, nothing else.
func ValidProcessName(name string) bool {
	if name == "" || len(name) > maxProcessNameLen || name == "." || name == ".." {
		return false
	}
	if strings.HasPrefix(name, "-") {
		return false
	}
	for _, r := range name {
		if r == '/' || r <= ' ' || r == 0x7f {
			return false
		}
	}
	return true
}

// ProcessKiller ends processes by name as killall does: SIGTERM, then
// SIGKILL for any still alive once the grace period or the caller's context
// runs out.
type ProcessKiller struct {
	FS     afero.Fs
	Clock  clockwork.Clock
	Signal func(pid int, sig syscall.Signal) error
	// ProcPath is where processes are listed, normally /proc.
	ProcPath string
	Grace    time.Duration
	Poll     time.Duration
	// SelfPID is never signalled.
	SelfPID int
}

// NewProcessKiller returns a killer for this system's processes.
func NewProcessKiller() *ProcessKiller {
	return &ProcessKiller{
		FS:       afero.NewOsFs(),
		Clock:    clockwork.NewRealClock(),
		Signal:   syscall.Kill,
		ProcPath: "/proc",
		Grace:    5 * time.Second,
		Poll:     100 * time.Millisecond,
		SelfPID:  os.Getpid(),
	}
}

// Kill ends every process called one of names and reports how many it
// signalled. Invalid names are skipped.
func (k *ProcessKiller) Kill(ctx context.Context, names []string) (int, error) {
	wanted, err := validNames(names)
	if err != nil {
		return 0, err
	}

	pids := k.find(wanted)
	if len(pids) == 0 {
		return 0, nil
	}
	for _, pid := range pids {
		if err := k.Signal(pid, syscall.SIGTERM); err != nil {
			log.Debug().Err(err).Int("pid", pid).Msg("SIGTERM failed")
		}
	}

	deadline := k.Clock.Now().Add(k.Grace)
	for {
		alive := k.alive(pids, wanted)
		if len(alive) == 0 {
			return len(pids), nil
		}
		if !k.Clock.Now().Before(deadline) {
			return len(pids), k.forceKill(alive)
		}
		select {
		case <-ctx.Done():
			return len(pids), k.forceKill(k.alive(pids, wanted))
		case <-k.Clock.After(k.Poll):
		}
	}
}

// KillNow sends SIGKILL to every process called one of names, as killall -9
// does, and reports how many it signalled. Invalid names are skipped.
func (k *ProcessKiller) KillNow(names []string) (int, error) {
	wanted, err := validNames(names)
	if err != nil {
		return 0, err
	}
	pids := k.find(wanted)
	return len(pids), k.forceKill(pids)
}

func validNames(names []string) ([]string, error) {
	wanted := make([]string, 0, len(names))
	for _, name := range names {
		if ValidProcessName(name) {
			wanted = append(wanted, name)
		} else {
			log.Warn().Str("name", name).Msg("refusing to kill a process by an invalid name")
		}
	}
	if len(wanted) == 0 {
		return nil, ErrNoProcessNames
	}
	return wanted, nil
}

func (k *ProcessKiller) forceKill(pids []int) error {
	var errs []error
	for _, pid := range pids {
		log.Debug().Int("pid", pid).Msg("sending SIGKILL")
		if err := k.Signal(pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			errs = append(errs, fmt.Errorf("SIGKILL %d: %w", pid, err))
		}
	}
	return errors.Join(errs...)
}

// alive returns the pids that still belong to one of names, so a pid reused
// by another process meanwhile is left alone. A zombie has already exited
// and only waits for its parent to reap it, so it does not count.
func (k *ProcessKiller) alive(pids []int, names []string) []int {
	var alive []int
	for _, pid := range pids {
		if k.matches(pid, names) && !k.zombie(pid) {
			alive = append(alive, pid)
		}
	}
	return alive
}

func (k *ProcessKiller) find(names []string) []int {
	entries, err := afero.ReadDir(k.FS, k.ProcPath)
	if err != nil {
		log.Debug().Err(err).Msg("cannot list processes")
		return nil
	}
	var pids []int
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 1 || pid == k.SelfPID {
			continue
		}
		if k.matches(pid, names) {
			pids = append(pids, pid)
		}
	}
	return pids
}

func (k *ProcessKiller) matches(pid int, names []string) bool {
	dir := filepath.Join(k.ProcPath, strconv.Itoa(pid))
	data, err := afero.ReadFile(k.FS, filepath.Join(dir, "comm"))
	if err != nil {
		return false
	}
	comm := strings.TrimRight(string(data), "\n")
	for _, name := range names {
		if comm == name {
			return true
		}
		// comm holds only the first 15 bytes of a longer name, so the
		// command line has to confirm it.
		if len(name) > commLen && comm == name[:commLen] && argv0Base(k.FS, dir) == name {
			return true
		}
	}
	return false
}

// zombie reports whether pid has exited and not been reaped. The state
// follows the name in /proc/<pid>/stat, and the name may itself hold spaces
// and parentheses, so it is read after the last closing parenthesis.
func (k *ProcessKiller) zombie(pid int) bool {
	data, err := afero.ReadFile(k.FS, filepath.Join(k.ProcPath, strconv.Itoa(pid), "stat"))
	if err != nil {
		return false
	}
	end := bytes.LastIndexByte(data, ')')
	if end < 0 {
		return false
	}
	fields := strings.Fields(string(data[end+1:]))
	return len(fields) > 0 && fields[0] == "Z"
}

func argv0Base(fs afero.Fs, dir string) string {
	data, err := afero.ReadFile(fs, filepath.Join(dir, "cmdline"))
	if err != nil || len(data) == 0 {
		return ""
	}
	argv0, _, _ := bytes.Cut(data, []byte{0})
	return filepath.Base(string(argv0))
}
