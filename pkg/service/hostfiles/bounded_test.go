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

package hostfiles

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStalledRootDoesNotStarveOtherRoots(t *testing.T) {
	t.Parallel()
	stalled, healthy := "/test-stalled-"+t.Name(), "/test-healthy-"+t.Name()
	short := &Browser{timeout: 50 * time.Millisecond}
	block := make(chan struct{})
	stall := func() (int, error) {
		<-block
		return 0, nil
	}

	// Reads that never answer time out but keep their slots.
	for range maxConcurrentRootReads {
		_, err := bounded(context.Background(), short, stalled, stall)
		require.ErrorIs(t, err, ErrTimeout)
	}
	_, err := bounded(context.Background(), short, stalled, stall)
	require.ErrorIs(t, err, ErrBusy, "the stalled root has no slot left")

	got, err := bounded(context.Background(), short, healthy, func() (int, error) { return 7, nil })
	require.NoError(t, err, "another root still gets a slot")
	assert.Equal(t, 7, got)

	// Once the filesystem answers, the stalled root's slots come back.
	close(block)
	long := &Browser{timeout: 5 * time.Second}
	got, err = bounded(context.Background(), long, stalled, func() (int, error) { return 1, nil })
	require.NoError(t, err)
	assert.Equal(t, 1, got)
}

func TestBoundedReportsCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	b := &Browser{timeout: 5 * time.Second}
	started := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	go func() {
		<-started
		cancel()
	}()
	_, err := bounded(ctx, b, "/test-cancel-"+t.Name(), func() (int, error) {
		close(started)
		<-release
		return 0, nil
	})
	require.ErrorIs(t, err, context.Canceled)
}

// swappedTree is a root with games/covers/a.png and, outside it,
// private/a.png. Its browser swaps covers for a link to private after the
// path is checked and before it is opened.
func swappedTree(t *testing.T, link func(base string) string) (b *Browser, rootPath string) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	rootPath = filepath.Join(base, "games")
	covers := filepath.Join(rootPath, "covers")
	private := filepath.Join(base, "private")
	for _, dir := range []string{covers, private} {
		require.NoError(t, os.MkdirAll(dir, 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "a.png"), []byte(filepath.Base(dir)), 0o600))
	}
	if err = os.Symlink(link(base), filepath.Join(rootPath, "probe")); err != nil {
		t.Skipf("symlinks are not available: %v", err)
	}
	b = NewBrowser([]string{rootPath}, "")
	b.afterCheck = func() {
		require.NoError(t, os.Rename(covers, filepath.Join(rootPath, "covers.old")))
		require.NoError(t, os.Symlink(link(base), covers))
		leaked, readErr := os.ReadFile(filepath.Join(covers, "a.png")) //nolint:gosec // test temp folder
		require.NoError(t, readErr)
		require.Equal(t, "private", string(leaked), "the swapped folder leads outside the root")
	}
	return b, rootPath
}

func TestFolderSwappedAfterTheCheckCannotLeadOutOfTheRoot(t *testing.T) {
	t.Parallel()
	links := map[string]func(base string) string{
		"absolute": func(base string) string { return filepath.Join(base, "private") },
		"relative": func(string) string { return filepath.Join("..", "private") },
	}
	for name, link := range links {
		t.Run(name+" image", func(t *testing.T) {
			t.Parallel()
			b, rootPath := swappedTree(t, link)
			image, err := b.OpenImage(context.Background(), rootID(rootPath), "covers/a.png")
			if image != nil {
				_ = image.File.Close()
			}
			require.ErrorIs(t, err, ErrNotFound, "the swapped folder is not followed out of the root")
		})
		t.Run(name+" listing", func(t *testing.T) {
			t.Parallel()
			b, rootPath := swappedTree(t, link)
			listing, err := b.ListImages(context.Background(), rootID(rootPath), "covers")
			require.ErrorIs(t, err, ErrNotFound, "listing %v", listing.Entries)
		})
	}
}

func TestBoundedClosesWhatALateReadOpened(t *testing.T) {
	t.Parallel()
	b := &Browser{timeout: 50 * time.Millisecond}
	release := make(chan struct{})
	discarded := make(chan int, 1)
	_, err := boundedDiscard(context.Background(), b, "/test-discard-"+t.Name(), func() (int, error) {
		<-release
		return 3, nil
	}, func(v int) { discarded <- v })
	require.ErrorIs(t, err, ErrTimeout)
	close(release)
	select {
	case v := <-discarded:
		assert.Equal(t, 3, v)
	case <-time.After(5 * time.Second):
		require.FailNow(t, "the late value was not discarded")
	}
}
