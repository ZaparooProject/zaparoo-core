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
	roots   []Root
	timeout time.Duration
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
	done := make(chan result, 1)
	go func() {
		defer reads.release(root)
		value, err := fn()
		done <- result{value: value, err: err}
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

// Resolve returns the real path of rel under the root with the given ID. rel
// uses forward slashes; empty is the root itself. A path that leaves its
// root, by ".." or through a symlink, is refused.
func (b *Browser) Resolve(ctx context.Context, id, rel string) (string, error) {
	root, err := b.root(id)
	if err != nil {
		return "", err
	}
	clean, err := cleanRel(rel)
	if err != nil {
		return "", err
	}
	return bounded(ctx, b, root.Path, func() (string, error) {
		_, resolved, resolveErr := resolveUnder(root.Path, clean)
		return resolved, resolveErr
	})
}

// resolveUnder returns the real paths of root and of clean under it. It
// touches the filesystem, so it runs inside bounded.
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

// ResolveImage is Resolve for a regular file with an image extension.
func (b *Browser) ResolveImage(ctx context.Context, id, rel string) (string, error) {
	root, err := b.root(id)
	if err != nil {
		return "", err
	}
	clean, err := cleanRel(rel)
	if err != nil {
		return "", err
	}
	return bounded(ctx, b, root.Path, func() (string, error) {
		_, path, resolveErr := resolveUnder(root.Path, clean)
		if resolveErr != nil {
			return "", resolveErr
		}
		info, statErr := os.Stat(path)
		if statErr != nil {
			return "", ErrNotFound
		}
		if !info.Mode().IsRegular() || !IsImageName(path) {
			return "", ErrNotFile
		}
		return path, nil
	})
}

// ListImages lists the folders and image files of one folder under a root,
// folders first, each group by name. Hidden entries and anything a symlink
// leads out of the root are left out.
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
		info, statErr := os.Stat(dir)
		if statErr != nil {
			return Listing{}, ErrNotFound
		}
		if !info.IsDir() {
			return Listing{}, ErrNotDir
		}
		return readImages(rootReal, dir, filepath.ToSlash(clean))
	})
}

// readImages reads dir and keeps its folders and image files. relDir is
// dir's path under root, with forward slashes.
func readImages(root, dir, relDir string) (Listing, error) {
	file, err := os.Open(dir) //nolint:gosec // dir was resolved inside a browse root
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
		path := filepath.Join(dir, name)
		if dirEntry.Type()&os.ModeSymlink != 0 {
			target, linkErr := filepath.EvalSymlinks(path)
			if linkErr != nil || !within(root, target) {
				continue
			}
		}
		info, statErr := os.Stat(path)
		if statErr != nil {
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
