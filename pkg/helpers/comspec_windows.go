//go:build windows

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

package helpers

import (
	"os"
	"path/filepath"
)

// ComSpec returns the absolute path to the Windows command interpreter
// (cmd.exe), resolving it without relying on %PATH%. Some machines have a
// %PATH% that does not contain C:\Windows\System32 (e.g. a truncated or
// stripped environment), which makes exec lookups of the bare name "cmd"
// fail. Using an absolute path also avoids Go's exec.ErrDot protection, which
// rejects executables found only relative to the current directory.
//
// %ComSpec% and %SystemRoot% are set by the OS independently of %PATH%, so
// they survive a broken PATH.
func ComSpec() string {
	if cs := os.Getenv("ComSpec"); cs != "" {
		return cs
	}
	if sr := os.Getenv("SystemRoot"); sr != "" {
		return filepath.Join(sr, "System32", "cmd.exe")
	}
	return `C:\Windows\System32\cmd.exe`
}
