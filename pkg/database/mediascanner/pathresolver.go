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
	"os"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
)

// pathResolver memoizes directory listings and resolved paths for the
// duration of a single path-discovery pass. FindPath resolves filesystem case
// by listing every parent directory of its input, so probing many candidate
// paths under shared roots re-reads the same large directories over and over —
// seconds per listing on cold SD-card storage. The memo is deliberately
// pass-scoped: a new resolver is created per discovery run, so there is no
// cross-run staleness to invalidate.
//
// Safe for concurrent use; concurrent lookups of the same directory share one
// read (the losers wait for the winner's result).
type pathResolver struct {
	readDirFn func(ctx context.Context, path string) ([]os.DirEntry, error)
	dirs      map[string]*dirListing
	resolved  map[string]resolvedPath
	mu        syncutil.Mutex
}

type dirListing struct {
	ready   chan struct{}
	err     error
	entries []os.DirEntry
}

type resolvedPath struct {
	err  error
	path string
}

func newPathResolver() *pathResolver {
	return &pathResolver{
		readDirFn: readDirWithContext,
		dirs:      map[string]*dirListing{},
		resolved:  map[string]resolvedPath{},
	}
}

// readDir returns the memoized successful listing for path. Concurrent callers
// share one in-flight read; failed reads are removed so a later caller can retry.
func (r *pathResolver) readDir(ctx context.Context, path string) ([]os.DirEntry, error) {
	r.mu.Lock()
	listing, ok := r.dirs[path]
	if !ok {
		listing = &dirListing{ready: make(chan struct{})}
		r.dirs[path] = listing
		r.mu.Unlock()

		listing.entries, listing.err = r.readDirFn(ctx, path)

		r.mu.Lock()
		if listing.err != nil {
			delete(r.dirs, path)
		}
		close(listing.ready)
		r.mu.Unlock()
		return listing.entries, listing.err
	}
	r.mu.Unlock()

	<-listing.ready
	return listing.entries, listing.err
}

// findPath is FindPath with pass-scoped memoization of both directory
// listings and full resolutions.
func (r *pathResolver) findPath(ctx context.Context, path string) (string, error) {
	r.mu.Lock()
	if res, ok := r.resolved[path]; ok {
		r.mu.Unlock()
		return res.path, res.err
	}
	r.mu.Unlock()

	resolved, err := findPathWithReadDir(ctx, path, r.readDir)
	if ctx.Err() == nil {
		r.mu.Lock()
		r.resolved[path] = resolvedPath{path: resolved, err: err}
		r.mu.Unlock()
	}
	return resolved, err
}
