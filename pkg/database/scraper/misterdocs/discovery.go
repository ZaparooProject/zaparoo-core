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

package misterdocs

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/spf13/afero"
)

const (
	artworkDirName = "Artwork"
	indexFileName  = "index.tsv"
)

type sourceKind uint8

const (
	sourceArtwork sourceKind = iota
	sourceManuals
)

type sourceDir struct {
	Path     string
	SystemID string
	Kind     sourceKind
}

func candidateDocsRoots(roots []string) []string {
	seen := make(map[string]struct{}, len(roots)*2)
	result := make([]string, 0, len(roots)*2)
	appendRoot := func(path string) {
		path = filepath.Clean(path)
		if path == "." || path == string(filepath.Separator) {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		result = append(result, path)
	}
	for _, root := range roots {
		root = filepath.Clean(root)
		if strings.EqualFold(filepath.Base(root), "games") {
			appendRoot(filepath.Join(filepath.Dir(root), "docs"))
		}
		appendRoot(filepath.Join(root, "docs"))
	}
	for _, root := range extraDocsRoots {
		appendRoot(root)
	}
	return result
}

// extraDocsRoots are mount points artwork packs tell consumers to probe that
// MiSTer's own games-folder list does not reach. Packs install with the
// Downloader's "pext" path, so docs can land on any USB drive. Roots that do
// not exist are skipped during discovery, so probing costs one stat each.
var extraDocsRoots = []string{ //nolint:gochecknoglobals // Fixed MiSTer mount points.
	"/media/usb6/docs",
	"/media/usb7/docs",
}

func discoverSources(fs afero.Fs, roots []string) ([]sourceDir, error) {
	var result []sourceDir
	seen := make(map[string]struct{})
	for _, docsRoot := range candidateDocsRoots(roots) {
		if !isRegularDir(fs, docsRoot) {
			continue
		}
		systems, err := afero.ReadDir(fs, docsRoot)
		if err != nil {
			continue
		}
		for _, systemEntry := range systems {
			if !systemEntry.IsDir() || systemEntry.Mode()&os.ModeSymlink != 0 {
				continue
			}
			systemDir := filepath.Join(docsRoot, systemEntry.Name())
			children, readErr := afero.ReadDir(fs, systemDir)
			if readErr != nil {
				continue
			}
			for _, child := range children {
				if !child.IsDir() || child.Mode()&os.ModeSymlink != 0 {
					continue
				}
				path := filepath.Join(systemDir, child.Name())
				var kind sourceKind
				var systemID string
				switch {
				case strings.EqualFold(child.Name(), artworkDirName) && hasArtworkContent(fs, path):
					kind = sourceArtwork
					systemID = resolveSourceSystem(systemEntry.Name(), "")
				case strings.Contains(strings.ToLower(child.Name()), "manual"):
					kind = sourceManuals
					systemID = resolveSourceSystem(systemEntry.Name(), child.Name())
				default:
					continue
				}
				if systemID == "" || !pathWithin(path, docsRoot) {
					continue
				}
				key := fmt.Sprintf("%d:%s:%s", kind, systemID, filepath.Clean(path))
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				result = append(result, sourceDir{Path: path, SystemID: systemID, Kind: kind})
			}
		}
	}
	return result, nil
}

func resolveSourceSystem(parent, manualDir string) string {
	candidates := make([]string, 0, 2)
	if manualDir != "" {
		name := strings.TrimSpace(manualDir)
		lower := strings.ToLower(name)
		if idx := strings.LastIndex(lower, "manual"); idx >= 0 {
			name = strings.TrimSpace(strings.Trim(name[:idx], "-_() "))
		}
		if name != "" {
			candidates = append(candidates, name)
		}
	}
	candidates = append(candidates, parent)

	for _, candidate := range candidates {
		if override := sourceSystemOverrides[normalizeSystemLabel(candidate)]; override != "" {
			return override
		}
		if sys, err := systemdefs.LookupSystem(candidate); err == nil {
			return sys.ID
		}
	}
	return ""
}

var sourceSystemOverrides = map[string]string{ //nolint:gochecknoglobals // Stable MiSTer folder aliases.
	"famicomdisksystem": systemdefs.SystemFDS,
	"segasg1000":        systemdefs.SystemSG1000,
}

func normalizeSystemLabel(value string) string {
	value = strings.ToLower(value)
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, value)
}

func pathWithin(path, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// hasArtworkContent reports whether an Artwork directory holds anything worth
// loading. The index resolves every dump not filed under its own key, but a
// pack shipped without one still serves exact-key images, so a directory with
// images and no index is a valid source rather than a directory to ignore.
func hasArtworkContent(fs afero.Fs, dir string) bool {
	if isRegularFile(fs, filepath.Join(dir, indexFileName)) {
		return true
	}
	directory, err := fs.Open(dir)
	if err != nil {
		return false
	}
	defer func() { _ = directory.Close() }()
	for {
		entries, readErr := directory.Readdir(directoryReadBatch)
		for _, entry := range entries {
			if !entry.IsDir() && entry.Mode()&os.ModeSymlink == 0 &&
				supportedImageExt(filepath.Ext(entry.Name())) {
				return true
			}
		}
		if readErr != nil {
			return false
		}
	}
}

func isRegularDir(fs afero.Fs, path string) bool {
	info, err := lstat(fs, path)
	return err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0
}

func isRegularFile(fs afero.Fs, path string) bool {
	info, err := lstat(fs, path)
	return err == nil && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0
}

func lstat(fs afero.Fs, path string) (os.FileInfo, error) {
	if lstater, ok := fs.(afero.Lstater); ok {
		info, _, err := lstater.LstatIfPossible(path)
		if err != nil {
			return nil, fmt.Errorf("lstat %q: %w", path, err)
		}
		return info, nil
	}
	info, err := fs.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat %q: %w", path, err)
	}
	return info, nil
}

func sourcesBySystem(sources []sourceDir) map[string][]sourceDir {
	result := make(map[string][]sourceDir)
	for _, source := range sources {
		result[source.SystemID] = append(result[source.SystemID], source)
	}
	return result
}

// artworkSiblings adds the shared-catalogue fallbacks the artwork pack format
// specifies and the general system definitions do not model. ScreenScraper
// splits dual-mode cartridges between the Game Boy and Game Boy Color
// catalogues on its own criteria, so each has to try the other, and Super Game
// Boy ships no pack of its own. Famicom Disk System is asymmetric on purpose:
// a disk release may borrow the cartridge box, but a cartridge must never
// receive the disk release's.
var artworkSiblings = map[string][]string{ //nolint:gochecknoglobals // Fixed artwork pack rules.
	systemdefs.SystemGameboy:      {systemdefs.SystemGameboyColor},
	systemdefs.SystemGameboyColor: {systemdefs.SystemGameboy},
	systemdefs.SystemSuperGameboy: {systemdefs.SystemGameboy, systemdefs.SystemGameboyColor},
	systemdefs.SystemFDS:          {systemdefs.SystemNES},
}

// artworkFallbackBlocks drops general system fallbacks between systems the
// artwork pack catalogues separately. Filling an SG-1000 gap with a
// ColecoVision box serves art for a different release, and the pack's own rule
// is that an absent image beats a wrong one.
var artworkFallbackBlocks = map[string]map[string]struct{}{ //nolint:gochecknoglobals // Fixed artwork pack rules.
	systemdefs.SystemSG1000:            {systemdefs.SystemColecoVision: {}},
	systemdefs.SystemNeoGeoPocketColor: {systemdefs.SystemNeoGeoPocket: {}},
}

func sourceIDsForTarget(targetID string) []string {
	seen := make(map[string]struct{})
	var result []string
	var visit func(string)
	visit = func(id string) {
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		result = append(result, id)
		blocked := artworkFallbackBlocks[id]
		if sys, err := systemdefs.GetSystem(id); err == nil {
			for _, fallback := range sys.Fallbacks {
				if _, ok := blocked[fallback]; ok {
					continue
				}
				visit(fallback)
			}
		}
		for _, sibling := range artworkSiblings[id] {
			visit(sibling)
		}
	}
	visit(targetID)
	return result
}

func orderedTargetSystems(indexed, requested []string) []string {
	indexedSet := make(map[string]struct{}, len(indexed))
	for _, id := range indexed {
		indexedSet[id] = struct{}{}
	}
	candidates := indexed
	if len(requested) > 0 {
		candidates = requested
	}
	seen := make(map[string]struct{}, len(candidates))
	result := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		sys, err := systemdefs.LookupSystem(candidate)
		if err != nil {
			continue
		}
		if _, ok := indexedSet[sys.ID]; !ok {
			continue
		}
		if _, ok := seen[sys.ID]; ok {
			continue
		}
		seen[sys.ID] = struct{}{}
		result = append(result, sys.ID)
	}
	if len(requested) == 0 {
		sort.Strings(result)
	}
	return result
}
