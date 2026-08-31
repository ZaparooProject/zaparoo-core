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
	"testing"
)

func TestComSpec_UsesComSpecEnvWhenSet(t *testing.T) {
	want := `D:\custom\cmd.exe`
	t.Setenv("ComSpec", want)
	t.Setenv("SystemRoot", `C:\Windows`)

	if got := ComSpec(); got != want {
		t.Errorf("ComSpec() = %q, want %q", got, want)
	}
}

func TestComSpec_FallsBackToSystemRoot(t *testing.T) {
	t.Setenv("ComSpec", "")
	t.Setenv("SystemRoot", `C:\Windows`)

	want := `C:\Windows\System32\cmd.exe`
	if got := ComSpec(); got != want {
		t.Errorf("ComSpec() = %q, want %q", got, want)
	}
}

func TestComSpec_FallsBackToDefault(t *testing.T) {
	t.Setenv("ComSpec", "")
	t.Setenv("SystemRoot", "")

	want := `C:\Windows\System32\cmd.exe`
	if got := ComSpec(); got != want {
		t.Errorf("ComSpec() = %q, want %q", got, want)
	}
}
