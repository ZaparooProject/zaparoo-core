/*
Zaparoo Core
Copyright (c) 2025 The Zaparoo Project Contributors.
SPDX-License-Identifier: GPL-3.0-or-later

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

package tty2oled

import "time"

// TTY2OLED Protocol Commands
const (
	// Handshake command
	CmdHandshake = "QWERTZ"

	// Core display commands
	CmdCore       = "CMDCOR"   // CMDCOR,<corename>,<transition>
	CmdText       = "CMDTXT"   // CMDTXT,<font>,<color>,<bgcolor>,<x>,<y>,<text>
	CmdClearShow  = "CMDCLS"   // CMDCLS - clear display and update
	CmdClearNoUpd = "CMDCLSWU" // CMDCLSWU - clear display without update
	CmdContrast   = "CMDCON"   // CMDCON,<contrast>
	CmdRotate     = "CMDROT"   // CMDROT,1
	CmdOrgLogo    = "CMDSORG"  // CMDSORG - show original logo after rotation
	CmdClear      = "CMDCLEAR" // Clear display (custom command - deprecated)

	// Screensaver commands
	CmdScreensaver = "CMDSAVER" // CMDSAVER,<mode>,<interval>,<start>

	// Time/RTC commands
	CmdSetTime = "CMDSETTIME" // CMDSETTIME,<timestamp>

	// Hardware info command
	CmdHardwareInfo = "CMDHWINF" // CMDHWINF - request hardware info
)

// Protocol parameters
const (
	// Transition effects
	TransitionNone  = "0"
	TransitionSlide = "1"
	TransitionFade  = "2"

	// Contrast levels (0-255)
	ContrastMin     = 0
	ContrastMax     = 255
	ContrastDefault = 128

	// Screensaver modes
	ScreensaverOff   = "0"
	ScreensaverClock = "1"
	ScreensaverBlank = "2"
	ScreensaverLogo  = "3"

	// Communication settings
	CommandTerminator = "\n"
	ReadTimeout       = 1 * time.Second // For device detection
	WriteTimeout      = 100 * time.Millisecond
	WaitDuration      = 200 * time.Millisecond // Shell script WAITSECS=0.2
)

// Picture format priorities (in order of preference)
var PictureFormats = []string{
	"GSC_US",   // Priority 1: GSC US region variant
	"XBM_US",   // Priority 2: XBM US region variant
	"GSC",      // Priority 3: GSC standard
	"XBM",      // Priority 4: XBM standard
	"XBM_TEXT", // Priority 5: XBM text variant
}

// Device identification strings that might indicate tty2oled devices
var DeviceIdentifiers = []string{
	"tty2oled",
	"TTY2OLED",
	"Arduino", // Many tty2oled devices show up as Arduino
	"CH340",   // Common USB-serial chip
	"CP210",   // Another common USB-serial chip
	"FTDI",    // FTDI USB-serial devices
	"ESP32",   // ESP32-based tty2oled devices (like HWESP32DE)
}
