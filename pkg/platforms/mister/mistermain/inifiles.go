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
	"os"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/pkg/platforms/mister/config"
	"github.com/rs/zerolog/log"
)

type INIFile struct {
	DisplayName string
	Filename    string
	Path        string
	ID          int
}

func GetAllINIFiles() ([]INIFile, error) {
	var inis []INIFile

	files, err := os.ReadDir(config.SDRootDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	var iniFilenames []string

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		if filepath.Ext(strings.ToLower(file.Name())) == ".ini" {
			iniFilenames = append(iniFilenames, file.Name())
		}
	}

	currentID := 1

	for _, filename := range iniFilenames {
		lower := strings.ToLower(filename)

		if strings.EqualFold(lower, config.DefaultIniFilename) {
			inis = append(inis, INIFile{
				ID:          currentID,
				DisplayName: "Main",
				Filename:    filename,
				Path:        filepath.Join(config.SDRootDir, filename),
			})

			currentID++
		} else if strings.HasPrefix(lower, "mister_") {
			iniFile := INIFile{
				ID:          currentID,
				DisplayName: "",
				Filename:    filename,
				Path:        filepath.Join(config.SDRootDir, filename),
			}

			iniFile.DisplayName = filename[7:]
			iniFile.DisplayName = strings.TrimSuffix(iniFile.DisplayName, filepath.Ext(iniFile.DisplayName))

			switch iniFile.DisplayName {
			case "":
				iniFile.DisplayName = " -- "
			case "alt_1":
				iniFile.DisplayName = "Alt1"
			case "alt_2":
				iniFile.DisplayName = "Alt2"
			case "alt_3":
				iniFile.DisplayName = "Alt3"
			}

			if len(iniFile.DisplayName) > 4 {
				iniFile.DisplayName = iniFile.DisplayName[0:4]
			}

			if len(inis) < 4 {
				inis = append(inis, iniFile)
			}

			currentID++
		}
	}

	log.Debug().Int("total_ini_files", len(iniFilenames)).Int("usable_ini_files", len(inis)).Msg("discovered INI files")
	return inis, nil
}
