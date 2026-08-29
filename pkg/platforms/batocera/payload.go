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

package batocera

import (
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/batocera/payload"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/updatepayload"
)

// UpdatePayload lists the platform-owned files an update installs alongside the
// binary. The list itself lives in the payload subpackage so build tooling can
// read it without compiling this one.
func UpdatePayload() []updatepayload.File {
	return payload.Files()
}
