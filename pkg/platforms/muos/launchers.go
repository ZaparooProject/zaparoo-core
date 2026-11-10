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

package muos

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esde"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

// platformLaunchers builds one launcher per Zaparoo system from the ROM
// folders on every mounted card. A folder's system comes from its muOS core
// assignment, or from the frontend's automatic assignment by folder name.
func (p *Platform) platformLaunchers() []platforms.Launcher {
	p.cacheMu.Lock()
	now := p.getClock().Now()
	if p.launchers != nil && now.Sub(p.launchersAt) <= cacheTTL {
		cached := slices.Clone(p.launchers)
		p.cacheMu.Unlock()
		return cached
	}
	p.cacheMu.Unlock()

	launchers := p.buildLaunchers()

	p.cacheMu.Lock()
	p.launchers = launchers
	p.launchersAt = now
	p.cacheMu.Unlock()
	return slices.Clone(launchers)
}

func (p *Platform) buildLaunchers() []platforms.Launcher {
	fs := p.filesystem()
	db := p.coreDB()

	folderSystems := make(map[string]string)
	for _, mount := range mountPoints(fs) {
		entries, err := afero.ReadDir(fs, filepath.Join(mount, romsDirName))
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			if !e.IsDir() || strings.HasPrefix(name, ".") {
				continue
			}
			if _, seen := folderSystems[name]; seen {
				continue
			}
			systemID, ok := p.folderSystem(fs, db, name)
			if !ok {
				log.Debug().Str("folder", name).Msg("muOS ROM folder has no Zaparoo system")
				continue
			}
			folderSystems[name] = systemID
		}
	}

	folders := make([]string, 0, len(folderSystems))
	for folder := range folderSystems {
		folders = append(folders, folder)
	}
	slices.Sort(folders)

	launchers := make([]platforms.Launcher, 0, len(folders)+1)
	byID := make(map[string]int)
	for _, folder := range folders {
		systemID := folderSystems[folder]
		if at, ok := byID[systemID]; ok {
			launchers[at].Folders = append(launchers[at].Folders, folder)
			continue
		}
		byID[systemID] = len(launchers)
		launchers = append(launchers, platforms.Launcher{
			ID:                  systemID,
			SystemID:            systemID,
			Folders:             []string{folder},
			Extensions:          systemExtensions(systemID),
			ScanExcludes:        esde.ScanExcludes(systemID),
			UsesRunningInstance: runningInstance,
			Launch:              p.launchContent,
			WaitForReady:        p.waitForReady,
			ReadyTimeout:        readyTimeoutFor,
			GameRunning:         p.gameRunning,
		})
	}
	for i := range launchers {
		if len(launchers[i].Extensions) == 0 {
			launchers[i].Test = isPlainFile
		}
	}

	launchers = append(launchers, platforms.Launcher{
		ID:            "Generic",
		Extensions:    []string{".sh"},
		AllowListOnly: true,
		Launch: func(_ *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
			if err := p.cmdExec().Start(context.Background(), path); err != nil {
				return nil, fmt.Errorf("failed to start command: %w", err)
			}
			return nil, nil //nolint:nilnil // Command launches don't return a process handle
		},
	})
	return launchers
}

// folderSystem resolves a top-level ROM folder to a Zaparoo system ID.
func (*Platform) folderSystem(fs afero.Fs, db *coreDB, folder string) (string, bool) {
	key := ""
	if lines, err := readLines(fs, filepath.Join(infoContentDir, folder, "core.cfg")); err == nil {
		key = lineAt(lines, 1)
	}
	if key == "" {
		if system, ok := db.systemForFolder(folder); ok {
			key = system
		}
	}
	if key == "" {
		return "", false
	}
	systemID, ok := systemKeyMap[key]
	return systemID, ok
}

// systemExtensions is every file extension EmulationStation folders know
// for a system.
func systemExtensions(systemID string) []string {
	var exts []string
	for _, folder := range esde.GetFoldersForSystemID(systemID) {
		for _, ext := range esde.SystemMap[folder].Extensions {
			if !slices.Contains(exts, ext) {
				exts = append(exts, ext)
			}
		}
	}
	slices.Sort(exts)
	return exts
}

func isPlainFile(_ *config.Instance, path string) bool {
	base := filepath.Base(path)
	if strings.HasPrefix(base, ".") {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
