//go:build linux

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

package startup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/pathutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/spf13/afero"
)

type Startup struct {
	Entries []Entry
}

type Entry struct {
	Name    string
	Cmds    []string
	Enabled bool
}

func (s *Startup) Load() error {
	return s.loadFile(afero.NewOsFs(), config.StartupFile)
}

func (s *Startup) loadFile(fs afero.Fs, path string) error {
	var entries []Entry

	contents, err := afero.ReadFile(fs, path)
	if os.IsNotExist(err) {
		contents = []byte{}
	} else if err != nil {
		return fmt.Errorf("failed to read startup file %s: %w", path, err)
	}

	lines := strings.Split(string(contents), "\n")
	sections := make([][]string, 0)

	section := make([]string, 0)
	for i, line := range lines {
		if i == 0 && strings.HasPrefix(line, "#!") {
			continue
		}

		if line == "" && len(section) != 0 {
			sections = append(sections, section)
			section = make([]string, 0)
		} else if line != "" {
			section = append(section, line)
		}
	}

	if len(section) != 0 {
		sections = append(sections, section)
	}

	for _, section := range sections {
		name := ""
		cmds := make([]string, 0)
		enabled := false

		if section[0] != "" && section[0][0] == '#' {
			name = strings.TrimSpace(section[0][1:])
			cmds = append(cmds, section[1:]...)
		} else {
			cmds = append(cmds, section...)
		}

		for _, line := range cmds {
			if line != "" && line[0] != '#' {
				enabled = true
				break
			}
		}

		if len(cmds) != 0 {
			entries = append(entries, Entry{
				Name:    name,
				Enabled: enabled,
				Cmds:    cmds,
			})
		}
	}

	s.Entries = entries

	return nil
}

func (s *Startup) Save() error {
	return s.saveFile(afero.NewOsFs(), config.StartupFile)
}

func (s *Startup) saveFile(fs afero.Fs, path string) error {
	if len(s.Entries) == 0 {
		return errors.New("no startup entries to save")
	}

	var contents strings.Builder
	_, _ = contents.WriteString("#!/bin/sh\n\n")

	for _, entry := range s.Entries {
		if entry.Name != "" {
			_, _ = contents.WriteString("# " + entry.Name + "\n")
		}

		for _, cmd := range entry.Cmds {
			_, _ = contents.WriteString(cmd + "\n")
		}

		_, _ = contents.WriteString("\n")
	}

	// Ensure parent directory exists before writing
	dir := filepath.Dir(path)
	err := fs.MkdirAll(dir, 0o750)
	if err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	mode := os.FileMode(0o644)
	if info, statErr := fs.Stat(path); statErr == nil {
		mode = info.Mode().Perm()
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return fmt.Errorf("failed to stat startup file: %w", statErr)
	}
	err = pathutil.WriteFileAtomic(fs, path, []byte(contents.String()), mode)
	if err != nil {
		return fmt.Errorf("failed to write startup file %s: %w", path, err)
	}
	return nil
}

func (s *Startup) Exists(name string) bool {
	for _, entry := range s.Entries {
		if entry.Name == name {
			return true
		}
	}

	return false
}

func (s *Startup) Enable(name string) error {
	for i, entry := range s.Entries {
		if entry.Name == name && !entry.Enabled {
			s.Entries[i].Enabled = true
			for j, cmd := range entry.Cmds {
				if cmd != "" && cmd[0] == '#' {
					s.Entries[i].Cmds[j] = cmd[1:]
				}
			}

			return nil
		}
	}

	return fmt.Errorf("startup entry not found: %s", name)
}

func (s *Startup) Add(name, cmd string) error {
	if s.Exists(name) {
		return fmt.Errorf("startup entry already exists: %s", name)
	}

	s.Entries = append(s.Entries, Entry{
		Name:    name,
		Enabled: true,
		Cmds:    strings.Split(cmd, "\n"),
	})

	return nil
}

func (s *Startup) AddService(name string) error {
	path, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	cmd := fmt.Sprintf("[[ -e %s ]] && %s -service $1", path, path)

	return s.Add(name, cmd)
}

func (s *Startup) Remove(name string) error {
	for i, entry := range s.Entries {
		if entry.Name == name {
			s.Entries = append(s.Entries[:i], s.Entries[i+1:]...)
			return nil
		}
	}

	return fmt.Errorf("startup entry not found: %s", name)
}
