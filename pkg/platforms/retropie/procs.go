//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package retropie

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// esComm is EmulationStation's process name as the kernel truncates it.
const esComm = "emulationstatio"

// procOps is the process boundary: finding EmulationStation and runcommand
// processes and signalling them.
type procOps interface {
	alive(pid int) bool
	cmdline(pid int) string
	descendants(pid int) []int
	findByComm(comm string) []int
	signal(pid int, sig syscall.Signal) error
}

type procInfo struct {
	comm string
	ppid int
}

// procFS reads process state from a /proc tree.
type procFS struct {
	root string
}

func (p procFS) dir() string {
	if p.root == "" {
		return "/proc"
	}
	return p.root
}

func (p procFS) alive(pid int) bool {
	_, ok := p.stat(pid)
	// A zombie has exited; only its parent's wait is missing.
	return ok && !p.zombie(pid)
}

func (p procFS) zombie(pid int) bool {
	data, err := os.ReadFile(filepath.Join(p.dir(), strconv.Itoa(pid), "stat"))
	if err != nil {
		return false
	}
	end := bytes.LastIndexByte(data, ')')
	if end < 0 || end+2 >= len(data) {
		return false
	}
	return data[end+2] == 'Z'
}

func (p procFS) cmdline(pid int) string {
	data, err := os.ReadFile(filepath.Join(p.dir(), strconv.Itoa(pid), "cmdline"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(bytes.ReplaceAll(data, []byte{0}, []byte{' '})))
}

// stat parses comm and ppid from /proc/<pid>/stat. comm is the text between
// the first "(" and the last ")", since a process name may contain either.
func (p procFS) stat(pid int) (procInfo, bool) {
	data, err := os.ReadFile(filepath.Join(p.dir(), strconv.Itoa(pid), "stat"))
	if err != nil {
		return procInfo{}, false
	}
	start := bytes.IndexByte(data, '(')
	end := bytes.LastIndexByte(data, ')')
	if start < 0 || end <= start {
		return procInfo{}, false
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) < 2 {
		return procInfo{}, false
	}
	ppid, err := strconv.Atoi(fields[1])
	if err != nil {
		return procInfo{}, false
	}
	return procInfo{comm: string(data[start+1 : end]), ppid: ppid}, true
}

func (p procFS) all() map[int]procInfo {
	entries, err := os.ReadDir(p.dir())
	if err != nil {
		return nil
	}
	procs := make(map[int]procInfo, len(entries))
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}
		if info, ok := p.stat(pid); ok {
			procs[pid] = info
		}
	}
	return procs
}

func (p procFS) descendants(pid int) []int {
	return descendantsOf(p.all(), pid)
}

func descendantsOf(procs map[int]procInfo, pid int) []int {
	children := make(map[int][]int, len(procs))
	for child, info := range procs {
		children[info.ppid] = append(children[info.ppid], child)
	}
	var out []int
	queue := []int{pid}
	seen := map[int]bool{pid: true}
	for len(queue) > 0 {
		next := queue[0]
		queue = queue[1:]
		for _, child := range children[next] {
			if seen[child] {
				continue
			}
			seen[child] = true
			out = append(out, child)
			queue = append(queue, child)
		}
	}
	return out
}

func (p procFS) findByComm(comm string) []int {
	var pids []int
	for pid, info := range p.all() {
		if info.comm == comm && !p.zombie(pid) {
			pids = append(pids, pid)
		}
	}
	return pids
}

func (procFS) signal(pid int, sig syscall.Signal) error {
	if err := syscall.Kill(pid, sig); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("signal %d to pid %d: %w", sig, pid, err)
	}
	return nil
}
