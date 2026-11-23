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

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/sim"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/simfixtures"
)

func cmdFixtures(ctx context.Context, g globals, args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("fixtures: subcommand required (gen, index, bigdb)")
	}
	sub, args := args[0], args[1:]
	switch sub {
	case "gen":
		return fixturesGen(ctx, g, args, stdout)
	case "index":
		return fixturesIndex(ctx, g, args, stdout)
	case "bigdb":
		return fixturesBigDB(ctx, g, args, stdout)
	default:
		return fmt.Errorf("fixtures: unknown subcommand %q (gen, index, bigdb)", sub)
	}
}

func fixturesGen(ctx context.Context, g globals, args []string, stdout io.Writer) error {
	layout, err := sim.NewLayout(g.root)
	if err != nil {
		return fmt.Errorf("sim layout: %w", err)
	}
	if err = layout.CheckRoot(); err != nil {
		return fmt.Errorf("check sim root: %w", err)
	}
	if err = layout.Ensure(); err != nil {
		return fmt.Errorf("create sim root: %w", err)
	}
	fs := newFlags("fixtures gen")
	opts := simfixtures.Options{Root: layout.ROMs}
	systems := fs.String("systems", "", "comma-separated ES-DE folders or system IDs")
	fs.BoolVar(&opts.All, "all", false, "every system")
	fs.IntVar(&opts.PerSystem, "per-system", 20, "titles per system")
	fs.Int64Var(&opts.Seed, "seed", 1, "seed; the same seed writes the same library")
	fs.StringVar(&opts.Source, "source", "gamedatabase", "title source: gamedatabase, synthetic or dats")
	fs.StringVar(&opts.DatsDir, "dats", "", "directory of DAT files (with --source dats)")
	fs.BoolVar(&opts.Realistic, "realistic", false, "No-Intro/TOSEC style regions, revisions, discs, hacks")
	fs.BoolVar(&opts.Zips, "zips", false, "write some titles as zip archives")
	fs.BoolVar(&opts.Gamelist, "gamelist", false, "write gamelist.xml per system")
	fs.BoolVar(&opts.MediaImages, "media-images", false, "write artwork in media/ folders")
	fs.BoolVar(&opts.Subfolders, "subfolders", false, "put some titles in subfolders")
	fs.BoolVar(&opts.EdgeCases, "edge-cases", false, "unicode, long names, ignored and hidden files")
	if parseErr := parse(fs, args); parseErr != nil {
		return parseErr
	}
	if opts.DatsDir != "" && opts.Source == "gamedatabase" {
		opts.Source = "dats"
	}
	opts.Systems = splitList(*systems)
	if !opts.All && len(opts.Systems) == 0 {
		return errors.New("fixtures gen: pass --systems or --all")
	}
	report, err := simfixtures.Generate(ctx, opts)
	if err != nil {
		return fmt.Errorf("generate fixtures: %w", err)
	}
	if g.json {
		return writeJSON(stdout, report)
	}
	_, _ = fmt.Fprintf(stdout, "wrote %d files for %d systems under %s\n",
		report.Files, len(report.Systems), report.Root)
	return nil
}

// fixturesIndex runs a real media.generate and waits for it to finish.
func fixturesIndex(ctx context.Context, g globals, args []string, stdout io.Writer) error {
	fs := newFlags("fixtures index")
	timeout := fs.Duration("timeout", 10*time.Minute, "give up after this long")
	systems := fs.String("systems", "", "comma-separated system IDs (default: all)")
	if err := parse(fs, args); err != nil {
		return err
	}
	in, err := connect(g)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()

	start := time.Now()
	status, err := sim.NewAPI(in.state.APIURL).Index(ctx, splitList(*systems))
	if err != nil {
		return fmt.Errorf("index media: %w", err)
	}
	if g.json {
		return writeJSON(stdout, status)
	}
	total := 0
	if status.TotalMedia != nil {
		total = *status.TotalMedia
	}
	_, _ = fmt.Fprintf(stdout, "indexed %d media in %s\n", total, time.Since(start).Round(time.Millisecond))
	return nil
}

func fixturesBigDB(ctx context.Context, g globals, args []string, stdout io.Writer) error {
	layout, err := sim.NewLayout(g.root)
	if err != nil {
		return fmt.Errorf("sim layout: %w", err)
	}
	if st, stErr := layout.ReadState(); stErr == nil && processAlive(st.PID) {
		return errors.New("fixtures bigdb: stop the sim first; it writes the MediaDB directly")
	}
	if err = layout.CheckRoot(); err != nil {
		return fmt.Errorf("check sim root: %w", err)
	}
	if err = layout.Ensure(); err != nil {
		return fmt.Errorf("create sim root: %w", err)
	}
	fs := newFlags("fixtures bigdb")
	var opts simfixtures.BigDBOptions
	fs.IntVar(&opts.Entries, "entries", 100000, "media entries to stage")
	fs.Int64Var(&opts.Seed, "seed", 1, "seed")
	systems := fs.String("systems", "", "comma-separated system IDs (default: a spread)")
	if parseErr := parse(fs, args); parseErr != nil {
		return parseErr
	}
	opts.Systems = splitList(*systems)
	dbPath := filepath.Join(layout.Data, "media.db")
	report, err := simfixtures.BigDB(ctx, dbPath, opts)
	if err != nil {
		return fmt.Errorf("big db: %w", err)
	}
	return output(g, stdout, report, fmt.Sprintf("staged %d entries into %s", opts.Entries, dbPath))
}
