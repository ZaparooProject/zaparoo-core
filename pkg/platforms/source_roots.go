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

package platforms

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
)

// SourceScheme is the virtual path scheme of media in a source root: a media
// folder a host application granted to Core, which Core cannot open through
// the operating system. A path in one is source://<root id>/<dir>/.../<file>,
// the multi-segment virtual path form (see virtualpath.CreateVirtualPathSegments).
const SourceScheme = "source"

// SourceRootPath returns the path of the source root the host knows as
// reference, source://<id>. The ID is a hash of the reference, so it is
// stable for as long as the host keeps the same reference, including after
// access is lost and granted again, and it reveals nothing of the reference.
func SourceRootPath(reference string) string {
	sum := sha256.Sum256([]byte("zaparoo-host-source-v1\x00" + reference))
	return SourceScheme + "://" + hex.EncodeToString(sum[:])
}

// SourceEntry is one entry of a source root directory.
type SourceEntry struct {
	// Name is the entry's own name, never a path.
	Name string
	// Size is the file size in bytes, or -1 when unknown.
	Size int64
	Dir  bool
}

// SourceRootReader is implemented by a platform whose media sits in source
// roots. Core treats a source root like a directory from RootDirs: it looks
// in it for the folders launchers declare, walks the matching system folders
// and indexes their files as source:// paths. The platform only lists and
// reads; it makes no decision about systems, media or identities.
//
// Launching is the platform's own: a launcher that matches a source:// path
// receives it and opens it through the host.
type SourceRootReader interface {
	// SourceRoots returns the source roots Core may index now, each made by
	// SourceRootPath. It is called once at the start of an index run.
	SourceRoots(ctx context.Context) ([]string, error)
	// ReadSourceDir lists one directory. path is a source root or a
	// directory below one, as a canonical multi-segment virtual path.
	ReadSourceDir(ctx context.Context, path string) ([]SourceEntry, error)
}
