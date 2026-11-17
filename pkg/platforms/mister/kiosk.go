//go:build linux && !android

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

package mister

import (
	"encoding/json"
	"io"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/spf13/afero"
)

const (
	// mainCommandKiosk is the Main fork's own lock: with it on, nothing on
	// the device opens Main's menu.
	mainCommandKiosk = "kiosk"
	// mainCapabilityKioskSealed is advertised by a Main whose lock also
	// covers the controls that leave a running core without the menu: the
	// keyboard reset combinations, the user button and the console lease.
	mainCapabilityKioskSealed = "kiosk_sealed"
)

var _ platforms.KioskHost = (*Platform)(nil)

// KioskUnsealed implements platforms.KioskHost.
func (*Platform) KioskUnsealed() []string {
	fs := afero.NewOsFs()
	return kioskUnsealed(fs, newMainCommands(fs))
}

// SetKioskLocked implements platforms.KioskHost. It fails when the running
// Main has no lock, or is between processes; the caller offers it again.
func (*Platform) SetKioskLocked(locked bool) error {
	return setMainKiosk(newMainCommands(afero.NewOsFs()), locked)
}

func setMainKiosk(main *mainCommands, locked bool) error {
	arg := "off"
	if locked {
		arg = "on"
	}
	return main.send(mainCommandKiosk, arg)
}

func kioskUnsealed(fs afero.Fs, main *mainCommands) []string {
	if !main.supports(mainCommandKiosk) {
		return []string{platforms.KioskUnsealedMainLock}
	}
	reasons := []string{}
	if !main.supports(mainCapabilityKioskSealed) {
		reasons = append(reasons, platforms.KioskUnsealedMainControls)
	}
	if frontendActive(fs) && !frontendHonorsKiosk(fs) {
		reasons = append(reasons, platforms.KioskUnsealedFrontend)
	}
	return reasons
}

// frontendHonorsKiosk reports whether the running frontend says it hides
// what a locked device should not offer. A frontend that predates the lock
// leaves the flag out, which reads as no.
func frontendHonorsKiosk(fs afero.Fs) bool {
	file, err := fs.Open(frontendStatusPath)
	if err != nil {
		return false
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, frontendStatusMaxBytes+1))
	if err != nil || len(data) > frontendStatusMaxBytes {
		return false
	}
	var status frontendStatus
	if err := json.Unmarshal(data, &status); err != nil {
		return false
	}
	return status.Kiosk
}
