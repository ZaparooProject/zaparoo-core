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

// Package gamedatabase holds an embedded copy of PigSaint's GameDataBase, a
// per-release catalog of console and arcade games with their genre, player,
// input and franchise tags, release date, developer and publisher. The data
// is split into one bundle per platform so a caller only decodes the
// platforms it needs. It is generated from a pinned upstream commit by
// scripts/gamedatabase; see LICENSE in this directory for its terms.
package gamedatabase

import (
	"bufio"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/zstdembed"
)

//go:embed data
var embedded embed.FS

const (
	dataDir      = "data"
	bundleSuffix = ".tsv.zst"
	versionFile  = "VERSION"
	// Fields is the number of tab-separated columns in a bundle row.
	Fields = 8
	// maxLineBytes bounds one row. The longest upstream row is a few
	// kilobytes; a longer line means the file is not what the generator wrote.
	maxLineBytes = 64 << 10
)

// SameAsTitle is the upstream marker for a title-screen cell that repeats
// the title.
const SameAsTitle = "="

// ErrUnknownBundle reports a bundle name with no embedded file.
var ErrUnknownBundle = errors.New("unknown gamedatabase bundle")

// Row is one release as upstream lists it. ForEach reuses the Row it passes
// to its callback, so copy a row that has to outlive the call.
type Row struct {
	Title       string
	TitleScreen string
	ID          string
	Region      string
	Date        string
	Developer   string
	Publisher   string
	Tags        string
}

// ScreenTitle is the in-game title, resolving the same-as-title marker.
func (r *Row) ScreenTitle() string {
	if r.TitleScreen == SameAsTitle {
		return r.Title
	}
	return r.TitleScreen
}

// Version is the upstream commit and commit date the data was generated
// from, as "<commit> <date>".
func Version() string {
	data, err := embedded.ReadFile(path.Join(dataDir, versionFile))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// Bundles lists the embedded bundle names in sorted order.
func Bundles() []string {
	entries, err := fs.ReadDir(embedded, dataDir)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if name, ok := strings.CutSuffix(entry.Name(), bundleSuffix); ok && !entry.IsDir() {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

// ForEach decodes one bundle and calls fn for each row in upstream order. It
// holds one row in memory at a time. A non-nil error from fn stops the read
// and is returned.
func ForEach(bundle string, fn func(row *Row) error) error {
	if bundle == "" || strings.ContainsAny(bundle, `/\.`) {
		return fmt.Errorf("%w: %q", ErrUnknownBundle, bundle)
	}
	file, err := embedded.Open(path.Join(dataDir, bundle+bundleSuffix))
	if err != nil {
		return fmt.Errorf("%w: %q", ErrUnknownBundle, bundle)
	}
	defer func() { _ = file.Close() }()
	zr, err := zstdembed.NewReader(file)
	if err != nil {
		return fmt.Errorf("open gamedatabase bundle %s: %w", bundle, err)
	}
	defer func() { _ = zr.Close() }()

	scanner := bufio.NewScanner(zr)
	scanner.Buffer(make([]byte, 0, 16<<10), maxLineBytes)
	var row Row
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), "\t")
		if len(fields) != Fields || fields[0] == "" {
			return fmt.Errorf("gamedatabase bundle %s: malformed row", bundle)
		}
		row = Row{
			Title: fields[0], TitleScreen: fields[1], ID: fields[2], Region: fields[3],
			Date: fields[4], Developer: fields[5], Publisher: fields[6], Tags: fields[7],
		}
		if err := fn(&row); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read gamedatabase bundle %s: %w", bundle, err)
	}
	return nil
}
