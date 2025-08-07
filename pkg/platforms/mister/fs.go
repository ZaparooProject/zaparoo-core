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

package mister

import (
	"bytes"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"strings"

	misterconfig "github.com/ZaparooProject/zaparoo-core/pkg/platforms/mister/config"
	"github.com/rs/zerolog/log"
)

func ActiveGameEnabled() bool {
	_, err := os.Stat(misterconfig.ActiveGameFile)
	return err == nil
}

func SetActiveGame(path string) error {
	file, err := os.Create(misterconfig.ActiveGameFile)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			log.Error().Err(closeErr).Msg("failed to close active game file")
		}
	}()

	_, err = file.WriteString(path)
	if err != nil {
		return err
	}

	return nil
}

func GetActiveGame() (string, error) {
	data, err := os.ReadFile(misterconfig.ActiveGameFile)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

// Convert a launchable path to an absolute path.
func ResolvePath(path string) string {
	if path == "" {
		return path
	}

	cwd, _ := os.Getwd()
	defer func() {
		if err := os.Chdir(cwd); err != nil {
			log.Error().Err(err).Str("path", cwd).Msg("failed to restore working directory")
		}
	}()
	if err := os.Chdir(misterconfig.SDRootDir); err != nil {
		return path
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}

	return abs
}

type RecentEntry struct {
	Directory string
	Name      string
	Label     string
}

func ReadRecent(path string) ([]RecentEntry, error) {
	var recents []RecentEntry

	if _, err := os.Stat(path); err != nil {
		return nil, err
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	for {
		entry := make([]byte, 1024+256+256)
		n, err := file.Read(entry)
		if err == io.EOF || n == 0 {
			break
		} else if err != nil {
			return nil, err
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

type MGLFile struct {
	XMLName xml.Name `xml:"file"`
	Type    string   `xml:"type,attr"`
	Path    string   `xml:"path,attr"`
	Delay   int      `xml:"delay,attr"`
	Index   int      `xml:"index,attr"`
}

type MGL struct {
	XMLName xml.Name `xml:"mistergamedescription"`
	Rbf     string   `xml:"rbf"`
	SetName string   `xml:"setname"`
	File    MGLFile  `xml:"file"`
}

func ReadMgl(path string) (MGL, error) {
	var mgl MGL

	if _, err := os.Stat(path); err != nil {
		return mgl, err
	}

	file, err := os.ReadFile(path)
	if err != nil {
		return mgl, err
	}

	decoder := xml.NewDecoder(bytes.NewReader(file))
	decoder.Strict = false

	err = decoder.Decode(&mgl)
	if err != nil {
		return mgl, err
	}

	return mgl, nil
}
