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

package mgls

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	s "strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	misterconfig "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/cores"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/tracker/activegame"
	"github.com/rs/zerolog/log"
)

// MRA represents the structure of a MiSTer Arcade ROM file.
type MRA struct {
	XMLName xml.Name `xml:"misterromdescription"`
	SetName string   `xml:"setname"`
	Name    string   `xml:"name"`
	Rbf     string   `xml:"rbf"`
}

// ReadMRA parses an MRA file and returns the MRA struct with extracted metadata.
func ReadMRA(path string) (MRA, error) {
	var mra MRA

	// Check if file exists
	if _, err := os.Stat(path); err != nil {
		return mra, fmt.Errorf("failed to stat MRA file: %w", err)
	}

	// Read the file
	data, err := os.ReadFile(path) //nolint:gosec // Path is validated MRA file from user's media directory
	if err != nil {
		return mra, fmt.Errorf("failed to read MRA file: %w", err)
	}

	// Parse XML
	err = xml.Unmarshal(data, &mra)
	if err != nil {
		return mra, fmt.Errorf("failed to parse MRA XML: %w", err)
	}

	return mra, nil
}

func GenerateMgl(core *cores.Core, path, override string) (string, error) {
	if core == nil {
		return "", errors.New("no core supplied for MGL generation")
	}

	rbfPath := cores.ResolveRBFPathForLauncher(core.LauncherID, core.ID, core.RBF)
	mgl := fmt.Sprintf("<mistergamedescription>\n\t<rbf>%s</rbf>\n", rbfPath)

	if core.SetName != "" {
		sameDir := ""
		if core.SetNameSameDir {
			sameDir = " same_dir=\"1\""
		}

		mgl += fmt.Sprintf("\t<setname%s>%s</setname>\n", sameDir, core.SetName)
	}

	if path == "" {
		mgl += "</mistergamedescription>"
		return mgl, nil
	} else if override != "" {
		mgl += override
		mgl += "</mistergamedescription>"
		return mgl, nil
	}

	mglDef, err := cores.PathToMGLDef(core, path)
	if err != nil {
		return "", fmt.Errorf("failed to get MGL definition: %w", err)
	}

	mgl += fmt.Sprintf(
		"\t<file delay=\"%d\" type=%q index=\"%d\" path=\"../../../../..%s\"/>\n",
		mglDef.Delay, mglDef.Method, mglDef.Index, path,
	)

	if mglDef.ResetDelay > 0 {
		mgl += fmt.Sprintf("\t<reset delay=\"%d\" hold=\"%d\"/>\n",
			mglDef.ResetDelay, mglDef.ResetHold)
	}

	mgl += "</mistergamedescription>"
	return mgl, nil
}

func writeTempFile(content string) (string, error) {
	tmpFile, err := os.Create(misterconfig.LastLaunchFile)
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer func() {
		if closeErr := tmpFile.Close(); closeErr != nil {
			log.Error().Err(closeErr).Msg("failed to close temp MGL file")
		}
	}()

	_, err = tmpFile.WriteString(content)
	if err != nil {
		return "", fmt.Errorf("failed to write to temp file: %w", err)
	}
	return tmpFile.Name(), nil
}

func launchFile(path string) error {
	_, err := os.Stat(misterconfig.CmdInterface)
	if err != nil {
		return fmt.Errorf("command interface not accessible: %w", err)
	}

	lowerPath := s.ToLower(path)
	if !s.HasSuffix(lowerPath, ".mgl") && !s.HasSuffix(lowerPath, ".mra") && !s.HasSuffix(lowerPath, ".rbf") {
		return fmt.Errorf("not a valid launch file: %s", path)
	}

	log.Debug().Str("file", path).Msg("sending to command interface")
	cmd, err := os.OpenFile(misterconfig.CmdInterface, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("failed to open command interface: %w", err)
	}
	defer func() {
		if err := cmd.Close(); err != nil {
			log.Error().Err(err).Msg("failed to close command interface")
		}
	}()

	if _, err := fmt.Fprintf(cmd, "load_core %s\n", path); err != nil {
		return fmt.Errorf("failed to write to command interface: %w", err)
	}

	return nil
}

func launchTempMgl(cfg *config.Instance, system *cores.Core, path string) error {
	override, err := cores.RunSystemHook(cfg, system, path)
	if err != nil {
		return fmt.Errorf("failed to run system hook: %w", err)
	}
	if override != "" {
		log.Debug().Str("system", system.ID).Str("hook_result", override).Msg("system hook executed")
	}

	mgl, err := GenerateMgl(system, path, override)
	if err != nil {
		return fmt.Errorf("failed to generate MGL: %w", err)
	}
	log.Debug().Str("system", system.ID).Str("rbf", system.RBF).Msg("MGL generated successfully")

	tmpFile, err := writeTempFile(mgl)
	if err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	return launchFile(tmpFile)
}

// LaunchShortCore attempts to launch a core with a short path, as per what's
// allowed in an MGL file.
func LaunchShortCore(path string) error {
	mgl := fmt.Sprintf(
		"<mistergamedescription>\n\t<rbf>%s</rbf>\n</mistergamedescription>\n",
		path,
	)

	tmpFile, err := writeTempFile(mgl)
	if err != nil {
		return fmt.Errorf("failed to write to command interface: %w", err)
	}

	return launchFile(tmpFile)
}

func LaunchGame(cfg *config.Instance, system *cores.Core, path string) error {
	ext := s.ToLower(filepath.Ext(path))
	log.Info().Str("system", system.ID).Str("path", path).Str("type", ext).Msg("launching game")

	switch ext {
	case ".mra":
		err := launchFile(path)
		if err != nil {
			return fmt.Errorf("failed to write to command interface: %w", err)
		}
		log.Debug().Str("path", path).Msg("arcade game launched via MRA")
	case ".mgl":
		err := launchFile(path)
		if err != nil {
			return fmt.Errorf("failed to write to command interface: %w", err)
		}
		log.Debug().Str("path", path).Msg("game launched via MGL file")

		if activegame.ActiveGameEnabled() {
			if err := activegame.SetActiveGame(path); err != nil {
				log.Error().Err(err).Str("path", path).Msg("failed to set active game")
			}
		}
	default:
		err := launchTempMgl(cfg, system, path)
		if err != nil {
			return fmt.Errorf("failed to write to command interface: %w", err)
		}
		log.Debug().Str("system", system.ID).Str("path", path).Msg("game launched via generated MGL")

		if activegame.ActiveGameEnabled() {
			if err := activegame.SetActiveGame(path); err != nil {
				log.Error().Err(err).Str("path", path).Msg("failed to set active game")
			}
		}
	}

	return nil
}

// LaunchCore Launch a core given a possibly partial path, as per MGL files.
func LaunchCore(cfg *config.Instance, _ platforms.Platform, system *cores.Core) error {
	if _, err := os.Stat(misterconfig.CmdInterface); err != nil {
		return fmt.Errorf("command interface not accessible: %w", err)
	}

	if system.SetName != "" {
		return LaunchGame(cfg, system, "")
	}

	rbfInfo, ok := cores.GlobalRBFCache.GetBySystemID(system.ID)
	if !ok {
		return fmt.Errorf("no core found for system %s (not in cache)", system.ID)
	}
	path := rbfInfo.Path

	cmd, err := os.OpenFile(misterconfig.CmdInterface, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("failed to open command interface: %w", err)
	}
	defer func() {
		if err := cmd.Close(); err != nil {
			log.Error().Err(err).Msg("failed to close command interface")
		}
	}()

	if _, err := fmt.Fprintf(cmd, "load_core %s\n", path); err != nil {
		return fmt.Errorf("failed to write to command interface: %w", err)
	}

	return nil
}

func LaunchBasicFile(path string) error {
	var err error
	isGame := false
	ext := s.ToLower(filepath.Ext(path))
	switch ext {
	case ".mra":
		err = launchFile(path)
		if err != nil {
			return fmt.Errorf("failed to write to command interface: %w", err)
		}
	case ".mgl":
		err = launchFile(path)
		if err != nil {
			return fmt.Errorf("failed to write to command interface: %w", err)
		}
		isGame = true
	case ".rbf":
		err = launchFile(path)
		if err != nil {
			return fmt.Errorf("failed to write to command interface: %w", err)
		}
	default:
		return fmt.Errorf("unknown file type: %s", ext)
	}

	if activegame.ActiveGameEnabled() && isGame {
		err := activegame.SetActiveGame(path)
		if err != nil {
			return fmt.Errorf("failed to set active game: %w", err)
		}
	}

	return nil
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

	cleanPath := filepath.Clean(path)
	if _, err := os.Stat(cleanPath); err != nil {
		return mgl, fmt.Errorf("failed to stat file: %w", err)
	}

	file, err := os.ReadFile(cleanPath) // #nosec G304 -- Reading trusted MGL configuration files
	if err != nil {
		return mgl, fmt.Errorf("failed to read file: %w", err)
	}

	decoder := xml.NewDecoder(bytes.NewReader(file))
	decoder.Strict = false

	err = decoder.Decode(&mgl)
	if err != nil {
		return mgl, fmt.Errorf("failed to decode MGL: %w", err)
	}

	return mgl, nil
}
