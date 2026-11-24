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

package simfixtures

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/scantest"
)

// DefaultBigDBSystems are the systems BigDB fills when BigDBOptions.Systems
// is empty: a spread of cartridge, disc and arcade systems.
var DefaultBigDBSystems = []string{ //nolint:gochecknoglobals // Default list, read-only.
	systemdefs.SystemNES, systemdefs.SystemSNES, systemdefs.SystemGenesis,
	systemdefs.SystemMasterSystem, systemdefs.SystemGameboy, systemdefs.SystemGameboyColor,
	systemdefs.SystemGBA, systemdefs.SystemNintendo64, systemdefs.SystemPSX, systemdefs.SystemArcade,
}

// BigDBOptions selects what BigDB stages.
type BigDBOptions struct {
	// Root is the library root the staged paths sit under. Nothing is
	// written there. Empty means "roms" beside the database's folder, the
	// simulator's layout (<root>/data/media.db, <root>/roms).
	Root string
	// Systems are system IDs or ES-DE folder names; DefaultBigDBSystems when
	// empty.
	Systems []string
	// Entries is the total number of media to stage, split evenly across
	// the systems.
	Entries int
	// Seed makes the staged names deterministic.
	Seed int64
}

// BigDBReport describes what BigDB staged.
type BigDBReport struct {
	Path      string        `json:"path"`
	Systems   []BigDBSystem `json:"systems"`
	Entries   int           `json:"entries"`
	ElapsedMS int64         `json:"elapsedMs"`
}

// BigDBSystem is one system's share of a BigDB run.
type BigDBSystem struct {
	SystemID       string `json:"systemId"`
	Folder         string `json:"folder"`
	Entries        int    `json:"entries"`
	TitlesInserted int64  `json:"titlesInserted"`
	MediaUpserted  int64  `json:"mediaUpserted"`
}

// dataDirPlatform is the least platform mediadb.OpenMediaDB needs: it only
// reads the data directory. Any other method panics, which a change to the
// opener would show at once in BigDB's test.
type dataDirPlatform struct {
	platforms.Platform
	dir string
}

func (dataDirPlatform) ID() string { return "simfixtures" }

func (p dataDirPlatform) Settings() platforms.Settings {
	return platforms.Settings{DataDir: p.dir, HostManagedPaths: true}
}

// BigDB opens (or creates) the MediaDB at mediaDBPath and stages
// opts.Entries media into it through the production staging pipeline,
// without touching the filesystem, for performance work. Each system is
// indexed as a full scan, so media already in the database for those systems
// and not staged now are marked missing. The database must not be open
// elsewhere. mediaDBPath must name the media database file (media.db).
func BigDB(ctx context.Context, mediaDBPath string, opts BigDBOptions) (*BigDBReport, error) {
	started := time.Now()
	if opts.Entries <= 0 {
		return nil, fmt.Errorf("simfixtures: Entries must be positive, got %d", opts.Entries)
	}
	if filepath.Base(mediaDBPath) != config.MediaDbFile {
		return nil, fmt.Errorf("simfixtures: media database path must end in %s, got %s",
			config.MediaDbFile, mediaDBPath)
	}
	names := opts.Systems
	if len(names) == 0 {
		names = DefaultBigDBSystems
	}
	targets, err := resolveTargets(names, false)
	if err != nil {
		return nil, err
	}
	targets = oneFolderPerSystem(targets)
	if len(targets) == 0 {
		return nil, errors.New("simfixtures: no systems selected")
	}
	root := opts.Root
	if root == "" {
		root = filepath.Join(filepath.Dir(filepath.Dir(mediaDBPath)), "roms")
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("simfixtures: resolve root: %w", err)
	}

	db, err := mediadb.OpenMediaDB(ctx, dataDirPlatform{dir: filepath.Dir(mediaDBPath)})
	if err != nil {
		return nil, fmt.Errorf("simfixtures: open media database: %w", err)
	}
	report := &BigDBReport{Path: mediaDBPath, Entries: opts.Entries}
	runErr := stageBigDB(ctx, db, root, targets, &opts, report)
	closeErr := db.Close()
	if runErr != nil {
		return nil, errors.Join(runErr, closeErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("simfixtures: close media database: %w", closeErr)
	}
	report.ElapsedMS = time.Since(started).Milliseconds()
	return report, nil
}

// oneFolderPerSystem keeps the first folder of each system ID: BigDB splits
// entries by system, and a system's paths all go under one folder.
func oneFolderPerSystem(targets []target) []target {
	seen := make(map[string]bool, len(targets))
	out := targets[:0]
	for _, t := range targets {
		if !seen[t.info.SystemID] {
			seen[t.info.SystemID] = true
			out = append(out, t)
		}
	}
	return out
}

func stageBigDB(
	ctx context.Context, db *mediadb.MediaDB, root string, targets []target, opts *BigDBOptions, report *BigDBReport,
) error {
	per, extra := opts.Entries/len(targets), opts.Entries%len(targets)
	ids := make([]string, 0, len(targets))
	for i, t := range targets {
		count := per
		if i < extra {
			count++
		}
		if count == 0 {
			continue
		}
		results, err := bigDBResults(root, t, count, opts.Seed)
		if err != nil {
			return fmt.Errorf("simfixtures: %s: %w", t.info.SystemID, err)
		}
		stats, err := scantest.Index(ctx, db, t.info.SystemID, database.ScanReconcileOpts{}, results...)
		if err != nil {
			return fmt.Errorf("simfixtures: index %s: %w", t.info.SystemID, err)
		}
		ids = append(ids, t.info.SystemID)
		report.Systems = append(report.Systems, BigDBSystem{
			SystemID: t.info.SystemID, Folder: t.folder, Entries: len(results),
			TitlesInserted: stats.TitlesInserted, MediaUpserted: stats.MediaUpserted,
		})
	}
	// The steps the indexer runs after its systems, so the database reads
	// like one a real index wrote.
	if err := db.RecomputeVersionGroups(ctx, ids); err != nil {
		return fmt.Errorf("simfixtures: regroup versions: %w", err)
	}
	if err := db.UpdateLastGenerated(); err != nil {
		return fmt.Errorf("simfixtures: record index time: %w", err)
	}
	if err := db.AnalyzeApproximate(); err != nil {
		return fmt.Errorf("simfixtures: analyze: %w", err)
	}
	if err := db.RebuildSlugSearchCache(); err != nil {
		return fmt.Errorf("simfixtures: rebuild slug search cache: %w", err)
	}
	if err := db.RebuildTagCache(); err != nil {
		return fmt.Errorf("simfixtures: rebuild tag cache: %w", err)
	}
	return nil
}

// bigDBResults names count media for one system: real titles where the
// GameDataBase has the system, then synthetic ones, each with a region tag.
func bigDBResults(root string, t target, count int, seed int64) ([]platforms.ScanResult, error) {
	rng := systemRand(seed, "bigdb/"+t.folder)
	opts := &Options{Source: SourceGameDatabase, PerSystem: count}
	titles, _, err := pickTitles(opts, t.info.SystemID, nil, rng)
	if err != nil {
		return nil, err
	}
	ext := romExtension(t.info.Extensions)
	if isArchiveExt(ext) {
		ext = ".zip"
	}
	dir := filepath.Join(root, t.folder)
	results := make([]platforms.ScanResult, len(titles))
	for i := range titles {
		stem := titles[i].stem
		if !titles[i].set {
			region := titles[i].region
			if region == "" {
				region = synthRegions[rng.IntN(len(synthRegions))]
			}
			stem += " (" + region + ")"
		}
		results[i] = platforms.ScanResult{Path: filepath.Join(dir, stem+ext)}
	}
	return results, nil
}
