//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package recalbox

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/pathutil"
	"github.com/spf13/afero"
)

const (
	// ScriptPath is a permanent user script: ES runs it once when it starts
	// and leaves it running across frontend restarts.
	ScriptPath = "/recalbox/share/userscripts/zaparoo(permanent).sh"
	// SupervisorPIDFile is written by the script so it runs only once.
	SupervisorPIDFile = "/tmp/zaparoo-supervisor.pid"

	scriptExecPlaceholder = "@ZAPAROO_BIN@"
	maxPIDFileSize        = 32

	supervisorStopTimeout = 10 * time.Second
	supervisorStopPoll    = 200 * time.Millisecond
)

// Installer installs Core as a Recalbox user script.
type Installer struct {
	FS afero.Fs
	// StartScript runs the installed script detached from the caller.
	StartScript func(path string) error
	// Signal sends a signal to a process.
	Signal func(pid int, sig syscall.Signal) error
	// Sleep waits between checks that a stopped supervisor has exited.
	Sleep func(time.Duration)
	// Script is the supervisor template; it names @ZAPAROO_BIN@.
	Script string
	// Executable is the absolute path of the binary the script runs.
	Executable string
}

// NewInstaller returns an Installer for the real system.
func NewInstaller(script, executable string) *Installer {
	return &Installer{
		FS:          afero.NewOsFs(),
		StartScript: startDetached,
		Signal:      killProcess,
		Sleep:       time.Sleep,
		Script:      script,
		Executable:  executable,
	}
}

func renderScript(template, executable string) (string, error) {
	if !filepath.IsAbs(executable) {
		return "", fmt.Errorf("executable path must be absolute: %q", executable)
	}
	if strings.ContainsFunc(executable, func(r rune) bool {
		return r < 0x20 || r == 0x7f || strings.ContainsRune(" \t\"'`\\%$;&|<>()*?[]{}!#~", r)
	}) {
		return "", fmt.Errorf("install Zaparoo to a path without spaces or special characters: %q", executable)
	}
	if !strings.Contains(template, scriptExecPlaceholder) {
		return "", errors.New("script template has no executable placeholder")
	}
	return strings.ReplaceAll(template, scriptExecPlaceholder, executable), nil
}

// Install writes the user script and starts it, so Core runs now and on
// every boot. Running it again stops the running supervisor first, so the
// reinstalled binary and script take over instead of waiting for a reboot.
func (i *Installer) Install() error {
	script, err := renderScript(i.Script, i.Executable)
	if err != nil {
		return err
	}
	if err := i.stopSupervisor(); err != nil {
		return err
	}
	if err := i.FS.MkdirAll(filepath.Dir(ScriptPath), 0o755); err != nil {
		return fmt.Errorf("create user script directory: %w", err)
	}
	if err := pathutil.WriteFileAtomic(i.FS, ScriptPath, []byte(script), 0o755); err != nil {
		return fmt.Errorf("write user script: %w", err)
	}
	if err := i.StartScript(ScriptPath); err != nil {
		return fmt.Errorf("start user script: %w", err)
	}
	return nil
}

// Uninstall stops the supervisor, which stops Core, and removes the script.
func (i *Installer) Uninstall() error {
	if err := i.stopSupervisor(); err != nil {
		return err
	}
	if err := i.FS.Remove(ScriptPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove user script: %w", err)
	}
	return nil
}

// stopSupervisor stops a running supervisor, which stops Core, and waits for
// it to exit. The supervisor removes its PID file on the way out, and a new
// one started before then would see the old one and exit at once.
func (i *Installer) stopSupervisor() error {
	pid, ok := i.supervisorPID()
	if !ok {
		return nil
	}
	if err := i.Signal(pid, syscall.SIGTERM); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return fmt.Errorf("stop Zaparoo supervisor: %w", err)
	}
	sleep := i.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	for waited := time.Duration(0); waited < supervisorStopTimeout; waited += supervisorStopPoll {
		if _, running := i.supervisorPID(); !running {
			return nil
		}
		sleep(supervisorStopPoll)
	}
	if _, running := i.supervisorPID(); running {
		return fmt.Errorf("zaparoo supervisor (pid %d) did not exit after %s", pid, supervisorStopTimeout)
	}
	return nil
}

func (i *Installer) supervisorPID() (int, bool) {
	data, err := afero.ReadFile(i.FS, SupervisorPIDFile)
	if err != nil || len(data) > maxPIDFileSize {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 1 {
		return 0, false
	}
	cmdline, err := afero.ReadFile(i.FS, filepath.Join(procRoot, strconv.Itoa(pid), "cmdline"))
	if err != nil || !strings.Contains(string(cmdline), filepath.Base(ScriptPath)) {
		return 0, false
	}
	return pid, true
}

func startDetached(path string) error {
	cmd := exec.Command("/bin/sh", path) //nolint:gosec,noctx // fixed script path; outlives the installer
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open %s: %w", os.DevNull, err)
	}
	defer func() { _ = devNull.Close() }()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = devNull, devNull, devNull
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("run %s: %w", path, err)
	}
	if err := cmd.Process.Release(); err != nil {
		return fmt.Errorf("release %s: %w", path, err)
	}
	return nil
}
