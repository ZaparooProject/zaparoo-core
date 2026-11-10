//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package recalbox

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const (
	// launcherName is the configgen entry point ES runs for every game; the
	// emulator is its child.
	launcherName = "emulatorlauncher"
	procRoot     = "/proc"

	stopTermTimeout = 5 * time.Second
	stopKillTimeout = 2 * time.Second
	stopPollEvery   = 100 * time.Millisecond
)

type procEntry struct {
	cmdline []string
	pid     int
	ppid    int
}

func (p *Platform) scanProcs() []procEntry {
	entries, err := afero.ReadDir(p.fs, p.procDir)
	if err != nil {
		return nil
	}
	procs := make([]procEntry, 0, len(entries))
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 0 {
			continue
		}
		ppid, ok := p.parentPID(pid)
		if !ok {
			continue
		}
		procs = append(procs, procEntry{pid: pid, ppid: ppid, cmdline: p.cmdline(pid)})
	}
	return procs
}

func (p *Platform) cmdline(pid int) []string {
	data, err := afero.ReadFile(p.fs, filepath.Join(p.procDir, strconv.Itoa(pid), "cmdline"))
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimRight(string(data), "\x00"), "\x00")
}

// parentPID reads the parent PID from /proc/<pid>/stat. The comm field can
// hold spaces and parentheses, so fields are counted from the last ')'.
func (p *Platform) parentPID(pid int) (int, bool) {
	data, err := afero.ReadFile(p.fs, filepath.Join(p.procDir, strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0, false
	}
	s := string(data)
	end := strings.LastIndexByte(s, ')')
	if end < 0 {
		return 0, false
	}
	fields := strings.Fields(s[end+1:])
	if len(fields) < 2 {
		return 0, false
	}
	ppid, err := strconv.Atoi(fields[1])
	if err != nil || ppid < 0 {
		return 0, false
	}
	return ppid, true
}

// isLauncher reports whether cmdline runs emulatorlauncher: as the program
// itself, or as the script a python interpreter runs. Other arguments are
// not checked, so an editor or grep naming the file is not a launcher.
func isLauncher(cmdline []string) bool {
	if len(cmdline) == 0 {
		return false
	}
	prog := filepath.Base(cmdline[0])
	if strings.HasPrefix(prog, launcherName) {
		return true
	}
	if !strings.HasPrefix(prog, "python") || len(cmdline) < 2 {
		return false
	}
	return strings.HasPrefix(filepath.Base(cmdline[1]), launcherName)
}

// findLaunchers returns the PIDs of running emulatorlauncher processes.
func (p *Platform) findLaunchers() []int {
	var pids []int
	self := os.Getpid()
	for _, proc := range p.scanProcs() {
		if proc.pid <= 1 || proc.pid == self {
			continue
		}
		if isLauncher(proc.cmdline) {
			pids = append(pids, proc.pid)
		}
	}
	return pids
}

// descendantsOf lists every process below roots, deepest first.
func descendantsOf(procs []procEntry, roots []int) []int {
	children := make(map[int][]int, len(procs))
	for _, proc := range procs {
		children[proc.ppid] = append(children[proc.ppid], proc.pid)
	}
	var result []int
	seen := map[int]struct{}{}
	var walk func(pid int)
	walk = func(pid int) {
		for _, child := range children[pid] {
			if _, ok := seen[child]; ok {
				continue
			}
			seen[child] = struct{}{}
			walk(child)
			result = append(result, child)
		}
	}
	for _, root := range roots {
		seen[root] = struct{}{}
	}
	for _, root := range roots {
		walk(root)
	}
	return result
}

func (p *Platform) alive(pid int) bool {
	_, err := p.fs.Stat(filepath.Join(p.procDir, strconv.Itoa(pid)))
	return err == nil
}

func (p *Platform) anyAlive(pids []int) bool {
	for _, pid := range pids {
		if p.alive(pid) {
			return true
		}
	}
	return false
}

func (p *Platform) waitGone(pids []int, timeout time.Duration) bool {
	deadline := p.clock.Now().Add(timeout)
	for p.anyAlive(pids) {
		if !p.clock.Now().Before(deadline) {
			return false
		}
		p.clock.Sleep(stopPollEvery)
	}
	return true
}

func (p *Platform) signalAll(pids []int, sig syscall.Signal) {
	for _, pid := range pids {
		if err := p.signal(pid, sig); err != nil && !errors.Is(err, syscall.ESRCH) {
			log.Debug().Err(err).Int("pid", pid).Str("signal", sig.String()).Msg("failed to signal process")
		}
	}
}

// stopGame ends the game ES is running. The emulator is asked to quit first
// so configgen can restore the display when it returns; whatever is left
// after stopTermTimeout is killed.
func (p *Platform) stopGame() error {
	procs := p.scanProcs()
	var launchers []int
	for _, proc := range procs {
		if isLauncher(proc.cmdline) {
			launchers = append(launchers, proc.pid)
		}
	}
	if len(launchers) == 0 {
		return nil
	}
	children := descendantsOf(procs, launchers)
	log.Info().Ints("launchers", launchers).Ints("children", children).Msg("stopping running game")

	if len(children) > 0 {
		p.signalAll(children, syscall.SIGTERM)
	} else {
		p.signalAll(launchers, syscall.SIGTERM)
	}
	if p.waitGone(launchers, stopTermTimeout) {
		return nil
	}

	log.Debug().Msg("game did not exit after SIGTERM, sending SIGKILL")
	all := append(descendantsOf(p.scanProcs(), launchers), launchers...)
	p.signalAll(all, syscall.SIGKILL)
	if p.waitGone(launchers, stopKillTimeout) {
		return nil
	}
	return fmt.Errorf("%w: emulatorlauncher is still running", platforms.ErrStopFailed)
}

func killProcess(pid int, sig syscall.Signal) error {
	if err := syscall.Kill(pid, sig); err != nil {
		return fmt.Errorf("kill %d: %w", pid, err)
	}
	return nil
}
