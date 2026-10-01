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
	"errors"
	"fmt"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
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

// ErrNotSourcePath reports a path that is not a source root or a path below one.
var ErrNotSourcePath = errors.New("not a source root path")

// IsSourcePath reports whether path is a source root or a path below one.
func IsSourcePath(path string) bool {
	prefix := SourceScheme + "://"
	return len(path) > len(prefix) && strings.EqualFold(path[:len(prefix)], prefix)
}

// SourceLocation splits a source root, or a path below one, into the root's
// ID and the decoded segments below it. A root has no segments. A path below
// a root must be in its canonical form.
func SourceLocation(path string) (id string, segments []string, err error) {
	if !IsSourcePath(path) {
		return "", nil, ErrNotSourcePath
	}
	rest := path[len(SourceScheme)+3:]
	if !strings.Contains(rest, "/") {
		return rest, nil, nil
	}
	parsed, err := virtualpath.ParseVirtualPathSegments(path)
	if err != nil {
		return "", nil, fmt.Errorf("parse source path %s: %w", path, err)
	}
	return parsed.ID, parsed.Segments, nil
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

// SourceFileReader is implemented by a platform that can also return a source
// root file's bytes, not just list its directories: a separate, optional
// capability from SourceRootReader, since returning file content needs a real
// read of the host's data (e.g. through Binder on Android), not just a
// directory listing.
type SourceFileReader interface {
	// ReadSourceFile returns up to limit+1 bytes of the file at path (a
	// source root path below a root, never a bare root), so a caller can
	// detect an oversized file without reading all of it.
	ReadSourceFile(ctx context.Context, path string, limit int64) ([]byte, error)
}
