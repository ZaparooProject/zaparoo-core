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

//go:build !linux && !windows && !darwin

package power

// Read reports an unknown power state. Core ships builds for Linux, Windows
// and macOS, and each has its own reader; a build for anything else has no way
// to ask the hardware.
//
// Unknown is the fail-safe answer rather than the convenient one. The updater
// treats it as "could lose power at any moment" and refuses an automatic
// install, which a person can still force past once they have been told the
// charge is unreadable. Reporting no battery instead would hand every such
// build a green light no reading supports.
func Read() (Status, error) {
	return Status{Source: SourceUnknown}, nil
}
