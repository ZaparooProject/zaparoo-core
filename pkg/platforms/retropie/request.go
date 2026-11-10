//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package retropie

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/afero"
)

const (
	requestFile = "request"
	holdFile    = "hold"
	loopPIDFile = "loop.pid"
	stateFile   = "state"
)

// systemNamePattern matches RetroPie system names. The autostart block checks
// the same pattern, since runcommand builds config paths from the name.
var systemNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_.+-]*$`)

func validateSystemName(system string) error {
	if !systemNamePattern.MatchString(system) {
		return fmt.Errorf("invalid RetroPie system name: %q", system)
	}
	return nil
}

// validateROMPath rejects paths runcommand cannot pass through safely.
// runcommand substitutes the ROM into its emulator command inside double
// quotes and evals it, escaping only "$", so a quote, backtick or backslash
// in the path would be run as shell code.
func validateROMPath(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("ROM path must be absolute: %s", path)
	}
	for _, r := range path {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("ROM path contains a control character: %q", path)
		}
	}
	if strings.ContainsAny(path, "\"`\\") {
		return fmt.Errorf("RetroPie cannot launch a ROM path containing a quote, backtick or backslash: %s", path)
	}
	return nil
}

// encodeRequest builds a launch request: kind, system and ROM path, each
// terminated by NUL so no field can carry another.
func encodeRequest(kind requestKind, system, rom string) ([]byte, error) {
	if kind != requestSystem && kind != requestPort {
		return nil, fmt.Errorf("invalid request kind: %q", kind)
	}
	if err := validateSystemName(system); err != nil {
		return nil, err
	}
	if err := validateROMPath(rom); err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(kind)+len(system)+len(rom)+3)
	for _, field := range []string{string(kind), system, rom} {
		out = append(out, field...)
		out = append(out, 0)
	}
	return out, nil
}

func writeRequest(fs afero.Fs, stateDir string, data []byte) error {
	if err := fs.MkdirAll(stateDir, 0o750); err != nil {
		return fmt.Errorf("create %s: %w", stateDir, err)
	}
	return writeFileReplacing(fs, filepath.Join(stateDir, requestFile), data, 0o600)
}

func removeIfExists(fs afero.Fs, path string) error {
	if err := fs.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
}

func fileExists(fs afero.Fs, path string) bool {
	_, err := fs.Stat(path)
	return err == nil
}

// readLoopPID returns the pid of the autostart shell running the launch
// loop, if one is running. The pid file survives reboots, so the process
// must also still be the autostart script.
func readLoopPID(fs afero.Fs, procs procOps, stateDir string) (int, bool) {
	data, err := afero.ReadFile(fs, filepath.Join(stateDir, loopPIDFile))
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	if !procs.alive(pid) || !strings.Contains(procs.cmdline(pid), autostartFile) {
		return 0, false
	}
	return pid, true
}
