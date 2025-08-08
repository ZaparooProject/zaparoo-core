// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

package mistermain

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type RecentEntry struct {
	Directory string
	Name      string
	Label     string
}

func ReadRecent(path string) ([]RecentEntry, error) {
	recents := make([]RecentEntry, 0, 10)

	cleanPath := filepath.Clean(path)
	if _, err := os.Stat(cleanPath); err != nil {
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	file, err := os.Open(cleanPath) // #nosec G304 -- Reading trusted MiSTer recent files
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}

	for {
		entry := make([]byte, 1024+256+256)
		n, err := file.Read(entry)
		if err == io.EOF || n == 0 {
			break
		} else if err != nil {
			return nil, fmt.Errorf("failed to read file: %w", err)
		}

		empty := true
		for _, b := range entry {
			if b != 0 {
				empty = false
			}
		}
		if empty {
			break
		}

		recents = append(recents, RecentEntry{
			Directory: strings.Trim(string(entry[:1024]), "\x00"),
			Name:      strings.Trim(string(entry[1024:1280]), "\x00"),
			Label:     strings.Trim(string(entry[1280:1536]), "\x00"),
		})
	}

	return recents, nil
}
