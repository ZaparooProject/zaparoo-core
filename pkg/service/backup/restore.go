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

package backup

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/rs/zerolog/log"
)

const (
	restoreJournalVersion     = 5
	restoreTransactionDir     = ".restore-transaction"
	restoreJournalPlanName    = "journal-plan.json"
	restoreJournalStateName   = "journal-state.log"
	restoreRollbackDir        = "rollback"
	restoreUserDBRollbackName = "user.db"

	restorePhasePrepared  = "prepared"
	restorePhaseApplying  = "applying"
	restorePhaseCommitted = "committed"

	restoreEntryPending = ""
	restoreEntryStarted = "started"
	restoreEntryApplied = "applied"

	restoreEventPhase         = "phase"
	restoreEventEntry         = "entry"
	restoreEventUserDBStarted = "userDbStarted"

	maxRestoreJournalEventBytes = 256
	maxRestoreJournalEvents     = 2*(maxArchiveEntries-1) + 3
	maxRestoreJournalStateBytes = maxRestoreJournalEventBytes * maxRestoreJournalEvents
)

var (
	ErrRestoreMediaActive      = errors.New("cannot restore backup while media is active")
	ErrRestoreLaunchInProgress = errors.New("cannot restore backup while media is launching")
	ErrRestoreRecoveryNeeded   = errors.New("backup restore rollback requires recovery")
	ErrRestoreJournalConflict  = errors.New("a pending backup restore transaction exists")
)

type restoreJournalEvent struct {
	Kind     string `json:"kind"`
	State    string `json:"state,omitempty"`
	Sequence int    `json:"sequence"`
	Index    int    `json:"index,omitempty"`
}

type restoreJournal struct {
	OperationID       string                   `json:"operationId"`
	Phase             string                   `json:"phase"`
	UserDBRollback    *restoreRollbackArtifact `json:"userDbRollback,omitempty"`
	UserDBFile        *FileRef                 `json:"userDbFile,omitempty"`
	Entries           []restoreJournalEntry    `json:"entries"`
	CreatedDirs       []restoreJournalDir      `json:"createdDirs,omitempty"`
	Version           int                      `json:"version"`
	Sequence          int                      `json:"sequence"`
	MaxLogicalSize    int64                    `json:"maxLogicalSize"`
	UserDBRestoreUsed bool                     `json:"userDbRestoreUsed,omitempty"`
	UserDBStarted     bool                     `json:"userDbStarted,omitempty"`
}

type restoreJournalEntry struct {
	Root           string      `json:"root"`
	Rel            string      `json:"rel"`
	PolicyPrefix   string      `json:"policyPrefix,omitempty"`
	RollbackPath   string      `json:"rollbackPath,omitempty"`
	RollbackSHA256 string      `json:"rollbackSha256,omitempty"`
	State          string      `json:"state,omitempty"`
	File           FileRef     `json:"file"`
	RollbackSize   int64       `json:"rollbackSize,omitempty"`
	Mode           os.FileMode `json:"mode,omitempty"`
	Existed        bool        `json:"existed"`
}

type restoreJournalDir struct {
	Root string `json:"root"`
	Rel  string `json:"rel"`
}

type restoreRollbackArtifact struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type restoreTarget struct {
	root         string
	rel          string
	policyPrefix string
}

type restoreRootPolicy struct {
	root   string
	prefix string
}

type restorePolicy struct {
	definition *platforms.BackupDefinition
	logical    string
	roots      []restoreRootPolicy
}

type restorePolicyMatch struct {
	policy      restoreRootPolicy
	targetRel   string
	categoryRel string
}

func (m *Manager) beginRestoreGate() (func(bool), error) {
	if m.restoreGate == nil {
		return func(bool) {}, nil
	}
	finish, err := m.restoreGate()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRestoreLaunchInProgress, err)
	}
	return finish, nil
}

func (m *Manager) requireRestoreIdle() error {
	if m.activeMedia != nil && m.activeMedia() != nil {
		return ErrRestoreMediaActive
	}
	return nil
}

func (m *Manager) preparePlatformRestore() (func(bool) error, error) {
	preparer, ok := m.pl.(platforms.BackupRestorePreparer)
	if !ok {
		return func(bool) error { return nil }, nil
	}
	finish, err := preparer.PrepareBackupRestore()
	if err != nil {
		return nil, fmt.Errorf("preparing platform profile data for restore: %w", err)
	}
	return finish, nil
}

func (m *Manager) RecoverRestore(ctx context.Context) error {
	lease, err := m.begin(ctx, OperationRecovery, OperationWrite)
	if err != nil {
		return err
	}
	defer lease.Release()
	return m.recoverRestoreLocked(lease.Context())
}

func (m *Manager) recoverRestoreLocked(ctx context.Context) error {
	dir := m.restoreTransactionPath()
	info, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("checking restore transaction: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("invalid restore transaction path: %s", dir)
	}
	journal, err := m.readRestoreJournal()
	if errors.Is(err, os.ErrNotExist) {
		// Mutation starts only after a complete prepared journal is durable.
		return m.removeRestoreTransaction(dir)
	}
	if err != nil {
		return err
	}
	if journal.Phase == restorePhaseCommitted || journal.Phase == restorePhasePrepared {
		return m.removeRestoreTransaction(dir)
	}
	if err = ctx.Err(); err != nil {
		return fmt.Errorf("recovering backup restore: %w", err)
	}
	if err = m.rollbackRestore(&journal); err != nil {
		return fmt.Errorf("%w: %w", ErrRestoreRecoveryNeeded, err)
	}
	return m.removeRestoreTransaction(dir)
}

func (m *Manager) applyRestoreTransaction(
	ctx context.Context,
	manifest *Manifest,
	openPayload func(FileRef) (io.ReadCloser, error),
) error {
	if err := m.recoverRestoreLocked(ctx); err != nil {
		return err
	}
	journal, err := m.prepareRestoreJournal(ctx, manifest)
	if err != nil {
		return err
	}
	if err = m.persistRestorePhase(&journal, restorePhaseApplying); err != nil {
		_ = m.removeRestoreTransaction(m.restoreTransactionPath())
		return err
	}

	applyErr := m.applyRestoreJournal(ctx, &journal, openPayload)
	if applyErr != nil {
		rollbackErr := m.rollbackRestore(&journal)
		if rollbackErr != nil {
			return errors.Join(
				applyErr,
				fmt.Errorf("%w: %w", ErrRestoreRecoveryNeeded, rollbackErr),
			)
		}
		if cleanupErr := m.removeRestoreTransaction(m.restoreTransactionPath()); cleanupErr != nil {
			return errors.Join(applyErr, cleanupErr)
		}
		return applyErr
	}

	if err = m.persistRestorePhase(&journal, restorePhaseCommitted); err != nil {
		rollbackErr := m.rollbackRestore(&journal)
		if rollbackErr != nil {
			return errors.Join(err, fmt.Errorf("%w: %w", ErrRestoreRecoveryNeeded, rollbackErr))
		}
		return errors.Join(err, m.removeRestoreTransaction(m.restoreTransactionPath()))
	}
	if err = m.removeRestoreTransaction(m.restoreTransactionPath()); err != nil {
		log.Warn().Err(err).Msg("committed restore cleanup deferred until restart")
	}
	return nil
}

func (m *Manager) prepareRestoreJournal(
	ctx context.Context, manifest *Manifest,
) (journal restoreJournal, err error) {
	dir := m.restoreTransactionPath()
	if _, statErr := os.Lstat(dir); statErr == nil {
		return restoreJournal{}, ErrRestoreJournalConflict
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return restoreJournal{}, fmt.Errorf("checking restore transaction directory: %w", statErr)
	}
	if err = os.MkdirAll(filepath.Dir(dir), 0o750); err != nil {
		return restoreJournal{}, fmt.Errorf("creating private restore directory: %w", err)
	}
	if err = os.Mkdir(dir, 0o700); err != nil {
		return restoreJournal{}, fmt.Errorf("creating restore transaction directory: %w", err)
	}
	if err = m.syncRestoreDirectory(filepath.Dir(dir)); err != nil {
		_ = os.RemoveAll(dir)
		return restoreJournal{}, fmt.Errorf("syncing restore transaction parent: %w", err)
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, m.removeRestoreTransaction(dir))
		}
	}()
	if err = os.Mkdir(filepath.Join(dir, restoreRollbackDir), 0o700); err != nil {
		return restoreJournal{}, fmt.Errorf("creating restore rollback directory: %w", err)
	}

	operationID, err := newRestoreOperationID()
	if err != nil {
		return restoreJournal{}, err
	}
	journal = restoreJournal{
		Version:        restoreJournalVersion,
		OperationID:    operationID,
		Phase:          restorePhasePrepared,
		MaxLogicalSize: m.cfg.BackupMaxSizeBytes(),
	}

	if hasUserDBRestore(manifest.Files) {
		if m.database == nil || m.database.UserDB == nil {
			return restoreJournal{}, errors.New("database is not available for restore")
		}
		backup, backupErr := m.database.UserDB.Backup("restore-rollback", false)
		if backupErr != nil {
			return restoreJournal{}, fmt.Errorf("creating private user database rollback: %w", backupErr)
		}
		if spaceErr := preflightRollbackSpace(dir, backup.Size); spaceErr != nil {
			return restoreJournal{}, spaceErr
		}
		artifact, artifactErr := writeRestoreRollbackArtifact(
			ctx,
			backup.Path,
			filepath.Join(dir, restoreRollbackDir, restoreUserDBRollbackName),
		)
		if artifactErr != nil {
			return restoreJournal{}, fmt.Errorf("copying private user database rollback: %w", artifactErr)
		}
		journal.UserDBRollback = artifact
		journal.UserDBRestoreUsed = true
	}

	seenTargets := make(map[string]struct{}, len(manifest.Files))
	seenDirs := make(map[string]struct{})
	targetBytes := make(map[string]int64)
	spacePaths := map[string]struct{}{dir: {}}
	var incomingBytes, rollbackBytes, userDBBytes int64
	for i := range manifest.Files {
		if err = ctx.Err(); err != nil {
			return restoreJournal{}, fmt.Errorf("preparing restore rollback: %w", err)
		}
		file := &manifest.Files[i]
		if file.Size < 0 || incomingBytes > math.MaxInt64-file.Size {
			return restoreJournal{}, errors.New("restore space requirement overflow")
		}
		incomingBytes += file.Size
		if file.Category == CategoryZaparoo && file.RestorePath == "user.db" {
			fileCopy := *file
			journal.UserDBFile = &fileCopy
			userDBBytes = file.Size
			continue
		}
		target, targetErr := m.resolveRestoreTarget(file)
		if targetErr != nil {
			return restoreJournal{}, targetErr
		}
		targetKey := target.root + "\x00" + target.rel
		if _, exists := seenTargets[targetKey]; exists {
			return restoreJournal{}, fmt.Errorf("duplicate resolved restore target: %s", file.RestorePath)
		}
		seenTargets[targetKey] = struct{}{}
		spacePaths[target.root] = struct{}{}
		entry := restoreJournalEntry{
			File: *file, Root: target.root, Rel: target.rel, PolicyPrefix: target.policyPrefix,
		}
		if entryErr := inspectRollbackEntry(&entry, len(journal.Entries)); entryErr != nil {
			return restoreJournal{}, entryErr
		}
		if entry.RollbackSize > m.cfg.BackupMaxSizeBytes()-rollbackBytes {
			return restoreJournal{}, errors.New("restore rollback exceeds backup size limit")
		}
		rollbackBytes += entry.RollbackSize
		journal.Entries = append(journal.Entries, entry)
		if file.Size > m.cfg.BackupMaxSizeBytes()-targetBytes[target.root] {
			return restoreJournal{}, errors.New("restore target data exceeds backup size limit")
		}
		targetBytes[target.root] += file.Size

		for _, created := range missingTargetDirs(target) {
			key := created.Root + "\x00" + created.Rel
			if _, exists := seenDirs[key]; exists {
				continue
			}
			seenDirs[key] = struct{}{}
			journal.CreatedDirs = append(journal.CreatedDirs, created)
		}
	}
	requiredSpace, spaceErr := conservativeRestoreSpaceRequirement(
		incomingBytes, rollbackBytes, userDBBytes,
	)
	if spaceErr != nil {
		return restoreJournal{}, spaceErr
	}
	journalSpace, journalSpaceErr := restoreJournalStorageRequirement(&journal)
	if journalSpaceErr != nil {
		return restoreJournal{}, journalSpaceErr
	}
	if journalSpace > math.MaxInt64-requiredSpace {
		return restoreJournal{}, errors.New("restore space requirement overflow")
	}
	requiredSpace += journalSpace
	if preflightErr := preflightConservativeRestoreSpace(
		spacePaths, requiredSpace, helpers.FreeDiskSpace,
	); preflightErr != nil {
		return restoreJournal{}, preflightErr
	}
	for i := range journal.Entries {
		if writeErr := m.writeRollbackEntry(ctx, &journal.Entries[i]); writeErr != nil {
			return restoreJournal{}, writeErr
		}
	}
	if syncErr := m.syncRestoreDirectory(filepath.Join(dir, restoreRollbackDir)); syncErr != nil {
		return restoreJournal{}, fmt.Errorf("syncing restore rollback directory: %w", syncErr)
	}
	if writeErr := m.writeRestoreJournal(&journal); writeErr != nil {
		return restoreJournal{}, writeErr
	}
	return journal, nil
}

func inspectRollbackEntry(entry *restoreJournalEntry, index int) error {
	root, err := os.OpenRoot(entry.Root)
	if err != nil {
		return fmt.Errorf("opening restore target root: %w", err)
	}
	defer func() { _ = root.Close() }()
	info, err := root.Stat(entry.Rel)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stating restore target %s: %w", entry.File.RestorePath, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("restore target is not a regular file: %s", entry.File.RestorePath)
	}
	entry.Existed = true
	entry.Mode = info.Mode().Perm()
	entry.RollbackPath = filepath.Join(restoreRollbackDir, fmt.Sprintf("%06d", index))
	entry.RollbackSize = info.Size()
	return nil
}

func (m *Manager) writeRollbackEntry(ctx context.Context, entry *restoreJournalEntry) error {
	if !entry.Existed {
		return nil
	}
	root, err := os.OpenRoot(entry.Root)
	if err != nil {
		return fmt.Errorf("opening restore target root: %w", err)
	}
	defer func() { _ = root.Close() }()
	source, err := root.Open(entry.Rel)
	if err != nil {
		return fmt.Errorf("opening restore rollback source %s: %w", entry.File.RestorePath, err)
	}
	defer func() { _ = source.Close() }()
	rollbackPath := filepath.Join(m.restoreTransactionPath(), entry.RollbackPath)
	// #nosec G304 -- path is generated inside the private restore transaction directory.
	rollback, err := os.OpenFile(rollbackPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("creating restore rollback artifact: %w", err)
	}
	hash := sha256.New()
	written, copyErr := io.Copy(
		io.MultiWriter(rollback, hash), &contextReader{ctx: ctx, reader: source},
	)
	syncErr := rollback.Sync()
	closeErr := rollback.Close()
	if copyErr != nil {
		return fmt.Errorf("copying restore rollback artifact: %w", copyErr)
	}
	if syncErr != nil {
		return fmt.Errorf("syncing restore rollback artifact: %w", syncErr)
	}
	if closeErr != nil {
		return fmt.Errorf("closing restore rollback artifact: %w", closeErr)
	}
	if written != entry.RollbackSize {
		return fmt.Errorf("restore target changed while preparing rollback: %s", entry.File.RestorePath)
	}
	entry.RollbackSHA256 = hex.EncodeToString(hash.Sum(nil))
	return nil
}

func writeRestoreRollbackArtifact(
	ctx context.Context, sourcePath, destinationPath string,
) (*restoreRollbackArtifact, error) {
	// #nosec G304 -- sourcePath is returned by private UserDB backup creation.
	source, err := os.Open(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("opening rollback source: %w", err)
	}
	defer func() { _ = source.Close() }()
	info, err := source.Stat()
	if err != nil {
		return nil, fmt.Errorf("stating rollback source: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("rollback source is not a regular file")
	}
	// #nosec G304 -- destinationPath is fixed inside the private transaction directory.
	destination, err := os.OpenFile(destinationPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("creating rollback artifact: %w", err)
	}
	hash := sha256.New()
	written, copyErr := io.Copy(
		io.MultiWriter(destination, hash),
		&contextReader{ctx: ctx, reader: source},
	)
	syncErr := destination.Sync()
	closeErr := destination.Close()
	if copyErr != nil {
		return nil, fmt.Errorf("copying rollback artifact: %w", copyErr)
	}
	if syncErr != nil {
		return nil, fmt.Errorf("syncing rollback artifact: %w", syncErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("closing rollback artifact: %w", closeErr)
	}
	if written != info.Size() {
		return nil, errors.New("user database rollback changed while copying")
	}
	return &restoreRollbackArtifact{
		Path:   filepath.Join(restoreRollbackDir, restoreUserDBRollbackName),
		SHA256: hex.EncodeToString(hash.Sum(nil)),
		Size:   written,
	}, nil
}

func (m *Manager) applyRestoreJournal(
	ctx context.Context,
	journal *restoreJournal,
	openPayload func(FileRef) (io.ReadCloser, error),
) error {
	for i := range journal.Entries {
		entry := &journal.Entries[i]
		target, err := m.validateJournalTarget(entry)
		if err != nil {
			return err
		}
		payload, err := openPayload(entry.File)
		if err != nil {
			return err
		}
		if writeErr := m.persistRestoreEntryState(journal, i, restoreEntryStarted); writeErr != nil {
			_ = payload.Close()
			return writeErr
		}
		mode := os.FileMode(0o600)
		if entry.Existed {
			mode = entry.Mode
		}
		if installErr := installPayloadAtTarget(
			ctx, target, &entry.File, payload, mode, m.syncRestoreDirectory, journal.OperationID, i,
		); installErr != nil {
			return installErr
		}
		if writeErr := m.persistRestoreEntryState(journal, i, restoreEntryApplied); writeErr != nil {
			return writeErr
		}
	}

	if !journal.UserDBRestoreUsed {
		return nil
	}
	if err := m.persistRestoreUserDBStarted(journal); err != nil {
		return err
	}
	if journal.UserDBFile == nil {
		return errors.New("restore journal is missing user database payload metadata")
	}
	return m.restoreUserDBPayload(ctx, journal.UserDBFile, openPayload)
}

func (m *Manager) restoreUserDBPayload(
	ctx context.Context,
	file *FileRef,
	openPayload func(FileRef) (io.ReadCloser, error),
) error {
	// Portable snapshots are created without paired-client rows, so a plain
	// swap would unpair every client. Paired clients are destination device
	// state, not backup content: carry the live rows across the restore.
	clients, err := m.database.UserDB.ListClients()
	if err != nil {
		return fmt.Errorf("snapshotting paired clients before restore: %w", err)
	}
	payload, err := openPayload(*file)
	if err != nil {
		return err
	}
	name := databaseBackupName(time.Now().UTC())
	backupDir := filepath.Join(filepath.Dir(m.database.UserDB.GetDBPath()), "backups")
	if err = os.MkdirAll(backupDir, 0o750); err != nil {
		_ = payload.Close()
		return fmt.Errorf("creating user database restore staging directory: %w", err)
	}
	staged := filepath.Join(backupDir, name)
	if err = installVerifiedPayload(ctx, staged, file, payload); err != nil {
		return fmt.Errorf("staging user database restore: %w", err)
	}
	defer func() { _ = os.Remove(staged) }()
	if _, err = m.database.UserDB.RestoreBackup(name); err != nil {
		return fmt.Errorf("restoring user database: %w", err)
	}
	// Runs after MigrateUp inside RestoreBackup, so the insert always
	// targets the current schema even when the backup is from an older
	// Core. Failure fails the restore, which rolls back to the full
	// pre-restore copy (clients included).
	if err = m.database.UserDB.ReplaceAllClients(clients); err != nil {
		return fmt.Errorf("preserving paired clients after restore: %w", err)
	}
	return nil
}

func (m *Manager) restoreUserDBRollback(artifact *restoreRollbackArtifact) (err error) {
	if m.database == nil || m.database.UserDB == nil {
		return errors.New("database is not available for user database rollback")
	}
	artifactPath := filepath.Join(m.restoreTransactionPath(), artifact.Path)
	// #nosec G304 -- artifact path is fixed and validated inside the private transaction directory.
	payload, err := os.Open(artifactPath)
	if err != nil {
		return fmt.Errorf("opening user database rollback artifact: %w", err)
	}
	file := &FileRef{
		RestorePath: "user.db",
		SHA256:      artifact.SHA256,
		Size:        artifact.Size,
	}
	backupDir := filepath.Join(filepath.Dir(m.database.UserDB.GetDBPath()), "backups")
	if err = os.MkdirAll(backupDir, 0o750); err != nil {
		_ = payload.Close()
		return fmt.Errorf("creating user database rollback staging directory: %w", err)
	}
	name := databaseBackupName(time.Now().UTC())
	staged := filepath.Join(backupDir, name)
	defer func() {
		removeErr := os.Remove(staged)
		if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			err = errors.Join(err, fmt.Errorf("removing staged user database rollback: %w", removeErr))
			return
		}
		if syncErr := m.syncRestoreDirectory(backupDir); syncErr != nil {
			err = errors.Join(err, fmt.Errorf("syncing user database rollback cleanup: %w", syncErr))
		}
	}()
	if err = installVerifiedPayload(context.Background(), staged, file, payload); err != nil {
		return fmt.Errorf("staging user database rollback: %w", err)
	}
	if err = m.syncRestoreDirectory(backupDir); err != nil {
		return fmt.Errorf("syncing staged user database rollback: %w", err)
	}
	if _, err = m.database.UserDB.RestoreBackup(name); err != nil {
		return fmt.Errorf("restoring user database rollback: %w", err)
	}
	return nil
}

func (m *Manager) rollbackRestore(journal *restoreJournal) error {
	var rollbackErr error
	if journal.UserDBStarted && journal.UserDBRollback != nil {
		if err := m.restoreUserDBRollback(journal.UserDBRollback); err != nil {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("rolling back user database: %w", err))
		}
	}
	for i := len(journal.Entries) - 1; i >= 0; i-- {
		entry := &journal.Entries[i]
		if entry.State == restoreEntryPending {
			continue
		}
		target, err := m.validateJournalTarget(entry)
		if err != nil {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("validating rollback target: %w", err))
			continue
		}
		if err = m.removeRestoreStagedTemp(target, journal.OperationID, i); err != nil {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("removing staged restore remnant: %w", err))
			continue
		}
		digest, exists, digestErr := readRestoreTargetDigest(target)
		if digestErr != nil {
			rollbackErr = errors.Join(rollbackErr, digestErr)
			continue
		}
		if !entry.Existed {
			switch {
			case !exists:
				continue
			case !restoreDigestMatches(digest, &entry.File):
				rollbackErr = errors.Join(rollbackErr, fmt.Errorf(
					"restore rollback found unexpected content at %s", entry.File.RestorePath,
				))
				continue
			}
			if removeErr := m.removeRestoredTarget(target); removeErr != nil {
				rollbackErr = errors.Join(rollbackErr, fmt.Errorf(
					"removing restored file %s: %w", entry.File.RestorePath, removeErr,
				))
			}
			continue
		}

		rollbackFile := FileRef{
			RestorePath: entry.File.RestorePath,
			SHA256:      entry.RollbackSHA256,
			Size:        entry.RollbackSize,
		}
		switch {
		case !exists:
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf(
				"restore rollback target disappeared: %s", entry.File.RestorePath,
			))
			continue
		case restoreDigestMatches(digest, &rollbackFile):
			continue
		case !restoreDigestMatches(digest, &entry.File):
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf(
				"restore rollback found unexpected content at %s", entry.File.RestorePath,
			))
			continue
		}
		rollbackPath := filepath.Join(m.restoreTransactionPath(), entry.RollbackPath)
		// #nosec G304 -- rollback path comes from a validated private journal.
		payload, openErr := os.Open(rollbackPath)
		if openErr != nil {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("opening rollback artifact: %w", openErr))
			continue
		}
		installErr := installPayloadAtTarget(
			context.Background(), target, &rollbackFile, payload, entry.Mode,
			m.syncRestoreDirectory, journal.OperationID, i,
		)
		if installErr != nil {
			rollbackErr = errors.Join(rollbackErr, installErr)
		}
	}
	return rollbackErr
}

type restoreTargetDigest struct {
	sha256 string
	size   int64
}

func readRestoreTargetDigest(target restoreTarget) (digest restoreTargetDigest, exists bool, err error) {
	root, err := os.OpenRoot(target.root)
	if err != nil {
		return digest, false, fmt.Errorf("opening rollback target root: %w", err)
	}
	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("closing rollback target root: %w", closeErr))
		}
	}()
	file, err := root.Open(target.rel)
	if errors.Is(err, os.ErrNotExist) {
		return digest, false, nil
	}
	if err != nil {
		return digest, false, fmt.Errorf("opening rollback target: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("closing rollback target: %w", closeErr))
		}
	}()
	info, err := file.Stat()
	if err != nil {
		return digest, false, fmt.Errorf("stating rollback target: %w", err)
	}
	if !info.Mode().IsRegular() {
		return digest, false, errors.New("restore rollback target is not a regular file")
	}
	hash := sha256.New()
	if _, err = io.Copy(hash, file); err != nil {
		return digest, false, fmt.Errorf("hashing rollback target: %w", err)
	}
	return restoreTargetDigest{sha256: hex.EncodeToString(hash.Sum(nil)), size: info.Size()}, true, nil
}

func restoreDigestMatches(digest restoreTargetDigest, file *FileRef) bool {
	return digest.size == file.Size && digest.sha256 == file.SHA256
}

func (m *Manager) removeRestoredTarget(target restoreTarget) error {
	root, err := os.OpenRoot(target.root)
	if err != nil {
		return fmt.Errorf("opening rollback root: %w", err)
	}
	removeErr := root.Remove(target.rel)
	closeErr := root.Close()
	if removeErr != nil {
		return fmt.Errorf("removing restored target: %w", removeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("closing rollback root: %w", closeErr)
	}
	parentPath := target.root
	if parent := filepath.Dir(target.rel); parent != "." {
		parentPath = filepath.Join(target.root, parent)
	}
	if err = m.syncRestoreDirectory(parentPath); err != nil {
		return fmt.Errorf("syncing restored target removal: %w", err)
	}
	return nil
}

func installPayloadAtTarget(
	ctx context.Context,
	target restoreTarget,
	file *FileRef,
	payload io.ReadCloser,
	mode os.FileMode,
	syncDirectory func(string) error,
	operationID string,
	index int,
) (err error) {
	defer func() {
		if closeErr := payload.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	root, err := os.OpenRoot(target.root)
	if err != nil {
		return fmt.Errorf("opening restore root: %w", err)
	}
	defer func() { _ = root.Close() }()
	parent := filepath.Dir(target.rel)
	if parent != "." {
		if err = root.MkdirAll(parent, 0o750); err != nil {
			return fmt.Errorf("creating restore directory for %s: %w", file.RestorePath, err)
		}
		if err = syncRestoreTargetDirectoryChain(syncDirectory, target.root, parent); err != nil {
			return fmt.Errorf("syncing restore directory for %s: %w", file.RestorePath, err)
		}
	}
	tmpRel := restoreTempRel(target, operationID, index)
	tmp, err := root.OpenFile(tmpRel, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("creating staged restore file for %s: %w", file.RestorePath, err)
	}
	defer func() { _ = root.Remove(tmpRel) }()
	hash := sha256.New()
	limited := &io.LimitedReader{R: &contextReader{ctx: ctx, reader: payload}, N: file.Size + 1}
	written, copyErr := io.Copy(io.MultiWriter(tmp, hash), limited)
	syncErr := tmp.Sync()
	closeErr := tmp.Close()
	if copyErr != nil {
		return fmt.Errorf("staging restore payload %s: %w", file.RestorePath, copyErr)
	}
	if syncErr != nil {
		return fmt.Errorf("syncing restore payload %s: %w", file.RestorePath, syncErr)
	}
	if closeErr != nil {
		return fmt.Errorf("closing restore payload %s: %w", file.RestorePath, closeErr)
	}
	if written != file.Size {
		return fmt.Errorf("restore payload size mismatch: %s", file.RestorePath)
	}
	if hex.EncodeToString(hash.Sum(nil)) != file.SHA256 {
		return fmt.Errorf("restore payload hash mismatch: %s", file.RestorePath)
	}
	if err = root.Chmod(tmpRel, mode.Perm()); err != nil {
		return fmt.Errorf("setting restore payload mode %s: %w", file.RestorePath, err)
	}
	if err = root.Rename(tmpRel, target.rel); err != nil {
		return fmt.Errorf("installing restore payload %s: %w", file.RestorePath, err)
	}
	parentPath := target.root
	if parent != "." {
		parentPath = filepath.Join(target.root, parent)
	}
	if err = syncDirectory(parentPath); err != nil {
		return fmt.Errorf("syncing restored payload directory %s: %w", file.RestorePath, err)
	}
	return nil
}

func restoreTempRel(target restoreTarget, operationID string, index int) string {
	return filepath.Join(
		filepath.Dir(target.rel),
		fmt.Sprintf(".restore-%s-%06d", operationID, index),
	)
}

func (m *Manager) removeRestoreStagedTemp(target restoreTarget, operationID string, index int) error {
	root, err := os.OpenRoot(target.root)
	if err != nil {
		return fmt.Errorf("opening restore root: %w", err)
	}
	tmpRel := restoreTempRel(target, operationID, index)
	removeErr := root.Remove(tmpRel)
	removed := removeErr == nil
	closeErr := root.Close()
	if errors.Is(removeErr, os.ErrNotExist) {
		removeErr = nil
	}
	if removeErr != nil {
		return fmt.Errorf("removing restore temp file: %w", removeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("closing restore root: %w", closeErr)
	}
	if !removed {
		return nil
	}
	parentPath := target.root
	if parent := filepath.Dir(target.rel); parent != "." {
		parentPath = filepath.Join(target.root, parent)
	}
	if err = m.syncRestoreDirectory(parentPath); err != nil {
		return fmt.Errorf("syncing restore temp removal: %w", err)
	}
	return nil
}

func syncRestoreTargetDirectoryChain(syncDirectory func(string) error, root, parent string) error {
	current := root
	if err := syncDirectory(current); err != nil {
		return err
	}
	for _, part := range strings.Split(filepath.ToSlash(parent), "/") {
		current = filepath.Join(current, part)
		if err := syncDirectory(current); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) resolveRestoreTarget(file *FileRef) (restoreTarget, error) {
	policy, err := m.restoreTargetPolicy(file)
	if err != nil {
		return restoreTarget{}, err
	}
	physicalPath, err := resolvePhysicalRestorePath(policy.logical)
	if err != nil {
		return restoreTarget{}, fmt.Errorf("resolving restore target %s: %w", file.RestorePath, err)
	}
	if m.isSensitiveRestoreTarget(physicalPath) {
		return restoreTarget{}, fmt.Errorf("restore target is sensitive: %s", file.RestorePath)
	}
	match, ok := matchRestorePolicy(physicalPath, policy.roots)
	if !ok {
		return restoreTarget{}, fmt.Errorf("restore target escapes trusted roots: %s", file.RestorePath)
	}
	if policy.definition == nil {
		if filepath.Clean(match.categoryRel) != filepath.Clean(filepath.FromSlash(file.RestorePath)) {
			return restoreTarget{}, fmt.Errorf("zaparoo restore symlink target is not allowed: %s", file.RestorePath)
		}
	} else if !definitionIncludes(policy.definition, match.categoryRel) {
		return restoreTarget{}, fmt.Errorf("restore symlink target violates category policy: %s", file.RestorePath)
	}
	return restoreTarget{
		root: match.policy.root, rel: match.targetRel, policyPrefix: match.policy.prefix,
	}, nil
}

func (m *Manager) restoreTargetPolicy(file *FileRef) (restorePolicy, error) {
	if file.Category == CategoryZaparoo {
		root := helpers.DataDir(m.pl)
		if file.RestorePath == config.CfgFile {
			root = helpers.ConfigDir(m.pl)
		}
		policy := restorePolicy{
			logical: filepath.Join(root, filepath.FromSlash(file.RestorePath)),
			roots:   buildRestoreRootPolicies([]string{root}),
		}
		if len(policy.roots) == 0 {
			return restorePolicy{}, fmt.Errorf("zaparoo restore root is unavailable: %s", root)
		}
		return policy, nil
	}
	provider, ok := m.pl.(platforms.BackupProvider)
	if !ok {
		return restorePolicy{}, errors.New("platform does not support backup restore")
	}
	definitions := provider.BackupDefinitions()
	for i := range definitions {
		def := &definitions[i]
		if def.Category != file.Category || !allowedRestorePath(file, []platforms.BackupDefinition{*def}) {
			continue
		}
		policy := restorePolicy{
			definition: def,
			logical: filepath.Join(
				m.platformRestoreRoot(helpers.DataDir(m.pl)), filepath.FromSlash(file.RestorePath),
			),
			roots: buildRestoreRootPolicies(m.definitionRestoreRootCandidates(def)),
		}
		if len(policy.roots) == 0 {
			return restorePolicy{}, fmt.Errorf("restore roots are unavailable for %s", file.RestorePath)
		}
		return policy, nil
	}
	return restorePolicy{}, fmt.Errorf("restore path is outside collector policy: %s", file.RestorePath)
}

func (m *Manager) definitionRestoreRootCandidates(def *platforms.BackupDefinition) []string {
	return definitionCategoryRoots(def, m.pl.RootDirs(m.cfg))
}

func (m *Manager) isSensitiveRestoreTarget(target string) bool {
	for _, root := range canonicalCategoryRoots([]string{helpers.ConfigDir(m.pl)}, "") {
		if filepath.Clean(target) == filepath.Join(root, config.AuthFile) {
			return true
		}
	}
	return false
}

func buildRestoreRootPolicies(candidates []string) []restoreRootPolicy {
	seen := make(map[string]struct{}, len(candidates))
	var policies []restoreRootPolicy
	for _, candidate := range candidates {
		for _, policy := range restoreRootPolicyChain(candidate) {
			key := policy.root + "\x00" + policy.prefix
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			policies = append(policies, policy)
		}
	}
	sort.Slice(policies, func(i, j int) bool {
		left := len(filepath.Join(policies[i].root, policies[i].prefix))
		right := len(filepath.Join(policies[j].root, policies[j].prefix))
		if left != right {
			return left > right
		}
		return len(policies[i].root) > len(policies[j].root)
	})
	return policies
}

func restoreRootPolicyChain(candidate string) []restoreRootPolicy {
	candidate, err := filepath.Abs(candidate)
	if err != nil {
		return nil
	}
	current := filepath.Clean(candidate)
	categoryRoot := true
	var prefixParts []string
	var policies []restoreRootPolicy
	for {
		info, statErr := os.Lstat(current)
		if statErr == nil {
			resolved := current
			if info.Mode()&os.ModeSymlink != 0 {
				if categoryRoot {
					// The category target must match another independently approved
					// policy; resolving it here would make any target trusted.
					statErr = os.ErrInvalid
				} else {
					resolved, statErr = filepath.EvalSymlinks(current)
					if statErr == nil {
						info, statErr = os.Stat(resolved)
					}
				}
			}
			if statErr == nil && info.IsDir() {
				if absolute, absErr := filepath.Abs(resolved); absErr == nil {
					policies = append(policies, restoreRootPolicy{
						root: filepath.Clean(absolute), prefix: filepath.Join(prefixParts...),
					})
				}
			}
		} else if !errors.Is(statErr, os.ErrNotExist) && !errors.Is(statErr, os.ErrInvalid) {
			return policies
		}
		parent := filepath.Dir(current)
		if parent == current {
			return policies
		}
		prefixParts = append([]string{filepath.Base(current)}, prefixParts...)
		current = parent
		categoryRoot = false
	}
}

func matchRestorePolicy(
	target string, policies []restoreRootPolicy,
) (restorePolicyMatch, bool) {
	return matchRestorePolicyForRoot(target, policies, "")
}

func matchRestorePolicyForRoot(
	target string, policies []restoreRootPolicy, requiredRoot string,
) (restorePolicyMatch, bool) {
	for _, policy := range policies {
		if requiredRoot != "" && policy.root != requiredRoot {
			continue
		}
		categoryRoot := filepath.Join(policy.root, policy.prefix)
		categoryRel, err := filepath.Rel(categoryRoot, target)
		if err != nil || categoryRel == ".." || strings.HasPrefix(categoryRel, ".."+string(os.PathSeparator)) {
			continue
		}
		targetRel, err := filepath.Rel(policy.root, target)
		if err != nil {
			continue
		}
		return restorePolicyMatch{
			policy: policy, targetRel: targetRel, categoryRel: categoryRel,
		}, true
	}
	return restorePolicyMatch{}, false
}

func resolvePhysicalRestorePath(logicalPath string) (string, error) {
	logicalPath, err := filepath.Abs(logicalPath)
	if err != nil {
		return "", fmt.Errorf("resolving absolute restore path: %w", err)
	}
	logicalPath = filepath.Clean(logicalPath)
	resolved, err := filepath.EvalSymlinks(logicalPath)
	if err == nil {
		absolute, absErr := filepath.Abs(resolved)
		if absErr != nil {
			return "", fmt.Errorf("resolving symlink target path: %w", absErr)
		}
		return absolute, nil
	}
	if info, lstatErr := os.Lstat(logicalPath); lstatErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("restore target is a broken symlink")
	} else if lstatErr != nil && !errors.Is(lstatErr, os.ErrNotExist) {
		return "", fmt.Errorf("checking restore target: %w", lstatErr)
	}
	parent, err := resolveRestoreParent(filepath.Dir(logicalPath))
	if err != nil {
		return "", fmt.Errorf("resolving restore target parent: %w", err)
	}
	return filepath.Join(parent, filepath.Base(logicalPath)), nil
}

func resolveRestoreParent(parent string) (string, error) {
	resolved, err := filepath.EvalSymlinks(parent)
	if err == nil {
		absolute, absErr := filepath.Abs(resolved)
		if absErr != nil {
			return "", fmt.Errorf("resolving restore parent path: %w", absErr)
		}
		return absolute, nil
	}
	info, lstatErr := os.Lstat(parent)
	if lstatErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("restore parent is a broken symlink")
	}
	if lstatErr != nil && !errors.Is(lstatErr, os.ErrNotExist) {
		return "", fmt.Errorf("checking restore parent: %w", lstatErr)
	}
	if filepath.Dir(parent) == parent {
		return "", errors.New("no existing restore path ancestor")
	}
	grandparent, err := resolveRestoreParent(filepath.Dir(parent))
	if err != nil {
		return "", fmt.Errorf("resolving ancestor restore parent: %w", err)
	}
	return filepath.Join(grandparent, filepath.Base(parent)), nil
}

func (m *Manager) validateJournalTarget(entry *restoreJournalEntry) (restoreTarget, error) {
	if err := validateRestorePath(filepath.ToSlash(entry.Rel)); err != nil {
		return restoreTarget{}, fmt.Errorf("invalid restore journal target: %w", err)
	}
	if entry.PolicyPrefix != "" {
		if err := validateRestorePath(filepath.ToSlash(entry.PolicyPrefix)); err != nil {
			return restoreTarget{}, fmt.Errorf("invalid restore journal policy prefix: %w", err)
		}
	}
	if !filepath.IsAbs(entry.Root) || filepath.Clean(entry.Root) != entry.Root {
		return restoreTarget{}, errors.New("restore journal root must be an absolute clean path")
	}
	rootInfo, err := os.Lstat(entry.Root)
	if err != nil {
		return restoreTarget{}, fmt.Errorf("checking restore journal root: %w", err)
	}
	if !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return restoreTarget{}, errors.New("restore journal root is not a physical directory")
	}

	targetPath := filepath.Join(entry.Root, entry.Rel)
	physicalPath, err := resolvePhysicalRestorePath(targetPath)
	if err != nil {
		return restoreTarget{}, fmt.Errorf("resolving restore journal target: %w", err)
	}
	policy := restoreRootPolicy{root: entry.Root, prefix: entry.PolicyPrefix}
	match, ok := matchRestorePolicyForRoot(physicalPath, []restoreRootPolicy{policy}, entry.Root)
	if !ok || filepath.Clean(match.targetRel) != filepath.Clean(entry.Rel) {
		return restoreTarget{}, fmt.Errorf(
			"restore journal target escapes persisted policy: %s", entry.File.RestorePath,
		)
	}
	if m.isSensitiveRestoreTarget(physicalPath) {
		return restoreTarget{}, fmt.Errorf("restore journal target is sensitive: %s", entry.File.RestorePath)
	}
	if entry.File.Category == CategoryZaparoo {
		if (entry.File.RestorePath != config.UserDbFile &&
			!allowedRestorePath(&entry.File, m.zaparooRestoreDefinitions())) ||
			filepath.Clean(match.categoryRel) != filepath.Clean(filepath.FromSlash(entry.File.RestorePath)) {
			return restoreTarget{}, fmt.Errorf("invalid zaparoo restore journal target: %s", entry.File.RestorePath)
		}
	} else if !m.journalDefinitionIncludes(&entry.File, match.categoryRel) {
		return restoreTarget{}, fmt.Errorf(
			"restore journal target violates category policy: %s", entry.File.RestorePath,
		)
	}
	return restoreTarget{
		root: entry.Root, rel: entry.Rel, policyPrefix: entry.PolicyPrefix,
	}, nil
}

func (m *Manager) journalDefinitionIncludes(file *FileRef, categoryRel string) bool {
	provider, ok := m.pl.(platforms.BackupProvider)
	if !ok {
		return false
	}
	definitions := provider.BackupDefinitions()
	for i := range definitions {
		definition := &definitions[i]
		if definition.Category == file.Category &&
			allowedRestorePath(file, []platforms.BackupDefinition{*definition}) &&
			definitionIncludes(definition, categoryRel) {
			return true
		}
	}
	return false
}

func missingTargetDirs(target restoreTarget) []restoreJournalDir {
	root, err := os.OpenRoot(target.root)
	if err != nil {
		return nil
	}
	defer func() { _ = root.Close() }()
	var missing []restoreJournalDir
	parent := filepath.Dir(target.rel)
	for parent != "." && parent != "" {
		info, statErr := root.Stat(parent)
		if statErr == nil {
			if !info.IsDir() {
				return nil
			}
			break
		}
		if !errors.Is(statErr, os.ErrNotExist) {
			return nil
		}
		missing = append(missing, restoreJournalDir{Root: target.root, Rel: parent})
		parent = filepath.Dir(parent)
	}
	slices.Reverse(missing)
	return missing
}

func conservativeRestoreSpaceRequirement(
	incomingBytes, rollbackBytes, userDBBytes int64,
) (int64, error) {
	if incomingBytes < 0 || rollbackBytes < 0 || userDBBytes < 0 {
		return 0, errors.New("restore space requirement contains a negative size")
	}
	if incomingBytes > math.MaxInt64-rollbackBytes {
		return 0, errors.New("restore space requirement overflow")
	}
	required := incomingBytes + rollbackBytes
	// UserDB restore later needs a manual staging copy and an internal
	// pre-restore/atomic replacement reserve in addition to its incoming copy.
	if userDBBytes > (math.MaxInt64-required)/2 {
		return 0, errors.New("restore space requirement overflow")
	}
	return required + (2 * userDBBytes), nil
}

func preflightConservativeRestoreSpace(
	paths map[string]struct{},
	required int64,
	freeSpace func(string) (uint64, error),
) error {
	if required < 0 {
		return errors.New("restore space requirement contains a negative size")
	}
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	for _, path := range ordered {
		free, err := freeSpace(path)
		if err != nil {
			return fmt.Errorf("checking conservative restore disk space under %s: %w", path, err)
		}
		if uint64(required) > free {
			return fmt.Errorf(
				"insufficient disk space for conservative restore preflight under %s: have %d bytes, need %d",
				path, free, required,
			)
		}
	}
	return nil
}

func restoreJournalStorageRequirement(journal *restoreJournal) (int64, error) {
	plan := *journal
	plan.Sequence = 1
	plan.Entries = slices.Clone(journal.Entries)
	for i := range plan.Entries {
		if plan.Entries[i].Existed && plan.Entries[i].RollbackSHA256 == "" {
			plan.Entries[i].RollbackSHA256 = strings.Repeat("0", sha256.Size*2)
		}
	}
	data, err := json.MarshalIndent(&plan, "", "  ")
	if err != nil {
		return 0, fmt.Errorf("encoding restore journal space estimate: %w", err)
	}
	events := 2*len(plan.Entries) + 2
	if plan.UserDBRestoreUsed {
		events++
	}
	if events > maxRestoreJournalEvents {
		return 0, errors.New("restore journal has too many events")
	}
	stateBytes := int64(events * maxRestoreJournalEventBytes)
	if int64(len(data)) > math.MaxInt64-stateBytes {
		return 0, errors.New("restore journal space requirement overflow")
	}
	return int64(len(data)) + stateBytes, nil
}

func preflightRollbackSpace(transactionDir string, size int64) error {
	free, err := helpers.FreeDiskSpace(transactionDir)
	if err != nil {
		return fmt.Errorf("checking restore rollback disk space: %w", err)
	}
	if size < 0 || uint64(size) > free {
		return fmt.Errorf("insufficient disk space for %d bytes of restore rollback", size)
	}
	return nil
}

func (m *Manager) writeRestoreJournal(journal *restoreJournal) error {
	if journal.Sequence != 0 {
		return errors.New("restore journal plan is already persisted")
	}
	plan := *journal
	plan.Sequence = 1
	data, err := json.MarshalIndent(&plan, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding restore journal plan: %w", err)
	}
	dir := m.restoreTransactionPath()
	planPath := filepath.Join(dir, restoreJournalPlanName)
	if _, err = os.Lstat(planPath); err == nil {
		return errors.New("restore journal plan already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("checking restore journal plan: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".journal-plan-*")
	if err != nil {
		return fmt.Errorf("creating restore journal plan: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if err = tmp.Chmod(0o600); err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err != nil {
		return fmt.Errorf("writing restore journal plan: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("closing restore journal plan: %w", closeErr)
	}
	if err = os.Rename(tmpPath, planPath); err != nil {
		return fmt.Errorf("installing restore journal plan: %w", err)
	}
	if err = m.syncRestoreDirectory(dir); err != nil {
		return fmt.Errorf("syncing restore journal directory: %w", err)
	}
	journal.Sequence = plan.Sequence
	return nil
}

func (m *Manager) appendRestoreJournalEvent(journal *restoreJournal, event *restoreJournalEvent) error {
	event.Sequence = journal.Sequence + 1
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encoding restore journal event: %w", err)
	}
	data = append(data, '\n')
	if len(data) > maxRestoreJournalEventBytes {
		return errors.New("restore journal event exceeds size limit")
	}
	dir := m.restoreTransactionPath()
	statePath := filepath.Join(dir, restoreJournalStateName)
	_, statErr := os.Lstat(statePath)
	created := errors.Is(statErr, os.ErrNotExist)
	if statErr != nil && !created {
		return fmt.Errorf("checking restore journal state: %w", statErr)
	}
	// #nosec G304 -- statePath is fixed inside the private restore transaction directory.
	state, err := os.OpenFile(statePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("opening restore journal state: %w", err)
	}
	written, writeErr := state.Write(data)
	if writeErr == nil && written != len(data) {
		writeErr = io.ErrShortWrite
	}
	if writeErr == nil {
		writeErr = state.Sync()
	}
	closeErr := state.Close()
	if writeErr != nil {
		return fmt.Errorf("writing restore journal state: %w", writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("closing restore journal state: %w", closeErr)
	}
	if created {
		if err = m.syncRestoreDirectory(dir); err != nil {
			return fmt.Errorf("syncing restore journal directory: %w", err)
		}
	}
	journal.Sequence = event.Sequence
	return nil
}

func restoreJournalReadyToCommit(journal *restoreJournal) bool {
	for i := range journal.Entries {
		if journal.Entries[i].State != restoreEntryApplied {
			return false
		}
	}
	return !journal.UserDBRestoreUsed || journal.UserDBStarted
}

func (m *Manager) persistRestorePhase(journal *restoreJournal, phase string) error {
	valid := (journal.Phase == restorePhasePrepared && phase == restorePhaseApplying) ||
		(journal.Phase == restorePhaseApplying && phase == restorePhaseCommitted &&
			restoreJournalReadyToCommit(journal))
	if !valid {
		return fmt.Errorf("invalid restore phase transition: %s to %s", journal.Phase, phase)
	}
	if err := m.appendRestoreJournalEvent(journal, &restoreJournalEvent{
		Kind: restoreEventPhase, State: phase,
	}); err != nil {
		return err
	}
	journal.Phase = phase
	return nil
}

func (m *Manager) persistRestoreEntryState(journal *restoreJournal, index int, state string) error {
	if index < 0 || index >= len(journal.Entries) {
		return errors.New("restore journal entry index is out of range")
	}
	entry := &journal.Entries[index]
	valid := (entry.State == restoreEntryPending && state == restoreEntryStarted) ||
		(entry.State == restoreEntryStarted && state == restoreEntryApplied)
	if !valid {
		return fmt.Errorf("invalid restore entry transition: %s to %s", entry.State, state)
	}
	if err := m.appendRestoreJournalEvent(journal, &restoreJournalEvent{
		Kind: restoreEventEntry, State: state, Index: index,
	}); err != nil {
		return err
	}
	entry.State = state
	return nil
}

func (m *Manager) persistRestoreUserDBStarted(journal *restoreJournal) error {
	if !journal.UserDBRestoreUsed || journal.UserDBStarted {
		return errors.New("invalid user database restore transition")
	}
	if err := m.appendRestoreJournalEvent(journal, &restoreJournalEvent{
		Kind: restoreEventUserDBStarted,
	}); err != nil {
		return err
	}
	journal.UserDBStarted = true
	return nil
}

func applyRestoreJournalEvent(journal *restoreJournal, event *restoreJournalEvent) error {
	if event.Sequence != journal.Sequence+1 {
		return errors.New("restore journal event sequence is invalid")
	}
	switch event.Kind {
	case restoreEventPhase:
		valid := (journal.Phase == restorePhasePrepared && event.State == restorePhaseApplying) ||
			(journal.Phase == restorePhaseApplying && event.State == restorePhaseCommitted &&
				restoreJournalReadyToCommit(journal))
		if !valid {
			return errors.New("restore journal phase event is invalid")
		}
		journal.Phase = event.State
	case restoreEventEntry:
		if event.Index < 0 || event.Index >= len(journal.Entries) {
			return errors.New("restore journal entry event index is invalid")
		}
		entry := &journal.Entries[event.Index]
		valid := (entry.State == restoreEntryPending && event.State == restoreEntryStarted) ||
			(entry.State == restoreEntryStarted && event.State == restoreEntryApplied)
		if !valid {
			return errors.New("restore journal entry event is invalid")
		}
		entry.State = event.State
	case restoreEventUserDBStarted:
		if event.State != "" || !journal.UserDBRestoreUsed || journal.UserDBStarted {
			return errors.New("restore journal user database event is invalid")
		}
		journal.UserDBStarted = true
	default:
		return errors.New("restore journal event kind is invalid")
	}
	journal.Sequence = event.Sequence
	return nil
}

func readRestoreJournalEvents(path string, journal *restoreJournal) error {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stating restore journal state: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() > int64(maxRestoreJournalStateBytes) {
		return errors.New("restore journal state is invalid")
	}
	// #nosec G304 -- path is fixed inside the private restore transaction directory.
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading restore journal state: %w", err)
	}
	lines := bytes.Split(data, []byte{'\n'})
	complete := len(lines)
	if !bytes.HasSuffix(data, []byte{'\n'}) {
		complete--
	}
	for _, line := range lines[:complete] {
		if len(line) == 0 {
			continue
		}
		if len(line)+1 > maxRestoreJournalEventBytes {
			return errors.New("restore journal event exceeds size limit")
		}
		var event restoreJournalEvent
		if err = json.Unmarshal(line, &event); err != nil {
			return fmt.Errorf("decoding restore journal event: %w", err)
		}
		if applyErr := applyRestoreJournalEvent(journal, &event); applyErr != nil {
			return applyErr
		}
	}
	return nil
}

func (m *Manager) readRestoreJournal() (restoreJournal, error) {
	var journal restoreJournal
	planPath := filepath.Join(m.restoreTransactionPath(), restoreJournalPlanName)
	// #nosec G304 -- planPath is fixed inside the private restore transaction directory.
	data, err := os.ReadFile(planPath)
	if errors.Is(err, os.ErrNotExist) {
		return journal, os.ErrNotExist
	}
	if err != nil {
		return journal, fmt.Errorf("reading restore journal plan: %w", err)
	}
	if err = json.Unmarshal(data, &journal); err != nil {
		return journal, fmt.Errorf("decoding restore journal plan: %w", err)
	}
	if journal.Version != restoreJournalVersion || journal.OperationID == "" || journal.Sequence != 1 ||
		(journal.Phase != restorePhasePrepared && journal.Phase != restorePhaseApplying &&
			journal.Phase != restorePhaseCommitted) {
		return journal, errors.New("invalid restore journal")
	}
	if stateErr := readRestoreJournalEvents(
		filepath.Join(m.restoreTransactionPath(), restoreJournalStateName), &journal,
	); stateErr != nil {
		return journal, stateErr
	}
	if journal.Phase == restorePhaseCommitted || journal.Phase == restorePhasePrepared {
		return journal, nil
	}
	if validateErr := m.validateRestoreJournal(&journal); validateErr != nil {
		return journal, validateErr
	}
	return journal, nil
}

func (m *Manager) validateRestoreJournal(journal *restoreJournal) error {
	operationID, err := hex.DecodeString(journal.OperationID)
	if err != nil || len(operationID) != 12 {
		return errors.New("restore journal has invalid operation ID")
	}
	if journal.MaxLogicalSize <= 0 || journal.MaxLogicalSize == math.MaxInt64 {
		return errors.New("restore journal has invalid logical size limit")
	}
	if len(journal.Entries) > maxArchiveEntries-1 {
		return errors.New("restore journal has too many entries")
	}
	files := make([]FileRef, 0, len(journal.Entries)+1)
	entryTargets := make(map[string][]string, len(journal.Entries))
	var total int64
	for i := range journal.Entries {
		entry := &journal.Entries[i]
		if entry.State != restoreEntryPending && entry.State != restoreEntryStarted &&
			entry.State != restoreEntryApplied {
			return errors.New("restore journal has invalid entry state")
		}
		if journal.Phase == restorePhasePrepared && entry.State != restoreEntryPending {
			return errors.New("prepared restore journal contains started entry")
		}
		if journal.Phase == restorePhaseCommitted && entry.State != restoreEntryApplied {
			return errors.New("committed restore journal contains incomplete entry")
		}
		files = append(files, entry.File)
		if journal.MaxLogicalSize <= 0 || entry.File.Size < 0 ||
			entry.File.Size > journal.MaxLogicalSize-total {
			return errors.New("restore journal exceeds backup size limit")
		}
		total += entry.File.Size
		hash, err := hex.DecodeString(entry.File.SHA256)
		if err != nil || len(hash) != sha256.Size {
			return fmt.Errorf("restore journal has invalid payload hash: %s", entry.File.RestorePath)
		}
		if entry.Existed {
			expectedRollback := filepath.Join(restoreRollbackDir, fmt.Sprintf("%06d", i))
			rollbackHash, hashErr := hex.DecodeString(entry.RollbackSHA256)
			if entry.RollbackPath != expectedRollback || entry.RollbackSize < 0 ||
				hashErr != nil || len(rollbackHash) != sha256.Size {
				return errors.New("restore journal has invalid rollback metadata")
			}
		} else if entry.RollbackPath != "" || entry.RollbackSHA256 != "" || entry.RollbackSize != 0 {
			return errors.New("restore journal has rollback metadata for a new file")
		}
		if _, err = m.validateJournalTarget(entry); err != nil {
			return err
		}
		entryTargets[entry.Root] = append(entryTargets[entry.Root], filepath.Clean(entry.Rel))
	}
	if journal.UserDBRestoreUsed {
		artifact := journal.UserDBRollback
		if artifact == nil {
			return errors.New("restore journal is missing user database rollback metadata")
		}
		artifactHash, artifactHashErr := hex.DecodeString(artifact.SHA256)
		if journal.UserDBFile == nil ||
			artifact.Path != filepath.Join(restoreRollbackDir, restoreUserDBRollbackName) ||
			artifact.Size < 0 || artifact.Size > journal.MaxLogicalSize ||
			artifactHashErr != nil || len(artifactHash) != sha256.Size {
			return errors.New("restore journal has invalid user database rollback metadata")
		}
		artifactPath := filepath.Join(m.restoreTransactionPath(), artifact.Path)
		artifactInfo, artifactErr := os.Lstat(artifactPath)
		if artifactErr != nil || !artifactInfo.Mode().IsRegular() || artifactInfo.Size() != artifact.Size {
			return errors.New("restore journal user database rollback artifact is unavailable")
		}
		hash, hashErr := hex.DecodeString(journal.UserDBFile.SHA256)
		if journal.UserDBFile.Category != CategoryZaparoo ||
			journal.UserDBFile.RestorePath != "user.db" || journal.UserDBFile.Size < 0 ||
			journal.UserDBFile.Size > journal.MaxLogicalSize-total ||
			hashErr != nil || len(hash) != sha256.Size {
			return errors.New("restore journal has invalid user database payload metadata")
		}
		files = append(files, *journal.UserDBFile)
	} else if journal.UserDBFile != nil || journal.UserDBRollback != nil || journal.UserDBStarted {
		return errors.New("restore journal has unexpected user database metadata")
	}
	if err := validateFiles(files); err != nil {
		return fmt.Errorf("restore journal contains invalid files: %w", err)
	}
	for _, dir := range journal.CreatedDirs {
		if err := validateRestorePath(filepath.ToSlash(dir.Rel)); err != nil {
			return errors.New("restore journal contains invalid created directory")
		}
		valid := false
		for _, target := range entryTargets[dir.Root] {
			if strings.HasPrefix(target, filepath.Clean(dir.Rel)+string(os.PathSeparator)) {
				valid = true
				break
			}
		}
		if !valid {
			return errors.New("restore journal created directory is not a target parent")
		}
	}
	return nil
}

func (m *Manager) restoreTransactionPath() string {
	return filepath.Join(helpers.DataDir(m.pl), "backups", restoreTransactionDir)
}

func (m *Manager) syncRestoreDirectory(path string) error {
	if m.directorySync != nil {
		return m.directorySync(path)
	}
	return syncDirectory(path)
}

func syncDirectory(path string) error {
	// #nosec G304 -- callers pass validated restore transaction or target directories.
	dir, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("opening directory for sync: %w", err)
	}
	syncErr := dir.Sync()
	closeErr := dir.Close()
	if syncErr != nil {
		return fmt.Errorf("syncing directory: %w", syncErr)
	}
	if closeErr != nil {
		return fmt.Errorf("closing synced directory: %w", closeErr)
	}
	return nil
}

func (m *Manager) removeRestoreTransaction(dir string) error {
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("removing restore transaction: %w", err)
	}
	if err := m.syncRestoreDirectory(filepath.Dir(dir)); err != nil {
		return fmt.Errorf("syncing restore transaction removal: %w", err)
	}
	return nil
}

func newRestoreOperationID() (string, error) {
	var value [12]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generating restore operation ID: %w", err)
	}
	return hex.EncodeToString(value[:]), nil
}

func hasUserDBRestore(files []FileRef) bool {
	for _, file := range files {
		if file.Category == CategoryZaparoo && file.RestorePath == "user.db" {
			return true
		}
	}
	return false
}
