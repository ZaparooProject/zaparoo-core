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

package pinup

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
)

// ScanResults converts a library into indexable media. Every table becomes a
// popper:// virtual path keyed by GameID; the display name is what users see.
func ScanResults(lib Library) []platforms.ScanResult {
	return ScanResultsWithRoot(lib, "")
}

// ScanResultsWithRoot adds local table-file provenance when Popper provides a
// trustworthy games directory and filename.
func ScanResultsWithRoot(lib Library, installRoot string) []platforms.ScanResult {
	results := make([]platforms.ScanResult, 0, len(lib.Tables))
	for i := range lib.Tables {
		table := &lib.Tables[i]
		name := table.DisplayName()
		if name == "" || virtualpath.ContainsControlChar(name) {
			continue
		}
		emulator := lib.Emulators[table.EmulatorID]
		results = append(results, platforms.ScanResult{
			Path: TablePath(table.ID, name), Name: name,
			Source: popperMetadataSource(installRoot, &emulator, table.FileName), NoExt: true,
		})
	}
	return results
}

func popperMetadataSource(installRoot string, emulator *Emulator, fileName string) *platforms.MediaSource {
	gamesDir := filepath.Clean(strings.Trim(strings.TrimSpace(emulator.GamesDir), `"`))
	fileName = strings.Trim(strings.TrimSpace(fileName), `"`)
	if gamesDir == "." || gamesDir == "" || fileName == "" ||
		virtualpath.ContainsControlChar(gamesDir) || virtualpath.ContainsControlChar(fileName) {
		return nil
	}
	if !filepath.IsAbs(gamesDir) {
		if installRoot == "" {
			return nil
		}
		gamesDir = filepath.Join(installRoot, gamesDir)
	}
	path := filepath.Clean(fileName)
	if !filepath.IsAbs(path) {
		path = filepath.Join(gamesDir, path)
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return nil
	}
	return &platforms.MediaSource{Path: path, Root: gamesDir, Kind: platforms.MediaSourceFile}
}
