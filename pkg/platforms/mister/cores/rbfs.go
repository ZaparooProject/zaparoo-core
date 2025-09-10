//go:build linux

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

func ParseRBFPath(path string) RBFInfo {
	info := RBFInfo{
		Path:     path,
		Filename: filepath.Base(path),
	}

	if strings.Contains(info.Filename, "_") {
		info.ShortName = info.Filename[0:strings.LastIndex(info.Filename, "_")]
	} else {
		info.ShortName = strings.TrimSuffix(info.Filename, filepath.Ext(info.Filename))
	}

	if strings.HasPrefix(path, config.SDRootDir) {
		relDir := strings.TrimPrefix(filepath.Dir(path), config.SDRootDir+"/")
		info.MglName = filepath.Join(relDir, info.ShortName)
	} else {
		info.MglName = path
	}

	return info
}

// Find all rbf files in the top 2 menu levels of the SD card.
func shallowScanRBF() ([]RBFInfo, error) {
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
			newPath, err := os.Readlink(path)
			if err != nil {
				return RBFInfo{}, fmt.Errorf("failed to readlink %s: %w", path, err)
			}

			return ParseRBFPath(newPath), nil
		}
		return ParseRBFPath(path), nil
	}

	files, err := os.ReadDir(config.SDRootDir)
	if err != nil {
		return results, fmt.Errorf("failed to read SD root directory %s: %w", config.SDRootDir, err)
	}

	for _, file := range files {
		if file.IsDir() && strings.HasPrefix(file.Name(), "_") {
			subFiles, err := os.ReadDir(filepath.Join(config.SDRootDir, file.Name()))
			if err != nil {
				continue
			}

			for _, subFile := range subFiles {
				if isRbf(subFile) {
					path := filepath.Join(config.SDRootDir, file.Name(), subFile.Name())
					info, err := infoSymlink(path)
					if err != nil {
						continue
					}
					results = append(results, info)
				}
			}
		} else if isRbf(file) {
			path := filepath.Join(config.SDRootDir, file.Name())
			info, err := infoSymlink(path)
			if err != nil {
				continue
			}
			results = append(results, info)
		}
	}

	return results, nil
}

func SystemsWithRBF() map[string]RBFInfo {
	// TODO: include alt rbfs somehow?
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
