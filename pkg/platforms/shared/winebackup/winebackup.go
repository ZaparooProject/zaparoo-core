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

// Package winebackup holds the backup policy for game saves inside Wine and
// Proton prefixes, shared by every Linux platform that runs Windows games.
package winebackup

import (
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
)

// UserSavePatterns matches the per-user folders Windows games write saves to,
// relative to a prefix's drive_c/users directory.
var UserSavePatterns = []platforms.BackupPattern{
	{Glob: "*/Documents/**"},
	{Glob: "*/Saved Games/**"},
	{Glob: "*/AppData/Roaming/**"},
	{Glob: "*/AppData/LocalLow/**"},
	{Glob: "*/AppData/Local/**"},
}

// SaveExclusions drops Wine's legacy compatibility links and the caches,
// logs and launcher data that share the user save folders.
var SaveExclusions = []platforms.BackupPattern{
	{Glob: "*/Application Data/**"},
	{Glob: "*/Cookies/**"},
	{Glob: "*/Local Settings/**"},
	{Glob: "*/My Documents/**"},
	{Glob: "*/NetHood/**"},
	{Glob: "*/PrintHood/**"},
	{Glob: "*/Recent/**"},
	{Glob: "*/SendTo/**"},
	{Glob: "*/Start Menu/**"},
	{Glob: "*/Templates/**"},
	{Glob: "*/AppData/Local/Battle.net/**"},
	{Glob: "*/AppData/Local/Temp/**"},
	{Glob: "*/AppData/Local/D3DSCache/**"},
	{Glob: "*/AppData/Local/NVIDIA/**"},
	{Glob: "*/AppData/Local/AMD/**"},
	{Glob: "*.dxvk.bin"},
	{Glob: "*.dxvk.lut"},
	{Glob: "CachedData.db"},
	{Contains: "internet cache/"},
	{Contains: "cache/"},
	{Contains: "caches/"},
	{Contains: "crashdumps/"},
	{Contains: "crashes/"},
	{Contains: "logs/"},
	{Contains: "telemetry/"},
}

// PrefixPatterns roots path globs at prefix so a definition above the users
// directory matches the same files. Basename globs and substring patterns
// already match at any depth and pass through unchanged.
func PrefixPatterns(prefix string, patterns []platforms.BackupPattern) []platforms.BackupPattern {
	result := make([]platforms.BackupPattern, 0, len(patterns))
	for _, pattern := range patterns {
		if pattern.Glob != "" && strings.ContainsAny(pattern.Glob, `/\`) {
			result = append(result, platforms.BackupPattern{Glob: filepath.Join(prefix, pattern.Glob)})
			continue
		}
		result = append(result, pattern)
	}
	return result
}
