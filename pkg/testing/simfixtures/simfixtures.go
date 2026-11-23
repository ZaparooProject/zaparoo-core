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

// Package simfixtures writes a realistic, deterministic ROM library to disk
// for the desktop simulator and for in-process tests, and fills a MediaDB
// with a large library for performance work.
//
// Generate lays the library out the way an ES-DE install does: one folder
// per system, named as in esde.SystemMap, under a root. ROM files are
// zero-byte placeholders (zip archives are real but tiny), so Core's real
// indexer and scrapers read the library exactly as they would a user's. Where
// the embedded GameDataBase has a system's games, titles are real ones, so the
// gamedatabase scraper matches them; elsewhere they are fictional.
//
// It lives under pkg/testing beside scantest because BigDB imports the
// production staging pipeline (mediascanner); mediascanner's own tests must
// not import this package.
package simfixtures

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"os"
	"slices"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esde"
)

// Title sources for Options.Source.
const (
	SourceGameDatabase = "gamedatabase"
	SourceSynthetic    = "synthetic"
	SourceDATs         = "dats"
)

// DefaultPerSystem is the number of titles per system when Options.PerSystem
// is zero.
const DefaultPerSystem = 20

// maxSamples bounds SystemReport.Samples.
const maxSamples = 5

// Options selects what Generate writes.
type Options struct {
	// Root is the library root, e.g. <simroot>/roms. Each system gets one
	// ES-DE folder under it.
	Root string
	// Source is where titles come from: SourceGameDatabase (the default),
	// SourceSynthetic or SourceDATs. A system the source has nothing for
	// falls back to synthetic titles; SystemReport.Source says which was used.
	Source string
	// DatsDir is the directory of Logiqx XML DAT files read for SourceDATs.
	// Each DAT's header name says its system, as in No-Intro and Redump DATs.
	DatsDir string
	// Systems are ES-DE folder names (snes) or Zaparoo system IDs (SNES),
	// matched case-insensitively. A folder name writes that folder; a system
	// ID writes its canonical folder.
	Systems []string
	// PerSystem is the number of titles per system, DefaultPerSystem when
	// zero. Realistic and EdgeCases add files beyond it: extra regions,
	// revisions, discs and the edge-case files.
	PerSystem int
	// Seed makes the output deterministic: the same options and seed write
	// the same files.
	Seed int64
	// All writes every system ID in esde.SystemMap, in its canonical folder.
	All bool
	// Realistic decorates names the way No-Intro, Redump and GoodTools sets
	// do: regions (from the GameDataBase row where there is one), languages,
	// revisions, [!] dumps, multi-disc games with an .m3u playlist on CD
	// systems, hacks and translations.
	Realistic bool
	// Zips writes some titles as zip archives holding one ROM, on systems
	// whose extensions include .zip. Systems that only take archives (arcade)
	// always get zips.
	Zips bool
	// Gamelist writes an ES-DE gamelist.xml in each system folder.
	Gamelist bool
	// MediaImages writes small PNGs where the media-folder scraper looks:
	// <system folder>/media/<kind>/<path relative to the folder>.png.
	MediaImages bool
	// Subfolders puts some titles in nested folders.
	Subfolders bool
	// EdgeCases adds files that exercise the scanner: CJK and accented
	// names, very long names, brackets and ampersands, an uppercase
	// extension, and files Core must skip (dot-directories, a .zaparooignore
	// folder, __MACOSX, AppleDouble files, wrong extensions).
	EdgeCases bool
}

// Report describes what Generate wrote.
type Report struct {
	Root   string `json:"root"`
	Source string `json:"source"`
	// Systems are in the order generated: sorted by system ID, then folder.
	Systems []SystemReport `json:"systems"`
	Seed    int64          `json:"seed"`
	// Files counts every file written, including gamelists and images.
	Files int `json:"files"`
	// Media counts the files Core's indexer is expected to index.
	Media int `json:"media"`
}

// SystemReport describes one system folder.
type SystemReport struct {
	Folder   string `json:"folder"`
	SystemID string `json:"systemId"`
	// Source is the title source actually used for this system.
	Source string `json:"source"`
	// Extension is the ROM extension chosen for the system's plain files.
	Extension string `json:"extension"`
	// Samples are a few media paths, relative to the system folder with
	// forward slashes.
	Samples []string `json:"samples"`
	// MediaPaths are the absolute paths of every file Core is expected to
	// index, sorted. Not serialised: a large library would bloat the JSON.
	MediaPaths []string `json:"-"`
	// Titles is the number of distinct games written.
	Titles int `json:"titles"`
	// Files counts every file written under the folder.
	Files int `json:"files"`
	// Media counts files Core's indexer is expected to index; see
	// MediaPaths. It assumes the platform indexes zip files as files, not
	// as folders (Settings.ZipsAsDirs false, as on the simulator).
	Media int `json:"media"`
	// Skipped counts edge-case files written for Core to ignore.
	Skipped int `json:"skipped"`
	// Images counts artwork files written under media/.
	Images int `json:"images"`
	// Gamelist is true when a gamelist.xml was written.
	Gamelist bool `json:"gamelist"`
}

// target is one system folder to generate.
type target struct {
	folder string
	info   esde.SystemInfo
}

// Generate writes the library described by opts under opts.Root and reports
// what it wrote. Existing files under a system folder are overwritten, not
// removed first.
//
//nolint:gocritic // hugeParam: by value so defaults never write back to the caller.
func Generate(ctx context.Context, opts Options) (*Report, error) {
	if opts.Root == "" {
		return nil, errors.New("simfixtures: Root is required")
	}
	if opts.PerSystem < 0 {
		return nil, fmt.Errorf("simfixtures: PerSystem must not be negative, got %d", opts.PerSystem)
	}
	if opts.PerSystem == 0 {
		opts.PerSystem = DefaultPerSystem
	}
	if opts.Source == "" {
		opts.Source = SourceGameDatabase
	}
	var dats datIndex
	switch opts.Source {
	case SourceGameDatabase, SourceSynthetic:
	case SourceDATs:
		var err error
		if dats, err = loadDATs(opts.DatsDir); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("simfixtures: unknown source %q (want %s, %s or %s)",
			opts.Source, SourceGameDatabase, SourceSynthetic, SourceDATs)
	}
	targets, err := resolveTargets(opts.Systems, opts.All)
	if err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return nil, errors.New("simfixtures: no systems selected; set Systems or All")
	}
	if err := os.MkdirAll(opts.Root, 0o750); err != nil {
		return nil, fmt.Errorf("simfixtures: create root: %w", err)
	}

	report := &Report{Root: opts.Root, Source: opts.Source, Seed: opts.Seed}
	for _, t := range targets {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("simfixtures: %w", err)
		}
		sys, err := generateSystem(ctx, &opts, t, dats)
		if err != nil {
			return nil, fmt.Errorf("simfixtures: %s: %w", t.folder, err)
		}
		report.Systems = append(report.Systems, *sys)
		report.Files += sys.Files
		report.Media += sys.Media
	}
	return report, nil
}

// resolveTargets turns folder names and system IDs into system folders,
// sorted by system ID then folder, without duplicates.
func resolveTargets(systems []string, all bool) ([]target, error) {
	idFolders := make(map[string][]string)
	ids := make(map[string]string) // lower-case ID to ID
	for folder, info := range esde.SystemMap {
		idFolders[info.SystemID] = append(idFolders[info.SystemID], folder)
		ids[strings.ToLower(info.SystemID)] = info.SystemID
	}
	seen := make(map[string]bool)
	var out []target
	add := func(folder string) {
		if seen[folder] {
			return
		}
		seen[folder] = true
		out = append(out, target{folder: folder, info: esde.SystemMap[folder]})
	}
	if all {
		for _, id := range ids {
			add(CanonicalFolder(id))
		}
	}
	for _, name := range systems {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		lower := strings.ToLower(name)
		if _, ok := esde.SystemMap[lower]; ok {
			add(lower)
			continue
		}
		id, ok := ids[lower]
		if !ok {
			return nil, fmt.Errorf("simfixtures: %q is not an ES-DE folder or system ID in esde.SystemMap", name)
		}
		add(CanonicalFolder(id))
	}
	slices.SortFunc(out, func(a, b target) int {
		if c := strings.Compare(a.info.SystemID, b.info.SystemID); c != 0 {
			return c
		}
		return strings.Compare(a.folder, b.folder)
	})
	return out, nil
}

// CanonicalFolder returns the ES-DE folder Generate writes for a system ID:
// the folder named the lower-cased ID if there is one, else the first of the
// ID's folders in sorted order. It returns "" for an ID no folder maps to.
// The ID is matched case-insensitively.
func CanonicalFolder(systemID string) string {
	folders := esde.GetFoldersForSystemID(systemID)
	if len(folders) == 0 {
		return ""
	}
	lower := strings.ToLower(systemID)
	if slices.Contains(folders, lower) {
		return lower
	}
	slices.Sort(folders)
	return folders[0]
}

// isArchiveExt reports an extension Generate writes as an archive.
func isArchiveExt(ext string) bool {
	return ext == ".zip" || ext == ".7z"
}

// genericExts are extensions many systems share, passed over when a system
// has a more telling one (megadrive's .gen over .bin).
var genericExts = []string{".bin", ".rom", ".img"} //nolint:gochecknoglobals // Static table.

// romExtension picks the extension for a system's plain ROM files: .chd on
// a disc system that takes it, else the first specific extension, else the
// first that is not an archive or playlist, else .zip for a system that
// only takes archives.
func romExtension(exts []string) string {
	lowered := make([]string, len(exts))
	for i, ext := range exts {
		lowered[i] = strings.ToLower(ext)
	}
	if slices.Contains(lowered, ".chd") {
		return ".chd"
	}
	usable := func(ext string) bool { return !isArchiveExt(ext) && ext != ".m3u" }
	for _, ext := range lowered {
		if usable(ext) && !slices.Contains(genericExts, ext) {
			return ext
		}
	}
	for _, ext := range lowered {
		if usable(ext) {
			return ext
		}
	}
	if slices.Contains(lowered, ".zip") || len(lowered) == 0 {
		return ".zip"
	}
	return lowered[0]
}

// isDiscSystem reports a system whose games are disc images.
func isDiscSystem(exts []string) bool {
	return slices.ContainsFunc(exts, func(ext string) bool {
		switch strings.ToLower(ext) {
		case ".chd", ".cue", ".gdi", ".cdi":
			return true
		default:
			return false
		}
	})
}

func hasExt(exts []string, want string) bool {
	return slices.ContainsFunc(exts, func(ext string) bool { return strings.EqualFold(ext, want) })
}

// systemRand returns the generator for one system folder, derived from the
// seed and the folder so adding a system leaves the others unchanged.
func systemRand(seed int64, key string) *rand.Rand {
	h := fnv.New64a()
	_, _ = h.Write([]byte(key))
	return rand.New(rand.NewPCG(uint64(seed), h.Sum64())) //nolint:gosec // Deterministic fixtures, not security.
}
