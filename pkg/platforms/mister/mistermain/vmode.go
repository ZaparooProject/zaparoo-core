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

package mistermain

import (
	"fmt"
	"os"

	misterconfig "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
)

const VideoModeFormatRGB32 = "18888"

// fb_cmd0 = scaled = fb_cmd0 $fmt $rb $scale
// fb_cmd1 = exact = fb_cmd1 $fmt $rb $width $height

// in vmode script, checks for rescount contents at start, sets mode,
// then polls until it's the same value (up to 5 times)
// /sys/module/MiSTer_fb/parameters/res_count

func SetVideoMode(width, height int) error {
	if _, err := os.Stat(misterconfig.CmdInterface); err != nil {
		return fmt.Errorf("command interface not accessible: %w", err)
	}

	cmd, err := os.OpenFile(misterconfig.CmdInterface, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("failed to open command interface: %w", err)
	}
	defer func(cmd *os.File) {
		_ = cmd.Close()
	}(cmd)

	cmdStr := fmt.Sprintf(
		"%s %d %d %d",
		VideoModeFormatRGB32[1:],
		VideoModeFormatRGB32[0],
		width,
		height,
	)

	_, err = cmd.WriteString(cmdStr)
	if err != nil {
		return fmt.Errorf("failed to write to command interface: %w", err)
	}

	return nil
}
