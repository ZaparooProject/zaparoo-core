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

// Package mediaslot defines media slot identifiers shared across launch,
// playback, and playlist routing. It has no internal dependencies so it can
// be imported by any package without causing import cycles.
package mediaslot

import (
	"fmt"
	"strings"
)

// Media slot identifiers.
const (
	Primary    = "primary"
	Background = "background"
)

// Normalize returns the canonical slot name for raw, which may be empty,
// mixed-case, or padded with whitespace. An empty raw value resolves to
// Primary. Returns an error for any unrecognised slot name.
func Normalize(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", Primary:
		return Primary, nil
	case Background, "bg":
		return Background, nil
	default:
		return "", fmt.Errorf("unsupported media slot: %s", raw)
	}
}
