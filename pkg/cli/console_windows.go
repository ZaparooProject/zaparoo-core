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

package cli

import (
	"os"

	"golang.org/x/sys/windows"
)

var procAttachConsole = windows.NewLazySystemDLL("kernel32.dll").NewProc("AttachConsole")

// attachConsole points the standard streams at the console this process was
// started from.
//
// The Windows binary is linked for the GUI subsystem, because it is a tray
// application and must not open a console window of its own. A GUI process
// starts with no console attached, so anything written to stdout went to a
// handle that was never valid: every flag that prints its answer was silently
// invisible unless the user redirected to a file. The link from -upload-log is
// the case where that costs the most, because the link is the whole point.
//
// Nothing here runs when there is no console to attach to — started at logon,
// from the installer or from Explorer — and nothing runs when no arguments were
// passed, so launching the tray from a terminal does not turn that terminal into
// a log destination for the rest of the session.
func attachConsole() {
	if len(os.Args) < 2 {
		return
	}

	// Read the handles before attaching: a caller who redirected to a file or a
	// pipe already has a real one, and the console must not replace it.
	redirected := map[uint32]bool{
		windows.STD_OUTPUT_HANDLE: hasStdHandle(windows.STD_OUTPUT_HANDLE),
		windows.STD_ERROR_HANDLE:  hasStdHandle(windows.STD_ERROR_HANDLE),
	}

	if !attachParentConsole() {
		return
	}

	if !redirected[windows.STD_OUTPUT_HANDLE] {
		if f := openConsoleOut(); f != nil {
			os.Stdout = f
		}
	}
	if !redirected[windows.STD_ERROR_HANDLE] {
		if f := openConsoleOut(); f != nil {
			os.Stderr = f
		}
	}
}

// attachParentConsole binds this process to its parent's console, reporting
// whether it now has one.
//
// x/sys/windows does not wrap AttachConsole, so this calls kernel32 directly.
// attachParentProcess is the documented sentinel for "whatever started me".
func attachParentConsole() bool {
	const attachParentProcess = ^uintptr(0)
	r, _, _ := procAttachConsole.Call(attachParentProcess)
	return r != 0
}

// hasStdHandle reports whether a standard handle was inherited, which is what a
// shell redirect looks like from in here.
func hasStdHandle(id uint32) bool {
	h, err := windows.GetStdHandle(id)
	return err == nil && h != 0 && h != windows.InvalidHandle
}

// openConsoleOut opens the attached console for writing. Each stream gets its
// own handle so that closing one cannot take the other with it.
func openConsoleOut() *os.File {
	name, err := windows.UTF16PtrFromString("CONOUT$")
	if err != nil {
		return nil
	}
	h, err := windows.CreateFile(
		name,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		0,
		0,
	)
	if err != nil {
		return nil
	}
	return os.NewFile(uintptr(h), "CONOUT$")
}
