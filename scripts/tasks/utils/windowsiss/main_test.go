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

package main

import (
	"encoding/base64"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The build copies the bundled driver in only when the generated script asks
// for it, so an architecture with no MSI has to produce an empty name rather
// than a plausible-looking one.
func TestViGEmMsiForArch(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		arch string
		want string
	}{
		{"amd64", "ViGEmBus.x64.msi"},
		{"386", "ViGEmBus.msi"},
		{"arm64", ""},
		{"riscv64", ""},
	} {
		t.Run(tc.arch, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, vigemMsiForArch(tc.arch))
		})
	}
}

// decodeEncodedCommand reverses what powershell.exe -EncodedCommand expects.
func decodeEncodedCommand(t *testing.T, encoded string) string {
	t.Helper()

	raw, err := base64.StdEncoding.DecodeString(encoded)
	require.NoError(t, err)
	require.Equal(t, 0, len(raw)%2, "UTF-16 needs an even number of bytes")

	units := make([]uint16, 0, len(raw)/2)
	for i := 0; i < len(raw); i += 2 {
		units = append(units, uint16(raw[i])|uint16(raw[i+1])<<8)
	}
	return string(utf16.Decode(units))
}

// The elevated script is the only thing standing between a staging directory
// an unprivileged process can write and an install with administrator rights,
// so its guards are pinned here rather than left to review.
func TestViGEmInstallCommandGuardsTheElevatedInstall(t *testing.T) {
	t.Parallel()

	script := decodeEncodedCommand(t, vigemInstallCommand("ViGEmBus.x64.msi"))

	assert.Contains(t, script, "ViGEmBus.x64.msi", "the script must look for this architecture's package")
	assert.Contains(t, script, "Get-AuthenticodeSignature", "the package must be checked before it is installed")
	assert.Contains(t, script, "Nefarius Software Solutions", "the publisher must be pinned")
	assert.Contains(t, script, "SetSecurityDescriptorSddlForm", "the copy must be locked down")

	// Order is the whole point: a package checked before it is locked could be
	// checked as one file and installed as another.
	lock := strings.Index(script, "Set-Acl")
	check := strings.Index(script, "Get-AuthenticodeSignature")
	install := strings.Index(script, "msiexec")
	require.Positive(t, lock)
	assert.Less(t, lock, check, "the copy must be locked before it is checked")
	assert.Less(t, check, install, "the package must be checked before it is installed")

	// A single backslash: a raw Go string keeps whatever is written, and a
	// doubled one would send PowerShell to a path that does not exist.
	assert.Contains(t, script, `Zaparoo\vigembus-stage`)
	assert.NotContains(t, script, `Zaparoo\\vigembus-stage`)
}

func TestViGEmInstallCommandIsEmptyWithoutAPackage(t *testing.T) {
	t.Parallel()

	assert.Empty(t, vigemInstallCommand(""))
}
