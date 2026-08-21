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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/updatepayload"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
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

type payloadInstallOps struct {
	fs            afero.Fs
	replace       func(string, string) error
	remove        func(string) error
	stat          func(string) (os.FileInfo, error)
	syncDirectory func(string) error
	saveMarker    func(string, *pendingMarker) error
}

func defaultPayloadInstallOps() payloadInstallOps {
	return payloadInstallOps{
		fs:            afero.NewOsFs(),
		replace:       replaceFile,
		remove:        os.Remove,
		stat:          os.Lstat,
		syncDirectory: syncDir,
		saveMarker:    saveMarker,
	}
}

func (o payloadInstallOps) withDefaults() payloadInstallOps {
	defaults := defaultPayloadInstallOps()
	if o.fs == nil {
		o.fs = defaults.fs
	}
	_, osBacked := o.fs.(*afero.OsFs)
	if o.replace == nil {
		if osBacked {
			o.replace = defaults.replace
		} else {
			o.replace = o.fs.Rename
		}
	}
	if o.remove == nil {
		o.remove = o.fs.Remove
	}
	if o.stat == nil {
		if osBacked {
			o.stat = defaults.stat
		} else {
			o.stat = o.fs.Stat
		}
	}
	if o.syncDirectory == nil {
		if osBacked {
			o.syncDirectory = defaults.syncDirectory
		} else {
			o.syncDirectory = func(string) error { return nil }
		}
	}
	if o.saveMarker == nil {
		o.saveMarker = defaults.saveMarker
	}
	return o
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
	payload            payloadInstallOps
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

	payloadOps := opts.payload.withDefaults()
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

	payloadBackups, payloadErr := preparePayloadCandidates(opts.Staged, opts.TargetPath, payloadOps)
	if payloadErr != nil {
		_ = os.Remove(candidatePath)
		removePreparedPayload(payloadBackups, payloadOps)
		if cleanupErr := removeStagingDir(ctx, opts.Staged.Dir); cleanupErr != nil {
			log.Warn().Err(cleanupErr).Str("dir", opts.Staged.Dir).
				Msg("could not remove staging after payload preparation failed")
		}
		return payloadErr
	}

	// Everything up to here can be abandoned by deleting candidates and backups. From the
	// snapshot on, the device is committed to either finishing or unwinding, so
	// this is where a caller gets its last say.
	if opts.PreQuiesce != nil {
		if err := opts.PreQuiesce(ctx); err != nil {
			_ = os.Remove(candidatePath)
			removePreparedPayload(payloadBackups, payloadOps)
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
		removePreparedPayload(payloadBackups, payloadOps)
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
		PayloadBackups:     payloadBackups,
	}
	if err := preserveCurrentBinary(opts.TargetPath, backupPath); err != nil {
		removeInstallArtifacts(ctx, m, candidatePath, opts.binary, payloadOps)
		return fmt.Errorf("preserving the current binary: %w", err)
	}
	if err := saveMarker(dir, m); err != nil {
		removeInstallArtifacts(ctx, m, candidatePath, opts.binary, payloadOps)
		return fmt.Errorf("recording the start of the update install: %w", err)
	}

	for _, payload := range m.PayloadBackups {
		if err := payloadOps.replace(payload.CandidatePath, payload.TargetPath); err != nil {
			return abortInstallAfterErrorWithOps(ctx, opts.DataDir, m, candidatePath, opts.binary, payloadOps,
				fmt.Errorf("installing payload file %q: %w", payload.TargetPath, err))
		}
		if err := payloadOps.syncDirectory(filepath.Dir(payload.TargetPath)); err != nil {
			return abortInstallAfterErrorWithOps(ctx, opts.DataDir, m, candidatePath, opts.binary, payloadOps,
				fmt.Errorf("flushing installed payload file %q: %w", payload.TargetPath, err))
		}
	}

	// Target holds the old executable until this swap and the verified new one
	// after it. Where the platform lets a running binary be overwritten that is
	// one rename, so a power cut lands on one side or the other. Where it has
	// to be vacated first the target name is briefly empty instead, and the
	// marker written above is what tells the next boot to put the backup back.
	if err := opts.binary.replace(candidatePath, opts.TargetPath); err != nil {
		return abortInstallAfterErrorWithOps(ctx, opts.DataDir, m, candidatePath, opts.binary, payloadOps,
			fmt.Errorf("installing the staged binary: %w", err))
	}
	// From here the target name means the new binary, so an unwind has real work
	// to do. Before it, a failed swap left the old one exactly where it was and
	// the unwind has to leave it alone.
	m.BinaryReplaced = true
	if err := syncDir(filepath.Dir(opts.TargetPath)); err != nil {
		return abortInstallAfterErrorWithOps(ctx, opts.DataDir, m, candidatePath, opts.binary, payloadOps,
			fmt.Errorf("flushing the installed binary: %w", err))
	}

	m.State = markerInstalled
	if err := saveMarker(dir, m); err != nil {
		return abortInstallAfterErrorWithOps(ctx, opts.DataDir, m, candidatePath, opts.binary, payloadOps,
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

func preparePayloadCandidates(
	staged *StagedUpdate,
	binaryPath string,
	ops payloadInstallOps,
) ([]payloadBackup, error) {
	if staged == nil || len(staged.payloadFiles) == 0 {
		return nil, nil
	}
	ops = ops.withDefaults()
	prepared := make([]payloadBackup, 0, len(staged.payloadFiles))
	for _, file := range staged.payloadFiles {
		targetPath, ok := updatepayload.ResolveInstallPath(binaryPath, file.RelativePath)
		if !ok {
			return prepared, fmt.Errorf("payload target %q is not configured", file.RelativePath)
		}
		entry := payloadBackup{
			TargetPath:    targetPath,
			BackupPath:    installSidecarPath(targetPath, installBackupSuffix),
			CandidatePath: installSidecarPath(targetPath, installCandidateSuffix),
		}
		prepared = append(prepared, entry)
		entryAt := &prepared[len(prepared)-1]

		//nolint:gosec // target path is constrained beneath the resolved install root
		if err := (afero.Afero{Fs: ops.fs}).MkdirAll(filepath.Dir(targetPath), stateDirPerm); err != nil {
			return prepared, fmt.Errorf("creating payload target directory: %w", err)
		}
		if err := ops.remove(entry.CandidatePath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return prepared, fmt.Errorf("removing stale payload candidate %q: %w", entry.CandidatePath, err)
		}

		targetInfo, statErr := ops.stat(targetPath)
		switch {
		case errors.Is(statErr, os.ErrNotExist):
			entryAt.OriginalMissing = true
			entryAt.BackupPath = ""
		case statErr != nil:
			return prepared, fmt.Errorf("reading payload target %q: %w", targetPath, statErr)
		case !targetInfo.Mode().IsRegular():
			return prepared, fmt.Errorf("payload target %q is not a regular file", targetPath)
		}

		if err := copyPayloadFile(ops, file.Path, entry.CandidatePath, file.Mode); err != nil {
			return prepared, fmt.Errorf("preparing payload candidate %q: %w", targetPath, err)
		}
		if err := ops.syncDirectory(filepath.Dir(targetPath)); err != nil {
			return prepared, fmt.Errorf("flushing payload candidate %q: %w", targetPath, err)
		}
		if entryAt.OriginalMissing {
			continue
		}
		if _, backupErr := ops.stat(entry.BackupPath); backupErr == nil {
			if err := ops.remove(entry.BackupPath); err != nil {
				return prepared, fmt.Errorf("removing orphaned payload backup %q: %w", entry.BackupPath, err)
			}
		} else if !errors.Is(backupErr, os.ErrNotExist) {
			return prepared, fmt.Errorf("checking payload backup %q: %w", entry.BackupPath, backupErr)
		}
		if err := copyPayloadFile(ops, targetPath, entry.BackupPath, targetInfo.Mode().Perm()); err != nil {
			return prepared, fmt.Errorf("preserving payload target %q: %w", targetPath, err)
		}
		if err := ops.syncDirectory(filepath.Dir(targetPath)); err != nil {
			return prepared, fmt.Errorf("flushing payload backup %q: %w", targetPath, err)
		}
	}
	return prepared, nil
}

func copyPayloadFile(ops payloadInstallOps, source, target string, mode os.FileMode) error {
	ops = ops.withDefaults()
	//nolint:gosec // source and target are derived from verified staging and a controlled install root
	src, err := ops.fs.Open(source)
	if err != nil {
		return fmt.Errorf("opening payload source: %w", err)
	}
	defer func() { _ = src.Close() }()

	//nolint:gosec // payload modes are normalized during staging
	dst, err := ops.fs.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode.Perm())
	if err != nil {
		return fmt.Errorf("creating payload sidecar: %w", err)
	}
	_, copyErr := io.Copy(dst, src)
	chmodErr := ops.fs.Chmod(target, mode.Perm())
	syncErr := dst.Sync()
	closeErr := dst.Close()
	if copyErr != nil {
		_ = ops.remove(target)
		return fmt.Errorf("copying payload sidecar: %w", copyErr)
	}
	if chmodErr != nil {
		_ = ops.remove(target)
		return fmt.Errorf("setting payload sidecar permissions: %w", chmodErr)
	}
	if syncErr != nil {
		_ = ops.remove(target)
		return fmt.Errorf("flushing payload sidecar: %w", syncErr)
	}
	if closeErr != nil {
		_ = ops.remove(target)
		return fmt.Errorf("closing payload sidecar: %w", closeErr)
	}
	return nil
}

func removePreparedPayload(payloads []payloadBackup, ops payloadInstallOps) {
	ops = ops.withDefaults()
	for _, payload := range payloads {
		for _, path := range []string{payload.CandidatePath, payload.BackupPath} {
			if path == "" {
				continue
			}
			if err := ops.remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				log.Warn().Err(err).Str("path", path).Msg("could not remove unused payload sidecar")
			}
		}
	}
}

func abortInstallAfterError(
	ctx context.Context, dataDir string, m *pendingMarker, candidatePath string,
	binary installBinaryOps, installErr error,
) error {
	return abortInstallAfterErrorWithOps(
		ctx, dataDir, m, candidatePath, binary, defaultPayloadInstallOps(), installErr,
	)
}

func abortInstallAfterErrorWithOps(
	ctx context.Context, dataDir string, m *pendingMarker, candidatePath string,
	binary installBinaryOps, payloadOps payloadInstallOps, installErr error,
) error {
	if abortErr := abortInstallWithOps(ctx, dataDir, m, candidatePath, binary, payloadOps); abortErr != nil {
		return fmt.Errorf("%w; aborting the partial install also failed: %w", installErr, abortErr)
	}
	return installErr
}

// abortInstall restores payload files and the binary without restoring UserDB.
// The incoming version has not run yet, so restoring its snapshot would discard
// writes made while Apply was live. Callers hold markerMu.
func abortInstall(
	ctx context.Context, dataDir string, m *pendingMarker, candidatePath string, binary installBinaryOps,
) error {
	return abortInstallWithOps(ctx, dataDir, m, candidatePath, binary, defaultPayloadInstallOps())
}

func abortInstallWithOps(
	ctx context.Context,
	dataDir string,
	m *pendingMarker,
	candidatePath string,
	binary installBinaryOps,
	payloadOps payloadInstallOps,
) error {
	if m == nil {
		return nil
	}
	payloadOps = payloadOps.withDefaults()
	fileOps := defaultWatchdogFileOps()
	fileOps.fs = payloadOps.fs
	fileOps.binary = binary
	fileOps.replace = payloadOps.replace
	fileOps.syncDirectory = payloadOps.syncDirectory
	fileOps.marker.save = payloadOps.saveMarker
	if err := restorePayloadFiles(stateDirFor(dataDir), m, fileOps); err != nil {
		return err
	}
	if err := restoreBinaryAfterFailedInstall(m, binary); err != nil {
		return err
	}

	if err := clearMarker(stateDirFor(dataDir)); err != nil {
		return err
	}
	removeInstallArtifacts(ctx, m, candidatePath, binary, payloadOps)
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
	ctx context.Context,
	m *pendingMarker,
	candidatePath string,
	binary installBinaryOps,
	payloadOptions ...payloadInstallOps,
) {
	payloadOps := defaultPayloadInstallOps()
	if len(payloadOptions) > 0 {
		payloadOps = payloadOptions[0].withDefaults()
	}
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
	if m != nil {
		removePreparedPayload(m.PayloadBackups, payloadOps)
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
