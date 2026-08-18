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

package updater

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/updater/otameta"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tarMember describes one entry to write into a test tar. The size in the
// header always matches the body, because tar.Writer refuses to let a header
// lie about it; the guard against an oversized declared size is exercised by
// lowering the stager's limit instead.
type tarMember struct {
	name     string
	linkname string
	body     []byte
	mode     int64
	typeflag byte
}

// zipMember describes one entry to write into a test zip. The mode carries what
// a zip uses instead of a type flag: a symlink or a directory is a mode bit.
type zipMember struct {
	name string
	body []byte
	mode fs.FileMode
}

func writeTarGz(t *testing.T, path string, members []tarMember) {
	t.Helper()

	var buf bytes.Buffer
	gz, err := gzip.NewWriterLevel(&buf, gzip.BestSpeed)
	require.NoError(t, err)
	tw := tar.NewWriter(gz)

	for _, m := range members {
		header := &tar.Header{
			Name:     m.name,
			Linkname: m.linkname,
			Typeflag: m.typeflag,
			Mode:     m.mode,
		}
		if header.Typeflag == 0 {
			header.Typeflag = tar.TypeReg
		}
		if header.Mode == 0 {
			header.Mode = 0o644
		}
		if header.Typeflag == tar.TypeReg {
			header.Size = int64(len(m.body))
		}
		require.NoError(t, tw.WriteHeader(header))
		if header.Typeflag == tar.TypeReg && len(m.body) > 0 {
			_, writeErr := tw.Write(m.body)
			require.NoError(t, writeErr)
		}
	}

	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o600))
}

func writeZip(t *testing.T, path string, members []zipMember) {
	t.Helper()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	for _, m := range members {
		header := &zip.FileHeader{Name: m.name, Method: zip.Deflate}
		mode := m.mode
		if mode == 0 {
			mode = 0o644
		}
		header.SetMode(mode)
		w, err := zw.CreateHeader(header)
		require.NoError(t, err)
		if len(m.body) > 0 {
			_, writeErr := w.Write(m.body)
			require.NoError(t, writeErr)
		}
	}

	require.NoError(t, zw.Close())
	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o600))
}

// extractHarness lays a tree out so an escape is visible: the archive, the
// payload directory extraction is allowed to write into, and a sibling holding
// a file that must never change.
type extractHarness struct {
	stager      *stager
	base        string
	archivePath string
	destPath    string
	payloadDir  string
}

func newExtractHarness(t *testing.T, ext string) *extractHarness {
	t.Helper()

	base := t.TempDir()
	payloadDir := filepath.Join(base, "staging", payloadSubdir)
	require.NoError(t, os.MkdirAll(payloadDir, 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(base, "archive"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(base, "outside"), 0o750))
	require.NoError(t, os.WriteFile(
		filepath.Join(base, "outside", "sentinel"), []byte("untouched"), 0o600))

	return &extractHarness{
		stager: &stager{
			binaryName:       "zaparoo",
			goos:             "linux",
			goarch:           "amd64",
			maxFileBytes:     maxStagedFileBytes,
			maxInflatedBytes: maxArchiveInflatedBytes,
		},
		base:        base,
		archivePath: filepath.Join(base, "archive", "release"+ext),
		destPath:    filepath.Join(payloadDir, "zaparoo"),
		payloadDir:  payloadDir,
	}
}

// extract runs extraction and asserts the only thing it changed is inside the
// payload directory, which is the whole point of naming the destination
// ourselves rather than unpacking archive-supplied paths.
func (h *extractHarness) extract(t *testing.T, ext string) error {
	t.Helper()

	// Hashed as the archive stands right now, so a test that deliberately
	// corrupts one still exercises the format handling instead of stopping at the
	// on-disk digest check. TestExtractBinary_RejectsBytesChangedOnDisk covers
	// that check on its own.
	return h.extractWithDigest(t, ext, archiveDigest(t, h.archivePath))
}

func (h *extractHarness) extractWithDigest(t *testing.T, ext string, want []byte) error {
	t.Helper()

	before := snapshotOutside(t, h.base, h.payloadDir)
	err := h.stager.extractBinary(context.Background(), h.archivePath, ext, want, h.destPath)
	assert.Equal(t, before, snapshotOutside(t, h.base, h.payloadDir),
		"extraction wrote outside the payload directory")
	return err
}

func archiveDigest(t *testing.T, path string) []byte {
	t.Helper()

	body, err := os.ReadFile(path) //nolint:gosec // test path under t.TempDir
	require.NoError(t, err)
	sum := sha256.Sum256(body)
	return sum[:]
}

// snapshotOutside records the whole tree except the payload directory.
func snapshotOutside(t *testing.T, base, payloadDir string) map[string]string {
	t.Helper()

	found := make(map[string]string)
	err := filepath.WalkDir(base, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == payloadDir {
			return fs.SkipDir
		}
		rel, relErr := filepath.Rel(base, path)
		if relErr != nil {
			return fmt.Errorf("relative path of %s: %w", path, relErr)
		}
		if d.IsDir() {
			found[rel] = "dir"
			return nil
		}
		body, readErr := os.ReadFile(path) //nolint:gosec // test tree under t.TempDir
		if readErr != nil {
			return fmt.Errorf("reading %s: %w", path, readErr)
		}
		sum := sha256.Sum256(body)
		found[rel] = hex.EncodeToString(sum[:])
		return nil
	})
	require.NoError(t, err)
	return found
}

func TestMatchExecutableName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		cmd    string
		goos   string
		goarch string
		target string
		want   bool
	}{
		{name: "bare name", cmd: "zaparoo", goos: "linux", goarch: "amd64", target: "zaparoo", want: true},
		{
			name: "version with underscore", cmd: "zaparoo", goos: "linux", goarch: "amd64",
			target: "zaparoo_2.11.0", want: true,
		},
		{
			name: "version with dash", cmd: "zaparoo", goos: "linux", goarch: "amd64",
			target: "zaparoo-2.11.0", want: true,
		},
		{
			name: "version with v prefix", cmd: "zaparoo", goos: "linux", goarch: "amd64",
			target: "zaparoo_v2.11.0", want: true,
		},
		{
			name: "prerelease version", cmd: "zaparoo", goos: "linux", goarch: "amd64",
			target: "zaparoo-2.11.0-beta.1", want: true,
		},
		{
			name: "os and arch", cmd: "zaparoo", goos: "linux", goarch: "amd64",
			target: "zaparoo_linux_amd64", want: true,
		},
		{
			name: "version and os and arch", cmd: "zaparoo", goos: "linux", goarch: "amd64",
			target: "zaparoo_2.11.0_linux_amd64", want: true,
		},
		{
			name: "exe suffix accepted", cmd: "zaparoo", goos: "linux", goarch: "amd64",
			target: "zaparoo.exe", want: true,
		},
		{
			name: "windows binary", cmd: "Zaparoo.exe", goos: "windows", goarch: "amd64",
			target: "Zaparoo.exe", want: true,
		},
		{
			name: "windows binary without extension", cmd: "Zaparoo.exe", goos: "windows", goarch: "amd64",
			target: "Zaparoo", want: true,
		},
		{
			name: "match is case sensitive", cmd: "Zaparoo.exe", goos: "windows", goarch: "amd64",
			target: "zaparoo.exe", want: false,
		},
		{
			name: "mister binary keeps its sh name", cmd: "zaparoo.sh", goos: "linux", goarch: "arm",
			target: "zaparoo.sh", want: true,
		},
		{
			name: "mister binary with version", cmd: "zaparoo.sh", goos: "linux", goarch: "arm",
			target: "zaparoo.sh_2.11.0", want: true,
		},
		// The dot in zaparoo.sh has to stay a dot. Without QuoteMeta it would
		// match any character in that position.
		{
			name: "dot in the name is literal", cmd: "zaparoo.sh", goos: "linux", goarch: "arm",
			target: "zaparoosh", want: false,
		},
		{
			name: "mister binary is not the plain name", cmd: "zaparoo.sh", goos: "linux", goarch: "arm",
			target: "zaparoo", want: false,
		},
		{
			name: "sh suffix is not the plain binary", cmd: "zaparoo", goos: "linux", goarch: "amd64",
			target: "zaparoo.sh", want: false,
		},
		{
			name: "other os", cmd: "zaparoo", goos: "linux", goarch: "amd64",
			target: "zaparoo_windows_amd64", want: false,
		},
		{
			name: "other arch", cmd: "zaparoo", goos: "linux", goarch: "amd64",
			target: "zaparoo_linux_arm64", want: false,
		},
		// arm must not match an arm64 archive member.
		{
			name: "arm does not match arm64", cmd: "zaparoo", goos: "linux", goarch: "arm",
			target: "zaparoo_linux_arm64", want: false,
		},
		{name: "licence", cmd: "zaparoo", goos: "linux", goarch: "amd64", target: "LICENSE.txt", want: false},
		{name: "trailing junk", cmd: "zaparoo", goos: "linux", goarch: "amd64", target: "zaparoo2", want: false},
		{name: "nested path", cmd: "zaparoo", goos: "linux", goarch: "amd64", target: "bin/zaparoo", want: false},
		{name: "empty target", cmd: "zaparoo", goos: "linux", goarch: "amd64", target: "", want: false},
		{name: "empty command", cmd: "", goos: "linux", goarch: "amd64", target: "zaparoo", want: false},
		{name: "command is only an extension", cmd: ".exe", goos: "windows", goarch: "amd64", target: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := matchExecutableName(tt.cmd, tt.goos, tt.goarch, tt.target)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestWantsMember_RejectsAnythingWithAPathInIt(t *testing.T) {
	t.Parallel()

	s := &stager{binaryName: "zaparoo", goos: "linux", goarch: "amd64"}

	assert.True(t, s.wantsMember("zaparoo"))
	assert.False(t, s.wantsMember("bin/zaparoo"))
	assert.False(t, s.wantsMember(`bin\zaparoo`))
	assert.False(t, s.wantsMember("../zaparoo"))
	assert.False(t, s.wantsMember("/zaparoo"))
	assert.False(t, s.wantsMember("./zaparoo"))
}

func TestExtractFromTarGz_TakesOnlyTheBinary(t *testing.T) {
	t.Parallel()

	h := newExtractHarness(t, otameta.ArchiveExtTarGz)
	binary := []byte("this stands in for the executable")
	writeTarGz(t, h.archivePath, []tarMember{
		{name: "LICENSE.txt", body: []byte("gpl")},
		{name: "README.txt", body: []byte("readme")},
		{name: "zaparoo", body: binary, mode: 0o755},
	})

	require.NoError(t, h.extract(t, otameta.ArchiveExtTarGz))

	got, err := os.ReadFile(h.destPath) //nolint:gosec // test path under t.TempDir
	require.NoError(t, err)
	assert.Equal(t, binary, got)

	// The licence and readme are in the archive and are deliberately not
	// written: extraction pulls what it wants, it does not unpack.
	entries, err := os.ReadDir(h.payloadDir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "zaparoo", entries[0].Name())
}

func TestExtractFromZip_TakesOnlyTheBinary(t *testing.T) {
	t.Parallel()

	h := newExtractHarness(t, otameta.ArchiveExtZip)
	binary := []byte("this stands in for the executable")
	writeZip(t, h.archivePath, []zipMember{
		{name: "LICENSE.txt", body: []byte("gpl")},
		{name: "README.txt", body: []byte("readme")},
		{name: "zaparoo", body: binary, mode: 0o755},
	})

	require.NoError(t, h.extract(t, otameta.ArchiveExtZip))

	got, err := os.ReadFile(h.destPath) //nolint:gosec // test path under t.TempDir
	require.NoError(t, err)
	assert.Equal(t, binary, got)

	entries, err := os.ReadDir(h.payloadDir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "zaparoo", entries[0].Name())
}

func TestExtractFromTarGz_VersionedMemberName(t *testing.T) {
	t.Parallel()

	h := newExtractHarness(t, otameta.ArchiveExtTarGz)
	binary := []byte("versioned")
	writeTarGz(t, h.archivePath, []tarMember{
		{name: "zaparoo_2.11.0_linux_amd64", body: binary, mode: 0o755},
	})

	require.NoError(t, h.extract(t, otameta.ArchiveExtTarGz))

	// It landed under the name this package chose, not the one in the archive.
	got, err := os.ReadFile(h.destPath) //nolint:gosec // test path under t.TempDir
	require.NoError(t, err)
	assert.Equal(t, binary, got)
}

func TestExtractBinary_UnknownExtension(t *testing.T) {
	t.Parallel()

	h := newExtractHarness(t, ".7z")
	require.NoError(t, os.WriteFile(h.archivePath, []byte("not an archive"), 0o600))

	err := h.extract(t, ".7z")
	require.ErrorIs(t, err, ErrArchiveRejected)
	assert.NoFileExists(t, h.destPath)
}

// TestExtractBinary_RejectsBytesChangedOnDisk is the claim the download's own
// hash cannot make. That one proves the bytes that arrived were right; this one
// proves the bytes about to be installed are. Between the two there is a close,
// and the storage these devices run on has been seen acknowledging an fsync and
// later handing back zeroed pages, so the archive is re-read through the handle
// extraction is about to use rather than trusted.
func TestExtractBinary_RejectsBytesChangedOnDisk(t *testing.T) {
	t.Parallel()

	for _, ext := range []string{otameta.ArchiveExtTarGz, otameta.ArchiveExtZip} {
		t.Run(ext, func(t *testing.T) {
			t.Parallel()

			h := newExtractHarness(t, ext)
			writeArchive(t, h.archivePath, ext, "zaparoo", []byte("the binary"))

			// What the download verified, before the disk changed under it.
			want := archiveDigest(t, h.archivePath)

			body, err := os.ReadFile(h.archivePath) //nolint:gosec // test path under t.TempDir
			require.NoError(t, err)
			body[len(body)/2] ^= 0xff
			//nolint:gosec // G703: test path under t.TempDir
			require.NoError(t, os.WriteFile(h.archivePath, body, 0o600))

			err = h.extractWithDigest(t, ext, want)
			require.ErrorIs(t, err, ErrChecksumMismatch)
			assert.NoFileExists(t, h.destPath, "a binary was staged out of an archive that no longer verifies")
		})
	}
}

// TestExtractBinary_StopsOnCancellation asserts a cancelled staging attempt gets
// no further than the digest re-read, and is not blamed on the archive.
func TestExtractBinary_StopsOnCancellation(t *testing.T) {
	t.Parallel()

	for _, ext := range []string{otameta.ArchiveExtTarGz, otameta.ArchiveExtZip} {
		t.Run(ext, func(t *testing.T) {
			t.Parallel()

			h := newExtractHarness(t, ext)
			writeArchive(t, h.archivePath, ext, "zaparoo", []byte("the binary"))

			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			err := h.stager.extractBinary(ctx, h.archivePath, ext, archiveDigest(t, h.archivePath), h.destPath)
			require.ErrorIs(t, err, context.Canceled)
			// Nothing was wrong with the archive, so nothing may say there was.
			require.NotErrorIs(t, err, ErrChecksumMismatch)
			require.NotErrorIs(t, err, ErrArchiveRejected)
			assert.NoFileExists(t, h.destPath)
		})
	}
}

// TestExtractWalk_StopsOnCancellation covers the walk itself, which is the
// stretch that is otherwise uninterruptible: a gzip stream cannot be seeked past,
// so tar inflates every member it skips, and a shutdown part-way through would
// keep decompressing tens of megabytes on a device trying to stop. The walks are
// called directly because reaching them past the digest read needs a context that
// is live for one and dead for the other.
func TestExtractWalk_StopsOnCancellation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		walk func(*extractHarness, context.Context, *os.File, int64) error
		ext  string
	}{
		{
			ext: otameta.ArchiveExtTarGz,
			walk: func(h *extractHarness, ctx context.Context, f *os.File, _ int64) error {
				return h.stager.extractFromTarGz(ctx, f, h.destPath)
			},
		},
		{
			ext: otameta.ArchiveExtZip,
			walk: func(h *extractHarness, ctx context.Context, f *os.File, size int64) error {
				return h.stager.extractFromZip(ctx, f, size, h.destPath)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.ext, func(t *testing.T) {
			t.Parallel()

			h := newExtractHarness(t, tt.ext)
			writeArchive(t, h.archivePath, tt.ext, "zaparoo", []byte("the binary"))

			f, err := os.Open(h.archivePath) //nolint:gosec // test path under t.TempDir
			require.NoError(t, err)
			t.Cleanup(func() { _ = f.Close() })
			info, err := f.Stat()
			require.NoError(t, err)

			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			err = tt.walk(h, ctx, f, info.Size())
			require.ErrorIs(t, err, context.Canceled)
			// ErrArchiveRejected is a permanent verdict on the release. A walk that
			// was interrupted reached no verdict at all, so it must not return one.
			require.NotErrorIs(t, err, ErrArchiveRejected)
			assert.NoFileExists(t, h.destPath)
		})
	}
}

// TestExtractFromTarGz_InflateBudget bounds what reaching the binary can be made
// to cost. The member cap alone does not: a skipped member still has to be
// inflated in full, so content ahead of the binary is work the walk cannot
// decline.
func TestExtractFromTarGz_InflateBudget(t *testing.T) {
	t.Parallel()

	h := newExtractHarness(t, otameta.ArchiveExtTarGz)
	// Compresses to almost nothing on the wire and to well over the budget once
	// inflated, which is the shape of the attack.
	writeTarGz(t, h.archivePath, []tarMember{
		{name: "filler.bin", body: make([]byte, 64<<10)},
		{name: "zaparoo", body: []byte("the binary"), mode: 0o755},
	})
	h.stager.maxInflatedBytes = 4 << 10

	err := h.extract(t, otameta.ArchiveExtTarGz)
	require.ErrorIs(t, err, ErrArchiveRejected)
	assert.Contains(t, err.Error(), "bytes of content in it")
	assert.NoFileExists(t, h.destPath)
}

// TestExtractFromZip_NeedsNoInflateBudget is the other half of that: a zip member
// the walk does not want is never opened, so the same filler costs nothing and
// must not be refused.
func TestExtractFromZip_NeedsNoInflateBudget(t *testing.T) {
	t.Parallel()

	h := newExtractHarness(t, otameta.ArchiveExtZip)
	binary := []byte("the binary")
	writeZip(t, h.archivePath, []zipMember{
		{name: "filler.bin", body: make([]byte, 64<<10)},
		{name: "zaparoo", body: binary, mode: 0o755},
	})
	h.stager.maxInflatedBytes = 4 << 10

	require.NoError(t, h.extract(t, otameta.ArchiveExtZip))

	got, err := os.ReadFile(h.destPath) //nolint:gosec // test path under t.TempDir
	require.NoError(t, err)
	assert.Equal(t, binary, got)
}

// writeArchive writes a minimal release archive in either format.
func writeArchive(t *testing.T, path, ext, memberName string, binary []byte) {
	t.Helper()

	switch ext {
	case otameta.ArchiveExtTarGz:
		writeTarGz(t, path, []tarMember{{name: memberName, body: binary, mode: 0o755}})
	case otameta.ArchiveExtZip:
		writeZip(t, path, []zipMember{{name: memberName, body: binary, mode: 0o755}})
	default:
		t.Fatalf("unsupported archive extension %q", ext)
	}
}

func TestExtractFromTarGz_HostileMembers(t *testing.T) {
	t.Parallel()

	binary := []byte("the binary")

	crowded := make([]tarMember, 0, maxArchiveMembers+1)
	for i := range maxArchiveMembers {
		crowded = append(crowded, tarMember{name: fmt.Sprintf("filler-%d", i), body: []byte("x")})
	}
	crowded = append(crowded, tarMember{name: "zaparoo", body: binary})

	tests := []struct {
		name    string
		wantMsg string
		members []tarMember
		limit   int64
	}{
		{
			name: "traversal name",
			members: []tarMember{
				{name: "../../etc/passwd", body: []byte("root:x:0:0")},
				{name: "LICENSE.txt", body: []byte("gpl")},
			},
			wantMsg: "no zaparoo in it",
		},
		{
			name: "absolute name",
			members: []tarMember{
				{name: "/etc/passwd", body: []byte("root:x:0:0")},
			},
			wantMsg: "no zaparoo in it",
		},
		{
			name: "nested binary",
			members: []tarMember{
				{name: "bin/zaparoo", body: binary},
			},
			wantMsg: "no zaparoo in it",
		},
		{
			name: "symlink standing in for the binary",
			members: []tarMember{
				{name: "zaparoo", linkname: "/etc/passwd", typeflag: tar.TypeSymlink},
			},
			wantMsg: "no zaparoo in it",
		},
		{
			name: "hardlink standing in for the binary",
			members: []tarMember{
				{name: "zaparoo", linkname: "LICENSE.txt", typeflag: tar.TypeLink},
				{name: "LICENSE.txt", body: []byte("gpl")},
			},
			wantMsg: "no zaparoo in it",
		},
		{
			name: "character device",
			members: []tarMember{
				{name: "zaparoo", typeflag: tar.TypeChar},
			},
			wantMsg: "no zaparoo in it",
		},
		{
			name: "block device",
			members: []tarMember{
				{name: "zaparoo", typeflag: tar.TypeBlock},
			},
			wantMsg: "no zaparoo in it",
		},
		{
			name: "fifo",
			members: []tarMember{
				{name: "zaparoo", typeflag: tar.TypeFifo},
			},
			wantMsg: "no zaparoo in it",
		},
		{
			name: "directory using the binary name",
			members: []tarMember{
				{name: "zaparoo", typeflag: tar.TypeDir, mode: 0o755},
			},
			wantMsg: "no zaparoo in it",
		},
		{
			name: "no binary at all",
			members: []tarMember{
				{name: "LICENSE.txt", body: []byte("gpl")},
				{name: "README.txt", body: []byte("readme")},
			},
			wantMsg: "no zaparoo in it",
		},
		{
			name: "two different names both match",
			members: []tarMember{
				{name: "zaparoo", body: binary},
				{name: "zaparoo_linux_amd64", body: []byte("a different binary")},
			},
			wantMsg: "more than one zaparoo",
		},
		{
			name: "duplicate names",
			members: []tarMember{
				{name: "zaparoo", body: binary},
				{name: "zaparoo", body: []byte("a different binary")},
			},
			wantMsg: "more than one zaparoo",
		},
		{
			name: "declared size over the limit",
			members: []tarMember{
				{name: "zaparoo", body: bytes.Repeat([]byte("x"), 128)},
			},
			limit:   16,
			wantMsg: "over the 16 byte limit",
		},
		{
			name:    "too many members",
			members: crowded,
			wantMsg: fmt.Sprintf("more than %d members", maxArchiveMembers),
		},
		{
			name: "empty binary",
			members: []tarMember{
				{name: "zaparoo", body: nil},
			},
			wantMsg: "the update binary is empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newExtractHarness(t, otameta.ArchiveExtTarGz)
			if tt.limit > 0 {
				h.stager.maxFileBytes = tt.limit
			}
			writeTarGz(t, h.archivePath, tt.members)

			err := h.extract(t, otameta.ArchiveExtTarGz)
			require.ErrorIs(t, err, ErrArchiveRejected)
			assert.Contains(t, err.Error(), tt.wantMsg)
		})
	}
}

func TestExtractFromZip_HostileMembers(t *testing.T) {
	t.Parallel()

	binary := []byte("the binary")

	crowded := make([]zipMember, 0, maxArchiveMembers+1)
	for i := range maxArchiveMembers {
		crowded = append(crowded, zipMember{name: fmt.Sprintf("filler-%d", i), body: []byte("x")})
	}
	crowded = append(crowded, zipMember{name: "zaparoo", body: binary})

	tests := []struct {
		name    string
		wantMsg string
		members []zipMember
		limit   int64
	}{
		{
			name: "traversal name",
			members: []zipMember{
				{name: "../../etc/passwd", body: []byte("root:x:0:0")},
			},
			wantMsg: "no zaparoo in it",
		},
		{
			name: "windows traversal name",
			members: []zipMember{
				{name: `..\zaparoo`, body: binary},
			},
			wantMsg: "no zaparoo in it",
		},
		{
			name: "absolute name",
			members: []zipMember{
				{name: "/etc/passwd", body: []byte("root:x:0:0")},
			},
			wantMsg: "no zaparoo in it",
		},
		{
			name: "nested binary",
			members: []zipMember{
				{name: "bin/zaparoo", body: binary},
			},
			wantMsg: "no zaparoo in it",
		},
		{
			name: "symlink standing in for the binary",
			members: []zipMember{
				{name: "zaparoo", body: []byte("/etc/passwd"), mode: fs.ModeSymlink | 0o777},
			},
			wantMsg: "no zaparoo in it",
		},
		{
			name: "directory using the binary name",
			members: []zipMember{
				{name: "zaparoo", mode: fs.ModeDir | 0o755},
			},
			wantMsg: "no zaparoo in it",
		},
		{
			name: "no binary at all",
			members: []zipMember{
				{name: "LICENSE.txt", body: []byte("gpl")},
			},
			wantMsg: "no zaparoo in it",
		},
		{
			name: "two different names both match",
			members: []zipMember{
				{name: "zaparoo", body: binary},
				{name: "zaparoo_linux_amd64", body: []byte("a different binary")},
			},
			wantMsg: "more than one zaparoo",
		},
		{
			name: "duplicate names",
			members: []zipMember{
				{name: "zaparoo", body: binary},
				{name: "zaparoo", body: []byte("a different binary")},
			},
			wantMsg: "more than one zaparoo",
		},
		{
			name: "declared size over the limit",
			members: []zipMember{
				{name: "zaparoo", body: bytes.Repeat([]byte("x"), 128)},
			},
			limit:   16,
			wantMsg: "over the 16 byte limit",
		},
		{
			name:    "too many members",
			members: crowded,
			wantMsg: fmt.Sprintf("more than %d members", maxArchiveMembers),
		},
		{
			name: "empty binary",
			members: []zipMember{
				{name: "zaparoo", body: nil},
			},
			wantMsg: "the update binary is empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newExtractHarness(t, otameta.ArchiveExtZip)
			if tt.limit > 0 {
				h.stager.maxFileBytes = tt.limit
			}
			writeZip(t, h.archivePath, tt.members)

			err := h.extract(t, otameta.ArchiveExtZip)
			require.ErrorIs(t, err, ErrArchiveRejected)
			assert.Contains(t, err.Error(), tt.wantMsg)
		})
	}
}

func TestExtractFromTarGz_CorruptArchive(t *testing.T) {
	t.Parallel()

	t.Run("not gzip at all", func(t *testing.T) {
		t.Parallel()

		h := newExtractHarness(t, otameta.ArchiveExtTarGz)
		require.NoError(t, os.WriteFile(h.archivePath, []byte("this is not gzip"), 0o600))

		err := h.extract(t, otameta.ArchiveExtTarGz)
		require.ErrorIs(t, err, ErrArchiveRejected)
		assert.NoFileExists(t, h.destPath)
	})

	t.Run("truncated tar inside valid gzip", func(t *testing.T) {
		t.Parallel()

		h := newExtractHarness(t, otameta.ArchiveExtTarGz)
		full := filepath.Join(t.TempDir(), "full.tar.gz")
		writeTarGz(t, full, []tarMember{
			{name: "LICENSE.txt", body: bytes.Repeat([]byte("gpl"), 4096)},
			{name: "zaparoo", body: bytes.Repeat([]byte("bin"), 4096)},
		})
		body, err := os.ReadFile(full) //nolint:gosec // test path under t.TempDir
		require.NoError(t, err)
		//nolint:gosec // G703: test path under t.TempDir
		require.NoError(t, os.WriteFile(h.archivePath, body[:len(body)/2], 0o600))

		err = h.extract(t, otameta.ArchiveExtTarGz)
		require.ErrorIs(t, err, ErrArchiveRejected)
	})
}

func TestExtractFromZip_CorruptArchive(t *testing.T) {
	t.Parallel()

	h := newExtractHarness(t, otameta.ArchiveExtZip)
	require.NoError(t, os.WriteFile(h.archivePath, []byte("this is not a zip"), 0o600))

	err := h.extract(t, otameta.ArchiveExtZip)
	require.ErrorIs(t, err, ErrArchiveRejected)
	assert.NoFileExists(t, h.destPath)
}

// TestCopyStagedFile_LimitsBytesThatArrive covers the backstop no archive header
// can talk its way past: the limit is applied to what actually turns up, so a
// member that streams more than it declared is still caught.
func TestCopyStagedFile_LimitsBytesThatArrive(t *testing.T) {
	t.Parallel()

	dest := filepath.Join(t.TempDir(), "zaparoo")
	err := copyStagedFile(context.Background(), strings.NewReader(strings.Repeat("x", 64)), dest, 16)

	require.ErrorIs(t, err, ErrArchiveRejected)
	assert.Contains(t, err.Error(), "larger than the 16 byte limit")
}

func TestCopyStagedFile_ExactLimitIsAccepted(t *testing.T) {
	t.Parallel()

	dest := filepath.Join(t.TempDir(), "zaparoo")
	require.NoError(t, copyStagedFile(context.Background(), strings.NewReader("0123456789abcdef"), dest, 16))

	got, err := os.ReadFile(dest) //nolint:gosec // test path under t.TempDir
	require.NoError(t, err)
	assert.Equal(t, "0123456789abcdef", string(got))
}

func TestCopyStagedFile_RefusesToOverwrite(t *testing.T) {
	t.Parallel()

	dest := filepath.Join(t.TempDir(), "zaparoo")
	require.NoError(t, os.WriteFile(dest, []byte("already here"), 0o600))

	err := copyStagedFile(context.Background(), strings.NewReader("new"), dest, 64)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "creating the staged update binary")

	got, err := os.ReadFile(dest) //nolint:gosec // test path under t.TempDir
	require.NoError(t, err)
	assert.Equal(t, "already here", string(got))
}

func TestCopyStagedFile_ReadError(t *testing.T) {
	t.Parallel()

	dest := filepath.Join(t.TempDir(), "zaparoo")
	err := copyStagedFile(context.Background(), io.MultiReader(
		strings.NewReader("some bytes"),
		&failingReader{},
	), dest, 1024)

	require.ErrorIs(t, err, ErrArchiveRejected)
	assert.Contains(t, err.Error(), "extracting the update binary")
}

// failingReader stands in for an archive member whose stream dies mid-read.
type failingReader struct{}

func (*failingReader) Read([]byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}

// TestCopyIntoSink_WriteError is the failure the classification turns on: the
// bytes arriving are fine and the disk underneath is not. A device that has run
// out of space must not have the release condemned for it, so this error carries
// no verdict.
func TestCopyIntoSink_WriteError(t *testing.T) {
	t.Parallel()

	sink := &failingSink{err: errDiskFull}
	err := copyIntoSink(context.Background(), strings.NewReader(strings.Repeat("x", 64)), sink, 1024)

	require.Error(t, err)
	require.NotErrorIs(t, err, ErrArchiveRejected)
	require.ErrorIs(t, err, errDiskFull)
	assert.Contains(t, err.Error(), "writing the staged update binary")
	assert.True(t, sink.closed, "the destination has to be closed either way")
}

// TestCopyIntoSink_ReadErrorStillRejects is the other half of the same switch: a
// stream that dies mid-member is the archive's problem and does earn a verdict.
func TestCopyIntoSink_ReadErrorStillRejects(t *testing.T) {
	t.Parallel()

	sink := &failingSink{}
	err := copyIntoSink(context.Background(), io.MultiReader(
		strings.NewReader("some bytes"),
		&failingReader{},
	), sink, 1024)

	require.ErrorIs(t, err, ErrArchiveRejected)
	assert.Contains(t, err.Error(), "extracting the update binary")
}

// errDiskFull stands in for the write fault a full card produces, without
// needing a platform-specific errno.
var errDiskFull = errors.New("no space left on device")

// failingSink stands in for a destination whose writes fail, which is how a full
// or failing card behaves. With err nil it accepts everything.
type failingSink struct {
	err    error
	closed bool
}

func (s *failingSink) Write(p []byte) (int, error) {
	if s.err != nil {
		return 0, s.err
	}
	return len(p), nil
}

func (*failingSink) Sync() error { return nil }

func (s *failingSink) Close() error {
	s.closed = true
	return nil
}
