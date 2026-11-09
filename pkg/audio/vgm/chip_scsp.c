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


// Unity build of the vendored scsp core; see third_party/README.

#include "csrc/zvgm_prefix.h"

// The SCSP noise generator and LFO call rand(). Route them to a small
// generator of our own so other C code's rand() state is left alone.
#include <stdlib.h>
#include "csrc/zvgm_rand.h"
#define rand() zvgm_rand()
#include "third_party/libvgm/cores/scsp.c"
#include "csrc/zvgm_suffix.h"
