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


// cgo compiles only the sources in the package directory, so each vendored
// Game_Music_Emu translation unit is built through a wrapper like this one.

// Gb_Cpu.cpp carries one local change: run() keeps pc in a uint16_t.
// Upstream declares it unsigned, so a program counter running off the top
// of memory never wrapped and indexed past the end of code_map. Go's build
// cache does not see edits to included files; change this wrapper when the
// vendored file changes.

#include "third_party/gme/gme/Gb_Cpu.cpp"
