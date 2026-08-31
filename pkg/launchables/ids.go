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
	MisterOtherTamagotchi    = uuid.MustParse("ee9f97fb-df75-460a-82af-13ed03c3deb5")
	MisterOtherSonicMania    = uuid.MustParse("02abc813-464d-4c7c-8feb-5d11411b959c")
	MisterOtherQuake         = uuid.MustParse("c483707b-deb9-4f9b-8414-7fd45717fa7f")
	MisterArcadeThirdStrike  = uuid.MustParse("618433aa-0e8b-4230-800e-ec33758affb7")
	MisterGenesisPaprium     = uuid.MustParse("b97b6512-01ac-4685-94c9-96264ee86f01")
	MisterConsoleMMS2Gameboy = uuid.MustParse("c25b1083-e180-4cad-83f8-ad34c1598b4d")

	MisterConsoleAY38500            = uuid.MustParse("8229ba4f-722c-5758-bea7-e87cf1189249")
	MisterConsoleBBCBridgeCompanion = uuid.MustParse("7c10204b-487a-5fc5-b581-a02e5cace541")
	MisterConsoleMyVision           = uuid.MustParse("8aabe6fa-663f-5042-9bbc-5854203ecb8a")
	MisterConsoleSuperVision8000    = uuid.MustParse("c5749c4c-ed8f-5dbf-95aa-ae9050d6444a")

	MisterComputerAltair8800  = uuid.MustParse("9f8c3edf-d3e1-5318-ae32-239ba7b525e3")
	MisterComputerArchie      = uuid.MustParse("7c4b3326-1eff-5d0d-90f4-a38647d5e59a")
	MisterComputerAtariST     = uuid.MustParse("db259c65-6377-5d97-8739-86b3c2ca3722")
	MisterComputerC128        = uuid.MustParse("e67fea5e-c9aa-52b9-a3d0-e618802f399a")
	MisterComputerCoCo3       = uuid.MustParse("9994a8bf-c544-577c-870f-af46cace1ce5")
	MisterComputerColecoAdam  = uuid.MustParse("a519cc51-beca-59d0-943d-0b0ad4b6096c")
	MisterComputerEG2000      = uuid.MustParse("b248cffd-830b-5512-8ff1-ac8b03237954")
	MisterComputerEnterprise  = uuid.MustParse("58d5a253-2b6e-5487-92af-3dedb2420e7a")
	MisterComputerHomelab     = uuid.MustParse("78754749-7d52-5cf2-a244-403fe1a8f83f")
	MisterComputerIQ151       = uuid.MustParse("5a5481eb-7ba5-5555-bbd2-74617735a40f")
	MisterComputerMacLC       = uuid.MustParse("0f44cfc2-9841-57ab-8ce6-29385abf0a2e")
	MisterComputerOndraSPO186 = uuid.MustParse("3ed6d7a9-cfca-5611-a286-9715453d04de")
	MisterComputerPC88        = uuid.MustParse("8423a436-b5d0-5b14-8708-fd07bd20e506")
	MisterComputerPCjr        = uuid.MustParse("66593d3c-aeab-57cd-aa0c-74b3b7e11d5b")
	MisterComputerSharpMZ     = uuid.MustParse("fe290bd4-beb2-5aa3-a8c1-d2397180a25e")
	MisterComputerTK2000      = uuid.MustParse("30780a8e-2a6b-5d9d-aa33-589fb427d5c3")
	MisterComputerTandy1000   = uuid.MustParse("1aae818d-8a80-5bca-9ed7-36078f499689")
	MisterComputerVT52        = uuid.MustParse("ffa35024-0bbf-57fc-a0c4-7712498c1d7a")
)

// ZaparooLaunchableNamespace derives stable UUIDs (via uuid.NewSHA1) for
// user-configured launchables from their config.toml "id" string, so the
// same id always produces the same UUID across restarts and devices.
var ZaparooLaunchableNamespace = uuid.MustParse("f7a49fd1-2910-4fa8-8b41-db0f3510e1fc")
