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

package windows

import (
	"context"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/inputactivity"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/windowsinput"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
)

var _ platforms.InputActivitySource = (*Platform)(nil)

func windowsInputSource() *inputactivity.Windows {
	// Zaparoo's own virtual pads are XInput devices too and must not count, and
	// neither must the keys and pad buttons Core injects itself.
	return inputactivity.NewWindows(windowsinput.ActiveUserIndices, inputactivity.Injections{
		Keyboard: windowsinput.LastKeyboardInjection,
		Gamepad:  windowsinput.LastGamepadInjection,
	})
}

// InputCoverage implements platforms.InputActivitySource. Keyboard and mouse
// come from the session's last-input time, controllers from XInput plus winmm;
// each is reported only when its API loads.
func (*Platform) InputCoverage() platforms.InputClass {
	return platforms.InputClass(windowsInputSource().Coverage())
}

// WatchInput implements platforms.InputActivitySource.
func (*Platform) WatchInput(ctx context.Context, responsive bool, onActivity func()) error {
	return windowsInputSource().Watch(ctx, responsive, onActivity) //nolint:wrapcheck // already descriptive
}
