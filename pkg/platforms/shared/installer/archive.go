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

package installer

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/rs/zerolog/log"
)

// maxArchiveEntries bounds how many entries an archive download may hold. A
// multi-disc game is a handful of files; tens of thousands is a hostile or
// broken archive, and each entry costs a file creation.
const maxArchiveEntries = 10000

// ErrUnsafeArchive is an archive entry that cannot be written safely: one
// that names a path outside the extraction folder, a link, or a device.
var ErrUnsafeArchive = errors.New("unsafe archive entry")

// errArchiveTooLarge is an archive that expands past the space allowed for
// it, whatever its headers claimed.
var errArchiveTooLarge = errors.New("archive expands past the free space allowed for it")

// safeArchiveEntryName reports whether a zip entry name is a relative path
// that stays inside the extraction folder on every OS Core runs on. Entry
// names come from the server, so anything that could be read as a different
// path elsewhere (a drive letter, a backslash separator, a stream name) is
// refused rather than interpreted.
func safeArchiveEntryName(name string) bool {
	name = strings.TrimSuffix(name, "/")
	if name == "" || strings.HasPrefix(name, "/") {
		return false
	}
	if strings.ContainsAny(name, "\\:") {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
	}
	if path.Clean(name) != name {
		return false
	}
	for _, seg := range strings.Split(name, "/") {
		if seg == "" || seg == "." || seg == ".." || isWindowsDeviceName(seg) {
			return false
		}
	}
	return true
}

// isWindowsDeviceName reports whether a path segment names a DOS device on
// Windows, such as NUL or com1.txt. Such a segment is refused on every OS so
// an archive installs the same way everywhere.
func isWindowsDeviceName(seg string) bool {
	// Windows ignores an extension and trailing dots and spaces here.
	if i := strings.IndexByte(seg, '.'); i >= 0 {
		seg = seg[:i]
	}
	seg = strings.ToUpper(strings.TrimRight(seg, " "))
	switch seg {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
		return true
	}
	if len(seg) < 4 || (seg[:3] != "COM" && seg[:3] != "LPT") {
		return false
	}
	switch seg[3:] {
	case "1", "2", "3", "4", "5", "6", "7", "8", "9", "\u00b9", "\u00b2", "\u00b3":
		return true
	}
	return false
}

type ctxReader struct {
	ctx context.Context //nolint:containedctx // scoped to one copy
	r   io.Reader
}

func (c *ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, fmt.Errorf("extraction cancelled: %w", err)
	}
	n, err := c.r.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		return n, fmt.Errorf("read archive entry: %w", err)
	}
	return n, err //nolint:wrapcheck // io.EOF must reach io.Copy unwrapped
}

// extractZip writes the regular files and folders in the zip at zipPath into
// dest, which must not already hold any of them. At most budget bytes are
// written; an archive that expands further is stopped as soon as it passes
// the limit, since its size headers are untrusted.
func extractZip(ctx context.Context, zipPath, dest string, budget int64) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer func() { _ = r.Close() }()

	if len(r.File) > maxArchiveEntries {
		return fmt.Errorf("%w: %d entries", ErrUnsafeArchive, len(r.File))
	}

	//nolint:gosec // Safe: other processes may see installed media
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return fmt.Errorf("create extraction folder: %w", err)
	}

	remaining := budget
	for _, f := range r.File {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("extraction cancelled: %w", err)
		}
		if !safeArchiveEntryName(f.Name) {
			return fmt.Errorf("%w: %q", ErrUnsafeArchive, f.Name)
		}
		rel := filepath.FromSlash(strings.TrimSuffix(f.Name, "/"))
		if !filepath.IsLocal(rel) {
			return fmt.Errorf("%w: %q", ErrUnsafeArchive, f.Name)
		}
		target := filepath.Join(dest, rel)
		if !isWithinDir(dest, target) {
			return fmt.Errorf("%w: %q", ErrUnsafeArchive, f.Name)
		}

		mode := f.Mode()
		switch {
		case mode.IsDir():
			//nolint:gosec // Safe: other processes may see installed media
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("create folder %q: %w", f.Name, err)
			}
			continue
		case !mode.IsRegular():
			return fmt.Errorf("%w: %q is not a regular file", ErrUnsafeArchive, f.Name)
		}

		if remaining < 0 || f.UncompressedSize64 > uint64(remaining) {
			return errArchiveTooLarge
		}
		written, err := extractZipFile(ctx, f, target, remaining)
		if err != nil {
			return err
		}
		remaining -= written
	}
	return nil
}

func extractZipFile(ctx context.Context, f *zip.File, target string, limit int64) (int64, error) {
	//nolint:gosec // Safe: other processes may see installed media
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return 0, fmt.Errorf("create folder for %q: %w", f.Name, err)
	}
	rc, err := f.Open()
	if err != nil {
		return 0, fmt.Errorf("open %q: %w", f.Name, err)
	}
	defer func() { _ = rc.Close() }()

	// O_EXCL: an entry repeated in the archive, or two names that are the
	// same file on a case-insensitive filesystem, fails instead of one
	// silently replacing the other.
	//nolint:gosec // target was checked to sit inside the extraction folder
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return 0, fmt.Errorf("create %q: %w", f.Name, err)
	}
	// Read one byte past the limit so an over-long entry is told apart from
	// one that fills it exactly.
	lr := io.LimitReader(&ctxReader{ctx: ctx, r: rc}, limit+1)
	n, copyErr := io.Copy(out, lr)
	closeErr := out.Close()
	if copyErr != nil {
		return 0, fmt.Errorf("extract %q: %w", f.Name, copyErr)
	}
	if closeErr != nil {
		return 0, fmt.Errorf("close %q: %w", f.Name, closeErr)
	}
	if n > limit {
		return 0, errArchiveTooLarge
	}
	return n, nil
}

// launchExtRank orders the files a multi-file game could be started from.
// A playlist names every disc, so it wins; then the formats that describe a
// disc's tracks, which must be opened instead of the tracks themselves.
var launchExtRank = []string{".m3u", ".cue", ".gdi", ".ccd", ".mds"}

// maxLaunchSearchDepth bounds how far into an extracted game the launch file
// is looked for. RomM nests a game's files at most a level or two down.
const maxLaunchSearchDepth = 4

// pickLaunchFile returns the file under dir to launch the game from: one with
// an extension in exts (any extension when exts is empty), preferring a
// playlist, then a disc descriptor, then the shallowest file, then the name.
func pickLaunchFile(dir string, exts []string) (string, error) {
	accept := extSet(exts)

	type candidate struct {
		path  string
		rank  int
		depth int
	}
	var found []candidate
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, relErr := filepath.Rel(dir, p)
		if relErr != nil {
			return fmt.Errorf("relative path: %w", relErr)
		}
		depth := 0
		if rel != "." {
			depth = strings.Count(rel, string(filepath.Separator)) + 1
		}
		if d.IsDir() {
			if depth >= maxLaunchSearchDepth {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(p))
		if len(accept) > 0 {
			if _, ok := accept[ext]; !ok {
				return nil
			}
		}
		rank := slices.Index(launchExtRank, ext)
		if rank < 0 {
			rank = len(launchExtRank)
		}
		found = append(found, candidate{path: p, rank: rank, depth: depth})
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("search %s: %w", dir, err)
	}
	if len(found) == 0 {
		return "", fmt.Errorf("no launchable file in %s", dir)
	}
	slices.SortFunc(found, func(a, b candidate) int {
		if a.rank != b.rank {
			return a.rank - b.rank
		}
		if a.depth != b.depth {
			return a.depth - b.depth
		}
		return strings.Compare(a.path, b.path)
	})
	return found[0].path, nil
}

// extSet normalizes launch extensions to a lowercase, dot-prefixed set.
func extSet(exts []string) map[string]struct{} {
	accept := make(map[string]struct{}, len(exts))
	for _, e := range exts {
		e = strings.ToLower(e)
		if e != "" && !strings.HasPrefix(e, ".") {
			e = "." + e
		}
		accept[e] = struct{}{}
	}
	return accept
}

// maxPlaylistSize bounds how much of an extracted .m3u is read. A playlist
// is a few lines of file names.
const maxPlaylistSize = 64 * 1024

// dropSingleDiscPlaylists removes each .m3u under dir that names exactly one
// disc, when that disc is a file in the extracted game the system can open
// itself. RomM adds a playlist to every multi-file download, and a game
// folder holding both a playlist and the disc it names is indexed as two
// games. A playlist naming several discs is kept: it is how discs are
// swapped. Anything unexpected keeps the playlist.
func dropSingleDiscPlaylists(dir string, exts []string) {
	var playlists []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, relErr := filepath.Rel(dir, p)
		if relErr != nil {
			return fmt.Errorf("relative path: %w", relErr)
		}
		if d.IsDir() {
			if rel != "." && strings.Count(rel, string(filepath.Separator))+1 >= maxLaunchSearchDepth {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() && strings.EqualFold(filepath.Ext(p), ".m3u") {
			playlists = append(playlists, rel)
		}
		return nil
	})
	if err != nil {
		log.Warn().Err(err).Msgf("error checking playlists in %s", dir)
		return
	}
	if len(playlists) == 0 {
		return
	}

	// Through a root, a name in a playlist cannot reach outside the game.
	root, err := os.OpenRoot(dir)
	if err != nil {
		log.Warn().Err(err).Msgf("error checking playlists in %s", dir)
		return
	}
	defer func() { _ = root.Close() }()
	accept := extSet(exts)
	for _, rel := range playlists {
		disc, ok := singlePlaylistEntry(root, rel)
		if !ok {
			continue
		}
		target := filepath.Join(filepath.Dir(rel), filepath.FromSlash(disc))
		if len(accept) > 0 {
			if _, opens := accept[strings.ToLower(filepath.Ext(target))]; !opens {
				continue
			}
		}
		info, statErr := root.Lstat(target)
		if statErr != nil || !info.Mode().IsRegular() {
			continue
		}
		if rmErr := root.Remove(rel); rmErr != nil {
			log.Warn().Err(rmErr).Msgf("error removing single-disc playlist: %s", filepath.Join(dir, rel))
		}
	}
}

// singlePlaylistEntry returns the only entry of the .m3u at rel under root,
// ignoring blank lines and comments. It answers false for a playlist with any
// other number of entries, or one that cannot be read in full.
func singlePlaylistEntry(root *os.Root, rel string) (string, bool) {
	f, err := root.Open(rel)
	if err != nil {
		return "", false
	}
	defer func() { _ = f.Close() }()

	data, err := io.ReadAll(io.LimitReader(f, maxPlaylistSize+1))
	if err != nil || len(data) > maxPlaylistSize {
		return "", false
	}
	var entry string
	count := 0
	for _, line := range strings.Split(strings.TrimPrefix(string(data), "\ufeff"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		count++
		entry = line
	}
	if count != 1 {
		return "", false
	}
	return entry, true
}
