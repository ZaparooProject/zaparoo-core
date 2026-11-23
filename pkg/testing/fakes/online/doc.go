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

// Package online fakes the Zaparoo Online device API. It is a contract fake
// for Core development, not a reference server. Every route and rule in it is
// taken from what Core's own clients send and accept: the device link flow,
// the heartbeat and device record, cloud backup, play history sync, Library
// sync and remote operations. Where Core does not pin a behaviour down, the
// fake picks the simplest answer Core takes and says so in a comment. It
// says nothing about how the real service works inside.
//
// The fake keeps its state in memory, or in a JSON file under
// Options.StateDir so a simulated device keeps its link and backups across
// restarts. A control API, mounted by the sim on its control socket, and the
// CLI on top of it approve link requests, set the account, queue remote
// operations and inject faults.
package online
