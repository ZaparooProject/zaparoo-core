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

package launchables

import "github.com/google/uuid"

// Define stable launchable UUIDs here, then import them from the platform
// package that owns the actual launch behavior.
var (
	MisterOtherChess         = uuid.MustParse("cc7bb790-cc2c-47d2-aecc-fdd192e9d1e1")
	MisterOtherDonut         = uuid.MustParse("65fc7c57-559b-4114-82db-7e96d1164cd6")
	MisterOtherEpochGalaxyII = uuid.MustParse("d6443fe6-1c01-48f7-aa1d-30aca8fdc967")
	MisterOtherFlappyBird    = uuid.MustParse("2b1c5f98-3a74-450e-8984-347e31c4a602")
	MisterOtherGameOfLife    = uuid.MustParse("cc28721f-9239-4931-b10f-2a5c6180d26f")
	MisterOtherGBMidi        = uuid.MustParse("565a41bc-7c9e-43a4-afcf-781659efe338")
	MisterOtherGenMidi       = uuid.MustParse("91fafc5c-25d1-4356-b5b5-3628573d77b3")
	MisterOtherSlugCross     = uuid.MustParse("e05893a4-59ee-4919-a3cd-47ff8c5e518d")
	MisterOtherTomyScramble  = uuid.MustParse("349dfee7-bc32-4004-b377-ff8fe8083836")
	MisterArcadeThirdStrike  = uuid.MustParse("618433aa-0e8b-4230-800e-ec33758affb7")
)
