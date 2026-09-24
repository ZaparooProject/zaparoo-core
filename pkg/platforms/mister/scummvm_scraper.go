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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
)

// scummVMGameDirectory resolves a configured game directory, or "" when it
// cannot carry local metadata.
func scummVMGameDirectory(game ScummVMGame) string {
	if game.Path == "" || strings.Contains(game.Path, "://") || virtualpath.ContainsControlChar(game.Path) {
		return ""
	}
	directory := filepath.Clean(game.Path)
	if !filepath.IsAbs(directory) {
		directory = filepath.Join(scummvmBaseDir, directory)
	}
	if filepath.Dir(directory) == directory {
		return ""
	}
	return directory
}

// scummVMMetadataSources converts ScummVM's configured game directories into
// provenance carried by the normal media indexing pipeline. The result is
// parallel to games, with nil for a game that has no usable directory.
//
// A game's metadata root is normally its parent directory. Variants kept in
// subfolders of one game folder would each get that game folder as a root and
// lose the gamelist and artwork stored beside their sibling games, so a root
// whose own parent is another game's root is folded into it. Folding stops at a
// folder a game is configured on: that folder holds one game's data, so it is
// not a collection its neighbours share.
//
// ScummVM adds one target per detected variant of a game, such as each
// language on a multilingual disc, all configured on the same directory. Each
// source is grouped by the target's game ID, so metadata for that directory
// reaches every variant while a directory holding different games stays
// ambiguous.
func scummVMMetadataSources(games []ScummVMGame) []*platforms.MediaSource {
	directories := make([]string, len(games))
	configured := make(map[string]struct{}, len(games))
	roots := make(map[string]struct{}, len(games))
	for i, game := range games {
		directories[i] = scummVMGameDirectory(game)
		if directories[i] == "" {
			continue
		}
		// Keyed the way the rest of the pipeline compares paths, so a folder
		// named in another case on a case-insensitive card is one folder.
		configured[helpers.NormalizePathForComparison(directories[i])] = struct{}{}
		roots[helpers.NormalizePathForComparison(filepath.Dir(directories[i]))] = struct{}{}
	}
	sources := make([]*platforms.MediaSource, len(games))
	for i, game := range games {
		directory := directories[i]
		if directory == "" {
			continue
		}
		root := filepath.Dir(directory)
		for {
			if _, own := configured[helpers.NormalizePathForComparison(root)]; own {
				break
			}
			parent := filepath.Dir(root)
			_, shared := roots[helpers.NormalizePathForComparison(parent)]
			if !shared || filepath.Dir(parent) == parent {
				break
			}
			root = parent
		}
		sources[i] = &platforms.MediaSource{
			Path: directory, Root: root, Kind: platforms.MediaSourceDirectory, Group: game.GameID,
		}
	}
	return sources
}
