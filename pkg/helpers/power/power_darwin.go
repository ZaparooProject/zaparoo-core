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

//go:build darwin

package power

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

// pmsetPath is the power management tool macOS ships. It is addressed by
// absolute path so the reading does not depend on the PATH the service
// inherited.
const pmsetPath = "/usr/bin/pmset"

// pmsetTimeout bounds the reading. pmset answers immediately in normal
// operation, and the caller is the update gate, on the path of an install the
// user is waiting on, so a wedged call must not stall it.
const pmsetTimeout = 2 * time.Second

// Read reports the device's power state from pmset, which is how macOS
// exposes the battery without linking IOKit through cgo.
func Read() (Status, error) {
	ctx, cancel := context.WithTimeout(context.Background(), pmsetTimeout)
	defer cancel()

	output, err := exec.CommandContext(ctx, pmsetPath, "-g", "batt").Output()
	if err != nil {
		return Status{Source: SourceUnknown}, fmt.Errorf("reading power status from pmset: %w", err)
	}
	return parsePmsetBatt(string(output)), nil
}
