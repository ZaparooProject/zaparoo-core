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

package cores

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
)

type RBFInfo struct {
	Path      string // full path to RBF file
	Filename  string // base filename of RBF file
	ShortName string // base filename without date or extension
	MglName   string // relative path launch-able from MGL file
}

func stripOfficialDateSuffix(name string) string {
	if len(name) < 10 || name[len(name)-9] != '_' {
		return name
	}
	for _, r := range name[len(name)-8:] {
		if r < '0' || r > '9' {
			return name
		}
	}
	return name[:len(name)-9]
}

func ParseRBFPath(path string) RBFInfo {
	return parseRBFPathAt(config.SDRootDir, path)
}

func parseRBFPathAt(root, path string) RBFInfo {
	info := RBFInfo{
		Path:     path,
		Filename: filepath.Base(path),
	}

	info.ShortName = stripOfficialDateSuffix(strings.TrimSuffix(info.Filename, filepath.Ext(info.Filename)))

	if strings.HasPrefix(path, root) {
		relDir, err := filepath.Rel(root, filepath.Dir(path))
		switch {
		case err != nil || relDir == ".." || strings.HasPrefix(relDir, ".."+string(os.PathSeparator)):
			info.MglName = path
		case relDir == "." || relDir == "":
			info.MglName = info.ShortName
		default:
			info.MglName = filepath.Join(relDir, info.ShortName)
		}
	} else {
		info.MglName = path
	}

	return info
}

// Find all rbf files in the top 2 menu levels of the SD card, plus
// RetroAchievements cores under _RA_Cores/Cores.
func shallowScanRBF() ([]RBFInfo, error) {
	return shallowScanRBFAt(config.SDRootDir)
}

func shallowScanRBFAt(root string) ([]RBFInfo, error) {
	results := make([]RBFInfo, 0)

	isRbf := func(file os.DirEntry) bool {
		return filepath.Ext(strings.ToLower(file.Name())) == ".rbf"
	}

	infoSymlink := func(path string) (RBFInfo, error) {
		info, err := os.Lstat(path)
		if err != nil {
			return RBFInfo{}, fmt.Errorf("failed to lstat %s: %w", path, err)
		}

		if info.Mode()&os.ModeSymlink != 0 {
			newPath, readlinkErr := os.Readlink(path)
			if readlinkErr != nil {
				return RBFInfo{}, fmt.Errorf("failed to readlink %s: %w", path, readlinkErr)
			}

			return parseRBFPathAt(root, newPath), nil
		}
		return parseRBFPathAt(root, path), nil
	}

	addRBF := func(path string) {
		info, err := infoSymlink(path)
		if err != nil {
			return
		}
		results = append(results, info)
	}

	files, err := os.ReadDir(root)
	if err != nil {
		return results, fmt.Errorf("failed to read SD root directory %s: %w", root, err)
	}

	for _, file := range files {
		if file.IsDir() && strings.HasPrefix(file.Name(), "_") {
			subFiles, subErr := os.ReadDir(filepath.Join(root, file.Name()))
			if subErr != nil {
				continue
			}

			for _, subFile := range subFiles {
				if isRbf(subFile) {
					addRBF(filepath.Join(root, file.Name(), subFile.Name()))
				}
			}
		} else if isRbf(file) {
			addRBF(filepath.Join(root, file.Name()))
		}
	}

	raCoreDir := filepath.Join(root, "_RA_Cores", "Cores")
	raCoreFiles, raErr := os.ReadDir(raCoreDir)
	if raErr == nil {
		for _, file := range raCoreFiles {
			if isRbf(file) {
				addRBF(filepath.Join(raCoreDir, file.Name()))
			}
		}
	}

	return results, nil
}

func SystemsWithRBF() map[string]RBFInfo {
	results := make(map[string]RBFInfo)

	rbfFiles, err := shallowScanRBF()
	if err != nil {
		return results
	}

	for _, rbfFile := range rbfFiles {
		for _, system := range Systems {
			shortName := system.RBF

			if strings.Contains(shortName, "/") {
				shortName = shortName[strings.LastIndex(shortName, "/")+1:]
			}

			if strings.EqualFold(rbfFile.ShortName, shortName) {
				results[system.ID] = rbfFile
			}
		}
	}

	return results
}
