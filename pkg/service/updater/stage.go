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
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/tlsroots"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/updater/otameta"
	"github.com/rs/zerolog/log"
)

const (
	// maxArchiveBytes refuses an absurd declared archive size before a byte is
	// read. Release archives are 10-16 MB today.
	maxArchiveBytes = 256 << 20

	// maxStagedFileBytes caps one file copied out of an archive. It is enforced
	// against the bytes that actually arrive rather than the size the archive
	// declares, so a lying header is caught mid-copy rather than trusted. The
	// binary is around 45 MB uncompressed today.
	maxStagedFileBytes = 256 << 20

	// maxArchiveMembers bounds how much of an archive is walked. Releases carry
	// the binary, a licence, a readme and on one platform a scripts directory,
	// so a hundred is generous; the point is that the walk terminates.
	maxArchiveMembers = 100

	// maxArchiveInflatedBytes bounds the total content the walk reads out of one
	// archive, across every member including the ones it skips. maxStagedFileBytes
	// caps the file that is kept; this caps what getting to it can be made to
	// cost.
	maxArchiveInflatedBytes = 384 << 20

	// downloadStallTimeout is how long a transfer may make no progress at all
	// before it is abandoned. It deliberately bounds silence rather than total
	// duration: a legitimate download to a MiSTer over a slow link can run for
	// minutes, and killing that would leave those devices unable to update.
	downloadStallTimeout = 90 * time.Second

	// stallChecks is how many times the stall guard looks per timeout. Checking
	// more often than the timeout keeps the worst-case detection delay to a
	// fraction of it rather than double.
	stallChecks = 4

	// probeTimeout bounds the staged binary's version check. It prints one line
	// and exits before opening anything, so a binary that has not answered in
	// ten seconds is not going to.
	probeTimeout = 10 * time.Second

	// probeWaitDelay is how long the probe waits for the output pipes to close
	// after the process itself is gone. Without it a leftover child holding those
	// pipes would block the probe past its timeout, which would strand the whole
	// update rather than fail it.
	probeWaitDelay = 2 * time.Second

	// probeOutputLimit caps how much of the staged binary's output the probe
	// keeps. The probe runs a binary that arrived over the network moments ago,
	// and one that fails by printing without stopping would otherwise be held
	// whole in memory on a device that has a few hundred megabytes of it. Only
	// the first line matters here, so keeping a few kilobytes loses nothing the
	// error message would have used.
	probeOutputLimit = 8 << 10

	// stagingRemoveAttempts and stagingRemoveDelay bound how long removing a
	// staging directory is retried. A staging directory holds a binary this
	// process may have just finished executing for the probe, and Windows can keep
	// such a file open for a moment after the process itself is gone, which fails
	// a single removal with a sharing violation. Retrying costs nothing on the
	// platforms where the first attempt always works; on the one where it does
	// not, it is the difference between a failed update leaving its whole payload
	// on the disk and leaving nothing.
	stagingRemoveAttempts = 20
	stagingRemoveDelay    = 100 * time.Millisecond

	// stagingSubdir holds one directory per staged version, under the updater's
	// own directory rather than anywhere the platform might clean up.
	stagingSubdir = "staging"

	// payloadSubdir holds the files pulled out of the archive, all named by this
	// package.
	payloadSubdir = "payload"

	stagedFilePerm   = 0o600
	stagedBinaryPerm = 0o755
)

var (
	// ErrNotAnUpgrade means the release is not newer than what is running. It is
	// checked here rather than trusted from the check response because that is
	// what stops a stale or tampered manifest installing an older build.
	ErrNotAnUpgrade = errors.New("release is not newer than the running version")

	// ErrUpgradeFloor means the release declares a minimum version to come from
	// and this device is below it, so it has to take an intermediate build
	// first.
	ErrUpgradeFloor = errors.New("release cannot be installed directly from the running version")

	// ErrArchiveRejected covers everything about the archive that fails a rule:
	// a size the manifest will not vouch for, an unreadable or overlong member,
	// a missing binary, or two of them.
	ErrArchiveRejected = errors.New("release archive was rejected")

	// ErrChecksumMismatch means the bytes that arrived are not the bytes the
	// signed manifest describes.
	ErrChecksumMismatch = errors.New("release archive does not match the manifest checksum")

	// ErrDownloadStalled means the transfer stopped making progress, either
	// because the stall guard saw no bytes for the whole stall timeout or because
	// one of the transport's own deadlines ran out waiting for a connect, a
	// handshake or the response headers. It is a fault of the network, never of
	// the release.
	ErrDownloadStalled = errors.New("release archive download stalled")

	// ErrProbeFailed means the staged binary would not run here, or ran and
	// disagreed about what version it is. This is the check that keeps a bad
	// build from reaching a device with no supervisor to recover it.
	ErrProbeFailed = errors.New("staged binary failed its version probe")
)

// StageOptions describes one staging attempt.
type StageOptions struct {
	// Release comes from a manifest whose signature has already been checked.
	// The archive and the version are re-derived from it here rather than
	// trusting anything passed alongside it.
	Release *otameta.Release
	// PlatformID is the platform half of the archive name.
	PlatformID string
	// Arch and OS default to this build's. They are settable so the selection
	// and the archive member rules can be tested for platforms other than the
	// one running the test.
	Arch string
	OS   string
	// TargetPath is the binary that will eventually be replaced. Only its base
	// name is read here: it names the archive member to pull out, and the name
	// the staged copy is written under.
	TargetPath string
	// StagingRoot holds one directory per staged version.
	StagingRoot string
	// CurrentVersion is the version running now, which the release has to beat.
	CurrentVersion string
}

// StagedUpdate is a verified release unpacked into files this process named,
// ready for the install stage to move into place. Nothing outside Dir has been
// touched to produce it.
type StagedUpdate struct {
	// Dir is the staging directory holding the archive and the payload.
	// Removing it undoes the whole staging attempt.
	Dir string
	// BinaryPath is the new executable. It came out of an archive whose bytes on
	// disk were checked against the signed manifest immediately before it was
	// read, and it has answered a version probe on this device.
	BinaryPath string
	// ArchivePath is the downloaded archive, kept so a later stage can report
	// what it installed from.
	ArchivePath string
	// Version is the release version, without the tag's leading v.
	Version string
}

// assetFetcher retrieves an asset URL. Production hands back the CDN response
// body; tests serve archives from a local server.
type assetFetcher func(ctx context.Context, target string) (io.ReadCloser, error)

// cappedBuilder keeps the first probeOutputLimit bytes written to it and
// discards the rest. It reports every write as fully consumed, so the process
// on the other end drains normally instead of blocking on a pipe nobody is
// reading.
type cappedBuilder struct {
	buf strings.Builder
}

func (b *cappedBuilder) Write(p []byte) (int, error) {
	if room := probeOutputLimit - b.buf.Len(); room > 0 {
		// strings.Builder.Write never fails.
		_, _ = b.buf.Write(p[:min(room, len(p))])
	}
	return len(p), nil
}

func (b *cappedBuilder) String() string {
	return b.buf.String()
}

// stager holds the resolved settings for one staging attempt. The limits and
// timeouts are fields rather than constants read at the point of use so tests
// can drive the guards without a 256 MB fixture or a 90 second wait.
type stager struct {
	fetch            assetFetcher
	release          *otameta.Release
	chmod            func(string, os.FileMode) error
	stagingRoot      string
	goarch           string
	binaryName       string
	goos             string
	current          string
	platformID       string
	maxFileBytes     int64
	maxInflatedBytes int64
	stallTimeout     time.Duration
	probeTimeout     time.Duration
}

// stagingRootFor returns where staged versions live for a data directory.
func stagingRootFor(dataDir string) string {
	dir := stateDirFor(dataDir)
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, stagingSubdir)
}

// Stage downloads a release, checks it against the signed manifest, pulls the
// binary out of it and proves that binary runs, without touching the live
// install. Every failure leaves the device exactly as it was.
func Stage(ctx context.Context, opts *StageOptions) (*StagedUpdate, error) {
	// tlsroots hands back a transport this operation owns outright, so closing
	// it at the end does not affect anything else in the process.
	transport := tlsroots.Transport(nil)
	transport.ResponseHeaderTimeout = responseHeaderTimeout
	defer transport.CloseIdleConnections()

	return stageRelease(ctx, opts, assetFetcherFor(transport))
}

// stageRelease is Stage with the transfer injected, so tests can serve an
// archive without a network.
func stageRelease(ctx context.Context, opts *StageOptions, fetch assetFetcher) (*StagedUpdate, error) {
	s, err := newStager(opts, fetch)
	if err != nil {
		return nil, err
	}
	return s.run(ctx)
}

func newStager(opts *StageOptions, fetch assetFetcher) (*stager, error) {
	if opts == nil {
		return nil, errors.New("staging an update needs options")
	}
	if opts.Release == nil {
		return nil, errors.New("staging an update needs a release")
	}
	if opts.PlatformID == "" {
		return nil, errors.New("staging an update needs a platform id")
	}
	if opts.CurrentVersion == "" {
		return nil, errors.New("staging an update needs the running version")
	}
	if opts.StagingRoot == "" {
		return nil, errors.New("staging an update needs a staging directory")
	}
	if fetch == nil {
		return nil, errors.New("staging an update needs a way to fetch the archive")
	}

	// The base name is both the member to pull out and the name to write, so a
	// target path that does not name a file has nothing to stage.
	binaryName := filepath.Base(opts.TargetPath)
	if opts.TargetPath == "" || binaryName == "." || binaryName == string(filepath.Separator) {
		return nil, fmt.Errorf("staging an update needs the path of the binary to replace, got %q", opts.TargetPath)
	}

	s := &stager{
		fetch:            fetch,
		release:          opts.Release,
		platformID:       opts.PlatformID,
		goos:             opts.OS,
		goarch:           opts.Arch,
		binaryName:       binaryName,
		stagingRoot:      opts.StagingRoot,
		current:          opts.CurrentVersion,
		maxFileBytes:     maxStagedFileBytes,
		maxInflatedBytes: maxArchiveInflatedBytes,
		stallTimeout:     downloadStallTimeout,
		probeTimeout:     probeTimeout,
		chmod:            os.Chmod,
	}
	if s.goos == "" {
		s.goos = runtime.GOOS
	}
	if s.goarch == "" {
		s.goarch = runtime.GOARCH
	}
	return s, nil
}

func (s *stager) run(ctx context.Context) (*StagedUpdate, error) {
	asset, version, err := s.selectArchive()
	if err != nil {
		return nil, err
	}

	// The version has already been through semver parsing, which admits only
	// digits, dots, hyphens and alphanumerics, so it cannot name anything but a
	// single directory. Asserted rather than argued about, because it is the one
	// string out of the manifest that becomes a path.
	if version == "." || version == ".." || version != filepath.Base(version) {
		return nil, fmt.Errorf("%w: %q cannot name a staging directory", ErrArchiveRejected, version)
	}
	dir := filepath.Join(s.stagingRoot, version)

	// A previous attempt that died without cleaning up would otherwise collide
	// with this one's exclusive file creation.
	if rmErr := removeStagingDir(dir); rmErr != nil {
		return nil, fmt.Errorf("clearing previous update staging directory: %w", rmErr)
	}
	pruneStagingRoot(s.stagingRoot, version)
	//nolint:gosec // G703: the version is asserted above to be a single path element
	if mkErr := os.MkdirAll(dir, stateDirPerm); mkErr != nil {
		return nil, fmt.Errorf("creating update staging directory: %w", mkErr)
	}

	staged, err := s.stageInto(ctx, dir, asset, version)
	if err != nil {
		// Nothing outside this directory has been written, so discarding it
		// leaves no trace of the attempt.
		if rmErr := removeStagingDir(dir); rmErr != nil {
			log.Warn().Err(rmErr).Str("dir", dir).Msg("could not remove failed update staging directory")
		}
		return nil, err
	}

	log.Info().
		Str("version", staged.Version).
		Str("binary", staged.BinaryPath).
		Msg("staged update is verified and runnable")
	return staged, nil
}

// pruneStagingRoot deletes every staged version except the one being staged now.
//
// The failure path below removes this attempt's own directory, but it only runs
// when the process lives long enough to reach it. A power cut or a kill during a
// download leaves a directory that nothing afterwards is looking for: the next
// release computes a different name, so without this the whole ~60 MB of a
// half-staged version stays on the SD card for the life of the device, next to
// the config and the media database. Sweeping on the way in rather than on the
// way out means an orphan is collected by the next attempt whatever killed the
// last one.
//
// Failures are logged and not returned. Being unable to tidy up is not a reason
// to refuse an update.
func pruneStagingRoot(root, keep string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Warn().Err(err).Str("dir", root).Msg("could not read the update staging directory")
		}
		return
	}

	for _, entry := range entries {
		if entry.Name() == keep {
			continue
		}
		stale := filepath.Join(root, entry.Name())
		if rmErr := removeStagingDir(stale); rmErr != nil {
			log.Warn().Err(rmErr).Str("dir", stale).Msg("could not remove an orphaned update staging directory")
			continue
		}
		log.Info().Str("dir", stale).Msg("removed an orphaned update staging directory")
	}
}

// removeStagingDir deletes a staging directory, retrying for a bounded time.
//
// See stagingRemoveAttempts for why one attempt is not enough. Sleeping only
// happens when a removal has actually failed, so the ordinary path is a single
// call.
func removeStagingDir(dir string) error {
	var err error
	for attempt := range stagingRemoveAttempts {
		if attempt > 0 {
			time.Sleep(stagingRemoveDelay)
		}
		//nolint:gosec // G703: callers pass a path this package built under its own staging root
		if err = os.RemoveAll(dir); err == nil {
			return nil
		}
	}
	return fmt.Errorf("removing update staging directory %q: %w", dir, err)
}

func (s *stager) stageInto(
	ctx context.Context, dir string, asset *otameta.Asset, version string,
) (*StagedUpdate, error) {
	ext, err := otameta.ArchiveExtension(asset.Name)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrArchiveRejected, err)
	}
	wantDigest, err := assetDigest(asset)
	if err != nil {
		return nil, err
	}

	// The archive is written under a name built from the version and extension
	// rather than the one the manifest gives it, so no metadata string reaches
	// the filesystem even though selection has already constrained it.
	archivePath := filepath.Join(dir, otameta.ArchiveBaseName(s.platformID, s.goarch, version)+ext)
	if err := s.downloadArchive(ctx, asset, archivePath); err != nil {
		return nil, err
	}

	payloadDir := filepath.Join(dir, payloadSubdir)
	//nolint:gosec // G703: dir is the staging directory this package created, payloadSubdir is a constant
	if err := os.MkdirAll(payloadDir, stateDirPerm); err != nil {
		return nil, fmt.Errorf("creating update payload directory: %w", err)
	}

	binaryPath := filepath.Join(payloadDir, s.binaryName)
	if err := s.extractBinary(ctx, archivePath, ext, wantDigest, binaryPath); err != nil {
		return nil, err
	}

	// Extraction is a long uninterruptible stretch on a slow device, so the probe
	// is only meaningful if the caller is still interested in the answer. Without
	// this, a shutdown part-way through staging would run the probe on a dead
	// context and report a perfectly good build as unrunnable.
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("staging the update was cancelled: %w", err)
	}

	// The exec bit is set here rather than taken from the archive, so an archive
	// cannot decide what is executable.
	//
	// A failure here is not fatal, because on the volumes MiSTer and MiSTeX
	// install to it does not mean what it looks like it means. /media/fat is vfat
	// or exFAT, which has no mode bits: the exec bit comes from the mount's mask,
	// and chmod is either a silent no-op or an outright error depending on which
	// driver mounted it. In the error case the file is already executable and
	// refusing here would reject a release that runs perfectly well. The probe
	// below is what actually decides whether this binary can execute, so let it,
	// and record that we could not set the bit ourselves in case the probe then
	// fails for a reason that needs this context.
	//nolint:gosec // an executable has to be executable; the archive does not get a say in it
	if err := s.chmod(binaryPath, stagedBinaryPerm); err != nil {
		log.Warn().Err(err).
			Str("binary", binaryPath).
			Msg("could not set the exec bit on the staged binary; leaving it to the probe")
	}

	if err := s.probeBinary(ctx, binaryPath, version); err != nil {
		return nil, err
	}

	return &StagedUpdate{
		Dir:         dir,
		BinaryPath:  binaryPath,
		ArchivePath: archivePath,
		Version:     version,
	}, nil
}

// selectArchive picks the one archive this device may install from the release
// and refuses anything that is not a step forward. The version assertions are
// made here, against the release's own tag, because the check that offered the
// update is a separate operation whose answer this one does not take on trust.
func (s *stager) selectArchive() (*otameta.Asset, string, error) {
	if s.release.Draft {
		return nil, "", fmt.Errorf("%w: %s is a draft", ErrArchiveRejected, s.release.TagName)
	}

	version := otameta.VersionFromTag(s.release.TagName)
	target, err := semver.NewVersion(version)
	if err != nil {
		return nil, "", fmt.Errorf("%w: release %q has no usable version: %w",
			ErrArchiveRejected, s.release.TagName, err)
	}
	current, err := semver.NewVersion(s.current)
	if err != nil {
		return nil, "", fmt.Errorf("reading the running version %q: %w", s.current, err)
	}

	if !target.GreaterThan(current) {
		return nil, "", fmt.Errorf("%w: %s is not newer than %s", ErrNotAnUpgrade, version, s.current)
	}

	if s.release.MinUpgradeFrom != "" {
		floor, floorErr := semver.NewVersion(s.release.MinUpgradeFrom)
		if floorErr != nil {
			return nil, "", fmt.Errorf("%w: %s declares an unusable min_upgrade_from %q: %w",
				ErrArchiveRejected, s.release.TagName, s.release.MinUpgradeFrom, floorErr)
		}
		if current.LessThan(floor) {
			return nil, "", fmt.Errorf("%w: %s needs %s or newer first, running %s",
				ErrUpgradeFloor, version, s.release.MinUpgradeFrom, s.current)
		}
	}

	asset, err := otameta.SelectAsset(s.release, s.platformID, s.goarch)
	if err != nil {
		return nil, "", fmt.Errorf("selecting the update archive: %w", err)
	}
	return asset, version, nil
}

// downloadArchive streams the archive to disk, hashing as it goes, and accepts
// it only if both the length and the digest are exactly what the signed
// manifest says. The body is never read into memory.
func (s *stager) downloadArchive(ctx context.Context, asset *otameta.Asset, dest string) error {
	if asset.Size <= 0 {
		return fmt.Errorf("%w: the manifest declares no size for %s", ErrArchiveRejected, asset.Name)
	}
	if asset.Size > maxArchiveBytes {
		return fmt.Errorf("%w: the manifest declares %d bytes for %s, over the %d byte limit",
			ErrArchiveRejected, asset.Size, asset.Name, maxArchiveBytes)
	}
	want, err := assetDigest(asset)
	if err != nil {
		return err
	}

	stallCtx, guard := newStallGuard(ctx, s.stallTimeout)
	defer guard.stop()

	body, err := s.fetch(stallCtx, asset.URL)
	if err != nil {
		// Classified the same way a mid-body failure is. Before the first byte the
		// transport usually decides first: the dial, the TLS handshake and the
		// response headers each carry a deadline of their own, and all three are
		// shorter than the stall timeout, so classifyTransferErr maps a timeout
		// from any of them onto the same stall verdict. The guard covers the case
		// none of those deadlines can see, where every hop of a redirect chain
		// answers inside its own budget and the transfer as a whole still goes
		// nowhere.
		return s.classifyTransferErr(ctx, guard, err, 0)
	}
	defer closeQuietly(body, "update archive response")

	//nolint:gosec // the path is built by this package inside its own staging directory
	f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, stagedFilePerm)
	if err != nil {
		return fmt.Errorf("creating the update archive file: %w", err)
	}

	// One byte past the declared size, so an archive of exactly that length is
	// accepted and a longer one is detected rather than silently truncated into
	// something that would then fail the digest for the wrong reason.
	digest := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(f, digest), guard.reader(io.LimitReader(body, asset.Size+1)))
	syncErr := f.Sync()
	closeErr := f.Close()

	switch {
	case copyErr != nil:
		return s.classifyTransferErr(ctx, guard, copyErr, written)
	case syncErr != nil:
		return fmt.Errorf("flushing the update archive to disk: %w", syncErr)
	case closeErr != nil:
		return fmt.Errorf("closing the update archive: %w", closeErr)
	}

	if written != asset.Size {
		return fmt.Errorf("%w: %s is %d bytes, the manifest declares %d",
			ErrArchiveRejected, asset.Name, written, asset.Size)
	}
	if subtle.ConstantTimeCompare(digest.Sum(nil), want) != 1 {
		return fmt.Errorf("%w: %s hashes to %s, the manifest declares %s",
			ErrChecksumMismatch, asset.Name, hex.EncodeToString(digest.Sum(nil)), asset.SHA256)
	}

	log.Debug().Str("archive", asset.Name).Int64("bytes", written).Msg("update archive verified")
	return nil
}

// assetDigest decodes the digest the signed manifest gives for an asset. Both
// the download and the read that feeds extraction check against it, so it is
// decoded in one place.
func assetDigest(asset *otameta.Asset) ([]byte, error) {
	want, err := hex.DecodeString(asset.SHA256)
	if err != nil || len(want) != sha256.Size {
		return nil, fmt.Errorf("%w: the manifest has no usable sha256 for %s", ErrArchiveRejected, asset.Name)
	}
	return want, nil
}

// classifyTransferErr tells the three ways a transfer can fail apart. Both the
// request and the body read cancel through the same guarded context, so the
// error either side hands back says only "cancelled" and the reason has to come
// from which of the two cancelled it. Caller intent is checked first: a caller
// who gave up mid-stall is not reporting a network fault.
func (s *stager) classifyTransferErr(
	ctx context.Context, guard *stallGuard, err error, written int64,
) error {
	var netErr net.Error
	switch {
	case ctx.Err() != nil:
		return fmt.Errorf("update archive download was cancelled after %d bytes: %w", written, ctx.Err())
	case guard.tripped():
		return fmt.Errorf("%w after %d bytes with no progress for %s",
			ErrDownloadStalled, written, s.stallTimeout)
	case errors.As(err, &netErr) && netErr.Timeout():
		// A deadline the transport owns rather than one the guard watches: the
		// connect, the handshake or the response headers gave up. Silence the
		// guard never gets to see, but silence all the same, and dead versus slow
		// is the distinction this sentinel exists to draw.
		return fmt.Errorf("%w after %d bytes: %w", ErrDownloadStalled, written, err)
	default:
		return fmt.Errorf("downloading the update archive: %w", err)
	}
}

// probeBinary runs the staged binary's version flag. It is the load-bearing
// check for the platforms with no supervisor: the binary has to demonstrably
// execute here, and agree about what it is, before anything replaces the one
// that is currently working. It catches a wrong architecture, a libc mismatch,
// a missing shared library, an exec bit a vfat mount dropped, a noexec mount,
// and a version that disagrees with the manifest.
func (s *stager) probeBinary(ctx context.Context, binaryPath, version string) error {
	probeCtx, cancel := context.WithTimeout(ctx, s.probeTimeout)
	defer cancel()

	//nolint:gosec // the path is a file this process just created inside its own staging directory
	cmd := exec.CommandContext(probeCtx, binaryPath, "-"+config.VersionFlagName)
	var stdout, stderr cappedBuilder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// Killing the process is not enough to unblock Wait if it left a child
	// holding these pipes. This runs a binary that was downloaded seconds ago,
	// so the timeout has to bound the call and not just the process.
	cmd.WaitDelay = probeWaitDelay

	runErr := cmd.Run()
	if runErr != nil {
		switch {
		case ctx.Err() != nil:
			// The caller gave up. Reporting that as a failed probe would
			// condemn a build that was never actually judged.
			return fmt.Errorf("update probe was cancelled: %w", ctx.Err())
		case errors.Is(probeCtx.Err(), context.DeadlineExceeded):
			return fmt.Errorf("%w: no answer within %s", ErrProbeFailed, s.probeTimeout)
		default:
			return fmt.Errorf("%w: %w (stderr: %s)", ErrProbeFailed, runErr, clip(stderr.String(), 256))
		}
	}

	// One matching line, not the whole stream: the comparison is made by the
	// build that is already installed against one that did not exist when it
	// shipped, so a future release that prints something extra alongside its
	// version must not be judged unrunnable for it.
	want := config.VersionLine(version, s.platformID)
	if !hasLine(stdout.String(), want) {
		return fmt.Errorf("%w: printed %q, expected a line reading %q",
			ErrProbeFailed, clip(stdout.String(), 256), want)
	}

	log.Debug().Str("binary", binaryPath).Msg("staged binary answered its version probe")
	return nil
}

// assetFetcherFor returns a fetcher backed by an HTTP transport.
func assetFetcherFor(transport *http.Transport) assetFetcher {
	return func(ctx context.Context, target string) (io.ReadCloser, error) {
		// No client deadline: the archive is the one response whose size is not
		// bounded by a small constant, so total duration is the caller's context
		// to bound and the stall guard is what tells slow apart from dead.
		client := &http.Client{Transport: transport}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, http.NoBody)
		if err != nil {
			return nil, fmt.Errorf("creating the update archive request: %w", err)
		}

		res, err := client.Do(req) //nolint:bodyclose // the body is the return value; the caller closes it
		if err != nil {
			return nil, fmt.Errorf("requesting the update archive: %w", err)
		}
		if res.StatusCode != http.StatusOK {
			closeQuietly(res.Body, "update archive response")
			return nil, fmt.Errorf("the update archive request failed with status %d", res.StatusCode)
		}
		return res.Body, nil
	}
}

func closeQuietly(c io.Closer, what string) {
	if err := c.Close(); err != nil {
		log.Debug().Err(err).Msgf("closing %s", what)
	}
}

// hasLine reports whether any line of out is exactly want. A trailing carriage
// return is ignored so output that has been through a CRLF channel still
// matches.
func hasLine(out, want string) bool {
	for line := range strings.SplitSeq(out, "\n") {
		if strings.TrimSuffix(line, "\r") == want {
			return true
		}
	}
	return false
}

// clip shortens a string for an error message.
func clip(s string, limit int) string {
	trimmed := strings.TrimSpace(s)
	if len(trimmed) <= limit {
		return trimmed
	}
	return trimmed[:limit] + "…"
}
