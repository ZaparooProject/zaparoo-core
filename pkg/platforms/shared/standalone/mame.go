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

package standalone

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
)

const (
	// mameDefaultROMPath is MAME's built-in rompath, relative to its working
	// directory.
	mameDefaultROMPath    = "roms"
	maxMAMEConfigSize     = 1 << 20
	mameShowConfigTimeout = 10 * time.Second
)

// MAMEArgs launches a MAME ROM set by name. The set's folder is put first in
// the rompath and the rompath MAME is configured with follows it, so BIOS,
// parent and CHD folders the user configured keep working. MAME strips the
// folder and extension from the set name itself, so only the name is passed.
func MAMEArgs(ctx *Context) ([]string, error) {
	machine, err := SetName(ctx.Path, ".zip", ".7z")
	if err != nil {
		return nil, err
	}
	gameDir := filepath.Dir(ctx.Path)
	if strings.Contains(gameDir, ";") {
		return nil, errors.New("MAME ROM folder cannot contain ';'")
	}
	configured := mameDefaultROMPath
	if ctx.MAMEConfig != nil {
		output, configErr := ctx.MAMEConfig()
		if configErr != nil {
			return nil, configErr
		}
		configured = MAMEConfiguredROMPath(output)
	}
	entries := []string{gameDir}
	for _, entry := range strings.Split(configured, ";") {
		entry = strings.TrimSpace(entry)
		if entry == "" || filepath.Clean(entry) == filepath.Clean(gameDir) {
			continue
		}
		entries = append(entries, entry)
	}
	return []string{"-rompath", strings.Join(entries, ";"), machine}, nil
}

// MAMEShowConfig runs command, which must be MAME with -showconfig, and
// returns what it prints: the settings MAME ends up with after reading every
// ini file on its inipath. Asking MAME is the only reliable way to find them,
// because builds and packages use different inipaths and defaults.
func MAMEShowConfig(command *platforms.LaunchCommand, prepare func(cmd *exec.Cmd)) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), mameShowConfigTimeout)
	defer cancel()
	//nolint:gosec // Executable comes from discovery and arguments are fixed.
	cmd := exec.CommandContext(ctx, command.Executable, command.Args...)
	cmd.Dir = command.Dir
	if len(command.Env) > 0 {
		cmd.Env = helpers.MergeEnviron(os.Environ(), command.Env)
	}
	if prepare != nil {
		prepare(cmd)
	}
	output := &limitedBuffer{limit: maxMAMEConfigSize}
	cmd.Stdout = output
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("read MAME configuration: %w", err)
	}
	return output.Bytes(), nil
}

// MAMEConfiguredROMPath returns the rompath in MAME's -showconfig output, or
// MAME's default when it is not set. The value is returned as written: MAME
// expands environment variables and resolves relative entries against its own
// working directory.
func MAMEConfiguredROMPath(showConfig []byte) string {
	value, found := mameConfigValue(bytes.NewReader(showConfig), "rompath")
	if !found || value == "" {
		return mameDefaultROMPath
	}
	return value
}

func mameConfigValue(r io.Reader, key string) (value string, found bool) {
	scanner := bufio.NewScanner(io.LimitReader(r, maxMAMEConfigSize))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || line[0] == '#' {
			continue
		}
		name, rest, ok := cutSpace(line)
		if !ok || !strings.EqualFold(name, key) {
			continue
		}
		rest = strings.TrimSpace(rest)
		if len(rest) >= 2 && rest[0] == '"' && rest[len(rest)-1] == '"' {
			rest = rest[1 : len(rest)-1]
		}
		// Later lines override earlier ones, as they do in MAME.
		value, found = rest, true
	}
	return value, found
}

// limitedBuffer keeps the first limit bytes written to it and drops the rest,
// so a misbehaving executable cannot grow Core's memory. It only implements
// Write: an io.ReaderFrom would let io.Copy bypass the limit.
type limitedBuffer struct {
	buf   bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if room := b.limit - b.buf.Len(); room > 0 {
		b.buf.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

// Bytes returns what was kept.
func (b *limitedBuffer) Bytes() []byte { return b.buf.Bytes() }

func cutSpace(line string) (before, after string, found bool) {
	index := strings.IndexAny(line, " \t")
	if index < 0 {
		return line, "", false
	}
	return line[:index], line[index+1:], true
}
