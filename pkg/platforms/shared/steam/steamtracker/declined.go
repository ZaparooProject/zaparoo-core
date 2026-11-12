//go:build (linux && !android) || darwin || windows

/*
Zaparoo Core
Copyright (c) 2026 The Zaparoo Project Contributors.
SPDX-License-Identifier: GPL-3.0-or-later

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

package steamtracker

import "github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"

// publishDeclined reports whether Core declined to publish a game the tracker
// just reported. Core declines a game that a stop was meant for when the stop
// landed before the game had started, and the game must then be stopped
// rather than tracked. Other media in its place means something newer
// started, which is left alone.
func publishDeclined(current *models.ActiveMedia) bool {
	return current == nil
}
