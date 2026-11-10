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

package lakka

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper/libretrothumbs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/retroarch"
	"github.com/rs/zerolog/log"
)

// playlistLauncherPrefix starts the ID of each system's playlist launcher.
const playlistLauncherPrefix = "RetroArchPlaylist"

// scanPlaylists adds a system's RetroArch playlist entries to the files the
// folder scan found. Only files inside the index roots are added, and a file
// already found is left as it is.
func (p *Platform) scanPlaylists(
	ctx context.Context,
	cfg *config.Instance,
	systemID string,
	results []platforms.ScanResult,
) ([]platforms.ScanResult, error) {
	items := p.playlists.load().bySystem[systemID]
	if len(items) == 0 {
		return results, nil
	}

	seen := make(map[string]bool, len(results)+len(items))
	for i := range results {
		seen[results[i].Path] = true
	}
	roots := p.RootDirs(cfg)
	added := 0
	for i := range items {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("scan retroarch playlists: %w", err)
		}
		item := &items[i]
		if seen[item.Path] || (item.Inner != "" && seen[filepath.Join(item.Path, item.Inner)]) {
			continue
		}
		if !inAnyRoot(roots, item.Path) {
			log.Debug().Str("path", item.Path).Msg("skipping retroarch playlist entry outside the index roots")
			continue
		}
		if info, err := p.fs.Stat(item.Path); err != nil || info.IsDir() {
			log.Debug().Str("path", item.Path).Msg("skipping missing retroarch playlist entry")
			continue
		}
		seen[item.Path] = true
		results = append(results, platforms.ScanResult{Path: item.Path, Name: item.Name})
		added++
	}
	log.Debug().Str("system", systemID).Int("added", added).Msg("indexed retroarch playlist entries")
	return results, nil
}

func inAnyRoot(roots []string, path string) bool {
	for _, root := range roots {
		rel, err := filepath.Rel(filepath.Clean(root), path)
		if err == nil && rel != "." && rel != ".." && !filepath.IsAbs(rel) &&
			!strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// playlistLaunchers match playlist entries the system launchers' folders do
// not cover. They are scan-only: Core hands the launch to the system's own
// launcher, which runs it with the playlist's core.
func (p *Platform) playlistLaunchers(systems map[string]bool) []platforms.Launcher {
	ids := make([]string, 0, len(systems))
	for _, id := range libretrothumbs.SupportedSystems() {
		if systems[id] {
			ids = append(ids, id)
		}
	}
	launchers := make([]platforms.Launcher, 0, len(ids))
	for _, id := range ids {
		launchers = append(launchers, platforms.Launcher{
			ID:                 playlistLauncherPrefix + id,
			SystemID:           id,
			ScanOnly:           true,
			SkipFilesystemScan: true,
			Test: func(_ *config.Instance, path string) bool {
				_, ok := p.playlists.cached().lookup(path, id)
				return ok
			},
		})
	}
	return launchers
}

// playlistCommand wraps a launcher's command builder so a playlist entry of
// systemID starts with the core its playlist recorded. Entries without a
// usable core, and everything else, use the launcher's own core.
func (p *Platform) playlistCommand(
	build func(*config.Instance, string, *platforms.LaunchOptions) (*platforms.LaunchCommand, error),
	systemID string,
) func(*config.Instance, string, *platforms.LaunchOptions) (*platforms.LaunchCommand, error) {
	return func(cfg *config.Instance, path string, opts *platforms.LaunchOptions) (*platforms.LaunchCommand, error) {
		if core, ok := p.playlistCore(path, systemID); ok {
			spec := retroarch.BuildCommand(p.retroArchOptions(), retroarch.CoreLaunch{Core: core}, path)
			log.Info().Str("path", path).Str("core", core).Msg("launching with the retroarch playlist core")
			return &platforms.LaunchCommand{Executable: spec.Name, Args: spec.Args, Env: spec.Env}, nil
		}
		return build(cfg, path, opts)
	}
}

// playlistCore returns the core file, inside the cores directory, that the
// playlist entry at path recorded. Only the core's file name is used, so a
// playlist cannot point RetroArch at a library outside the cores directory.
func (p *Platform) playlistCore(path, systemID string) (string, bool) {
	item, ok := p.playlists.load().lookup(path, systemID)
	if !ok || item.Core == "" {
		return "", false
	}
	core := filepath.Base(filepath.Clean(item.Core))
	if !strings.HasSuffix(core, "_libretro.so") {
		log.Debug().Str("core", item.Core).Msg("ignoring retroarch playlist core that is not a libretro core")
		return "", false
	}
	if info, err := p.fs.Stat(filepath.Join(p.coresDir, core)); err != nil || info.IsDir() {
		log.Debug().Str("core", item.Core).Msg("retroarch playlist core is not installed, using the system core")
		return "", false
	}
	if info, err := p.fs.Stat(p.retroArchExec); err != nil || info.IsDir() || info.Mode().Perm()&0o111 == 0 {
		return "", false
	}
	return core, true
}
