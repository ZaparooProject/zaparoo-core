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
	UserDB             UpdateBackupper
	Staged             *StagedUpdate
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

	if candidateErr := prepareInstallCandidate(ctx, opts.Staged.BinaryPath, candidatePath,
		opts.Staged.Version, opts.PlatformID); candidateErr != nil {
		if cleanupErr := removeStagingDir(ctx, opts.Staged.Dir); cleanupErr != nil {
			log.Warn().Err(cleanupErr).Str("dir", opts.Staged.Dir).
				Msg("could not remove staging after candidate preparation failed")
		}
		return candidateErr
	}

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
		removeInstallArtifacts(ctx, m, candidatePath)
		return fmt.Errorf("preserving the current binary: %w", err)
	}
	if err := saveMarker(dir, m); err != nil {
		removeInstallArtifacts(ctx, m, candidatePath)
		return fmt.Errorf("recording the start of the update install: %w", err)
	}

	// Target remains the old executable until this single atomic replacement.
	// A power cut before it leaves the old target intact; one after it leaves the
	// verified target plus the durable old-binary copy and marker.
	if err := replaceFile(candidatePath, opts.TargetPath); err != nil {
		return abortInstallAfterError(ctx, opts.DataDir, m, candidatePath,
			fmt.Errorf("installing the staged binary: %w", err))
	}
	if err := syncDir(filepath.Dir(opts.TargetPath)); err != nil {
		return abortInstallAfterError(ctx, opts.DataDir, m, candidatePath,
			fmt.Errorf("flushing the installed binary: %w", err))
	}

	m.State = markerInstalled
	if err := saveMarker(dir, m); err != nil {
		return abortInstallAfterError(ctx, opts.DataDir, m, candidatePath,
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
	ctx context.Context, dataDir string, m *pendingMarker, candidatePath string, installErr error,
) error {
	if abortErr := abortInstall(ctx, dataDir, m, candidatePath); abortErr != nil {
		return fmt.Errorf("%w; aborting the partial install also failed: %w", installErr, abortErr)
	}
	return installErr
}

// abortInstall restores only the binary. The new version has not run yet, so
// restoring the UserDB snapshot would discard writes made while Apply was live.
// Callers hold markerMu.
func abortInstall(ctx context.Context, dataDir string, m *pendingMarker, candidatePath string) error {
	if m == nil {
		return nil
	}
	if _, backupErr := os.Stat(m.BackupPath); backupErr == nil {
		if restoreErr := replaceFile(m.BackupPath, m.TargetPath); restoreErr != nil {
			return fmt.Errorf("restoring the current binary after a failed install: %w", restoreErr)
		}
		if syncErr := syncDir(filepath.Dir(m.TargetPath)); syncErr != nil {
			return fmt.Errorf("flushing the restored current binary: %w", syncErr)
		}
	} else if errors.Is(backupErr, os.ErrNotExist) {
		if _, targetErr := os.Stat(m.TargetPath); targetErr != nil {
			return fmt.Errorf("failed install has neither current binary nor backup: %w", targetErr)
		}
	} else {
		return fmt.Errorf("checking the current binary backup after a failed install: %w", backupErr)
	}

	if err := clearMarker(stateDirFor(dataDir)); err != nil {
		return err
	}
	removeInstallArtifacts(ctx, m, candidatePath)
	return nil
}

func removeInstallArtifacts(ctx context.Context, m *pendingMarker, candidatePath string) {
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
