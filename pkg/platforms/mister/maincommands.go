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

package mister

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"

	misterconfig "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/spf13/afero"
)

const (
	mainCapabilitiesVersion = "1"

	// Commands the Zaparoo Main fork lists in its capability file.
	mainCommandCheat = "cheat"
	mainCommandSave  = "save"

	// maxMainCommandArg bounds a free-text argument sent to Main. The fork
	// copies the cheat command, verb included, into a 320-byte buffer.
	maxMainCommandArg = 255
)

// mainCapabilitiesPath is written by the Zaparoo Main fork at startup. Stock
// Main ignores the fork's commands without a reply, so this file is the only
// way to tell the two apart.
var mainCapabilitiesPath = filepath.Join(string(filepath.Separator), "tmp", "zaparoo_main")

var (
	errMainCommandUnsupported = errors.New("the running MiSTer Main does not support this command")
	errMainCommandArg         = errors.New("argument cannot be sent to MiSTer Main")
)

// mainCommands sends the Zaparoo Main fork's own commands through Main's
// command interface.
type mainCommands struct {
	fs               afero.Fs
	capabilitiesPath string
	commandPath      string
}

func newMainCommands(fs afero.Fs) *mainCommands {
	return &mainCommands{
		fs:               fs,
		capabilitiesPath: mainCapabilitiesPath,
		commandPath:      misterconfig.CmdInterface,
	}
}

// supports reports whether the running Main accepts command. The capability
// file names the Main process that wrote it. Main starts a new process on
// every core load and the fork rewrites the file each time, so a live pid
// means the fork is the Main running now; a stock Main started in its place
// leaves the dead pid behind.
func (m *mainCommands) supports(command string) bool {
	contents, err := afero.ReadFile(m.fs, m.capabilitiesPath)
	if err != nil {
		return false
	}
	fields := strings.Fields(string(contents))
	if len(fields) < 2 || fields[0] != mainCapabilitiesVersion {
		return false
	}
	pid, err := strconv.Atoi(fields[1])
	if err != nil || pid <= 0 {
		return false
	}
	if _, err := m.fs.Stat(filepath.Join(string(filepath.Separator), "proc", strconv.Itoa(pid))); err != nil {
		return false
	}
	return slices.Contains(fields[2:], command)
}

// send writes one command line for a fork command Main has advertised.
func (m *mainCommands) send(command string, args ...string) error {
	if !m.supports(command) {
		return fmt.Errorf("%w: zaparoo_%s", errMainCommandUnsupported, command)
	}
	line := "zaparoo_" + command
	if len(args) > 0 {
		line += " " + strings.Join(args, " ")
	}

	cmd, err := m.fs.OpenFile(m.commandPath, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open Main command interface: %w", err)
	}
	defer func() { _ = cmd.Close() }()
	if _, err := cmd.WriteString(line + "\n"); err != nil {
		return fmt.Errorf("write Main command: %w", err)
	}
	return nil
}

// mainCommandArg checks a free-text argument from a card or API caller before
// it is put on Main's command line. Main splits commands on newlines, so any
// control character could smuggle in a second command such as load_core.
func mainCommandArg(arg string) (string, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return "", fmt.Errorf("%w: empty", errMainCommandArg)
	}
	if len(arg) > maxMainCommandArg {
		return "", fmt.Errorf("%w: longer than %d bytes", errMainCommandArg, maxMainCommandArg)
	}
	if strings.ContainsFunc(arg, unicode.IsControl) {
		return "", fmt.Errorf("%w: contains a control character", errMainCommandArg)
	}
	return arg, nil
}
