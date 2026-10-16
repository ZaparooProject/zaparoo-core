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

package linuxbase

import (
	"context"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/inputactivity"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
)

var _ platforms.InputActivitySource = (*Base)(nil)

// InputCoverage implements platforms.InputActivitySource. Keyboard and mouse
// are covered only when the session reports idle time over D-Bus (the desktop
// idle timer, else logind), and controllers only when event nodes can be
// read, so InputAll needs both to work.
func (*Base) InputCoverage() platforms.InputClass {
	return platforms.InputClass(inputactivity.NewDesktop().Coverage())
}

// WatchInput implements platforms.InputActivitySource.
func (*Base) WatchInput(ctx context.Context, responsive bool, onActivity func()) error {
	return inputactivity.NewDesktop().Watch(ctx, responsive, onActivity) //nolint:wrapcheck // already descriptive
}
