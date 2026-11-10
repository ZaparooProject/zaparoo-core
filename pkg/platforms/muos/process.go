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
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const (
	// commLen is the length the kernel truncates a process name to in comm.
	commLen        = 15
	closePollStep  = 100 * time.Millisecond
	closeTermPolls = 30
	closeKillPolls = 70
)

// externalForeground is the foreground name the general external launcher
// records. It runs the port script without exec, so no process carries it.
const (
	externalForeground = "external"
	externalLauncher   = "/opt/muos/script/launch/ext-general.sh"
)

// isMenuModule reports whether a foreground process name is one of the
// frontend's menu modules rather than running content. Every menu runs
// inside muxfrontend; muxcharge and muxbackup are the only other modules
// the boot scripts start. Content launchers also record mux names, such as
// muxretro and muxmedia, so a prefix match is not enough.
func isMenuModule(name string) bool {
	switch name {
	case "muxfrontend", "muxcharge", "muxbackup":
		return true
	default:
		return false
	}
}

func (p *Platform) foreground() string {
	return readFirstLine(p.filesystem(), foregroundFile)
}

// pidsOf lists live processes whose name or program matches name, the way
// pgrep matches the foreground process recorded by the frontend.
func (p *Platform) pidsOf(name string) []int {
	if name == "" || strings.ContainsRune(name, '/') {
		return nil
	}
	fs := p.filesystem()
	entries, err := afero.ReadDir(fs, p.procDir())
	if err != nil {
		return nil
	}
	comm := name
	if len(comm) > commLen {
		comm = comm[:commLen]
	}
	self := p.selfPID()

	var pids []int
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 0 || pid == self || !e.IsDir() {
			continue
		}
		dir := filepath.Join(p.procDir(), e.Name())
		if procZombie(fs, dir) {
			continue
		}
		if readFirstLine(fs, filepath.Join(dir, "comm")) == comm || procProgram(fs, dir) == name {
			pids = append(pids, pid)
		}
	}
	return pids
}

// foregroundPIDs lists the processes behind a foreground name. For external
// ports that is everything the general launcher script started, leaving the
// script itself so the frontend's launch script runs its cleanup.
func (p *Platform) foregroundPIDs(name string) []int {
	if name != externalForeground {
		return p.pidsOf(name)
	}
	fs := p.filesystem()
	entries, err := afero.ReadDir(fs, p.procDir())
	if err != nil {
		return nil
	}
	self := p.selfPID()
	parents := make(map[int]int, len(entries))
	var roots []int
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 0 || pid == self || !e.IsDir() {
			continue
		}
		dir := filepath.Join(p.procDir(), e.Name())
		if procZombie(fs, dir) {
			continue
		}
		parents[pid] = procParent(fs, dir)
		if procRunsScript(fs, dir, externalLauncher) {
			roots = append(roots, pid)
		}
	}
	if len(roots) == 0 {
		return nil
	}

	inTree := make(map[int]bool, len(roots))
	for _, root := range roots {
		inTree[root] = true
	}
	var pids []int
	for pid := range parents {
		for ancestor, seen := parents[pid], 0; ancestor > 1 && seen < len(parents); seen++ {
			if inTree[ancestor] && !inTree[pid] {
				pids = append(pids, pid)
				break
			}
			ancestor = parents[ancestor]
		}
	}
	slices.Sort(pids)
	return pids
}

// procParent returns a process's parent pid, or 0 if it cannot be read.
func procParent(fs afero.Fs, dir string) int {
	stat := readFirstLine(fs, filepath.Join(dir, "stat"))
	end := strings.LastIndexByte(stat, ')')
	if end < 0 {
		return 0
	}
	fields := strings.Fields(stat[end+1:])
	if len(fields) < 2 {
		return 0
	}
	ppid, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0
	}
	return ppid
}

// procRunsScript reports whether a process is a shell running script.
func procRunsScript(fs afero.Fs, dir, script string) bool {
	data, err := readLimited(fs, filepath.Join(dir, "cmdline"), maxSmallFileSize)
	if err != nil {
		return false
	}
	args := strings.Split(strings.TrimRight(string(data), "\x00"), "\x00")
	return args[0] == script || (len(args) > 1 && args[1] == script)
}

func procZombie(fs afero.Fs, dir string) bool {
	stat := readFirstLine(fs, filepath.Join(dir, "stat"))
	end := strings.LastIndexByte(stat, ')')
	if end < 0 {
		return stat == ""
	}
	fields := strings.Fields(stat[end+1:])
	return len(fields) > 0 && (fields[0] == "Z" || fields[0] == "X")
}

func procProgram(fs afero.Fs, dir string) string {
	data, err := readLimited(fs, filepath.Join(dir, "cmdline"), maxSmallFileSize)
	if err != nil || len(data) == 0 {
		return ""
	}
	argv0, _, _ := strings.Cut(string(data), "\x00")
	return filepath.Base(argv0)
}

func (p *Platform) signalAll(pids []int, sig syscall.Signal) {
	for _, pid := range pids {
		if err := p.sendSignal(pid, sig); err != nil && !errors.Is(err, syscall.ESRCH) {
			log.Debug().Err(err).Int("pid", pid).Stringer("signal", sig).Msg("signal failed")
		}
	}
}

func (p *Platform) waitGone(name string, polls int) bool {
	for range polls {
		if len(p.foregroundPIDs(name)) == 0 {
			return true
		}
		p.pause(closePollStep)
	}
	return len(p.foregroundPIDs(name)) == 0
}

// closeProcess ends a foreground process the way the frontend's quit
// script does: resume it in case it is paused, ask it to exit, then force
// it if it does not.
func (p *Platform) closeProcess(name string) error {
	pids := p.foregroundPIDs(name)
	if len(pids) == 0 {
		return nil
	}
	log.Info().Str("process", name).Ints("pids", pids).Msg("closing muOS foreground process")

	p.signalAll(pids, syscall.SIGCONT)
	p.pause(closePollStep)
	p.signalAll(p.foregroundPIDs(name), syscall.SIGTERM)
	if p.waitGone(name, closeTermPolls) {
		return nil
	}

	log.Warn().Str("process", name).Msg("process ignored SIGTERM, killing it")
	p.signalAll(p.foregroundPIDs(name), syscall.SIGKILL)
	if p.waitGone(name, closeKillPolls) {
		return nil
	}
	return fmt.Errorf("%w: %s is still running", platforms.ErrStopFailed, name)
}
