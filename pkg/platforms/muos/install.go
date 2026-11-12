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
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/afero"
)

const (
	initScriptName = "zaparoo.sh"
	userInitDir    = runDir + "/storage/init"
	// supervisorPIDFile is written by the init script so a second copy, from
	// User Init after -install already started one, exits at once.
	supervisorPIDFile = "/run/zaparoo-supervisor.pid"

	serviceStopTimeout = 10 * time.Second
)

// daemonExitConfig is the status the daemon exits with when it can never
// start in its current state, such as a database newer than the binary.
const daemonExitConfig = 78

// InitScript returns the muOS user init script that starts the Zaparoo
// daemon at boot and restarts it if it crashes.
func InitScript(exe string) (string, error) {
	if !filepath.IsAbs(exe) {
		return "", fmt.Errorf("zaparoo path is not absolute: %s", exe)
	}
	if strings.ContainsAny(exe, "\n\r\x00") {
		return "", errors.New("zaparoo path contains a control character")
	}
	quoted := shellQuote(exe)
	dir := shellQuote(filepath.Dir(exe))
	return fmt.Sprintf(`#!/bin/sh
# Starts the Zaparoo Core service. Enable User Init in muOS advanced
# settings for this to run at boot.

PIDFILE=%s
if [ -f "$PIDFILE" ]; then
	OLD=$(cat "$PIDFILE" 2>/dev/null)
	case "$(tr '\000' ' ' < "/proc/$OLD/cmdline" 2>/dev/null)" in
	*%s*) exit 0 ;;
	esac
fi
echo $$ > "$PIDFILE"
cd %s || exit 1

while :; do
	%s -daemon
	RC=$?
	{ [ "$RC" -eq 0 ] || [ "$RC" -eq %d ]; } && break
	sleep 5
done
rm -f "$PIDFILE"
`, shellQuote(supervisorPIDFile), initScriptName, dir, quoted, daemonExitConfig), nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// initDirs lists where the user init script can go, in order of
// preference: the frontend's active storage link, then the SD card.
func initDirs(fs afero.Fs) []string {
	return []string{userInitDir, filepath.Join(romMount(fs), "MUOS", "init")}
}

// Install writes the user init script and returns its path.
func Install(fs afero.Fs, exe string) (string, error) {
	script, err := InitScript(exe)
	if err != nil {
		return "", err
	}
	var lastErr error
	for _, dir := range initDirs(fs) {
		if info, statErr := fs.Stat(dir); statErr != nil || !info.IsDir() {
			lastErr = fmt.Errorf("user init folder not found: %s", dir)
			continue
		}
		path := filepath.Join(dir, initScriptName)
		if writeErr := afero.WriteFile(fs, path, []byte(script), 0o755); writeErr != nil {
			lastErr = fmt.Errorf("write %s: %w", path, writeErr)
			continue
		}
		return path, nil
	}
	return "", fmt.Errorf("installing muOS init script: %w", lastErr)
}

// Uninstall removes the user init script and stops the running service.
func (p *Platform) Uninstall(exe string) error {
	fs := p.filesystem()
	for _, dir := range initDirs(fs) {
		if err := removeIfExists(fs, filepath.Join(dir, initScriptName)); err != nil {
			return err
		}
	}
	return p.StopService(exe)
}

// StopService stops the init script's supervisor loop and running daemons
// of the given binary other than the calling process, and waits for them to
// exit. A reinstall calls it first so the new binary and script take over.
func (p *Platform) StopService(exe string) error {
	pids := p.daemonPIDs(filepath.Base(exe))
	if pid, ok := p.supervisorPID(); ok {
		pids = append(pids, pid)
	}
	for _, pid := range pids {
		if err := p.sendSignal(pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
			return fmt.Errorf("stopping zaparoo process %d: %w", pid, err)
		}
	}
	for waited := time.Duration(0); waited < serviceStopTimeout; waited += closePollStep {
		if !p.anyAlive(pids) {
			return nil
		}
		p.pause(closePollStep)
	}
	if p.anyAlive(pids) {
		return fmt.Errorf("zaparoo service did not stop within %s", serviceStopTimeout)
	}
	return nil
}

func (p *Platform) anyAlive(pids []int) bool {
	fs := p.filesystem()
	for _, pid := range pids {
		dir := filepath.Join(p.procDir(), strconv.Itoa(pid))
		if exists, err := afero.DirExists(fs, dir); err == nil && exists && !procZombie(fs, dir) {
			return true
		}
	}
	return false
}

// supervisorPID returns the init script's supervisor loop, if it is running.
func (p *Platform) supervisorPID() (int, bool) {
	fs := p.filesystem()
	data, err := readLimited(fs, supervisorPIDFile, maxSmallFileSize)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 1 || pid == p.selfPID() {
		return 0, false
	}
	cmdline, err := readLimited(fs, filepath.Join(p.procDir(), strconv.Itoa(pid), "cmdline"), maxSmallFileSize)
	if err != nil || !strings.Contains(string(cmdline), initScriptName) {
		return 0, false
	}
	return pid, true
}

// daemonPIDs finds running Zaparoo daemons by their program name and
// daemon flag.
func (p *Platform) daemonPIDs(program string) []int {
	fs := p.filesystem()
	var pids []int
	for _, pid := range p.pidsOf(program) {
		data, err := readLimited(fs, filepath.Join(p.procDir(), strconv.Itoa(pid), "cmdline"), maxSmallFileSize)
		if err != nil {
			continue
		}
		for _, arg := range strings.Split(string(data), "\x00") {
			if arg == "-daemon" || arg == "--daemon" {
				pids = append(pids, pid)
				break
			}
		}
	}
	return pids
}
