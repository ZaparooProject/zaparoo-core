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

const (
	VideoModeFormatRGB32 = "18888" // 32-bit RGBA (8 bits per channel), rb=1
	VideoModeFormatRGB16 = "1565"  // 16-bit RGB (5 red, 6 green, 5 blue), rb=1
)

// fb_cmd0 = scaled = fb_cmd0 $fmt $rb $divisor
//   - Creates framebuffer at display_resolution/divisor
//   - Scales to fill entire screen (no borders)
//   - divisor: 1=full, 2=half, 3=third, 4=quarter
//
// fb_cmd1 = exact = fb_cmd1 $fmt $rb $width $height
//   - Creates framebuffer at exact dimensions
//   - Integer-scales to fit display
//   - Centers with borders

// SetVideoModeScaled sets a scaled video mode that fills the entire screen.
// divisor controls resolution: 1=full, 2=half, 3=third, 4=quarter
func SetVideoModeScaled(divisor int) error {
	if divisor < 1 || divisor > 4 {
		return fmt.Errorf("divisor must be 1-4, got %d", divisor)
	}

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

	// fb_cmd0 format: "fb_cmd0 format rb divisor"
	cmdStr := fmt.Sprintf(
		"fb_cmd0 %s %d %d",
		VideoModeFormatRGB32[1:], // "8888"
		VideoModeFormatRGB32[0],  // Rune '1' as int (49)
		divisor,
	)

	_, err = cmd.WriteString(cmdStr)
	if err != nil {
		return fmt.Errorf("failed to write to command interface: %w", err)
	}

	return nil
}

// SetVideoModeExact sets an exact video mode with specific width and height.
// The framebuffer is created at the exact dimensions and integer-scaled to fit the display.
func SetVideoModeExact(width, height int, format string) error {
	if width < 1 || height < 1 {
		return fmt.Errorf("width and height must be positive, got %dx%d", width, height)
	}

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

	// fb_cmd1 format: "fb_cmd1 format rb width height"
	// format is typically "8888" for RGB32
	// rb is '1' for RGBA order
	cmdStr := fmt.Sprintf(
		"fb_cmd1 %s %d %d %d",
		format[1:], // "8888"
		format[0],  // Rune '1' as int (49)
		width,
		height,
	)

	_, err = cmd.WriteString(cmdStr)
	if err != nil {
		return fmt.Errorf("failed to write to command interface: %w", err)
	}

	return nil
}
