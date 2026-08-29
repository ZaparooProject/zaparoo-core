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

package payload

import (
	"path/filepath"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/updatepayload"
)

var files = []updatepayload.File{
	{
		SourcePath: filepath.Join("cmd", "batocera", "scripts", "configs", "emulationstation", "scripts",
			"game-selected", "zaparoo_game_select.sh"),
		ArchivePath: "scripts/configs/emulationstation/scripts/game-selected/zaparoo_game_select.sh",
		InstallPath: "configs/emulationstation/scripts/game-selected/zaparoo_game_select.sh",
		Mode:        0o755,
	},
	{
		SourcePath:  filepath.Join("cmd", "batocera", "scripts", "configs", "multimedia_keys.conf"),
		ArchivePath: "scripts/configs/multimedia_keys.conf",
		InstallPath: "configs/multimedia_keys.conf",
		Mode:        0o644,
	},
	{
		SourcePath:  filepath.Join("cmd", "batocera", "scripts", "content_downloader.png"),
		ArchivePath: "scripts/content_downloader.png",
		InstallPath: "content_downloader.png",
		Mode:        0o644,
	},
	{
		SourcePath:  filepath.Join("cmd", "batocera", "scripts", "ports", "Zaparoo.sh"),
		ArchivePath: "scripts/ports/Zaparoo.sh",
		InstallPath: "../roms/ports/Zaparoo.sh",
		Mode:        0o755,
	},
	{
		SourcePath:  filepath.Join("cmd", "batocera", "scripts", "services", "zaparoo_service"),
		ArchivePath: "scripts/services/zaparoo_service",
		InstallPath: "services/zaparoo_service",
		Mode:        0o755,
	},
	{
		SourcePath:  filepath.Join("cmd", "batocera", "scripts", "zaparoo_wrapper.sh"),
		ArchivePath: "scripts/zaparoo_wrapper.sh",
		InstallPath: "zaparoo_wrapper.sh",
		Mode:        0o755,
	},
	{
		SourcePath:  filepath.Join("cmd", "batocera", "scripts", "zaparoo_write_game.sh"),
		ArchivePath: "scripts/zaparoo_write_game.sh",
		InstallPath: "zaparoo_write_game.sh",
		Mode:        0o755,
	},
}

// Files lists the platform-owned files an update installs alongside the
// binary.
//
// The list lives here rather than in the batocera package so the release
// tooling can read it without compiling that package. Importing any symbol
// from a platform package pulls in the whole readers stack, which needs libnfc
// headers — something a machine that only builds archives has no reason to
// have.
func Files() []updatepayload.File {
	return append([]updatepayload.File(nil), files...)
}
