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
	"os"
	"path/filepath"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/userdb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/inbox"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

// rollbackAttemptLimit is how many boots may try to roll one update back.
//
// A rollback that fails on something time might fix keeps its marker so the next
// boot resumes it, and startup carries on in between. That is worth doing a few
// times and no more: past this the device is not recovering on its own, and each
// further attempt is another boot spent running a version that already failed.
// Giving up is not the worse outcome — it records why, tells the user, and
// leaves the snapshot on disk to restore by hand.
const rollbackAttemptLimit = 3

// watchdogAction is what the boot after an update has to do about it.
type watchdogAction int

const (
	// actionNone means there is nothing to resolve and startup carries on.
	actionNone watchdogAction = iota
	// actionConfirm means the new version is running and now has to prove it
	// stays up. The marker moves to confirming and startup carries on.
	actionConfirm
	// actionClear means the marker describes an update this process is not part
	// of, so it is discarded and startup carries on.
	actionClear
	// actionAbortInstall means the outgoing version is still running after an
	// interrupted binary swap, so only the old binary and unused artifacts need
	// restoring; the live UserDB was never opened by the target version.
	actionAbortInstall
	// actionFinalize completes idempotent cleanup after a terminal outcome was
	// durably recorded.
	actionFinalize
	// actionRollBack means the update did not work and the previous version has
	// to be put back.
	actionRollBack
)

type watchdogFileOps struct {
	fs      afero.Fs
	replace func(string, string) error
	// binary puts back a file that may be the image this process is running
	// from, which not every platform will let a plain replacement touch, and
	// clears whatever an earlier swap had to leave behind to do it.
	binary          installBinaryOps
	syncDirectory   func(string) error
	removeStaging   func(context.Context, string) error
	restoreDatabase func(context.Context, afero.Fs, string, string) error
}

// ErrRolledBack means an update was rolled back and the previous version is now
// on disk. The process is still running the image that failed, so every caller
// of Start has to re-exec rather than exit: on the platforms this matters for
// there is no supervisor to start anything again.
var (
	ErrRolledBack           = errors.New("update rolled back, restart into the restored version")
	errRollbackPrerequisite = errors.New("rollback prerequisite is unavailable")
)

type rolledBackError struct {
	cause      error
	targetPath string
}

func (e *rolledBackError) Error() string {
	if e.cause == nil {
		return ErrRolledBack.Error()
	}
	return fmt.Sprintf("%s: %v", ErrRolledBack, e.cause)
}

func (e *rolledBackError) Unwrap() []error {
	if e.cause == nil {
		return []error{ErrRolledBack}
	}
	return []error{ErrRolledBack, e.cause}
}

func defaultWatchdogFileOps() watchdogFileOps {
	return watchdogFileOps{
		fs:              afero.NewOsFs(),
		replace:         replaceFile,
		binary:          defaultInstallBinaryOps(),
		syncDirectory:   syncDir,
		removeStaging:   removeStagingDir,
		restoreDatabase: userdb.RestoreFileTo,
	}
}

func newRolledBackError(targetPath string, cause error) error {
	return &rolledBackError{targetPath: targetPath, cause: cause}
}

// RollbackTargetPath returns the restored executable path carried by a rollback
// result, allowing callers to re-exec that path rather than the failing image.
func RollbackTargetPath(err error) (string, bool) {
	var rollbackErr *rolledBackError
	if !errors.As(err, &rollbackErr) || rollbackErr.targetPath == "" {
		return "", false
	}
	return rollbackErr.targetPath, true
}

// decideWatchdogAction maps a marker and the version actually running onto what
// to do about it. It is separated from the work so the table can be tested
// without a filesystem, which matters because getting a row wrong here either
// bricks a device or undoes a good update.
func decideWatchdogAction(m *pendingMarker, currentVersion string) watchdogAction {
	if m == nil {
		return actionNone
	}
	// A durable terminal outcome turns startup into idempotent cleanup, never
	// another rollback. A blocked rollback deliberately retains its marker and
	// snapshot for manual recovery.
	switch m.Outcome {
	case outcomeSucceeded, outcomeRolledBack:
		return actionFinalize
	case outcomeRollbackBlocked:
		return actionNone
	case "":
		// Continue with the in-progress state below.
	default:
		return actionNone
	}

	switch m.State {
	case markerInstalling:
		if m.PreviousVersion == currentVersion {
			return actionAbortInstall
		}
		return actionRollBack

	case markerRollingBack:
		// A rollback started and did not finish. Whatever is running now, the
		// files are half swapped and the only way out is through.
		return actionRollBack

	case markerInstalled:
		// The install completed and this is the first boot after it. Running the
		// version the update aimed for means the binary at least starts.
		if m.TargetVersion == currentVersion {
			return actionConfirm
		}
		// If the outgoing version is still running, the target never opened the
		// UserDB and the install can be aborted without restoring its snapshot.
		if m.PreviousVersion == currentVersion {
			return actionAbortInstall
		}
		return actionRollBack

	case markerConfirming:
		if m.TargetVersion == currentVersion {
			// The previous boot got this far and then never confirmed, so the
			// new version starts but does not survive startup.
			return actionRollBack
		}
		// A different version is running, so this marker belongs to an update
		// that was superseded or undone by other means.
		return actionClear

	default:
		// Same schema version, unrecognised state: the file is damaged in a way
		// parsing did not catch, and it cannot direct a rollback.
		return actionClear
	}
}

// RunStartupWatchdog resolves any update left pending by a previous boot. It
// runs before configuration, the databases or the network are available,
// because the failure it exists to catch is a binary that cannot get that far.
//
// It returns ErrRolledBack when the previous version has been put back and the
// caller must re-exec into it. Every other error is advisory: startup continues,
// because refusing to boot over a bookkeeping problem is the outcome this whole
// mechanism exists to avoid.
func RunStartupWatchdog(ctx context.Context, dataDir, currentVersion string) error {
	return runStartupWatchdogWithOps(ctx, dataDir, currentVersion, defaultWatchdogFileOps())
}

func runStartupWatchdogWithOps(
	ctx context.Context, dataDir, currentVersion string, fileOps watchdogFileOps,
) error {
	markerMu.Lock()
	defer markerMu.Unlock()

	dir := stateDirFor(dataDir)
	m, err := loadMarker(dir)
	if err != nil {
		// A newer or unusable marker cannot safely direct this build. Leave it and
		// every update snapshot alone so a compatible build or operator still has
		// the evidence and recovery files.
		log.Warn().Err(err).Msg("leaving an update marker this build cannot safely resolve")
		if errors.Is(err, errMarkerUnusable) {
			if resultErr := recordUnusableMarkerResult(fileOps.fs, dir, currentVersion); resultErr != nil {
				log.Warn().Err(resultErr).Msg("could not record unusable update marker for user notification")
			}
		}
		return nil
	}

	switch decideWatchdogAction(m, currentVersion) {
	case actionNone:
		if m != nil && m.Outcome == outcomeRollbackBlocked {
			if err := recordMarkerResult(dir, m); err != nil {
				log.Warn().Err(err).Msg("could not retain the blocked rollback result")
			}
		}
		sweepUpdateSnapshotsFS(fileOps.fs, dataDir, snapshotToKeep(m))
		return nil

	case actionClear:
		log.Info().
			Str("markerVersion", m.TargetVersion).
			Str("running", currentVersion).
			Msg("discarding an update marker that does not describe this version")
		if err := clearMarker(dir); err != nil {
			return err
		}
		sweepUpdateSnapshotsFS(fileOps.fs, dataDir, "")
		return nil

	case actionConfirm:
		m.State = markerConfirming
		m.Attempts++
		if err := saveMarker(dir, m); err != nil {
			// Without confirming on disk a failed startup looks like a first
			// boot to the next one, so it would try to confirm again instead of
			// rolling back. Report it; the next boot re-runs this same step.
			return fmt.Errorf("recording that the updated version is being confirmed: %w", err)
		}
		log.Info().
			Str("from", m.PreviousVersion).
			Str("to", m.TargetVersion).
			Int("attempts", m.Attempts).
			Msg("confirming an updated version")
		return nil

	case actionAbortInstall:
		return abortInstall(ctx, dataDir, m,
			installSidecarPath(m.TargetPath, installCandidateSuffix), fileOps.binary)

	case actionFinalize:
		return finalizeTerminalUpdate(ctx, dataDir, m, fileOps)

	case actionRollBack:
		return rollBack(ctx, dataDir, m, fileOps)

	default:
		return nil
	}
}

// RollBackFailedStart is the same recovery driven by a startup that got past the
// watchdog and then failed anyway. The watchdog only sees a process that never
// started; this sees one that started and could not finish, which on a device
// with no supervisor is just as fatal.
func RollBackFailedStart(ctx context.Context, dataDir, currentVersion string) error {
	return rollBackFailedStartWithOps(ctx, dataDir, currentVersion, defaultWatchdogFileOps())
}

func rollBackFailedStartWithOps(
	ctx context.Context, dataDir, currentVersion string, fileOps watchdogFileOps,
) error {
	markerMu.Lock()
	defer markerMu.Unlock()

	m, err := loadMarker(stateDirFor(dataDir))
	if err != nil || m == nil {
		return nil //nolint:nilerr // a marker this build cannot own is not ours to act on
	}
	if m.Outcome != "" || m.TargetVersion != currentVersion {
		return nil
	}
	log.Error().
		Str("from", m.PreviousVersion).
		Str("to", m.TargetVersion).
		Msg("the updated version failed to start, rolling back")
	return rollBack(ctx, dataDir, m, fileOps)
}

// RecordCleanShutdown resets a confirming marker so an orderly stop during the
// soak window restarts confirmation on the next boot instead of looking like a
// crash. A startup failure still rolls back through Start's deferred hook after
// cleanup completes.
func RecordCleanShutdown(dataDir, currentVersion string) error {
	markerMu.Lock()
	defer markerMu.Unlock()

	dir := stateDirFor(dataDir)
	m, err := loadMarker(dir)
	if err != nil || m == nil {
		return nil // an unreadable or foreign marker is not ours to change
	}
	if m.State != markerConfirming || m.Outcome != "" || m.TargetVersion != currentVersion {
		return nil
	}
	m.State = markerInstalled
	return saveMarker(dir, m)
}

// Confirm commits an update that has stayed up long enough to be trusted. The
// terminal outcome is made durable before cleanup, so a crash at any cleanup
// boundary resumes cleanup rather than attempting rollback without its files.
func Confirm(ctx context.Context, dataDir, currentVersion string) (string, error) {
	return confirmWithOps(ctx, dataDir, currentVersion, defaultWatchdogFileOps())
}

func confirmWithOps(
	ctx context.Context, dataDir, currentVersion string, fileOps watchdogFileOps,
) (string, error) {
	markerMu.Lock()
	defer markerMu.Unlock()

	dir := stateDirFor(dataDir)
	m, err := loadMarker(dir)
	if err != nil || m == nil {
		return "", nil //nolint:nilerr // a marker this build cannot read is not ours to commit
	}
	if m.State != markerConfirming || m.Outcome != "" || m.TargetVersion != currentVersion {
		return "", nil
	}

	m.Outcome = outcomeSucceeded
	m.OutcomeAt = time.Now().UTC()
	if err := saveMarker(dir, m); err != nil {
		return "", fmt.Errorf("recording the confirmed update: %w", err)
	}
	if err := finalizeTerminalUpdate(ctx, dataDir, m, fileOps); err != nil {
		return m.TargetVersion, err
	}

	log.Info().
		Str("from", m.PreviousVersion).
		Str("to", m.TargetVersion).
		Msg("update confirmed")
	return m.TargetVersion, nil
}

// ReportLastUpdate posts the outcome of an update that finished before there was
// an inbox to post it to. Rollbacks are exactly that case: they run before the
// databases are open and then re-exec, so this is the only chance the user gets
// to hear that the version they installed did not work.
func ReportLastUpdate(dataDir string, inboxSvc *inbox.Service) {
	if inboxSvc == nil {
		return
	}
	dir := stateDirFor(dataDir)
	res := peekUpdateResult(dir)
	if res == nil {
		return
	}

	var title, body string
	severity := inbox.SeverityWarning
	switch res.Outcome {
	case outcomeRolledBack:
		title = "Update rolled back"
		body = fmt.Sprintf(
			"Version %s did not start, so version %s was restored. "+
				"Your data was restored from the snapshot taken before the update.",
			res.ToVersion, res.FromVersion)
	case outcomeRollbackBlocked:
		severity = inbox.SeverityError
		title = "Update could not be rolled back"
		body = fmt.Sprintf(
			"Version %s did not start and version %s could not be restored automatically. "+
				"The snapshot taken before the update is still in your backups and can be "+
				"restored by hand.",
			res.ToVersion, res.FromVersion)
	case outcomeSucceeded:
		severity = inbox.SeverityInfo
		title = "Update installed"
		body = fmt.Sprintf("Updated from version %s to version %s.", res.FromVersion, res.ToVersion)
	case outcomeRecoveryRequired:
		severity = inbox.SeverityError
		title = "Update needs manual recovery"
		body = "Core found update recovery data it cannot safely read. " +
			"Automatic and manual updates remain blocked while the quarantined pending.json.bad marker remains. " +
			"Preserve that marker and updater backups for support-assisted recovery."
	default:
		return
	}
	if res.Detail != "" {
		body += "\n\n" + res.Detail
	}

	if err := inboxSvc.Add(
		title,
		inbox.WithBody(body),
		inbox.WithCategory(inbox.CategoryUpdateResult),
		inbox.WithSeverity(severity),
	); err != nil {
		log.Error().Err(err).Msg("failed to add update result inbox message")
		return
	}
	if err := markUpdateResultReported(dir, res); err != nil {
		log.Warn().Err(err).Msg("update result was posted but could not be marked reported")
	}
}

// rollBack puts the previous version back. Callers hold markerMu.
//
// The order is deliberate. The user database is restored first, because that is
// the step with a way out: if it fails, nothing has moved and the device carries
// on running the new version, which at worst is suspect. Restoring the binary
// first and then failing on the database would leave an old binary in front of a
// database migrated past what it can open, and that combination does not boot.
func recordUnusableMarkerResult(fs afero.Fs, dir, currentVersion string) error {
	var at time.Time
	for _, path := range []string{markerPath(dir) + markerBadSuffix, markerPath(dir)} {
		if info, err := fs.Stat(path); err == nil {
			at = info.ModTime().UTC()
			break
		}
	}
	return recordUpdateResult(dir, &updateResult{
		At:        at,
		Outcome:   outcomeRecoveryRequired,
		ToVersion: currentVersion,
	})
}

func rollBack(
	ctx context.Context, dataDir string, m *pendingMarker, fileOps watchdogFileOps,
) error {
	dir := stateDirFor(dataDir)
	m.State = markerRollingBack
	m.RollbackAttempts++
	if err := saveMarker(dir, m); err != nil {
		// Nothing has been moved yet, and without this on disk an interrupted
		// rollback would not know to resume, nor how many times it already has.
		// Stop here rather than start a swap that cannot be finished.
		return fmt.Errorf("recording the start of the rollback: %w", err)
	}

	if !m.UserDBRestored {
		if err := restoreUserDB(ctx, dataDir, m, fileOps); err != nil {
			return handleRollbackFailure(dir, m, err)
		}
		// Recorded before the binary moves, because a rollback that fails after
		// this point resumes on the next boot with the device having been in
		// use in between. Writing the snapshot again then would throw those
		// writes away.
		m.UserDBRestored = true
		if err := saveMarker(dir, m); err != nil {
			return handleRollbackFailure(dir, m,
				fmt.Errorf("recording the restored user database: %w", err))
		}
	}
	if err := restoreReplacedFiles(dir, m, fileOps); err != nil {
		return handleRollbackFailure(dir, m, err)
	}

	m.Outcome = outcomeRolledBack
	m.OutcomeAt = time.Now().UTC()
	if err := saveMarker(dir, m); err != nil {
		return newRolledBackError(m.TargetPath,
			fmt.Errorf("recording the completed rollback: %w", err))
	}
	if err := finalizeTerminalUpdate(ctx, dataDir, m, fileOps); err != nil {
		return newRolledBackError(m.TargetPath,
			fmt.Errorf("finalizing the completed rollback: %w", err))
	}

	log.Warn().
		Str("failed", m.TargetVersion).
		Str("restored", m.PreviousVersion).
		Msg("update rolled back")
	return newRolledBackError(m.TargetPath, nil)
}

func handleRollbackFailure(dir string, m *pendingMarker, cause error) error {
	if errors.Is(cause, errRollbackPrerequisite) {
		return blockRollback(dir, m, cause)
	}
	if m.RollbackAttempts >= rollbackAttemptLimit {
		return blockRollback(dir, m, fmt.Errorf(
			"rollback failed on %d attempts: %w", m.RollbackAttempts, cause))
	}
	log.Error().Err(cause).
		Str("failed", m.TargetVersion).
		Str("wanted", m.PreviousVersion).
		Int("attempts", m.RollbackAttempts).
		Msg("rollback hit a retryable error; leaving it pending")
	return fmt.Errorf("rollback remains pending after a retryable error: %w", cause)
}

// blockRollback gives up on a rollback only when a prerequisite is permanently
// missing or corrupt. Retryable I/O errors leave markerRollingBack intact.
func blockRollback(dir string, m *pendingMarker, cause error) error {
	log.Error().Err(cause).
		Str("failed", m.TargetVersion).
		Str("wanted", m.PreviousVersion).
		Msg("could not roll the update back, leaving the new version installed")

	m.Outcome = outcomeRollbackBlocked
	m.OutcomeAt = time.Now().UTC()
	m.OutcomeDetail = cause.Error()
	if err := saveMarker(dir, m); err != nil {
		return fmt.Errorf("recording that rollback was abandoned: %w", err)
	}
	if err := recordMarkerResult(dir, m); err != nil {
		log.Warn().Err(err).Msg("could not record the blocked rollback result")
	}
	return nil
}

// restoreUserDB puts the snapshot taken before the install back over the live
// database. It runs once per update, not once per rollback attempt: a rollback
// that fails afterwards leaves the device running and in use until the next boot
// resumes it, and writing the snapshot a second time would discard everything
// done in between. Callers check m.UserDBRestored.
func restoreUserDB(
	ctx context.Context, dataDir string, m *pendingMarker, fileOps watchdogFileOps,
) error {
	if m.UserDBSnapshotPath == "" {
		return fmt.Errorf("%w: the update recorded no user database snapshot", errRollbackPrerequisite)
	}
	if _, err := fileOps.fs.Stat(m.UserDBSnapshotPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: user database snapshot: %w", errRollbackPrerequisite, err)
		}
		return fmt.Errorf("reading the user database snapshot: %w", err)
	}
	dbPath := filepath.Join(dataDir, config.UserDbFile)
	if err := fileOps.restoreDatabase(ctx, fileOps.fs, m.UserDBSnapshotPath, dbPath); err != nil {
		if errors.Is(err, userdb.ErrInvalidBackup) {
			return fmt.Errorf("%w: %w", errRollbackPrerequisite, err)
		}
		return fmt.Errorf("restoring the user database snapshot: %w", err)
	}
	return nil
}

// restoreReplacedFiles renames what the install displaced back over what it
// installed. The binary goes last so that a failure part way through leaves the
// new one in place, which is the state blockRollback is willing to run in.
func restoreReplacedFiles(dir string, m *pendingMarker, fileOps watchdogFileOps) error {
	for _, p := range m.PayloadBackups {
		if err := restoreReplacedFile(dir, m, p.BackupPath, p.TargetPath, fileOps); err != nil {
			return err
		}
	}
	// The binary is the one file the rollback may have to put back underneath a
	// process that is running from it, so it does not go through the plain
	// replacement the payload extras use.
	binaryOps := fileOps
	binaryOps.replace = fileOps.binary.replace
	return restoreReplacedFile(dir, m, m.BackupPath, m.TargetPath, binaryOps)
}

// restoreReplacedFile records the path about to move before renaming it. If a
// power cut lands after the rename but before the marker can advance, a missing
// backup is then proof that this exact move completed, not a guess based on the
// rollback's coarse state.
func restoreReplacedFile(
	dir string, m *pendingMarker, backupPath, targetPath string, fileOps watchdogFileOps,
) error {
	if backupPath == "" || targetPath == "" {
		return fmt.Errorf("%w: the update recorded no backup for a file it replaced", errRollbackPrerequisite)
	}

	_, statErr := fileOps.fs.Stat(backupPath)
	if statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) && m.RestoringPath == targetPath {
			if _, targetErr := fileOps.fs.Stat(targetPath); targetErr != nil {
				return fmt.Errorf("restored backup and target are both unavailable for %q: %w", targetPath, targetErr)
			}
			m.RestoringPath = ""
			if saveErr := saveMarker(dir, m); saveErr != nil {
				return fmt.Errorf("recording the completed file restore: %w", saveErr)
			}
			return nil
		}
		if errors.Is(statErr, os.ErrNotExist) {
			return fmt.Errorf("%w: backup of %q: %w", errRollbackPrerequisite, targetPath, statErr)
		}
		return fmt.Errorf("reading the backup of %q: %w", targetPath, statErr)
	}

	if m.RestoringPath != targetPath {
		m.RestoringPath = targetPath
		if err := saveMarker(dir, m); err != nil {
			return fmt.Errorf("recording the file being restored: %w", err)
		}
	}

	if err := fileOps.replace(backupPath, targetPath); err != nil {
		return fmt.Errorf("restoring %q: %w", targetPath, err)
	}
	if err := fileOps.syncDirectory(filepath.Dir(targetPath)); err != nil {
		return fmt.Errorf("flushing restored file %q: %w", targetPath, err)
	}
	m.RestoringPath = ""
	if err := saveMarker(dir, m); err != nil {
		return fmt.Errorf("recording the completed file restore: %w", err)
	}
	return nil
}

func finalizeTerminalUpdate(
	ctx context.Context, dataDir string, m *pendingMarker, fileOps watchdogFileOps,
) error {
	if m == nil || (m.Outcome != outcomeSucceeded && m.Outcome != outcomeRolledBack) {
		return errors.New("finalizing an update needs a completed outcome")
	}
	dir := stateDirFor(dataDir)
	if err := recordMarkerResult(dir, m); err != nil {
		return err
	}

	if m.Outcome == outcomeSucceeded {
		if err := removeReplacedFiles(m, fileOps); err != nil {
			return err
		}
	}
	if m.StagingDir != "" {
		if err := fileOps.removeStaging(ctx, m.StagingDir); err != nil {
			log.Warn().Err(err).Str("dir", m.StagingDir).
				Msg("could not remove the staging directory of a completed update")
		}
	}
	// After a confirmed update the process running from the superseded binary
	// has long exited, so this is where a swap that had to move it aside gets to
	// delete it. After a rollback it has not: the swap that just ran moved this
	// process's own image aside, and clearing that name is what the next
	// install's sweep is for. Both are why failing here is only worth a debug
	// line.
	fileOps.binary.sweep(m.TargetPath)
	sweepUpdateSnapshotsFS(fileOps.fs, dataDir, "")
	if err := clearMarker(dir); err != nil {
		return fmt.Errorf("clearing the completed update marker: %w", err)
	}
	return nil
}

func recordMarkerResult(dir string, m *pendingMarker) error {
	if m == nil || m.Outcome == "" {
		return nil
	}
	at := m.OutcomeAt
	if at.IsZero() {
		at = m.InstalledAt
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	return recordUpdateResult(dir, &updateResult{
		At:          at,
		Outcome:     m.Outcome,
		FromVersion: m.PreviousVersion,
		ToVersion:   m.TargetVersion,
		Detail:      m.OutcomeDetail,
	})
}

// removeReplacedFiles deletes the copies of what an update displaced, once that
// update is confirmed and they can no longer be needed.
func removeReplacedFiles(m *pendingMarker, fileOps watchdogFileOps) error {
	paths := make([]string, 0, len(m.PayloadBackups)+1)
	if m.BackupPath != "" {
		paths = append(paths, m.BackupPath)
	}
	for _, p := range m.PayloadBackups {
		if p.BackupPath != "" {
			paths = append(paths, p.BackupPath)
		}
	}

	var cleanupErr error
	dirs := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		if err := fileOps.fs.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			cleanupErr = errors.Join(cleanupErr,
				fmt.Errorf("removing backup of replaced file %q: %w", path, err))
			continue
		}
		dirs[filepath.Dir(path)] = struct{}{}
	}
	for dir := range dirs {
		if err := fileOps.syncDirectory(dir); err != nil {
			cleanupErr = errors.Join(cleanupErr,
				fmt.Errorf("flushing removed binary backups in %q: %w", dir, err))
		}
	}
	if cleanupErr != nil {
		return cleanupErr
	}
	return nil
}

// snapshotToKeep returns the snapshot a marker still depends on.
//
// A marker on disk always depends on its snapshot. The two outcomes that finish
// an update delete their marker, so the only one that survives to be read again
// is an abandoned rollback — and that is precisely the case where the snapshot
// is the user's remaining way back, which the inbox message tells them to use.
func snapshotToKeep(m *pendingMarker) string {
	if m == nil {
		return ""
	}
	return m.UserDBSnapshotPath
}

// sweepUpdateSnapshots deletes update snapshots no marker depends on any more.
// Retention deliberately ignores them, so without this a device that updated a
// dozen times would keep a dozen copies of its database forever.
func sweepUpdateSnapshots(dataDir, keep string) {
	sweepUpdateSnapshotsFS(afero.NewOsFs(), dataDir, keep)
}

func sweepUpdateSnapshotsFS(fs afero.Fs, dataDir, keep string) {
	dir := userdb.BackupsDir(dataDir)
	entries, err := afero.ReadDir(fs, dir)
	if err != nil {
		// A device that has never taken a backup has no directory here.
		if !errors.Is(err, os.ErrNotExist) {
			log.Debug().Err(err).Str("dir", dir).Msg("could not list backups to sweep update snapshots")
		}
		return
	}

	keep = filepath.Clean(keep)
	for _, entry := range entries {
		if entry.IsDir() || !userdb.IsUpdateSnapshotName(entry.Name()) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if keep != "." && filepath.Clean(path) == keep {
			continue
		}
		if err := fs.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Warn().Err(err).Str("path", path).Msg("could not remove an unreferenced update snapshot")
		}
	}
}
