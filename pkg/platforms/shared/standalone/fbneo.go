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
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/rs/zerolog/log"
)

const maxFBNeoConfigSize = 1 << 20

var fbneoROMPathLine = regexp.MustCompile(`^szAppRomPaths\[(\d+)\][ \t]?(.*)$`)

// ensureFBNeoROMPath makes dir one of the ROM folders in FinalBurn Neo's
// config (config\<exe name>.ini next to the executable). A slot that is empty
// or names a folder that does not exist is reused, so folders the user set up
// are never replaced.
func ensureFBNeoROMPath(exe, dir string) error {
	installDir := filepath.Dir(exe)
	name := strings.TrimSuffix(filepath.Base(exe), filepath.Ext(exe))
	configPath := filepath.Join(installDir, "config", name+".ini")
	manual := fmt.Errorf("add %s to FinalBurn Neo's ROM folders", dir)
	for _, r := range dir {
		if r < 0x20 || r > 0x7e {
			// FinalBurn Neo reads its config in the system code page.
			return manual
		}
	}

	data, err := readFBNeoConfig(configPath)
	if errors.Is(err, os.ErrNotExist) {
		// FinalBurn Neo fills in every setting this file leaves out.
		if mkErr := os.MkdirAll(filepath.Dir(configPath), 0o750); mkErr != nil {
			return fmt.Errorf("create FinalBurn Neo config folder: %w", mkErr)
		}
		content := "szAppRomPaths[0] roms/\r\nszAppRomPaths[1] " + dir + `\` + "\r\n"
		return writeFBNeoConfig(configPath, content)
	}
	if err != nil {
		return err
	}

	newline := "\n"
	if strings.Contains(data, "\r\n") {
		newline = "\r\n"
	}
	lines := strings.Split(strings.ReplaceAll(data, "\r\n", "\n"), "\n")
	// An empty slot is used first, then one naming a missing folder, then
	// the last of the empty placeholder folders FinalBurn Neo ships under its
	// own roms folder.
	empty, missing, placeholder := -1, -1, -1
	slots := make(map[int]string)
	for i, line := range lines {
		match := fbneoROMPathLine.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		value := strings.TrimSpace(match[2])
		slots[i] = match[1]
		if value == "" {
			if empty < 0 {
				empty = i
			}
			continue
		}
		path := filepath.FromSlash(strings.ReplaceAll(value, `\`, "/"))
		if !filepath.IsAbs(path) {
			path = filepath.Join(installDir, path)
		}
		if strings.EqualFold(filepath.Clean(path), filepath.Clean(dir)) {
			return nil
		}
		info, statErr := os.Stat(path)
		switch {
		case statErr != nil && !errors.Is(statErr, os.ErrNotExist):
			// A folder that cannot be inspected may still exist, so its slot
			// is kept.
			log.Warn().Err(statErr).Str("folder", path).
				Msg("cannot inspect FinalBurn Neo ROM folder, keeping its slot")
		case statErr != nil || !info.IsDir():
			if missing < 0 {
				missing = i
			}
		case isFBNeoPlaceholder(installDir, path):
			placeholder = i
		}
	}
	free := empty
	if free < 0 {
		free = missing
	}
	if free < 0 {
		free = placeholder
	}
	if free < 0 {
		return fmt.Errorf("FinalBurn Neo has no unused ROM folder slot: %w", manual)
	}
	lines[free] = "szAppRomPaths[" + slots[free] + "] " + dir + `\`
	log.Info().Str("config", configPath).Str("folder", dir).Str("slot", slots[free]).
		Msg("added ROM folder to FinalBurn Neo config")
	return writeFBNeoConfig(configPath, strings.Join(lines, newline))
}

// isFBNeoPlaceholder reports whether path is an empty folder directly inside
// FinalBurn Neo's roms folder, other than romdata which holds its own files.
func isFBNeoPlaceholder(installDir, path string) bool {
	parent := filepath.Join(installDir, "roms")
	if !strings.EqualFold(filepath.Dir(filepath.Clean(path)), parent) ||
		strings.EqualFold(filepath.Base(path), "romdata") {
		return false
	}
	entries, err := os.ReadDir(path)
	return err == nil && len(entries) == 0
}

func readFBNeoConfig(path string) (string, error) {
	f, err := os.Open(path) //nolint:gosec // path is built from the discovered emulator folder
	if err != nil {
		return "", fmt.Errorf("open FinalBurn Neo config: %w", err)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxFBNeoConfigSize+1))
	if err != nil {
		return "", fmt.Errorf("read FinalBurn Neo config: %w", err)
	}
	if len(data) > maxFBNeoConfigSize {
		return "", errors.New("FinalBurn Neo config is too large")
	}
	return string(data), nil
}

func writeFBNeoConfig(path, content string) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".zaparoo-fbneo-*.ini")
	if err != nil {
		return fmt.Errorf("write FinalBurn Neo config: %w", err)
	}
	tmpName := tmp.Name()
	_, err = tmp.WriteString(content)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmpName, path)
	}
	if err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("write FinalBurn Neo config: %w", err)
	}
	return nil
}
