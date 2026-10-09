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

package scraper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/fnv"
	"io"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/spf13/afero"
)

// listingBatch is how many names one directory read asks for.
const listingBatch = 256

// ListingState describes a directory by how many entries it holds and a
// digest of their names, from one listing and no stat. It moves whenever an
// entry is added, removed or renamed, which is when a scraper that matches
// files by name can find something new. A directory that cannot be read is
// described as such, so one that appears later reads as a change.
func ListingState(fs afero.Fs, dir string) string {
	directory, err := fs.Open(dir)
	if err != nil {
		return "unreadable"
	}
	defer func() { _ = directory.Close() }()
	var entries, names uint64
	for {
		batch, readErr := directory.Readdirnames(listingBatch)
		for _, name := range batch {
			entries++
			// Summed, so the digest does not depend on listing order.
			names += NameDigest(name)
		}
		if errors.Is(readErr, io.EOF) || readErr == nil && len(batch) == 0 {
			break
		}
		if readErr != nil {
			return "unreadable"
		}
	}
	return fmt.Sprintf("entries=%d names=%x", entries, names)
}

// NameDigest hashes one entry name for an order-independent listing digest.
func NameDigest(name string) uint64 {
	digest := fnv.New64a()
	_, _ = digest.Write([]byte(name))
	return digest.Sum64()
}

// SystemFingerprint joins the state of a scraper's source for one system with
// that system's library revision. A fill-missing run that finds the
// fingerprint it stored last time would resolve the same records to the same
// rows and add nothing, so it can leave the system alone.
//
// sourceState is whatever the scraper's result depends on outside the
// database, described without doing the scrape's own work. version is the
// scraper's own, bumped when a change to its matching or its writes means an
// unchanged system has to be scraped again.
func SystemFingerprint(version int, sourceState string, libraryRevision int64) string {
	sum := sha256.Sum256(fmt.Appendf(nil, "v%d\n%s\nlibrary=%d", version, sourceState, libraryRevision))
	return hex.EncodeToString(sum[:])
}

// SystemUnchanged reports whether a system's source and library are as the
// scraper's last completed run left them. It costs two keyed reads. A failure to tell is not an error
// for the run: the scraper just does its work.
func SystemUnchanged(
	ctx context.Context, db database.MediaDBI, scraperID, systemID string, version int, sourceState string,
) bool {
	stored, err := db.GetScrapeFingerprint(ctx, scraperID, systemID)
	if err != nil || stored == "" {
		return false
	}
	revision, err := db.LibraryRevision(ctx, systemID)
	if err != nil {
		return false
	}
	return stored == SystemFingerprint(version, sourceState, revision)
}

// RememberSystem records the state a scraper completed a system in. Pass the
// source state read before the scraper loaded its source, so a source that
// changed while the system was being scraped no longer matches and is read
// again next time.
func RememberSystem(
	ctx context.Context, db database.MediaDBI, scraperID, systemID string, version int, sourceState string,
) error {
	revision, err := db.LibraryRevision(ctx, systemID)
	if err != nil {
		return fmt.Errorf("%s: read library revision of %s: %w", scraperID, systemID, err)
	}
	fingerprint := SystemFingerprint(version, sourceState, revision)
	if err := db.SetScrapeFingerprint(ctx, scraperID, systemID, fingerprint); err != nil {
		return fmt.Errorf("%s: remember %s: %w", scraperID, systemID, err)
	}
	return nil
}
