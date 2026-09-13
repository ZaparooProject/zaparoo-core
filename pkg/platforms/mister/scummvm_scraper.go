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

//go:build linux

package mister

import (
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
)

// scummVMMetadataSource converts ScummVM's configured game directory into
// provenance carried by the normal media indexing pipeline.
func scummVMMetadataSource(game ScummVMGame) *platforms.MediaSource {
	if game.Path == "" || strings.Contains(game.Path, "://") || virtualpath.ContainsControlChar(game.Path) {
		return nil
	}
	directory := filepath.Clean(game.Path)
	if !filepath.IsAbs(directory) {
		directory = filepath.Join(scummvmBaseDir, directory)
	}
	root := filepath.Dir(directory)
	if root == directory {
		return nil
	}
	return &platforms.MediaSource{Path: directory, Root: root, Kind: platforms.MediaSourceDirectory}
}
