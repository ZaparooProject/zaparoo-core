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
	"runtime"
	"strings"
)

// dirCache caches directory listings to avoid redundant filesystem operations
// during path discovery. Not safe for concurrent use — intended to be created
// and consumed within a single GetSystemPaths call.
type dirCache struct {
	entries map[string][]os.DirEntry
}

func newDirCache() *dirCache {
	return &dirCache{entries: make(map[string][]os.DirEntry)}
}

// list returns the directory entries for the given path, reading from cache
// if available or performing a readDirWithContext and caching the result.
func (c *dirCache) list(ctx context.Context, path string) ([]os.DirEntry, error) {
	if cached, ok := c.entries[path]; ok {
		return cached, nil
	}
	entries, err := readDirWithContext(ctx, path)
	if err != nil {
		return nil, err
	}
	c.entries[path] = entries
	return entries, nil
}

// findEntry does a case-insensitive lookup for name in the directory listing
// for dirPath. On Linux, prefers an exact case match. Returns the actual
// entry name, or empty string if not found.
func (c *dirCache) findEntry(ctx context.Context, dirPath, name string) (string, error) {
	entries, err := c.list(ctx, dirPath)
	if err != nil {
		return "", err
	}

	if runtime.GOOS == "linux" {
		for _, e := range entries {
			if e.Name() == name {
				return e.Name(), nil
			}
		}
	}

	for _, e := range entries {
		if strings.EqualFold(e.Name(), name) {
			return e.Name(), nil
		}
	}

	return "", nil
}
