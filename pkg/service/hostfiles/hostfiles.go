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

// Package hostfiles lets a client browse files on the Core host without ever
// naming an absolute path. Core offers a fixed set of roots; a client names a
// root by ID and a path under it, and every path is resolved against its root
// again, symlinks included, before anything is listed or read.
package hostfiles

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
)

const (
	// MaxDirEntries caps how many entries of one folder are read. A larger
	// folder is listed in part and reported as truncated.
	MaxDirEntries = 10000
	// ReadTimeout bounds one request's filesystem work, waiting for a slot
	// included, so a share that stopped answering fails the request instead
	// of holding it.
	ReadTimeout = 5 * time.Second
	// maxConcurrentReads caps filesystem reads in flight. A read that timed
	// out keeps its slot until the filesystem answers.
	maxConcurrentReads = 8
	// maxConcurrentRootReads caps the reads in flight under one root, so a
	// root that stopped answering cannot take every slot from the others.
	maxConcurrentRootReads = 2
	// ImagesRootID is the ID of the root for Core's image folder.
	ImagesRootID = "images"
)

var (
	ErrUnknownRoot = errors.New("unknown root")
	ErrInvalidPath = errors.New("path must be relative to its root and stay inside it")
	ErrNotFound    = errors.New("no such file or folder")
	ErrNotDir      = errors.New("path is not a folder")
	ErrNotFile     = errors.New("path is not a file of the requested kind")
	ErrBusy        = errors.New("too many folders are being read; try again")
	ErrTimeout     = errors.New("the folder did not answer in time")
)

// imageExtensions are the files an image browse lists and serves.
var imageExtensions = []string{".jpg", ".jpeg", ".png", ".gif", ".webp"}

// IsImageName reports whether a file name has an image extension.
func IsImageName(name string) bool {
	return slices.Contains(imageExtensions, strings.ToLower(filepath.Ext(name)))
}

// Root is one folder a client may browse.
type Root struct {
	ID   string
	Name string
	Path string
}

// Entry is one folder or file in a listing.
type Entry struct {
	Modified time.Time
	Name     string
	// Path is the entry's path under its root, with forward slashes.
	Path  string
	Size  int64
	IsDir bool
}

// Listing is one folder's entries of the requested kind, folders first.
type Listing struct {
	Entries []Entry
	// Truncated reports a folder with more entries than were read.
	Truncated bool
}

// Browser resolves and lists paths under a set of roots. Every method that
// touches the filesystem runs it under ReadTimeout and a read slot.
type Browser struct {
	// afterCheck, when set by a test, runs between a path's check and its
	// open.
	afterCheck func()
	roots      []Root
	timeout    time.Duration
}

// readSlots counts the filesystem reads in flight, across browsers, in all
// and per root path.
type readSlots struct {
	perRoot map[string]int
	// freed is closed and replaced whenever a slot is released.
	freed chan struct{}
	mu    syncutil.Mutex
	total int
}

//nolint:gochecknoglobals // one budget for every browser
var reads = &readSlots{perRoot: make(map[string]int), freed: make(chan struct{})}

// acquire takes a slot for a read under root, waiting for one until ctx ends.
func (r *readSlots) acquire(ctx context.Context, root string) error {
	for {
		r.mu.Lock()
		if r.total < maxConcurrentReads && r.perRoot[root] < maxConcurrentRootReads {
			r.total++
			r.perRoot[root]++
			r.mu.Unlock()
			return nil
		}
		freed := r.freed
		r.mu.Unlock()
		select {
		case <-freed:
		case <-ctx.Done():
			return ErrBusy
		}
	}
}

func (r *readSlots) release(root string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.total--
	r.perRoot[root]--
	if r.perRoot[root] <= 0 {
		delete(r.perRoot, root)
	}
	close(r.freed)
	r.freed = make(chan struct{})
}

// bounded runs fn, which reads under root, in a slot and under the browser's
// timeout. A call that times out returns, but fn keeps its slot until the
// filesystem answers it.
func bounded[T any](ctx context.Context, b *Browser, root string, fn func() (T, error)) (T, error) {
	return boundedDiscard(ctx, b, root, fn, nil)
}

// boundedDiscard is bounded for a value that holds a resource: when the call
// has already returned, a value fn produces later is handed to discard.
func boundedDiscard[T any](
	ctx context.Context, b *Browser, root string, fn func() (T, error), discard func(T),
) (T, error) {
	var zero T
	ctx, cancel := context.WithTimeout(ctx, b.timeout)
	defer cancel()
	if err := reads.acquire(ctx, root); err != nil {
		if parentErr := context.Cause(ctx); parentErr != nil && !errors.Is(parentErr, context.DeadlineExceeded) {
			return zero, fmt.Errorf("wait for a read slot: %w", parentErr)
		}
		return zero, err
	}
	type result struct {
		err   error
		value T
	}
	// Unbuffered: either the caller takes the result or, once it has
	// returned, ctx is done and the result is discarded.
	done := make(chan result)
	go func() {
		defer reads.release(root)
		value, err := fn()
		select {
		case done <- result{value: value, err: err}:
		case <-ctx.Done():
			if err == nil && discard != nil {
				discard(value)
			}
		}
	}()
	select {
	case r := <-done:
		return r.value, r.err
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return zero, ErrTimeout
		}
		return zero, fmt.Errorf("read folder: %w", ctx.Err())
	}
}

// rootID names a root by its path, so the ID is stable between requests and
// reveals nothing a client could use as a path.
func rootID(path string) string {
	sum := sha256.Sum256([]byte(path))
	return "r" + hex.EncodeToString(sum[:5])
}

// NewBrowser returns a browser over the index roots and Core's image folder.
// A folder named twice appears once. It does not touch the filesystem: Roots
// leaves out the folders that do not exist.
func NewBrowser(indexRoots []string, imageDir string) *Browser {
	b := &Browser{timeout: ReadTimeout}
	seen := make(map[string]struct{})
	add := func(id, path string) {
		if path == "" || !filepath.IsAbs(path) {
			return
		}
		path = filepath.Clean(path)
		if _, dup := seen[path]; dup {
			return
		}
		seen[path] = struct{}{}
		if id == "" {
			id = rootID(path)
		}
		b.roots = append(b.roots, Root{ID: id, Name: path, Path: path})
	}
	add(ImagesRootID, imageDir)
	for _, root := range indexRoots {
		add("", root)
	}
	return b
}

// Roots returns the folders a client may browse: those that exist and
// answered in time. Each is checked on its own, so one that stopped answering
// is left out without holding up the rest.
func (b *Browser) Roots(ctx context.Context) []Root {
	exists := make([]bool, len(b.roots))
	done := make(chan struct{}, len(b.roots))
	for i := range b.roots {
		go func() {
			defer func() { done <- struct{}{} }()
			path := b.roots[i].Path
			isDir, err := bounded(ctx, b, path, func() (bool, error) {
				info, err := os.Stat(path)
				return err == nil && info.IsDir(), nil
			})
			exists[i] = err == nil && isDir
		}()
	}
	for range b.roots {
		<-done
	}
	roots := make([]Root, 0, len(b.roots))
	for i, root := range b.roots {
		if exists[i] {
			roots = append(roots, root)
		}
	}
	return roots
}

// root returns the root with the given ID.
func (b *Browser) root(id string) (Root, error) {
	index := slices.IndexFunc(b.roots, func(r Root) bool { return r.ID == id })
	if index < 0 {
		return Root{}, ErrUnknownRoot
	}
	return b.roots[index], nil
}

// resolveUnder returns the real paths of root and of clean under it, and
// refuses a path that leaves the root, by ".." or through a symlink. It
// touches the filesystem, so it runs inside bounded. The result is only a
// check: open it through an os.Root at rootReal.
func resolveUnder(rootPath, clean string) (rootReal, resolved string, err error) {
	rootReal, err = filepath.EvalSymlinks(rootPath)
	if err != nil {
		return "", "", ErrNotFound
	}
	resolved, err = filepath.EvalSymlinks(filepath.Join(rootReal, clean))
	if err != nil {
		return "", "", ErrNotFound
	}
	if !within(rootReal, resolved) {
		return "", "", ErrInvalidPath
	}
	return rootReal, resolved, nil
}

// cleanRel validates a client's path and returns it in the host's form.
func cleanRel(rel string) (string, error) {
	if strings.ContainsRune(rel, 0) || strings.Contains(rel, `\`) {
		return "", ErrInvalidPath
	}
	if rel == "" || rel == "." {
		return "", nil
	}
	if strings.HasPrefix(rel, "/") {
		return "", ErrInvalidPath
	}
	for _, part := range strings.Split(rel, "/") {
		if part == ".." {
			return "", ErrInvalidPath
		}
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if filepath.IsAbs(clean) || filepath.VolumeName(clean) != "" {
		return "", ErrInvalidPath
	}
	return clean, nil
}

// within reports whether path is root or lies under it.
func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) &&
		!filepath.IsAbs(rel)
}

// Image is an image file opened under a browse root. The caller closes File.
type Image struct {
	Info os.FileInfo
	File *os.File
	// Path is the file's real path when it was opened, for naming it. Read
	// the file through File: Path is not checked again.
	Path string
}

// OpenImage opens a regular file with an image extension under the root with
// the given ID. rel uses forward slashes. A path that leaves its root, by
// ".." or through a symlink, is refused, and the file is opened through the
// root, so a folder swapped for a link after the check cannot lead outside.
func (b *Browser) OpenImage(ctx context.Context, id, rel string) (*Image, error) {
	root, err := b.root(id)
	if err != nil {
		return nil, err
	}
	clean, err := cleanRel(rel)
	if err != nil {
		return nil, err
	}
	return boundedDiscard(ctx, b, root.Path, func() (*Image, error) {
		rootReal, path, resolveErr := resolveUnder(root.Path, clean)
		if resolveErr != nil {
			return nil, resolveErr
		}
		b.checked()
		dirRoot, openErr := os.OpenRoot(rootReal)
		if openErr != nil {
			return nil, ErrNotFound
		}
		defer func() { _ = dirRoot.Close() }()
		relPath, relErr := filepath.Rel(rootReal, path)
		if relErr != nil {
			return nil, ErrInvalidPath
		}
		// Checked before the open, which would wait on a named pipe.
		info, statErr := dirRoot.Stat(relPath)
		if statErr != nil {
			return nil, ErrNotFound
		}
		if !info.Mode().IsRegular() || !IsImageName(path) {
			return nil, ErrNotFile
		}
		file, openErr := dirRoot.Open(relPath)
		if openErr != nil {
			return nil, ErrNotFound
		}
		info, statErr = file.Stat()
		if statErr != nil || !info.Mode().IsRegular() {
			_ = file.Close()
			return nil, ErrNotFile
		}
		return &Image{File: file, Info: info, Path: path}, nil
	}, func(image *Image) { _ = image.File.Close() })
}

// ListImages lists the folders and image files of one folder under a root,
// folders first, each group by name. Hidden entries and anything a symlink
// leads out of the root are left out. The folder is read through the root,
// so a folder swapped for a link after the check cannot lead outside.
func (b *Browser) ListImages(ctx context.Context, id, rel string) (Listing, error) {
	root, err := b.root(id)
	if err != nil {
		return Listing{}, err
	}
	clean, err := cleanRel(rel)
	if err != nil {
		return Listing{}, err
	}
	return bounded(ctx, b, root.Path, func() (Listing, error) {
		rootReal, dir, resolveErr := resolveUnder(root.Path, clean)
		if resolveErr != nil {
			return Listing{}, resolveErr
		}
		b.checked()
		top, openErr := os.OpenRoot(rootReal)
		if openErr != nil {
			return Listing{}, ErrNotFound
		}
		defer func() { _ = top.Close() }()
		relDir, relErr := filepath.Rel(rootReal, dir)
		if relErr != nil {
			return Listing{}, ErrInvalidPath
		}
		info, statErr := top.Stat(relDir)
		if statErr != nil {
			return Listing{}, ErrNotFound
		}
		if !info.IsDir() {
			return Listing{}, ErrNotDir
		}
		folder, openErr := top.OpenRoot(relDir)
		if openErr != nil {
			return Listing{}, ErrNotFound
		}
		defer func() { _ = folder.Close() }()
		return readImages(top, folder, rootReal, dir, filepath.ToSlash(clean))
	})
}

// checked runs the test hook between a path's check and its open.
func (b *Browser) checked() {
	if b.afterCheck != nil {
		b.afterCheck()
	}
}

// readImages reads folder and keeps its folders and image files. top is the
// browse root at rootReal, dir is folder's real path when it was checked and
// relDir its path under the root as the client named it, with forward
// slashes.
func readImages(top, folder *os.Root, rootReal, dir, relDir string) (Listing, error) {
	file, err := folder.Open(".")
	if err != nil {
		return Listing{}, ErrNotFound
	}
	defer func() { _ = file.Close() }()
	dirEntries, err := file.ReadDir(MaxDirEntries + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return Listing{}, fmt.Errorf("read folder: %w", err)
	}
	var listing Listing
	if len(dirEntries) > MaxDirEntries {
		dirEntries = dirEntries[:MaxDirEntries]
		listing.Truncated = true
	}
	for _, dirEntry := range dirEntries {
		name := dirEntry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		info, ok := entryInfo(top, folder, rootReal, filepath.Join(dir, name), dirEntry)
		if !ok {
			continue
		}
		entry := Entry{Name: name, IsDir: info.IsDir(), Modified: info.ModTime()}
		switch {
		case entry.IsDir:
		case info.Mode().IsRegular() && IsImageName(name):
			entry.Size = info.Size()
		default:
			continue
		}
		entry.Path = name
		if relDir != "" && relDir != "." {
			entry.Path = relDir + "/" + name
		}
		listing.Entries = append(listing.Entries, entry)
	}
	sort.SliceStable(listing.Entries, func(i, j int) bool {
		a, c := &listing.Entries[i], &listing.Entries[j]
		if a.IsDir != c.IsDir {
			return a.IsDir
		}
		if lowerA, lowerC := strings.ToLower(a.Name), strings.ToLower(c.Name); lowerA != lowerC {
			return lowerA < lowerC
		}
		return a.Name < c.Name
	})
	return listing, nil
}

// entryInfo describes one folder entry, read through the roots. A symlink is
// described by its target, and left out when the target is outside the root.
func entryInfo(top, folder *os.Root, rootReal, path string, dirEntry os.DirEntry) (os.FileInfo, bool) {
	if dirEntry.Type()&os.ModeSymlink == 0 {
		info, err := folder.Lstat(dirEntry.Name())
		return info, err == nil
	}
	// An absolute link inside the root is kept, which os.Root alone refuses,
	// so the target is resolved here and then read through the root.
	target, err := filepath.EvalSymlinks(path)
	if err != nil || !within(rootReal, target) {
		return nil, false
	}
	relTarget, err := filepath.Rel(rootReal, target)
	if err != nil {
		return nil, false
	}
	info, err := top.Stat(relTarget)
	return info, err == nil
}
