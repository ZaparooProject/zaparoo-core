//go:build linux && !android

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

package mister

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	misterconfig "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
)

// mainArcadeRoot mirrors MiSTer Main's set_arcade_root: the MRA path is cut
// after its first folder starting with an underscore, and with no such folder
// the storage root is used.
func mainArcadeRoot(mraPath string) string {
	idx := strings.Index(mraPath, "/_")
	if idx < 0 {
		return misterconfig.SDRootDir
	}
	end := strings.IndexByte(mraPath[idx+1:], '/')
	if end < 0 {
		return misterconfig.SDRootDir
	}
	return mraPath[:idx+1+end]
}

// mainFindArcadeRBF mirrors the arcade matching in MiSTer Main's get_rbf. It
// returns the newest RBF in coresDir named Arcade-<rbf> or <rbf>, followed by
// "." or "_".
func mainFindArcadeRBF(coresDir, rbf string) (string, bool) {
	entries, err := os.ReadDir(coresDir)
	if err != nil {
		return "", false
	}
	best := ""
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || len(name) <= 4 || !strings.EqualFold(name[len(name)-4:], ".rbf") {
			continue
		}
		for _, prefix := range []string{"Arcade-" + rbf, rbf} {
			if len(name) <= len(prefix) || !strings.EqualFold(name[:len(prefix)], prefix) {
				continue
			}
			if sep := name[len(prefix)]; (sep == '.' || sep == '_') && (best == "" || best < name) {
				best = name
			}
		}
	}
	if best == "" {
		return "", false
	}
	return filepath.Join(coresDir, best), true
}

// resolveArcadeMRAPath returns an MRA path that MiSTer Main can find the arcade
// core for. Main only searches <arcade root>/cores, so an MRA on a root that
// lacks the core (e.g. a USB copy of _Arcade) fails to load. In that case an
// identical relative path under another root is used.
func resolveArcadeMRAPath(mraPath, rbf string, roots []string) (string, error) {
	if rbf == "" {
		return mraPath, nil
	}
	coresDir := filepath.Join(mainArcadeRoot(mraPath), "cores")
	if _, ok := mainFindArcadeRBF(coresDir, rbf); ok {
		return mraPath, nil
	}

	if idx := strings.Index(mraPath, "/_"); idx >= 0 {
		rel := mraPath[idx+1:]
		for _, root := range roots {
			candidate := filepath.Join(root, rel)
			if candidate == mraPath {
				continue
			}
			if _, err := os.Stat(candidate); err != nil {
				continue
			}
			if _, ok := mainFindArcadeRBF(filepath.Join(mainArcadeRoot(candidate), "cores"), rbf); ok {
				return candidate, nil
			}
		}
	}

	return "", fmt.Errorf("arcade core %q not found in %s", rbf, coresDir)
}
