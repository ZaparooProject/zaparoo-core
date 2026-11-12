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

// Command gamedatabase builds the bundles under pkg/database/gamedatabase/data
// from a checkout of PigSaint's GameDataBase
// (https://github.com/PigSaint/GameDataBase). It keeps the title, title
// screen, ID, region, release date, developer, publisher and tags of every
// row, drops the native-script title and hash columns, and merges the arcade
// manufacturer files into one bundle.
//
// The checkout must be at pinnedCommit: refreshing the data is a change to
// that constant plus the regenerated files.
//
// Usage: go run ./scripts/gamedatabase <checkout dir> pkg/database/gamedatabase/data
package main

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/zstdembed"
)

const (
	// pinnedCommit is the upstream commit the embedded data is built from,
	// and pinnedDate its commit date.
	pinnedCommit = "44574f6a7c089ceec88b1f69bcb3e4bbe0689b94"
	pinnedDate   = "2026-10-09"

	bundleSuffix = ".tsv.zst"
	versionFile  = "VERSION"

	maxRowsPerFile = 100_000
	maxCellBytes   = 4096
)

// upstreamHeader is the column layout the generator understands. A change
// upstream stops the build so the columns are looked at again.
var upstreamHeader = []string{
	"Title", "Title (exact)", "Title screen", "Title screen (exact)", "ID", "Region", "Release date",
	"Developer", "Publisher", "Tags", "MD5", "SHA1", "SHA256", "SHA512",
}

// keptColumns are the upstream column indexes written to a bundle, in order.
var keptColumns = []int{0, 2, 4, 5, 6, 7, 8, 9}

// bundles maps every upstream CSV to the bundle it is written to. Files that
// share a bundle are concatenated in file-name order.
var bundles = map[string]string{
	"arcade_capcom.csv":     "arcade",
	"arcade_irem.csv":       "arcade",
	"arcade_konami.csv":     "arcade",
	"arcade_nichibutsu.csv": "arcade",
	"arcade_nintendo.csv":   "arcade",
	"arcade_sega.csv":       "arcade",
	"arcade_snk.csv":        "arcade",
	"arcade_taito.csv":      "arcade",

	"console_nec_cdrom2.csv":                            "pcenginecd",
	"console_nec_pcengine_turbografx_supergrafx.csv":    "pcengine",
	"console_nec_pcfx.csv":                              "pcfx",
	"console_nintendo_64dd.csv":                         "n64dd",
	"console_nintendo_bandai_sufamiturbo.csv":           "sufami",
	"console_nintendo_famicom_nes.csv":                  "nes",
	"console_nintendo_famicomdisksystem.csv":            "fds",
	"console_nintendo_gameboy.csv":                      "gameboy",
	"console_nintendo_gameboyadvance.csv":               "gba",
	"console_nintendo_gameboycolor.csv":                 "gameboycolor",
	"console_nintendo_nintendo64.csv":                   "n64",
	"console_nintendo_satellaview.csv":                  "satellaview",
	"console_nintendo_superfamicom_snes.csv":            "snes",
	"console_nintendo_virtualboy.csv":                   "virtualboy",
	"console_pioneer_laseractive.csv":                   "laseractive",
	"console_sega_gamegear.csv":                         "gamegear",
	"console_sega_markIII_mastersystem.csv":             "mastersystem",
	"console_sega_megacd_segacd.csv":                    "megacd",
	"console_sega_megadrive_genesis.csv":                "megadrive",
	"console_sega_saturn.csv":                           "saturn",
	"console_sega_sg1000_sc3000_othellomultivision.csv": "sg1000",
	"console_sega_super32x.csv":                         "sega32x",
	"console_snk_neogeopocket_neogeopocketcolor.csv":    "neogeopocket",
}

// checkoutCommit reads the commit a checkout's HEAD points at without
// running git.
func checkoutCommit(dir string) (string, error) {
	gitDir := filepath.Join(dir, ".git")
	head, err := os.ReadFile(filepath.Join(gitDir, "HEAD")) //nolint:gosec // operator-supplied checkout
	if err != nil {
		return "", fmt.Errorf("read checkout HEAD: %w", err)
	}
	ref, isRef := strings.CutPrefix(strings.TrimSpace(string(head)), "ref: ")
	if !isRef {
		return strings.TrimSpace(string(head)), nil
	}
	loose := filepath.Join(gitDir, filepath.FromSlash(ref))
	if data, readErr := os.ReadFile(loose); readErr == nil { //nolint:gosec // as above
		return strings.TrimSpace(string(data)), nil
	}
	packed, err := os.ReadFile(filepath.Join(gitDir, "packed-refs")) //nolint:gosec // as above
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", ref, err)
	}
	for line := range strings.SplitSeq(string(packed), "\n") {
		if hash, name, ok := strings.Cut(line, " "); ok && name == ref {
			return hash, nil
		}
	}
	return "", fmt.Errorf("resolve %s: not found", ref)
}

// clean makes one cell safe for a tab-separated line: control characters and
// every kind of whitespace become a single space.
func clean(cell string) (string, error) {
	if !utf8.ValidString(cell) {
		return "", errors.New("invalid UTF-8")
	}
	if len(cell) > maxCellBytes {
		return "", fmt.Errorf("cell of %d bytes", len(cell))
	}
	cell = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return ' '
		}
		return r
	}, cell)
	return strings.Join(strings.Fields(cell), " "), nil
}

// convert reads one upstream CSV and appends its rows to out.
func convert(name string, in io.Reader, out *bytes.Buffer) (int, error) {
	reader := csv.NewReader(in)
	reader.FieldsPerRecord = len(upstreamHeader)
	header, err := reader.Read()
	if err != nil {
		return 0, fmt.Errorf("%s: read header: %w", name, err)
	}
	if !slices.Equal(header, upstreamHeader) {
		return 0, fmt.Errorf("%s: unexpected header %q", name, header)
	}
	rows := 0
	for {
		record, readErr := reader.Read()
		if errors.Is(readErr, io.EOF) {
			return rows, nil
		}
		if readErr != nil {
			return 0, fmt.Errorf("%s: %w", name, readErr)
		}
		rows++
		if rows > maxRowsPerFile {
			return 0, fmt.Errorf("%s: more than %d rows", name, maxRowsPerFile)
		}
		for i, column := range keptColumns {
			cell, cleanErr := clean(record[column])
			if cleanErr != nil {
				return 0, fmt.Errorf("%s row %d column %q: %w", name, rows, upstreamHeader[column], cleanErr)
			}
			if i == 0 && cell == "" {
				return 0, fmt.Errorf("%s row %d: empty title", name, rows)
			}
			if i > 0 {
				out.WriteByte('\t')
			}
			out.WriteString(cell)
		}
		out.WriteByte('\n')
	}
}

func run(checkout, outDir string) error {
	commit, err := checkoutCommit(checkout)
	if err != nil {
		return err
	}
	if commit != pinnedCommit {
		return fmt.Errorf("checkout is at %s, generator is pinned to %s", commit, pinnedCommit)
	}
	entries, err := os.ReadDir(checkout)
	if err != nil {
		return fmt.Errorf("read checkout: %w", err)
	}
	var files []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".csv") {
			continue
		}
		if _, known := bundles[entry.Name()]; !known {
			return fmt.Errorf("%s is not in the bundle table", entry.Name())
		}
		files = append(files, entry.Name())
	}
	slices.Sort(files)
	if len(files) != len(bundles) {
		return fmt.Errorf("checkout has %d of the %d expected CSV files", len(files), len(bundles))
	}

	contents := make(map[string]*bytes.Buffer)
	total := 0
	for _, name := range files {
		data, readErr := os.ReadFile(filepath.Join(checkout, name)) //nolint:gosec // operator-supplied checkout
		if readErr != nil {
			return fmt.Errorf("read %s: %w", name, readErr)
		}
		bundle := bundles[name]
		if contents[bundle] == nil {
			contents[bundle] = &bytes.Buffer{}
		}
		rows, convErr := convert(name, bytes.NewReader(data), contents[bundle])
		if convErr != nil {
			return convErr
		}
		total += rows
	}

	if mkErr := os.MkdirAll(outDir, 0o755); mkErr != nil { //nolint:gosec // checked-in data directory
		return fmt.Errorf("create %s: %w", outDir, mkErr)
	}
	old, err := filepath.Glob(filepath.Join(outDir, "*"+bundleSuffix))
	if err != nil {
		return fmt.Errorf("list %s: %w", outDir, err)
	}
	for _, stale := range old {
		if rmErr := os.Remove(stale); rmErr != nil { //nolint:gosec // a bundle in the operator's data directory
			return fmt.Errorf("remove %s: %w", stale, rmErr)
		}
	}
	packedTotal := 0
	for bundle, raw := range contents {
		packed, packErr := zstdembed.Compress(raw.Bytes())
		if packErr != nil {
			return fmt.Errorf("compress %s: %w", bundle, packErr)
		}
		packedTotal += len(packed)
		target := filepath.Join(outDir, bundle+bundleSuffix)
		if wrErr := os.WriteFile(target, packed, 0o644); wrErr != nil { //nolint:gosec // checked-in data file
			return fmt.Errorf("write %s: %w", target, wrErr)
		}
	}
	version := pinnedCommit + " " + pinnedDate + "\n"
	//nolint:gosec // checked-in data file
	if wrErr := os.WriteFile(filepath.Join(outDir, versionFile), []byte(version), 0o644); wrErr != nil {
		return fmt.Errorf("write version: %w", wrErr)
	}
	//nolint:forbidigo // CLI output
	_, _ = fmt.Printf("wrote %d rows in %d bundles (%d bytes) from GameDataBase %s\n",
		total, len(contents), packedTotal, pinnedCommit)
	return nil
}

func main() {
	if len(os.Args) != 3 {
		_, _ = fmt.Fprintln(os.Stderr, "usage: gamedatabase <checkout dir> <data dir>")
		os.Exit(2)
	}
	if err := run(os.Args[1], os.Args[2]); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
