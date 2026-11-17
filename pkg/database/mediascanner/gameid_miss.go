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

package mediascanner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"runtime/debug"
	"sync"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/rs/zerolog/log"
)

// gameIDMissFormat is bumped when Core's own handling of disc identification
// changes in a way that could find an ID where it found none before.
const gameIDMissFormat = 1

const gameIDModulePath = "github.com/ZaparooProject/go-gameid"

// gameIDMissVersion identifies the code a recorded miss was produced by: a
// new go-gameid release may read a disc the previous one could not, so its
// misses are read again.
var gameIDMissVersion = sync.OnceValue(func() string {
	module := "unknown"
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, dep := range info.Deps {
			if dep.Path != gameIDModulePath {
				continue
			}
			module = dep.Version
			if dep.Replace != nil {
				module = dep.Replace.Version + "+" + dep.Replace.Path
			}
		}
	}
	return fmt.Sprintf("%d/%s", gameIDMissFormat, module)
})

// gameIDFileState is what a disc image looked like when it was read.
type gameIDFileState struct {
	// Listing is set for a disc that could not be read because a file it
	// names was not there, such as a cue sheet whose track is missing. It is
	// the system's walk marker at the time, so the disc is read again once
	// the system's files change.
	Listing string `json:"listing,omitempty"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"modTime"`
}

type gameIDMissRecord struct {
	Files   map[string]gameIDFileState `json:"files"`
	Version string                     `json:"version"`
}

// gameIDMisses remembers the disc images of one system that were read and
// carry no game ID. Reading an image is the slowest thing the scanner does to
// a file, and one that yielded nothing leaves no row behind to say so, so
// without this record every index reads it again and the system can never be
// left alone as unchanged.
//
// A miss stands while the file keeps its size and modification time and the
// identification code keeps its version.
type gameIDMisses struct {
	known map[string]gameIDFileState
	next  map[string]gameIDFileState
	// listing is the walk marker of the system being staged, or "" when its
	// walk cannot be trusted to describe the system's files.
	listing string
	stored  string
}

func loadGameIDMisses(ctx context.Context, db database.MediaDBI, systemID, listing string) *gameIDMisses {
	misses := &gameIDMisses{next: map[string]gameIDFileState{}, listing: listing}
	stored, err := db.ScanGameIDMisses(ctx, systemID)
	if err != nil {
		log.Debug().Err(err).Str("system", systemID).Msg("gameid miss record unreadable, reading discs again")
		return misses
	}
	misses.stored = stored
	if stored == "" {
		return misses
	}
	var record gameIDMissRecord
	if err := json.Unmarshal([]byte(stored), &record); err != nil {
		log.Debug().Err(err).Str("system", systemID).Msg("gameid miss record invalid, reading discs again")
		return misses
	}
	if record.Version == gameIDMissVersion() {
		misses.known = record.Files
	}
	return misses
}

func gameIDFileStateOf(path string) (gameIDFileState, bool) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return gameIDFileState{}, false
	}
	return gameIDFileState{Size: info.Size(), ModTime: info.ModTime().UnixNano()}, true
}

// isKnown reports whether path was already read in its current state and
// found to carry no game ID, and keeps the miss for the next record if so.
func (m *gameIDMisses) isKnown(path string) bool {
	if m == nil {
		return false
	}
	known, ok := m.known[path]
	if !ok {
		return false
	}
	state, ok := gameIDFileStateOf(path)
	if !ok || state.Size != known.Size || state.ModTime != known.ModTime {
		return false
	}
	if known.Listing != "" && known.Listing != m.listing {
		return false
	}
	state.Listing = known.Listing
	m.next[path] = state
	return true
}

// record notes that reading path found no game ID. before is the file's
// state when the read started. It reports whether the miss was recorded: a
// file that could not be opened or read, or that changed during the read,
// says nothing about its contents and is read again by the next index.
//
// A disc that names a file which is not there is recorded against the
// system's current files instead: nothing but a change to those files can
// make it readable.
func (m *gameIDMisses) record(path string, before gameIDFileState, beforeOK bool, identifyErr error) bool {
	if m == nil || !beforeOK {
		return false
	}
	listing := ""
	var pathErr *fs.PathError
	if errors.As(identifyErr, &pathErr) {
		if !errors.Is(identifyErr, fs.ErrNotExist) || m.listing == "" {
			return false
		}
		listing = m.listing
	}
	after, ok := gameIDFileStateOf(path)
	if !ok || after != before {
		return false
	}
	after.Listing = listing
	m.next[path] = after
	return true
}

// save stores the misses of the files this reconcile staged, in the open
// indexing transaction. Misses of files that are gone are dropped with it.
func (m *gameIDMisses) save(ctx context.Context, db database.MediaDBI, systemID string) error {
	if m == nil {
		return nil
	}
	value := ""
	if len(m.next) > 0 {
		// encoding/json writes map keys in sorted order, so equal records
		// encode equally and an unchanged record is not rewritten.
		encoded, err := json.Marshal(gameIDMissRecord{Version: gameIDMissVersion(), Files: m.next})
		if err != nil {
			return fmt.Errorf("failed to encode gameid miss record: %w", err)
		}
		value = string(encoded)
	}
	if value == m.stored {
		return nil
	}
	if err := db.SetScanGameIDMisses(ctx, systemID, value); err != nil {
		return fmt.Errorf("failed to store gameid miss record for %s: %w", systemID, err)
	}
	m.stored = value
	return nil
}
