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

// Extraction here pulls rather than unpacks. It walks a bounded number of
// archive members, ignores everything that is not a regular file, and copies
// the one member it wants into a path this package chose. No name out of the
// archive ever reaches the filesystem, which is what removes zip-slip, "..",
// absolute paths and duplicate names as things to get right: there is no code
// path where an archive decides where a byte lands.

package updater

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/updatepayload"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/updater/otameta"
)

const (
	// semverPattern matches a version inside an archive member name.
	semverPattern = `(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)` +
		`(?:-((?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?` +
		`(?:\+([0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?`
)

// ctxReader ends a read once the context is done. The archive walks need it
// because they are otherwise uninterruptible: a gzip stream cannot seek, so tar
// has to inflate every member it skips on the way past, and a cancelled staging
// attempt would keep decompressing tens of megabytes on a device that is trying
// to shut down.
type ctxReader struct {
	ctx    context.Context
	source io.Reader
}

func (r *ctxReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, fmt.Errorf("reading the update archive: %w", err)
	}
	//nolint:wrapcheck // a reader wrapper has to pass io.EOF and the source's errors through unchanged
	return r.source.Read(p)
}

// stagedSink is where a member copied out of an archive lands. *os.File is the
// only implementation outside tests; the interface is here so a destination that
// fails mid-write can be exercised without filling a real disk.
type stagedSink interface {
	io.Writer
	Sync() error
	Close() error
}

// errWriter remembers the destination's own failures. io.Copy fuses the two
// directions into a single error value, and here they mean opposite things: a
// read that fails is the archive's problem, a write that fails is the device's.
type errWriter struct {
	dest io.Writer
	err  error
}

func (w *errWriter) Write(p []byte) (int, error) {
	n, err := w.dest.Write(p)
	if err != nil {
		w.err = err
	}
	//nolint:wrapcheck // a writer wrapper has to pass the destination's error through unchanged
	return n, err
}

// extractRelease opens the archive once, proves the bytes sitting on disk are
// still the ones the signed manifest describes, and copies the executable and
// configured platform payload out of that same open file.
func (s *stager) extractRelease(
	ctx context.Context,
	archivePath, ext string,
	want []byte,
	binaryPath, payloadDir string,
) ([]stagedPayloadFile, error) {
	//nolint:gosec // the path is built by this package inside its own staging directory
	f, err := os.Open(archivePath)
	if err != nil {
		return nil, fmt.Errorf("opening the update archive: %w", err)
	}
	defer closeQuietly(f, "update archive")

	size, err := verifyOpenArchive(ctx, f, want)
	if err != nil {
		return nil, err
	}
	// Only the tar path reads sequentially; zip addresses the handle directly and
	// does not care where the offset is. Rewinding both keeps that an
	// implementation detail of the format rather than of this function.
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("rewinding the update archive: %w", err)
	}

	switch ext {
	case otameta.ArchiveExtTarGz:
		return s.extractFromTarGz(ctx, f, binaryPath, payloadDir)
	case otameta.ArchiveExtZip:
		return s.extractFromZip(ctx, f, size, binaryPath, payloadDir)
	default:
		return nil, fmt.Errorf("%w: %q is not an archive type this build unpacks", ErrArchiveRejected, ext)
	}
}

// extractBinary preserves the focused extraction seam used by binary-only
// tests. Production staging uses extractRelease so configured payload files are
// returned to the installer.
func (s *stager) extractBinary(ctx context.Context, archivePath, ext string, want []byte, destPath string) error {
	_, err := s.extractRelease(ctx, archivePath, ext, want, destPath, filepath.Dir(destPath))
	return err
}

// verifyOpenArchive re-checks the archive against the manifest digest, reading
// through the handle extraction is about to use.
//
// The download already hashed these bytes on their way to disk. This hashes them
// on the way back off it, which is a different claim: in between there is a
// close, and the storage these devices run on has been observed acknowledging an
// fsync and later returning zeroed pages, so "the bytes that arrived were right"
// does not establish "the bytes about to be installed are right". Reading
// through the same open file rather than re-opening by path is what ties the two
// reads to one inode.
//
// Without this the .tar.gz platforms have no integrity check on the second read
// at all: tar carries no member checksum, and the walk stops at the
// end-of-archive marker without ever driving the gzip stream to its trailer. The
// .zip platforms get per-member CRC32 for free, which is weaker than this and
// covers only the member that is read.
func verifyOpenArchive(ctx context.Context, f *os.File, want []byte) (int64, error) {
	digest := sha256.New()
	size, err := io.Copy(digest, &ctxReader{ctx: ctx, source: f})
	if err != nil {
		return 0, fmt.Errorf("re-reading the update archive: %w", err)
	}

	got := digest.Sum(nil)
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return 0, fmt.Errorf("%w: the archive on disk hashes to %s, the manifest declares %s",
			ErrChecksumMismatch, hex.EncodeToString(got), hex.EncodeToString(want))
	}
	return size, nil
}

type archiveMemberTarget struct {
	destPath     string
	relativePath string
	binary       bool
}

func (s *stager) memberTarget(name, binaryPath, payloadDir string) (archiveMemberTarget, bool) {
	if s.wantsMember(name) {
		return archiveMemberTarget{destPath: binaryPath, binary: true}, true
	}
	for _, root := range s.payloadRoots {
		relative, ok := updatepayload.RelativeMember(root, name)
		if !ok || !updatepayload.IncludeSource(relative) {
			continue
		}
		archivePath := path.Join(root.ArchiveRoot, relative)
		destPath := filepath.Join(payloadDir, filepath.FromSlash(archivePath))
		nativeRelative, err := filepath.Rel(payloadDir, destPath)
		if err != nil || nativeRelative == ".." || strings.HasPrefix(nativeRelative, ".."+string(filepath.Separator)) {
			return archiveMemberTarget{}, false
		}
		return archiveMemberTarget{destPath: destPath, relativePath: archivePath}, true
	}
	return archiveMemberTarget{}, false
}

func (s *stager) invalidPayloadMember(name string) bool {
	for _, root := range s.payloadRoots {
		if strings.HasPrefix(name, root.ArchiveRoot+"/") ||
			strings.HasPrefix(name, root.ArchiveRoot+`\`) {
			_, ok := updatepayload.RelativeMember(root, name)
			return !ok
		}
	}
	return false
}

func safePayloadMode(mode os.FileMode) os.FileMode {
	if mode.Perm()&0o111 != 0 {
		return 0o755
	}
	return 0o644
}

func (s *stager) copyArchiveMember(
	ctx context.Context,
	src io.Reader,
	target archiveMemberTarget,
	mode os.FileMode,
) (stagedPayloadFile, error) {
	if !target.binary {
		//nolint:gosec // target is derived from a validated configured payload root
		if err := os.MkdirAll(filepath.Dir(target.destPath), stateDirPerm); err != nil {
			return stagedPayloadFile{}, fmt.Errorf("creating staged payload directory: %w", err)
		}
	}
	if err := copyStagedFile(ctx, src, target.destPath, s.maxFileBytes); err != nil {
		return stagedPayloadFile{}, err
	}
	if target.binary {
		return stagedPayloadFile{}, nil
	}
	mode = safePayloadMode(mode)
	if err := s.chmod(target.destPath, mode); err != nil {
		return stagedPayloadFile{}, fmt.Errorf(
			"setting staged payload permissions for %q: %w", target.relativePath, err)
	}
	info, err := os.Stat(target.destPath)
	if err != nil {
		return stagedPayloadFile{}, fmt.Errorf("reading staged payload %q: %w", target.relativePath, err)
	}
	return stagedPayloadFile{
		Path:         target.destPath,
		RelativePath: target.relativePath,
		Mode:         mode,
		Size:         info.Size(),
	}, nil
}

func (s *stager) extractFromTarGz(
	ctx context.Context,
	f *os.File,
	binaryPath, payloadDir string,
) ([]stagedPayloadFile, error) {
	gz, err := gzip.NewReader(&ctxReader{ctx: ctx, source: f})
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("reading the update archive was cancelled: %w", ctxErr)
		}
		return nil, fmt.Errorf("%w: reading the update archive: %w", ErrArchiveRejected, err)
	}
	defer closeQuietly(gz, "update archive decompressor")

	inflated := &io.LimitedReader{R: gz, N: s.maxInflatedBytes + 1}
	overBudget := func() bool { return inflated.N <= 0 }
	tr := tar.NewReader(inflated)
	foundBinary := false
	seenPayload := make(map[string]struct{})
	var payloads []stagedPayloadFile
	var payloadBytes int64
	members := 0
	for {
		header, nextErr := tr.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, fmt.Errorf("reading the update archive was cancelled: %w", ctxErr)
			}
			if overBudget() {
				return nil, fmt.Errorf("%w: more than %d bytes of content in it",
					ErrArchiveRejected, s.maxInflatedBytes)
			}
			return nil, fmt.Errorf("%w: reading the update archive: %w", ErrArchiveRejected, nextErr)
		}
		members++
		if members > maxArchiveMembers {
			return nil, fmt.Errorf("%w: more than %d members", ErrArchiveRejected, maxArchiveMembers)
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		if s.invalidPayloadMember(header.Name) {
			return nil, fmt.Errorf("%w: invalid payload member %q", ErrArchiveRejected, header.Name)
		}
		target, wanted := s.memberTarget(header.Name, binaryPath, payloadDir)
		if !wanted {
			continue
		}
		if target.binary && foundBinary {
			return nil, fmt.Errorf("%w: more than one %s", ErrArchiveRejected, s.binaryName)
		}
		if !target.binary {
			if _, duplicate := seenPayload[target.relativePath]; duplicate {
				return nil, fmt.Errorf("%w: duplicate payload member %q", ErrArchiveRejected, target.relativePath)
			}
			if header.Size > maxStagedPayloadBytes-payloadBytes {
				return nil, fmt.Errorf("%w: payload files exceed the %d byte limit",
					ErrArchiveRejected, maxStagedPayloadBytes)
			}
		}
		if header.Size > s.maxFileBytes {
			return nil, fmt.Errorf("%w: %s declares %d bytes, over the %d byte limit",
				ErrArchiveRejected, header.Name, header.Size, s.maxFileBytes)
		}
		payload, copyErr := s.copyArchiveMember(ctx, tr, target, header.FileInfo().Mode())
		if copyErr != nil {
			if ctx.Err() == nil && overBudget() {
				return nil, fmt.Errorf("%w: more than %d bytes of content in it",
					ErrArchiveRejected, s.maxInflatedBytes)
			}
			return nil, copyErr
		}
		if target.binary {
			foundBinary = true
			continue
		}
		seenPayload[target.relativePath] = struct{}{}
		payloadBytes += payload.Size
		if payloadBytes > maxStagedPayloadBytes {
			return nil, fmt.Errorf("%w: payload files exceed the %d byte limit",
				ErrArchiveRejected, maxStagedPayloadBytes)
		}
		payloads = append(payloads, payload)
	}
	if !foundBinary {
		return nil, fmt.Errorf("%w: no %s in it", ErrArchiveRejected, s.binaryName)
	}
	return payloads, nil
}

func (s *stager) extractFromZip(
	ctx context.Context,
	f *os.File,
	size int64,
	binaryPath, payloadDir string,
) ([]stagedPayloadFile, error) {
	r, err := zip.NewReader(f, size)
	if err != nil {
		return nil, fmt.Errorf("%w: reading the update archive: %w", ErrArchiveRejected, err)
	}
	if len(r.File) > maxArchiveMembers {
		return nil, fmt.Errorf("%w: more than %d members", ErrArchiveRejected, maxArchiveMembers)
	}

	foundBinary := false
	seenPayload := make(map[string]struct{})
	var payloads []stagedPayloadFile
	var payloadBytes int64
	for _, member := range r.File {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("reading the update archive: %w", ctxErr)
		}
		if !member.Mode().IsRegular() {
			continue
		}
		if s.invalidPayloadMember(member.Name) {
			return nil, fmt.Errorf("%w: invalid payload member %q", ErrArchiveRejected, member.Name)
		}
		target, wanted := s.memberTarget(member.Name, binaryPath, payloadDir)
		if !wanted {
			continue
		}
		if target.binary && foundBinary {
			return nil, fmt.Errorf("%w: more than one %s", ErrArchiveRejected, s.binaryName)
		}
		if !target.binary {
			if _, duplicate := seenPayload[target.relativePath]; duplicate {
				return nil, fmt.Errorf("%w: duplicate payload member %q", ErrArchiveRejected, target.relativePath)
			}
			if member.FileInfo().Size() > maxStagedPayloadBytes-payloadBytes {
				return nil, fmt.Errorf("%w: payload files exceed the %d byte limit",
					ErrArchiveRejected, maxStagedPayloadBytes)
			}
		}
		if declared := member.FileInfo().Size(); declared > s.maxFileBytes {
			return nil, fmt.Errorf("%w: %s declares %d bytes, over the %d byte limit",
				ErrArchiveRejected, member.Name, declared, s.maxFileBytes)
		}
		rc, openErr := member.Open()
		if openErr != nil {
			return nil, fmt.Errorf("%w: reading %s: %w", ErrArchiveRejected, member.Name, openErr)
		}
		payload, copyErr := s.copyArchiveMember(ctx, rc, target, member.Mode())
		closeQuietly(rc, "update archive member")
		if copyErr != nil {
			return nil, copyErr
		}
		if target.binary {
			foundBinary = true
			continue
		}
		seenPayload[target.relativePath] = struct{}{}
		payloadBytes += payload.Size
		if payloadBytes > maxStagedPayloadBytes {
			return nil, fmt.Errorf("%w: payload files exceed the %d byte limit",
				ErrArchiveRejected, maxStagedPayloadBytes)
		}
		payloads = append(payloads, payload)
	}
	if !foundBinary {
		return nil, fmt.Errorf("%w: no %s in it", ErrArchiveRejected, s.binaryName)
	}
	return payloads, nil
}

// wantsMember reports whether a member is the binary being staged. It has to be
// the member's whole name: release archives are flat, so a nested path claiming
// to be the binary is not something a genuine build produces.
func (s *stager) wantsMember(name string) bool {
	if strings.ContainsAny(name, `/\`) {
		return false
	}
	return matchExecutableName(s.binaryName, s.goos, s.goarch, name)
}

// matchExecutableName reports whether an archive member names the executable
// cmd: the bare name, optionally carrying a version and an os/arch pair, with
// an optional .exe. It reimplements the rule go-selfupdate applies inside its
// own decompressor, because nothing here goes through that decompressor and the
// rule decides which file gets installed.
func matchExecutableName(cmd, goos, goarch, target string) bool {
	base := strings.TrimSuffix(cmd, ".exe")
	if base == "" {
		return false
	}
	// Every part is quoted, so a name with punctuation in it stays a name. The
	// MiSTer builds are called zaparoo.sh, which without quoting would match
	// anything in that position.
	pattern := regexp.MustCompile(fmt.Sprintf(
		`^%s([_-]v?%s)?([_-]%s[_-]%s)?(\.exe)?$`,
		regexp.QuoteMeta(base),
		semverPattern,
		regexp.QuoteMeta(goos),
		regexp.QuoteMeta(goarch),
	))
	return pattern.MatchString(target)
}

// copyStagedFile writes an archive member to a path this package chose. The
// limit is enforced against the bytes that actually arrive rather than the size
// the archive declares, so a header that lies about its size is caught mid-copy
// instead of being trusted to fill a disk.
func copyStagedFile(ctx context.Context, src io.Reader, destPath string, limit int64) error {
	//nolint:gosec // the path is built by this package inside its own staging directory
	f, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, stagedFilePerm)
	if err != nil {
		return fmt.Errorf("creating the staged update binary: %w", err)
	}
	return copyIntoSink(ctx, src, f, limit)
}

// copyIntoSink is copyStagedFile with the destination already open. It owns
// flushing and closing it, whichever way the copy goes.
func copyIntoSink(ctx context.Context, src io.Reader, dest stagedSink, limit int64) error {
	sink := &errWriter{dest: dest}
	limited := io.LimitReader(&ctxReader{ctx: ctx, source: src}, limit+1)
	written, copyErr := io.Copy(sink, limited)
	syncErr := dest.Sync()
	closeErr := dest.Close()

	switch {
	case copyErr != nil && ctx.Err() != nil:
		return fmt.Errorf("extracting the update binary was cancelled: %w", ctx.Err())
	case copyErr != nil && sink.err != nil:
		// The device failed, not the release. ErrArchiveRejected is a verdict on
		// the build itself, so a full or failing SD card must not earn one: the
		// flush below already reports the same physical fault as an ordinary
		// error, and so does the download's own write path.
		return fmt.Errorf("writing the staged update binary: %w", sink.err)
	case copyErr != nil:
		return fmt.Errorf("%w: extracting the update binary: %w", ErrArchiveRejected, copyErr)
	case syncErr != nil:
		return fmt.Errorf("flushing the staged update binary to disk: %w", syncErr)
	case closeErr != nil:
		return fmt.Errorf("closing the staged update binary: %w", closeErr)
	}

	if written > limit {
		return fmt.Errorf("%w: the update binary is larger than the %d byte limit",
			ErrArchiveRejected, limit)
	}
	if written == 0 {
		return fmt.Errorf("%w: the update binary is empty", ErrArchiveRejected)
	}
	return nil
}
