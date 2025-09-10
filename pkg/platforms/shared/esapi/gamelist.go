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

package esapi

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/rs/zerolog/log"
)

type Game struct {
	Name string `xml:"name"`
	Path string `xml:"path"`
}

type GameList struct {
	XMLName xml.Name `xml:"gameList"`
	Games   []Game   `xml:"game"`
}

func ReadGameListXML(path string) (GameList, error) {
	// Clean and validate the path to prevent directory traversal attacks
	cleanPath := filepath.Clean(path)
	if !filepath.IsAbs(cleanPath) {
		cleanPath = filepath.Join(".", cleanPath)
	}

	xmlFile, err := os.Open(cleanPath) // #nosec G304 - path is validated above
	if err != nil {
		return GameList{}, fmt.Errorf("failed to open gamelist XML file %s: %w", cleanPath, err)
	}
	defer func(xmlFile *os.File) {
		closeErr := xmlFile.Close()
		if closeErr != nil {
			log.Warn().Err(closeErr).Msg("error closing xml file")
		}
	}(xmlFile)

	data, err := io.ReadAll(xmlFile)
	if err != nil {
		return GameList{}, fmt.Errorf("failed to read gamelist XML file %s: %w", path, err)
	}

	var gameList GameList
	err = xml.Unmarshal(data, &gameList)
	if err != nil {
		return GameList{}, fmt.Errorf("failed to unmarshal gamelist XML: %w", err)
	}

	return gameList, nil
}
