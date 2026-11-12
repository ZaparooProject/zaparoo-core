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

package android

import (
	"context"
	"errors"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
)

var _ platforms.InputActivitySource = (*Platform)(nil)

var errInputFromHost = errors.New("input activity must be supplied by the host app")

// InputCoverage implements platforms.InputActivitySource. Core cannot observe
// input on Android; the host app owns the device and would have to supply it,
// so nothing is covered and anything needing input activity refuses.
func (*Platform) InputCoverage() platforms.InputClass {
	return 0
}

// WatchInput implements platforms.InputActivitySource.
func (*Platform) WatchInput(context.Context, bool, func()) error {
	return errInputFromHost
}
