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

// Package pathutil provides path resolution utilities with no dependencies on
// other Zaparoo packages. This allows both config and helpers to use these
// functions without circular imports.
package pathutil

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/afero"
)

var mediaPathURI = regexp.MustCompile(`^([a-zA-Z][a-zA-Z0-9+.-]*)://(.+)$`)

// WriteFileAtomic writes a sibling temporary file, syncs and closes it, then
// replaces the destination. Failures before replacement preserve the old file.
// This does not sync the parent directory or promise power-loss durability.
func WriteFileAtomic(fs afero.Fs, path string, data []byte, mode os.FileMode) (err error) {
	path, err = atomicWriteDestination(fs, path)
	if err != nil {
		return err
	}
	file, err := afero.TempFile(fs, filepath.Dir(path), ".config-*")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	tempPath := file.Name()
	closed := false
	replaced := false
	defer func() {
		if !closed {
			err = errors.Join(err, file.Close())
		}
		if !replaced {
			if removeErr := fs.Remove(tempPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				err = errors.Join(err, fmt.Errorf("remove temporary file: %w", removeErr))
			}
		}
	}()
	if chmodErr := fs.Chmod(tempPath, mode); chmodErr != nil {
		return fmt.Errorf("set temporary file permissions: %w", chmodErr)
	}
	written, err := file.Write(data)
	if err != nil {
		return fmt.Errorf("write temporary file: %w", err)
	}
	if written != len(data) {
		return fmt.Errorf("write temporary file: %w", io.ErrShortWrite)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync temporary file: %w", err)
	}
	closeErr := file.Close()
	closed = true
	if closeErr != nil {
		return fmt.Errorf("close temporary file: %w", closeErr)
	}
	if err := fs.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace file: %w", err)
	}
	replaced = true
	return nil
}

// Follow final-component symlinks just as WriteFile does, rather than replacing
// the link itself. Parent directory resolution remains the filesystem's job.
func atomicWriteDestination(fs afero.Fs, path string) (string, error) {
	lstater, ok := fs.(afero.Lstater)
	if !ok {
		return path, nil
	}
	for range 40 {
		info, _, err := lstater.LstatIfPossible(path)
		if errors.Is(err, os.ErrNotExist) {
			return path, nil
		}
		if err != nil {
			return "", fmt.Errorf("inspect replacement destination: %w", err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			return path, nil
		}
		reader, ok := fs.(afero.LinkReader)
		if !ok {
			return "", errors.New("filesystem cannot resolve replacement symlink")
		}
		target, err := reader.ReadlinkIfPossible(path)
		if err != nil {
			return "", fmt.Errorf("resolve replacement symlink: %w", err)
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(path), target)
		}
		path = target
	}
	return "", errors.New("too many replacement symlinks")
}

// ExeDir returns the directory containing the currently running executable.
// Returns an empty string if the executable path cannot be determined.
func ExeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Dir(exe)
}

// CanonicalMediaPath normalizes a persisted media path to media.db's storage
// format. Filesystem paths are cleaned and stored with forward slashes; URI
// paths are kept unchanged.
func CanonicalMediaPath(path string) string {
	if path == "" || mediaPathURI.MatchString(path) {
		return path
	}
	path = strings.ReplaceAll(path, `\`, string(filepath.Separator))
	return filepath.ToSlash(filepath.Clean(path))
}

// ResolveRelativePath resolves a path relative to ExeDir if it is not
// absolute. Absolute and empty paths are returned unchanged.
func ResolveRelativePath(path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	exeDir := ExeDir()
	if exeDir == "" {
		return path
	}
	return filepath.Join(exeDir, path)
}

// ErrNotLocalFile reports a path that is not a plain file reachable from its
// root without leaving it or following a link.
var ErrNotLocalFile = errors.New("not a regular file under the root")

// LocalFileUnder resolves rel, a slash-separated relative path, against root
// and returns the joined path with its file info. Every directory on the way
// must be a real directory and the last element a regular file: an absolute
// path, a ".." step, a symlink or any other link type is refused, so the
// result cannot point outside root.
func LocalFileUnder(root, rel string) (path string, info os.FileInfo, err error) {
	if root == "" || rel == "" || strings.Contains(rel, `\`) {
		return "", nil, ErrNotLocalFile
	}
	rel = filepath.FromSlash(rel)
	if !filepath.IsLocal(rel) {
		return "", nil, ErrNotLocalFile
	}
	parts := strings.Split(filepath.Clean(rel), string(filepath.Separator))
	current := root
	for i, part := range parts {
		current = filepath.Join(current, part)
		info, err = os.Lstat(current)
		if err != nil {
			return "", nil, fmt.Errorf("stat %s: %w", current, err)
		}
		last := i == len(parts)-1
		if (last && !info.Mode().IsRegular()) || (!last && info.Mode().Type() != os.ModeDir) {
			return "", nil, ErrNotLocalFile
		}
	}
	return current, info, nil
}
