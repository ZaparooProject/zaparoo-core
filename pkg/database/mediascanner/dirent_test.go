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
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errFakeDirEntryInfo = errors.New("fake dir entry has no info")

// fakeDirEntry reports a dirent type independently of what the path really is,
// which is what a FAT-family readdir does to a symlink.
type fakeDirEntry struct {
	name string
	mode fs.FileMode
}

func (f fakeDirEntry) Name() string             { return f.name }
func (f fakeDirEntry) IsDir() bool              { return f.mode.IsDir() }
func (f fakeDirEntry) Type() fs.FileMode        { return f.mode }
func (fakeDirEntry) Info() (fs.FileInfo, error) { return nil, errFakeDirEntryInfo }

type fakeFileInfo struct{ mode fs.FileMode }

func (fakeFileInfo) Name() string        { return "fake" }
func (fakeFileInfo) Size() int64         { return 0 }
func (f fakeFileInfo) Mode() fs.FileMode { return f.mode }
func (fakeFileInfo) ModTime() time.Time  { return time.Time{} }
func (f fakeFileInfo) IsDir() bool       { return f.mode.IsDir() }
func (fakeFileInfo) Sys() any            { return nil }

// TestEntryIsSymlink_FallsBackWhereDirentTypesLie covers the MiSTer exFAT case:
// readdir calls a symlink a regular file, so trusting the dirent type alone
// leaves ScanSkipInternalSymlinks doing nothing and the alias gets indexed as a
// second copy of media already scanned under its real path.
func TestEntryIsSymlink_FallsBackWhereDirentTypesLie(t *testing.T) {
	t.Parallel()

	lstatSaysLink := func() (os.FileInfo, error) {
		return fakeFileInfo{mode: fs.ModeSymlink}, nil
	}
	lstatSaysFile := func() (os.FileInfo, error) {
		return fakeFileInfo{mode: 0}, nil
	}
	lstatFails := func() (os.FileInfo, error) {
		return nil, errors.New("lstat refused")
	}

	tests := []struct {
		lstat       func() (os.FileInfo, error)
		name        string
		entry       fakeDirEntry
		reliable    bool
		wantSymlink bool
	}{
		{
			name:        "dirent reports the link",
			entry:       fakeDirEntry{name: "a", mode: fs.ModeSymlink},
			lstat:       lstatSaysFile,
			reliable:    true,
			wantSymlink: true,
		},
		{
			name:        "dirent lies and types are trusted",
			entry:       fakeDirEntry{name: "a"},
			lstat:       lstatSaysLink,
			reliable:    true,
			wantSymlink: false,
		},
		{
			name:        "dirent lies and types are not trusted",
			entry:       fakeDirEntry{name: "a"},
			lstat:       lstatSaysLink,
			reliable:    false,
			wantSymlink: true,
		},
		{
			name:        "ordinary file stays an ordinary file",
			entry:       fakeDirEntry{name: "a"},
			lstat:       lstatSaysFile,
			reliable:    false,
			wantSymlink: false,
		},
		{
			name:        "a failed lstat leaves the dirent's answer",
			entry:       fakeDirEntry{name: "a"},
			lstat:       lstatFails,
			reliable:    false,
			wantSymlink: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.wantSymlink, entryIsSymlink(tt.entry, tt.reliable, tt.lstat))
		})
	}
}

// TestEntryIsSymlink_NeverLstatsADirectory keeps the fallback off the common
// path: directory entries are reported correctly everywhere here, so paying an
// lstat for them would add cost to every directory in the walk for nothing.
func TestEntryIsSymlink_NeverLstatsADirectory(t *testing.T) {
	t.Parallel()

	called := false
	lstat := func() (os.FileInfo, error) {
		called = true
		return fakeFileInfo{mode: fs.ModeSymlink}, nil
	}

	got := entryIsSymlink(fakeDirEntry{name: "d", mode: fs.ModeDir}, false, lstat)
	assert.False(t, got)
	assert.False(t, called, "a directory entry must not be lstatted")
}

// TestEntryIsSymlink_AgreesWithTheFilesystem pins the fallback against a real
// link rather than a stub, so the mode check cannot drift from what lstat
// actually returns.
func TestEntryIsSymlink_AgreesWithTheFilesystem(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "target.rom")
	require.NoError(t, os.WriteFile(target, []byte("x"), 0o600))
	link := filepath.Join(dir, "alias.rom")
	require.NoError(t, os.Symlink(target, link))

	lstat := func() (os.FileInfo, error) { return os.Lstat(link) }

	// The dirent lies, exactly as a FAT-family readdir would.
	assert.True(t, entryIsSymlink(fakeDirEntry{name: "alias.rom"}, false, lstat))
	assert.False(t, entryIsSymlink(fakeDirEntry{name: "alias.rom"}, true, lstat))
}

// TestDirentTypesReportSymlinks_TrustsAnOrdinaryFilesystem guards the fail-open
// rule: an unrecognised or unreadable filesystem must keep the walk's existing
// cost rather than adding an lstat per entry.
func TestDirentTypesReportSymlinks_TrustsAnOrdinaryFilesystem(t *testing.T) {
	t.Parallel()

	assert.True(t, direntTypesReportSymlinks(t.TempDir()))
	assert.True(t, direntTypesReportSymlinks(filepath.Join(t.TempDir(), "does-not-exist")))
}
