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
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
)

// fingerprintVersion is bumped when a change to matching or to what is
// written means an unchanged system has to be scraped again.
const fingerprintVersion = 1

// sourceState describes every pack folder that serves targetID, in the order
// the step would read them, without parsing any of them and without a stat
// per image: one listing of names each. Metadata files are few, so each is
// named with its size and modification time. Images and manuals contribute
// how many there are and a digest of their names, which moves whenever a pack
// gains, loses or renames a file. A file replaced under the same name keeps
// the path already stored for it, so it needs no new scrape.
func (s *scraperImpl) sourceState(
	ctx context.Context, opts scraper.ScrapeOptions, targetID string,
) (string, error) {
	var state strings.Builder
	for _, sourceID := range sourceIDsForTarget(targetID) {
		for _, source := range s.sources[sourceID] {
			if err := waitForScrape(ctx, opts); err != nil {
				return "", err
			}
			_, _ = fmt.Fprintf(&state, "dir %q system=%s kind=%d image=%s metadata=%t\n",
				filepath.ToSlash(source.Path), source.SystemID, source.Kind, source.Image, source.Metadata)
			if err := s.describeSourceDir(ctx, source.Path, &state); err != nil {
				return "", err
			}
		}
	}
	return state.String(), nil
}

func (s *scraperImpl) describeSourceDir(ctx context.Context, dir string, state *strings.Builder) error {
	directory, err := s.fs.Open(dir)
	if err != nil {
		return fmt.Errorf("misterdocs: read source directory %q: %w", dir, err)
	}
	defer func() { _ = directory.Close() }()

	var metadata []string
	var files, names uint64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		batch, readErr := directory.Readdirnames(directoryReadBatch)
		for _, name := range batch {
			if !strings.EqualFold(filepath.Ext(name), ".tsv") {
				files++
				// Summed, so the digest does not depend on listing order.
				names += scraper.NameDigest(name)
				continue
			}
			info, statErr := lstat(s.fs, filepath.Join(dir, name))
			if statErr != nil {
				return fmt.Errorf("misterdocs: inspect %q: %w", name, statErr)
			}
			metadata = append(metadata, fmt.Sprintf("%q %d %d", name, info.Size(), info.ModTime().UnixNano()))
		}
		if errors.Is(readErr, io.EOF) || readErr == nil && len(batch) == 0 {
			break
		}
		if readErr != nil {
			return fmt.Errorf("misterdocs: read source directory %q: %w", dir, readErr)
		}
	}
	sort.Strings(metadata)
	_, _ = fmt.Fprintf(state, "%s\nfiles=%d names=%x\n", strings.Join(metadata, "\n"), files, names)
	return nil
}
