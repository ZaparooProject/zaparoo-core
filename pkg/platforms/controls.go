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

package platforms

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
)

// ControlArgSlot is the optional argument naming the save state slot for
// save_state and load_state.
const ControlArgSlot = "slot"

var (
	// ErrControlUnavailable reports a control the launcher defines but the
	// running core or emulator cannot perform right now.
	ErrControlUnavailable = errors.New("control not available for the running media")
	// ErrInvalidControlArg reports a control argument the caller got wrong,
	// such as a save state slot out of range.
	ErrInvalidControlArg = errors.New("invalid control argument")
)

// ControlNames returns the sorted action names a launcher defines controls for.
func ControlNames(l *Launcher) []string {
	if l == nil || len(l.Controls) == 0 {
		return nil
	}
	names := make([]string, 0, len(l.Controls))
	for action := range l.Controls {
		names = append(names, action)
	}
	sort.Strings(names)
	return names
}

// SlotArg reads the optional "slot" argument of params as a number in
// [minSlot, maxSlot]. An absent or empty argument selects minSlot.
func SlotArg(params ControlParams, minSlot, maxSlot int) (int, error) {
	raw := params.Args[ControlArgSlot]
	if raw == "" {
		return minSlot, nil
	}
	slot, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%w: slot %q must be a number", ErrInvalidControlArg, raw)
	}
	if slot < minSlot || slot > maxSlot {
		return 0, fmt.Errorf("%w: slot %d must be between %d and %d", ErrInvalidControlArg, slot, minSlot, maxSlot)
	}
	return slot, nil
}
