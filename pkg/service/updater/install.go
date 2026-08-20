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
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/rs/zerolog/log"
)

const (
	installBackupSuffix    = ".zaparoo-update-backup"
	installCandidateSuffix = ".zaparoo-update-new"
)

// UpdateBackupper is the part of the live UserDB needed to arm rollback before
// an update replaces the running binary.
type UpdateBackupper interface {
	BackupForUpdate(targetVersion string) (database.BackupInfo, func() error, error)
}

type installOptions struct {
	UserDB UpdateBackupper
	// PreQuiesce runs once the candidate binary is in place and before the
	// user database is closed for its snapshot. That is the last moment an
	// install can still be called off with nothing to unwind, so it is where
	// the second power check goes.
	PreQuiesce func(context.Context) error
	Staged     *StagedUpdate
	progress   *progressReporter
	// binary is how the install and its unwind move the executable around. The
	// zero value does the real thing.
	binary             installBinaryOps
	TargetPath         string
	DataDir            string
	PreviousVersion    string
	PlatformID         string
	Trigger            updateTrigger
	ManifestGeneration int64
}

// installStaged copies the verified binary onto the target filesystem, snapshots
// the live UserDB, records recovery metadata, then swaps the binary into place.
// Every filesystem mutation after the marker is durable can be resumed or
// undone by the startup watchdog.
func installStaged(ctx context.Context, opts *installOptions) (retErr error) {
	if opts == nil || opts.Staged == nil {
		return errors.New("installing an update needs a staged release")
	}
	if opts.UserDB == nil {
		return errors.New("installing an update needs the user database")
	}
	if opts.TargetPath == "" || opts.DataDir == "" {
		return errors.New("installing an update needs target and data paths")
	}

	markerMu.Lock()
	defer markerMu.Unlock()

	dir := stateDirFor(opts.DataDir)
	pending, loadErr := loadMarker(dir)
	if loadErr != nil {
		return fmt.Errorf("checking for an unresolved update: %w", loadErr)
	}
	if pending != nil {
		return fmt.Errorf("an update to %s is still unresolved", pending.TargetVersion)
	}

	backupPath := installSidecarPath(opts.TargetPath, installBackupSuffix)
	candidatePath := installSidecarPath(opts.TargetPath, installCandidateSuffix)
	if _, statErr := os.Lstat(backupPath); statErr == nil {
		if _, targetErr := os.Stat(opts.TargetPath); targetErr != nil {
			return fmt.Errorf("orphaned binary backup exists but target is unavailable: %w", targetErr)
		}
		if removeErr := os.Remove(backupPath); removeErr != nil {
			return fmt.Errorf("removing an orphaned binary backup: %w", removeErr)
		}
		if syncErr := syncDir(filepath.Dir(opts.TargetPath)); syncErr != nil {
			return fmt.Errorf("flushing removal of an orphaned binary backup: %w", syncErr)
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return fmt.Errorf("checking for an unresolved binary backup: %w", statErr)
	}
	if removeErr := os.Remove(candidatePath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		return fmt.Errorf("removing a stale install candidate: %w", removeErr)
	}
	// Where a swap moves the outgoing binary aside instead of overwriting it,
	// the process running from it cannot delete it before exiting. This is the
	// first moment that file is reliably gone.
	opts.binary.sweep(opts.TargetPath)

	if candidateErr := prepareInstallCandidate(ctx, opts.Staged.BinaryPath, candidatePath,
		opts.Staged.Version, opts.PlatformID); candidateErr != nil {
		if cleanupErr := removeStagingDir(ctx, opts.Staged.Dir); cleanupErr != nil {
			log.Warn().Err(cleanupErr).Str("dir", opts.Staged.Dir).
				Msg("could not remove staging after candidate preparation failed")
		}
		return candidateErr
	}

	// Everything up to here can be abandoned by deleting two files. From the
	// snapshot on, the device is committed to either finishing or unwinding, so
	// this is where a caller gets its last say.
	if opts.PreQuiesce != nil {
		if err := opts.PreQuiesce(ctx); err != nil {
			_ = os.Remove(candidatePath)
			if cleanupErr := removeStagingDir(ctx, opts.Staged.Dir); cleanupErr != nil {
				log.Warn().Err(cleanupErr).Str("dir", opts.Staged.Dir).
					Msg("could not remove staging after the update was called off")
			}
			return err
		}
	}

	opts.progress.stage(ProgressInstalling)
	snapshot, resumeUserDB, err := opts.UserDB.BackupForUpdate(opts.Staged.Version)
	if err != nil {
		_ = os.Remove(candidatePath)
		if cleanupErr := removeStagingDir(ctx, opts.Staged.Dir); cleanupErr != nil {
			log.Warn().Err(cleanupErr).Str("dir", opts.Staged.Dir).
				Msg("could not remove staging after the update snapshot failed")
		}
		return fmt.Errorf("snapshotting the user database before update: %w", err)
	}
	resumeOnFailure := true
	defer func() {
		if resumeOnFailure && resumeUserDB != nil {
			if resumeErr := resumeUserDB(); resumeErr != nil {
				retErr = errors.Join(retErr,
					fmt.Errorf("reopening user database after failed update install: %w", resumeErr))
			}
		}
	}()

	m := &pendingMarker{
		InstalledAt:        time.Now().UTC(),
		State:              markerInstalling,
		Trigger:            opts.Trigger,
		TargetPath:         opts.TargetPath,
		BackupPath:         backupPath,
		StagingDir:         opts.Staged.Dir,
		PreviousVersion:    opts.PreviousVersion,
		TargetVersion:      opts.Staged.Version,
		PlatformID:         opts.PlatformID,
		UserDBSnapshotPath: snapshot.Path,
		ManifestGeneration: opts.ManifestGeneration,
	}
	if err := preserveCurrentBinary(opts.TargetPath, backupPath); err != nil {
		removeInstallArtifacts(ctx, m, candidatePath, opts.binary)
		return fmt.Errorf("preserving the current binary: %w", err)
	}
	if err := saveMarker(dir, m); err != nil {
		removeInstallArtifacts(ctx, m, candidatePath, opts.binary)
		return fmt.Errorf("recording the start of the update install: %w", err)
	}

	// Target holds the old executable until this swap and the verified new one
	// after it. Where the platform lets a running binary be overwritten that is
	// one rename, so a power cut lands on one side or the other. Where it has
	// to be vacated first the target name is briefly empty instead, and the
	// marker written above is what tells the next boot to put the backup back.
	if err := opts.binary.replace(candidatePath, opts.TargetPath); err != nil {
		return abortInstallAfterError(ctx, opts.DataDir, m, candidatePath, opts.binary,
			fmt.Errorf("installing the staged binary: %w", err))
	}
	// From here the target name means the new binary, so an unwind has real work
	// to do. Before it, a failed swap left the old one exactly where it was and
	// the unwind has to leave it alone.
	m.BinaryReplaced = true
	if err := syncDir(filepath.Dir(opts.TargetPath)); err != nil {
		return abortInstallAfterError(ctx, opts.DataDir, m, candidatePath, opts.binary,
			fmt.Errorf("flushing the installed binary: %w", err))
	}

	m.State = markerInstalled
	if err := saveMarker(dir, m); err != nil {
		return abortInstallAfterError(ctx, opts.DataDir, m, candidatePath, opts.binary,
			fmt.Errorf("recording the completed update install: %w", err))
	}

	resumeOnFailure = false
	log.Info().
		Str("from", opts.PreviousVersion).
		Str("to", opts.Staged.Version).
		Str("target", opts.TargetPath).
		Msg("installed update and armed startup watchdog")
	return nil
}

func preserveCurrentBinary(targetPath, backupPath string) error {
	info, err := os.Stat(targetPath)
	if err != nil {
		return fmt.Errorf("reading current binary: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("current binary %q is not a regular file", targetPath)
	}

	src, err := os.Open(targetPath) //nolint:gosec // resolved executable path
	if err != nil {
		return fmt.Errorf("opening current binary: %w", err)
	}
	defer func() { _ = src.Close() }()

	//nolint:gosec // backup must preserve executable mode for rollback
	dst, err := os.OpenFile(backupPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return fmt.Errorf("creating current binary backup: %w", err)
	}
	_, copyErr := io.Copy(dst, src)
	// File.Sync must follow Chmod so executable-mode metadata reaches the same
	// durability barrier as contents before the rollback marker is armed.
	chmodErr := dst.Chmod(info.Mode().Perm())
	syncErr := dst.Sync()
	closeErr := dst.Close()
	if copyErr != nil {
		_ = os.Remove(backupPath)
		return fmt.Errorf("copying current binary backup: %w", copyErr)
	}
	if syncErr != nil {
		_ = os.Remove(backupPath)
		return fmt.Errorf("flushing current binary backup: %w", syncErr)
	}
	if closeErr != nil {
		_ = os.Remove(backupPath)
		return fmt.Errorf("closing current binary backup: %w", closeErr)
	}
	if chmodErr != nil {
		log.Warn().Err(chmodErr).Str("binary", backupPath).
			Msg("could not preserve binary backup permissions; filesystem may derive them from mount options")
	}
	if runtime.GOOS != "windows" {
		backupInfo, statErr := os.Stat(backupPath)
		if statErr != nil {
			return fmt.Errorf("checking binary backup permissions: %w", statErr)
		}
		if backupInfo.Mode().Perm()&0o111 == 0 {
			return fmt.Errorf("binary backup %q is not executable on the target filesystem", backupPath)
		}
	}
	if err := syncDir(filepath.Dir(targetPath)); err != nil {
		return fmt.Errorf("flushing current binary backup directory: %w", err)
	}
	return nil
}

func prepareInstallCandidate(
	ctx context.Context, stagedPath, candidatePath, version, platformID string,
) error {
	src, err := os.Open(stagedPath) //nolint:gosec // path comes from this package's verified staging result
	if err != nil {
		return fmt.Errorf("opening the staged binary: %w", err)
	}
	defer func() { _ = src.Close() }()

	//nolint:gosec // executable candidate must be runnable; archive permissions are not trusted
	dst, err := os.OpenFile(candidatePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, stagedBinaryPerm)
	if err != nil {
		return fmt.Errorf("creating the install candidate: %w", err)
	}

	_, copyErr := io.Copy(dst, src)
	syncErr := dst.Sync()
	closeErr := dst.Close()
	if copyErr != nil {
		_ = os.Remove(candidatePath)
		return fmt.Errorf("copying the staged binary to the target filesystem: %w", copyErr)
	}
	if syncErr != nil {
		_ = os.Remove(candidatePath)
		return fmt.Errorf("flushing the install candidate: %w", syncErr)
	}
	if closeErr != nil {
		_ = os.Remove(candidatePath)
		return fmt.Errorf("closing the install candidate: %w", closeErr)
	}
	// A restrictive process umask may have removed execute bits from OpenFile's
	// mode. Filesystems without mode bits can reject Chmod while still executing
	// the file through mount options, so the probe below remains authoritative.
	//nolint:gosec // an executable candidate must be executable
	if err := os.Chmod(candidatePath, stagedBinaryPerm); err != nil {
		log.Warn().Err(err).Str("binary", candidatePath).
			Msg("could not set install candidate permissions; leaving the decision to the probe")
	}
	if err := syncDir(filepath.Dir(candidatePath)); err != nil {
		_ = os.Remove(candidatePath)
		return fmt.Errorf("flushing the install candidate directory: %w", err)
	}

	// Probe again after copying because staging and the live install can be on
	// different filesystems with different execution and permission semantics.
	probe := newProbeStager(platformID)
	if err := probe.probeBinary(ctx, candidatePath, version); err != nil {
		_ = os.Remove(candidatePath)
		return fmt.Errorf("probing the install candidate on the target filesystem: %w", err)
	}
	return nil
}

func abortInstallAfterError(
	ctx context.Context, dataDir string, m *pendingMarker, candidatePath string,
	binary installBinaryOps, installErr error,
) error {
	if abortErr := abortInstall(ctx, dataDir, m, candidatePath, binary); abortErr != nil {
		return fmt.Errorf("%w; aborting the partial install also failed: %w", installErr, abortErr)
	}
	return installErr
}

// abortInstall restores only the binary. The new version has not run yet, so
// restoring the UserDB snapshot would discard writes made while Apply was live.
// Callers hold markerMu.
func abortInstall(
	ctx context.Context, dataDir string, m *pendingMarker, candidatePath string, binary installBinaryOps,
) error {
	if m == nil {
		return nil
	}
	if err := restoreBinaryAfterFailedInstall(m, binary); err != nil {
		return err
	}

	if err := clearMarker(stateDirFor(dataDir)); err != nil {
		return err
	}
	removeInstallArtifacts(ctx, m, candidatePath, binary)
	return nil
}

// restoreBinaryAfterFailedInstall puts the outgoing binary back, but only where
// something actually took its place.
//
// Every way the install swap can fail leaves the target holding the binary it
// started with, or holding nothing at all. Where it still holds the old binary
// there is nothing to restore, and restoring anyway would not be harmless: on a
// platform that has to vacate the name before writing it, it moves the image
// this process is running from into a hidden name and opens a second window
// where the device has no executable — to install a copy of what is already
// there.
func restoreBinaryAfterFailedInstall(m *pendingMarker, binary installBinaryOps) error {
	_, targetErr := os.Stat(m.TargetPath)
	if targetErr != nil && !errors.Is(targetErr, os.ErrNotExist) {
		return fmt.Errorf("checking the binary after a failed install: %w", targetErr)
	}
	installed := targetErr == nil
	if installed && !m.BinaryReplaced {
		return nil
	}

	if _, backupErr := os.Stat(m.BackupPath); backupErr == nil {
		if restoreErr := binary.replace(m.BackupPath, m.TargetPath); restoreErr != nil {
			return fmt.Errorf("restoring the current binary after a failed install: %w", restoreErr)
		}
		if syncErr := syncDir(filepath.Dir(m.TargetPath)); syncErr != nil {
			return fmt.Errorf("flushing the restored current binary: %w", syncErr)
		}
	} else if errors.Is(backupErr, os.ErrNotExist) {
		if !installed {
			return fmt.Errorf("failed install has neither current binary nor backup: %w", targetErr)
		}
		log.Warn().
			Str("target", m.TargetPath).
			Str("backup", m.BackupPath).
			Msg("failed install backup is missing; leaving the installed binary in place")
	} else {
		return fmt.Errorf("checking the current binary backup after a failed install: %w", backupErr)
	}
	return nil
}

func removeInstallArtifacts(
	ctx context.Context, m *pendingMarker, candidatePath string, binary installBinaryOps,
) {
	if m != nil {
		binary.sweep(m.TargetPath)
	}
	if m != nil && m.BackupPath != "" {
		if err := os.Remove(m.BackupPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Warn().Err(err).Str("path", m.BackupPath).Msg("could not remove unused binary backup")
		}
	}
	if candidatePath != "" {
		if err := os.Remove(candidatePath); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Warn().Err(err).Str("path", candidatePath).Msg("could not remove update install candidate")
		}
	}
	if m != nil && m.UserDBSnapshotPath != "" {
		if err := os.Remove(m.UserDBSnapshotPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Warn().Err(err).Str("path", m.UserDBSnapshotPath).Msg("could not remove unused update snapshot")
		}
	}
	if m != nil && m.StagingDir != "" {
		if err := removeStagingDir(ctx, m.StagingDir); err != nil {
			log.Warn().Err(err).Str("dir", m.StagingDir).Msg("could not remove unused update staging directory")
		}
	}
}

func installSidecarPath(targetPath, suffix string) string {
	ext := filepath.Ext(targetPath)
	base := strings.TrimSuffix(targetPath, ext)
	return base + suffix + ext
}
