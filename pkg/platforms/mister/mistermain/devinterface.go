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

	"github.com/ZaparooProject/zaparoo-core/pkg/platforms/mister/config"
	"github.com/rs/zerolog/log"
)

func LaunchMenu() error {
	if _, err := os.Stat(config.CmdInterface); err != nil {
		return fmt.Errorf("command interface not accessible: %w", err)
	}

	cmd, err := os.OpenFile(config.CmdInterface, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("failed to open command interface: %w", err)
	}
	defer func() {
		if err := cmd.Close(); err != nil {
			log.Error().Err(err).Msg("failed to close command interface")
		}
	}()

	// TODO: don't hardcode here
	if _, err := fmt.Fprintf(cmd, "load_core %s\n", filepath.Join(config.SDRootDir, "menu.rbf")); err != nil {
		return fmt.Errorf("failed to write to command interface: %w", err)
	}

	return nil
}

func RunDevCmd(cmd, args string) error {
	_, err := os.Stat(config.CmdInterface)
	if err != nil {
		return fmt.Errorf("command interface not accessible: %w", err)
	}

	dev, err := os.OpenFile(config.CmdInterface, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("failed to open command interface: %w", err)
	}
	defer func(dev *os.File) {
		closeErr := dev.Close()
		if closeErr != nil {
			log.Error().Msgf("error closing cmd interface: %s", closeErr)
		}
	}(dev)

	_, err = fmt.Fprintf(dev, "%s %s\n", cmd, args)
	if err != nil {
		return fmt.Errorf("failed to write to command interface: %w", err)
	}

	return nil
}
