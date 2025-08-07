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
	"fmt"
	"os"
	"path/filepath"
	s "strings"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	misterconfig "github.com/ZaparooProject/zaparoo-core/pkg/platforms/mister/config"
	"github.com/rs/zerolog/log"
)

func GenerateMgl(cfg *config.Instance, system *Core, path string, override string) (string, error) {
	// override the system rbf with the user specified one
	// TODO: this needs to look up the core by launcher using the zaparoo config
	// for _, setCore := range cfg.Systems.SetCore {
	//	parts := s.SplitN(setCore, ":", 2)
	//	if len(parts) != 2 {
	//		continue
	//	}
	//
	//	if s.EqualFold(parts[0], system.ID) {
	//		system.RBF = parts[1]
	//		break
	//	}
	//}

	mgl := fmt.Sprintf("<mistergamedescription>\n\t<rbf>%s</rbf>\n", system.RBF)

	if system.SetName != "" {
		sameDir := ""
		if system.SetNameSameDir {
			sameDir = " same_dir=\"1\""
		}

		mgl += fmt.Sprintf("\t<setname%s>%s</setname>\n", sameDir, system.SetName)
	}

	if path == "" {
		mgl += "</mistergamedescription>"
		return mgl, nil
	} else if override != "" {
		mgl += override
		mgl += "</mistergamedescription>"
		return mgl, nil
	}

	mglDef, err := PathToMGLDef(system, path)
	if err != nil {
		return "", err
	}

	mgl += fmt.Sprintf("<file delay=\"%d\" type=\"%s\" index=\"%d\" path=\"../../../../..%s\"/>\n", mglDef.Delay, mglDef.Method, mglDef.Index, path)
	mgl += "</mistergamedescription>"
	return mgl, nil
}

func writeTempFile(content string) (string, error) {
	tmpFile, err := os.Create(misterconfig.LastLaunchFile)
	if err != nil {
		return "", err
	}
	defer func() {
		if closeErr := tmpFile.Close(); closeErr != nil {
			log.Error().Err(closeErr).Msg("failed to close temp MGL file")
		}
	}()

	_, err = tmpFile.WriteString(content)
	if err != nil {
		return "", err
	}
	return tmpFile.Name(), nil
}

func launchFile(path string) error {
	_, err := os.Stat(misterconfig.CmdInterface)
	if err != nil {
		return fmt.Errorf("command interface not accessible: %w", err)
	}

	if !s.HasSuffix(s.ToLower(path), ".mgl") && !s.HasSuffix(s.ToLower(path), ".mra") && !s.HasSuffix(s.ToLower(path), ".rbf") {
		return fmt.Errorf("not a valid launch file: %s", path)
	}

	cmd, err := os.OpenFile(misterconfig.CmdInterface, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer func() {
		if err := cmd.Close(); err != nil {
			log.Error().Err(err).Msg("failed to close command interface")
		}
	}()

	if _, err := fmt.Fprintf(cmd, "load_core %s\n", path); err != nil {
		return err
	}

	return nil
}

func launchTempMgl(cfg *config.Instance, system *Core, path string) error {
	override, err := RunSystemHook(cfg, system, path)
	if err != nil {
		return err
	}

	mgl, err := GenerateMgl(cfg, system, path, override)
	if err != nil {
		return err
	}

	tmpFile, err := writeTempFile(mgl)
	if err != nil {
		return err
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
		return err
	}

	return launchFile(tmpFile)
}

func LaunchGame(cfg *config.Instance, system *Core, path string) error {
	switch s.ToLower(filepath.Ext(path)) {
	case ".mra":
		err := launchFile(path)
		if err != nil {
			return err
		}
	case ".mgl":
		err := launchFile(path)
		if err != nil {
			return err
		}

		if ActiveGameEnabled() {
			if err := SetActiveGame(path); err != nil {
				log.Error().Err(err).Str("path", path).Msg("failed to set active game")
			}
		}
	default:
		err := launchTempMgl(cfg, system, path)
		if err != nil {
			return err
		}

		if ActiveGameEnabled() {
			if err := SetActiveGame(path); err != nil {
				log.Error().Err(err).Str("path", path).Msg("failed to set active game")
			}
		}
	}

	return nil
}

// LaunchCore Launch a core given a possibly partial path, as per MGL files.
func LaunchCore(cfg *config.Instance, pl platforms.Platform, system Core) error {
	if _, err := os.Stat(misterconfig.CmdInterface); err != nil {
		return fmt.Errorf("command interface not accessible: %w", err)
	}

	if system.SetName != "" {
		return LaunchGame(cfg, &system, "")
	}

	var path string
	rbfs := SystemsWithRbf()
	if _, ok := rbfs[system.ID]; ok {
		path = rbfs[system.ID].Path
	} else {
		return fmt.Errorf("no core found for system %s", system.ID)
	}

	cmd, err := os.OpenFile(misterconfig.CmdInterface, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer func() {
		if err := cmd.Close(); err != nil {
			log.Error().Err(err).Msg("failed to close command interface")
		}
	}()

	if _, err := fmt.Fprintf(cmd, "load_core %s\n", path); err != nil {
		return err
	}

	return nil
}

func LaunchMenu() error {
	if _, err := os.Stat(misterconfig.CmdInterface); err != nil {
		return fmt.Errorf("command interface not accessible: %w", err)
	}

	cmd, err := os.OpenFile(misterconfig.CmdInterface, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer func() {
		if err := cmd.Close(); err != nil {
			log.Error().Err(err).Msg("failed to close command interface")
		}
	}()

	// TODO: don't hardcode here
	if _, err := fmt.Fprintf(cmd, "load_core %s\n", filepath.Join(misterconfig.SDRootDir, "menu.rbf")); err != nil {
		return err
	}

	return nil
}

// LaunchGenericFile Given a generic file path, launch it using the correct method, if possible.
func LaunchGenericFile(cfg *config.Instance, path string, core *Core) error {
	var err error
	isGame := false
	ext := s.ToLower(filepath.Ext(path))
	switch ext {
	case ".mra":
		err = launchFile(path)
		if err != nil {
			return err
		}
	case ".mgl":
		err = launchFile(path)
		if err != nil {
			return err
		}
		isGame = true
	case ".rbf":
		err = launchFile(path)
		if err != nil {
			return err
		}
	default:
		if core == nil {
			return fmt.Errorf("unknown file type: %s", ext)
		}

		err = launchTempMgl(cfg, core, path)
		if err != nil {
			return err
		}
		isGame = true
	}

	if ActiveGameEnabled() && isGame {
		err := SetActiveGame(path)
		if err != nil {
			return err
		}
	}

	return nil
}
